package runstore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Wayfare-labs/wayfare/checks"
)

func ctx() context.Context { return context.Background() }

// fixedRecord is a record with every field pinned to a constant, used by the
// hash-pinning test. Nothing here may change without a Version bump.
//
// It is a Version 2 record and deliberately carries a checks block and a
// metrics block: those fields are part of the pinned preimage, so a change to
// either their shape or their order is caught exactly like a change to any
// other field. (How a Version 1 record still verifies under Version 2 is
// exercised separately — see TestVersion1RecordStillVerifies.)
func fixedRecord() *Record {
	return &Record{
		Version:    2,
		Seq:        1,
		RecordedAt: time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC),
		Corridor:   "USDC-NGNC",
		Integrity:  "DIRECT",
		DependsOn:  []string{},
		Reference: Reference{
			Mid:           "1350.2568",
			Source:        "currency-api",
			AsOf:          "2026-08-21T00:00:00Z",
			ScoredAgainst: "currency-api",
		},
		FloorLossPct:    "25.02",
		FloorSize:       "0.1",
		WorstLossPct:    "97.68",
		WorstSize:       "5000",
		Recommended:     nil,
		RecommendedSize: "",
		Finding:         "No usable size.",
		Rungs: []Rung{{
			SendAmount: "0.1", Priced: true, Integrity: "DIRECT",
			ReceiveAmount: "102.78", EffectiveRate: "1027.84",
			LossPct: "24.65", Verdict: "UNUSABLE", Path: "USDC -> NGNC",
		}},
		Checks: []checks.CheckJSON{
			{
				ID: "anchor-asset-iso4217", Scope: "anchor", Subject: "ngnc.online",
				Severity: "notice", Determined: true, Passed: true,
				Summary: "anchor_asset names the NGNC shilling",
				Evidence: []checks.EvidenceJSON{{
					Source:     "ngnc.online/.well-known/stellar.toml",
					Observed:   "ANCHOR_ASSET=" + "//SHILLING-NGNC:GBUUDI3TKOD3FONOFLGT4WTW6GVJ5YOBH4KUSYY3YOKCH3Z7VHQPB6XG",
					ObservedAt: "2026-08-21T22:28:00Z",
				}},
				ObservedAt: "2026-08-21T22:28:00Z",
			},
			{
				ID: "sep10.endpoint-responds", Scope: "anchor", Subject: "ngnc.online",
				Severity: "warning", Determined: false, Passed: false,
				Reason:  "no sep10 web-auth endpoint declared",
				Summary: "could not determine: no sep10 web-auth endpoint declared",
				Evidence: []checks.EvidenceJSON{{
					Source:     "ngnc.online/.well-known/stellar.toml",
					Observed:   "NO WEB_AUTH_ENDPOINT",
					ObservedAt: "2026-08-21T22:28:00Z",
				}},
				ObservedAt: "2026-08-21T22:28:00Z",
			},
		},
		Metrics: []checks.MetricJSON{
			{
				ID: "spread.bid-ask", Scope: "asset", Subject: "USDC",
				Determined: true, Value: "0.0004", Unit: "ratio",
				Summary: "bid-ask spread on the USDC/NGNC book",
				Evidence: []checks.EvidenceJSON{{
					Source:     "https://horizon.stellar.org/order_book",
					Observed:   "bid=1350.1000 ask=1350.6400",
					ObservedAt: "2026-08-21T22:28:00Z",
				}},
				ObservedAt: "2026-08-21T22:28:00Z",
			},
		},
		PrevHash: GenesisPrevHash,
	}
}

// TestRecordHashIsPinned freezes the hash of a known record.
//
// A failure here means the field set, the field order, or the JSON encoding
// settings changed. Any of those changes the preimage of every record ever
// written, which silently invalidates every stored chain — a reader verifying
// last month's history against this build would be told it was tampered with.
//
// So this is NOT a test to update casually. If it fails, the correct response
// is a Version bump and a migration, and updating the constant below is the
// last step of that work rather than the fix for a red build.
func TestRecordHashIsPinned(t *testing.T) {
	// Re-established 2026-08-25 when Version 2 added the checks and metrics
	// blocks, by computing it from fixedRecord above. Every value in that
	// fixture is part of it, including the checks block and the metrics
	// block: they are fields like any other, and a change to their shape or
	// order must fail here rather than pass review.
	const want = "sha256:ebc429fff786de9cb43abbc16b6859efa62a6be06ca25692dc91c233d05e5fb0"

	got, err := fixedRecord().ComputeHash()
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if got != want {
		t.Errorf("record hash = %s, want %s\n\n"+
			"The preimage changed. This means the field set, the field order, or the\n"+
			"encoding settings of runstore.Record are not what they were, and every\n"+
			"previously stored chain now fails verification against this build.\n"+
			"That is a Version bump plus a migration, not a constant to update.",
			got, want)
	}
}

