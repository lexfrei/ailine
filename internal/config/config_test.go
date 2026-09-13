package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	cfg := Defaults()

	if !cfg.Segments.Model {
		t.Error("expected model segment enabled by default")
	}

	if !cfg.Segments.Worktree {
		t.Error("expected worktree segment enabled by default")
	}

	if cfg.Segments.Cost != CostAuto {
		t.Errorf("expected cost segment auto by default, got %q", cfg.Segments.Cost)
	}

	if !cfg.Segments.Status {
		t.Error("expected status segment enabled by default")
	}

	if !cfg.Segments.Context {
		t.Error("expected context segment enabled by default")
	}

	if !cfg.Segments.PromptCache {
		t.Error("expected prompt cache segment enabled by default")
	}

	if !cfg.Segments.Compactions {
		t.Error("expected compactions segment enabled by default")
	}

	if !cfg.Segments.Quota {
		t.Error("expected quota segment enabled by default")
	}

	if !cfg.Segments.Credits {
		t.Error("expected credits segment enabled by default")
	}

	if cfg.Cache.UsageTTL != 10*time.Minute {
		t.Errorf("expected usage TTL 60s, got %v", cfg.Cache.UsageTTL)
	}

	if cfg.Cache.StatusTTL != 15*time.Second {
		t.Errorf("expected status TTL 15s, got %v", cfg.Cache.StatusTTL)
	}

	if cfg.Theme != ThemeEmoji {
		t.Errorf("expected emoji theme by default, got %q", cfg.Theme)
	}
}

func TestNormalizeTheme(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"emoji": ThemeEmoji,
		"":      ThemeEmoji,
		"text":  ThemeText,
		"bogus": "",
		"EMOJI": "",
	}

	for in, want := range cases {
		if got := NormalizeTheme(in); got != want {
			t.Errorf("NormalizeTheme(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLoadTheme(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`theme = "text"`:  ThemeText,
		`theme = "emoji"`: ThemeEmoji,
		`theme = "bogus"`: ThemeEmoji, // invalid falls back to emoji
		``:                ThemeEmoji, // absent key defaults to emoji
	}

	for content, want := range cases {
		configPath := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}

		if got := Load(configPath).Theme; got != want {
			t.Errorf("Load(%q).Theme = %q, want %q", content, got, want)
		}
	}
}

func TestValidateBadTheme(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`theme = "bogus"`), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)

	found := false

	for _, p := range problems {
		if p == `theme: unknown value "bogus" (expected emoji or text)` {
			found = true
		}
	}

	if !found {
		t.Errorf("expected theme validation error, got %v", problems)
	}
}

func TestValidateGoodTheme(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`theme = "text"`), 0o600); err != nil {
		t.Fatal(err)
	}

	if problems := Validate(configPath); len(problems) != 0 {
		t.Errorf("expected no problems for theme=text, got %v", problems)
	}
}

func TestLoadMissingFile(t *testing.T) {
	t.Parallel()

	cfg := Load("/nonexistent/config.toml")

	if !cfg.Segments.Model || cfg.Segments.Cost != CostAuto {
		t.Error("expected defaults when config file is missing")
	}
}

func TestLoadEmptyPath(t *testing.T) {
	t.Parallel()

	cfg := Load("")

	if !cfg.Segments.Quota {
		t.Error("expected defaults when config path is empty")
	}
}

func TestLoadPartialConfig(t *testing.T) {
	t.Parallel()

	content := `
[segments]
cost = false
status = false
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load(configPath)

	if !cfg.Segments.Model {
		t.Error("expected model segment enabled (not in config)")
	}

	if !cfg.Segments.Worktree {
		t.Error("expected worktree segment enabled (not in config)")
	}

	if cfg.Segments.Cost != CostOff {
		t.Errorf("expected cost segment off, got %q", cfg.Segments.Cost)
	}

	if cfg.Segments.Status {
		t.Error("expected status segment disabled")
	}

	if !cfg.Segments.Quota {
		t.Error("expected quota segment enabled (not in config)")
	}

	if cfg.Cache.UsageTTL != 10*time.Minute {
		t.Errorf("expected default usage TTL, got %v", cfg.Cache.UsageTTL)
	}
}

func TestLoadFullConfig(t *testing.T) {
	t.Parallel()

	content := `
[segments]
model = false
worktree = false
cost = false
status = false
context = false
prompt_cache = false
compactions = false
quota = false
credits = false

[cache]
usage_ttl = "120s"
status_ttl = "30s"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load(configPath)

	if cfg.Segments.Model || cfg.Segments.Worktree || cfg.Segments.Cost != CostOff || cfg.Segments.Status ||
		cfg.Segments.Context || cfg.Segments.PromptCache || cfg.Segments.Compactions || cfg.Segments.Quota ||
		cfg.Segments.Credits {
		t.Error("expected all segments disabled")
	}

	if cfg.Cache.UsageTTL != 120*time.Second {
		t.Errorf("expected usage TTL 120s, got %v", cfg.Cache.UsageTTL)
	}

	if cfg.Cache.StatusTTL != 30*time.Second {
		t.Errorf("expected status TTL 30s, got %v", cfg.Cache.StatusTTL)
	}
}

