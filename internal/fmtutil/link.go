package fmtutil

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// OSC 8 hyperlink delimiters. The BEL terminator is used over ST because it is
// what Claude Code emits for its own footer links, and older terminals accept
// it more widely.
const (
	oscLinkPrefix = "\x1b]8;;"
	oscLinkSuffix = "\x07"
)

// Hyperlinks enables OSC 8 wrapping in Link. No sequence can be left hanging
// open: Link opens and closes within one segment, and JoinPipeWrap breaks
// lines only between segments. What remains uncertain is the display side,
// which SupportsHyperlinks answers.
var Hyperlinks = false

// Environment variables consulted by SupportsHyperlinks.
const (
	envTermProgram    = "TERM_PROGRAM"
	envTerm           = "TERM"
	envWTSession      = "WT_SESSION"
	envVTEVersion     = "VTE_VERSION"
	envKonsoleVersion = "KONSOLE_VERSION"
	envDomterm        = "DOMTERM"
	envKittyWindowID  = "KITTY_WINDOW_ID"
	envTmux           = "TMUX"
	envScreen         = "STY"
	envZellij         = "ZELLIJ"
)

// vteHyperlinkVersion is VTE 0.50, the first release to render OSC 8.
const vteHyperlinkVersion = 5000

// konsoleHyperlinkVersion is Konsole 20.04, the first release to render OSC 8.
const konsoleHyperlinkVersion = 200400

// terminalsWithHyperlinks are the TERM_PROGRAM values known to render OSC 8,
// spelled as each terminal sets them and checked against the allowlist at
// https://github.com/spencerbeggs/std-osc8/blob/main/docs/terminals.md.
// Apple_Terminal is deliberately absent: it prints the label without the link.
var terminalsWithHyperlinks = map[string]bool{
	"iTerm.app":    true,
	"WezTerm":      true,
	"mintty":       true,
	"ghostty":      true,
	"vscode":       true,
	"Hyper":        true,
	"rio":          true,
	"Tabby":        true,
	"WarpTerminal": true,
}

// SupportsHyperlinks reports whether a hyperlink written now would reach the
// screen: a terminal known to render OSC 8, with no multiplexer in between.
// Both are recognized by environment variable because there is no query for
// the capability, and statusline output is a pipe rather than a tty.
func SupportsHyperlinks() bool {
	// Under tmux the statusline's own OSC 8 never reaches the terminal, while a
	// bare printf of the same bytes does: Claude Code's renderer drops it
	// (anthropics/claude-code#23438, closed as not planned). screen and zellij
	// are gated on the assumption that they share that rendering path, not on
	// a report of their own. The outer terminal's TERM_PROGRAM survives into
	// the session, so without this the detection would say yes for a link that
	// cannot be clicked.
	if os.Getenv(envTmux) != "" || os.Getenv(envScreen) != "" || os.Getenv(envZellij) != "" {
		return false
	}

	if terminalsWithHyperlinks[os.Getenv(envTermProgram)] {
		return true
	}

	term := os.Getenv(envTerm)
	if term == "xterm-kitty" {
		return true
	}

	// Both ship a -direct truecolor entry beside the plain one, and foot's
	// terminfo name is a build option (-Dterminfo-base-name), commonly
	// foot-extra. Match the family instead of enumerating what a packager
	// might have chosen.
	if strings.HasPrefix(term, "foot") || strings.HasPrefix(term, "alacritty") {
		return true
	}

	if os.Getenv(envKittyWindowID) != "" {
		return true
	}

	if os.Getenv(envWTSession) != "" || os.Getenv(envDomterm) != "" {
		return true
	}

	return atLeast(os.Getenv(envVTEVersion), vteHyperlinkVersion) ||
		atLeast(os.Getenv(envKonsoleVersion), konsoleHyperlinkVersion)
}

func atLeast(raw string, minimum int) bool {
	value, err := strconv.Atoi(strings.TrimSpace(raw))

	return err == nil && value >= minimum
}

// Link wraps text in an OSC 8 hyperlink pointing at url. It returns text
// unchanged when hyperlinks are off or the URL is not safe to emit.
func Link(text, url string) string {
	if !Hyperlinks || !linkableURL(url) {
		return text
	}

	return oscLinkPrefix + url + oscLinkSuffix + text + oscLinkPrefix + oscLinkSuffix
}

// linkableURL reports whether url can be placed inside an OSC 8 sequence. The
// URL reaches us from the harness, which sourced it from a forge API, so it is
// checked rather than trusted: a control byte would let the value close the
// sequence and write its own escapes into the terminal. C1 is rejected along
// with C0, since U+009C and U+009D are ST and OSC in the eight-bit form.
//
// Invalid UTF-8 is rejected wholesale rather than scanned byte by byte: a raw
// 0x9C would otherwise decode to U+FFFD and slip past the C1 test, while
// rejecting the 0x80-0x9F byte range outright would also reject every
// non-ASCII URL, whose continuation bytes live in it.
func linkableURL(url string) bool {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return false
	}

	if !utf8.ValidString(url) {
		return false
	}

	return strings.IndexFunc(url, func(r rune) bool {
		return r < ' ' || r == 0x7f || (r >= 0x80 && r <= 0x9f)
	}) == -1
}