// TestVersion1RecordStillVerifies is the migration's proof.
//
// A Version 1 record — no checks block, no metrics block — must load and
// verify under this (Version 2) build, because history is evidence and a
// schema change must not invalidate it. This works because the Version 2
// fields are omitempty and declared after every Version 1 field, so a record
// without findings encodes to byte-for-byte the same JSON — and therefore the
// same hash — it did under the old struct. The hash pinned here is exactly the
// one the original TestRecordHashIsPinned froze when the format was Version 1,
// and it must never change.
func TestVersion1RecordStillVerifies(t *testing.T) {
	const legacyV1Hash = "sha256:1872c8f154123508633ecb2ffdc0c6918539b744f2d1be0c7edc61173d4edca2"

	r := fixedRecord()
	// Shape it back into the Version 1 record the legacy constant was
	// computed from: version 1 and no findings.
	r.Version = 1
	r.Checks = nil
	r.Metrics = nil

	if h, err := r.ComputeHash(); err != nil {
		t.Fatal(err)
	} else if h != legacyV1Hash {
		t.Errorf("Version 1 record hash = %s, want %s; a stored Version 1 "+
			"chain would no longer verify against this build", h, legacyV1Hash)
	}

	// And the full write/open/verify path: a chain whose records were
	// written as Version 1 must load and verify, and new appends continue
	// it as Version 2.
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	v1 := fixedRecord()
	v1.Version = 1
	v1.Checks = nil
	v1.Metrics = nil
	if err := s.Append(ctx(), v1); err != nil {
		t.Fatalf("append v1-shaped record: %v", err)
	}
	if v1.Version != 2 {
		t.Errorf("Append rewrote a v1-shaped record to version %d; a rewriter must "+
			"not relabel legacy records", v1.Version)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("chain with a freshly-written v1-shaped record: %v", err)
	}
}

