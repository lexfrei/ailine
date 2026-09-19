// Package provider detects which API provider Claude Code talks to, so
// statusline segments can query the matching usage and status APIs.
package provider

import (
	"net/url"
	"os"
	"strings"
)

// Provider names an API provider serving the Anthropic-compatible endpoint
// Claude Code is configured with.
type Provider int

const (
	// Anthropic is the default: api.anthropic.com or an unspecified base URL.
	Anthropic Provider = iota
	// Zai is Z.ai's GLM endpoint (with its Zhipu bigmodel.cn mirrors), which
	// serves the Anthropic-compatible API Claude Code can run on.
	Zai
)

// String renders the provider name for logs and diagnostics.
func (p Provider) String() string {
	switch p {
	case Anthropic:
		return nameAnthropic
	case Zai:
		return nameZai
	default:
		return nameAnthropic
	}
}

// Provider names as rendered by String.
const (
	nameAnthropic = "anthropic"
	nameZai       = "zai"
)

// zaiHosts are the servers hosting Z.ai's Anthropic-compatible API. The
// bigmodel.cn mirrors share the account system and the monitor API shape, so
// they are one provider from the statusline's point of view.
func isZaiHost(host string) bool {
	switch host {
	case "api.z.ai", "open.bigmodel.cn", "dev.bigmodel.cn":
		return true
	default:
		return strings.HasSuffix(host, ".bigmodel.cn")
	}
}

// Detect maps an ANTHROPIC_BASE_URL value onto a provider. An empty URL means
// the default Anthropic endpoint. Anything else is Anthropic too: unknown
// gateways keep the historical behavior rather than guessing a quota source.
func Detect(baseURL string) Provider {
	if baseURL == "" {
		return Anthropic
	}

	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return Anthropic
	}

	if isZaiHost(parsed.Hostname()) {
		return Zai
	}

	return Anthropic
}

// apiPathSuffixes are the Anthropic-compatible base paths Claude Code is
// configured with on Z.ai. The provider's own APIs (usage, monitoring) live on
// the server root instead, so the suffix has to come off.
var apiPathSuffixes = []string{
	"/api/anthropic",
	"/api/coding/paas/v4",
	"/api/paas/v4",
}

// ServerRoot strips the known API path suffixes from a base URL, leaving the
// root the provider's own APIs are served under:
// "https://api.z.ai/api/anthropic" becomes "https://api.z.ai". A URL without a
// known suffix is trimmed of its trailing slash and returned as is.
func ServerRoot(baseURL string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/")

	for _, suffix := range apiPathSuffixes {
		if root, ok := strings.CutSuffix(trimmed, suffix); ok {
			return root
		}
	}

	return trimmed
}

// APIKey returns the bearer key for the provider's own APIs, reading the same
// variables Claude Code's configuration exposes to the statusline process.
// ANTHROPIC_AUTH_TOKEN is what a Z.ai setup carries; the ZAI_API_KEY and
// GLM_API_KEY spellings are accepted as fallbacks for setups that export them.
func APIKey() string {
	for _, name := range []string{"ANTHROPIC_AUTH_TOKEN", "ZAI_API_KEY", "GLM_API_KEY"} {
		if val := os.Getenv(name); val != "" {
			return val
		}
	}

	return ""
}
