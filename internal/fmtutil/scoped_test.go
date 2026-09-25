package fmtutil_test

import (
	"testing"

	"github.com/lexfrei/ailine/internal/fmtutil"
)

const (
	fableID          = "claude-fable-5"
	fableDisplay     = "Fable 5"
	fableBucket      = "Fable"
	fableLabel       = "7d-fable"
	opusID           = "claude-opus-4-8"
	opusDisplay      = "Opus 4.8"
	sonnetID         = "claude-sonnet-5"
	sonnetDisplay    = "Sonnet 5"
	sonnetBucketName = "sonnet"
	opus45Display    = "Opus 4.5"
	sonnet45Display  = "Sonnet 4.5"
	sonnet45ID       = "claude-sonnet-4-5"
	sonnet45DatedID  = "claude-sonnet-4-5-20250929"
)

func TestMatchesModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     string
		displayName string
		bucket      string
		want        bool
	}{
		{"fable id matches server bucket", fableID, fableDisplay, fableBucket, true},
		{"opus id matches opus bucket", opusID, opusDisplay, "opus", true},
		{"sonnet id matches sonnet bucket", sonnetID, sonnetDisplay, sonnetBucketName, true},
		{"opus model does not match fable bucket", opusID, opusDisplay, fableBucket, false},
		{"falls back to display name when id is empty", "", sonnetDisplay, sonnetBucketName, true},
		{"empty bucket matches nothing", sonnetID, sonnetDisplay, "", false},
		{"non-model bucket does not match", opusID, opusDisplay, "oauth", false},
		{"multi-word bucket still matches the id", fableID, fableDisplay, fableDisplay, true},
		// The server writes versions with dots ("Claude Opus 4.5") while model
		// ids use dashes ("claude-opus-4-5"); both spellings name one model.
		{"dotted version matches a dashed id", "claude-opus-4-5", opus45Display, "Claude " + opus45Display, true},
		{"dotted bucket matches the dotted display name", "", opus45Display, opus45Display, true},
		// Consecutive versions of a family are served at once, so the server can
		// report a bucket per version. A bucket naming an older version is a
		// plain prefix of the newer model's id and must not match it, or the
		// statusline would show the quota of a model the session is not running.
		{
			"older version bucket does not match a newer model", sonnet45DatedID, sonnet45Display,
			"Claude Sonnet 4", false,
		},
		{
			"matching version bucket matches a dated id", sonnet45DatedID, sonnet45Display,
			"Claude " + sonnet45Display, true,
		},
		// A bucket that names no version is the family bucket: it covers every
		// version of that family.
		{"family bucket matches a versioned id", sonnet45ID, sonnet45Display, sonnetBucketName, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fmtutil.MatchesModel(tt.modelID, tt.displayName, tt.bucket)
			if got != tt.want {
				t.Errorf("MatchesModel(%q, %q, %q) = %v, want %v",
					tt.modelID, tt.displayName, tt.bucket, got, tt.want)
			}
		})
	}
}

// The server sends scope.model.id (null today). Once it carries a value, an
// exact id comparison is authoritative and must win over the name heuristic —
// including when the names would have disagreed.
func TestMatchesModelPrefersTheServerID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		modelID     string
		bucketID    string
		bucketName  string
		displayName string
		want        bool
	}{
		{
			name:       "matching ids match whatever the names say",
			modelID:    fableID,
			bucketID:   fableID,
			bucketName: "Something Else",
			want:       true,
		},
		{
			name:       "differing ids do not match even when the names would",
			modelID:    sonnet45ID,
			bucketID:   "claude-sonnet-4",
			bucketName: sonnetBucketName,
			want:       false,
		},
		{
			name:        "an empty bucket id falls back to the name",
			modelID:     fableID,
			bucketID:    "",
			bucketName:  fableBucket,
			displayName: fableDisplay,
			want:        true,
		},
		// One model is written both dated and undated. Comparing ids for equality
		// would call these two different models and drop the segment entirely.
		{
			name:       "an undated bucket id matches the dated model id",
			modelID:    sonnet45DatedID,
			bucketID:   sonnet45ID,
			bucketName: sonnet45Display,
			want:       true,
		},
		{
			name:       "a dated bucket id matches the undated model id",
			modelID:    sonnet45ID,
			bucketID:   sonnet45DatedID,
			bucketName: sonnet45Display,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fmtutil.MatchesScopedModel(tt.modelID, tt.displayName,
				fmtutil.ScopedWindow{ID: tt.bucketID, Name: tt.bucketName})
			if got != tt.want {
				t.Errorf("MatchesScopedModel(%q, %q, {ID: %q, Name: %q}) = %v, want %v",
					tt.modelID, tt.displayName, tt.bucketID, tt.bucketName, got, tt.want)
			}
		})
	}
}

