// Package main is the entry point for the claudeline CLI.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexfrei/claudeline/internal/compaction"
	"github.com/lexfrei/claudeline/internal/config"
	"github.com/lexfrei/claudeline/internal/fmtutil"
	"github.com/lexfrei/claudeline/internal/gitinfo"
	"github.com/lexfrei/claudeline/internal/status"
	"github.com/lexfrei/claudeline/internal/usage"
)

var (
	version = "dev"
	commit  = "none"
)

type stdinRateWindow struct {
	UsedPercentage float64 `json:"used_percentage"` //nolint:tagliatelle // External API format
	ResetsAt       float64 `json:"resets_at"`       //nolint:tagliatelle // External API format
}

type stdinRepoInfo struct {
	Host  string `json:"host"`
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type stdinPRInfo struct {
	Number      int    `json:"number"`
	URL         string `json:"url"`
	ReviewState string `json:"review_state"` //nolint:tagliatelle // External API format
	// Kind is "mr" for GitLab merge requests. GitHub pull requests omit it.
	// Requires Claude Code 2.1.234.
	Kind string `json:"kind"`
}

// stdinMissCause names why the last prompt-cache miss happened. The harness may
// report more than one cause for a single miss. Requires Claude Code 2.1.260.
type stdinMissCause struct {
	Causes []string `json:"causes"`
}

// stdinPromptCache carries the main conversation's prompt-cache state. Absent
// before Claude Code 2.1.251 and, on every version, until the session's first
// API response.
type stdinPromptCache struct {
	// Warm reports whether the cached prefix is still within its TTL. It is
	// false whenever the last response reported no cache tokens, which includes
	// the case where caching never happened at all — hence CachingObserved.
	Warm bool `json:"warm"`
	// CachingObserved reports whether any response this session carried cache
	// tokens. False means prompt caching is off, or the provider or gateway
	// does not report it.
	CachingObserved bool            `json:"caching_observed"` //nolint:tagliatelle // External API format
	LastMissCause   *stdinMissCause `json:"last_miss_cause"`  //nolint:tagliatelle // External API format
}

type stdinData struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"` //nolint:tagliatelle // External API format
	} `json:"model"`
	Effort struct {
		Level string `json:"level"`
	} `json:"effort"`
	Thinking struct {
		Enabled bool `json:"enabled"`
	} `json:"thinking"`
	FastMode  bool   `json:"fast_mode"` //nolint:tagliatelle // External API format
	Cwd       string `json:"cwd"`
	Workspace struct {
		CurrentDir  string         `json:"current_dir"`  //nolint:tagliatelle // External API format
		GitWorktree string         `json:"git_worktree"` //nolint:tagliatelle // External API format
		Repo        *stdinRepoInfo `json:"repo"`
	} `json:"workspace"`
	PR   *stdinPRInfo `json:"pr"`
	Cost struct {
		TotalCostUSD float64 `json:"total_cost_usd"` //nolint:tagliatelle // External API format
	} `json:"cost"`
	ContextWindow struct {
		UsedPercentage float64 `json:"used_percentage"` //nolint:tagliatelle // External API format
	} `json:"context_window"` //nolint:tagliatelle // External API format
	PromptCache    *stdinPromptCache `json:"prompt_cache"`    //nolint:tagliatelle // External API format
	TranscriptPath string            `json:"transcript_path"` //nolint:tagliatelle // External API format
	RateLimits     struct {
		FiveHour *stdinRateWindow `json:"five_hour"` //nolint:tagliatelle // External API format
		SevenDay *stdinRateWindow `json:"seven_day"` //nolint:tagliatelle // External API format
	} `json:"rate_limits"` //nolint:tagliatelle // External API format
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".claudelinerc.toml")
}

