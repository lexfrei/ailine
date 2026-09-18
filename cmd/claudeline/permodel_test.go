package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexfrei/claudeline/internal/config"
	"github.com/lexfrei/claudeline/internal/httpclient"
	"github.com/lexfrei/claudeline/internal/keychain"
	"github.com/lexfrei/claudeline/internal/usage"
)

const (
	flagConfig        = "--config"
	flagPerModelQuota = "--per-model-quota"
	// valueFalse is the spelling a mode flag takes on the command line.
	valueFalse = "false"
)

// stubScopedUsageAPI serves a usage response holding a top-level Opus window and
// a Fable window delivered the way the API actually delivers per-model quotas:
// as a model-scoped entry of limits[].
func stubScopedUsageAPI(t *testing.T) {
	t.Helper()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: []byte(`{
				"seven_day": {"utilization": 45, "resets_at": "` + resetsAt + `"},
				"seven_day_opus": {"utilization": 12, "resets_at": "` + resetsAt + `"},
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 90,
					 "resets_at": "` + resetsAt + `",
					 "scope": {"model": {"id": null, "display_name": "Fable"}}}
				]
			}`),
		}, nil
	}
}

func modelStdin(id, displayName string) *stdinData {
	data := &stdinData{}
	data.Model.ID = id
	data.Model.DisplayName = displayName

	return data
}

func TestPerModelQuotaAutoShowsSelectedModelOnly(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubScopedUsageAPI(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-fable-5", "Fable 5"), cfg), " | ")

	if !strings.Contains(joined, "7d-fable: 90%") {
		t.Errorf("expected the Fable window while Fable is selected, got %q", joined)
	}

	if strings.Contains(joined, "7d-opus") {
		t.Errorf("expected no Opus window while Fable is selected, got %q", joined)
	}

	if !strings.Contains(joined, "7d: 45%") {
		t.Errorf("expected the account-wide window, got %q", joined)
	}
}

func TestPerModelQuotaAutoFollowsTheModel(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubScopedUsageAPI(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-opus-4-8", "Opus 4.8"), cfg), " | ")

	if !strings.Contains(joined, "7d-opus: 12%") {
		t.Errorf("expected the Opus window while Opus is selected, got %q", joined)
	}

	if strings.Contains(joined, "7d-fable") {
		t.Errorf("expected no Fable window while Opus is selected, got %q", joined)
	}
}

func TestPerModelQuotaAllShowsEveryWindow(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubScopedUsageAPI(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAll

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-opus-4-8", "Opus 4.8"), cfg), " | ")

	for _, want := range []string{"7d-opus: 12%", "7d-fable: 90%"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q in all-windows mode, got %q", want, joined)
		}
	}
}

func TestPerModelQuotaOffHidesEveryWindow(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubScopedUsageAPI(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelOff

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-fable-5", "Fable 5"), cfg), " | ")

	for _, unwanted := range []string{"7d-fable", "7d-opus"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("expected no %s window when per-model quotas are off, got %q", unwanted, joined)
		}
	}

	if !strings.Contains(joined, "7d: 45%") {
		t.Errorf("expected the account-wide window to survive, got %q", joined)
	}
}

// A model can be reported by two buckets at once — the coarse family window
// (seven_day_sonnet) and a finer server-named one ("Sonnet 4.5"). Both match the
// selected model, so auto mode must pick exactly one, or the statusline renders
// the same model twice. It keeps whichever is closer to its limit: usually the
// narrower bucket, but never at the price of hiding the quota about to run out.
func TestPerModelQuotaAutoPicksTheBindingBucket(t *testing.T) {
	tests := []struct {
		name        string
		familyPct   int
		scopedPct   int
		wantSegment string
		wantDropped string
	}{
		{
			name:        "narrower bucket is the one filling up",
			familyPct:   30,
			scopedPct:   60,
			wantSegment: "7d-sonnet-4-5: 60%",
			wantDropped: "7d-sonnet: 30%",
		},
		{
			name:        "family bucket is the one about to run out",
			familyPct:   95,
			scopedPct:   30,
			wantSegment: "7d-sonnet: 95%",
			wantDropped: "7d-sonnet-4-5: 30%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleanup := setupTestEnv(t)
			defer cleanup()

			resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

			keychain.GetFn = func() (string, error) { return testToken, nil }
			usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
				return &httpclient.Response{
					StatusCode: http.StatusOK,
					Body: fmt.Appendf(nil, `{
						"seven_day_sonnet": {"utilization": %d, "resets_at": %q},
						"limits": [
							{"kind": "weekly_scoped", "group": "weekly", "percent": %d,
							 "resets_at": %q,
							 "scope": {"model": {"display_name": "Sonnet 4.5"}}}
						]
					}`, tt.familyPct, resetsAt, tt.scopedPct, resetsAt),
				}, nil
			}

			cfg := insecureCfg()
			cfg.Segments.PerModelQuota = config.PerModelAuto

			joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-sonnet-4-5", "Sonnet 4.5"), cfg), " | ")

			if !strings.Contains(joined, tt.wantSegment) {
				t.Errorf("expected %q, got %q", tt.wantSegment, joined)
			}

			if strings.Contains(joined, tt.wantDropped) {
				t.Errorf("expected %q to be dropped as a duplicate, got %q", tt.wantDropped, joined)
			}
		})
	}
}

