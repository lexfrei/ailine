// Package config provides configuration loading from TOML files and CLI flags.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	defaultUsageTTL  = 10 * time.Minute
	defaultStatusTTL = 15 * time.Second
)

// Canonical values every mode option resolves to. The options below are named
// after them so a mode can be compared without knowing which option it came from.
const (
	modeAuto = "auto"
	modeOn   = "true"
	modeOff  = "false"
)

// Cost mode values.
const (
	CostAuto = modeAuto
	CostOn   = modeOn
	CostOff  = modeOff
)

// normalizeBoolish maps a boolean-ish value onto modeOn or modeOff. It accepts
// every spelling strconv.ParseBool accepts — the mode options were plain bool
// flags once, and a config or command line carrying "True" or "T" must keep
// working — plus the on/off pair. Returns "" when the value is not boolean-ish.
func normalizeBoolish(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case modeOn, "t", "1", "on":
		return modeOn
	case modeOff, "f", "0", "off":
		return modeOff
	default:
		return ""
	}
}

// NormalizeCostMode converts various user inputs to a canonical cost mode.
// Accepts "auto" and any boolean-ish value. Returns "" for unknown values.
func NormalizeCostMode(raw string) string {
	if isAuto(raw) {
		return CostAuto
	}

	return normalizeBoolish(raw)
}

// isAuto reports whether a mode value asks for the automatic behavior. An empty
// value means the option was not set, which is the automatic default.
func isAuto(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))

	return trimmed == modeAuto || trimmed == ""
}

// Per-model quota mode values.
const (
	// PerModelAuto shows only the per-model window matching the selected model.
	PerModelAuto = modeAuto
	// PerModelAll shows every per-model window the server reports.
	PerModelAll = modeOn
	// PerModelOff shows no per-model windows.
	PerModelOff = modeOff
)

// NormalizePerModelQuota converts user input to a canonical per-model quota mode.
// Booleans are accepted in every spelling the option's former bool flag took.
// Returns "" for unknown values.
func NormalizePerModelQuota(raw string) string {
	if isAuto(raw) {
		return PerModelAuto
	}

	return normalizeBoolish(raw)
}

// Theme values selecting the statusline icon style.
const (
	ThemeEmoji = "emoji"
	ThemeText  = "text"
)

// NormalizeTheme converts user input to a canonical theme. An empty value means
// the default ("emoji"). Returns "" for unknown values so callers can warn.
func NormalizeTheme(raw string) string {
	switch raw {
	case ThemeEmoji, "":
		return ThemeEmoji
	case ThemeText:
		return ThemeText
	default:
		return ""
	}
}

// Segments controls which statusline segments are displayed.
type Segments struct {
	Model         bool   `mapstructure:"model"`
	Effort        bool   `mapstructure:"effort"`
	Thinking      bool   `mapstructure:"thinking"`
	FastMode      bool   `mapstructure:"fast_mode"`
	Repo          bool   `mapstructure:"repo"`
	Worktree      bool   `mapstructure:"worktree"`
	Cost          string `mapstructure:"cost"`
	Status        bool   `mapstructure:"status"`
	Context       bool   `mapstructure:"context"`
	PromptCache   bool   `mapstructure:"prompt_cache"`
	Compactions   bool   `mapstructure:"compactions"`
	Quota         bool   `mapstructure:"quota"`
	PerModelQuota string `mapstructure:"per_model_quota"`
	Credits       bool   `mapstructure:"credits"`
}

// Cache controls cache TTL durations.
type Cache struct {
	UsageTTL  time.Duration `mapstructure:"usage_ttl"`
	StatusTTL time.Duration `mapstructure:"status_ttl"`
}

// Config holds all claudeline configuration.
type Config struct {
	Segments    Segments `mapstructure:"segments"`
	Cache       Cache    `mapstructure:"cache"`
	MacInsecure bool     `mapstructure:"mac_insecure"`
	// Theme selects the icon style: "emoji" (default) or "text" (no emoji,
	// status carried as text color).
	Theme string `mapstructure:"theme"`
}

