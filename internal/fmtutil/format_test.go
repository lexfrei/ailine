package fmtutil

import (
	"strings"
	"testing"
	"time"
)

// wantPipeJoinABC is the canonical pipe-joined fixture shared by tests of
// both JoinPipe and JoinPipeWrap, which must agree on the single-line form.
const wantPipeJoinABC = "a | b | c"

// Sample labels reused across Part fixtures.
const (
	testPRNumber  = "#19"
	testRepoSlug  = "lexfrei/ailine"
	testModelName = "Opus 4.7"
)

func TestDuration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		minutes int
		want    string
	}{
		{0, DurationNow},
		{-5, DurationNow},
		{7, "7m"},
		{60, "1h 0m"},
		{125, "2h 5m"},
		{1440, "1d 0h"},
		{1500, "1d 1h"},
		{10080, "7d 0h"},
		{4337, "3d 0h"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()

			if got := Duration(tt.minutes); got != tt.want {
				t.Errorf("Duration(%d) = %q, want %q", tt.minutes, got, tt.want)
			}
		})
	}
}

func TestRateIndicator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                       string
		usagePct, remaining, total int
		want                       string
	}{
		{"on_track", 45, 150, 300, "🟢"},
		{"slightly_ahead", 53, 150, 300, "🟡"},
		{"ahead", 60, 150, 300, "🟠"},
		{"way_ahead", 70, 150, 300, "🔴"},
		{"start_of_window", 0, 300, 300, "🟢"},
		{"end_of_window", 100, 0, 300, "🟢"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := RateIndicator(tt.usagePct, tt.remaining, tt.total); got != tt.want {
				t.Errorf("RateIndicator(%d, %d, %d) = %q, want %q",
					tt.usagePct, tt.remaining, tt.total, got, tt.want)
			}
		})
	}
}

func TestContextSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		pct       float64
		wantColor string
	}{
		{"green_low", 30, ansiGreen},
		{"green_border", 49.4, ansiGreen},
		{"yellow_border", 50, ansiYellow},
		{"yellow_high", 79, ansiYellow},
		{"red_border", 80, ansiRed},
		{"red_high", 95, ansiRed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ContextSegment(tt.pct)
			if !strings.HasPrefix(got, tt.wantColor) {
				t.Errorf("ContextSegment(%.1f) starts with wrong color, got %q", tt.pct, got)
			}
		})
	}
}

func TestParseISOUTC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{"basic_z", "2025-02-26T12:34:56Z"},
		{"nano_z", "2025-02-26T12:34:56.123456Z"},
		{"offset", "2025-02-26T12:34:56+00:00"},
		{"nano_offset", "2025-02-26T12:34:56.123456+00:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := ParseISOUTC(tt.raw)
			if err != nil {
				t.Errorf("ParseISOUTC(%q) failed: %v", tt.raw, err)

				return
			}

			if parsed.Year() != 2025 || parsed.Month() != 2 || parsed.Day() != 26 {
				t.Errorf("ParseISOUTC(%q) = %v, wrong date", tt.raw, parsed)
			}
		})
	}
}

func TestParseISOUTCInvalid(t *testing.T) {
	t.Parallel()

	if _, err := ParseISOUTC("not-a-date"); err == nil {
		t.Error("ParseISOUTC(invalid) should return error")
	}
}

func TestJoinPipe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"nil", nil, ""},
		{"single", []string{"a"}, "a"},
		{"multiple", []string{"a", "b", "c"}, wantPipeJoinABC},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := JoinPipe(tt.parts); got != tt.want {
				t.Errorf("JoinPipe(%v) = %q, want %q", tt.parts, got, tt.want)
			}
		})
	}
}

func TestFormatQuotaWindow(t *testing.T) {
	t.Parallel()

	win := &QuotaWindow{
		Utilization:      45.3,
		ResetsAt:         time.Now().Add(3 * time.Hour),
		TotalMinutes:     10080,
		RemainingMinutes: 6857,
	}

	got := FormatQuotaWindow(win, "7d")
	if got == "" {
		t.Error("FormatQuotaWindow returned empty string")
	}

	if !strings.Contains(got, "7d") || !strings.Contains(got, "45%") {
		t.Errorf("FormatQuotaWindow = %q, missing expected content", got)
	}
}

func TestFormatStaleQuotaWindow(t *testing.T) {
	t.Parallel()

	win := &QuotaWindow{
		Utilization:      45.3,
		ResetsAt:         time.Now().Add(3 * time.Hour),
		TotalMinutes:     10080,
		RemainingMinutes: 6857,
	}

	got := FormatStaleQuotaWindow(win, "7d")
	if !strings.Contains(got, "?%") {
		t.Errorf("FormatStaleQuotaWindow should contain '?%%', got %q", got)
	}
}

