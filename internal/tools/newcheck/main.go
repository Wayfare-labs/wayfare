// Command newcheck scaffolds a check and its offline test.
//
// A check is small, but most of what makes one correct is structural: the ID
// shape, the mandatory CannotDetermine, the injectable HTTP client with a
// guarded default, and undetermined-not-fail when an input is missing. Copied
// boilerplate is how one of those gets lost, so this tool writes the parts that
// are already decided and leaves the fact itself — and registration, which is
// a decision — to the contributor. docs/adding-a-check.md is the guide.
//
// From the repository root:
//
//	go run ./internal/tools/newcheck -id toml.example-fact -name ExampleFact
//	go run ./internal/tools/newcheck -id toml.example-fact -name ExampleFact -dry-run
//
// It writes checks/<snake_name>.go and checks/<snake_name>_test.go, refuses to
// overwrite either, and prints the Runner.Default() line to add. The generated
// test fails until its TODOs are answered, so an unimplemented scaffold cannot
// be merged unnoticed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

func main() {
	id := flag.String("id", "", "check ID in <source>.<fact> form, e.g. toml.example-fact")
	name := flag.String("name", "", "exported Go type name for the check, e.g. ExampleFact")
	dryRun := flag.Bool("dry-run", false, "print the files without writing them")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: newcheck -id <source>.<fact> -name <Name> [-dry-run]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *id == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "newcheck: -id and -name are required")
		flag.Usage()
		os.Exit(2)
	}
	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "newcheck: unexpected argument %q; flags only\n", flag.Arg(0))
		flag.Usage()
		os.Exit(2)
	}

	files, err := generate(options{ID: *id, Name: *name, Dir: "checks", DryRun: *dryRun})
	if err != nil {
		fmt.Fprintf(os.Stderr, "newcheck: %v\n", err)
		os.Exit(1)
	}

	if *dryRun {
		for _, f := range files {
			fmt.Printf("=== %s ===\n%s\n", f.path, f.body)
		}
	} else {
		for _, f := range files {
			fmt.Printf("wrote %s\n", f.path)
		}
	}
	fmt.Printf("\nAdd this line to Runner.Default() in checks/runner.go:\n\n    %s{HTTPClient: r.client()},\n", *name)
}

// options is everything the scaffold is built from. Dir exists so the tool's
// own test can run the generator against a temporary copy of checks/ without
// touching the tree.
type options struct {
	ID     string
	Name   string
	Dir    string
	DryRun bool
}

// scaffold is one generated file, already formatted.
type scaffold struct {
	path string
	body []byte
}

// idPattern is the ID shape docs/adding-a-check.md step 1 describes:
// <source>.<fact>, lower-case and hyphenated. Exactly one dot.
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`)

// namePattern requires an exported Go identifier. A check is constructed by
// name in Runner.Default(), so an unexported type would not be usable there.
var namePattern = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)

// generate renders the pair, refuses to overwrite, and writes it unless this is
// a dry run. Both existence checks happen before either file is written, so a
// single generate call never leaves half a scaffold behind.
func generate(o options) ([]scaffold, error) {
	id, err := validateID(o.ID)
	if err != nil {
		return nil, err
	}
	if !namePattern.MatchString(o.Name) {
		return nil, fmt.Errorf("invalid check name %q: use an exported Go identifier, e.g. ExampleFact", o.Name)
	}

	dir := o.Dir
	if dir == "" {
		dir = "checks"
	}
	snake := snakeName(id)
	checkPath := filepath.Join(dir, snake+".go")
	testPath := filepath.Join(dir, snake+"_test.go")

	for _, path := range []string{checkPath, testPath} {
		if _, err := os.Stat(path); err == nil {
			return nil, fmt.Errorf("%s already exists; refusing to overwrite an existing check", path)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}

	data := templateData{ID: id, Name: o.Name, Snake: snake}

	checkBody, err := render(checkTemplate, data)
	if err != nil {
		return nil, err
	}
	testBody, err := render(testTemplate, data)
	if err != nil {
		return nil, err
	}

	files := []scaffold{
		{path: checkPath, body: checkBody},
		{path: testPath, body: testBody},
	}
	if o.DryRun {
		return files, nil
	}

	for _, f := range files {
		// O_EXCL rather than WriteFile: the stat above is a courtesy, and two
		// concurrent runs must still not clobber each other.
		fh, err := os.OpenFile(f.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return nil, err
		}
		if _, err := fh.Write(f.body); err != nil {
			_ = fh.Close()
			return nil, err
		}
		if err := fh.Close(); err != nil {
			return nil, err
		}
	}
	return files, nil
}

// validateID enforces the documented shape and reports why it is wrong, since
// the ID is the handle a reader correlates findings by across runs.
func validateID(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("invalid check ID %q: IDs are <source>.<fact>, lower-case and hyphenated, "+
			"e.g. toml.anchor-asset-iso4217", id)
	}
	for _, part := range strings.Split(id, ".") {
		if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") || strings.Contains(part, "--") {
			return "", fmt.Errorf("invalid check ID %q: %q has a misplaced hyphen; hyphenate between words, "+
				"e.g. anchor-asset-iso4217", id, part)
		}
	}
	return id, nil
}

// snakeName maps an ID onto the file naming convention already in checks/:
// dots and hyphens both become underscores, so toml.example-fact lives in
// toml_example_fact.go.
func snakeName(id string) string {
	return strings.NewReplacer(".", "_", "-", "_").Replace(id)
}

type templateData struct {
	ID    string
	Name  string
	Snake string
}

// render executes a template and formats the result. format.Source is the
// reason the templates can be written for readability rather than column
// alignment, and it makes a broken template a generation error rather than an
// unparseable file the contributor has to debug.
func render(src string, data templateData) ([]byte, error) {
	tmpl, err := template.New("scaffold").Parse(src)
	if err != nil {
		return nil, fmt.Errorf("internal template: %w", err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("internal template: %w", err)
	}
	body, err := format.Source([]byte(buf.String()))
	if err != nil {
		return nil, fmt.Errorf("internal template does not produce valid Go: %w", err)
	}
	return body, nil
}

const checkTemplate = `package checks

