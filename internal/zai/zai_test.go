package zai

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lexfrei/claudeline/internal/httpclient"
)

// suitePaths holds the suite-wide redirected paths so per-test cleanups can
// restore them without ever pointing back at the production locations.
var suitePaths struct {
	cache, lastGood, retryAfter, authFail string
}

// TestMain redirects every on-disk path of the package into a temp directory
// for the whole suite, mirroring the usage package: ParseBody writes to
// LastGoodCachePath unconditionally, and the default paths are the ones the
// installed statusline reads.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "claudeline-zai")
	if err != nil {
		panic("creating temp dir for zai tests: " + err.Error())
	}

	CachePath = filepath.Join(dir, "usage-cache.json")
	LastGoodCachePath = filepath.Join(dir, "usage-last-good.json")
	RetryAfterPath = filepath.Join(dir, "usage-retry-after")
	AuthFailPath = filepath.Join(dir, "usage-auth-failed")
	suitePaths.cache, suitePaths.lastGood = CachePath, LastGoodCachePath
	suitePaths.retryAfter, suitePaths.authFail = RetryAfterPath, AuthFailPath

	code := m.Run()

	if removeErr := os.RemoveAll(dir); removeErr != nil {
		panic("removing temp dir for zai tests: " + removeErr.Error())
	}

	os.Exit(code)
}

// Guards the suite itself: without the redirect above, every ParseBody call in
// these tests rewrites the real cache in /tmp, leaving the installed statusline
// rendering test fixtures.
func TestSuiteRedirectsCachePaths(t *testing.T) {
	t.Parallel()

	paths := map[string][2]string{
		"CachePath":         {CachePath, defaultCachePath},
		"LastGoodCachePath": {LastGoodCachePath, defaultLastGoodCachePath},
		"RetryAfterPath":    {RetryAfterPath, defaultRetryAfterPath},
		"AuthFailPath":      {AuthFailPath, defaultAuthFailPath},
	}

	for name, pair := range paths {
		if actual, def := pair[0], pair[1]; actual == def {
			t.Errorf("%s still points at the production path %q; tests would clobber the live cache", name, def)
		}
	}
}

// liveCapture is the monitor API response captured from a real GLM Coding Plan
// account (values unchanged; the endpoint answers GETs without echoing the
// key). It carries all three entry kinds: the monthly TIME_LIMIT tool counter,
// the 5-hour TOKENS_LIMIT window, and the weekly one.
func liveCapture(resetFiveHour, resetWeekly, resetMonthly int64) []byte {
	return []byte(`{
		"code": 200,
		"msg": "Operation successful",
		"data": {
			"limits": [
				{"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 1000, "currentValue": 8,
				 "remaining": 992, "percentage": 1, "nextResetTime": ` + strconv.FormatInt(resetMonthly, 10) + `,
				 "usageDetails": [{"modelCode": "search-prime", "usage": 8}]},
				{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 7, "nextResetTime": ` + strconv.FormatInt(resetFiveHour, 10) + `},
				{"type": "TOKENS_LIMIT", "unit": 6, "number": 1, "percentage": 20, "nextResetTime": ` + strconv.FormatInt(resetWeekly, 10) + `}
			],
			"level": "pro"
		},
		"success": true
	}`)
}

// captureTimes returns reset timestamps the given distances from now, as epoch
// milliseconds.
func captureTimes(t *testing.T, fiveHour, weekly time.Duration) (int64, int64) {
	t.Helper()

	now := time.Now()

	return now.Add(fiveHour).UnixMilli(), now.Add(weekly).UnixMilli()
}

func TestParseBodyLiveCapture(t *testing.T) {
	t.Parallel()

	resetFiveHour, resetWeekly := captureTimes(t, 3*time.Hour, 5*24*time.Hour)

	data, err := ParseBody(liveCapture(resetFiveHour, resetWeekly, time.Now().Add(720*time.Hour).UnixMilli()))
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.FiveHour == nil {
		t.Fatal("expected FiveHour to be set from the unit=3/number=5 window")
	}

	if data.SevenDay == nil {
		t.Fatal("expected SevenDay to be set from the unit=6/number=1 window")
	}

	if got := int(data.FiveHour.Utilization + 0.5); got != 7 {
		t.Errorf("FiveHour utilization = %d, want 7", got)
	}

	if data.FiveHour.TotalMinutes != 300 {
		t.Errorf("FiveHour TotalMinutes = %d, want 300", data.FiveHour.TotalMinutes)
	}

	if got := int(data.SevenDay.Utilization + 0.5); got != 20 {
		t.Errorf("SevenDay utilization = %d, want 20", got)
	}

	if data.SevenDay.TotalMinutes != 10080 {
		t.Errorf("SevenDay TotalMinutes = %d, want 10080", data.SevenDay.TotalMinutes)
	}

	if want := time.UnixMilli(resetFiveHour).UTC(); !data.FiveHour.ResetsAt.Equal(want) {
		t.Errorf("FiveHour ResetsAt = %v, want %v", data.FiveHour.ResetsAt, want)
	}

	if data.ErrorType != "" {
		t.Errorf("ErrorType = %q, want empty", data.ErrorType)
	}
}

