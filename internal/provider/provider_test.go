package provider

import (
	"testing"
)

// testZaiRoot is the Z.ai server root repeated across the table.
const testZaiRoot = "https://api.z.ai"

func TestDetect(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want Provider
	}{
		{"empty url", "", Anthropic},
		{"default anthropic", "https://api.anthropic.com", Anthropic},
		{"zai global", "https://api.z.ai/api/anthropic", Zai},
		{"zai bare host", testZaiRoot, Zai},
		{"zai coding path", "https://api.z.ai/api/coding/paas/v4", Zai},
		{"zhipu open mirror", "https://open.bigmodel.cn/api/anthropic", Zai},
		{"zhipu dev mirror", "https://dev.bigmodel.cn/api/coding/paas/v4", Zai},
		{"zhipu subdomain mirror", "https://cn.bigmodel.cn/api/anthropic", Zai},
		{"local gateway", "http://localhost:8080", Anthropic},
		{"unparseable", "://not a url", Anthropic},
		{"whitespace", "  https://api.z.ai/api/anthropic  ", Zai},
		{"zai in path only", "https://proxy.example.com/api.z.ai", Anthropic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := Detect(tt.url); got != tt.want {
				t.Errorf("Detect(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestServerRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{"anthropic suffix", "https://api.z.ai/api/anthropic", testZaiRoot},
		{"coding suffix", "https://api.z.ai/api/coding/paas/v4", testZaiRoot},
		{"paas suffix", "https://open.bigmodel.cn/api/paas/v4", "https://open.bigmodel.cn"},
		{"trailing slash", "https://api.z.ai/", testZaiRoot},
		{"no suffix", testZaiRoot, testZaiRoot},
		{"empty", "", ""},
		{"unknown path kept", "https://gw.example.com/v1", "https://gw.example.com/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ServerRoot(tt.url); got != tt.want {
				t.Errorf("ServerRoot(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestProviderString(t *testing.T) {
	t.Parallel()

	if Anthropic.String() != "anthropic" {
		t.Errorf("Anthropic.String() = %q", Anthropic.String())
	}

	if Zai.String() != "zai" {
		t.Errorf("Zai.String() = %q", Zai.String())
	}
}
