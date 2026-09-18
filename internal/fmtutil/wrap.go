package fmtutil

import (
	"regexp"
	"strings"

	"github.com/mattn/go-runewidth"
)

// ansiEscape strips the escape sequences segments embed before measuring
// visual width. Without this `\033[32m🧠 50%\033[0m` would over-count. Covers
// SGR colors and OSC 8 hyperlinks under either terminator the sequence allows,
// BEL or ST, so switching Link from one to the other cannot silently skew the
// width. Cursor moves and other CSI escapes are not matched; a segment growing
// one needs this pattern extended.
var ansiEscape = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;[^\x07\x1b]*(?:\x07|\x1b\\\\)")

const (
	segmentSeparator = " | "
	// minWrapWidth is the smallest terminal width at which wrapping kicks in.
	// Below this, the per-line budget cannot meaningfully fit even a single
	// emoji + separator + label, so we degrade to JoinPipe to avoid emitting
	// a near-useless one-segment-per-line output. Callers driving from
	// COLUMNS should additionally subtract a safety margin upstream; this
	// constant only guards the JoinPipeWrap API surface.
	minWrapWidth = 10
)

// VisualWidth returns the visible cell width of s with the escape sequences
// ansiEscape covers stripped (SGR colors and OSC 8 hyperlinks) and emoji
// counted as two cells.
func VisualWidth(s string) int {
	return runewidth.StringWidth(ansiEscape.ReplaceAllString(s, ""))
}

// JoinPipeWrap packs segments separated by " | " into lines no wider than
// maxWidth visual cells. A segment that is itself wider than maxWidth lands
// on its own line (no mid-segment splitting). When maxWidth is too small
// to be meaningful, returns a single-line join (same as JoinPipe).
func JoinPipeWrap(segments []string, maxWidth int) string {
	if maxWidth < minWrapWidth || len(segments) == 0 {
		return JoinPipe(segments)
	}

	sepWidth := VisualWidth(segmentSeparator)

	var (
		lines        []string
		current      strings.Builder
		currentWidth int
	)

	appendInline := func(seg string, segWidth int) {
		if current.Len() > 0 {
			current.WriteString(segmentSeparator)

			currentWidth += sepWidth
		}

		current.WriteString(seg)

		currentWidth += segWidth
	}

	flush := func() {
		if current.Len() == 0 {
			return
		}

		lines = append(lines, current.String())
		currentWidth = 0

		current.Reset()
	}

	for _, seg := range segments {
		segWidth := VisualWidth(seg)

		fits := currentWidth == 0 || currentWidth+sepWidth+segWidth <= maxWidth
		if !fits {
			flush()
		}

		appendInline(seg, segWidth)
	}

	flush()

	return strings.Join(lines, "\n")
}