func TestParseBodyEnvelopeError(t *testing.T) {
	t.Parallel()

	// The monitor API answers HTTP 200 with code 500 / success:false on
	// internal misses (observed on /api/coding/usage); that is an error.
	data, err := ParseBody([]byte(`{"code":500,"msg":"404 NOT_FOUND","success":false}`))
	if err == nil {
		t.Fatalf("expected error, got data %+v", data)
	}
}

func TestParseBodyUnknownUnitFallsBackToHorizon(t *testing.T) {
	t.Parallel()

	subDaily := time.Now().Add(4 * time.Hour).UnixMilli()
	multiDay := time.Now().Add(6 * 24 * time.Hour).UnixMilli()

	body := []byte(`{"code":200,"msg":"ok","success":true,"data":{"limits":[
		{"type": "TOKENS_LIMIT", "unit": 99, "number": 1, "percentage": 42, "nextResetTime": ` + strconv.FormatInt(subDaily, 10) + `},
		{"type": "CREDIT_LIMIT", "unit": 99, "number": 2, "percentage": 60, "nextResetTime": ` + strconv.FormatInt(multiDay, 10) + `}
	]}}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.FiveHour == nil {
		t.Fatal("expected sub-daily unknown window to land in FiveHour")
	}

	if got := int(data.FiveHour.Utilization + 0.5); got != 42 {
		t.Errorf("FiveHour utilization = %d, want 42", got)
	}

	if data.SevenDay == nil {
		t.Fatal("expected multi-day unknown window to land in SevenDay")
	}

	if got := int(data.SevenDay.Utilization + 0.5); got != 60 {
		t.Errorf("SevenDay utilization = %d, want 60", got)
	}

	if data.SevenDay.TotalMinutes != 10080 {
		t.Errorf("SevenDay TotalMinutes = %d, want 10080", data.SevenDay.TotalMinutes)
	}
}

func TestParseBodySubDailyNeverLandsInSevenDay(t *testing.T) {
	t.Parallel()

	first := time.Now().Add(3 * time.Hour).UnixMilli()
	second := time.Now().Add(4 * time.Hour).UnixMilli()

	body := []byte(`{"code":200,"msg":"ok","success":true,"data":{"limits":[
		{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 7, "nextResetTime": ` + strconv.FormatInt(first, 10) + `},
		{"type": "TOKENS_LIMIT", "unit": 99, "number": 1, "percentage": 42, "nextResetTime": ` + strconv.FormatInt(second, 10) + `}
	]}}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if got := int(data.FiveHour.Utilization + 0.5); got != 7 {
		t.Errorf("recognized window must win FiveHour, got %d", got)
	}

	if data.SevenDay != nil {
		t.Errorf("sub-daily fallback window leaked into SevenDay: %+v", data.SevenDay)
	}
}

func TestParseBodySkipsTimeLimitAndMissingResets(t *testing.T) {
	t.Parallel()

	body := []byte(`{"code":200,"msg":"ok","success":true,"data":{"limits":[
		{"type": "TIME_LIMIT", "unit": 5, "number": 1, "usage": 1000, "percentage": 1},
		{"type": "TOKENS_LIMIT", "unit": 3, "number": 5, "percentage": 7}
	]}}`)

	data, err := ParseBody(body)
	if err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	if data.FiveHour != nil {
		t.Error("entry without reset time must be skipped")
	}

	if data.SevenDay != nil {
		t.Error("TIME_LIMIT entry must not become a quota window")
	}
}

// redirectPaths points the package's on-disk state at a private directory for
// one test. Tests using it must not run in parallel: they mutate the globals
// every other test reads.
func redirectPaths(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	CachePath = filepath.Join(dir, "cache.json")
	LastGoodCachePath = filepath.Join(dir, "last-good.json")
	RetryAfterPath = filepath.Join(dir, "retry-after")
	AuthFailPath = filepath.Join(dir, "auth-failed")

	t.Cleanup(restoreDefaultPaths)
}

func stubHTTP(t *testing.T, fn httpclient.GetFn) {
	t.Helper()

	HTTPGetFn = fn

	t.Cleanup(restoreHTTPGet)
}

func TestFetchCachesResponse(t *testing.T) {
	redirectPaths(t)

	resetFiveHour, resetWeekly := captureTimes(t, 3*time.Hour, 5*24*time.Hour)

	calls := 0

	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		calls++

		return &httpclient.Response{StatusCode: 200, Body: liveCapture(resetFiveHour, resetWeekly, 0)}, nil
	})

	for range 2 {
		data, err := Fetch("https://api.z.ai", "key")
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if data.FiveHour == nil {
			t.Fatal("expected FiveHour from fetch")
		}
	}

	if calls != 1 {
		t.Errorf("HTTP calls = %d, want 1 (second fetch must hit the cache)", calls)
	}
}