func TestLoadInvalidTOML(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("not valid toml [[["), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load(configPath)

	if !cfg.Segments.Model {
		t.Error("expected defaults on invalid TOML")
	}
}

func TestValidateGoodConfig(t *testing.T) {
	t.Parallel()

	content := `
[segments]
model = true
cost = "auto"
status = false

[cache]
usage_ttl = "5m"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)
	if len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}

func TestValidateToleratesDeprecatedOffpeak(t *testing.T) {
	t.Parallel()

	// The off-peak feature was removed, but existing configs may still carry
	// segments.offpeak. It must remain tolerated rather than flagged as a typo.
	content := `
[segments]
offpeak = true
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)
	if len(problems) != 0 {
		t.Errorf("expected deprecated offpeak key to be tolerated, got %v", problems)
	}
}

func TestValidateUnknownKey(t *testing.T) {
	t.Parallel()

	content := `
[segments]
mdl = true
cst = "auto"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)
	if len(problems) < 2 {
		t.Errorf("expected at least 2 problems for typos, got %v", problems)
	}
}

func TestValidateBadCostMode(t *testing.T) {
	t.Parallel()

	content := `
[segments]
cost = "audo"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)

	found := false

	for _, p := range problems {
		if p == `segments.cost: unknown value "audo" (expected auto, true, or false)` {
			found = true
		}
	}

	if !found {
		t.Errorf("expected cost validation error, got %v", problems)
	}
}

func TestValidateBadBoolValue(t *testing.T) {
	t.Parallel()

	content := `
[segments]
model = "yes"
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)
	if len(problems) == 0 {
		t.Error("expected validation error for model = \"yes\"")
	}
}

func TestValidateEmptyPath(t *testing.T) {
	t.Parallel()

	problems := Validate("")
	if problems != nil {
		t.Errorf("expected nil for empty path, got %v", problems)
	}
}

func TestValidateMissingFile(t *testing.T) {
	t.Parallel()

	problems := Validate("/nonexistent/config.toml")
	if len(problems) != 1 {
		t.Errorf("expected 1 problem for missing file, got %v", problems)
	}
}

func TestLoadUnmarshalError(t *testing.T) {
	t.Parallel()

	// Valid TOML syntax but wrong type: model expects bool, gets a table.
	content := `
[segments]
[segments.model]
nested = true
`

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := Load(configPath)

	// Should fall back to defaults when unmarshal fails.
	defaults := Defaults()
	if cfg.Segments.Model != defaults.Segments.Model {
		t.Errorf("expected default model=%v on unmarshal error, got %v", defaults.Segments.Model, cfg.Segments.Model)
	}

	if cfg.Cache.UsageTTL != defaults.Cache.UsageTTL {
		t.Errorf("expected default usage TTL on unmarshal error, got %v", cfg.Cache.UsageTTL)
	}
}

// A segment key rejects non-boolean values instead of silently reading as
// enabled, the same as every other boolean segment.
func TestValidatePromptCacheNotBoolean(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[segments]\nprompt_cache = \"yes\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	problems := Validate(configPath)

	if len(problems) != 1 || !strings.Contains(problems[0], "segments.prompt_cache") {
		t.Errorf("expected a prompt_cache type problem, got %v", problems)
	}
}

// The key must be known to `validate`, or a correct config reports a typo.
func TestValidatePromptCacheAccepted(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte("[segments]\nprompt_cache = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if problems := Validate(configPath); len(problems) != 0 {
		t.Errorf("expected no problems, got %v", problems)
	}
}
