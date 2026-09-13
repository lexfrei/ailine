// Package fmtutil provides formatting helpers for the Claude Code statusline.
package fmtutil

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiReset  = "\033[0m"
	// ansiOrange is a 256-color escape (no orange in the basic 8). The
	// VisualWidth strip regex matches it, so it stays width-safe.
	ansiOrange = "\033[38;5;208m"

	minutesPerDay  = 1440
	minutesPerHour = 60
	percentBase    = 100
	halfRound      = 0.5

	rateSlightlyAhead = 5
	rateFarAhead      = 15

	contextWarnPct = 50
	contextCritPct = 80
)

// ErrCannotParseTimestamp is returned when a timestamp string cannot be parsed.
var ErrCannotParseTimestamp = errors.New("cannot parse timestamp")

// DurationNow is the rendering used for non-positive durations.
const DurationNow = "now"

// Duration returns a human-readable duration from minutes.
func Duration(minutes int) string {
	if minutes <= 0 {
		return DurationNow
	}

	days := minutes / minutesPerDay
	hours := (minutes % minutesPerDay) / minutesPerHour
	mins := minutes % minutesPerHour

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// RateIndicator compares usage% with elapsed time% to produce a traffic-light emoji.
func RateIndicator(usagePct, remainingMins, totalMins int) string {
	elapsed := max(totalMins-remainingMins, 0)

	timePct := 0
	if totalMins > 0 {
		timePct = elapsed * percentBase / totalMins
	}

	diff := usagePct - timePct

	switch {
	case diff <= 0:
		return "🟢"
	case diff <= rateSlightlyAhead:
		return "🟡"
	case diff <= rateFarAhead:
		return "🟠"
	default:
		return "🔴"
	}
}

// ContextSegment returns an ANSI-colored context percentage segment.
func ContextSegment(pct float64) string {
	rounded := int(pct + halfRound)

	var color string

	switch {
	case rounded >= contextCritPct:
		color = ansiRed
	case rounded >= contextWarnPct:
		color = ansiYellow
	default:
		color = ansiGreen
	}

	return color + Part(fmt.Sprintf("%d%%", rounded), "🧠") + ansiReset
}

// coldCacheIcon marks a prompt cache that has gone cold.
const coldCacheIcon = "🧊"

// PromptCacheSegment renders the cold-prompt-cache marker, optionally naming
// the cause of the last miss. An empty cause means the harness could not
// identify one.
//
// The text theme has no icon to carry the meaning, so the segment names itself
// there: a bare cause word standing between a percentage and a repo path would
// read as nothing at all.
func PromptCacheSegment(cause string) string {
	return promptCacheSegmentStyled(Style, cause)
}

// promptCacheSegmentStyled is the pure core behind PromptCacheSegment, kept
// separate so tests can exercise both styles without mutating the Style global.
func promptCacheSegmentStyled(style IconStyle, cause string) string {
	if style == StyleText {
		if cause == "" {
			return "cache: cold"
		}

		return "cache: " + cause
	}

	if cause == "" {
		return coldCacheIcon
	}

	return PartStyled(style, cause, coldCacheIcon)
}

// ParseISOUTC parses an ISO-8601 timestamp to UTC time.
func ParseISOUTC(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)

	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05+00:00",
	}

	for _, layout := range formats {
		parsed, parseErr := time.Parse(layout, raw)
		if parseErr == nil {
			return parsed.UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("%w: %s", ErrCannotParseTimestamp, raw)
}

// JoinPipe joins string segments with " | " separator.
func JoinPipe(parts []string) string {
	return strings.Join(parts, " | ")
}

// IconStyle selects how Part renders the icons attached to a part.
type IconStyle int

const (
	// StyleEmoji is the historical rendering: icons are shown as emoji glyphs
	// around the text. Output is byte-for-byte identical to pre-theme releases.
	StyleEmoji IconStyle = iota
	// StyleText drops every emoji icon. When one of the icons is a status glyph
	// (a rate circle 🟢🟡🟠🔴 or a severity marker 🔶/⚠️) its color is carried onto
	// the text instead, so rate and severity survive as color rather than a glyph.
	StyleText
)

// Style is the process-global icon style, set once at startup before any
// segment is built. It defaults to StyleEmoji so untouched code and every
// existing test keep their historical output.
var Style = StyleEmoji

// statusColor maps a status glyph to the ANSI color it stands for. Used only in
// StyleText: the glyph is dropped and its color wraps the part's text. Covers
// the rate circles and the platform-severity markers so all severity levels —
// yellow (minor), orange (major) and red (critical) — survive as text color.
var statusColor = map[string]string{
	"🟢":  ansiGreen,
	"🟡":  ansiYellow,
	"🟠":  ansiOrange,
	"🔴":  ansiRed,
	"🔶":  ansiOrange,
	"⚠️": ansiYellow,
}

// Part renders a statusline part from its text and zero or more icons under the
// process-global Style. The first icon (when present and non-empty) is the
// leading icon, placed to the left of the text; any further icons are
// qualifiers glued to the right without separators. Zero icons is valid and
// yields just the text — the case a no-emoji theme relies on.
func Part(text string, icons ...string) string {
	return PartStyled(Style, text, icons...)
}

// PartStyled renders a part under an explicit style. It is the pure core behind
// Part, kept separate so tests can exercise both styles without mutating the
// Style global.
func PartStyled(style IconStyle, text string, icons ...string) string {
	if style == StyleText {
		for _, icon := range icons {
			if color, ok := statusColor[icon]; ok {
				return color + text + ansiReset
			}
		}

		return text
	}

	var lead, sub string
	if len(icons) > 0 {
		lead = icons[0]
		sub = strings.Join(icons[1:], "")
	}

	out := text
	if lead != "" {
		out = lead + " " + text
	}

	if sub != "" {
		out += " " + sub
	}

	return out
}

// Quota window constants.
const (
	FiveHourWindowMinutes = 300
	SevenDayWindowMinutes = 10_080
	exhaustedThresholdPct = 99
)

// QuotaWindow represents a single usage quota window (5-hour or 7-day).
type QuotaWindow struct {
	Utilization      float64
	ResetsAt         time.Time
	TotalMinutes     int
	RemainingMinutes int
}

// ExtraUsage represents monthly extra/overuse budget.
type ExtraUsage struct {
	MonthlyLimit float64
	UsedCredits  float64
}

// ExhaustedWindow contains information about which rate limit window was exhausted.
type ExhaustedWindow struct {
	// Name is "5h", "7d", or a per-model label built by ScopedLabel from the
	// bucket name the server supplied ("7d-opus", "7d-fable", …) — the set is
	// open, since the server names the buckets.
	Name    string
	Minutes int // minutes until reset
}

// Data is the parsed quota usage data.
type Data struct {
	FiveHour          *QuotaWindow
	SevenDay          *QuotaWindow
	SevenDayOpus      *QuotaWindow
	SevenDaySonnet    *QuotaWindow
	SevenDayCowork    *QuotaWindow
	SevenDayOAuthApps *QuotaWindow
	// Scoped holds per-model weekly windows the server names itself (limits[]
	// entries scoped to a model, e.g. "Fable"). Unlike the fields above these
	// need no client change when a new model ships.
	Scoped    []ScopedWindow
	Extra     *ExtraUsage
	ErrorType string
}

// FormatStaleQuotaWindow formats a window with ?% but real time and indicator.
func FormatStaleQuotaWindow(win *QuotaWindow, label string) string {
	pct := int(win.Utilization + halfRound)
	indicator := RateIndicator(pct, win.RemainingMinutes, win.TotalMinutes)
	dur := Duration(win.RemainingMinutes)

	return Part(fmt.Sprintf("%s: ?%% (%s)", label, dur), indicator)
}

// FormatQuotaWindow formats a single quota window for display.
func FormatQuotaWindow(win *QuotaWindow, label string) string {
	pct := int(win.Utilization + halfRound)
	indicator := RateIndicator(pct, win.RemainingMinutes, win.TotalMinutes)
	dur := Duration(win.RemainingMinutes)

	return Part(fmt.Sprintf("%s: %d%% (%s)", label, pct, dur), indicator)
}

// FormatRateLimitSegment formats the explicit exhausted-limit segment.
func FormatRateLimitSegment(exhausted *ExhaustedWindow) string {
	if exhausted == nil {
		return Part("limit hit", "⛔")
	}

	if exhausted.Minutes <= 0 {
		return Part(exhausted.Name+" limit hit", "⛔")
	}

	return Part(fmt.Sprintf("%s limit hit (%s)", exhausted.Name, Duration(exhausted.Minutes)), "⛔")
}

// FindExhaustedWindow returns the most saturated active window that is exhausted.
// The account-wide windows are always considered; perModel adds the per-model
// windows the caller decided to display, so a hidden window never names the limit.
func FindExhaustedWindow(data *Data, perModel []ScopedWindow) *ExhaustedWindow {
	if data == nil {
		return nil
	}

	type windowEntry struct {
		win  *QuotaWindow
		name string
	}

	windows := []windowEntry{
		{data.FiveHour, "5h"},
		{data.SevenDay, "7d"},
	}

	for _, scoped := range perModel {
		windows = append(windows, windowEntry{scoped.Window, ScopedLabel(scoped.Name)})
	}

	var best *ExhaustedWindow

	bestPct := -1

	for _, entry := range windows {
		if entry.win == nil || entry.win.RemainingMinutes <= 0 {
			continue
		}

		pct := int(entry.win.Utilization + halfRound)
		if pct < exhaustedThresholdPct {
			continue
		}

		if pct > bestPct || (pct == bestPct && (best == nil || entry.win.RemainingMinutes < best.Minutes)) {
			bestPct = pct
			best = &ExhaustedWindow{
				Name:    entry.name,
				Minutes: entry.win.RemainingMinutes,
			}
		}
	}

	return best
}