// Several versions of a family run at once, so the server reports a bucket per
// version. The bucket of the version the session is NOT running must never be
// picked, however close to its limit it sits — a red segment for someone else's
// quota is worse than no segment at all.
func TestPerModelQuotaAutoIgnoresOtherVersionsOfTheFamily(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: fmt.Appendf(nil, `{
				"seven_day": {"utilization": 40, "resets_at": %q},
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 95, "resets_at": %q,
					 "scope": {"model": {"display_name": "Claude Sonnet 4"}}},
					{"kind": "weekly_scoped", "group": "weekly", "percent": 12, "resets_at": %q,
					 "scope": {"model": {"display_name": "Claude Sonnet 4.5"}}}
				]
			}`, resetsAt, resetsAt, resetsAt),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-sonnet-4-5-20250929", "Sonnet 4.5"), cfg), " | ")

	if !strings.Contains(joined, "7d-claude-sonnet-4-5: 12%") {
		t.Errorf("expected the running model's own bucket, got %q", joined)
	}

	if strings.Contains(joined, "7d-claude-sonnet-4: 95%") {
		t.Errorf("expected the Sonnet 4 bucket to be ignored on a Sonnet 4.5 session, got %q", joined)
	}
}

// When the server scopes a bucket to a model id, that id decides the match — the
// name is only a fallback. Pinned so a bucket whose name would not match still
// renders when its id does.
func TestPerModelQuotaAutoMatchesOnServerID(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: fmt.Appendf(nil, `{
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 42, "resets_at": %q,
					 "scope": {"model": {"id": "claude-fable-5", "display_name": "Storyteller"}}}
				]
			}`, resetsAt),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-fable-5", "Fable 5"), cfg), " | ")

	if !strings.Contains(joined, "7d-storyteller: 42%") {
		t.Errorf("expected the bucket matched by server id, got %q", joined)
	}
}

// Two buckets can share a label while naming different models: the family window
// (seven_day_opus, covering every Opus) and a limits[] entry scoped to one
// version. Auto mode must pick among the buckets that match the running model —
// collapsing the label first would let the non-matching version win the collision
// and take the applicable family window down with it.
func TestPerModelQuotaAutoKeepsTheApplicableWindowOnLabelCollision(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: fmt.Appendf(nil, `{
				"seven_day_opus": {"utilization": 20, "resets_at": %q},
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 96, "resets_at": %q,
					 "scope": {"model": {"id": "claude-opus-4-5", "display_name": "Opus"}}}
				]
			}`, resetsAt, resetsAt),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-opus-4-8", "Opus 4.8"), cfg), " | ")

	if !strings.Contains(joined, "7d-opus: 20%") {
		t.Errorf("expected the family window that applies to this Opus, got %q", joined)
	}

	if strings.Contains(joined, "96%") {
		t.Errorf("expected the quota of another Opus version to stay hidden, got %q", joined)
	}
}

// stdin carries the dated model id while the server scopes its bucket to the
// undated one. They name one model, so the segment must still render — comparing
// the ids for equality would silently drop it.
func TestPerModelQuotaAutoMatchesDatedModelID(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: fmt.Appendf(nil, `{
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 55, "resets_at": %q,
					 "scope": {"model": {"id": "claude-sonnet-4-5", "display_name": "Sonnet 4.5"}}}
				]
			}`, resetsAt),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	stdin := modelStdin("claude-sonnet-4-5-20250929", "Sonnet 4.5")

	joined := strings.Join(appendUsageSegments(nil, stdin, cfg), " | ")

	if !strings.Contains(joined, "7d-sonnet-4-5: 55%") {
		t.Errorf("expected the dated model id to match the undated bucket id, got %q", joined)
	}
}

