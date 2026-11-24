package main

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenerateWritesACompilingPairThatFailsUntilEdited covers the three
// acceptance criteria together, because they only mean something together: a
// pair that compiles but passes untouched would let a stub merge, and one that
// fails to compile is not a scaffold.
func TestGenerateWritesACompilingPairThatFailsUntilEdited(t *testing.T) {
	root := scaffoldCopy(t)

	files, err := generate(options{
		ID:   "toml.example-fact",
		Name: "ExampleFact",
		Dir:  filepath.Join(root, "checks"),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	want := []string{
		filepath.Join(root, "checks", "toml_example_fact.go"),
		filepath.Join(root, "checks", "toml_example_fact_test.go"),
	}
	if len(files) != len(want) {
		t.Fatalf("generate wrote %d files, want %d", len(files), len(want))
	}
	for i, path := range want {
		if files[i].path != path {
			t.Errorf("file %d = %q, want %q", i, files[i].path, path)
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("generated file %s is missing: %v", path, err)
		}
	}

	// Compiling the package is the first half. -run ^$ compiles the whole
	// package, tests included, without running anything.
	if out, err := goIn(root, "test", "-run", "^$", "./checks"); err != nil {
		t.Fatalf("the generated pair does not compile: %v\n%s", err, out)
	}

	// The second half: an untouched scaffold must be red. If this ever passes,
	// the tool has stopped protecting the merge.
	out, err := goIn(root, "test", "-run", "^TestExampleFact$", "./checks")
	if err == nil {
		t.Fatalf("the generated test passed untouched; an unimplemented scaffold must not be mergeable:\n%s", out)
	}
	if !strings.Contains(out, "TODO") {
		t.Errorf("the generated test failed without naming a TODO, so the failure does not tell its "+
			"author what to do:\n%s", out)
	}
}

// TestGenerateRefusesToOverwrite pins the refusal in both directions: an
// existing check and an existing test are each enough to stop the run, and
// neither half is written when it does.
func TestGenerateRefusesToOverwrite(t *testing.T) {
	for _, existing := range []string{"toml_example_fact.go", "toml_example_fact_test.go"} {
		t.Run(existing, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, existing)
			if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
				t.Fatal(err)
			}

			_, err := generate(options{ID: "toml.example-fact", Name: "ExampleFact", Dir: dir})
			if err == nil {
				t.Fatalf("generate overwrote %s", path)
			}
			if !strings.Contains(err.Error(), "already exists") {
				t.Errorf("error = %q, want it to say the file already exists", err)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 1 {
				t.Errorf("directory holds %d files after a refused run, want only the existing one", len(entries))
			}
		})
	}
}

// TestGenerateRejectsMalformedInput covers the ID shape docs/adding-a-check.md
// step 1 documents and the exported-name requirement. Both are reported before
// anything is written.
func TestGenerateRejectsMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		opts options
	}{
		{"no source", options{ID: "example-fact", Name: "ExampleFact"}},
		{"no fact", options{ID: "toml", Name: "ExampleFact"}},
		{"upper-case fact", options{ID: "toml.Example-Fact", Name: "ExampleFact"}},
		{"upper-case source", options{ID: "TOML.example-fact", Name: "ExampleFact"}},
		{"empty segment", options{ID: "toml..fact", Name: "ExampleFact"}},
		{"leading hyphen", options{ID: "toml.-fact", Name: "ExampleFact"}},
		{"trailing hyphen", options{ID: "toml.fact-", Name: "ExampleFact"}},
		{"double hyphen", options{ID: "toml.example--fact", Name: "ExampleFact"}},
		{"trailing dot", options{ID: "toml.fact.", Name: "ExampleFact"}},
		{"name is unexported", options{ID: "toml.example-fact", Name: "exampleFact"}},
		{"name has an underscore", options{ID: "toml.example-fact", Name: "Example_Fact"}},
		{"name starts with a digit", options{ID: "toml.example-fact", Name: "1Example"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Dir = t.TempDir()
			if _, err := generate(tc.opts); err == nil {
				t.Fatalf("generate accepted %+v", tc.opts)
			}
			entries, err := os.ReadDir(tc.opts.Dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Errorf("a rejected run wrote %d files", len(entries))
			}
		})
	}
}

// TestDryRunWritesNothing keeps -dry-run honest: it prints the same files a real
// run would write, and touches nothing.
func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()

	files, err := generate(options{ID: "toml.example-fact", Name: "ExampleFact", Dir: dir, DryRun: true})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("dry run produced %d files, want 2", len(files))
	}
	for _, f := range files {
		if len(f.body) == 0 {
			t.Errorf("dry run produced an empty body for %s", f.path)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("-dry-run wrote %d files", len(entries))
	}
}

// TestSnakeNameMatchesThePackageConvention pins the mapping the issue names:
// checks/<snake_name>.go, with the dot and every hyphen becoming underscores.
func TestSnakeNameMatchesThePackageConvention(t *testing.T) {
	if got := snakeName("toml.example-fact"); got != "toml_example_fact" {
		t.Errorf("snakeName = %q, want toml_example_fact", got)
	}
	if got := snakeName("issuer.auth-flags"); got != "issuer_auth_flags" {
		t.Errorf("snakeName = %q, want issuer_auth_flags", got)
	}
}

// scaffoldCopy mirrors the module — every .go file plus go.mod and go.sum — into
// a temporary directory, so the generator's test can add its files to a copy of
// checks/ and compile that copy. testdata is skipped because nothing here runs
// the recorded-response tests, only compiles the package.
func scaffoldCopy(t *testing.T) string {
	t.Helper()

	root := moduleRoot(t)
	dst := t.TempDir()

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", ".freebuff":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") && rel != "go.mod" && rel != "go.sum" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatalf("copying the module: %v", err)
	}
	return dst
}

// moduleRoot walks up from the test's working directory to the module root.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the test's working directory")
		}
		dir = parent
	}
}

// goIn runs the go command in dir with workspaces disabled, so the module copy
// is tested as its own module rather than through the contributor's GOWORK.
func goIn(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