func newRootCmd() *cobra.Command {
	var configPath string

	cfg := config.Defaults()

	rootCmd := &cobra.Command{
		Use:     "claudeline",
		Short:   "Real-time statusline for Claude Code",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		Args:    cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			raw, err := io.ReadAll(os.Stdin)
			if err != nil {
				fmt.Println(fmtutil.Part("stdin error", "⚠️"))

				return
			}

			fmt.Print(buildStatusline(raw, &cfg))
		},
	}

	rootCmd.PersistentPreRun = func(_ *cobra.Command, _ []string) {
		cfg = config.Load(configPath)

		applyFlagOverrides(rootCmd, &cfg)
		applyRuntimeConfig(&cfg)
	}

	rootCmd.SetVersionTemplate("claudeline {{.Version}}\n")

	flags := rootCmd.PersistentFlags()
	flags.StringVar(&configPath, "config", defaultConfigPath(), "config file path")
	flags.Bool("no-model", false, "disable model segment")
	flags.Bool("no-effort", false, "disable effort indicator on model segment")
	flags.Bool("no-thinking", false, "disable thinking indicator on model segment")
	flags.Bool("no-fast-mode", false, "disable fast-mode indicator on model segment")
	flags.Bool("no-repo", false, "disable combined repo/PR segment (falls back to bare worktree)")
	flags.Bool("no-worktree", false, "disable worktree segment")
	flags.String("cost", "", "cost segment mode: auto (default), true, false")
	flags.Bool("no-status", false, "disable status segment")
	flags.Bool("no-context", false, "disable context segment")
	flags.Bool("no-prompt-cache", false, "disable prompt cache segment")
	flags.Bool("no-compactions", false, "disable compactions segment")
	flags.Bool("no-quota", false, "disable quota segment")
	flags.Bool("mac-insecure", false, "use macOS Keychain + Anthropic API for per-model quotas and credits")
	// The value must be attached with "=": a bare --per-model-quota keeps its
	// original meaning (show every window), which requires NoOptDefVal, and a
	// flag carrying NoOptDefVal never consumes the next argument — so the space
	// form would leave the value behind as a positional arg and exit non-zero,
	// blanking the statusline. Spell the "=" form everywhere it is documented.
	flags.String("per-model-quota", "",
		"per-model quota segments, requires --mac-insecure: --per-model-quota=auto (selected model, default), =true (all), =false (none)")
	flags.Lookup("per-model-quota").NoOptDefVal = config.PerModelAll

	flags.Bool("no-credits", false, "disable credits segment (only with --mac-insecure)")
	flags.String("theme", "", "icon theme: emoji (default) or text")
	flags.String("hyperlinks", "", "OSC 8 hyperlinks: auto (default), true, false")

	// Deprecated no-op: the off-peak promotion feature was removed. The flag is
	// kept (hidden) so existing statusLine.command invocations carrying
	// --no-offpeak keep parsing instead of failing and blanking the statusline.
	flags.Bool("no-offpeak", false, "deprecated no-op: off-peak indicators were removed")
	_ = flags.MarkHidden("no-offpeak")

	rootCmd.AddCommand(newValidateCmd(&configPath))

	return rootCmd
}

func newValidateCmd(configPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config file",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			problems := config.Validate(*configPath)
			if len(problems) == 0 {
				fmt.Println("config ok")

				return
			}

			for _, p := range problems {
				fmt.Fprintf(os.Stderr, "error: %s\n", p)
			}

			os.Exit(1)
		},
	}
}

func applyFlagOverrides(cmd *cobra.Command, cfg *config.Config) {
	applyIdentityFlags(cmd, cfg)
	applyDisplayFlags(cmd, cfg)
	applyUsageFlags(cmd, cfg)
}