func TestFindExhaustedWindow(t *testing.T) {
	t.Parallel()

	got := FindExhaustedWindow(&Data{
		FiveHour: &QuotaWindow{
			Utilization:      88,
			RemainingMinutes: 45,
		},
		SevenDay: &QuotaWindow{
			Utilization:      99,
			RemainingMinutes: 125,
		},
	}, nil)

	if got == nil {
		t.Fatal("expected non-nil ExhaustedWindow")
	}

	if got.Name != "7d" {
		t.Errorf("Name = %q, want %q", got.Name, "7d")
	}

	if got.Minutes != 125 {
		t.Errorf("Minutes = %d, want %d", got.Minutes, 125)
	}
}

func TestFindExhaustedWindowFiveHour(t *testing.T) {
	t.Parallel()

	got := FindExhaustedWindow(&Data{
		FiveHour: &QuotaWindow{
			Utilization:      100,
			RemainingMinutes: 30,
		},
		SevenDay: &QuotaWindow{
			Utilization:      50,
			RemainingMinutes: 125,
		},
	}, nil)

	if got == nil {
		t.Fatal("expected non-nil ExhaustedWindow")
	}

	if got.Name != "5h" {
		t.Errorf("Name = %q, want %q", got.Name, "5h")
	}
}

func TestFindExhaustedWindowPerModel(t *testing.T) {
	t.Parallel()

	data := &Data{
		SevenDay: &QuotaWindow{
			Utilization:      50,
			RemainingMinutes: 125,
		},
		SevenDayOpus: &QuotaWindow{
			Utilization:      100,
			RemainingMinutes: 60,
		},
	}

	got := FindExhaustedWindow(data, data.PerModelWindows())
	if got == nil {
		t.Fatal("expected non-nil ExhaustedWindow")
	}

	if got.Name != "7d-opus" {
		t.Errorf("Name = %q, want %q", got.Name, "7d-opus")
	}

	gotDisabled := FindExhaustedWindow(data, nil)
	if gotDisabled != nil {
		t.Errorf("expected nil when per-model windows are not displayed, got %+v", gotDisabled)
	}
}

func TestFindExhaustedWindowNil(t *testing.T) {
	t.Parallel()

	if got := FindExhaustedWindow(nil, nil); got != nil {
		t.Error("expected nil for nil data")
	}
}

func TestFindExhaustedWindowNoExhausted(t *testing.T) {
	t.Parallel()

	got := FindExhaustedWindow(&Data{
		FiveHour: &QuotaWindow{Utilization: 50, RemainingMinutes: 45},
		SevenDay: &QuotaWindow{Utilization: 60, RemainingMinutes: 125},
	}, nil)

	if got != nil {
		t.Error("expected nil when no window is exhausted")
	}
}

func TestFormatRateLimitSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    *ExhaustedWindow
		expected string
	}{
		{"nil window", nil, "⛔ limit hit"},
		{"5h with time", &ExhaustedWindow{Name: "5h", Minutes: 134}, "⛔ 5h limit hit (2h 14m)"},
		{"7d with time", &ExhaustedWindow{Name: "7d", Minutes: 1440}, "⛔ 7d limit hit (1d 0h)"},
		{"zero minutes", &ExhaustedWindow{Name: "5h", Minutes: 0}, "⛔ 5h limit hit"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FormatRateLimitSegment(tt.input); got != tt.expected {
				t.Errorf("FormatRateLimitSegment() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		icons []string
		want  string
	}{
		{"leading icon and text", testRepoSlug, []string{"🐙"}, "🐙 " + testRepoSlug},
		{"review icon left of number", testPRNumber, []string{"📝"}, "📝 " + testPRNumber},
		{"leading plus one sub-icon", "7d: 42% (4d 2h)", []string{"🟡", "⬆"}, "🟡 7d: 42% (4d 2h) ⬆"},
		{"sub-icons glued without separators", testModelName, []string{"🤖", "⏫", "💭", "⚡"}, "🤖 " + testModelName + " ⏫💭⚡"},
		{"empty sub-icons ignored", testModelName, []string{"🤖", "", ""}, "🤖 " + testModelName},
		{"zero icons yields text only", testPRNumber, nil, testPRNumber},
		{"single icon before count", "2", []string{"🔄"}, "🔄 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Part(tt.text, tt.icons...); got != tt.want {
				t.Errorf("Part(%q, %v) = %q, want %q", tt.text, tt.icons, got, tt.want)
			}
		})
	}
}

