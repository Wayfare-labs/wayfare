package wayfare

// The package import graph, enforced from the ADR that documents it (issue
// #926).
//
// ADR 005 states the dependency graph between Wayfare's runtime packages, and
// ADR 003 states that layers 3 and 4 have no packages at all. Both are rules a
// reviewer has to remember today: the import that breaks ADR 005 — `monitor`
// reaching into `server` for a helper, or `server` triggering a sweep — is a
// one-line change that compiles, passes the other tests, and reads as
// harmless.
//
// The allow-list below is not written here. It is read out of the fenced code
// block in docs/adr/005-why-the-scheduler-does-not-depend-on-the-server.md, so
// the document and the build cannot disagree: adding an edge means adding it
// to the ADR, and editing the ADR without editing the code fails the test just
// as loudly as the reverse.
//
// ADR 003's absence rule is enforced the same way, from the list of forbidden
// package names that ADR carries.

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "github.com/Wayfare-labs/wayfare"

// adr005 is the document whose code block is the allow-list.
const adr005 = "docs/adr/005-why-the-scheduler-does-not-depend-on-the-server.md"

// adr003 is the document that says layers 3 and 4 have no packages.
const adr003 = "docs/adr/003-why-layers-3-and-4-have-no-packages.md"

// graphEdge is one row of the ADR's graph: a package and the internal
// packages it may import.
type graphEdge struct {
	pkg     string
	imports []string
}