// Defaults returns a Config with all segments enabled and default TTLs.
func Defaults() Config {
	return Config{
		Segments: Segments{
			Model:         true,
			Effort:        true,
			Thinking:      true,
			FastMode:      true,
			Repo:          true,
			Worktree:      true,
			Cost:          CostAuto,
			Status:        true,
			Context:       true,
			PromptCache:   true,
			Compactions:   true,
			Quota:         true,
			PerModelQuota: PerModelAuto,
			Credits:       true,
		},
		Cache: Cache{
			UsageTTL:  defaultUsageTTL,
			StatusTTL: defaultStatusTTL,
		},
		Theme: ThemeEmoji,
	}
}

// Load reads configuration from a TOML file at the given path.
// Missing file or parse errors result in defaults being used.
func Load(configPath string) Config {
	cfg := Defaults()

	if configPath == "" {
		return cfg
	}

	viperInstance := viper.New()
	viperInstance.SetConfigFile(configPath)
	viperInstance.SetConfigType("toml")

	setViperDefaults(viperInstance)

	readErr := viperInstance.ReadInConfig()
	if readErr != nil {
		return cfg
	}

	unmarshalErr := viperInstance.Unmarshal(&cfg)
	if unmarshalErr != nil {
		fmt.Fprintf(os.Stderr, "claudeline: config parse error: %v\n", unmarshalErr)

		return Defaults()
	}

	cfg.Segments.Cost = NormalizeCostMode(cfg.Segments.Cost)
	if cfg.Segments.Cost == "" {
		fmt.Fprintf(os.Stderr, "claudeline: invalid cost mode %q, using auto\n", viperInstance.GetString("segments.cost"))

		cfg.Segments.Cost = CostAuto
	}

	cfg.Segments.PerModelQuota = NormalizePerModelQuota(cfg.Segments.PerModelQuota)
	if cfg.Segments.PerModelQuota == "" {
		fmt.Fprintf(os.Stderr, "claudeline: invalid per-model quota mode %q, using auto\n",
			viperInstance.GetString("segments.per_model_quota"))

		cfg.Segments.PerModelQuota = PerModelAuto
	}

	cfg.Theme = NormalizeTheme(cfg.Theme)
	if cfg.Theme == "" {
		fmt.Fprintf(os.Stderr, "claudeline: invalid theme %q, using emoji\n", viperInstance.GetString("theme"))

		cfg.Theme = ThemeEmoji
	}

	return cfg
}

// knownKeys is the set of all valid configuration keys.
var knownKeys = map[string]bool{
	"segments.model":           true,
	"segments.effort":          true,
	"segments.thinking":        true,
	"segments.fast_mode":       true,
	"segments.repo":            true,
	"segments.worktree":        true,
	"segments.cost":            true,
	"segments.status":          true,
	"segments.context":         true,
	"segments.prompt_cache":    true,
	"segments.compactions":     true,
	"segments.quota":           true,
	"segments.per_model_quota": true,
	"segments.credits":         true,
	// Deprecated no-op: the off-peak promotion feature was removed, but the key
	// stays tolerated so existing configs do not trip `validate`.
	"segments.offpeak": true,
	"cache.usage_ttl":  true,
	"cache.status_ttl": true,
	"mac_insecure":     true,
	"theme":            true,
}

// Validate checks the config file at the given path for errors.
// Returns a list of human-readable problems. Empty list means valid.
func Validate(configPath string) []string {
	if configPath == "" {
		return nil
	}

	viperInstance := viper.New()
	viperInstance.SetConfigFile(configPath)
	viperInstance.SetConfigType("toml")

	setViperDefaults(viperInstance)

	readErr := viperInstance.ReadInConfig()
	if readErr != nil {
		return []string{fmt.Sprintf("cannot read config: %v", readErr)}
	}

	var problems []string

	// Check for unknown keys (typos).
	for _, key := range viperInstance.AllKeys() {
		if !knownKeys[key] {
			problems = append(problems, fmt.Sprintf("unknown key %q (typo?)", key))
		}
	}

	var cfg Config

	unmarshalErr := viperInstance.Unmarshal(&cfg)
	if unmarshalErr != nil {
		return append(problems, fmt.Sprintf("cannot parse config: %v", unmarshalErr))
	}

	problems = append(problems, validateSegments(&cfg.Segments, viperInstance)...)
	problems = append(problems, validateCache(&cfg.Cache)...)
	problems = append(problems, validateTheme(cfg.Theme)...)

	return problems
}

