package wayfare

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests are the CI defence of the two-dependency position
// (CONTRIBUTING.md, docs/dependency-policy.md, backlog #264 / issue #317).
//
// The position itself is deliberate: shopspring/decimal because binary
// floating point cannot carry money, BurntSushi/toml because asset identity
// rides on stellar.toml and deserves a reviewed parser, and the standard
// library for everything else. The tests below do not argue the position —
// they make drifting from it a red build rather than a quiet fact a reviewer
// has to catch by diffing go.mod against memory.

// parseGoModule reads go.mod and returns the block of direct require entries
// as module→version pairs. Everything else in the file (indirect requires,
// go directive, toolchain) is ignored: this build treats go.mod's require
// block as the pin, because there is no vendor directory or separate
// lockfile.
func parseGoModule(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("go.mod")
	if err != nil {
		t.Fatalf("go.mod must exist for the dependency guard: %v", err)
	}
	defer f.Close()

	out := map[string]string{}
	inRequire := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "require ("):
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case strings.HasPrefix(line, "require "):
			// Single-line form: require module version
			fields := strings.Fields(strings.TrimPrefix(line, "require "))
			if len(fields) == 2 {
				out[fields[0]] = fields[1]
			}
		case inRequire:
			fields := strings.Fields(line)
			// "module version" or "module version // indirect"
			if len(fields) >= 2 && !strings.HasPrefix(fields[1], "//") {
				out[fields[0]] = fields[1]
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading go.mod: %v", err)
	}
	return out
}

// parseGoSum returns every module/version pair go.sum holds a hash for.
// Both the module hash (h1:) and the go.mod hash (/go.mod) lines count: an
// entry only in one form is still an entry on the dependency surface.
func parseGoSum(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("go.sum")
	if err != nil {
		t.Fatalf("go.sum must exist for the dependency guard: %v", err)
	}
	defer f.Close()

	out := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) >= 2 {
			out[fields[0]+"@"+strings.TrimSuffix(fields[1], "/go.mod")] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading go.sum: %v", err)
	}
	return out
}

// TestDirectDependenciesAreExactlyTheDeclaredSet is the core guard: go.mod
// must require exactly the two modules the policy names — no more, no fewer,
// no renames. A third dependency fails here and the PR explains why the
// policy changed; that is the process working, not the test being wrong.
func TestDirectDependenciesAreExactlyTheDeclaredSet(t *testing.T) {
	require := parseGoModule(t)

	got := make([]string, 0, len(require))
	for m := range require {
		got = append(got, m)
	}
	// Order-insensitive comparison against the declared set.
	want := map[string]bool{}
	for _, m := range DirectDependencies {
		want[m] = true
	}
	if len(require) != len(want) {
		t.Errorf("go.mod requires %d modules (%v), the policy allows exactly %d (%v)",
			len(require), got, len(want), DirectDependencies)
	}
	for m := range require {
		if !want[m] {
			t.Errorf("go.mod requires %q, which is not in the declared dependency set %v; "+
				"adding a dependency is a policy change (docs/dependency-policy.md) and must be argued in the PR that makes it",
				m, DirectDependencies)
		}
	}
	for m := range want {
		if _, ok := require[m]; !ok {
			t.Errorf("declared dependency %q is missing from go.mod; update DirectDependencies in deps.go", m)
		}
	}
}

// TestDirectDependenciesArePinnedToDeclaredVersions checks the pin: the
// version resolved in go.mod must equal the one declared in deps.go. If this
// fails, either go.mod moved without the declaration following (update
// deps.go, and dependency-policy.md's stated versions), or the declaration
// moved without go.mod (restore the pin).
func TestDirectDependenciesArePinnedToDeclaredVersions(t *testing.T) {
	require := parseGoModule(t)
	for m, want := range DirectDependenciesPinned {
		got, ok := require[m]
		if !ok {
			t.Errorf("pinned dependency %q is not required by go.mod", m)
			continue
		}
		if got != want {
			t.Errorf("dependency %q resolves to %s, the policy pins %s; "+
				"moving the version is a supply-chain decision and must be deliberate",
				m, got, want)
		}
	}
}

// TestGoSumHoldsExactlyThePinnedSurface guards the transitive closure: with
// two direct dependencies and neither having transitive requirements, go.sum
// carries exactly four lines — both modules at their pinned version, in both
// hash forms. A fifth module version in go.sum means something new entered
// the build's supply chain, however it got there.
func TestGoSumHoldsExactlyThePinnedSurface(t *testing.T) {
	sum := parseGoSum(t)

	want := map[string]bool{}
	for _, m := range DirectDependencies {
		v := DirectDependenciesPinned[m]
		want[m+"@"+v] = true
	}
	// The go.mod-hash form appears as module@version too after the suffix
	// trim in parseGoSum, so the want set is already complete: each module
	// contributes one distinct key, reached by both hash lines.

	for k := range sum {
		if !want[k] {
			t.Errorf("go.sum contains %q, which is outside the declared dependency surface %v; "+
				"a new module version has entered the supply chain and must be argued in its PR", k, want)
		}
	}
	for k := range want {
		if !sum[k] {
			t.Errorf("go.sum is missing %q; run go mod tidy and commit the result", k)
		}
	}
}

// TestDirectDependenciesAreImportedWhereThePolicySays keeps the declaration
// honest against the tree: each allowed dependency must still be imported
// somewhere in the module. A dependency go.mod requires but no package
// imports is drift of a different kind — a pin nobody uses.
func TestDirectDependenciesAreImportedWhereThePolicySays(t *testing.T) {
	for _, dep := range DirectDependencies {
		path := strings.TrimPrefix(dep, "github.com/")
		// Search the working tree's Go sources for the import path.
		found, err := treeImports(path)
		if err != nil {
			t.Fatalf("scanning for imports of %s: %v", dep, err)
		}
		if !found {
			t.Errorf("no package imports %s; go.mod requires a dependency the tree does not use", dep)
		}
	}
}

// treeImports reports whether any .go file under the working tree imports
// the given module path. It walks the tree recursively and stops at the
// first hit. Deliberately naive — a line-level scan rather than a parse —
// because the question it answers is "does anything use this module at
// all", not "where"; a false positive can only make the test miss a real
// absence, never fail a clean tree. Small and dependency-free on purpose:
// this test is part of the surface it guards.
func treeImports(modulePath string) (bool, error) {
	found := false
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Module caches and VCS internals are never part of the answer, and
		// the walk starts at the module root, so hidden directories are
		// somebody else's business.
		if d.IsDir() && (d.Name() == ".git" || strings.HasPrefix(d.Name(), ".")) {
			if path != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if found || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if strings.Contains(sc.Text(), modulePath) {
				found = true
				return nil
			}
		}
		return sc.Err()
	})
	return found, err
}
