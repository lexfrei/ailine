package fmtutil

import (
	"strings"
	"testing"
)

// termIterm2 is the TERM_PROGRAM value of a terminal that does render OSC 8,
// used wherever a case needs the detection to say yes on its own.
const termIterm2 = "iTerm.app"

// withHyperlinks turns hyperlink rendering on for one test. Hyperlinks is a
// package global, so the tests that touch it cannot run in parallel.
func withHyperlinks(t *testing.T) {
	t.Helper()

	prev := Hyperlinks
	Hyperlinks = true

	t.Cleanup(func() { Hyperlinks = prev })
}

func TestLinkOffLeavesTextAlone(t *testing.T) {
	got := Link("#42", "https://github.com/o/r/pull/42")
	if got != "#42" {
		t.Errorf("Link with hyperlinks off = %q, want %q", got, "#42")
	}
}

func TestLinkWrapsInOSC8(t *testing.T) {
	withHyperlinks(t)

	got := Link("#42", "https://github.com/o/r/pull/42")
	want := "\x1b]8;;https://github.com/o/r/pull/42\x07#42\x1b]8;;\x07"

	if got != want {
		t.Errorf("Link = %q, want %q", got, want)
	}
}

func TestLinkRejectsUnusableURLs(t *testing.T) {
	withHyperlinks(t)

	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "github.com/o/r/pull/42"},
		{"non-http scheme", "file:///etc/passwd"},
		{"script scheme", "javascript:alert(1)"},
		{"embedded escape", "https://example.com/\x1b]0;pwned\x07"},
		{"embedded newline", "https://example.com/\npwned"},
		{"embedded bell", "https://example.com/\x07"},
		{"eight-bit string terminator", "https://example.com/\u009cpwned"},
		{"eight-bit osc", "https://example.com/\u009d0;pwned"},
		{"raw c1 byte, invalid utf-8", "https://example.com/\x9cpwned"},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			got := Link("#42", tcase.url)
			if got != "#42" {
				t.Errorf("Link(%q) = %q, want the bare text", tcase.url, got)
			}
		})
	}
}

// A hyperlink is decoration around the same glyphs, so it must not change how
// wide the segment measures. Without this the wrapper would break lines early.
func TestLinkKeepsVisualWidth(t *testing.T) {
	withHyperlinks(t)

	plain := "🐙 lexfrei/claudeline"
	linked := Link(plain, "https://github.com/lexfrei/claudeline")

	if got, want := VisualWidth(linked), VisualWidth(plain); got != want {
		t.Errorf("VisualWidth(linked) = %d, want %d", got, want)
	}
}

func TestLinkNestsInsideColor(t *testing.T) {
	withHyperlinks(t)

	linked := Link("\x1b[31m#42\x1b[0m", "https://example.com/pull/42")
	if !strings.Contains(linked, "\x1b[31m#42\x1b[0m") {
		t.Errorf("Link dropped the color codes: %q", linked)
	}

	if got, want := VisualWidth(linked), 3; got != want {
		t.Errorf("VisualWidth = %d, want %d", got, want)
	}
}

func TestSupportsHyperlinksByEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"iterm2", map[string]string{envTermProgram: termIterm2}, true},
		{"wezterm", map[string]string{envTermProgram: "WezTerm"}, true},
		{"ghostty", map[string]string{envTermProgram: "ghostty"}, true},
		{"vscode", map[string]string{envTermProgram: "vscode"}, true},
		{"kitty", map[string]string{envTerm: "xterm-kitty"}, true},
		{"kitty with a rewritten TERM", map[string]string{envKittyWindowID: "1"}, true},
		{"mintty", map[string]string{envTermProgram: "mintty"}, true},
		{"foot", map[string]string{envTerm: "foot"}, true},
		{"foot truecolor", map[string]string{envTerm: "foot-direct"}, true},
		{"foot packaged under another terminfo name", map[string]string{envTerm: "foot-extra"}, true},
		{"foot-extra truecolor", map[string]string{envTerm: "foot-extra-direct"}, true},
		{"alacritty", map[string]string{envTerm: "alacritty"}, true},
		{"alacritty truecolor", map[string]string{envTerm: "alacritty-direct"}, true},
		{"windows terminal", map[string]string{envWTSession: "abc"}, true},
		{"domterm", map[string]string{envDomterm: "1"}, true},
		{"vte 0.50", map[string]string{envVTEVersion: "5002"}, true},
		{"vte too old", map[string]string{envVTEVersion: "4600"}, false},
		{"konsole 20.04", map[string]string{envKonsoleVersion: "200400"}, true},
		{"konsole too old", map[string]string{envKonsoleVersion: "180800"}, false},
		{"apple terminal", map[string]string{envTermProgram: "Apple_Terminal"}, false},
		{"iterm2 inside tmux", map[string]string{envTermProgram: termIterm2, envTmux: "/tmp/tmux-501/default,1,0"}, false},
		{"iterm2 inside screen", map[string]string{envTermProgram: termIterm2, envScreen: "1234.pts-0.host"}, false},
		{"iterm2 inside zellij", map[string]string{envTermProgram: termIterm2, envZellij: "0"}, false},
		{"unknown", map[string]string{envTerm: "xterm-256color"}, false},
		{"nothing set", map[string]string{}, false},
	}

	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			for _, key := range []string{envTermProgram, envTerm, envWTSession, envVTEVersion, envKonsoleVersion, envDomterm, envKittyWindowID, envTmux, envScreen, envZellij} {
				t.Setenv(key, "")
			}

			for key, value := range tcase.env {
				t.Setenv(key, value)
			}

			if got := SupportsHyperlinks(); got != tcase.want {
				t.Errorf("SupportsHyperlinks() = %v, want %v", got, tcase.want)
			}
		})
	}
}
