package runstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/route"
)

// writeChain writes sealed records, already linked, as one corridor file.
func writeChain(t *testing.T, dir string, records ...*Record) {
	t.Helper()
	var b strings.Builder
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "USDC-NGNC"+FileExt), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// version5Record is a sealed Version 5 record carrying the optional fields
// earlier migrations added (snapshot, correction), so the tests below prove
// those still load once Version moves to 6.
func version5Record(t *testing.T) *Record {
	t.Helper()
	r := fixedRecord()
	r.Version = 5
	r.Snapshot = "sha256:" + strings.Repeat("b", 64)
	if err := r.Seal(); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestVersion6AddsLedgerRangeWithoutChangingOlderHashes(t *testing.T) {
	r := fixedRecord()
	before, err := r.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	pre, err := r.Preimage()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(pre), "ledger") {
		t.Fatalf("a record with no ledger data must not mention ledgers in its preimage:\n%s", pre)
	}

	r.LedgerRange = &LedgerRange{First: 100, Last: 104}
	after, err := r.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("ledger_range must be inside the hashed preimage")
	}

	r2 := fixedRecord()
	r2.Rungs[0].Ledger = 100
	rungHash, err := r2.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if rungHash == before {
		t.Error("a rung's ledger must be inside the hashed preimage")
	}
}

func TestVersion5ChainContinuesWithVersion6LedgerRange(t *testing.T) {
	dir := t.TempDir()
	legacy := version5Record(t)
	writeChain(t, dir, legacy)

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open v5 chain: %v", err)
	}
	next := fixedRecord()
	next.LedgerRange = &LedgerRange{First: 61_000_000, Last: 61_000_007}
	next.Rungs[0].Ledger = 61_000_000
	if err := s.Append(ctx(), next); err != nil {
		t.Fatalf("Append v6 record: %v", err)
	}
	if next.Version != 6 || next.PrevHash != legacy.Hash {
		t.Errorf("appended record = version %d prev %s; want v6 prev %s", next.Version, next.PrevHash, legacy.Hash)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("mixed v5/v6 chain does not verify: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen v5/v6 chain: %v", err)
	}
	latest, err := reopened.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest.LedgerRange == nil || *latest.LedgerRange != (LedgerRange{First: 61_000_000, Last: 61_000_007}) {
		t.Errorf("stored ledger range = %+v, want 61000000..61000007", latest.LedgerRange)
	}
	if latest.Rungs[0].Ledger != 61_000_000 {
		t.Errorf("stored rung ledger = %d, want 61000000", latest.Rungs[0].Ledger)
	}
}

// TestReadOnlyStoreLoadsVersion5RecordWithSnapshot pins the fix to the
// read-only loader's snapshot check, which compared against the current
// Version: once Version moved to 6 it would have refused every valid
// Version 5 record carrying a snapshot hash. Snapshot arrived in Version 4.
func TestReadOnlyStoreLoadsVersion5RecordWithSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeChain(t, dir, version5Record(t))

	s, err := OpenFS(os.DirFS(dir), ".")
	if err != nil {
		t.Fatalf("OpenFS refused a valid v5 record with a snapshot: %v", err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("v5 record does not verify: %v", err)
	}
}

func TestLedgerMetadataInPreVersion6RecordIsRefused(t *testing.T) {
	for name, mutate := range map[string]func(*Record){
		"range": func(r *Record) { r.LedgerRange = &LedgerRange{First: 1, Last: 2} },
		"rung":  func(r *Record) { r.Rungs[0].Ledger = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			r := fixedRecord()
			r.Version = 5
			mutate(r)
			if err := r.Seal(); err != nil {
				t.Fatal(err)
			}

			dir := t.TempDir()
			writeChain(t, dir, r)
			if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "ledger metadata") {
				t.Errorf("Open: err = %v, want a ledger-metadata refusal", err)
			}
			if _, err := OpenFS(os.DirFS(dir), "."); err == nil || !strings.Contains(err.Error(), "ledger metadata") {
				t.Errorf("OpenFS: err = %v, want a ledger-metadata refusal", err)
			}
		})
	}
}

func TestFromCorridorJSONCarriesLedgerRange(t *testing.T) {
	c := route.CorridorJSON{
		SendAsset:    route.AssetJSON{Code: "USDC"},
		ReceiveAsset: route.AssetJSON{Code: "NGNC"},
		LedgerRange:  &route.LedgerRangeJSON{First: 10, Last: 13, Span: 3},
		Rungs: []route.RungJSON{
			{SendAmount: "1", Ledger: 10},
			{SendAmount: "10", Ledger: 13},
			{SendAmount: "100"},
		},
	}
	r := FromCorridorJSON(c)
	if r.LedgerRange == nil || *r.LedgerRange != (LedgerRange{First: 10, Last: 13}) {
		t.Errorf("record ledger range = %+v, want 10..13", r.LedgerRange)
	}
	got := []int64{r.Rungs[0].Ledger, r.Rungs[1].Ledger, r.Rungs[2].Ledger}
	if got[0] != 10 || got[1] != 13 || got[2] != 0 {
		t.Errorf("rung ledgers = %v, want [10 13 0]", got)
	}

	c.LedgerRange = nil
	if FromCorridorJSON(c).LedgerRange != nil {
		t.Error("an unknown range must stay absent in the record")
	}
}
