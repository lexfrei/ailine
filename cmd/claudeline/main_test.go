package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lexfrei/claudeline/internal/config"
	"github.com/lexfrei/claudeline/internal/fmtutil"
	"github.com/lexfrei/claudeline/internal/httpclient"
	"github.com/lexfrei/claudeline/internal/keychain"
	"github.com/lexfrei/claudeline/internal/status"
	"github.com/lexfrei/claudeline/internal/usage"
)

const (
	testToken       = "test-token"
	testModelOpus47 = "🤖 Opus 4.7"
	flagNoModel     = "--no-model"
	flagNoWorktree  = "--no-worktree"
)

func defaultCfg() *config.Config {
	cfg := config.Defaults()

	return &cfg
}

func insecureCfg() *config.Config {
	cfg := config.Defaults()
	cfg.MacInsecure = true

	return &cfg
}

func setupTestEnv(t *testing.T) func() {
	t.Helper()

	// buildStatusline now reads COLUMNS via wrapWidth(). Pin it to "" so that
	// tests inheriting a narrow developer terminal (or CI runner) don't see
	// spurious wraps. Tests that exercise the wrap path must call t.Setenv
	// themselves to opt in.
	t.Setenv("COLUMNS", "sentinel")
	os.Unsetenv("COLUMNS")

	dir := t.TempDir()

	origStatusPath := status.CachePath
	origUsagePath := usage.CachePath
	origLastGoodPath := usage.LastGoodCachePath
	origRetryAfterPath := usage.RetryAfterPath
	origAuthFailPath := usage.AuthFailPath
	origStatusHTTP := status.HTTPGetFn
	origUsageHTTP := usage.HTTPGetFn
	origToken := keychain.GetFn
	origStatusTTL := status.CacheTTL
	origUsageTTL := usage.CacheTTL

	status.CachePath = filepath.Join(dir, "status-cache.json")
	usage.CachePath = filepath.Join(dir, "usage-cache.json")
	usage.LastGoodCachePath = filepath.Join(dir, "usage-last-good.json")
	usage.RetryAfterPath = filepath.Join(dir, "retry-after")
	usage.AuthFailPath = filepath.Join(dir, "auth-failed")

	return func() {
		status.CachePath = origStatusPath
		usage.CachePath = origUsagePath
		usage.LastGoodCachePath = origLastGoodPath
		usage.RetryAfterPath = origRetryAfterPath
		usage.AuthFailPath = origAuthFailPath
		status.HTTPGetFn = origStatusHTTP
		usage.HTTPGetFn = origUsageHTTP
		keychain.GetFn = origToken
		status.CacheTTL = origStatusTTL
		usage.CacheTTL = origUsageTTL
	}
}

func failHTTP(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
	return nil, keychain.ErrNoToken
}

func TestBuildStatuslineMinimal(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(`{}`), defaultCfg())

	if !strings.Contains(got, "🤖 Claude") {
		t.Errorf("expected model name, got %q", got)
	}

	if !strings.Contains(got, "💰 $0.00") {
		t.Errorf("expected zero cost, got %q", got)
	}

	// In default mode (no --mac-insecure), no rate_limits in stdin = no quota segments.
	if strings.Contains(got, "⏳") || strings.Contains(got, "7d") || strings.Contains(got, "5h") {
		t.Errorf("expected no quota segments without rate_limits in stdin, got %q", got)
	}

	// Empty stdin = no worktree segment even though the toggle is on by default.
	if strings.Contains(got, "🌿") {
		t.Errorf("expected no worktree without git_worktree in stdin, got %q", got)
	}
}

func TestBuildStatuslineWithStdinRateLimits(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	resetsAt := float64(time.Now().Add(3 * time.Hour).Unix())

	input := fmt.Sprintf(`{
		"model":{"display_name":"Opus 4.6"},
		"rate_limits":{
			"five_hour":{"used_percentage":30,"resets_at":%f},
			"seven_day":{"used_percentage":55,"resets_at":%f}
		}
	}`, resetsAt, resetsAt)

	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "5h: 30%") {
		t.Errorf("expected 5h quota from stdin, got %q", got)
	}

	if !strings.Contains(got, "7d: 55%") {
		t.Errorf("expected 7d quota from stdin, got %q", got)
	}

	// Should NOT contain any API-path artifacts.
	if strings.Contains(got, "⏳") || strings.Contains(got, "💳") || strings.Contains(got, "/login") {
		t.Errorf("expected no API-path artifacts, got %q", got)
	}
}

func TestBuildStatuslineStdinNoRateLimits(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(`{"model":{"display_name":"Sonnet"}}`), defaultCfg())

	if !strings.Contains(got, "🤖 Sonnet") {
		t.Errorf("expected model name, got %q", got)
	}

	// No rate_limits = no quota segments (graceful).
	if strings.Contains(got, "7d") || strings.Contains(got, "5h") {
		t.Errorf("expected no quota without rate_limits, got %q", got)
	}
}

func TestBuildStatuslineStdinPartialRateLimits(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	resetsAt := float64(time.Now().Add(2 * time.Hour).Unix())

	input := fmt.Sprintf(`{"rate_limits":{"five_hour":{"used_percentage":42,"resets_at":%f}}}`, resetsAt)

	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "5h: 42%") {
		t.Errorf("expected 5h quota, got %q", got)
	}

	// seven_day is absent — should not appear.
	if strings.Contains(got, "7d") {
		t.Errorf("expected no 7d without seven_day in stdin, got %q", got)
	}
}

