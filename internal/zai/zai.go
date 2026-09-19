// Package zai provides access to the Z.ai (GLM) coding-plan usage API.
package zai

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lexfrei/claudeline/internal/cache"
	"github.com/lexfrei/claudeline/internal/fmtutil"
	"github.com/lexfrei/claudeline/internal/httpclient"
)

// ErrUnexpectedStatus is returned when the usage API reports failure or answers
// with a status other than 200/401/403/429.
var ErrUnexpectedStatus = errors.New("unexpected status from z.ai usage API")

const (
	quotaLimitPath = "/api/monitor/usage/quota/limit"
	apiTimeout     = 3 * time.Second

	retryAfterBuffer         = 5 * time.Second
	defaultRetryAfterSeconds = 30
	authFailTTL              = 1 * time.Hour

	// subDailyHorizon bounds the reset distance of a session-length window,
	// used when a window carries no recognized unit code.
	subDailyHorizon = 24 * time.Hour
)

// Default locations of the on-disk state. Named so tests can assert they were
// redirected: ParseBody writes to LastGoodCachePath on every call, so a suite
// that left these in place would overwrite the running statusline's cache.
const (
	defaultCachePath         = "/tmp/claudeline-zai-usage-cache.json"
	defaultLastGoodCachePath = "/tmp/claudeline-zai-usage-last-good.json"
	defaultRetryAfterPath    = "/tmp/claudeline-zai-usage-retry-after"
	defaultAuthFailPath      = "/tmp/claudeline-zai-usage-auth-failed"
)

// CacheTTL is the cache duration for usage data. Configurable at startup.
var CacheTTL = 10 * time.Minute

// CachePath is the path to the usage cache file. Replaceable for testing.
var CachePath = defaultCachePath

// LastGoodCachePath stores the last successful API response. Replaceable for testing.
var LastGoodCachePath = defaultLastGoodCachePath

// RetryAfterPath stores the retry-after deadline. Replaceable for testing.
var RetryAfterPath = defaultRetryAfterPath

// AuthFailPath stores the key hash of the last authentication failure. Replaceable for testing.
var AuthFailPath = defaultAuthFailPath

// HTTPGetFn is the function used for HTTP requests. Replaceable for testing.
var HTTPGetFn httpclient.GetFn = httpclient.Get

// Limit type values. TOKENS_LIMIT entries carry the plan's percentage quota
// windows; openusage documents CREDIT_LIMIT as a newer spelling of the same
// entry, so both are accepted. TIME_LIMIT entries count monthly tool calls (a
// count, not a percentage window) and are skipped.
const (
	limitTokens = "TOKENS_LIMIT"
	limitCredit = "CREDIT_LIMIT"
)

// Unit codes observed in limits[] entries. The pair (unit=3, number=5) names
// the 5-hour session window; (unit=6, number=1) names the weekly window.
const (
	unitHours = 3
	unitWeeks = 6
)

// Error types reported through fmtutil.Data.ErrorType, mirroring the Anthropic
// vocabulary the rendering layer already understands.
const (
	errTypeRateLimit = "rate_limit_error"
	errTypeAuth      = "authentication_error"
)

// apiResponse mirrors the JSON envelope of the monitor API:
//
//	{"code":200,"msg":"Operation successful","data":{"limits":[…]},"success":true}
type apiResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data *struct {
		Limits []apiLimit `json:"limits"`
	} `json:"data"`
	Success bool `json:"success"`
}

// apiLimit is one entry of the limits[] array. nextResetTime is epoch
// milliseconds; percentage is integer percent (0-100). usage/currentValue/
// remaining are count fields used only by TIME_LIMIT entries.
type apiLimit struct {
	Type          string  `json:"type"`
	Unit          int     `json:"unit"`
	Number        int     `json:"number"`
	Percentage    float64 `json:"percentage"`
	NextResetTime int64   `json:"nextResetTime"`
}