import (
	"context"
	"net/http"
	"time"
)

// {{.Name}} checks TODO: the one fact this check observes.
//
// TODO: say why the fact matters — what a reader would get wrong without it —
// and name the document or endpoint it is read from.
type {{.Name}} struct {
	// HTTPClient is injectable so this check can be replayed from a snapshot
	// and tested with no network. Nil means GuardedClient, the same default
	// every networked check in this package uses.
	HTTPClient *http.Client
}

// Describe implements Check.
func (c {{.Name}}) Describe() Descriptor {
	return Descriptor{
		ID:       "{{.ID}}",
		Scope:    ScopeAnchor, // ScopeAnchor, ScopeAsset or ScopeCorridor
		Cost:     CostFree,    // CostFree, CostOneRequest or CostExpensive
		Severity: SeverityNotice,
		Title:    "TODO: one line naming what is checked",

		// CannotDetermine is mandatory and is the most valuable prose here:
		// what would a reader wrongly conclude from a pass?
		CanDetermine:    "TODO: what this check can establish, and from which document or endpoint.",
		CannotDetermine: "TODO: what a passing result does not prove.",
	}
}

// Run implements Check.
func (c {{.Name}}) Run(_ context.Context, s Subject) CheckResult {
	d := c.Describe()

	// TODO: read what you need from s, and return Undetermined with a reason —
	// never Fail — when it is absent. Network I/O goes through c.client().
	_ = c.client

	return Undetermined(d, s, "TODO: this check has not been implemented yet")
}

// client returns the injected HTTP client or the guarded default. Delete it if
// the check reads only Subject.Profile and makes no request.
func (c {{.Name}}) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return GuardedClient(15 * time.Second)
}
`

const testTemplate = `package checks

import (
	"context"
	"testing"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// Test{{.Name}} covers the three states the contract requires: undetermined,
// passed and failed. It fails as generated, because the check below is still a
// stub that can only report undetermined.
//
// TODO: replace the placeholder fixture and expectations with the real ones.
// Every case must run from recorded bytes — CI runs this suite with no network,
// and snapshot.Replayer answers a miss with ErrNotRecorded rather than reaching
// upstream.
func Test{{.Name}}(t *testing.T) {
	// TODO: record a snapshot for this check, load it with snapshot.Load, and
	// hand its client to the check, e.g.
	//
	//	m, err := snapshot.Load(filepath.Join("testdata", "....json"))
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//	c := {{.Name}}{HTTPClient: m.HTTPClient()}
	//
	// The zero manifest below answers every request with ErrNotRecorded, which
	// keeps the test offline while the check makes none.
	var m snapshot.Manifest
	c := {{.Name}}{HTTPClient: m.HTTPClient()}

	// TODO: build the subject the check needs from a real document. The zero
	// profile is a placeholder; a check that needs an input it does not have
	// must report Undetermined, never Fail.
	var profile anchor.Profile
	observed := Subject{Domain: profile.Domain, Profile: &profile}

	cases := []struct {
		name       string
		subject    Subject
		determined bool
		passed     bool
	}{
		// Keep a missing-input case: absent input is undetermined, not failed.
		{"missing input is undetermined", Subject{}, false, false},
		{"TODO: the fact holds", observed, true, true},
		{"TODO: the fact does not hold", observed, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Run(context.Background(), c, tc.subject)
			if got.Determined != tc.determined {
				t.Fatalf("Determined = %v, want %v (result: %s)", got.Determined, tc.determined, got.Summary)
			}
			if tc.determined && got.Passed != tc.passed {
				t.Fatalf("Passed = %v, want %v (result: %s)", got.Passed, tc.passed, got.Summary)
			}
			if !tc.determined && got.Reason == "" {
				t.Fatal("an undetermined result must carry a reason")
			}
		})
	}
}
`
