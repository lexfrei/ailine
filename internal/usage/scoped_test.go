package usage_test

import (
	"testing"
	"time"

	"github.com/lexfrei/ailine/internal/usage"
)

// scopedBody mirrors the shape the Anthropic usage API returns: per-model quota
// buckets do not arrive as top-level fields but as entries in limits[] with
// kind "weekly_scoped" and a scope.model.
const scopedBody = `{
  "five_hour": {"utilization": 58.0, "resets_at": "2099-01-01T00:00:00+00:00"},
  "seven_day": {"utilization": 78.0, "resets_at": "2099-01-02T00:00:00+00:00"},
  "limits": [
    {"kind": "session", "group": "session", "percent": 58, "severity": "warning", "is_active": false,
     "resets_at": "2099-01-01T00:00:00+00:00", "scope": null},
    {"kind": "weekly_all", "group": "weekly", "percent": 78, "severity": "warning", "is_active": false,
     "resets_at": "2099-01-02T00:00:00+00:00", "scope": null},
    {"kind": "weekly_scoped", "group": "weekly", "percent": 100, "severity": "critical", "is_active": true,
     "resets_at": "2099-01-02T00:00:00+00:00",
     "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}}
  ]
}`

func TestParseBodyScopedWindows(t *testing.T) {
	t.Parallel()

	data, err := usage.ParseBody([]byte(scopedBody))
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}

	if len(data.Scoped) != 1 {
		t.Fatalf("expected 1 scoped window, got %d", len(data.Scoped))
	}

	scoped := data.Scoped[0]
	if scoped.Name != "Fable" {
		t.Errorf("expected bucket name Fable, got %q", scoped.Name)
	}

	if scoped.Window == nil {
		t.Fatal("expected a parsed quota window, got nil")
	}

	if scoped.Window.Utilization != 100 {
		t.Errorf("expected utilization 100, got %v", scoped.Window.Utilization)
	}

	want := time.Date(2099, 1, 2, 0, 0, 0, 0, time.UTC)
	if !scoped.Window.ResetsAt.Equal(want) {
		t.Errorf("expected resets_at %v, got %v", want, scoped.Window.ResetsAt)
	}
}

// is_active marks a limit that is currently biting, not one that applies: the
// account's own session and weekly limits carry false while they still have
// headroom. A bucket must therefore render regardless of the flag — filtering on
// it would hide every quota that is not yet exhausted.
func TestParseBodyScopedRendersInactiveBuckets(t *testing.T) {
	t.Parallel()

	body := `{"limits": [
	  {"kind": "weekly_scoped", "group": "weekly", "percent": 20, "severity": "normal", "is_active": false,
	   "resets_at": "2099-01-02T00:00:00+00:00",
	   "scope": {"model": {"display_name": "Fable"}}}
	]}`

	data, err := usage.ParseBody([]byte(body))
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}

	if len(data.Scoped) != 1 {
		t.Fatalf("expected the inactive bucket to survive, got %d windows", len(data.Scoped))
	}

	if data.Scoped[0].Window.Utilization != 20 {
		t.Errorf("expected utilization 20, got %v", data.Scoped[0].Window.Utilization)
	}
}

func TestParseBodyScopedIgnoresNonModelLimits(t *testing.T) {
	t.Parallel()

	// A bucket must carry a model and a usable name to become a segment: a
	// surface-scoped entry has no model, an unparseable reset has no window, and
	// a blank name would render as a bare "7d-" label.
	body := `{"limits": [
	  {"kind": "weekly_scoped", "group": "weekly", "percent": 10, "resets_at": "2099-01-02T00:00:00+00:00",
	   "scope": {"model": null, "surface": {"display_name": "Cowork"}}},
	  {"kind": "weekly_scoped", "group": "weekly", "percent": 20, "resets_at": "not-a-timestamp",
	   "scope": {"model": {"display_name": "Broken"}}},
	  {"kind": "weekly_scoped", "group": "weekly", "percent": 30, "resets_at": "2099-01-02T00:00:00+00:00",
	   "scope": {"model": {"display_name": "   "}}}
	]}`

	data, err := usage.ParseBody([]byte(body))
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}

	if len(data.Scoped) != 0 {
		t.Errorf("expected no scoped windows, got %d: %+v", len(data.Scoped), data.Scoped)
	}
}

func TestParseBodyWithoutLimitsArray(t *testing.T) {
	t.Parallel()

	// Older responses have no limits[] at all; parsing must stay clean.
	data, err := usage.ParseBody([]byte(`{"five_hour": {"utilization": 10, "resets_at": "2099-01-01T00:00:00+00:00"}}`))
	if err != nil {
		t.Fatalf("ParseBody() error = %v", err)
	}

	if data.Scoped != nil {
		t.Errorf("expected nil scoped windows, got %+v", data.Scoped)
	}
}
