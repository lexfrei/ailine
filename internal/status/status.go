// Package status checks the Claude platform status for active incidents.
package status

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/lexfrei/ailine/internal/cache"
	"github.com/lexfrei/ailine/internal/fmtutil"
	"github.com/lexfrei/ailine/internal/httpclient"
)

const (
	apiURL     = "https://status.claude.com/api/v2/status.json"
	apiTimeout = 2 * time.Second
)

// CacheTTL is the cache duration for status data. Configurable at startup.
var CacheTTL = 15 * time.Second

// CachePath is the path to the status cache file. Replaceable for testing.
var CachePath = "/tmp/claude-status-cache.json"

// HTTPGetFn is the function used for HTTP requests. Replaceable for testing.
var HTTPGetFn httpclient.GetFn = httpclient.Get

// Platform status indicators reported by the status API.
const (
	IndicatorMinor    = "minor"
	IndicatorMajor    = "major"
	IndicatorCritical = "critical"
)

var statusMap = map[string]struct{ icon, text string }{
	IndicatorMinor:    {"⚠️", "degraded"},
	IndicatorMajor:    {"🔶", "major outage"},
	IndicatorCritical: {"🔴", "critical outage"},
}

type apiResponse struct {
	Status struct {
		Indicator string `json:"indicator"`
	} `json:"status"`
}

// renderStatus turns a platform status indicator into a themed segment, or ""
// when the indicator names no active incident. Rendering happens at read time
// (not when cached) so a theme change takes effect without waiting out the TTL.
func renderStatus(indicator string) string {
	entry, ok := statusMap[indicator]
	if !ok {
		return ""
	}

	return fmtutil.Part(entry.text, entry.icon)
}

// FetchAlert returns a status string if Claude platform has active incidents.
func FetchAlert() string {
	if cached, ok := cache.Read(CachePath, CacheTTL); ok {
		return renderStatus(string(cached))
	}

	httpResp, err := HTTPGetFn(apiURL, nil, apiTimeout)
	if err != nil {
		cache.Write(CachePath, nil)

		return ""
	}

	if httpResp.StatusCode != http.StatusOK {
		cache.Write(CachePath, nil)

		return ""
	}

	var resp apiResponse

	unmarshalErr := json.Unmarshal(httpResp.Body, &resp)
	if unmarshalErr != nil {
		cache.Write(CachePath, nil)

		return ""
	}

	cache.Write(CachePath, []byte(resp.Status.Indicator))

	return renderStatus(resp.Status.Indicator)
}
