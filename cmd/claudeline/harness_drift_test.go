package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// The statusline stdin payload is not documented anywhere; the installed Claude
// Code binary is the only source of truth. These tests extract the payload
// object literal from that binary and compare its field names against the
// pinned set, so a harness update that adds, renames, or drops a field turns
// into a test failure instead of a silently ignored field.
//
// The extraction anchors on field-name strings (stable across builds), not on
// minified variable names (not stable).

// harnessVersionsDir is where the native Claude Code installer keeps versioned
// binaries. Absent on CI, so the drift test skips there and runs on the
// machines that actually have a harness to drift against.
const harnessVersionsDir = ".local/share/claude/versions"

// payloadMarker sits inside the rate_limits assignment directly preceding the
// statusline payload literal. Both windows of interest surround it.
const payloadMarker = "five_hour:{used_percentage"

var errPayloadNotFound = errors.New("statusline payload literal not found in harness binary")

// Field names of the base payload, named because they appear both in the
// pinned schema and in the expectations of the extraction tests.
const (
	fieldSessionID      = "session_id"
	fieldTranscriptPath = "transcript_path"
)

// knownHarnessPayloadFields pins the snake_case field names of the statusline
// stdin payload as of Claude Code 2.1.270. On mismatch, re-verify the schema
// against cmd/claudeline/main.go's stdinData (new fields may deserve a
// segment; renames need a parser change), then update this list.
func knownHarnessPayloadFields() []string {
	return []string{
		"added_dirs",
		"agent",
		"agent_id",
		"agent_type",
		"branch",
		"cache_write_tokens",
		"caching_observed",
		"causes",
		"context_window",
		"cost",
		"current_dir",
		"cwd",
		"display_name",
		"effort",
		"enabled",
		"exceeds_200k_tokens",
		"expected_rebuilds",
		"expires_at",
		"fast_mode",
		"five_hour",
		"git_worktree",
		"hit_ratio",
		"id",
		"kind",
		"last_miss_at",
		"last_miss_cause",
		"level",
		"miss_causes",
		"miss_recache_tokens",
		"misses",
		"mode",
		"model",
		"name",
		"number",
		"original_branch",
		"original_cwd",
		"output_style",
		"path",
		"permission_mode",
		"pr",
		"project_dir",
		"prompt_cache",
		"prompt_id",
		"rate_limits",
		"recache_tokens_if_cold",
		"remote",
		"repo",
		"requests",
		"resets_at",
		"review_state",
		"scratchpad_dir",
		"session_id",
		"session_name",
		"seven_day",
		"spend_limit",
		"system_char_delta",
		"thinking",
		"tools_added",
		"tools_removed",
		"total_api_duration_ms",
		"total_cost_usd",
		"total_duration_ms",
		"total_lines_added",
		"total_lines_removed",
		"transcript_path",
		"ttl",
		"url",
		"used_percentage",
		"version",
		"vim",
		"warm",
		"workspace",
		"worktree",
	}
}

var (
	quotedStringRE = regexp.MustCompile(`"[^"]*"`)
	// A payload field is a snake_case key in a minified object literal:
	// preceded by an object/argument delimiter, followed by a colon. camelCase
	// identifiers (internal JS, not payload) do not match.
	payloadFieldRE = regexp.MustCompile(`[{,(&]([a-z_][a-z0-9_]*):`)
)

// harnessPayloadFields extracts the sorted, deduplicated field names of the
// statusline stdin payload from a Claude Code binary. Fields come from two
// places: the payload object literal, and the helpers whose results are spread
// into it — the base payload shared with hooks, and the prompt-cache
// statistics.
//
// Known blind spots, by name: the sub-fields of repo (host, owner, name) and
// context_window (used_percentage) arrive by reference and stay invisible to
// this extraction — the pinned occurrences of used_percentage and name come
// from other inline literals, not from these objects. A helper the minifier
// renames, or one whose result stops being spread directly, drops its fields
// here and surfaces as a missing-field failure rather than as silence.
func harnessPayloadFields(bin []byte) ([]string, error) {
	window, err := payloadWindow(bin)
	if err != nil {
		return nil, err
	}

	fields := fieldsInWindow(window)

	for _, name := range spreadHelpers(window) {
		for _, field := range helperFields(bin, name) {
			if !slices.Contains(fields, field) {
				fields = append(fields, field)
			}
		}
	}

	slices.Sort(fields)

	return fields, nil
}

// returnLiteral opens both the payload literal and every helper's own literal.
const returnLiteral = "return{"

// markerToReturnSpan bounds the distance from payloadMarker to the return that
// opens the payload literal, so an unrelated later return cannot be mistaken
// for it.
const markerToReturnSpan = 800