func TestPerModelWindows(t *testing.T) {
	t.Parallel()

	win := func(util float64) *fmtutil.QuotaWindow {
		return &fmtutil.QuotaWindow{Utilization: util, TotalMinutes: fmtutil.SevenDayWindowMinutes}
	}

	data := &fmtutil.Data{
		SevenDayOpus:      win(10),
		SevenDayOAuthApps: win(20),
		Scoped: []fmtutil.ScopedWindow{
			{Name: fableBucket, Window: win(100)},
			// The server may report a model both as a top-level field and as a
			// limits[] entry; it must not render twice.
			{Name: "Opus", Window: win(10)},
		},
	}

	got := data.PerModelWindows()

	labels := make([]string, 0, len(got))
	for _, w := range got {
		labels = append(labels, fmtutil.ScopedLabel(w.Name))
	}

	want := []string{"7d-opus", "7d-oauth", fableLabel}
	if len(labels) != len(want) {
		t.Fatalf("expected windows %v, got %v", want, labels)
	}

	for i, label := range want {
		if labels[i] != label {
			t.Errorf("window %d = %q, want %q", i, labels[i], label)
		}
	}
}

// The same model can arrive twice under one label — as a top-level field and as
// a limits[] entry. Only one of them can render, and it must be the one closest
// to its limit, the same rule MostBinding follows: keeping the first would let a
// green 20% window mask the 96% quota that actually binds.
func TestPerModelWindowsLabelCollisionKeepsTheBindingWindow(t *testing.T) {
	t.Parallel()

	win := func(util float64) *fmtutil.QuotaWindow {
		return &fmtutil.QuotaWindow{Utilization: util, TotalMinutes: fmtutil.SevenDayWindowMinutes}
	}

	tests := []struct {
		name      string
		topLevel  *fmtutil.QuotaWindow
		scoped    *fmtutil.QuotaWindow
		wantUtil  float64
		wantCount int
	}{
		{"scoped bucket binds", win(20), win(96), 96, 1},
		{"top-level bucket binds", win(96), win(20), 96, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := &fmtutil.Data{
				SevenDaySonnet: tt.topLevel,
				Scoped:         []fmtutil.ScopedWindow{{Name: "Sonnet", Window: tt.scoped}},
			}

			got := data.PerModelWindows()
			if len(got) != tt.wantCount {
				t.Fatalf("expected %d window, got %+v", tt.wantCount, got)
			}

			if got[0].Window.Utilization != tt.wantUtil {
				t.Errorf("kept the %v%% window, want the %v%% one that binds",
					got[0].Window.Utilization, tt.wantUtil)
			}
		})
	}
}

// Candidates are uncollapsed on purpose: two buckets sharing a label may name
// different models, and a caller selecting by model has to see both to pick the
// one that applies.
func TestPerModelCandidatesKeepsCollidingLabels(t *testing.T) {
	t.Parallel()

	win := func(util float64) *fmtutil.QuotaWindow {
		return &fmtutil.QuotaWindow{Utilization: util, TotalMinutes: fmtutil.SevenDayWindowMinutes}
	}

	data := &fmtutil.Data{
		SevenDayOpus: win(20),
		Scoped: []fmtutil.ScopedWindow{
			{Name: "Opus", ID: "claude-opus-4-5", Window: win(96)},
		},
	}

	got := data.PerModelCandidates()
	if len(got) != 2 {
		t.Fatalf("expected both colliding candidates, got %+v", got)
	}

	// The collapsed view keeps one, and that is the difference the caller relies on.
	if collapsed := data.PerModelWindows(); len(collapsed) != 1 {
		t.Errorf("expected the collapsed view to keep one window, got %+v", collapsed)
	}
}

func TestPerModelCandidatesNilData(t *testing.T) {
	t.Parallel()

	var data *fmtutil.Data

	if got := data.PerModelCandidates(); len(got) != 0 {
		t.Errorf("expected no candidates, got %+v", got)
	}
}

