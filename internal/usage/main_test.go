package usage

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain redirects every on-disk path of the package into a temp directory for
// the whole suite. ParseBody writes to LastGoodCachePath unconditionally, and the
// default paths are the ones the installed statusline reads — running the tests
// must not overwrite the cache of the statusline on the developer's machine.
// Both test packages (usage and usage_test) link into this binary, so one
// TestMain covers them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "ailine-usage")
	if err != nil {
		panic("creating temp dir for usage tests: " + err.Error())
	}

	CachePath = filepath.Join(dir, "usage-cache.json")
	LastGoodCachePath = filepath.Join(dir, "usage-last-good.json")
	RetryAfterPath = filepath.Join(dir, "usage-retry-after")
	AuthFailPath = filepath.Join(dir, "usage-auth-failed")

	code := m.Run()

	if removeErr := os.RemoveAll(dir); removeErr != nil {
		panic("removing temp dir for usage tests: " + removeErr.Error())
	}

	os.Exit(code)
}

// Guards the suite itself: without the redirect above, every ParseBody call in
// these tests rewrites the real cache in /tmp, leaving the installed statusline
// rendering test fixtures (a 2099 reset date, or no quota segments at all).
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