// markerPreWindow keeps the delimiter right before payloadMarker in view
// without reaching back into the enclosing function's signature, whose
// destructured parameters would read as payload fields.
const markerPreWindow = 10

// payloadWindow returns the span carrying the statusline payload fields: the
// tail of the rate_limits assignment that payloadMarker sits in — where
// five_hour, seven_day and spend_limit are built — through the payload object
// literal that follows it, closing brace included.
//
// Only the end is found by matching braces, and that is the point: the
// byte-counted window this replaces budgeted 1400 bytes from the "r" of
// "return", leaving the literal 1394 against its 1382 in 2.1.270 — twelve bytes
// of slack, close enough that a field appended to the literal would have been
// dropped without a word.
func payloadWindow(bin []byte) ([]byte, error) {
	markerAt := bytes.Index(bin, []byte(payloadMarker))
	if markerAt < 0 {
		return nil, errPayloadNotFound
	}

	tail := bin[markerAt:min(markerAt+markerToReturnSpan, len(bin))]

	returnAt := bytes.Index(tail, []byte(returnLiteral))
	if returnAt < 0 {
		return nil, errPayloadNotFound
	}

	start := markerAt + returnAt + len(returnLiteral) - 1

	end := matchDelim(bin[start:], '{', '}')
	if end < 0 {
		return nil, errPayloadNotFound
	}

	return bin[max(markerAt-markerPreWindow, 0) : start+end+1], nil
}

// spreadHelperRE matches the opening of a spread call: "...oRs(" or "...Na(".
var spreadHelperRE = regexp.MustCompile(`\.\.\.([A-Za-z_$][A-Za-z0-9_$]*)\(`)

// spreadHelpers returns the names of the helpers whose results are spread into
// the payload. A call followed by an operator — "...Zh(m)&&{...}",
// "...Zn()!==null&&{...}" — guards an inline object instead of supplying
// fields, and its body has nothing to do with the payload, so a call counts
// only when it ends the spread: the next character is "," or the literal's "}".
func spreadHelpers(window []byte) []string {
	var names []string

	for _, match := range spreadHelperRE.FindAllSubmatchIndex(window, -1) {
		parenAt := match[1] - 1

		closeParen := matchDelim(window[parenAt:], '(', ')')
		if closeParen < 0 {
			continue
		}

		rest := window[parenAt+closeParen+1:]
		if len(rest) == 0 || (rest[0] != ',' && rest[0] != '}') {
			continue
		}

		if name := string(window[match[2]:match[3]]); !slices.Contains(names, name) {
			names = append(names, name)
		}
	}

	return names
}

// helperFields returns the payload fields a spread helper contributes. The
// minifier reuses short names across modules — 2.1.270 defines fifteen
// functions called Na — so definitions are selected by shape instead: a body
// returning an object literal that carries at least one snake_case key. Every
// match is merged, which keeps a wrong match loud (its field names show up in
// the drift diff) instead of silently replacing the right one.
func helperFields(bin []byte, name string) []string {
	needle := []byte("function " + name + "(")

	var fields []string

	for from := 0; ; {
		rel := bytes.Index(bin[from:], needle)
		if rel < 0 {
			return fields
		}

		at := from + rel
		from = at + len(needle)

		body := helperBody(bin, at+len(needle)-1)
		if body == nil || !bytes.Contains(body, []byte(returnLiteral)) {
			continue
		}

		found := fieldsInWindow(body)
		if !slices.ContainsFunc(found, func(f string) bool { return strings.Contains(f, "_") }) {
			continue
		}

		for _, field := range found {
			if !slices.Contains(fields, field) {
				fields = append(fields, field)
			}
		}
	}
}

// helperBodyLookahead covers the gap between a parameter list and the body it
// belongs to, which minified output leaves empty.
const helperBodyLookahead = 8

// helperBody returns a function body, braces included, given the offset of the
// "(" opening its parameter list. The list is skipped by matching parentheses
// so a destructured parameter cannot be taken for the body.
func helperBody(bin []byte, parenAt int) []byte {
	closeParen := matchDelim(bin[parenAt:], '(', ')')
	if closeParen < 0 {
		return nil
	}

	rest := bin[parenAt+closeParen+1:]

	open := bytes.IndexByte(rest[:min(len(rest), helperBodyLookahead)], '{')
	if open < 0 {
		return nil
	}

	end := matchDelim(rest[open:], '{', '}')
	if end < 0 {
		return nil
	}

	return rest[open : open+end+1]
}

