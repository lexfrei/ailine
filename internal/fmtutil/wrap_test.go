package fmtutil

import (
	"strings"
	"testing"
)

func TestVisualWidth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "hello", 5},
		{"emoji 2-cell", "🤖", 2},
		{"emoji plus ascii", "🤖 Opus", 7},
		{"ansi color stripped", "\x1b[32m🧠 50%\x1b[0m", 6},
		{"osc8 hyperlink stripped", "\x1b]8;;https://example.com\x07#42\x1b]8;;\x07", 3},
		{"osc8 with ST terminator stripped", "\x1b]8;;https://example.com\x1b\\#42\x1b]8;;\x1b\\", 3},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			t.Parallel()

			got := VisualWidth(tcase.in)
			if got != tcase.want {
				t.Errorf("VisualWidth(%q) = %d, want %d", tcase.in, got, tcase.want)
			}
		})
	}
}

func TestJoinPipeWrapFitsInOneLine(t *testing.T) {
	t.Parallel()

	segs := []string{"a", "b", "c"}

	got := JoinPipeWrap(segs, 80)
	if got != wantPipeJoinABC {
		t.Errorf("expected single line, got %q", got)
	}

	if strings.Contains(got, "\n") {
		t.Errorf("expected no newline in narrow-fit output, got %q", got)
	}
}

func TestJoinPipeWrapZeroFallsBack(t *testing.T) {
	t.Parallel()

	segs := []string{"a", "b", "c"}

	// maxWidth=0 (env unset) must fall back to single line so we don't
	// fragment statuslines on terminals that don't expose COLUMNS.
	if got := JoinPipeWrap(segs, 0); got != wantPipeJoinABC {
		t.Errorf("maxWidth=0: expected single line, got %q", got)
	}
}

func TestJoinPipeWrapEmpty(t *testing.T) {
	t.Parallel()

	if got := JoinPipeWrap(nil, 80); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestJoinPipeWrapBreaksOnOverflow(t *testing.T) {
	t.Parallel()

	// Three 5-char segments + two " | " separators = 21 cells.
	// At maxWidth=14 the first two ("aaaaa | bbbbb" = 13) fit; the third wraps.
	segs := []string{"aaaaa", "bbbbb", "ccccc"}

	got := JoinPipeWrap(segs, 14)

	want := "aaaaa | bbbbb\nccccc"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinPipeWrapEmojiCountedAsTwoCells(t *testing.T) {
	t.Parallel()

	// "🤖 a" is 4 cells, "🐙 b" is 4 cells, separator " | " is 3 cells.
	// Inline = 4 + 3 + 4 = 11. At maxWidth=10 they must wrap.
	segs := []string{"🤖 a", "🐙 b"}

	got := JoinPipeWrap(segs, 10)
	if got != "🤖 a\n🐙 b" {
		t.Errorf("expected wrap on emoji width, got %q", got)
	}
}

func TestJoinPipeWrapOversizedSegmentGetsOwnLine(t *testing.T) {
	t.Parallel()

	// "longerthanwidth" is 15 cells, wider than maxWidth=10. It must land on
	// its own line — we never split a segment internally.
	segs := []string{"a", "longerthanwidth", "b"}

	got := JoinPipeWrap(segs, 10)

	want := "a\nlongerthanwidth\nb"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJoinPipeWrapMultiLineMixed(t *testing.T) {
	t.Parallel()

	segs := []string{
		"🤖 Opus 4.7 ⏫💭", // ~16 cells
		"🧠 67%",         // ~6 cells
		"🔄 2",           // ~4 cells
		"🟡 7d: 42%",     // ~10 cells
		"🔴 5h: 91%",     // ~10 cells
	}

	got := JoinPipeWrap(segs, 30)

	want := "🤖 Opus 4.7 ⏫💭 | 🧠 67%\n🔄 2 | 🟡 7d: 42% | 🔴 5h: 91%"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// Every line must stay within budget — pinned alongside the literal so a
	// regression in measurement or packing surfaces with the same fixture.
	for line := range strings.SplitSeq(got, "\n") {
		if width := VisualWidth(line); width > 30 {
			t.Errorf("line %q is %d cells, expected ≤ 30", line, width)
		}
	}
}
