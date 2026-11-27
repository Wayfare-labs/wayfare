package wayfare

// docs/api.md is what a consumer copies. Example drift is the most common API
// documentation bug: a path gains a required parameter, a field is renamed, and
// the prose still reads correctly while the example no longer runs (#669).
//
// These tests close that loop. They do not check that the examples are pretty,
// or that they match a golden transcript — a transcript would be one more thing
// to update and would say nothing about whether the request works. They execute
// the examples, through bash and the real curl, against a real server serving
// committed history and replayed snapshots, and assert the status and the
// top-level shape each example promises.
//
// The deployment host in the document is rewritten to the local server at run
// time. That is the only substitution: every flag, quote and line continuation
// in the example runs exactly as printed.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
	"github.com/Wayfare-labs/wayfare/server"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

const (
	apiDocsPath = "docs/api.md"

	// documentedHost is the deployment every example in the document uses.
	// CI substitutes the local server for it; leaving the document's host
	// intact keeps the examples copy-pasteable for a reader.
	documentedHost = "https://wayfare-cdb9.onrender.com"
)

// apiExample is one annotated curl block.
type apiExample struct {
	name string
	// fields are required top-level keys. A token of the form key=value
	// additionally requires that scalar value, which is how the live
	// example asserts it really measured rather than falling back to
	// history without saying so.
	fields []apiField
	script string
	line   int
}

type apiField struct {
	key   string
	value string // empty means presence only
}

// apiExampleMarker matches the comment placed immediately before a runnable
// block:
//
//	<!-- api-example: name -> key1,key2,key=value -->
var apiExampleMarker = regexp.MustCompile(`^<!-- api-example: ([a-z0-9-]+) -> (.+?) -->$`)

// parseAPIExamples reads the document. It returns the annotated examples and
// the line numbers of curl blocks that carry no annotation, because a new
// example that nobody annotated is exactly the drift this guards against.
func parseAPIExamples(raw string) ([]apiExample, []int) {
	lines := strings.Split(raw, "\n")
	var (
		examples   []apiExample
		unmarked   []int
		pending    *apiExample
		inBlock    bool
		blockStart int
		block      []string
	)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if !inBlock {
			if m := apiExampleMarker.FindStringSubmatch(trimmed); m != nil {
				pending = &apiExample{name: m[1], fields: parseAPIFields(m[2]), line: i + 1}
				continue
			}
			if trimmed == "```bash" {
				inBlock = true
				blockStart = i + 1
				block = nil
			}
			continue
		}

		if trimmed == "```" {
			inBlock = false
			script := strings.Join(block, "\n")
			if strings.Contains(script, "curl") {
				if pending == nil {
					unmarked = append(unmarked, blockStart)
					continue
				}
				pending.script = script
				examples = append(examples, *pending)
			}
			pending = nil
			continue
		}
		block = append(block, line)
	}

	return examples, unmarked
}

func parseAPIFields(spec string) []apiField {
	spec = strings.TrimSpace(spec)
	if spec == "-" {
		return nil
	}
	var out []apiField
	for _, tok := range strings.Split(spec, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		key, value, hasValue := strings.Cut(tok, "=")
		f := apiField{key: key}
		if hasValue {
			f.value = value
		}
		out = append(out, f)
	}
	return out
}

// TestAPIDocsCurlExamplesAreAnnotated makes the gate non-optional: adding a
// curl example without a marker fails the build, rather than quietly shrinking
// the set of examples CI checks.
func TestAPIDocsCurlExamplesAreAnnotated(t *testing.T) {
	raw, err := os.ReadFile(apiDocsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", apiDocsPath, err)
	}
	examples, unmarked := parseAPIExamples(string(raw))

	if len(examples) == 0 {
		t.Fatal("no annotated curl examples found; the documentation gate would " +
			"pass without executing anything")
	}
	for _, line := range unmarked {
		t.Errorf("%s:%d is a curl example with no api-example marker; every "+
			"example has to be executed in CI (#669)", apiDocsPath, line)
	}

	seen := map[string]bool{}
	for _, ex := range examples {
		if seen[ex.name] {
			t.Errorf("two examples are named %q; the name identifies the failure "+
				"in the CI log", ex.name)
		}
		seen[ex.name] = true
	}

	// Every endpoint the reference advertises must have an example that is
	// actually executed. Without this, deleting a section would leave CI
	// green while the document stopped demonstrating an endpoint.
	doc := string(raw)
	for _, path := range advertisedAPIPaths(t, doc) {
		found := false
		for _, ex := range examples {
			if strings.Contains(ex.script, path) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the endpoint table advertises %s but no executable example "+
				"requests it; the example is the documentation consumers run", path)
		}
	}
}