// matchDelim returns the index, relative to s, of the delimiter closing s[0],
// or -1 when the block is unterminated. Quoted spans are skipped so a brace
// inside a string cannot end the block early.
//
// Known gap: regex literals are not recognized, so a "{" inside a /.../ would
// end the scan early. No span scanned here carries one; were that to change,
// the field list comes out wrong and the pinned-schema comparison says so.
func matchDelim(span []byte, open, closing byte) int {
	if len(span) == 0 || span[0] != open {
		return -1
	}

	depth := 0

	for i := 0; i < len(span); i++ {
		switch c := span[i]; c {
		case '"', '\'', '`':
			skip := skipQuoted(span[i:], c)
			if skip == 0 {
				return -1
			}

			i += skip
		case open:
			depth++
		case closing:
			depth--

			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

// skipQuoted returns the offset of the quote closing the span at s[0], or 0
// when it is unterminated.
func skipQuoted(s []byte, quote byte) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case quote:
			return i
		}
	}

	return 0
}

// fieldsInWindow returns the deduplicated payload field names in a window.
func fieldsInWindow(window []byte) []string {
	// Quoted values (URLs, version strings) contain colons that would read as
	// keys; blank them out before matching.
	window = quotedStringRE.ReplaceAll(window, nil)

	var fields []string

	for _, match := range payloadFieldRE.FindAllSubmatch(window, -1) {
		field := string(match[1])
		if !slices.Contains(fields, field) {
			fields = append(fields, field)
		}
	}

	return fields
}

// installedHarnessBinary returns the newest Claude Code binary on this machine,
// or "" when none is installed (CI).
func installedHarnessBinary(t *testing.T) string {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return newestBinaryIn(filepath.Join(home, harnessVersionsDir))
}

// newestBinaryIn picks the newest non-empty file in a directory, or "" when
// there is none. Empty entries are download stubs the auto-updater leaves for
// the version it is fetching; reading one would misreport a failed download as
// a restructured payload.
func newestBinaryIn(dir string) string {
	entries, err := filepath.Glob(filepath.Join(dir, "*"))
	if err != nil {
		return ""
	}

	newest := ""

	var newestMod int64

	for _, path := range entries {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			continue
		}

		if mod := info.ModTime().UnixNano(); newest == "" || mod > newestMod {
			newest, newestMod = path, mod
		}
	}

	return newest
}

// The auto-updater leaves a zero-byte stub for the version it is downloading;
// by mtime that stub is the newest entry, and reading it would misreport a
// failed download as a restructured payload.
func TestNewestBinaryInSkipsEmptyStubs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	older := filepath.Join(dir, "2.1.226")
	if err := os.WriteFile(older, []byte("binary"), 0o600); err != nil {
		t.Fatalf("writing binary fixture: %v", err)
	}

	stub := filepath.Join(dir, "2.1.227")
	if err := os.WriteFile(stub, nil, 0o600); err != nil {
		t.Fatalf("writing stub fixture: %v", err)
	}

	// Make the stub unambiguously the newest entry, as it is during a download.
	if err := os.Chtimes(stub, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("bumping stub mtime: %v", err)
	}

	if got := newestBinaryIn(dir); got != older {
		t.Errorf("newestBinaryIn() = %q, want the non-empty %q", got, older)
	}
}

func TestNewestBinaryInEmptyDir(t *testing.T) {
	t.Parallel()

	if got := newestBinaryIn(t.TempDir()); got != "" {
		t.Errorf("expected no binary in an empty dir, got %q", got)
	}
}

func TestHarnessStdinSchemaDrift(t *testing.T) {
	t.Parallel()

	binPath := installedHarnessBinary(t)
	if binPath == "" {
		t.Skip("no installed Claude Code binary to check against")
	}

	bin, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("reading harness binary: %v", err)
	}

	fields, err := harnessPayloadFields(bin)
	if err != nil {
		t.Fatalf("%v in %s — the payload was restructured; re-verify the schema against stdinData and update this test", err, binPath)
	}

	known := knownHarnessPayloadFields()

	if !slices.Equal(fields, known) {
		var news, gone []string

		for _, f := range fields {
			if !slices.Contains(known, f) {
				news = append(news, f)
			}
		}

		for _, f := range known {
			if !slices.Contains(fields, f) {
				gone = append(gone, f)
			}
		}

		t.Errorf("statusline payload of %s drifted from the pinned schema:\n  new fields: %v\n  missing fields: %v\n"+
			"Check whether stdinData should parse the new fields, then update knownHarnessPayloadFields.",
			binPath, news, gone)
	}
}

