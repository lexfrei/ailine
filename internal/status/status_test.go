package status

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lexfrei/ailine/internal/fmtutil"
	"github.com/lexfrei/ailine/internal/httpclient"
)

var errTest = errors.New("test error")

// useTextStyle switches the global icon style to text for one test, restoring
// it after. Not parallel-safe: Style is shared process state.
func useTextStyle(t *testing.T) {
	t.Helper()

	prev := fmtutil.Style
	fmtutil.Style = fmtutil.StyleText

	t.Cleanup(func() { fmtutil.Style = prev })
}

func TestFetchAlertCached(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() { CachePath = origPath }()

	// The cache holds the raw indicator; FetchAlert renders it at read time.
	err := os.WriteFile(CachePath, []byte(IndicatorMajor), 0o600)
	if err != nil {
		t.Fatal(err)
	}

	got := FetchAlert()
	if got != "🔶 major outage" {
		t.Errorf("expected cached alert, got %q", got)
	}
}

func TestFetchAlertCachesRawIndicator(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"status":{"indicator":"minor"}}`),
		}, nil
	}

	FetchAlert()

	cached, err := os.ReadFile(CachePath)
	if err != nil {
		t.Fatal(err)
	}

	// Storing the raw indicator (not a rendered string) is what lets a theme
	// switch take effect on the next read instead of lagging the TTL.
	if string(cached) != IndicatorMinor {
		t.Errorf("expected raw indicator cached, got %q", string(cached))
	}
}

func TestFetchAlertTextTheme(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() { CachePath = origPath }()

	useTextStyle(t)

	// In text mode each severity carries its color: critical red, major orange,
	// minor yellow — derived through PartStyled so the test tracks the map.
	cases := map[string]string{
		IndicatorCritical: fmtutil.PartStyled(fmtutil.StyleText, "critical outage", "🔴"),
		IndicatorMajor:    fmtutil.PartStyled(fmtutil.StyleText, "major outage", "🔶"),
		IndicatorMinor:    fmtutil.PartStyled(fmtutil.StyleText, "degraded", "⚠️"),
	}

	for indicator, want := range cases {
		if err := os.WriteFile(CachePath, []byte(indicator), 0o600); err != nil {
			t.Fatal(err)
		}

		if got := FetchAlert(); got != want {
			t.Errorf("text theme %q = %q, want %q", indicator, got, want)
		}
	}
}

func TestFetchAlertFromAPI(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"status":{"indicator":"minor"}}`),
		}, nil
	}

	got := FetchAlert()
	if got != "⚠️ degraded" {
		t.Errorf("expected degraded, got %q", got)
	}
}

func TestFetchAlertNone(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"status":{"indicator":"none"}}`),
		}, nil
	}

	got := FetchAlert()
	if got != "" {
		t.Errorf("expected empty for none indicator, got %q", got)
	}
}

func TestFetchAlertHTTPError(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return nil, errTest
	}

	got := FetchAlert()
	if got != "" {
		t.Errorf("expected empty on HTTP error, got %q", got)
	}
}

func TestFetchAlertBadJSON(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusOK,
			Body:       []byte(`not json`),
		}, nil
	}

	got := FetchAlert()
	if got != "" {
		t.Errorf("expected empty on bad JSON, got %q", got)
	}
}

func TestFetchAlertNonOKStatus(t *testing.T) {
	dir := t.TempDir()
	origPath := CachePath
	origHTTP := HTTPGetFn

	CachePath = filepath.Join(dir, "status-cache.json")

	defer func() {
		CachePath = origPath
		HTTPGetFn = origHTTP
	}()

	HTTPGetFn = func(_ string, _ map[string]string, _ time.Duration) (*httpclient.Response, error) {
		return &httpclient.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       []byte(`server error`),
		}, nil
	}

	got := FetchAlert()
	if got != "" {
		t.Errorf("expected empty on non-OK status, got %q", got)
	}
}