func validateTheme(raw string) []string {
	if NormalizeTheme(raw) == "" {
		return []string{fmt.Sprintf("theme: unknown value %q (expected emoji or text)", raw)}
	}

	return nil
}

func validateSegments(seg *Segments, v *viper.Viper) []string {
	var problems []string

	if raw := seg.Cost; NormalizeCostMode(raw) == "" {
		problems = append(problems, fmt.Sprintf("segments.cost: unknown value %q (expected auto, true, or false)", raw))
	}

	if raw := seg.PerModelQuota; NormalizePerModelQuota(raw) == "" {
		problems = append(problems,
			fmt.Sprintf("segments.per_model_quota: unknown value %q (expected auto, true, or false)", raw))
	}

	boolFields := []struct {
		key string
		raw any
	}{
		{"segments.model", v.Get("segments.model")},
		{"segments.effort", v.Get("segments.effort")},
		{"segments.thinking", v.Get("segments.thinking")},
		{"segments.fast_mode", v.Get("segments.fast_mode")},
		{"segments.repo", v.Get("segments.repo")},
		{"segments.worktree", v.Get("segments.worktree")},
		{"segments.status", v.Get("segments.status")},
		{"segments.context", v.Get("segments.context")},
		{"segments.prompt_cache", v.Get("segments.prompt_cache")},
		{"segments.compactions", v.Get("segments.compactions")},
		{"segments.quota", v.Get("segments.quota")},
		{"segments.credits", v.Get("segments.credits")},
	}

	for _, field := range boolFields {
		if field.raw == nil {
			continue
		}

		if _, ok := field.raw.(bool); !ok {
			problems = append(problems, fmt.Sprintf("%s: expected true or false, got %v", field.key, field.raw))
		}
	}

	if v.Get("mac_insecure") != nil {
		if _, ok := v.Get("mac_insecure").(bool); !ok {
			problems = append(problems, fmt.Sprintf("mac_insecure: expected true or false, got %v", v.Get("mac_insecure")))
		}
	}

	return problems
}

func validateCache(cache *Cache) []string {
	var problems []string

	if cache.UsageTTL < 0 {
		problems = append(problems, fmt.Sprintf("cache.usage_ttl: negative duration %v", cache.UsageTTL))
	}

	if cache.StatusTTL < 0 {
		problems = append(problems, fmt.Sprintf("cache.status_ttl: negative duration %v", cache.StatusTTL))
	}

	return problems
}

func setViperDefaults(viperInstance *viper.Viper) {
	viperInstance.SetDefault("segments.model", true)
	viperInstance.SetDefault("segments.effort", true)
	viperInstance.SetDefault("segments.thinking", true)
	viperInstance.SetDefault("segments.fast_mode", true)
	viperInstance.SetDefault("segments.repo", true)
	viperInstance.SetDefault("segments.worktree", true)
	viperInstance.SetDefault("segments.cost", CostAuto)
	viperInstance.SetDefault("segments.status", true)
	viperInstance.SetDefault("segments.context", true)
	viperInstance.SetDefault("segments.prompt_cache", true)
	viperInstance.SetDefault("segments.compactions", true)
	viperInstance.SetDefault("segments.quota", true)
	viperInstance.SetDefault("segments.per_model_quota", PerModelAuto)
	viperInstance.SetDefault("segments.credits", true)
	viperInstance.SetDefault("mac_insecure", false)
	viperInstance.SetDefault("theme", ThemeEmoji)
	viperInstance.SetDefault("cache.usage_ttl", defaultUsageTTL)
	viperInstance.SetDefault("cache.status_ttl", defaultStatusTTL)
}