// actualImportGraph returns every package in the module and the set of
// internal packages it imports from its non-test files.
//
// Test files are excluded deliberately. A test may import a package the
// production code must not reach for (an integration test is the obvious
// case), and a rule about the shipped dependency graph should not be broken
// by a test helper.
func actualImportGraph(t *testing.T) map[string][]string {
	t.Helper()

	graph := map[string][]string{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != "." && (strings.HasPrefix(name, ".") || name == "testdata" || name == "vendor" || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		dir := path.Dir(filepath.ToSlash(p))
		pkg := "."
		if dir != "." {
			pkg = dir
		}
		if _, seen := graph[pkg]; !seen {
			graph[pkg] = []string{}
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		for _, spec := range f.Imports {
			imported, uerr := strconv.Unquote(spec.Path.Value)
			if uerr != nil {
				return uerr
			}
			if imported == modulePath {
				graph[pkg] = append(graph[pkg], ".")
				continue
			}
			if !strings.HasPrefix(imported, modulePath+"/") {
				continue
			}
			rel := strings.TrimPrefix(imported, modulePath+"/")
			graph[pkg] = append(graph[pkg], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module for its import graph: %v", err)
	}
	for pkg, imports := range graph {
		graph[pkg] = uniqueSorted(imports)
	}
	return graph
}

func uniqueSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// fencedBlocks returns the contents of every fenced code block in markdown.
func fencedBlocks(t *testing.T, file string) []string {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	var blocks []string
	var cur []string
	inBlock := false
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inBlock {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = nil
				inBlock = false
			} else {
				inBlock = true
			}
			continue
		}
		if inBlock {
			cur = append(cur, line)
		}
	}
	return blocks
}

// parseGraphBlock reads rows of the form "pkg → a, b, c" out of the ADR.
//
// The arrow is the one the ADR already uses. Rows without one are prose or a
// diagram and are ignored, so the block can carry a sentence without silently
// becoming part of the rule.
func parseGraphBlock(t *testing.T, adr string) []graphEdge {
	t.Helper()
	var edges []graphEdge
	for _, block := range fencedBlocks(t, adr) {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			arrow := strings.Index(line, "→")
			if arrow < 0 {
				continue
			}
			pkg := strings.TrimSpace(line[:arrow])
			if pkg == "" || strings.ContainsAny(pkg, " \t") {
				continue
			}
			rest := strings.TrimSpace(line[arrow+len("→"):])
			var imports []string
			for _, part := range strings.Split(rest, ",") {
				if part = strings.TrimSpace(part); part != "" {
					imports = append(imports, part)
				}
			}
			edges = append(edges, graphEdge{pkg: pkg, imports: uniqueSorted(imports)})
		}
	}
	if len(edges) == 0 {
		t.Fatalf("%s contains no import-graph rows; the allow-list has to live in the ADR", adr)
	}
	return edges
}

// TestPackageImportGraph is ADR 005's graph, checked against the code.
func TestPackageImportGraph(t *testing.T) {
	actual := actualImportGraph(t)
	declared := parseGraphBlock(t, adr005)

	allowed := map[string][]string{}
	for _, e := range declared {
		if _, dup := allowed[e.pkg]; dup {
			t.Fatalf("%s declares package %q twice; one row per package", adr005, e.pkg)
		}
		allowed[e.pkg] = e.imports
	}

	// Every library package must be declared. A new package is a change to
	// the architecture, and the ADR is where that change is recorded.
	for pkg := range actual {
		if isCommandOrExample(pkg) {
			continue
		}
		if _, ok := allowed[pkg]; !ok {
			t.Errorf("package %q is not in the dependency graph in %s; add a row for it there, and to nothing else", pkg, adr005)
		}
	}

	for pkg := range allowed {
		if _, ok := actual[pkg]; !ok {
			t.Errorf("%s declares package %q, which does not exist; remove its row", adr005, pkg)
		}
	}

	for pkg, imports := range actual {
		if isCommandOrExample(pkg) {
			continue
		}
		want, ok := allowed[pkg]
		if !ok {
			continue // already reported above
		}
		for _, imported := range imports {
			if !contains(want, imported) {
				t.Errorf("%s imports %s, which %s does not allow.\nThe ADR is the rule: either the import goes, or the graph in %s changes with it (and the reason for the new edge is written down).", pkg, imported, adr005, adr005)
			}
		}
		for _, allowedImport := range want {
			if !contains(imports, allowedImport) {
				t.Errorf("%s lists %s as an import of %s, but the code does not import it.\nA graph that overstates an edge is as wrong as one that hides it: %s and the code have to say the same thing.", adr005, allowedImport, pkg, adr005)
			}
		}
	}

	// ADR 005's headline: neither runtime component imports the other.
	if imports, ok := actual["monitor"]; ok && contains(imports, "server") {
		t.Errorf("monitor imports server; %s forbids it — the scheduler must run with -serve=false and reach the server only through runstore", adr005)
	}
	if imports, ok := actual["server"]; ok && contains(imports, "monitor") {
		t.Errorf("server imports monitor; %s forbids it — a request must never trigger a sweep", adr005)
	}

	// cmd/* and examples/* are leaves: they may import the library, but the
	// library may not import them.
	for pkg, imports := range actual {
		if isCommandOrExample(pkg) {
			continue
		}
		for _, imported := range imports {
			if isCommandOrExample(imported) {
				t.Errorf("%s imports %s; commands and examples are leaves of the graph and nothing under them may be depended on", pkg, imported)
			}
		}
	}
}

// TestLayersThreeAndFourHaveNoPackages is ADR 003's absence rule, checked
// against the directory tree.
//
// The forbidden names come from the ADR's own list, so "reserving the space"
// for a layer that does not exist yet fails here instead of passing review as
// an empty package.
func TestLayersThreeAndFourHaveNoPackages(t *testing.T) {
	actual := actualImportGraph(t)
	forbidden := parseForbiddenPackages(t, adr003)
	if len(forbidden) == 0 {
		t.Fatalf("%s lists no forbidden package names; the rule has to be written down where the test can read it", adr003)
	}
	for pkg := range actual {
		for _, name := range forbidden {
			if pkg == name || strings.HasPrefix(pkg, name+"/") || path.Base(pkg) == name {
				t.Errorf("package %q exists, but %s says layers 3 and 4 have no packages and no stubs (forbidden name %q).\nSpeculative structure is worse than none: when the real inputs exist, the design starts from them.", pkg, adr003, name)
			}
		}
	}
}

// parseForbiddenPackages reads the deny-list out of ADR 003: every line of the
// form "forbidden: a, b, c".
func parseForbiddenPackages(t *testing.T, adr string) []string {
	t.Helper()
	var out []string
	for _, block := range fencedBlocks(t, adr) {
		for _, line := range strings.Split(block, "\n") {
			line = strings.TrimSpace(line)
			const prefix = "forbidden:"
			if !strings.HasPrefix(strings.ToLower(line), prefix) {
				continue
			}
			for _, part := range strings.Split(line[len(prefix):], ",") {
				if part = strings.TrimSpace(part); part != "" {
					out = append(out, part)
				}
			}
		}
	}
	return uniqueSorted(out)
}

func isCommandOrExample(pkg string) bool {
	return strings.HasPrefix(pkg, "cmd/") || strings.HasPrefix(pkg, "examples/")
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
