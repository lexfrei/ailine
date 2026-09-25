package usage

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lexfrei/ailine/internal/httpclient"
	"github.com/lexfrei/ailine/internal/keychain"
)

const (
	testToken     = "test-token"
	authErrorType = "authentication_error"
	rateLimitType = "rate_limit_error"

	headerRetryAfter            = "Retry-After"
	headerContentType           = "Content-Type"
	headerContentSecurityPolicy = "Content-Security-Policy"
	headerXRobotsTag            = "X-Robots-Tag"

	mimeJSON   = "application/json"
	cspNone    = "default-src 'none'; frame-ancestors 'none'"
	robotsNone = "none"
)

var errTest = errors.New("test error")

func TestParseBody(t *testing.T) {
	t.Parallel()

	resetsAt := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)

	body := []byte(`{
		"five_hour": {"utilization": 30.5, "resets_at": "` + resetsAt + `"},
		"seven_day": {"utilization": 45.2, "resets_at": "` + resetsAt + `"}
	}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.FiveHour == nil {
		t.Fatal("expected FiveHour to be set")
	}

	if data.SevenDay == nil {
		t.Fatal("expected SevenDay to be set")
	}

	if data.Extra != nil {
		t.Error("expected Extra to be nil")
	}

	if int(data.FiveHour.Utilization+0.5) != 31 {
		t.Errorf("FiveHour utilization = %.1f, want ~30.5", data.FiveHour.Utilization)
	}
}

func TestParseBodyPerModelWindows(t *testing.T) {
	t.Parallel()

	resetsAt := time.Now().Add(3 * time.Hour).UTC().Format(time.RFC3339)

	body := []byte(`{
		"seven_day": {"utilization": 45.2, "resets_at": "` + resetsAt + `"},
		"seven_day_opus": {"utilization": 12.5, "resets_at": "` + resetsAt + `"},
		"seven_day_sonnet": {"utilization": 78.9, "resets_at": "` + resetsAt + `"},
		"seven_day_cowork": null,
		"seven_day_oauth_apps": {"utilization": 5.0, "resets_at": "` + resetsAt + `"}
	}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.SevenDayOpus == nil {
		t.Fatal("expected SevenDayOpus to be set")
	}

	if int(data.SevenDayOpus.Utilization+0.5) != 13 {
		t.Errorf("SevenDayOpus utilization = %.1f, want ~12.5", data.SevenDayOpus.Utilization)
	}

	if data.SevenDaySonnet == nil {
		t.Fatal("expected SevenDaySonnet to be set")
	}

	if int(data.SevenDaySonnet.Utilization+0.5) != 79 {
		t.Errorf("SevenDaySonnet utilization = %.1f, want ~78.9", data.SevenDaySonnet.Utilization)
	}

	if data.SevenDayCowork != nil {
		t.Error("expected SevenDayCowork to be nil (null in JSON)")
	}

	if data.SevenDayOAuthApps == nil {
		t.Fatal("expected SevenDayOAuthApps to be set")
	}

	if int(data.SevenDayOAuthApps.Utilization+0.5) != 5 {
		t.Errorf("SevenDayOAuthApps utilization = %.1f, want ~5.0", data.SevenDayOAuthApps.Utilization)
	}
}

func TestParseBodyError(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error": {"type": "authentication_error"}}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.ErrorType != authErrorType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, authErrorType)
	}
}