// advertisedAPIPaths reads the Endpoints table. "/" is skipped: it returns the
// UI rather than JSON, and its correctness is covered by the browser harness.
func advertisedAPIPaths(t *testing.T, doc string) []string {
	t.Helper()
	row := regexp.MustCompile("^\\|\\s*`GET`\\s*\\|\\s*`([^`]+)`\\s*\\|")
	var paths []string
	for _, line := range strings.Split(doc, "\n") {
		m := row.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[1] == "/" {
			continue
		}
		paths = append(paths, m[1])
	}
	if len(paths) == 0 {
		t.Fatal("the Endpoints table in docs/api.md yielded no GET paths; the " +
			"table has moved and the coverage check is looking in the wrong place")
	}
	return paths
}

// TestAPIDocsCurlExamplesExecute is the gate. Each example runs in bash with
// `set -euo pipefail` and a curl that fails on HTTP >= 400, so a failing
// example fails the job.
func TestAPIDocsCurlExamplesExecute(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash is required to execute the documented examples: %v", err)
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skipf("curl is required to execute the documented examples: %v", err)
	}

	raw, err := os.ReadFile(apiDocsPath)
	if err != nil {
		t.Fatalf("reading %s: %v", apiDocsPath, err)
	}
	examples, _ := parseAPIExamples(string(raw))
	if len(examples) == 0 {
		t.Fatal("no annotated curl examples to execute")
	}

	srv := newAPIDocsServer(t)

	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			script := strings.ReplaceAll(ex.script, documentedHost, srv.URL)
			if strings.Contains(script, documentedHost) {
				t.Fatalf("an example still references %s after substitution",
					documentedHost)
			}

			// -s is already in most examples; repeating it is harmless. The
			// important flag is --fail-with-body: a 404 or 500 must make the
			// shell fail, because curl by default exits 0 on an HTTP error
			// and an example that "runs" while returning an error page is
			// not a working example.
			harness := "set -euo pipefail\n" +
				"curl() { command curl --silent --show-error --fail-with-body \"$@\"; }\n" +
				"export -f curl\n" +
				script + "\n"

			cmd := exec.Command("bash", "-c", harness)
			cmd.Dir = "."
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s:%d failed: %v\n--- output ---\n%s\n--- script ---\n%s",
					apiDocsPath, ex.line, err, out, script)
			}

			if len(ex.fields) == 0 {
				return
			}
			last, err := lastJSONObject(out)
			if err != nil {
				t.Fatalf("%s:%d produced no JSON to check: %v\n--- output ---\n%s",
					apiDocsPath, ex.line, err, out)
			}
			for _, field := range ex.fields {
				value, ok := last[field.key]
				if !ok {
					t.Errorf("%s:%d: response has no top-level %q; the example's "+
						"marker declares it. Keys present: %s",
						apiDocsPath, ex.line, field.key, strings.Join(sortedAPIKeys(last), ", "))
					continue
				}
				if field.value == "" {
					continue
				}
				got := strings.Trim(string(value), `"`)
				if got != field.value {
					t.Errorf("%s:%d: %s = %s, want %s; the example's marker "+
						"declares it", apiDocsPath, ex.line, field.key, got, field.value)
				}
			}
		})
	}
}

// newAPIDocsServer serves the documented endpoints from committed bytes only:
// history-first reads the embedded chain, and a live request replays the
// committed USDC → NGNC snapshot. Nothing here touches the network, so the
// gate is safe in the offline CI job.
func newAPIDocsServer(t *testing.T) *httptest.Server {
	t.Helper()

	manifest, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
	if err != nil {
		t.Fatalf("loading the snapshot the live example replays: %v", err)
	}
	store, err := runstore.OpenFS(History, "data")
	if err != nil {
		t.Fatalf("opening the embedded history: %v", err)
	}

	engine := &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: manifest.HTTPClient(),
		},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1349.669672"),
		}),
	}

	// OpenFS walks and verifies every embedded chain, so this records the
	// same verdict cmd/wayfared records when it serves the embedded history;
	// without it /readyz would answer 503 and its example could not run.
	srv := httptest.NewServer((&server.Server{
		Engine:       engine,
		Store:        store,
		HistoryFirst: true,
		Verification: &server.Verification{
			ChainVerified: true,
			Note:          "embedded history verified at open",
		},
	}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// lastJSONObject decodes every JSON value in the output and returns the last,
// because a block that issues several curls prints several documents in
// sequence (and `pretty=1` prints one indented one).
func lastJSONObject(out []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(strings.NewReader(string(out)))
	var last map[string]json.RawMessage
	for {
		var next map[string]json.RawMessage
		err := dec.Decode(&next)
		if err == io.EOF {
			break
		}
		if err != nil {
			if last == nil {
				return nil, fmt.Errorf("decoding output: %w", err)
			}
			break
		}
		last = next
	}
	if last == nil {
		return nil, fmt.Errorf("output contained no JSON document")
	}
	return last, nil
}

func sortedAPIKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Small map, and the message only has to be stable enough to read.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
