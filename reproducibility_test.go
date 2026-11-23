package wayfare

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReproducibleBuildJobConfigured verifies that .github/workflows/ci.yml
// defines a job proving reproducible builds with the required flags (issue #728).
func TestReproducibleBuildJobConfigured(t *testing.T) {
	raw, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("reading ci.yml: %v", err)
	}
	content := string(raw)

	for _, want := range []string{
		"reproducible-build:",
		"-trimpath",
		"-buildvcs=false",
		"CGO_ENABLED=0",
		"sha256sum",
	} {
		if !strings.Contains(content, want) {
			t.Errorf(".github/workflows/ci.yml is missing %q in reproducible build configuration", want)
		}
	}
}

// TestWayfaredBuildIsByteIdenticalAcrossPaths builds cmd/wayfared in two
// separate directory paths using the documented flags and asserts the resulting
// binaries are bit-for-bit identical (issue #728).
func TestWayfaredBuildIsByteIdenticalAcrossPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow binary build test in short mode")
	}

	dirA := t.TempDir()
	dirB := t.TempDir()

	binA := filepath.Join(dirA, "wayfared")
	binB := filepath.Join(dirB, "wayfared")

	build := func(outPath string) string {
		cmd := exec.CommandContext(context.Background(), "go", "build",
			"-trimpath",
			"-buildvcs=false",
			"-ldflags=-s -w",
			"-o", outPath,
			"./cmd/wayfared",
		)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("building %s failed: %v\n%s", outPath, err, out)
		}

		bytes, err := os.ReadFile(outPath)
		if err != nil {
			t.Fatalf("reading %s: %v", outPath, err)
		}
		sum := sha256.Sum256(bytes)
		return hex.EncodeToString(sum[:])
	}

	hashA := build(binA)
	hashB := build(binB)

	if hashA != hashB {
		t.Fatalf("reproducibility failure: build A hash %s != build B hash %s", hashA, hashB)
	}
	t.Logf("both builds produced byte-identical binary: sha256:%s", hashA)
}