func TestFetchAuthFailureIsCached(t *testing.T) {
	redirectPaths(t)

	calls := 0

	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		calls++

		return &httpclient.Response{StatusCode: 401, Body: []byte(`{"code":401,"success":false}`)}, nil
	})

	for range 2 {
		data, err := Fetch("https://api.z.ai", "key")
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}

		if data.ErrorType != errTypeAuth {
			t.Errorf("ErrorType = %q, want authentication_error", data.ErrorType)
		}
	}

	if calls != 1 {
		t.Errorf("HTTP calls = %d, want 1 (auth failure must be cached for the TTL)", calls)
	}
}

func TestFetchRateLimited(t *testing.T) {
	redirectPaths(t)

	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: 429,
			Header:     map[string][]string{"Retry-After": {"60"}},
		}, nil
	})

	data, err := Fetch("https://api.z.ai", "key")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != errTypeRateLimit {
		t.Errorf("ErrorType = %q, want rate_limit_error", data.ErrorType)
	}

	// The retry-after deadline must gate the next call without hitting the API.
	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		t.Error("fetch during retry-after window must not reach the API")

		return nil, nil //nolint:nilnil // unreachable
	})

	data, err = Fetch("https://api.z.ai", "key")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if data.ErrorType != errTypeRateLimit {
		t.Errorf("ErrorType = %q, want rate_limit_error", data.ErrorType)
	}
}

func TestFetchRequestShape(t *testing.T) {
	redirectPaths(t)

	var gotURL, gotAuth string

	stubHTTP(t, func(url string, headers map[string]string, _ time.Duration) (*httpclient.Response, error) {
		gotURL = url
		gotAuth = headers["Authorization"]

		return &httpclient.Response{StatusCode: 200, Body: liveCapture(0, 0, 0)}, nil
	})

	if _, err := Fetch("https://api.z.ai", "test-key"); err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}

	if want := "https://api.z.ai" + quotaLimitPath; gotURL != want {
		t.Errorf("request URL = %q, want %q", gotURL, want)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-key")
	}
}

func TestFetchNoAPIKey(t *testing.T) {
	redirectPaths(t)

	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		t.Error("fetch without a key must not reach the API")

		return nil, nil //nolint:nilnil // unreachable
	})

	if _, err := Fetch("https://api.z.ai", ""); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestFetchLastGood(t *testing.T) {
	redirectPaths(t)

	if data := FetchLastGood(); data != nil {
		t.Fatalf("FetchLastGood on missing cache = %+v, want nil", data)
	}

	resetFiveHour, resetWeekly := captureTimes(t, 3*time.Hour, 5*24*time.Hour)

	if _, err := ParseBody(liveCapture(resetFiveHour, resetWeekly, 0)); err != nil {
		t.Fatalf("ParseBody failed: %v", err)
	}

	data := FetchLastGood()
	if data == nil {
		t.Fatal("expected last-good data after a successful parse")
	}

	if data.FiveHour == nil || data.SevenDay == nil {
		t.Error("last-good data lost its windows")
	}
}

func TestFetchUnexpectedStatus(t *testing.T) {
	redirectPaths(t)

	stubHTTP(t, func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{StatusCode: 500, Body: []byte("boom")}, nil
	})

	if _, err := Fetch("https://api.z.ai", "key"); err == nil {
		t.Error("expected error for HTTP 500")
	}
}

// restoreDefaultPaths restores the suite-wide redirected paths (never the
// production ones) after a test pointed them at its own directory.
func restoreDefaultPaths() {
	CachePath = suitePaths.cache
	LastGoodCachePath = suitePaths.lastGood
	RetryAfterPath = suitePaths.retryAfter
	AuthFailPath = suitePaths.authFail
}

func restoreHTTPGet() {
	HTTPGetFn = httpclient.Get
}