// applyRuntimeConfig pushes resolved config values into the package globals
// that rendering and fetching read at request time. It finalizes the theme and
// the hyperlink mode: config.Load already warned for a bad config-file value,
// so an invalid one reaching here can only come from a flag (overrides run
// after Load) — warn once and fall back, matching the config-file behavior.
func applyRuntimeConfig(cfg *config.Config) {
	status.CacheTTL = cfg.Cache.StatusTTL

	theme := config.NormalizeTheme(cfg.Theme)
	if theme == "" {
		fmt.Fprintf(os.Stderr, "claudeline: invalid theme %q, using emoji\n", cfg.Theme)

		theme = config.ThemeEmoji
	}

	if theme == config.ThemeText {
		fmtutil.Style = fmtutil.StyleText
	} else {
		fmtutil.Style = fmtutil.StyleEmoji
	}

	hyperlinks := config.NormalizeHyperlinks(cfg.Hyperlinks)
	if hyperlinks == "" {
		fmt.Fprintf(os.Stderr, "claudeline: invalid hyperlinks mode %q, using auto\n", cfg.Hyperlinks)

		hyperlinks = config.HyperlinksAuto
	}

	fmtutil.Hyperlinks = hyperlinks == config.HyperlinksOn ||
		(hyperlinks == config.HyperlinksAuto && fmtutil.SupportsHyperlinks())

	if cfg.MacInsecure {
		usage.CacheTTL = cfg.Cache.UsageTTL
	}
}

func applyIdentityFlags(cmd *cobra.Command, cfg *config.Config) {
	if flagSet(cmd, "no-model") {
		cfg.Segments.Model = false
	}

	if flagSet(cmd, "no-effort") {
		cfg.Segments.Effort = false
	}

	if flagSet(cmd, "no-thinking") {
		cfg.Segments.Thinking = false
	}

	if flagSet(cmd, "no-fast-mode") {
		cfg.Segments.FastMode = false
	}

	if flagSet(cmd, "no-repo") {
		cfg.Segments.Repo = false
	}

	if flagSet(cmd, "no-worktree") {
		cfg.Segments.Worktree = false
	}
}

func applyDisplayFlags(cmd *cobra.Command, cfg *config.Config) {
	// The flag takes the same spellings as the config key, and an unknown value
	// warns instead of being swallowed — a mode that silently reads as "auto"
	// looks identical to a typo that did nothing. An empty --cost= carries no
	// choice, so it leaves the config value alone.
	if raw, _ := cmd.PersistentFlags().GetString("cost"); flagSet(cmd, "cost") && raw != "" {
		mode := config.NormalizeCostMode(raw)
		if mode == "" {
			fmt.Fprintf(os.Stderr, "claudeline: invalid cost mode %q, using auto\n", raw)

			mode = config.CostAuto
		}

		cfg.Segments.Cost = mode
	}

	if flagSet(cmd, "theme") {
		if val, _ := cmd.PersistentFlags().GetString("theme"); val != "" {
			cfg.Theme = val
		}
	}

	if flagSet(cmd, "hyperlinks") {
		if val, _ := cmd.PersistentFlags().GetString("hyperlinks"); val != "" {
			cfg.Hyperlinks = val
		}
	}

	if flagSet(cmd, "no-status") {
		cfg.Segments.Status = false
	}

	if flagSet(cmd, "no-context") {
		cfg.Segments.Context = false
	}

	if flagSet(cmd, "no-prompt-cache") {
		cfg.Segments.PromptCache = false
	}

	if flagSet(cmd, "no-compactions") {
		cfg.Segments.Compactions = false
	}
}

func applyUsageFlags(cmd *cobra.Command, cfg *config.Config) {
	if flagSet(cmd, "no-quota") {
		cfg.Segments.Quota = false
	}

	if flagSet(cmd, "mac-insecure") {
		cfg.MacInsecure = true
	}

	// As with --cost, an empty --per-model-quota= carries no choice and leaves the
	// config value alone.
	if raw, _ := cmd.PersistentFlags().GetString("per-model-quota"); flagSet(cmd, "per-model-quota") && raw != "" {
		mode := config.NormalizePerModelQuota(raw)
		if mode == "" {
			fmt.Fprintf(os.Stderr, "claudeline: invalid per-model quota mode %q, using auto\n", raw)

			mode = config.PerModelAuto
		}

		cfg.Segments.PerModelQuota = mode
	}

	if flagSet(cmd, "no-credits") {
		cfg.Segments.Credits = false
	}
}

