package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/runstore"
)

// The export is offline-testable by construction: it reads a FileStore and
// writes to an io.Writer, so every case below drives a real chain in a temp dir
// and inspects the bytes that would have gone to stdout.

// exportRecord builds a record carrying every field the export publishes, so a
// dropped or reordered column fails rather than hiding behind an empty cell.
func exportRecord(at time.Time) *runstore.Record {
	return &runstore.Record{
		RecordedAt: at,
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		Reference: runstore.Reference{
			Mid:             "1350.2568",
			Source:          "currency-api",
			AsOf:            "2026-08-21T00:00:00Z",
			FetchedAt:       "2026-08-21T22:28:00Z",
			SecondaryMid:    "1348.0585",
			SecondarySource: "exchangerate-api",
			SecondaryAsOf:   "2026-08-21T00:02:31Z",
			DivergencePct:   "0.1600",
			ScoredAgainst:   "currency-api",
		},
		FloorLossPct: "25.02", FloorSize: "0.1",
		WorstLossPct: "97.68", WorstSize: "5000",
		Finding: "No usable size.",
	}
}

func seededStore(t *testing.T, n int) runstore.Store {
	t.Helper()
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC)
	for i := 0; i < n; i++ {
		rec := exportRecord(base.Add(time.Duration(i) * 6 * time.Hour))
		if err := store.Append(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// TestExportCSVWritesOneRowPerRecord pins the shape the issue asks for: a
// header, then one row per stored record, chronological, with the stored
// decimal strings copied through unchanged.
func TestExportCSVWritesOneRowPerRecord(t *testing.T) {
	store := seededStore(t, 3)

	var out bytes.Buffer
	code := exportChain(store, "USDC-NGNC", "csv", &out, freshLogger())
	if code != 0 {
		t.Fatalf("export exit = %d, want 0; output:\n%s", code, out.String())
	}

	rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatalf("the export is not parseable CSV: %v\n%s", err, out.String())
	}
	if len(rows) != 4 {
		t.Fatalf("got %d lines, want a header plus 3 records:\n%s", len(rows), out.String())
	}
	if strings.Join(rows[0], ",") != strings.Join(csvColumns, ",") {
		t.Errorf("header = %v, want %v", rows[0], csvColumns)
	}

	// The rows are chronological (oldest first), matching Store.All.
	if rows[1][0] != "2026-08-21T22:30:40Z" {
		t.Errorf("first row recorded_at = %q, want the oldest record", rows[1][0])
	}
	if rows[3][0] != "2026-08-22T10:30:40Z" {
		t.Errorf("last row recorded_at = %q, want the newest record", rows[3][0])
	}

	// Every cell of the oldest row is the stored value, unchanged.
	want := []string{
		"2026-08-21T22:30:40Z",
		"USDC-NGNC",
		"1",
		"DIRECT",
		"25.02",
		"0.1",
		"97.68",
		"5000",
		"1350.2568",
		"currency-api",
		"2026-08-21T00:00:00Z",
		"2026-08-21T22:28:00Z",
		"currency-api",
		"1348.0585",
		"exchangerate-api",
		"2026-08-21T00:02:31Z",
		"0.1600",
	}
	if len(rows[1]) != len(want) {
		t.Fatalf("row has %d cells, want %d: %v", len(rows[1]), len(want), rows[1])
	}
	for i := range want {
		if rows[1][i] != want[i] {
			t.Errorf("column %q = %q, want %q (stored verbatim)", csvColumns[i], rows[1][i], want[i])
		}
	}
}

// TestExportMoneyFieldsStayDecimalStrings is the project's boundary rule
// applied to the new artifact: a loss figure exported as "25.02" must never
// become a float, because that is how a published number stops matching the
// record it came from.
func TestExportMoneyFieldsStayDecimalStrings(t *testing.T) {
	store := seededStore(t, 1)

	var out bytes.Buffer
	if code := exportChain(store, "USDC-NGNC", "csv", &out, freshLogger()); code != 0 {
		t.Fatalf("export exit = %d", code)
	}

	rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	idx := map[string]int{}
	for i, c := range csvColumns {
		idx[c] = i
	}
	for col, want := range map[string]string{
		"floor_loss_pct":           "25.02",
		"floor_size":               "0.1",
		"worst_loss_pct":           "97.68",
		"worst_size":               "5000",
		"reference_mid":            "1350.2568",
		"reference_divergence_pct": "0.1600",
	} {
		if got := rows[1][idx[col]]; got != want {
			t.Errorf("%s = %q, want the stored string %q", col, got, want)
		}
	}
}

// TestExportRefusesABrokenChain is the issue's second acceptance criterion.
// A tampered record must produce no CSV at all — not a partial file, and not a
// warning followed by the data.
func TestExportRefusesABrokenChain(t *testing.T) {
	dir := t.TempDir()
	store, err := runstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	rec := exportRecord(time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC))
	if err := store.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	// Alter a measured figure after it was sealed. The hash no longer
	// reconciles, which is exactly the "quietly improved number" the chain
	// exists to catch.
	path := filepath.Join(dir, "USDC-NGNC"+runstore.FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"floor_loss_pct":"25.02"`, `"floor_loss_pct":"4.02"`, 1)
	if tampered == string(raw) {
		t.Fatal("test setup: the record does not carry the expected floor_loss_pct")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := exportChain(store, "USDC-NGNC", "csv", &out, freshLogger())
	if code != 1 {
		t.Errorf("export exit = %d, want 1 for a chain that fails verification", code)
	}
	if out.Len() != 0 {
		t.Errorf("a refused export wrote %d bytes; a partial CSV would read as a short history:\n%s",
			out.Len(), out.String())
	}
}

// TestExportRejectsUnknownCorridorAndFormat keeps usage errors distinct from
// verification failures: neither is a measurement, so neither may look like one
// on stdout.
func TestExportRejectsUnknownCorridorAndFormat(t *testing.T) {
	store := seededStore(t, 1)

	cases := []struct {
		name     string
		corridor string
		format   string
		store    runstore.Store
	}{
		{"unknown corridor", "USDC-KESC", "csv", store},
		{"unsupported format", "USDC-NGNC", "ndjson", store},
		{"empty corridor", "", "csv", store},
		{"no store", "USDC-NGNC", "csv", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := exportChain(tc.store, tc.corridor, tc.format, &out, freshLogger()); code != 2 {
				t.Errorf("exit = %d, want 2 for a usage error", code)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %q; a usage error must not emit a document", out.String())
			}
		})
	}
}

// TestExportAcceptsCorridorCaseAndFormatCase keeps the flag forgiving about
// case without changing the corridor key it resolves to.
func TestExportAcceptsCorridorCaseAndFormatCase(t *testing.T) {
	store := seededStore(t, 1)

	var out bytes.Buffer
	if code := exportChain(store, " usdc-ngnc ", "CSV", &out, freshLogger()); code != 0 {
		t.Fatalf("export exit = %d, want 0 for a case-insensitive corridor", code)
	}
	if !strings.HasPrefix(out.String(), "recorded_at,corridor") {
		t.Errorf("output does not start with the header: %q", out.String())
	}
}

// TestExportQuotesFieldsContainingSeparators guards the CSV quoting contract:
// a finding is not exported, but a corridor or source could legitimately
// contain a comma one day, and the writer must escape rather than split the
// cell.
func TestExportQuotesFieldsContainingSeparators(t *testing.T) {
	store, err := runstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec := exportRecord(time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC))
	rec.Reference.Source = "vendor, with comma"
	if err := store.Append(context.Background(), rec); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := exportChain(store, "USDC-NGNC", "csv", &out, freshLogger()); code != 0 {
		t.Fatalf("export exit = %d", code)
	}
	rows, err := csv.NewReader(strings.NewReader(out.String())).ReadAll()
	if err != nil {
		t.Fatalf("unparseable CSV: %v", err)
	}
	idx := map[string]int{}
	for i, c := range csvColumns {
		idx[c] = i
	}
	if got := rows[1][idx["reference_source"]]; got != "vendor, with comma" {
		t.Errorf("reference_source = %q, want the value intact inside one cell", got)
	}
}
