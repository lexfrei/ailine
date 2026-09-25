package config_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/lexfrei/ailine/internal/config"
)

// badValue is a value no mode option accepts.
const badValue = "nonsense"

func TestNormalizePerModelQuota(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want string
	}{
		{"", config.PerModelAuto},
		{"auto", config.PerModelAuto},
		{"AUTO", config.PerModelAuto},
		{"true", config.PerModelAll},
		{"1", config.PerModelAll},
		{"on", config.PerModelAll},
		{"false", config.PerModelOff},
		{"0", config.PerModelOff},
		{"off", config.PerModelOff},
		// The option was a bool flag before it was a mode, so every spelling
		// strconv.ParseBool took must keep resolving the same way.
		{"True", config.PerModelAll},
		{"TRUE", config.PerModelAll},
		{"T", config.PerModelAll},
		{"False", config.PerModelOff},
		{"FALSE", config.PerModelOff},
		{"F", config.PerModelOff},
		{badValue, ""},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			if got := config.NormalizePerModelQuota(tt.raw); got != tt.want {
				t.Errorf("NormalizePerModelQuota(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDefaultsPerModelQuotaIsAuto(t *testing.T) {
	t.Parallel()

	if got := config.Defaults().Segments.PerModelQuota; got != config.PerModelAuto {
		t.Errorf("expected per-model quota auto by default, got %q", got)
	}
}

// A boolean per_model_quota keeps working: it predates the mode and existing
// configs must not break.
func TestLoadPerModelQuotaBooleanCompat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want string
	}{
		{"legacy true", "[segments]\nper_model_quota = true\n", config.PerModelAll},
		{"legacy false", "[segments]\nper_model_quota = false\n", config.PerModelOff},
		{"explicit auto", "[segments]\nper_model_quota = \"auto\"\n", config.PerModelAuto},
		{"absent", "[segments]\nquota = true\n", config.PerModelAuto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "ailinerc.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatalf("writing config: %v", err)
			}

			if got := config.Load(path).Segments.PerModelQuota; got != tt.want {
				t.Errorf("per_model_quota = %q, want %q", got, tt.want)
			}
		})
	}
}

// Cost accepted the same bool spellings before this change; keep them working.
func TestNormalizeCostModeBoolSpellings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want string
	}{
		{"True", config.CostOn},
		{"T", config.CostOn},
		{"False", config.CostOff},
		{"F", config.CostOff},
		{"AUTO", config.CostAuto},
		{badValue, ""},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			t.Parallel()

			if got := config.NormalizeCostMode(tt.raw); got != tt.want {
				t.Errorf("NormalizeCostMode(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// An invalid value in the config file must not blank the statusline: warn and
// fall back to auto, the same way theme and cost do.
func TestLoadInvalidPerModelQuotaFallsBackToAuto(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ailinerc.toml")
	if err := os.WriteFile(path, []byte("[segments]\nper_model_quota = \"sometimes\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	if got := config.Load(path).Segments.PerModelQuota; got != config.PerModelAuto {
		t.Errorf("per_model_quota = %q, want %q", got, config.PerModelAuto)
	}
}

// The key moved out of the boolean checks into the mode check, so validate must
// still accept the boolean form an existing config carries.
func TestValidateAcceptsLegacyBooleanPerModelQuota(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		"[segments]\nper_model_quota = true\n",
		"[segments]\nper_model_quota = false\n",
	} {
		path := filepath.Join(t.TempDir(), "ailinerc.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("writing config: %v", err)
		}

		if problems := config.Validate(path); len(problems) != 0 {
			t.Errorf("expected %q to validate cleanly, got %v", body, problems)
		}
	}
}

func TestValidateRejectsUnknownPerModelQuota(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ailinerc.toml")
	if err := os.WriteFile(path, []byte("[segments]\nper_model_quota = \"sometimes\"\n"), 0o600); err != nil {
		t.Fatalf("writing config: %v", err)
	}

	problems := config.Validate(path)

	want := `segments.per_model_quota: unknown value "sometimes" (expected auto, true, or false)`
	if !slices.Contains(problems, want) {
		t.Errorf("expected per_model_quota validation error, got %v", problems)
	}
}