func TestPartStyledText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		text  string
		icons []string
		want  string
	}{
		{"green circle colors text", "7d: 42% (4d 2h)", []string{"🟢"}, ansiGreen + "7d: 42% (4d 2h)" + ansiReset},
		{"yellow circle colors text", "5h: 80%", []string{"🟡"}, ansiYellow + "5h: 80%" + ansiReset},
		{"orange circle colors text", "5h: 80%", []string{"🟠"}, ansiOrange + "5h: 80%" + ansiReset},
		{"red circle colors text", testPRNumber, []string{"🔴"}, ansiRed + testPRNumber + ansiReset},
		{"major diamond colors text orange", "major outage", []string{"🔶"}, ansiOrange + "major outage" + ansiReset},
		{"warning triangle colors text yellow", "degraded", []string{"⚠️"}, ansiYellow + "degraded" + ansiReset},
		{"non-circle leading icon dropped", testRepoSlug, []string{"🐙"}, testRepoSlug},
		{"sub-icons dropped", testModelName, []string{"🤖", "⏫", "💭", "⚡"}, testModelName},
		{"zero icons yields text", testPRNumber, nil, testPRNumber},
		{"first circle wins", "x", []string{"🟢", "🔴"}, ansiGreen + "x" + ansiReset},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := PartStyled(StyleText, tt.text, tt.icons...); got != tt.want {
				t.Errorf("PartStyled(StyleText, %q, %v) = %q, want %q", tt.text, tt.icons, got, tt.want)
			}
		})
	}
}

// useTextStyle switches the package-global Style to StyleText for one test and
// restores it after. Callers must NOT run in parallel, since Style is shared
// process state.
func useTextStyle(t *testing.T) {
	t.Helper()

	prev := Style
	Style = StyleText

	t.Cleanup(func() { Style = prev })
}

func TestPartUsesGlobalStyle(t *testing.T) {
	useTextStyle(t)

	want := ansiYellow + "7d: 42%" + ansiReset
	if got := Part("7d: 42%", "🟡"); got != want {
		t.Errorf("Part under StyleText = %q, want %q", got, want)
	}
}

func TestContextSegmentText(t *testing.T) {
	useTextStyle(t)

	// 67% is the yellow threshold (>=50, <80); the 🧠 is dropped and the text
	// carries the threshold color exactly once (no double wrap).
	want := ansiYellow + "67%" + ansiReset
	if got := ContextSegment(67); got != want {
		t.Errorf("ContextSegment(67) text mode = %q, want %q", got, want)
	}
}

func TestFormatQuotaWindowText(t *testing.T) {
	useTextStyle(t)

	// Low utilization with most of the window remaining -> green rate -> green
	// text, and no circle glyph survives.
	win := &QuotaWindow{
		Utilization:      10,
		ResetsAt:         time.Now().Add(3 * time.Hour),
		TotalMinutes:     10080,
		RemainingMinutes: 9000,
	}

	got := FormatQuotaWindow(win, "7d")
	if !strings.HasPrefix(got, ansiGreen) || !strings.HasSuffix(got, ansiReset) {
		t.Errorf("expected green-wrapped text, got %q", got)
	}

	if strings.ContainsAny(got, "🟢🟡🟠🔴") {
		t.Errorf("expected no circle glyph in text mode, got %q", got)
	}
}

func TestFormatRateLimitSegmentText(t *testing.T) {
	useTextStyle(t)

	// ⛔ is not a circle, so it is dropped to plain text.
	got := FormatRateLimitSegment(&ExhaustedWindow{Name: "5h", Minutes: 134})
	want := "5h limit hit (2h 14m)"

	if got != want {
		t.Errorf("FormatRateLimitSegment text mode = %q, want %q", got, want)
	}
}

func TestPromptCacheSegment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		style IconStyle
		cause string
		want  string
	}{
		{"emoji with cause", StyleEmoji, "tools", "🧊 tools"},
		{"emoji without cause", StyleEmoji, "", "🧊"},
		{"text with cause", StyleText, "tools", "cache: tools"},
		{"text without cause", StyleText, "", "cache: cold"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := promptCacheSegmentStyled(tt.style, tt.cause); got != tt.want {
				t.Errorf("promptCacheSegmentStyled(%v, %q) = %q, want %q", tt.style, tt.cause, got, tt.want)
			}
		})
	}
}

func TestPromptCacheSegmentUsesGlobalStyle(t *testing.T) {
	useTextStyle(t)

	if got := PromptCacheSegment("tools"); got != "cache: tools" {
		t.Errorf("PromptCacheSegment under StyleText = %q, want %q", got, "cache: tools")
	}
}