// stubExhaustedFableCache primes the last-good cache with a spent Fable bucket
// and makes the API answer 429, which is the state the rate-limit segment reads.
func stubExhaustedFableCache(t *testing.T) {
	t.Helper()

	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)

	if _, err := usage.ParseBody([]byte(`{
		"seven_day": {"utilization": 40, "resets_at": "` + resetsAt + `"},
		"limits": [
			{"kind": "weekly_scoped", "group": "weekly", "percent": 100, "is_active": true,
			 "resets_at": "` + resetsAt + `",
			 "scope": {"model": {"display_name": "Fable"}}}
		]
	}`)); err != nil {
		t.Fatalf("priming last-good cache: %v", err)
	}

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}}, nil
	}
}

// A model-scoped bucket may name the exhausted limit — the label comes from the
// server's bucket name, not from a fixed list.
func TestRateLimitSegmentNamesTheScopedWindow(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubExhaustedFableCache(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-fable-5", "Fable 5"), cfg), " | ")

	if !strings.Contains(joined, "7d-fable limit hit") {
		t.Errorf("expected the spent Fable bucket to name the limit, got %q", joined)
	}
}

// The mirror image: a window auto mode hides must never name the limit, or the
// statusline would blame a quota it is not showing.
func TestRateLimitSegmentIgnoresHiddenWindow(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubExhaustedFableCache(t)

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAuto

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-opus-4-8", "Opus 4.8"), cfg), " | ")

	if strings.Contains(joined, "7d-fable") {
		t.Errorf("expected the hidden Fable bucket to stay unnamed while Opus is selected, got %q", joined)
	}

	if !strings.Contains(joined, "limit hit") {
		t.Errorf("expected a rate-limit segment, got %q", joined)
	}
}

// Every documented spelling of the flag must parse and resolve to the mode the
// docs promise. The "=" form is the documented one: a bare --per-model-quota has
// to keep meaning "all", which forces NoOptDefVal, which in turn makes the space
// form leave its value behind as a positional argument and blank the statusline.
func TestPerModelQuotaFlagSpellings(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"bare flag keeps meaning all", []string{flagPerModelQuota}, config.PerModelAll},
		{"explicit auto", []string{flagPerModelQuota + "=auto"}, config.PerModelAuto},
		{"explicit true", []string{flagPerModelQuota + "=true"}, config.PerModelAll},
		{"explicit false", []string{flagPerModelQuota + "=false"}, config.PerModelOff},
		{"capitalized bool still parses", []string{flagPerModelQuota + "=True"}, config.PerModelAll},
		{"unset leaves the default", nil, config.PerModelAuto},
		{"invalid value falls back to auto", []string{flagPerModelQuota + "=bogus"}, config.PerModelAuto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(append([]string{flagConfig, ""}, tt.args...))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
			cmd.Run = nil

			if err := cmd.Execute(); err != nil {
				t.Fatalf("executing %v: %v", tt.args, err)
			}

			cfg := config.Defaults()
			applyFlagOverrides(cmd, &cfg)

			if cfg.Segments.PerModelQuota != tt.want {
				t.Errorf("per-model quota = %q, want %q", cfg.Segments.PerModelQuota, tt.want)
			}
		})
	}
}

// The flag and the config key feed the same normalizer, so every spelling the
// config accepts must resolve identically on the command line — and an unknown
// value must warn rather than pass silently as auto.
func TestCostFlagSpellings(t *testing.T) {
	tests := []struct {
		value string
		want  string
	}{
		{"auto", config.CostAuto},
		{"true", config.CostOn},
		{"on", config.CostOn},
		{"1", config.CostOn},
		{"True", config.CostOn},
		{valueFalse, config.CostOff},
		{"off", config.CostOff},
		{badModeValue, config.CostAuto},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{flagConfig, "", "--cost", tt.value})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
			cmd.Run = nil

			if err := cmd.Execute(); err != nil {
				t.Fatalf("executing --cost %s: %v", tt.value, err)
			}

			cfg := config.Defaults()
			applyFlagOverrides(cmd, &cfg)

			if cfg.Segments.Cost != tt.want {
				t.Errorf("--cost %s = %q, want %q", tt.value, cfg.Segments.Cost, tt.want)
			}
		})
	}
}