func TestParseBodyExtra(t *testing.T) {
	t.Parallel()

	resetsAt := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	body := []byte(`{
		"five_hour": {"utilization": 10, "resets_at": "` + resetsAt + `"},
		"extra_usage": {
			"is_enabled": true,
			"monthly_limit": 5000,
			"used_credits": 128
		}
	}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.Extra == nil {
		t.Fatal("expected Extra to be set")
	}

	if data.Extra.MonthlyLimit != 5000 {
		t.Errorf("MonthlyLimit = %.0f, want 5000", data.Extra.MonthlyLimit)
	}

	if data.Extra.UsedCredits != 128 {
		t.Errorf("UsedCredits = %.0f, want 128", data.Extra.UsedCredits)
	}
}

func TestParseBodyExtraDisabled(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"extra_usage": {
			"is_enabled": true,
			"monthly_limit": 5000,
			"used_credits": 0
		}
	}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.Extra != nil {
		t.Error("Extra should be nil when used_credits is 0")
	}
}

func TestParseBodyInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := ParseBody([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseWindowInvalidTimestamp(t *testing.T) {
	t.Parallel()

	win := &apiWindow{Utilization: 50, ResetsAt: "invalid"}
	got := parseWindow(win, 300)

	if got != nil {
		t.Error("expected nil for invalid timestamp")
	}
}

func TestParseWindowPastReset(t *testing.T) {
	t.Parallel()

	past := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	win := &apiWindow{Utilization: 50, ResetsAt: past}
	got := parseWindow(win, 300)

	if got == nil {
		t.Fatal("expected non-nil window")
	}

	if got.RemainingMinutes != 0 {
		t.Errorf("expected 0 remaining, got %d", got.RemainingMinutes)
	}
}

func TestParseRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		header   http.Header
		expected int
	}{
		{
			name:     "valid seconds",
			header:   http.Header{headerRetryAfter: []string{"6"}},
			expected: 6,
		},
		{
			name:     "zero seconds",
			header:   http.Header{headerRetryAfter: []string{"0"}},
			expected: 0,
		},
		{
			name:     "missing header uses default",
			header:   http.Header{},
			expected: defaultRetryAfterSeconds,
		},
		{
			name:     "negative value clamped to zero",
			header:   http.Header{headerRetryAfter: []string{"-5"}},
			expected: 0,
		},
		{
			name:     "non-numeric value uses default",
			header:   http.Header{headerRetryAfter: []string{"Wed, 11 Mar 2026 18:44:10 GMT"}},
			expected: defaultRetryAfterSeconds,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := parseRetryAfterSeconds(testCase.header)
			if got != testCase.expected {
				t.Errorf("parseRetryAfterSeconds() = %d, want %d", got, testCase.expected)
			}
		})
	}
}

func TestRetryAfterActive(t *testing.T) {
	dir := t.TempDir()
	origPath := RetryAfterPath
	RetryAfterPath = filepath.Join(dir, "retry-after")

	defer func() { RetryAfterPath = origPath }()

	// No file — not active.
	if retryAfterActive() {
		t.Error("expected false when no retry-after file")
	}

	// Future deadline — active.
	future := time.Now().UTC().Add(1 * time.Minute).Format(time.RFC3339)

	err := os.WriteFile(RetryAfterPath, []byte(future), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if !retryAfterActive() {
		t.Error("expected true for future deadline")
	}

	// Past deadline — not active.
	past := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339)

	err = os.WriteFile(RetryAfterPath, []byte(past), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if retryAfterActive() {
		t.Error("expected false for past deadline")
	}

	// Corrupted file — not active.
	err = os.WriteFile(RetryAfterPath, []byte("garbage"), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if retryAfterActive() {
		t.Error("expected false for corrupted file")
	}
}

// Tests below mutate package-level vars and cannot run in parallel.

func TestFetchCached(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origToken := keychain.GetFn

	CachePath = filepath.Join(dir, "usage-cache.json")

	defer func() {
		CachePath = origPath
		keychain.GetFn = origToken
	}()

	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	cached := `{"five_hour":{"utilization":25,"resets_at":"` + resetsAt + `"}}`

	err := os.WriteFile(CachePath, []byte(cached), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	keychain.GetFn = func() (string, error) {
		t.Error("should not call GetFn when cache is valid")

		return "", errTest
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.FiveHour == nil {
		t.Error("expected FiveHour from cache")
	}
}

func TestFetchRateLimitedRespectsRetryAfter(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	httpCalls := 0

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		httpCalls++

		return &httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`{"error":{"type":"rate_limit_error"}}`),
			Header:     http.Header{headerRetryAfter: []string{"6"}},
		}, nil
	}

	// First call — hits API, gets 429.
	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != rateLimitType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, rateLimitType)
	}

	if httpCalls != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", httpCalls)
	}

	// Verify retry-after file was written.
	if _, statErr := os.Stat(RetryAfterPath); statErr != nil {
		t.Fatal("expected retry-after file to be written")
	}

	// Second call — retry-after active, no HTTP request.
	data, err = Fetch()
	if err != nil {
		t.Fatalf("second Fetch failed: %v", err)
	}

	if data.ErrorType != rateLimitType {
		t.Errorf("second ErrorType = %q, want %q", data.ErrorType, rateLimitType)
	}

	if httpCalls != 1 {
		t.Errorf("expected no additional HTTP calls, got %d total", httpCalls)
	}
}

func TestFetchRateLimitedNotCachedInMainCache(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`{"error":{"type":"rate_limit_error"}}`),
			Header:     http.Header{headerRetryAfter: []string{"6"}},
		}, nil
	}

	_, _ = Fetch()

	// Main cache should NOT contain the error response.
	if _, statErr := os.Stat(CachePath); statErr == nil {
		t.Error("429 response should not be written to main cache")
	}
}

func TestFetchAuthErrorCachedByToken(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	httpCalls := 0
	currentToken := "expired-token"

	keychain.GetFn = func() (string, error) { return currentToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		httpCalls++

		return &httpclient.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`{"error":{"type":"authentication_error"}}`),
		}, nil
	}

	// First call — hits API, gets 401, stores token hash.
	data, _ := Fetch()
	if data.ErrorType != authErrorType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, authErrorType)
	}

	if httpCalls != 1 {
		t.Fatalf("expected 1 HTTP call, got %d", httpCalls)
	}

	// Second call with same token — skips API.
	data, _ = Fetch()
	if data.ErrorType != authErrorType {
		t.Errorf("second ErrorType = %q, want %q", data.ErrorType, authErrorType)
	}

	if httpCalls != 1 {
		t.Errorf("expected no additional HTTP calls with same token, got %d total", httpCalls)
	}

	// Third call with new token — hits API again.
	currentToken = "fresh-token"

	_, _ = Fetch()

	if httpCalls != 2 {
		t.Errorf("expected new HTTP call after token change, got %d total", httpCalls)
	}
}

func TestHashTokenIrreversible(t *testing.T) {
	t.Parallel()

	token := "sk-ant-test-token-12345"
	hashed := hashToken(token)

	if hashed == token {
		t.Error("hash should not equal the original token")
	}

	if len(hashed) != 64 {
		t.Errorf("expected 64-char SHA-256 hex, got %d chars", len(hashed))
	}

	// Same input produces same hash.
	if hashToken(token) != hashed {
		t.Error("expected deterministic hash")
	}

	// Different input produces different hash.
	if hashToken("other-token") == hashed {
		t.Error("expected different hash for different token")
	}
}

func TestAuthFailExpiresByTTL(t *testing.T) {
	dir := t.TempDir()
	origPath := AuthFailPath
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() { AuthFailPath = origPath }()

	token := "some-token"

	// Write auth-failed file with old mtime (older than authFailTTL).
	writeAuthFailed(token)

	past := time.Now().Add(-authFailTTL - 1*time.Minute)

	err := os.Chtimes(AuthFailPath, past, past)
	if err != nil {
		t.Fatal(err)
	}

	// Should return false — file is too old.
	if authFailedForToken(token) {
		t.Error("expected false after TTL expiry")
	}
}

func TestFetchUnexpectedStatusCode(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       []byte(`server error`),
		}, nil
	}

	_, err := Fetch()
	if err == nil {
		t.Error("expected error for unexpected status code")
	}
}

// Tests below use real API response shapes captured from production.
// They serve as regression fixtures for the Anthropic usage API format.

// Real 429 response from api.anthropic.com/api/oauth/usage.
// Headers: HTTP/2 429, retry-after: 6, content-type: application/json.
const real429Body = `{"error":{"type":"rate_limit_error","message":"Rate limited. Please try again later."}}`

// Real 401 response from api.anthropic.com/api/oauth/usage.
// Headers: HTTP/2 401, x-should-retry: false, content-type: application/json.
const real401Body = `{"type":"error","error":{"type":"authentication_error","message":"OAuth token has expired. Please obtain a new token or refresh your existing token.","details":{"error_visibility":"user_facing","error_code":"token_expired"}},"request_id":"req_011CYwnYqWfPFkeKDkDmyKqs"}`

// Real 200 response from api.anthropic.com/api/oauth/usage.
// Headers: HTTP/2 200, content-type: application/json.
const real200Body = `{"five_hour":{"utilization":0.0,"resets_at":null},"seven_day":{"utilization":99.0,"resets_at":"2026-03-11T19:00:00.536264+00:00"},"seven_day_oauth_apps":null,"seven_day_opus":null,"seven_day_sonnet":{"utilization":2.0,"resets_at":"2026-03-12T11:00:00.536285+00:00"},"seven_day_cowork":null,"iguana_necktie":null,"extra_usage":{"is_enabled":false,"monthly_limit":null,"used_credits":null,"utilization":null}}`

func TestFetchRateLimitedWithNonJSONBody(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(`Rate limited. Please try again later.`),
			Header:     http.Header{headerRetryAfter: []string{"10"}},
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("expected no error for 429, got: %v", err)
	}

	if data.ErrorType != rateLimitType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, rateLimitType)
	}
}

func TestFetchAuthErrorWithNonJSONBody(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(`Unauthorized`),
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("expected no error for 401, got: %v", err)
	}

	if data.ErrorType != authErrorType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, authErrorType)
	}
}

func TestFetchWithReal429Response(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       []byte(real429Body),
			Header: http.Header{
				headerContentType:           []string{mimeJSON},
				headerRetryAfter:            []string{"6"},
				"Cache-Control":             []string{"private, max-age=0, no-store, no-cache, must-revalidate, post-check=0, pre-check=0"},
				"Referrer-Policy":           []string{"same-origin"},
				"X-Frame-Options":           []string{"SAMEORIGIN"},
				headerContentSecurityPolicy: []string{cspNone},
				headerXRobotsTag:            []string{robotsNone},
			},
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != rateLimitType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, rateLimitType)
	}
}

func TestFetchWithReal401Response(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       []byte(real401Body),
			Header: http.Header{
				headerContentType:           []string{mimeJSON},
				"X-Should-Retry":            []string{"false"},
				"Request-Id":                []string{"req_011CYwnYqWfPFkeKDkDmyKqs"},
				headerContentSecurityPolicy: []string{cspNone},
				headerXRobotsTag:            []string{robotsNone},
			},
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != authErrorType {
		t.Errorf("ErrorType = %q, want %q", data.ErrorType, authErrorType)
	}
}

func TestFetchWithReal200Response(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origLastGood := LastGoodCachePath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")
	LastGoodCachePath = filepath.Join(dir, "last-good.json")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		LastGoodCachePath = origLastGood
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(real200Body),
			Header: http.Header{
				headerContentType:           []string{mimeJSON},
				"Request-Id":                []string{"req_011CYwnbPgEDyqe7p3VQohKX"},
				"Anthropic-Organization-Id": []string{"71f33990-619b-49dd-9175-8df9803e8b59"},
				headerContentSecurityPolicy: []string{cspNone},
				headerXRobotsTag:            []string{robotsNone},
			},
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != "" {
		t.Errorf("expected no error, got ErrorType = %q", data.ErrorType)
	}

	if data.SevenDay == nil {
		t.Fatal("expected SevenDay to be set")
	}

	if int(data.SevenDay.Utilization+0.5) != 99 {
		t.Errorf("SevenDay utilization = %.1f, want ~99", data.SevenDay.Utilization)
	}

	// five_hour has resets_at: null — should produce nil window.
	if data.FiveHour != nil {
		t.Error("expected FiveHour to be nil (resets_at is null)")
	}

	// seven_day_sonnet has valid resets_at — should produce non-nil window.
	if data.SevenDaySonnet == nil {
		t.Fatal("expected SevenDaySonnet to be set")
	}

	if int(data.SevenDaySonnet.Utilization+0.5) != 2 {
		t.Errorf("SevenDaySonnet utilization = %.1f, want ~2", data.SevenDaySonnet.Utilization)
	}

	// seven_day_opus is null — should produce nil window.
	if data.SevenDayOpus != nil {
		t.Error("expected SevenDayOpus to be nil (null in JSON)")
	}

	// seven_day_cowork is null — should produce nil window.
	if data.SevenDayCowork != nil {
		t.Error("expected SevenDayCowork to be nil (null in JSON)")
	}

	// seven_day_oauth_apps is null — should produce nil window.
	if data.SevenDayOAuthApps != nil {
		t.Error("expected SevenDayOAuthApps to be nil (null in JSON)")
	}

	// Extra usage is disabled.
	if data.Extra != nil {
		t.Error("expected Extra to be nil (is_enabled: false)")
	}
}

func TestFetchNoToken(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
	}()

	keychain.GetFn = func() (string, error) { return "", errTest }

	_, err := Fetch()
	if err == nil {
		t.Error("expected error when no token")
	}
}

func TestFetchEmptyToken(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
	}()

	keychain.GetFn = func() (string, error) { return "", nil }

	_, err := Fetch()
	if err == nil {
		t.Fatal("expected error when token is empty")
	}

	if !errors.Is(err, keychain.ErrNoToken) {
		t.Errorf("expected ErrNoToken in error chain, got: %v", err)
	}
}

func TestFetchHTTPError(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return nil, errTest
	}

	_, err := Fetch()
	if err == nil {
		t.Error("expected error on HTTP failure")
	}
}

func TestFetchSuccess(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origRetryPath := RetryAfterPath
	origAuthPath := AuthFailPath
	origToken := keychain.GetFn
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "usage-cache.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	defer func() {
		CachePath = origPath
		RetryAfterPath = origRetryPath
		AuthFailPath = origAuthPath
		keychain.GetFn = origToken
		HTTPGetFn = origHTTP
	}()

	resetsAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)

	keychain.GetFn = func() (string, error) { return testToken, nil }
	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"five_hour":{"utilization":30,"resets_at":"` + resetsAt + `"}}`),
		}, nil
	}

	data, err := Fetch()
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.FiveHour == nil {
		t.Error("expected FiveHour to be set")
	}

	if _, statErr := os.Stat(CachePath); statErr != nil {
		t.Error("expected cache file to be written")
	}
}