func flagSet(cmd *cobra.Command, name string) bool {
	flag := cmd.PersistentFlags().Lookup(name)

	return flag != nil && flag.Changed
}

func main() {
	err := newRootCmd().Execute()
	if err != nil {
		os.Exit(1)
	}
}

func buildStatusline(raw []byte, cfg *config.Config) string {
	var data stdinData

	unmarshalErr := json.Unmarshal(raw, &data)
	if unmarshalErr != nil && len(raw) > 0 {
		fmt.Fprintf(os.Stderr, "claudeline: stdin parse error: %v\n", unmarshalErr)
	}

	var segments []string

	segments = appendIdentitySegments(segments, &data, cfg)
	segments = appendCostAndStatusSegments(segments, &data, cfg)
	segments = appendContextSegments(segments, &data, cfg)

	if cfg.Segments.Quota || cfg.Segments.Credits {
		segments = appendUsageSegments(segments, &data, cfg)
	}

	segments = appendRepoSegment(segments, &data, cfg)

	return fmtutil.JoinPipeWrap(segments, wrapWidth())
}

// wrapWidth returns the visual width JoinPipeWrap should target.
//
// Claude Code 2.1.153+ exports COLUMNS to statusline commands. We subtract
// [wrapSafetyMargin] cells as a buffer: COLUMNS reflects width at script
// launch (the terminal may have resized since), and empirically rows
// exactly equal to COLUMNS were observed to drop their rightmost
// character. The margin is a defensive choice, not a documented host
// behavior. Returns 0 when COLUMNS is missing, unparseable, or smaller
// than the safety margin — JoinPipeWrap then falls back to a single line.
func wrapWidth() int {
	raw := os.Getenv("COLUMNS")
	if raw == "" {
		return 0
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= wrapSafetyMargin {
		return 0
	}

	return n - wrapSafetyMargin
}

const wrapSafetyMargin = 2

// appendRepoSegment renders the combined repo/PR/worktree segment, or falls
// back to a bare "🌳 worktree 🌿 branch" segment when no repo info is available.
func appendRepoSegment(segments []string, data *stdinData, cfg *config.Config) []string {
	if cfg.Segments.Repo && data.Workspace.Repo != nil {
		return append(segments, formatRepoSegment(data))
	}

	if cfg.Segments.Worktree {
		if parts := worktreeBranchParts(data); len(parts) > 0 {
			return append(segments, strings.Join(parts, " "))
		}
	}

	return segments
}

// worktreeBranchParts returns the display parts identifying where work happens:
// "🌳 <name>" when cwd is a linked worktree, and "🌿 <branch>" for the current
// branch read from cwd/.git/HEAD. Either part may be absent.
//
// The same name is never printed under both markers. In the main clone the
// worktree marker is omitted (the name would just duplicate the repo name). When
// a linked worktree's branch equals its directory name — the common
// `git worktree add ../feat-x feat-x` pattern — the branch marker is dropped
// since 🌳 already shows it. And when HEAD carries no branch (detached or
// unreadable), the branch marker falls back to the stdin worktree name only if
// no 🌳 marker is already shown.
func worktreeBranchParts(data *stdinData) []string {
	cwd := resolveCwd(data)
	worktree := gitinfo.LinkedWorktreeName(cwd)
	branch := gitinfo.CurrentBranch(cwd)

	var parts []string

	if worktree != "" {
		parts = append(parts, fmtutil.Part(worktree, "🌳"))
	}

	switch {
	case branch != "" && branch != worktree:
		parts = append(parts, fmtutil.Part(branch, "🌿"))
	case branch == "" && worktree == "" && data.Workspace.GitWorktree != "":
		parts = append(parts, fmtutil.Part(data.Workspace.GitWorktree, "🌿"))
	}

	return parts
}

// resolveCwd picks the most specific cwd value Claude Code provides.
func resolveCwd(data *stdinData) string {
	if data.Workspace.CurrentDir != "" {
		return data.Workspace.CurrentDir
	}

	return data.Cwd
}

// formatRepoSegment builds the combined repo segment:
//
//	🐙 owner/repo [<state> #N] [🌳 worktree] [🌿 branch]
//
// GitLab merge requests render as !N, matching GitLab's own notation.
//
// Host icon varies by `workspace.repo.host`; unknown hosts surface as
// "📦 host/owner/repo" so the source is still legible. The 🌳 worktree marker
// appears only inside a linked worktree.
func formatRepoSegment(data *stdinData) string {
	repo := data.Workspace.Repo
	icon, prefix := repoHostIcon(repo.Host)

	parts := []string{fmtutil.Link(fmtutil.Part(prefix+repo.Owner+"/"+repo.Name, icon), repoURL(repo))}

	if data.PR != nil && data.PR.Number > 0 {
		sigil := "#"
		if data.PR.Kind == "mr" {
			sigil = "!"
		}

		number := fmt.Sprintf("%s%d", sigil, data.PR.Number)
		if state := prReviewIcon(data.PR.ReviewState); state != "" {
			number = fmtutil.Part(number, state)
		}

		parts = append(parts, fmtutil.Link(number, data.PR.URL))
	}

	parts = append(parts, worktreeBranchParts(data)...)

	return strings.Join(parts, " ")
}

// repoURL builds the web address of the repository. The harness sends the host,
// owner and name but no URL, and https is the only scheme worth guessing for a
// self-hosted forge. Any missing part means there is nothing to link, rather
// than a link into a path that does not exist.
func repoURL(repo *stdinRepoInfo) string {
	if repo.Host == "" || repo.Owner == "" || repo.Name == "" {
		return ""
	}

	return "https://" + repo.Host + "/" + repo.Owner + "/" + repo.Name
}

// repoHostIcon returns the leading emoji and an optional host prefix.
// Known hosts get a dedicated icon and no prefix; unknown hosts get a
// generic icon and "<host>/" prefix so the origin stays visible.
func repoHostIcon(host string) (icon, prefix string) {
	switch host {
	case "github.com":
		return "🐙", ""
	case "gitlab.com":
		return "🦊", ""
	case "bitbucket.org":
		return "🪣", ""
	case "":
		return "📦", ""
	default:
		return "📦", host + "/"
	}
}

func prReviewIcon(state string) string {
	switch state {
	case "draft":
		return "📝"
	case "approved":
		return "✅"
	case "changes_requested":
		return "🔴"
	case "commented":
		return "💬"
	case "pending":
		return "👀"
	default:
		return ""
	}
}

// appendIdentitySegments adds model segment.
func appendIdentitySegments(segments []string, data *stdinData, cfg *config.Config) []string {
	if cfg.Segments.Model {
		model := "Claude"
		if data.Model.DisplayName != "" {
			model = data.Model.DisplayName
		}

		segments = append(segments, fmtutil.Part(model, append([]string{"🤖"}, modelSubIcons(data, cfg)...)...))
	}

	return segments
}

// modelSubIcons returns the qualifier icons shown to the right of the model
// name: effort level, thinking, and fast-mode. Empty when none apply.
func modelSubIcons(data *stdinData, cfg *config.Config) []string {
	var icons []string

	if cfg.Segments.Effort {
		if e := effortIndicator(data.Effort.Level); e != "" {
			icons = append(icons, e)
		}
	}

	if cfg.Segments.Thinking && data.Thinking.Enabled {
		icons = append(icons, "💭")
	}

	if cfg.Segments.FastMode && data.FastMode {
		icons = append(icons, "⚡")
	}

	return icons
}

func effortIndicator(level string) string {
	switch level {
	case "low":
		return "⬇️"
	case "high":
		return "⬆️"
	case "xhigh":
		return "⏫"
	case "max":
		return "🚀"
	default:
		return ""
	}
}

// appendCostAndStatusSegments adds cost and platform status segments.
func appendCostAndStatusSegments(segments []string, data *stdinData, cfg *config.Config) []string {
	if shouldShowCost(cfg.Segments.Cost, data.RateLimits.FiveHour != nil || data.RateLimits.SevenDay != nil) {
		segments = append(segments, fmtutil.Part(fmt.Sprintf("$%.2f", data.Cost.TotalCostUSD), "💰"))
	}

	if cfg.Segments.Status {
		if alert := status.FetchAlert(); alert != "" {
			segments = append(segments, alert)
		}
	}

	return segments
}

// appendContextSegments adds context window and compaction segments.
func appendContextSegments(segments []string, data *stdinData, cfg *config.Config) []string {
	if cfg.Segments.Context && data.ContextWindow.UsedPercentage > 0 {
		segments = append(segments, fmtutil.ContextSegment(data.ContextWindow.UsedPercentage))
	}

	if cfg.Segments.PromptCache && shouldShowPromptCache(data.PromptCache) {
		segments = append(segments, fmtutil.PromptCacheSegment(promptCacheCause(data.PromptCache.LastMissCause)))
	}

	if cfg.Segments.Compactions {
		if compactions := compaction.CountCompactions(data.TranscriptPath); compactions > 0 {
			segments = append(segments, fmtutil.Part(strconv.Itoa(compactions), "🔄"))
		}
	}

	return segments
}

// shouldShowPromptCache reports whether the cold-cache marker is worth a
// segment. A warm cache is the normal state and says nothing. Without
// CachingObserved the provider never reported cache tokens at all, so Warm
// stays false for the whole session and the marker would be permanent
// furniture rather than a signal.
func shouldShowPromptCache(cache *stdinPromptCache) bool {
	return cache != nil && cache.CachingObserved && !cache.Warm
}

// promptCacheCauseLabels shortens the harness's cache-miss cause names to one
// word each. A name absent here falls back to itself minus the "_changed"
// suffix, so a cause introduced by a later harness still reads as something.
var promptCacheCauseLabels = map[string]string{
	"system_prompt_changed":      "prompt",
	"tools_changed":              "tools",
	"model_changed":              "model",
	"fast_mode_changed":          "fast",
	"cache_scope_or_ttl_changed": "scope",
	"betas_changed":              "betas",
	"effort_changed":             "effort",
	"auto_mode_changed":          "auto",
	"overage_changed":            "limits",
	"extra_body_changed":         "request",
	"defer_loading_changed":      "defer",
	"messages_rewritten":         "history",
	"ttl_expired_5m":             "ttl",
	"ttl_expired_1h":             "ttl",
	"likely_server_side":         "server",
	// The harness reports "unknown" when it could not attribute the miss, and
	// only ever as the sole cause. An empty label renders the bare icon, which
	// is what "no cause identified" should look like.
	"unknown": "",
}

// promptCacheCause renders the cause of the session's most recent miss in one
// word, empty when none was identified.
//
// It usually describes an older event than the cold state: a miss rewrites the
// cache, so the prefix reads warm again on the request that missed, and the
// cache goes cold afterwards by idling past its TTL. A response carrying no
// cache tokens at all is the other way in, and there the miss can be seconds
// old.
//
// Several causes collapse to the first plus a "+", since the statusline has
// room for one word.
func promptCacheCause(cause *stdinMissCause) string {
	if cause == nil || len(cause.Causes) == 0 {
		return ""
	}

	label, ok := promptCacheCauseLabels[cause.Causes[0]]
	if !ok {
		label = strings.TrimSuffix(cause.Causes[0], "_changed")
	}

	// An empty label stands for "no cause identified", which nothing meaningful
	// can be appended to. The harness reports its one unlabelled cause alone
	// today, so this guards a future change rather than current behavior.
	if len(cause.Causes) > 1 && label != "" {
		label += "+"
	}

	return label
}

// shouldShowCost determines whether to display the cost segment.
// In "auto" mode, cost is hidden for subscribers (who have rate_limits).
func shouldShowCost(mode string, isSubscriber bool) bool {
	switch mode {
	case config.CostOn:
		return true
	case config.CostOff:
		return false
	default: // auto
		return !isSubscriber
	}
}

// stdinRateWindowToQuota converts a stdin rate limit window to a fmtutil.QuotaWindow.
func stdinRateWindowToQuota(win *stdinRateWindow, totalMinutes int) *fmtutil.QuotaWindow {
	if win == nil {
		return nil
	}

	resetsAt := time.Unix(int64(win.ResetsAt), 0)
	remaining := max(int(time.Until(resetsAt).Minutes()), 0)

	return &fmtutil.QuotaWindow{
		Utilization:      win.UsedPercentage,
		ResetsAt:         resetsAt,
		TotalMinutes:     totalMinutes,
		RemainingMinutes: remaining,
	}
}

// buildQuotaFromStdin builds quota data from stdin rate_limits (no API call).
func buildQuotaFromStdin(data *stdinData) *fmtutil.Data {
	return &fmtutil.Data{
		FiveHour: stdinRateWindowToQuota(data.RateLimits.FiveHour, fmtutil.FiveHourWindowMinutes),
		SevenDay: stdinRateWindowToQuota(data.RateLimits.SevenDay, fmtutil.SevenDayWindowMinutes),
	}
}

// modelQuotaSelector picks the per-model quota windows to display, following the
// per_model_quota mode and the model currently selected in the session.
type modelQuotaSelector struct {
	mode        string
	modelID     string
	displayName string
}

// newModelQuotaSelector normalizes the mode rather than trusting its writers to
// have done it: an unrecognized value would otherwise fall through to auto, which
// is indistinguishable from having asked for auto.
func newModelQuotaSelector(data *stdinData, cfg *config.Config) modelQuotaSelector {
	mode := config.NormalizePerModelQuota(cfg.Segments.PerModelQuota)
	if mode == "" {
		mode = config.PerModelAuto
	}

	return modelQuotaSelector{
		mode:        mode,
		modelID:     data.Model.ID,
		displayName: data.Model.DisplayName,
	}
}

// windows returns nothing when per-model quotas are off, every reported window
// when they are all requested, and otherwise (auto) only the window belonging to
// the model in use — so switching to Fable surfaces the Fable bucket by itself.
// A model reported by more than one bucket collapses to the bucket closest to
// its limit, so the constraint that will bite first is never the hidden one.
func (s modelQuotaSelector) windows(data *fmtutil.Data) []fmtutil.ScopedWindow {
	if data == nil || s.mode == config.PerModelOff {
		return nil
	}

	if s.mode == config.PerModelAll {
		return data.PerModelWindows()
	}

	// Match against the uncollapsed candidates: two buckets can share a label
	// while naming different models, and collapsing first could let the bucket of
	// another model win the label and hide the one that applies here.
	candidates := data.PerModelCandidates()

	matched := make([]fmtutil.ScopedWindow, 0, len(candidates))

	for _, win := range candidates {
		if fmtutil.MatchesScopedModel(s.modelID, s.displayName, win) {
			matched = append(matched, win)
		}
	}

	return fmtutil.MostBinding(matched)
}

// appendWindowSegments renders the account-wide windows followed by the per-model
// ones the caller selected, through the given formatter. The fresh and the stale
// paths differ only in that formatter, so the window list is assembled once.
func appendWindowSegments(
	segments []string,
	data *fmtutil.Data,
	perModel []fmtutil.ScopedWindow,
	format func(*fmtutil.QuotaWindow, string) string,
) []string {
	type labeledWindow struct {
		win   *fmtutil.QuotaWindow
		label string
	}

	windows := make([]labeledWindow, 0, 2+len(perModel)) //nolint:mnd // the two account-wide windows below
	windows = append(windows,
		labeledWindow{data.SevenDay, "7d"},
		labeledWindow{data.FiveHour, "5h"},
	)

	for _, scoped := range perModel {
		windows = append(windows, labeledWindow{scoped.Window, fmtutil.ScopedLabel(scoped.Name)})
	}

	for _, w := range windows {
		if w.win != nil {
			segments = append(segments, format(w.win, w.label))
		}
	}

	return segments
}

func appendQuotaWindows(segments []string, data *fmtutil.Data, perModel []fmtutil.ScopedWindow) []string {
	return appendWindowSegments(segments, data, perModel, fmtutil.FormatQuotaWindow)
}

func appendUsageSegments(segments []string, data *stdinData, cfg *config.Config) []string {
	if cfg.MacInsecure {
		return appendInsecureUsageSegments(segments, data, cfg)
	}

	return appendStdinUsageSegments(segments, data, cfg)
}

// appendStdinUsageSegments builds quota segments from stdin rate_limits (default, secure path).
// Per-model windows are unavailable here: stdin carries only the account-wide
// five_hour and seven_day windows.
func appendStdinUsageSegments(segments []string, data *stdinData, cfg *config.Config) []string {
	quotaData := buildQuotaFromStdin(data)

	if cfg.Segments.Quota {
		segments = appendQuotaWindows(segments, quotaData, nil)
	}

	return segments
}

// appendInsecureUsageSegments builds quota segments from Anthropic API via macOS Keychain (--mac-insecure).
// Per-model quotas live only here: the API reports them, stdin does not.
func appendInsecureUsageSegments(segments []string, data *stdinData, cfg *config.Config) []string {
	selector := newModelQuotaSelector(data, cfg)

	usageData, err := usage.Fetch()
	if err != nil {
		if cfg.Segments.Quota {
			segments = appendStaleQuotaSegments(segments, selector)
		}

		return segments
	}

	switch usageData.ErrorType {
	case "":
		// no error, continue
	case "rate_limit_error":
		return appendRateLimitSegments(segments, cfg, selector)
	default:
		return append(segments, fmtutil.Part("/login needed", "⚠️"))
	}

	if cfg.Segments.Quota {
		segments = appendQuotaWindows(segments, usageData, selector.windows(usageData))
	}

	if cfg.Segments.Credits && usageData.Extra != nil && usageData.Extra.UsedCredits > 0 {
		segments = append(segments, fmtutil.Part(fmt.Sprintf("$%.0f/$%.0f",
			usageData.Extra.UsedCredits, usageData.Extra.MonthlyLimit), "💳"))
	}

	return segments
}

func appendStaleQuotaSegments(segments []string, selector modelQuotaSelector) []string {
	lastGood := usage.FetchLastGood()

	return appendStaleQuotaWindows(segments, lastGood, selector.windows(lastGood))
}

// appendStaleQuotaWindows renders last-good data the caller already fetched and
// already ran through the selector. Both are worth doing once: reading last-good
// is a file read plus a parse that rewrites the cache, and the rate-limit path
// needs the selected windows anyway to name the exhausted one.
func appendStaleQuotaWindows(segments []string, lastGood *fmtutil.Data, perModel []fmtutil.ScopedWindow) []string {
	if lastGood == nil {
		return append(segments, fmtutil.Part("7d: ?% (?d)", "⏳"), fmtutil.Part("5h: ?% (?h)", "⏳"))
	}

	return appendWindowSegments(segments, lastGood, perModel, fmtutil.FormatStaleQuotaWindow)
}

func appendRateLimitSegments(segments []string, cfg *config.Config, selector modelQuotaSelector) []string {
	lastGood := usage.FetchLastGood()
	perModel := selector.windows(lastGood)

	segments = append(segments, fmtutil.FormatRateLimitSegment(fmtutil.FindExhaustedWindow(lastGood, perModel)))

	if cfg.Segments.Quota {
		segments = appendStaleQuotaWindows(segments, lastGood, perModel)
	}

	return segments
}