// End to end: a subscriber (rate limits present) hides cost under auto, so
// --cost on must be what makes the segment appear.
func TestCostFlagOnRendersForSubscriber(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stdin := []byte(`{"model":{"id":"claude-opus-4-8","display_name":"Opus 4.8"},
		"cost":{"total_cost_usd":1.23},
		"rate_limits":{"seven_day":{"used_percentage":50,"resets_at":9999999999}}}`)

	cfg := config.Defaults()
	cfg.Segments.Cost = config.NormalizeCostMode("on")

	if got := buildStatusline(stdin, &cfg); !strings.Contains(got, "$1.23") {
		t.Errorf("expected the cost segment under --cost on, got %q", got)
	}
}

// An empty value carries no choice, so it must leave the config value standing —
// the same rule --cost follows. Checked from a config that says something other
// than the default, or the assertion would pass for the wrong reason.
func TestEmptyModeFlagsLeaveTheConfigAlone(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want func(config.Config) string
	}{
		{"per-model-quota", flagPerModelQuota + "=", func(c config.Config) string { return c.Segments.PerModelQuota }},
		{"cost", "--cost=", func(c config.Config) string { return c.Segments.Cost }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs([]string{flagConfig, "", tt.arg})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
			cmd.Run = nil

			if err := cmd.Execute(); err != nil {
				t.Fatalf("executing %s: %v", tt.arg, err)
			}

			cfg := config.Defaults()
			cfg.Segments.PerModelQuota = config.PerModelOff
			cfg.Segments.Cost = config.CostOff

			applyFlagOverrides(cmd, &cfg)

			if got := tt.want(cfg); got != config.CostOff {
				t.Errorf("%s overrode the config with %q, want the config value %q", tt.arg, got, config.CostOff)
			}
		})
	}
}

// The space form cannot work: a bare --per-model-quota has to keep meaning "all",
// which requires NoOptDefVal, and such a flag never consumes the next argument —
// so the value lands as a positional arg and cobra.NoArgs rejects it. Pinned so
// that nobody "fixes" the parsing by dropping NoOptDefVal, silently changing what
// a bare flag means for everyone already passing it.
func TestPerModelQuotaSpaceFormIsRejected(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{flagConfig, "", flagPerModelQuota, "auto"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.RunE = func(_ *cobra.Command, _ []string) error { return nil }
	cmd.Run = nil

	if err := cmd.Execute(); err == nil {
		t.Error("expected the space form to be rejected, got no error")
	}
}

// In "all" mode the statusline mirrors what the API reports, bucket for bucket:
// a model covered by both a family window and a finer one shows up twice. Only
// auto collapses them, because only auto claims to show "the current model".
func TestPerModelQuotaAllKeepsOverlappingBuckets(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(72 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: []byte(`{
				"seven_day_sonnet": {"utilization": 30, "resets_at": "` + resetsAt + `"},
				"limits": [
					{"kind": "weekly_scoped", "group": "weekly", "percent": 60,
					 "resets_at": "` + resetsAt + `",
					 "scope": {"model": {"display_name": "Sonnet 4.5"}}}
				]
			}`),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAll

	joined := strings.Join(appendUsageSegments(nil, modelStdin("claude-sonnet-4-5", "Sonnet 4.5"), cfg), " | ")

	for _, want := range []string{"7d-sonnet: 30%", "7d-sonnet-4-5: 60%"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected %q — all mode reports every bucket as-is, got %q", want, joined)
		}
	}
}

// The default (secure) path reads quotas from stdin, which carries no per-model
// windows at all — so no model quota may appear there, whatever the mode says.
func TestPerModelQuotaAbsentOnStdinPath(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	stubScopedUsageAPI(t)

	cfg := config.Defaults()
	cfg.Segments.PerModelQuota = config.PerModelAll

	data := modelStdin("claude-fable-5", "Fable 5")
	data.RateLimits.SevenDay = &stdinRateWindow{
		UsedPercentage: 45,
		ResetsAt:       float64(time.Now().Add(72 * time.Hour).Unix()),
	}

	joined := strings.Join(appendUsageSegments(nil, data, &cfg), " | ")

	if strings.Contains(joined, "7d-fable") {
		t.Errorf("expected no per-model window on the stdin path, got %q", joined)
	}

	if !strings.Contains(joined, "7d: 45%") {
		t.Errorf("expected the stdin 7d window, got %q", joined)
	}
}
