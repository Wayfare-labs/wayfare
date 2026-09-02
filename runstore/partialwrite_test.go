package runstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Partial write and truncated final line (issue #130)
//
// NDJSON appended by a process killed mid-write is the realistic corruption:
// the writer is gone, so the file simply ends in the middle of a record, and
// the corruption is not retried or completed. The store's contract is that a
// chain with a torn tail is refused — never silently truncated, never served
// with a plausible-looking last record, and never "recovered" by guessing.
//
// These are the named cases for backlog #27 / issue #130. Each one pins a
// refusal that a plausible "repair" mutation would break: dropping the final
// unterminated line, skipping lines that fail to parse, or treating a record
// whose hash is missing as if it had never been sealed.
// ---------------------------------------------------------------------------

// TestPartialWrite_TruncatedFinalLineRefusedNamesCorridorAndLine covers the
// plain realistic kill — a valid chain followed by a record cut off mid-write,
// with no trailing newline — and asserts the diagnosability contract: the
// refusal must say which corridor and which line is torn, because an operator
// repairing a store by hand needs to find it. A store that merely said
// "corrupt" would fail this.
func TestPartialWrite_TruncatedFinalLineRefusedNamesCorridorAndLine(t *testing.T) {
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	partial := recordLine(t, r2)[:len(recordLine(t, r2))*6/10]

	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+partial)

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a store with a truncated final line from a partial write")
	}
	if !strings.Contains(err.Error(), "USDC-NGNC") {
		t.Errorf("error should name the corridor, got: %v", err)
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name line 2, got: %v", err)
	}
}

// TestPartialWrite_SingleTruncatedLineRefused covers the first-ever append
// being killed: the file is one torn line. Open must refuse, and the error
// must name line 1.
func TestPartialWrite_SingleTruncatedLineRefused(t *testing.T) {
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	partial := recordLine(t, r1)[:len(recordLine(t, r1))*6/10]

	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", partial)

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a store whose only line is a torn partial write")
	}
	if !strings.Contains(err.Error(), "USDC-NGNC") || !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error should name corridor and line 1, got: %v", err)
	}
}

// TestPartialWrite_SyntacticallyValidTruncatedLineRefused covers the sneaky
// kill: the line is cut at a value boundary, so the torn tail is still valid
// JSON — only the hash chain can tell it is incomplete. A parser that accepts
// "valid JSON" and skips verification would serve a fabricated-looking last
// record. Open must refuse through chain verification, naming the record.
func TestPartialWrite_SyntacticallyValidTruncatedLineRefused(t *testing.T) {
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	full := recordLine(t, r2)

	// The hash field is the last in the struct-declaration order, so
	// cutting at the comma before it and closing the object leaves a
	// complete JSON object whose prev_hash links it to the chain but
	// whose own hash never arrived.
	cut := strings.LastIndex(full, `,"hash"`)
	if cut < 0 {
		t.Fatal("fixture broken: record line has no hash field to cut at")
	}
	validPrefix := full[:cut] + "}"

	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+validPrefix)

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a truncated line that parsed as valid JSON but was never sealed")
	}
	if !strings.Contains(err.Error(), "USDC-NGNC") || !strings.Contains(err.Error(), "seq 2") {
		t.Errorf("error should name corridor and the torn record seq 2, got: %v", err)
	}
}

// TestPartialWrite_CompleteFinalRecordWithoutNewlineLoads pins the boundary
// of what "truncated" means: a complete record whose terminating newline was
// never written is NOT a partial write — every byte of the record is there.
// Refusing it would break files written by tools that omit the final newline;
// accepting it must not weaken the torn-tail refusals above. The chain must
// open, serve the record, verify, and accept the next append.
func TestPartialWrite_CompleteFinalRecordWithoutNewlineLoads(t *testing.T) {
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")

	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+recordLine(t, r2))

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open refused a complete final record without a trailing newline: %v", err)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Seq != 2 {
		t.Fatalf("Latest = %v, want the seq-2 record", latest)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("chain with a complete final record should verify: %v", err)
	}

	// The chain continues normally after the unterminated final line.
	r3 := buildValidRecord(3, r2.Hash, "USDC-NGNC")
	if err := s.Append(ctx(), r3); err != nil {
		t.Fatalf("append after an unterminated but complete record: %v", err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("chain should still verify after the append: %v", err)
	}
}

// TestPartialWrite_VerifyCatchesTruncationAfterOpen covers the restore path:
// the store is already open with its tip in memory, and the file on disk then
// gains a torn line (a bad restore, a partial copy). Verify — the command
// wayfared -verify-store runs — must catch it and name the line, while Latest
// must still serve the last complete record rather than the torn tail.
func TestPartialWrite_VerifyCatchesTruncationAfterOpen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	if err := s.Append(ctx(), r1); err != nil {
		t.Fatal(err)
	}

	// Simulate the file on disk gaining a torn second record after the
	// store was opened — the store's in-memory tip still points at r1.
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	full2 := recordLine(t, r2)
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, full2[:len(full2)/2]...), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err == nil {
		t.Fatal("Verify accepted a chain whose file gained a torn tail")
	} else if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("Verify should name line 2, got: %v", err)
	}

	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil || latest.Seq != 1 {
		t.Fatalf("Latest = %v, want the last complete record (seq 1)", latest)
	}
}

// TestPartialWrite_TruncatedFinalLineRefusedOnOpenFS covers the read-only
// embedded store: the published history is served from an fs.FS, and a torn
// tail embedded in it must be refused at load with the same corridor-and-line
// naming, never served as history.
func TestPartialWrite_TruncatedFinalLineRefusedOnOpenFS(t *testing.T) {
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	partial := recordLine(t, r2)[:len(recordLine(t, r2))*6/10]

	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+partial)

	_, err := OpenFS(os.DirFS(dir), ".")
	if err == nil {
		t.Fatal("OpenFS accepted an embedded chain with a truncated final line")
	}
	if !strings.Contains(err.Error(), "USDC-NGNC") || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("OpenFS error should name corridor and line 2, got: %v", err)
	}
}
