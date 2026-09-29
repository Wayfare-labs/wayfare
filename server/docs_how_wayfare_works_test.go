package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHowWayfareWorksExistsAndIsLinked pins issue #254's "done when": the
// plain-language document lives under docs/ and a reader landing on the
// README can find it from the section that orients new readers.
func TestHowWayfareWorksExistsAndIsLinked(t *testing.T) {
	const rel = "docs/how-wayfare-works.md"

	raw, err := os.ReadFile(filepath.Join("..", rel))
	if err != nil {
		t.Fatalf("%s must exist: %v", rel, err)
	}
	doc := string(raw)

	if !strings.Contains(doc, "# How Wayfare works") {
		t.Errorf("%s does not open with its title", rel)
	}

	readme, err := os.ReadFile(filepath.Join("..", "README.md"))
	if err != nil {
		t.Fatalf("README.md must exist: %v", err)
	}
	if !strings.Contains(string(readme), "](docs/how-wayfare-works.md)") {
		t.Error("README.md does not link docs/how-wayfare-works.md; a reader would not find it")
	}
}

// TestHowWayfareWorksChecksItsOwnClaims pins the issue's constraint that
// every measurement cited carries its source and the date it was checked,
// and that future capabilities are marked as future rather than described
// as built.
func TestHowWayfareWorksChecksItsOwnClaims(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "how-wayfare-works.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)

	// The dated-checked statement: when the claims were verified against the
	// code, so a reader can judge how much may have drifted since.
	if !strings.Contains(doc, "2026-09-29") {
		t.Error("the document does not state the date its claims were checked against the code")
	}

	// The published example figure is attributed to the document that carries
	// the raw output and its timestamp, not asserted bare.
	if !strings.Contains(doc, "docs/corridor-measurements.md") {
		t.Error("the cited measurement is not attributed to docs/corridor-measurements.md")
	}
	if !strings.Contains(doc, "measured 2026-08-08") {
		t.Error("the cited measurement does not carry the date it was measured")
	}

	// Layer 3 is described as unbuilt. Describing roadmap items as built is
	// exactly what the issue forbids.
	if !strings.Contains(doc, "not built") {
		t.Error("layer 3 is not marked as not built; future capability would read as shipped")
	}

	// The verdict thresholds cited must be the ones the code enforces
	// (route.ThresholdGood/Fair/Poor: 3, 8, 20) — the document may not
	// assert bands the repository does not implement.
	for _, band := range []string{"3%", "8%", "20%"} {
		if !strings.Contains(doc, band) {
			t.Errorf("verdict thresholds are missing %s; the document must match route's thresholds", band)
		}
	}

	// The ladder sizes cited must be the ones DefaultSizes runs.
	for _, size := range []string{"0.1", "500"} {
		if !strings.Contains(doc, size) {
			t.Errorf("the ladder description is missing the %s rung; the document must match dex.DefaultSizes", size)
		}
	}
}
