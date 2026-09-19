package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lexfrei/claudeline/internal/config"
	"github.com/lexfrei/claudeline/internal/httpclient"
	"github.com/lexfrei/claudeline/internal/status"
	"github.com/lexfrei/claudeline/internal/zai"
)

// zaiBaseURL / zaiAPIKey mirror a Claude Code session pointed at Z.ai's
// Anthropic-compatible endpoint.
const (
	zaiBaseURL = "https://api.z.ai/api/anthropic"
	zaiAPIKey  = "test-zai-key"

	displayGLM52 = "GLM-5.2"
)

// glmStdin is a realistic payload for a GLM session: the harness title-cases
// unknown model ids for display_name, and carries no Anthropic rate_limits.
// extra is spliced into the top-level object.
func glmStdin(extra string) string {
	return `{"model":{"id":"glm-5.2","display_name":"Glm 5.2"},
		"effort":{"level":"high"},"thinking":{"enabled":true},` +
		`"context_window":{"used_percentage":42.0}` + extra + `}`
}

// zaiQuotaBody carries the plan windows the monitor endpoint reports: 7% of
// the 5-hour quota, 20% of the weekly one.
func zaiQuotaBody(t *testing.T) []byte {
	t.Helper()

	return []byte(`{"code":200,"msg":"Operation successful","success":true,"data":{"limits":[
		{"type":"TOKENS_LIMIT","unit":3,"number":5,"percentage":7,` +
		`"nextResetTime":` + millis(t, 3*time.Hour) + `},
		{"type":"TOKENS_LIMIT","unit":6,"number":1,"percentage":20,` +
		`"nextResetTime":` + millis(t, 5*24*time.Hour) + `}
	]},"level":"pro"}`)
}

func millis(t *testing.T, d time.Duration) string {
	t.Helper()

	return strconv.FormatInt(time.Now().Add(d).UnixMilli(), 10)
}

func TestModelDisplayNameGLM(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"current model", "glm-5.2", displayGLM52},
		{"air variant", "glm-4.5-air", "GLM-4.5-Air"},
		{"turbo variant", "glm-5-turbo", "GLM-5-Turbo"},
		{"bare family", glmFamily, glmFamilyDisplay},
		{"uppercase id", displayGLM52, displayGLM52},
		{"unknown vendor still prettified", "acme-model-x", "Acme-Model-X"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := prettifyModelID(tt.id); got != tt.want {
				t.Errorf("prettifyModelID(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestModelDisplayNamePrefersRealCatalogName(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	got := buildStatusline([]byte(`{"model":{"id":"glm-5.2","display_name":"Custom Gateway Name"}}`), defaultCfg())
	if !strings.Contains(got, "🤖 Custom Gateway Name") {
		t.Errorf("catalog display name must win, got %q", got)
	}
}

func TestBuildStatuslineGLMModelSegment(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	zai.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if !strings.Contains(got, "🤖 GLM-5.2 ⬆️💭") {
		t.Errorf("expected prettified GLM model with indicators, got %q", got)
	}
}

func TestBuildStatuslineGLMCostAutoHidden(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 200, Body: zaiQuotaBody(t)}, nil
	}
	status.HTTPGetFn = failHTTP

	const withCost = `,"cost":{"total_cost_usd":1.5}`

	got := buildStatusline([]byte(glmStdin(withCost)), defaultCfg())
	if strings.Contains(got, "💰") {
		t.Errorf("cost auto must stay hidden on a quota-metered GLM plan, got %q", got)
	}

	forced := defaultCfg()
	forced.Segments.Cost = config.CostOn

	got = buildStatusline([]byte(glmStdin(withCost)), forced)
	if !strings.Contains(got, "💰 $1.50") {
		t.Errorf("cost=true must still render, got %q", got)
	}
}

func TestBuildStatuslineZaiQuotaWindows(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 200, Body: zaiQuotaBody(t)}, nil
	}
	// The Anthropic status feed must not be consulted on the Z.ai provider.
	status.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		t.Error("platform status must not be fetched for a non-Anthropic provider")

		return nil, nil //nolint:nilnil // unreachable
	}

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	for _, want := range []string{"5h: 7%", "7d: 20%"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in Z.ai quota segments, got %q", want, got)
		}
	}
}

func TestBuildStatuslineZaiModelIDFallback(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	// No base URL exported: the GLM model id alone must route to the Z.ai path.
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 200, Body: zaiQuotaBody(t)}, nil
	}
	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if !strings.Contains(got, "5h: 7%") {
		t.Errorf("model id detection must reach the Z.ai quota path, got %q", got)
	}
}

func TestBuildStatuslineZaiFetchErrorShowsPlaceholders(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = failHTTP
	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if !strings.Contains(got, "⏳ 7d: ?%") || !strings.Contains(got, "⏳ 5h: ?%") {
		t.Errorf("fetch error without last-good must render placeholders, got %q", got)
	}
}

func TestBuildStatuslineZaiKeyInvalid(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 401, Body: []byte(`{"code":401,"success":false}`)}, nil
	}
	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if !strings.Contains(got, "⚠️ key invalid") {
		t.Errorf("auth failure must name the key, got %q", got)
	}
}

func TestBuildStatuslineZaiNoKeyStaysSilent(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		t.Error("no key configured: the monitor API must not be queried")

		return nil, nil //nolint:nilnil // unreachable
	}
	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if strings.Contains(got, "⏳") || strings.Contains(got, "5h") || strings.Contains(got, "7d") {
		t.Errorf("keyless Z.ai session must render no quota segments, got %q", got)
	}
}

func TestBuildStatuslineZaiStaleAfterGoodSample(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	t.Setenv("ANTHROPIC_BASE_URL", zaiBaseURL)
	t.Setenv("ANTHROPIC_AUTH_TOKEN", zaiAPIKey)

	zai.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 200, Body: zaiQuotaBody(t)}, nil
	}
	status.HTTPGetFn = failHTTP

	if got := buildStatusline([]byte(glmStdin("")), defaultCfg()); !strings.Contains(got, "5h: 7%") {
		t.Fatalf("first render must show live windows, got %q", got)
	}

	// Expire the fresh response cache so the second render refetches (and
	// fails); the last-good sample must survive to render stale windows.
	if err := os.Remove(zai.CachePath); err != nil {
		t.Fatalf("expiring cache: %v", err)
	}

	zai.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(glmStdin("")), defaultCfg())
	if !strings.Contains(got, "5h: ?%") {
		t.Errorf("failed refetch must degrade to stale windows, got %q", got)
	}
}