func TestBuildStatuslineWithWorktree(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.6"},"workspace":{"git_worktree":"feat-api"}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🌿 feat-api") {
		t.Errorf("expected worktree segment, got %q", got)
	}

	// Worktree should appear after context/compactions, before quotas.
	if !strings.HasSuffix(got, "🌿 feat-api") {
		t.Errorf("expected worktree as last segment (no quotas in this input), got %q", got)
	}
}

func TestBuildStatuslineNoWorktreeField(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.6"},"workspace":{}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if strings.Contains(got, "🌿") {
		t.Errorf("expected no worktree segment when field is empty, got %q", got)
	}
}

func TestBuildStatuslineWorktreeWithModelDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Model = false

	input := `{"workspace":{"git_worktree":"feat-api"}}`
	got := buildStatusline([]byte(input), cfg)

	if !strings.Contains(got, "🌿 feat-api") {
		t.Errorf("expected worktree even when model disabled, got %q", got)
	}

	if strings.Contains(got, "🤖") {
		t.Errorf("expected no model segment, got %q", got)
	}
}

func TestBuildStatuslineWorktreeDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Worktree = false

	input := `{"model":{"display_name":"Opus 4.6"},"workspace":{"git_worktree":"feat-api"}}`
	got := buildStatusline([]byte(input), cfg)

	if strings.Contains(got, "🌿") {
		t.Errorf("expected no worktree when segment disabled, got %q", got)
	}
}

func TestBuildStatuslineRepoSegment(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "github repo only",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"}}}`,
			expected: "🐙 lexfrei/claudeline",
		},
		{
			name:     "github repo with branch fallback",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"},"git_worktree":"feat-api"}}`,
			expected: "🐙 lexfrei/claudeline 🌿 feat-api",
		},
		{
			name:     "github repo with draft PR",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"}},"pr":{"number":19,"review_state":"draft"}}`,
			expected: "🐙 lexfrei/claudeline 📝 #19",
		},
		{
			name:     "github repo with approved PR and branch",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"},"git_worktree":"feat-api"},"pr":{"number":42,"review_state":"approved"}}`,
			expected: "🐙 lexfrei/claudeline ✅ #42 🌿 feat-api",
		},
		{
			name:     "changes_requested",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"a","name":"b"}},"pr":{"number":1,"review_state":"changes_requested"}}`,
			expected: "🐙 a/b 🔴 #1",
		},
		{
			name:     "commented",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"a","name":"b"}},"pr":{"number":1,"review_state":"commented"}}`,
			expected: "🐙 a/b 💬 #1",
		},
		{
			name:     "pending review",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"a","name":"b"}},"pr":{"number":1,"review_state":"pending"}}`,
			expected: "🐙 a/b 👀 #1",
		},
		{
			name:     "PR without known review_state hides state icon",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"a","name":"b"}},"pr":{"number":1,"review_state":"unknown"}}`,
			expected: "🐙 a/b #1",
		},
		{
			name:     "gitlab host",
			input:    `{"workspace":{"repo":{"host":"gitlab.com","owner":"group","name":"proj"}}}`,
			expected: "🦊 group/proj",
		},
		{
			name:     "gitlab merge request uses ! instead of #",
			input:    `{"workspace":{"repo":{"host":"gitlab.com","owner":"group","name":"proj"}},"pr":{"number":7,"kind":"mr","review_state":"approved"}}`,
			expected: "🦊 group/proj ✅ !7",
		},
		{
			name:     "self-hosted gitlab merge request keeps the ! prefix",
			input:    `{"workspace":{"repo":{"host":"git.example.com","owner":"o","name":"r"}},"pr":{"number":7,"kind":"mr"}}`,
			expected: "📦 git.example.com/o/r !7",
		},
		{
			name:     "github pull request keeps # when kind is absent",
			input:    `{"workspace":{"repo":{"host":"github.com","owner":"a","name":"b"}},"pr":{"number":7,"review_state":"pending"}}`,
			expected: "🐙 a/b 👀 #7",
		},
		{
			name:     "bitbucket host",
			input:    `{"workspace":{"repo":{"host":"bitbucket.org","owner":"team","name":"app"}}}`,
			expected: "🪣 team/app",
		},
		{
			name:     "unknown host surfaces host prefix",
			input:    `{"workspace":{"repo":{"host":"git.example.com","owner":"o","name":"r"}}}`,
			expected: "📦 git.example.com/o/r",
		},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			got := buildStatusline([]byte(tcase.input), defaultCfg())
			if !strings.Contains(got, tcase.expected) {
				t.Errorf("expected %q in %q", tcase.expected, got)
			}
		})
	}
}