// The extractor itself is pinned against a synthetic minified payload, so a
// silent extraction regression cannot masquerade as "no drift". The base
// payload arrives via spread (...F()), so its fields must come from the second
// window — the main literal alone cannot see them.
func TestHarnessPayloadFieldsExtraction(t *testing.T) {
	t.Parallel()

	blob := []byte(`function F(){return{session_id:n,transcript_path:uL(n),effort:a}}` +
		`function G(o){agent_transcript_path:Pk(o)};` +
		`X={...C.five_hour&&{five_hour:{used_percentage:C.five_hour.utilization*100,resets_at:C.five_hour.resets_at}}};` +
		`return{...F(),cwd:d,model:{id:g,display_name:W(g)},version:{URL:"https://example.com/x"}.V,...(X.five_hour)&&{rate_limits:X},camelCaseKey:1}`)

	fields, err := harnessPayloadFields(blob)
	if err != nil {
		t.Fatalf("harnessPayloadFields() error = %v", err)
	}

	want := []string{
		"cwd", "display_name", "effort", "five_hour", "id", "model",
		"rate_limits", "resets_at", fieldSessionID, fieldTranscriptPath, "used_percentage", "version",
	}
	if !slices.Equal(fields, want) {
		t.Errorf("fields = %v, want %v", fields, want)
	}
}

func TestHarnessPayloadFieldsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		blob string
	}{
		{"no marker at all", "no payload here"},
		{"marker without a following return", `A={five_hour:{used_percentage:1}}; nothing else`},
		{"literal never closes", `A={five_hour:{used_percentage:1}};return{cwd:d,workspace:{repo:{`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := harnessPayloadFields([]byte(tt.blob)); !errors.Is(err, errPayloadNotFound) {
				t.Errorf("expected errPayloadNotFound, got %v", err)
			}
		})
	}
}

// The payload literal grows with every harness release: by 2.1.270 it was 1382
// bytes against the extractor's 1400-byte window, and a field appended at its
// end would have been dropped without a word. Extraction must follow the
// literal's braces, not a byte count.
func TestHarnessPayloadFieldsReadsPastFixedWindow(t *testing.T) {
	t.Parallel()

	blob := []byte(`function F(){return{session_id:n,transcript_path:uL(n),effort:a}}` +
		`X={...C.five_hour&&{five_hour:{used_percentage:C.five_hour.utilization*100,resets_at:C.five_hour.resets_at}}};` +
		`return{...F(),cwd:d,` + strings.Repeat(`filler:{a:1},`, 120) + `trailing_field:z}`)

	fields, err := harnessPayloadFields(blob)
	if err != nil {
		t.Fatalf("harnessPayloadFields() error = %v", err)
	}

	if !slices.Contains(fields, "trailing_field") {
		t.Errorf("field past the old fixed window was dropped; got %v", fields)
	}
}

// Telling a helper supplying fields from a predicate guarding an inline object
// is what keeps unrelated functions out of the extraction: the payload spreads
// ...Zn()!==null&&{remote:…}, and other functions the minifier also named Zn
// return payload-shaped objects of their own, whose fields would be pulled in.
func TestSpreadHelpersIgnoresGuardCalls(t *testing.T) {
	t.Parallel()

	window := []byte(`{...Base(e,U),cwd:d,...Cache(),...Guard(m)&&{effort:{level:x}},` +
		`...Other()!==null&&{remote:{session_id:e.id}},...P&&{repo:P}}`)

	got := spreadHelpers(window)

	want := []string{"Base", "Cache"}
	if !slices.Equal(got, want) {
		t.Errorf("spreadHelpers() = %v, want %v", got, want)
	}
}

// The minifier reuses short names across modules, so a helper is chosen by the
// shape of its body rather than by being the first definition of that name.
func TestHelperFieldsSkipsUnrelatedNamesakes(t *testing.T) {
	t.Parallel()

	bin := []byte(`function N(e,t){var r=!Array.isArray(e);return r}` +
		`function N(){return{session_id:n,transcript_path:p}}` +
		`function N(o){return o.length}`)

	got := helperFields(bin, "N")

	want := []string{fieldSessionID, fieldTranscriptPath}
	if !slices.Equal(got, want) {
		t.Errorf("helperFields() = %v, want %v", got, want)
	}
}

// A helper the payload spreads but the binary does not define contributes
// nothing rather than failing, so its fields go missing and the pinned-schema
// comparison is what reports the drift.
func TestHelperFieldsWithNoDefinition(t *testing.T) {
	t.Parallel()

	if got := helperFields([]byte(`return{...Missing(),cwd:d}`), "Missing"); got != nil {
		t.Errorf("helperFields() = %v, want nil", got)
	}
}

func TestMatchDelim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"flat object", `{a:1}`, 4},
		{"nested object", `{a:{b:2}}`, 8},
		{"brace inside a string", `{a:"}"}`, 6},
		{"escaped quote inside a string", `{a:"\""}`, 7},
		{"template literal", "{a:`x${y}z`}", 11},
		{"unterminated", `{a:{b:2}`, -1},
		{"unterminated string", `{a:"x}`, -1},
		{"wrong opening delimiter", `(a:1}`, -1},
		{"empty", ``, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := matchDelim([]byte(tt.input), '{', '}'); got != tt.want {
				t.Errorf("matchDelim(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}
