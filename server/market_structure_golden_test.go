package server

import (
	"encoding/json"
	"flag"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var updateMarketStructure = flag.Bool("update-market-structure", false,
	"rewrite the market-structure golden file from current behaviour")

// TestMarketStructureGolden pins the full wire shape of /api/market-structure.
//
// The endpoint landed late (#503) and its shape is already consumed by docs and
// soon by the UI, so a field rename or a value that silently changes type should
// fail here rather than reach a reader. The clock is pinned, which is what lets
// generated_at be frozen on disk instead of pattern-matched away: a golden file
// that skipped the timestamp would not notice the endpoint dropping it or
// changing its format.
//
// It reads no fixture network and no store — the issuer concentrations come
// from the static verified registry — so it runs under the offline test job.
func TestMarketStructureGolden(t *testing.T) {
	fixed := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	s := &Server{now: func() time.Time { return fixed }}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	status, raw := rawGet(t, srv.URL+"/api/market-structure?pretty=1")
	if status != 200 {
		t.Fatalf("status = %d, want 200: %s", status, raw)
	}

	// An empty registry would marshal to a valid but useless document; reject
	// it so a fixture that stopped resolving fails loudly instead of pinning
	// nothing.
	var doc struct {
		IssuerConcentrations map[string][]string `json:"issuer_concentrations"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("response is not JSON: %v\n%s", err, raw)
	}
	if len(doc.IssuerConcentrations) == 0 {
		t.Fatal("no issuer concentrations in the response; the golden file would pin nothing")
	}

	path := filepath.Join("testdata", "golden", "market-structure.json")
	if *updateMarketStructure {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file: %v\nRun: go test ./server/... -update-market-structure", err)
	}
	if string(raw) != string(want) {
		t.Errorf("market-structure response does not match %s.\n got: %s\nwant: %s\n\n"+
			"This pins the endpoint's wire shape. Confirm the new output is "+
			"intended before regenerating with -update-market-structure.",
			path, raw, want)
	}
}