func TestBuildStatuslineRepoFallbackToWorktree(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	// No repo info — should render bare worktree segment.
	input := `{"workspace":{"git_worktree":"feat-api"}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🌿 feat-api") {
		t.Errorf("expected bare worktree fallback, got %q", got)
	}

	if strings.Contains(got, "🐙") {
		t.Errorf("expected no repo icon when repo absent, got %q", got)
	}
}

func TestBuildStatuslineRepoDisabledFallsBackToWorktree(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Repo = false

	input := `{"workspace":{"repo":{"host":"github.com","owner":"o","name":"r"},"git_worktree":"feat-api"}}`

	got := buildStatusline([]byte(input), cfg)
	if !strings.Contains(got, "🌿 feat-api") {
		t.Errorf("expected bare worktree fallback when repo disabled, got %q", got)
	}

	if strings.Contains(got, "🐙") {
		t.Errorf("expected no repo segment when disabled, got %q", got)
	}
}

func TestBuildStatuslineRepoSegmentUsesBranchFromCwd(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/from-head\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		dir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌿 feat/from-head") {
		t.Errorf("expected branch from .git/HEAD, got %q", got)
	}
}

func TestBuildStatuslineRepoSegmentBranchPrefersOverWorktreeName(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/from-head\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Both `git_worktree` and a readable HEAD are present — branch wins.
	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"git_worktree":"side","repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		dir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌿 feat/from-head") {
		t.Errorf("expected branch to win over worktree name, got %q", got)
	}
}

func TestBuildStatuslineRepoSegmentShowsLinkedWorktreeAndBranch(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	root := t.TempDir()
	worktreeGitdir := filepath.Join(root, "main", ".git", "worktrees", "side")
	worktreeDir := filepath.Join(root, "side")

	if err := os.MkdirAll(worktreeGitdir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(worktreeGitdir, "HEAD"), []byte("ref: refs/heads/feat/side\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: "+worktreeGitdir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Inside a linked worktree both markers appear: 🌳 worktree dir name, then
	// 🌿 branch from its HEAD.
	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		worktreeDir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌳 side 🌿 feat/side") {
		t.Errorf("expected linked worktree and branch markers, got %q", got)
	}
}

func TestBuildStatuslineLinkedWorktreeBranchEqualsDirShowsNoDuplicate(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	root := t.TempDir()
	worktreeGitdir := filepath.Join(root, "main", ".git", "worktrees", "side")
	worktreeDir := filepath.Join(root, "side")

	if err := os.MkdirAll(worktreeGitdir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// The common `git worktree add ../side side` pattern: the worktree dir name
	// and the branch are both "side". The branch marker must be dropped so the
	// name is not printed twice (🌳 already shows it).
	if err := os.WriteFile(filepath.Join(worktreeGitdir, "HEAD"), []byte("ref: refs/heads/side\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: "+worktreeGitdir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		worktreeDir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌳 side") {
		t.Errorf("expected worktree marker, got %q", got)
	}

	if strings.Contains(got, "🌿") {
		t.Errorf("expected no branch marker when branch equals worktree dir name, got %q", got)
	}

	if n := strings.Count(got, "side"); n != 1 {
		t.Errorf("expected the worktree name to appear exactly once, got %d in %q", n, got)
	}
}

func TestBuildStatuslineLinkedWorktreeDetachedHeadShowsNoDuplicate(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	root := t.TempDir()
	worktreeGitdir := filepath.Join(root, "main", ".git", "worktrees", "side")
	worktreeDir := filepath.Join(root, "side")

	if err := os.MkdirAll(worktreeGitdir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(worktreeDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Detached HEAD inside a linked worktree: HEAD is a SHA, not a ref.
	if err := os.WriteFile(filepath.Join(worktreeGitdir, "HEAD"), []byte("3a7c2f1e0d8b9f4a5c6e7d8b9f4a5c6e7d8b9f4a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte("gitdir: "+worktreeGitdir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Claude Code's git_worktree equals the worktree dir name. The branch
	// marker must NOT fall back to it here — that would duplicate "side" under
	// both 🌳 and 🌿. Only the 🌳 marker is shown; there is no branch.
	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"git_worktree":"side","repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		worktreeDir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌳 side") {
		t.Errorf("expected worktree marker, got %q", got)
	}

	if strings.Contains(got, "🌿") {
		t.Errorf("expected no branch marker on detached HEAD in a linked worktree, got %q", got)
	}
}

func TestBuildStatuslineRepoSegmentFallsBackToWorktreeNameWhenDetached(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Detached HEAD — gitinfo returns empty, so we fall back to git_worktree.
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("3a7c2f1e0d8b9f4a5c6e7d8b9f4a5c6e7d8b9f4a\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	input := fmt.Sprintf(
		`{"workspace":{"current_dir":%q,"git_worktree":"side","repo":{"host":"github.com","owner":"o","name":"r"}}}`,
		dir,
	)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🐙 o/r 🌿 side") {
		t.Errorf("expected fallback to worktree name on detached HEAD, got %q", got)
	}
}

func TestBuildStatuslineBareWorktreeSegmentShowsBranch(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")

	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/feat/bare\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// No workspace.repo — falls back to bare worktree segment, but now shows
	// the branch read from cwd's .git/HEAD instead of being empty.
	input := fmt.Sprintf(`{"workspace":{"current_dir":%q}}`, dir)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "🌿 feat/bare") {
		t.Errorf("expected bare worktree segment with branch from HEAD, got %q", got)
	}
}

func TestBuildStatuslineBothRepoAndWorktreeDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Repo = false
	cfg.Segments.Worktree = false

	input := `{"workspace":{"repo":{"host":"github.com","owner":"o","name":"r"},"git_worktree":"feat-api"}}`

	got := buildStatusline([]byte(input), cfg)
	if strings.Contains(got, "🐙") || strings.Contains(got, "🌿") {
		t.Errorf("expected no repo or worktree segment, got %q", got)
	}
}

func TestBuildStatuslineWithModel(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.6"},"cost":{"total_cost_usd":42.50}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🤖 Opus 4.6") {
		t.Errorf("expected Opus 4.6, got %q", got)
	}

	if !strings.Contains(got, "💰 $42.50") {
		t.Errorf("expected $42.50, got %q", got)
	}
}

func TestBuildStatuslineEffortLevels(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cases := []struct {
		level    string
		expected string
	}{
		{"low", "🤖 Opus 4.7 ⬇️"},
		{"medium", testModelOpus47},
		{"high", "🤖 Opus 4.7 ⬆️"},
		{"xhigh", "🤖 Opus 4.7 ⏫"},
		{"max", "🤖 Opus 4.7 🚀"},
		{"", testModelOpus47},
		{"unknown", testModelOpus47},
	}

	for _, tcase := range cases {
		t.Run(tcase.level, func(t *testing.T) {
			input := fmt.Sprintf(`{"model":{"display_name":"Opus 4.7"},"effort":{"level":%q}}`, tcase.level)

			got := buildStatusline([]byte(input), defaultCfg())
			if !strings.Contains(got, tcase.expected) {
				t.Errorf("level=%q: expected %q, got %q", tcase.level, tcase.expected, got)
			}

			// Medium and unknown levels must not emit any effort indicator.
			if tcase.level == "medium" || tcase.level == "" || tcase.level == "unknown" {
				for _, ind := range []string{"⬇️", "⬆️", "⏫", "🚀"} {
					if strings.Contains(got, ind) {
						t.Errorf("level=%q: unexpected indicator %q, got %q", tcase.level, ind, got)
					}
				}
			}
		})
	}
}

func TestBuildStatuslineThinkingIndicator(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.7"},"thinking":{"enabled":true}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🤖 Opus 4.7 💭") {
		t.Errorf("expected thinking indicator, got %q", got)
	}
}

func TestBuildStatuslineFastModeIndicator(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.6"},"fast_mode":true}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🤖 Opus 4.6 ⚡") {
		t.Errorf("expected fast-mode indicator, got %q", got)
	}
}

func TestBuildStatuslineCombinedIndicators(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	input := `{"model":{"display_name":"Opus 4.7"},"effort":{"level":"high"},"thinking":{"enabled":true},"fast_mode":true}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🤖 Opus 4.7 ⬆️💭⚡") {
		t.Errorf("expected combined indicators, got %q", got)
	}
}

func TestBuildStatuslineIndicatorsDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Effort = false
	cfg.Segments.Thinking = false
	cfg.Segments.FastMode = false

	input := `{"model":{"display_name":"Opus 4.7"},"effort":{"level":"max"},"thinking":{"enabled":true},"fast_mode":true}`

	got := buildStatusline([]byte(input), cfg)
	if strings.Contains(got, "🚀") || strings.Contains(got, "💭") || strings.Contains(got, "⚡") {
		t.Errorf("expected no indicators when disabled, got %q", got)
	}

	if !strings.Contains(got, testModelOpus47) {
		t.Errorf("expected bare model name, got %q", got)
	}
}

func TestWrapWidth(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want int
	}{
		{name: "unset", set: false, want: 0},
		{name: "empty string", env: "", set: true, want: 0},
		{name: "non-numeric", env: "abc", set: true, want: 0},
		{name: "zero", env: "0", set: true, want: 0},
		{name: "equal to safety margin", env: "2", set: true, want: 0},
		{name: "negative", env: "-5", set: true, want: 0},
		{name: "just above margin", env: "3", set: true, want: 1},
		{name: "typical 80", env: "80", set: true, want: 78},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			if tcase.set {
				t.Setenv("COLUMNS", tcase.env)
			} else {
				// t.Setenv restores on cleanup; unset within the scope.
				t.Setenv("COLUMNS", "sentinel")
				os.Unsetenv("COLUMNS")
			}

			if got := wrapWidth(); got != tcase.want {
				t.Errorf("wrapWidth() = %d, want %d", got, tcase.want)
			}
		})
	}
}

func TestBuildStatuslineWrapsOnNarrowColumns(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	t.Setenv("COLUMNS", "30")

	resetsAt := float64(time.Now().Add(3 * time.Hour).Unix())
	input := fmt.Sprintf(`{
		"model":{"display_name":"Opus 4.7"},
		"effort":{"level":"xhigh"},
		"thinking":{"enabled":true},
		"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"}},
		"context_window":{"used_percentage":67},
		"rate_limits":{
			"five_hour":{"used_percentage":50,"resets_at":%f},
			"seven_day":{"used_percentage":80,"resets_at":%f}
		}
	}`, resetsAt, resetsAt)

	got := buildStatusline([]byte(input), defaultCfg())
	if !strings.Contains(got, "\n") {
		t.Errorf("expected wrapped output on narrow COLUMNS, got single line: %q", got)
	}
}