func TestPerModelWindowsEmpty(t *testing.T) {
	t.Parallel()

	if got := (&fmtutil.Data{}).PerModelWindows(); len(got) != 0 {
		t.Errorf("expected no windows, got %+v", got)
	}
}

// A cache miss yields a nil *Data, which FindExhaustedWindow accepts — so the
// idiomatic FindExhaustedWindow(d, d.PerModelWindows()) must survive it too.
func TestPerModelWindowsNilData(t *testing.T) {
	t.Parallel()

	var data *fmtutil.Data

	if got := data.PerModelWindows(); len(got) != 0 {
		t.Errorf("expected no windows, got %+v", got)
	}

	if got := fmtutil.FindExhaustedWindow(data, data.PerModelWindows()); got != nil {
		t.Errorf("expected no exhausted window, got %+v", got)
	}
}

func TestMostBinding(t *testing.T) {
	t.Parallel()

	win := func(util float64) *fmtutil.QuotaWindow {
		return &fmtutil.QuotaWindow{Utilization: util}
	}

	tests := []struct {
		name string
		in   []fmtutil.ScopedWindow
		want string
	}{
		{
			// The usual case: the narrower bucket is also the one filling up.
			name: "narrower bucket wins when it is closer to its limit",
			in: []fmtutil.ScopedWindow{
				{Name: sonnetBucketName, Window: win(30)},
				{Name: sonnet45Display, Window: win(60)},
			},
			want: sonnet45Display,
		},
		{
			// The case that rules out picking by name: dropping the family
			// window here would hide the quota about to run out.
			name: "family bucket wins when it is the one about to run out",
			in: []fmtutil.ScopedWindow{
				{Name: sonnetBucketName, Window: win(95)},
				{Name: sonnet45Display, Window: win(30)},
			},
			want: sonnetBucketName,
		},
		{
			name: "ties keep the first window",
			in: []fmtutil.ScopedWindow{
				{Name: sonnetBucketName, Window: win(50)},
				{Name: sonnet45Display, Window: win(50)},
			},
			want: sonnetBucketName,
		},
		{
			name: "a single window passes through",
			in:   []fmtutil.ScopedWindow{{Name: fableBucket, Window: win(10)}},
			want: fableBucket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := fmtutil.MostBinding(tt.in)
			if len(got) != 1 {
				t.Fatalf("expected exactly one window, got %+v", got)
			}

			if got[0].Name != tt.want {
				t.Errorf("kept %q, want %q", got[0].Name, tt.want)
			}
		})
	}
}

func TestMostBindingEmpty(t *testing.T) {
	t.Parallel()

	if got := fmtutil.MostBinding(nil); len(got) != 0 {
		t.Errorf("expected no windows, got %+v", got)
	}
}

// MostBinding is exported and takes a caller-built slice, so a window without
// data must be skipped rather than panic — every other window consumer guards it.
func TestMostBindingSkipsNilWindows(t *testing.T) {
	t.Parallel()

	got := fmtutil.MostBinding([]fmtutil.ScopedWindow{
		{Name: sonnetBucketName, Window: nil},
		{Name: fableBucket, Window: &fmtutil.QuotaWindow{Utilization: 10}},
	})

	if len(got) != 1 || got[0].Name != fableBucket {
		t.Errorf("expected the window carrying data, got %+v", got)
	}
}

func TestMostBindingAllNilWindows(t *testing.T) {
	t.Parallel()

	got := fmtutil.MostBinding([]fmtutil.ScopedWindow{
		{Name: sonnetBucketName, Window: nil},
		{Name: fableBucket, Window: nil},
	})

	if len(got) != 0 {
		t.Errorf("expected no windows when none carry data, got %+v", got)
	}
}

func TestScopedLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bucket string
		want   string
	}{
		{"single word", fableBucket, fableLabel},
		{"spaces become dashes", fableDisplay, "7d-fable-5"},
		{"already lowercase", "opus", "7d-opus"},
		{"surrounding space trimmed", " Fable ", fableLabel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := fmtutil.ScopedLabel(tt.bucket); got != tt.want {
				t.Errorf("ScopedLabel(%q) = %q, want %q", tt.bucket, got, tt.want)
			}
		})
	}
}
