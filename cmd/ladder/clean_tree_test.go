package main

// Tests for the provenance claim in the README's verification table
// ("Recorded snapshots ... provenance refuses a dirty tree") and in
// docs/snapshot-format.md rule 9 ("git_revision must not lie"): recording a
// snapshot refuses a working tree that git reports as dirty — including a
// tree whose only difference from HEAD is an untracked file. Backlog #31 /
// GitHub issue #134.
//
// The tests drive requireCleanTree against scratch repositories created in
// t.TempDir(), never against the checkout the test suite runs from, so the
// suite's own cleanliness cannot influence the result.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs git in dir, returning any error together with the captured
// output so a failure in test setup is diagnosable.
func runGit(t *testing.T, dir string, args ...string) error {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %v: %w\n%s", args, err, out)
	}
	return nil
}

// newTempRepo makes a scratch git repository containing one committed file
// and returns its path.
func newTempRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "tests@wayfare.local"},
		{"config", "user.name", "wayfare tests"},
		{"config", "commit.gpgSign", "false"},
	} {
		if err := runGit(t, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "."}, {"commit", "-q", "-m", "initial"}} {
		if err := runGit(t, dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	return dir
}

func TestRequireCleanTreeAcceptsCleanTree(t *testing.T) {
	dir := newTempRepo(t)

	dirty, err := requireCleanTree(dir, false)
	if err != nil {
		t.Fatalf("requireCleanTree refused a clean tree: %v", err)
	}
	if dirty {
		t.Error("a clean tree was reported dirty; the manifest would be mislabelled")
	}
}

func TestRequireCleanTreeRefusesModifiedTrackedFile(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := requireCleanTree(dir, false)
	if err == nil {
		t.Fatal("recording from a tree with a modified tracked file was not refused; " +
			"git_revision would name a tree that did not produce the bytes")
	}
	if !strings.Contains(err.Error(), "tracked.txt") {
		t.Errorf("refusal does not name the modified file, got: %v", err)
	}
}

func TestRequireCleanTreeRefusesStagedChange(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t, dir, "add", "staged.txt"); err != nil {
		t.Fatal(err)
	}

	_, err := requireCleanTree(dir, false)
	if err == nil {
		t.Fatal("recording from a tree with a staged-but-uncommitted change was not refused")
	}
	if !strings.Contains(err.Error(), "staged.txt") {
		t.Errorf("refusal does not name the staged file, got: %v", err)
	}
}

func TestRequireCleanTreeRefusesUntrackedFileOnly(t *testing.T) {
	// The headline case: no tracked file differs from HEAD at all, and the
	// tree is still refused. HEAD does not contain the untracked file, so
	// git_revision would not pin the working tree the bytes came from.
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := requireCleanTree(dir, false)
	if err == nil {
		t.Fatal("recording from a tree whose only difference from HEAD is an untracked " +
			"file was not refused; provenance must refuse a dirty tree")
	}
	if !strings.Contains(err.Error(), "scratch.txt") {
		t.Errorf("refusal does not name the untracked file, got: %v", err)
	}
}

func TestRequireCleanTreeAllowDirtyMarksModifiedTrackedFile(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirty, err := requireCleanTree(dir, true)
	if err != nil {
		t.Fatalf("-allow-dirty refused a modified tree: %v", err)
	}
	if !dirty {
		t.Error("-allow-dirty recording did not mark the manifest dirty")
	}
}

func TestRequireCleanTreeAllowDirtyMarksUntrackedFile(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "scratch.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dirty, err := requireCleanTree(dir, true)
	if err != nil {
		t.Fatalf("-allow-dirty refused a tree with an untracked file: %v", err)
	}
	if !dirty {
		t.Error("-allow-dirty recording did not mark the manifest dirty")
	}
}

func TestRequireCleanTreeOutsideAGitCheckout(t *testing.T) {
	// No git repository at all: recording is still possible, and the
	// manifest represents the missing revision honestly rather than
	// guessing one — this is not a refusal.
	dir := t.TempDir()

	dirty, err := requireCleanTree(dir, false)
	if err != nil {
		t.Fatalf("recording outside a git checkout was refused: %v", err)
	}
	if dirty {
		t.Error("outside a git checkout the tree must not be reported dirty")
	}
}