func TestBuildStatuslineStaysSingleLineWithoutColumns(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	t.Setenv("COLUMNS", "sentinel")
	os.Unsetenv("COLUMNS")

	resetsAt := float64(time.Now().Add(3 * time.Hour).Unix())
	input := fmt.Sprintf(`{
		"model":{"display_name":"Opus 4.7"},
		"rate_limits":{
			"five_hour":{"used_percentage":50,"resets_at":%f},
			"seven_day":{"used_percentage":80,"resets_at":%f}
		}
	}`, resetsAt, resetsAt)

	got := buildStatusline([]byte(input), defaultCfg())
	if strings.Contains(got, "\n") {
		t.Errorf("expected single line when COLUMNS unset, got: %q", got)
	}
}

func TestBuildStatuslineWithContext(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	input := `{"context_window":{"used_percentage":75}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🧠 75%") {
		t.Errorf("expected context percentage, got %q", got)
	}
}

func TestBuildStatuslineWithCompactions(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := "{\"subtype\":\"compact_boundary\"}\n{\"subtype\":\"compact_boundary\"}\n"

	err := os.WriteFile(transcript, []byte(lines), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	input := `{"transcript_path":"` + transcript + `"}`
	got := buildStatusline([]byte(input), defaultCfg())

	if !strings.Contains(got, "🔄 2") {
		t.Errorf("expected compaction count, got %q", got)
	}
}

func TestBuildStatuslineWithStatusAlert(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"status":{"indicator":"major"}}`),
		}, nil
	}
	usage.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(`{}`), defaultCfg())

	if !strings.Contains(got, "🔶 major outage") {
		t.Errorf("expected major outage alert, got %q", got)
	}
}

func TestBuildStatuslineInvalidJSON(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	// Capture stderr to verify error logging.
	origStderr := os.Stderr

	stderrR, stderrW, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}

	os.Stderr = stderrW

	got := buildStatusline([]byte(`not json`), defaultCfg())

	stderrW.Close()

	os.Stderr = origStderr

	stderrOut, readErr := io.ReadAll(stderrR)
	if readErr != nil {
		t.Fatal(readErr)
	}

	// Should still degrade gracefully with default values.
	if !strings.Contains(got, "🤖 Claude") {
		t.Errorf("expected graceful degradation, got %q", got)
	}

	// Should log parse error to stderr.
	if !strings.Contains(string(stderrOut), "stdin parse error") {
		t.Errorf("expected stderr parse error log, got %q", stderrOut)
	}
}

func TestBuildStatuslineEmptyInput(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	// Empty input should NOT log an error (empty stdin is a valid edge case).
	origStderr := os.Stderr

	stderrR, stderrW, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}

	os.Stderr = stderrW

	_ = buildStatusline([]byte{}, defaultCfg())

	stderrW.Close()

	os.Stderr = origStderr

	stderrOut, readErr := io.ReadAll(stderrR)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if strings.Contains(string(stderrOut), "stdin parse error") {
		t.Errorf("empty input should not log parse error, got %q", stderrOut)
	}
}

func TestAppendUsageSegmentsLoginNeeded(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`{"error":{"type":"authentication_error"}}`),
		}, nil
	}

	segments := appendUsageSegments(nil, &stdinData{}, insecureCfg())
	joined := strings.Join(segments, " | ")

	if !strings.Contains(joined, "⚠️ /login needed") {
		t.Errorf("expected login needed, got %q", joined)
	}
}