// Fetch retrieves coding-plan quota usage from the Z.ai monitor API (with
// caching). root is the server root (provider.ServerRoot of the configured
// base URL); key the bearer API key.
func Fetch(root, key string) (*fmtutil.Data, error) {
	if cached, ok := cache.Read(CachePath, CacheTTL); ok {
		return ParseBody(cached)
	}

	if retryAfterActive() {
		return &fmtutil.Data{ErrorType: errTypeRateLimit}, nil
	}

	if key == "" {
		return nil, fmt.Errorf("getting api key: %w", errNoAPIKey)
	}

	if authFailedForKey(key) {
		return &fmtutil.Data{ErrorType: errTypeAuth}, nil
	}

	resp, err := HTTPGetFn(root+quotaLimitPath, map[string]string{
		"Authorization": "Bearer " + key,
		"Accept":        "application/json",
	}, apiTimeout)
	if err != nil {
		return nil, fmt.Errorf("fetching usage: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		writeRetryAfter(resp.Header)

		return &fmtutil.Data{ErrorType: errTypeRateLimit}, nil
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		writeAuthFailed(key)

		return &fmtutil.Data{ErrorType: errTypeAuth}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %d", ErrUnexpectedStatus, resp.StatusCode)
	}

	cache.Write(CachePath, resp.Body)

	return ParseBody(resp.Body)
}

// errNoAPIKey is reported when no bearer key is configured.
var errNoAPIKey = errors.New("no api key found")

// retryAfterActive returns true if a retry-after deadline is set and has not passed.
func retryAfterActive() bool {
	data, ok := cache.ReadAny(RetryAfterPath)
	if !ok {
		return false
	}

	deadline, err := time.Parse(time.RFC3339, string(data))
	if err != nil {
		return false
	}

	return time.Now().UTC().Before(deadline)
}

// writeRetryAfter stores a retry-after deadline computed from the Retry-After
// header, falling back to defaultRetryAfterSeconds when it is missing or not an
// integer, and always adding retryAfterBuffer on top.
func writeRetryAfter(header http.Header) {
	seconds := defaultRetryAfterSeconds

	if val := header.Get("Retry-After"); val != "" {
		parsed, parseErr := strconv.Atoi(val)
		if parseErr == nil {
			seconds = max(parsed, 0)
		}
	}

	deadline := time.Now().UTC().Add(time.Duration(seconds)*time.Second + retryAfterBuffer)
	cache.Write(RetryAfterPath, []byte(deadline.Format(time.RFC3339)))
}

// authFailedForKey returns true if the given key received a 401/403 within authFailTTL.
func authFailedForKey(key string) bool {
	data, ok := cache.Read(AuthFailPath, authFailTTL)
	if !ok {
		return false
	}

	return string(data) == hashKey(key)
}

// writeAuthFailed stores the hash of the key that got an auth failure.
func writeAuthFailed(key string) {
	cache.Write(AuthFailPath, []byte(hashKey(key)))
}

// hashKey returns a hex-encoded SHA-256 hash of the key.
func hashKey(key string) string {
	h := sha256.Sum256([]byte(key))

	return hex.EncodeToString(h[:])
}

// ParseBody parses the monitor API response body into quota windows. A
// success:false envelope (the API answers HTTP 200 with code 500 on internal
// misses) is an error, not empty data.
func ParseBody(body []byte) (*fmtutil.Data, error) {
	var resp apiResponse

	unmarshalErr := json.Unmarshal(body, &resp)
	if unmarshalErr != nil {
		return nil, fmt.Errorf("parsing usage response: %w", unmarshalErr)
	}

	if !resp.Success || resp.Data == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnexpectedStatus, resp.Msg)
	}

	cache.Write(LastGoodCachePath, body)

	result := &fmtutil.Data{}

	for i := range resp.Data.Limits {
		applyLimit(result, &resp.Data.Limits[i])
	}

	return result, nil
}

// applyLimit folds one limits[] entry into the result. Recognized TOKENS_LIMIT
// windows land in the matching quota field with the window's full length;
// unknown unit codes fall back to classifying by reset horizon (sub-daily →
// five-hour, longer → seven-day) so a new window shape still renders instead of
// silently disappearing. A field already filled by a recognized code is never
// overwritten by the fallback.
func applyLimit(result *fmtutil.Data, limit *apiLimit) {
	if limit.Type != limitTokens && limit.Type != limitCredit {
		return // TIME_LIMIT counts tool calls, not percentage windows
	}

	win := parseWindow(limit)
	if win == nil {
		return
	}

	switch {
	case limit.Unit == unitHours && limit.Number == 5:
		win.TotalMinutes = fmtutil.FiveHourWindowMinutes
		result.FiveHour = win
	case limit.Unit == unitWeeks && limit.Number == 1:
		win.TotalMinutes = fmtutil.SevenDayWindowMinutes
		result.SevenDay = win
	default:
		// Unknown unit code: classify by reset horizon, and only into the
		// matching bucket — a sub-daily window must never land in SevenDay.
		if time.Duration(win.RemainingMinutes)*time.Minute <= subDailyHorizon {
			if result.FiveHour == nil {
				win.TotalMinutes = fmtutil.FiveHourWindowMinutes
				result.FiveHour = win
			}
		} else if result.SevenDay == nil {
			win.TotalMinutes = fmtutil.SevenDayWindowMinutes
			result.SevenDay = win
		}
	}
}

// parseWindow converts an API limit entry into a QuotaWindow. Entries whose
// reset timestamp is absent or in the past carry no schedule and are skipped.
func parseWindow(limit *apiLimit) *fmtutil.QuotaWindow {
	if limit.NextResetTime <= 0 {
		return nil
	}

	resetsAt := time.UnixMilli(limit.NextResetTime).UTC()

	return &fmtutil.QuotaWindow{
		Utilization:      limit.Percentage,
		ResetsAt:         resetsAt,
		RemainingMinutes: max(int(time.Until(resetsAt).Minutes()), 0),
	}
}

// FetchLastGood returns the last successful usage data (no TTL).
func FetchLastGood() *fmtutil.Data {
	body, ok := cache.ReadAny(LastGoodCachePath)
	if !ok {
		return nil
	}

	data, err := ParseBody(body)
	if err != nil || data.ErrorType != "" {
		return nil
	}

	return data
}