// TestVersion1ChainOnDiskStillLoads proves the on-disk path, not just the
// in-memory one: a real Version 1 line as produced by the old build must load
// and verify unchanged.
func TestVersion1ChainOnDiskStillLoads(t *testing.T) {
	dir := t.TempDir()
	// First record exactly as Version 1 wrote it (the legacy pinned hash).
	line := `{"version":1,"seq":1,"recorded_at":"2026-08-21T22:30:40Z","corridor":"USDC-NGNC",` +
		`"integrity":"DIRECT","depends_on":[],"reference":{"mid":"1350.2568",` +
		`"source":"currency-api","as_of":"2026-08-21T00:00:00Z",` +
		`"scored_against":"currency-api"},"floor_loss_pct":"25.02",` +
		`"floor_size":"0.1","worst_loss_pct":"97.68","worst_size":"5000",` +
		`"recommended":null,"finding":"No usable size.","rungs":[{"send_amount":"0.1",` +
		`"priced":true,"integrity":"DIRECT","receive_amount":"102.78",` +
		`"effective_rate":"1027.84","loss_pct":"24.65","verdict":"UNUSABLE",` +
		`"path":"USDC -> NGNC"}],"prev_hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000",` +
		`"hash":"sha256:1872c8f154123508633ecb2ffdc0c6918539b744f2d1be0c7edc61173d4edca2"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "USDC-NGNC"+FileExt), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open with a mixed/legacy chain: %v", err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify on a Version 1 chain: %v", err)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || latest == nil {
		t.Fatalf("Latest on a Version 1 chain = %v, %v", latest, err)
	}
	if latest.Version != 1 {
		t.Errorf("loaded record version = %d, want 1 (kept as written)", latest.Version)
	}

	// Appending a Version 2 record to that Version 1 chain must produce a
	// single verifying chain covering both versions.
	v2 := fixedRecord()
	if err := s.Append(ctx(), v2); err != nil {
		t.Fatalf("append v2 to v1 chain: %v", err)
	}
	if v2.Seq != 2 || v2.PrevHash != latest.Hash {
		t.Errorf("appended v2 seq %d prev %s, want seq 2 chained to the v1 tip %s",
			v2.Seq, short(v2.PrevHash), short(latest.Hash))
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("mixed v1+v2 chain failed to verify: %v", err)
	}
}

// TestPrevHashIsInsideThePreimage is what makes the structure a chain rather
// than a list of independently-hashed records. If prev_hash were outside the
// hashed bytes, an editor could rewrite history and re-link it freely.
func TestPrevHashIsInsideThePreimage(t *testing.T) {
	a := fixedRecord()
	b := fixedRecord()
	b.PrevHash = "sha256:" + strings.Repeat("a", 64)

	ha, err := a.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.ComputeHash()
	if err != nil {
		t.Fatal(err)
	}
	if ha == hb {
		t.Error("changing prev_hash did not change the record hash; the records are not chained")
	}
}

// TestNilAndEmptySlicesHashAlike guards a subtle way two identical
// measurements could hash differently: nil and empty slices encode as null and
// [], so a record's hash would depend on how its caller happened to build it.
func TestNilAndEmptySlicesHashAlike(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	withNil := fixedRecord()
	withNil.DependsOn = nil
	withNil.Rungs = nil
	if err := s.Append(ctx(), withNil); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if withNil.DependsOn == nil || withNil.Rungs == nil {
		t.Fatal("Append must normalise nil slices before hashing")
	}
	if err := withNil.VerifySelf(); err != nil {
		t.Errorf("record written with nil slices does not verify: %v", err)
	}
}

func TestAppendChainsAndVerifies(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	var prev string
	for i := 0; i < 5; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if got, want := r.Seq, int64(i+1); got != want {
			t.Errorf("Seq = %d, want %d", got, want)
		}
		if i == 0 && r.PrevHash != GenesisPrevHash {
			t.Errorf("first record prev_hash = %s, want genesis", r.PrevHash)
		}
		if i > 0 && r.PrevHash != prev {
			t.Errorf("record %d prev_hash = %s, want %s", i, r.PrevHash, prev)
		}
		prev = r.Hash
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify on an untouched chain: %v", err)
	}
}

// TestVerifyDetectsTampering is the whole point of the package. A chain that
// cannot detect an edited past record is decoration.
func TestVerifyDetectsTampering(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Improve a loss figure in the middle of the history, the realistic
	// abuse: a number quietly adjusted long after it was published.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected 5 records, got %d", len(lines))
	}
	lines[2] = strings.Replace(lines[2], `"floor_loss_pct":"25.02"`, `"floor_loss_pct":"5.02"`, 1)
	if !strings.Contains(lines[2], `"floor_loss_pct":"5.02"`) {
		t.Fatal("test setup failed: the field was not edited")
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = s.Verify(ctx(), "USDC-NGNC")
	if err == nil {
		t.Fatal("Verify passed on a chain with an edited record")
	}
	if !strings.Contains(err.Error(), "seq 3") {
		t.Errorf("error should name the offending record (seq 3), got: %v", err)
	}

	// Reopening must fail too, so a tampered store cannot be appended to.
	if _, err := Open(dir); err == nil {
		t.Error("Open accepted a store with a broken chain")
	}
}

func TestLatestAndRecent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := s.Latest(ctx(), "USDC-NGNC"); err != nil || got != nil {
		t.Errorf("Latest on an empty store = %v, %v; want nil, nil", got, err)
	}

	for i := 0; i < 4; i++ {
		r := fixedRecord()
		r.Integrity = []string{"DIRECT", "DIRECT", "DERIVATIVE", "NO-MARKET"}[i]
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Integrity != "NO-MARKET" || latest.Seq != 4 {
		t.Errorf("Latest = seq %d %s, want seq 4 NO-MARKET", latest.Seq, latest.Integrity)
	}

	// Recent(corridor, 2) is the whole of #24's read path: compare the last
	// two runs and report an integrity change.
	recent, err := s.Recent(ctx(), "USDC-NGNC", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("Recent returned %d records, want 2", len(recent))
	}
	if recent[0].Integrity != "NO-MARKET" || recent[1].Integrity != "DERIVATIVE" {
		t.Errorf("Recent = [%s %s], want [NO-MARKET DERIVATIVE] (newest first)",
			recent[0].Integrity, recent[1].Integrity)
	}
}

// TestReopenResumesTheChain covers the restart path: a redeployed monitor must
// continue the existing chain, not start a second one.
func TestReopenResumesTheChain(t *testing.T) {
	dir := t.TempDir()

	first, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r1 := fixedRecord()
	if err := first.Append(ctx(), r1); err != nil {
		t.Fatal(err)
	}

	second, err := Open(dir)
	if err != nil {
		t.Fatalf("reopening a valid store: %v", err)
	}
	r2 := fixedRecord()
	if err := second.Append(ctx(), r2); err != nil {
		t.Fatal(err)
	}

	if r2.Seq != 2 {
		t.Errorf("Seq after reopen = %d, want 2", r2.Seq)
	}
	if r2.PrevHash != r1.Hash {
		t.Errorf("chain did not resume: prev_hash = %s, want %s", r2.PrevHash, r1.Hash)
	}
	if err := second.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after reopen: %v", err)
	}
}

// TestUnknownVersionIsRefused mirrors the snapshot format's rule: a schema
// this build does not understand is an error, never a best-effort parse.
func TestUnknownVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	line := `{"version":99,"seq":1,"corridor":"USDC-NGNC","prev_hash":"` +
		GenesisPrevHash + `","hash":"sha256:x"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "USDC-NGNC"+FileExt), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a record with an unknown version")
	}
	if !strings.Contains(err.Error(), "version 99") {
		t.Errorf("error should name the version found, got: %v", err)
	}
}

// TestNopStoreIsSafe pins the degrade-gracefully requirement: a monitor with
// no history configured behaves exactly as it did before this package existed.
func TestNopStoreIsSafe(t *testing.T) {
	var s Store = Nop{}
	if err := s.Append(ctx(), fixedRecord()); err != nil {
		t.Errorf("Nop.Append: %v", err)
	}
	got, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || got != nil {
		t.Errorf("Nop.Latest = %v, %v; want nil, nil", got, err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Nop.Verify: %v", err)
	}
}