func TestAppendUsageSegmentsRateLimited(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)

	err := os.WriteFile(usage.LastGoodCachePath, []byte(`{
		"five_hour": {"utilization": 42, "resets_at": "`+resetsAt+`"},
		"seven_day": {"utilization": 99, "resets_at": "`+resetsAt+`"}
	}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`{"error":{"type":"rate_limit_error"}}`),
			Header:     http.Header{"Retry-After": []string{"6"}},
		}, nil
	}

	segments := appendUsageSegments(nil, &stdinData{}, insecureCfg())
	joined := strings.Join(segments, " | ")

	if strings.Contains(joined, "/login needed") {
		t.Errorf("rate_limit_error should not show login needed, got %q", joined)
	}

	if !strings.Contains(joined, "⛔ 7d limit hit") {
		t.Errorf("expected explicit rate-limit segment with window name, got %q", joined)
	}

	if !strings.Contains(joined, "7d: ?%") {
		t.Errorf("expected stale quota segments, got %q", joined)
	}
}

func TestAppendUsageSegmentsSuccess(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: []byte(`{
			"five_hour": {"utilization": 30, "resets_at": "` + resetsAt + `"},
			"seven_day": {"utilization": 45, "resets_at": "` + resetsAt + `"},
			"extra_usage": {"is_enabled": true, "monthly_limit": 5000, "used_credits": 128}
		}`),
		}, nil
	}

	segments := appendUsageSegments(nil, &stdinData{}, insecureCfg())
	joined := strings.Join(segments, " | ")

	if !strings.Contains(joined, "7d: 45%") {
		t.Errorf("expected 7d quota, got %q", joined)
	}

	if !strings.Contains(joined, "5h: 30%") {
		t.Errorf("expected 5h quota, got %q", joined)
	}

	if !strings.Contains(joined, "💳 $128/$5000") {
		t.Errorf("expected extra usage, got %q", joined)
	}
}

func TestAppendUsageSegmentsPerModel(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	resetsAt := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	usage.HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body: []byte(`{
				"seven_day": {"utilization": 45, "resets_at": "` + resetsAt + `"},
				"seven_day_opus": {"utilization": 12, "resets_at": "` + resetsAt + `"},
				"seven_day_sonnet": {"utilization": 78, "resets_at": "` + resetsAt + `"},
				"seven_day_cowork": null,
				"seven_day_oauth_apps": {"utilization": 5, "resets_at": "` + resetsAt + `"}
			}`),
		}, nil
	}

	cfg := insecureCfg()
	cfg.Segments.PerModelQuota = config.PerModelAll

	segments := appendUsageSegments(nil, &stdinData{}, cfg)
	joined := strings.Join(segments, " | ")

	if !strings.Contains(joined, "7d: 45%") {
		t.Errorf("expected 7d quota, got %q", joined)
	}

	if !strings.Contains(joined, "7d-opus: 12%") {
		t.Errorf("expected 7d-opus quota, got %q", joined)
	}

	if !strings.Contains(joined, "7d-sonnet: 78%") {
		t.Errorf("expected 7d-sonnet quota, got %q", joined)
	}

	if !strings.Contains(joined, "7d-oauth: 5%") {
		t.Errorf("expected 7d-oauth quota, got %q", joined)
	}

	if strings.Contains(joined, "7d-cowork") {
		t.Errorf("7d-cowork should not appear when null, got %q", joined)
	}

	// Verify per-model windows are hidden by default.
	segmentsDefault := appendUsageSegments(nil, &stdinData{}, insecureCfg())
	joinedDefault := strings.Join(segmentsDefault, " | ")

	if strings.Contains(joinedDefault, "7d-opus") {
		t.Errorf("per-model windows should be hidden by default, got %q", joinedDefault)
	}
}

func TestBuildStatuslineNoModel(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Model = false

	got := buildStatusline([]byte(`{}`), cfg)

	if strings.Contains(got, "🤖") {
		t.Errorf("expected no model segment, got %q", got)
	}
}

func TestBuildStatuslineNoCost(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Cost = config.CostOff

	got := buildStatusline([]byte(`{}`), cfg)

	if strings.Contains(got, "💰") {
		t.Errorf("expected no cost segment, got %q", got)
	}
}

func TestBuildStatuslineNoQuota(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Quota = false
	cfg.Segments.Credits = false

	got := buildStatusline([]byte(`{}`), cfg)

	if strings.Contains(got, "⏳") || strings.Contains(got, "7d") {
		t.Errorf("expected no quota segments, got %q", got)
	}
}

func TestBuildStatuslineAllDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.Model = false
	cfg.Segments.Worktree = false
	cfg.Segments.Cost = config.CostOff
	cfg.Segments.Status = false
	cfg.Segments.Context = false
	cfg.Segments.Compactions = false
	cfg.Segments.Quota = false
	cfg.Segments.Credits = false

	// Use real data for each segment so the disables must actually fire —
	// an absent field would hide the segment regardless of the config toggle.
	input := `{"model":{"display_name":"Opus 4.6"},"workspace":{"git_worktree":"feat-api"},"cost":{"total_cost_usd":1.5},"context_window":{"used_percentage":50}}`

	got := buildStatusline([]byte(input), cfg)

	if got != "" {
		t.Errorf("expected empty output, got %q", got)
	}
}

func TestNewRootCmdVersion(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--version"})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewRootCmdWithFlags(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	cmd := newRootCmd()
	cmd.SetArgs([]string{flagNoModel, flagNoWorktree, "--cost", "false", flagConfig, "/nonexistent/config.toml"})
	cmd.SetIn(strings.NewReader(`{"workspace":{"git_worktree":"feat-api"}}`))

	captured := captureStdout(t, func() {
		executeErr := cmd.Execute()
		if executeErr != nil {
			t.Errorf("unexpected error: %v", executeErr)
		}
	})

	if strings.Contains(captured, "🌿") {
		t.Errorf("--no-worktree should suppress worktree segment, got %q", captured)
	}

	if strings.Contains(captured, "🤖") {
		t.Errorf("--no-model should suppress model segment, got %q", captured)
	}
}

// captureStdout redirects os.Stdout for the duration of body and returns captured output.
func captureStdout(t *testing.T, body func()) string {
	t.Helper()

	orig := os.Stdout

	reader, writer, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}

	os.Stdout = writer

	done := make(chan string, 1)

	go func() {
		buf, readErr := io.ReadAll(reader)
		if readErr != nil {
			t.Errorf("readall: %v", readErr)
		}

		done <- string(buf)
	}()

	body()

	writer.Close()

	os.Stdout = orig

	return <-done
}

func TestNewRootCmdWithConfigFile(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	keychain.GetFn = func() (string, error) { return "", keychain.ErrNoToken }
	status.HTTPGetFn = failHTTP
	usage.HTTPGetFn = failHTTP

	configContent := `
mac_insecure = true

[segments]
model = false

[cache]
usage_ttl = "30s"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")

	writeErr := os.WriteFile(configPath, []byte(configContent), 0o600)
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	cmd := newRootCmd()
	cmd.SetArgs([]string{flagConfig, configPath})
	cmd.SetIn(strings.NewReader(`{}`))

	err := cmd.Execute()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if usage.CacheTTL != 30*time.Second {
		t.Errorf("expected usage TTL 30s from config, got %v", usage.CacheTTL)
	}
}

func TestApplyFlagOverrides(t *testing.T) {
	cmd := newRootCmd()
	args := []string{
		flagNoModel, flagNoWorktree, "--no-quota", "--no-credits", flagPerModelQuota,
		"--mac-insecure", "--no-prompt-cache",
	}
	cmd.SetArgs(args)

	parseErr := cmd.ParseFlags(args)
	if parseErr != nil {
		t.Fatal(parseErr)
	}

	cfg := config.Defaults()
	applyFlagOverrides(cmd, &cfg)

	if cfg.Segments.Model {
		t.Error("expected model disabled by flag")
	}

	if cfg.Segments.Worktree {
		t.Error("expected worktree disabled by flag")
	}

	if cfg.Segments.Quota {
		t.Error("expected quota disabled by flag")
	}

	if cfg.Segments.Credits {
		t.Error("expected credits disabled by flag")
	}

	if cfg.Segments.PromptCache {
		t.Error("expected prompt cache disabled by flag")
	}

	if cfg.Segments.PerModelQuota != config.PerModelAll {
		t.Error("expected per-model quota set to all by flag")
	}

	if cfg.Segments.Cost == config.CostOff {
		t.Error("expected cost still enabled")
	}

	if !cfg.MacInsecure {
		t.Error("expected mac-insecure enabled by flag")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()

	got := defaultConfigPath()
	if got == "" {
		t.Skip("could not determine home directory")
	}

	if !strings.HasSuffix(got, ".claudelinerc.toml") {
		t.Errorf("expected path ending with .claudelinerc.toml, got %q", got)
	}
}

func TestFlagSetUnknownFlag(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()

	if flagSet(cmd, "nonexistent-flag") {
		t.Error("expected false for unknown flag")
	}
}

// TestNoOffpeakFlagStillAccepted pins backward compatibility: the off-peak
// feature was removed, but a statusLine.command that still passes --no-offpeak
// must keep parsing. A parse error here would make main() exit 1 and blank the
// whole statusline, not just drop one segment.
func TestNoOffpeakFlagStillAccepted(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	if err := cmd.ParseFlags([]string{"--no-offpeak"}); err != nil {
		t.Fatalf("removed flag must remain a no-op for backward compat, got %v", err)
	}
}

func TestThemeFlagOverride(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd()
	if err := cmd.ParseFlags([]string{"--theme", "text"}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	applyFlagOverrides(cmd, &cfg)

	if cfg.Theme != config.ThemeText {
		t.Errorf("expected --theme text to override config, got %q", cfg.Theme)
	}
}

// TestApplyRuntimeConfigTheme pins the cfg.Theme -> fmtutil.Style glue, the one
// integration point every rendering test bypasses by setting Style directly. An
// inverted or miswired branch here would render the wrong theme in the real
// binary while leaving all those tests green.
func TestApplyRuntimeConfigTheme(t *testing.T) {
	cases := map[string]fmtutil.IconStyle{
		config.ThemeText:  fmtutil.StyleText,
		config.ThemeEmoji: fmtutil.StyleEmoji,
		"":                fmtutil.StyleEmoji, // unset normalizes to emoji
		"bogus":           fmtutil.StyleEmoji, // invalid flag value falls back
	}

	prev := fmtutil.Style

	t.Cleanup(func() { fmtutil.Style = prev })

	for theme, want := range cases {
		cfg := config.Defaults()
		cfg.Theme = theme

		applyRuntimeConfig(&cfg)

		if fmtutil.Style != want {
			t.Errorf("theme %q -> Style %d, want %d", theme, fmtutil.Style, want)
		}
	}
}

// useTextTheme switches the global icon style to text for one test, restoring
// it after. Not parallel-safe: Style is shared process state.
func useTextTheme(t *testing.T) {
	t.Helper()

	prev := fmtutil.Style
	fmtutil.Style = fmtutil.StyleText

	t.Cleanup(func() { fmtutil.Style = prev })
}

func TestBuildStatuslineTextThemeDropsEmoji(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	useTextTheme(t)

	input := `{"model":{"display_name":"Opus 4.7"},"effort":{"level":"xhigh"},"thinking":{"enabled":true},` +
		`"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"}},` +
		`"pr":{"number":19,"review_state":"changes_requested"}}`
	got := buildStatusline([]byte(input), defaultCfg())

	for _, emoji := range []string{"🤖", "⏫", "💭", "🐙", "📝", "🔴"} {
		if strings.Contains(got, emoji) {
			t.Errorf("text theme must drop emoji %q, got %q", emoji, got)
		}
	}

	for _, want := range []string{"Opus 4.7", "lexfrei/claudeline"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in %q", want, got)
		}
	}

	// changes_requested carries a 🔴 circle, so its #19 is colored red.
	if !strings.Contains(got, "\x1b[31m#19\x1b[0m") {
		t.Errorf("expected red-wrapped #19 for changes_requested, got %q", got)
	}
}

func TestBuildStatuslineTextThemePlainNumber(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	useTextTheme(t)

	// draft carries 📝 (not a circle), so #19 stays plain text, no color.
	input := `{"workspace":{"repo":{"host":"github.com","owner":"lexfrei","name":"claudeline"}},` +
		`"pr":{"number":19,"review_state":"draft"}}`
	got := buildStatusline([]byte(input), defaultCfg())

	if strings.Contains(got, "📝") || strings.Contains(got, "\x1b[") {
		t.Errorf("expected plain uncolored #19 for draft, got %q", got)
	}

	if !strings.Contains(got, "#19") {
		t.Errorf("expected #19 in %q", got)
	}
}

// promptCacheSegmentOf returns the statusline's prompt cache segment on its own,
// empty when there is none. Matching the whole segment is what lets these cases
// separate "🧊 tools" from "🧊 tools+" and from a bare "🧊" — a substring check
// passes on all three.
func promptCacheSegmentOf(t *testing.T, line string) string {
	t.Helper()

	for segment := range strings.SplitSeq(line, " | ") {
		if strings.HasPrefix(segment, "🧊") {
			return segment
		}
	}

	return ""
}

// promptCacheInput builds a stdin payload carrying only the prompt_cache
// object, which is all the segment reads.
func promptCacheInput(t *testing.T, cache string) []byte {
	t.Helper()

	return []byte(`{"model":{"display_name":"Opus"},"prompt_cache":` + cache + `}`)
}

// A warm cache is the normal state and says nothing worth a segment, so the
// statusline stays as it was before prompt_cache existed.
func TestPromptCacheSegmentSilentWhenWarm(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	got := buildStatusline(promptCacheInput(t, `{"warm":true,"caching_observed":true,"hit_ratio":0.9}`), defaultCfg())

	if strings.Contains(got, "🧊") {
		t.Errorf("expected no prompt cache segment while warm, got %q", got)
	}
}

// A cold cache means the next request re-processes the whole prefix, and the
// cause names what to stop doing mid-session.
func TestPromptCacheSegmentShowsCause(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	tests := []struct {
		name  string
		cause string
		want  string
	}{
		{"tool set changed", `{"causes":["tools_changed"],"tools_added":2}`, "🧊 tools"},
		{"idle past the 5m TTL", `{"causes":["ttl_expired_5m"]}`, "🧊 ttl"},
		{"idle past the 1h TTL", `{"causes":["ttl_expired_1h"]}`, "🧊 ttl"},
		{"effort switched", `{"causes":["effort_changed"]}`, "🧊 effort"},
		{"usage limits", `{"causes":["overage_changed"]}`, "🧊 limits"},
		{"nothing local changed", `{"causes":["likely_server_side"]}`, "🧊 server"},
		{"more than one cause", `{"causes":["tools_changed","system_prompt_changed"]}`, "🧊 tools+"},
		// A cause the harness adds later still reads as something, instead of
		// collapsing into the bare icon.
		{"cause added by a later harness", `{"causes":["sampling_params_changed"]}`, "🧊 sampling_params"},
		{"cause not attributed", `{"causes":["unknown"]}`, "🧊"},
		// Today the harness reports "unknown" alone. Were that to change, the
		// segment must not degrade into a bare "+".
		{"unlabelled cause alongside another", `{"causes":["unknown","tools_changed"]}`, "🧊"},
		{"no cause reported", `null`, "🧊"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := promptCacheInput(t,
				`{"warm":false,"caching_observed":true,"last_miss_cause":`+tt.cause+`}`)

			line := buildStatusline(input, defaultCfg())

			if got := promptCacheSegmentOf(t, line); got != tt.want {
				t.Errorf("prompt cache segment = %q, want %q (line %q)", got, tt.want, line)
			}
		})
	}
}

// Without caching_observed the provider or gateway never reported cache tokens,
// so warm is false for the whole session and a cold marker would be permanent
// noise rather than a signal.
func TestPromptCacheSegmentSilentWithoutCaching(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	got := buildStatusline(promptCacheInput(t, `{"warm":false,"caching_observed":false}`), defaultCfg())

	if strings.Contains(got, "🧊") {
		t.Errorf("expected no prompt cache segment when caching is not observed, got %q", got)
	}
}

// prompt_cache requires Claude Code 2.1.251, and is absent before the session's
// first API response on every version.
func TestPromptCacheSegmentSilentWhenAbsent(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	got := buildStatusline([]byte(`{"model":{"display_name":"Opus"}}`), defaultCfg())

	if strings.Contains(got, "🧊") {
		t.Errorf("expected no prompt cache segment without prompt_cache in stdin, got %q", got)
	}
}

func TestPromptCacheSegmentDisabled(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	cfg := defaultCfg()
	cfg.Segments.PromptCache = false

	got := buildStatusline(promptCacheInput(t,
		`{"warm":false,"caching_observed":true,"last_miss_cause":{"causes":["tools_changed"]}}`), cfg)

	if strings.Contains(got, "🧊") {
		t.Errorf("expected no prompt cache segment when disabled, got %q", got)
	}
}

// The text theme drops every emoji, so the cause has to survive on its own.
func TestPromptCacheSegmentTextTheme(t *testing.T) {
	cleanup := setupTestEnv(t)
	defer cleanup()

	status.HTTPGetFn = failHTTP

	useTextTheme(t)

	got := buildStatusline(promptCacheInput(t,
		`{"warm":false,"caching_observed":true,"last_miss_cause":{"causes":["tools_changed"]}}`), defaultCfg())

	if !strings.Contains(got, "cache: tools") {
		t.Errorf("expected a text-theme cache marker, got %q", got)
	}
}
