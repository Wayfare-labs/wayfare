package runstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedChain appends n fixed records to a fresh store, with a distinct
// measured value per seq so a history can tell them apart.
func seedChain(t *testing.T, dir string, n int) *FileStore {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for i := 0; i < n; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i%10))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	return s
}

// TestRotateTrimsToTheCeiling is the shape of the decision in ADR 007: a chain
// over the ceiling comes back at exactly the ceiling, re-sealed and verifiable,
// and the rotation report names what was dropped so the workflow can say so in
// the commit that lands it.
func TestRotateTrimsToTheCeiling(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 10)

	rot, err := s.Rotate(ctx(), 4)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rot) != 1 {
		t.Fatalf("Rotate returned %d rotations, want 1", len(rot))
	}
	r := rot[0]
	if r.Corridor != "USDC-NGNC" {
		t.Errorf("Corridor = %q, want USDC-NGNC", r.Corridor)
	}
	if r.RecordsBefore != 10 || r.RecordsAfter != 4 {
		t.Errorf("RecordsBefore/After = %d/%d, want 10/4", r.RecordsBefore, r.RecordsAfter)
	}
	if r.DroppedSeqStart != 1 || r.DroppedSeqEnd != 6 {
		t.Errorf("DroppedSeqStart/End = %d/%d, want 1/6", r.DroppedSeqStart, r.DroppedSeqEnd)
	}
	if r.NewHeadSeq != 7 {
		t.Errorf("NewHeadSeq = %d, want 7", r.NewHeadSeq)
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("Verify after rotation: %v", err)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || latest == nil {
		t.Fatalf("Latest = %v, %v", latest, err)
	}
	if latest.Seq != 10 {
		t.Errorf("Latest seq = %d, want 10 (the newest record survives)", latest.Seq)
	}

	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("window length = %d, want 4", len(all))
	}

	// The file itself matches the window, and a fresh Open verifies it, as
	// the next workflow run will experience.
	raw, err := os.ReadFile(filepath.Join(dir, "USDC-NGNC"+FileExt))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(raw)), "\n"); len(lines) != 4 {
		t.Errorf("file has %d lines, want 4", len(lines))
	}
	re, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen after rotation: %v", err)
	}
	if err := re.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after reopen: %v", err)
	}
}

// TestRotateLeavesUnderCeilingChainsUntouched: a chain at or below the ceiling
// is byte-for-byte unchanged — same records, same hashes. Rotation must never
// touch a chain that has not outgrown the repository.
func TestRotateLeavesUnderCeilingChainsUntouched(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 3)

	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	rawBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	rot, err := s.Rotate(ctx(), 10)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rot) != 0 {
		t.Errorf("Rotate returned %d rotations for a chain under the ceiling, want 0", len(rot))
	}

	rawAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawBefore) != string(rawAfter) {
		t.Error("a chain under the ceiling was modified by Rotate")
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestRotateReSealsTheWindowHead pins the hash-semantics of rotation: the
// oldest surviving record is re-sealed against GenesisPrevHash (its own
// predecessor is no longer in the file), and its seq stays original so a
// reader can still see where the window begins in the whole history.
func TestRotateReSealsTheWindowHead(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 5)

	if _, err := s.Rotate(ctx(), 3); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("window length = %d, want 3", len(all))
	}
	if all[0].Seq != 3 {
		t.Errorf("window head seq = %d, want 3 (seq numbers are preserved across rotation)", all[0].Seq)
	}
	if all[0].PrevHash != GenesisPrevHash {
		t.Errorf("window head prev_hash = %s, want genesis", short(all[0].PrevHash))
	}
	if err := all[0].VerifySelf(); err != nil {
		t.Errorf("re-sealed head does not verify: %v", err)
	}
}

// TestRotateToSingleRecordKeepsOnlyTheHead: the degenerate ceiling of one
// keeps the newest measurement alone, still valid, so rotation is defined at
// every positive ceiling rather than special-cased at the edges.
func TestRotateToSingleRecordKeepsOnlyTheHead(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 5)

	rot, err := s.Rotate(ctx(), 1)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rot) != 1 || rot[0].RecordsAfter != 1 {
		t.Fatalf("rotation = %+v, want a single-record window", rot)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || latest == nil {
		t.Fatalf("Latest = %v, %v", latest, err)
	}
	if latest.Seq != 5 {
		t.Errorf("Latest seq = %d, want 5 (the head survives)", latest.Seq)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestRotateThenAppendContinuesTheWindow is the rest of the workflow's loop:
// after a rotation the next sweep appends onto the re-sealed head with the
// next seq number, and the mixed result verifies. The store must not think the
// window is closed.
func TestRotateThenAppendContinuesTheWindow(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 5)

	if _, err := s.Rotate(ctx(), 3); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	head, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || head == nil {
		t.Fatalf("Latest after rotate = %v, %v", head, err)
	}

	next := fixedRecord()
	if err := s.Append(ctx(), next); err != nil {
		t.Fatalf("Append after rotation: %v", err)
	}
	if next.Seq != 6 {
		t.Errorf("appended seq = %d, want 6 (the sequence continues across rotation)", next.Seq)
	}
	if next.PrevHash != head.Hash {
		t.Errorf("appended prev_hash = %s, want the rotated head %s", short(next.PrevHash), short(head.Hash))
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Fatalf("Verify after append: %v", err)
	}

	re, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := re.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after reopen: %v", err)
	}
}

// TestRotateRefusesNonPositiveCeiling: a ceiling of zero or less is not "keep
// nothing" and not "no rotation" — it is a caller error, the same "unknown is
// never a default" discipline the rest of the package applies.
func TestRotateRefusesNonPositiveCeiling(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{0, -1, -100} {
		if _, err := s.Rotate(ctx(), n); err == nil {
			t.Errorf("Rotate(ceiling=%d) succeeded, want an error", n)
		}
	}
}

// TestRotatePreservesMeasuredContents is the honesty check of rotation: only
// the hash fields of surviving records move. Every measured figure — a loss, a
// timestamp, a verdict, a path — is byte-for-byte what it was, and the change
// to the window's hashes is real (re-sealing happened, not a no-op).
func TestRotatePreservesMeasuredContents(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 6)

	before := map[int64]string{} // seq -> rendered record with hash fields stripped
	for _, r := range mustAll(t, s) {
		before[r.Seq] = stripHashFields(t, r)
	}

	if _, err := s.Rotate(ctx(), 3); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	surviving := 0
	for _, r := range mustAll(t, s) {
		rendered := stripHashFields(t, r)
		if rendered != before[r.Seq] {
			t.Errorf("seq %d measured contents changed under rotation:\n got %s\nwant %s",
				r.Seq, rendered, before[r.Seq])
		}
		surviving++
	}
	if surviving != 3 {
		t.Errorf("window has %d surviving records, want 3", surviving)
	}
}

// TestRotateChangesTheWindowHashes is the other half of the honesty check:
// rotation is a real re-seal, not a no-op rename. Every surviving record's
// hash changes, because the preimage of each begins with the new head — a
// reader comparing the file before and after rotation sees every hash move,
// which is the visible evidence that a cut happened.
func TestRotateChangesTheWindowHashes(t *testing.T) {
	dir := t.TempDir()
	s := seedChain(t, dir, 5)

	before := map[int64]string{}
	for _, r := range mustAll(t, s) {
		before[r.Seq] = r.Hash
	}

	if _, err := s.Rotate(ctx(), 3); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	for _, r := range mustAll(t, s) {
		if r.Hash == before[r.Seq] {
			t.Errorf("seq %d hash unchanged after rotation (%s); a re-sealed window "+
				"must not masquerade as the original chain", r.Seq, short(r.Hash))
		}
	}
}

// TestRotateOnEmptyStoreDoesNothing: an empty store is a valid store with
// nothing to rotate, and must not error.
func TestRotateOnEmptyStoreDoesNothing(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rot, err := s.Rotate(ctx(), MaxWindow)
	if err != nil {
		t.Fatalf("Rotate on an empty store: %v", err)
	}
	if len(rot) != 0 {
		t.Errorf("Rotate returned %d rotations for an empty store, want 0", len(rot))
	}
}

// TestRotateIsPerCorridor: independent chains rotate independently — a chain
// over the ceiling is trimmed while a sibling under it is untouched, and the
// report names each corridor separately.
func TestRotateIsPerCorridor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		r := fixedRecord()
		r.Corridor = "USDC-NGNC"
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 7; i++ {
		r := fixedRecord()
		r.Corridor = "USDC-GHSC"
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	rot, err := s.Rotate(ctx(), 6)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if len(rot) != 1 {
		t.Fatalf("Rotate returned %d rotations, want 1 (only USDC-GHSC is over the ceiling)", len(rot))
	}
	if rot[0].Corridor != "USDC-GHSC" || rot[0].RecordsAfter != 6 {
		t.Errorf("rotation = %+v, want USDC-GHSC trimmed to 6", rot[0])
	}
	for _, c := range []string{"USDC-NGNC", "USDC-GHSC"} {
		if err := s.Verify(ctx(), c); err != nil {
			t.Errorf("Verify %s: %v", c, err)
		}
	}
	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil || len(all) != 5 {
		t.Errorf("USDC-NGNC window length = %d (%v), want 5 (untouched)", len(all), err)
	}
}

// mustAll returns the corridor's whole chain, failing the test if it cannot.
func mustAll(t *testing.T, s *FileStore) []*Record {
	t.Helper()
	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	return all
}

// stripHashFields renders a record with the two hash fields removed, so a
// test can compare a record's measured contents across rotation. Only hash and
// prev_hash may move under rotation; everything else must be byte-identical.
// prev_hash is deliberately stripped too — it is part of the preimage and the
// window head resets it — while every field a measurement actually carries
// stays in the rendered form.
func stripHashFields(t *testing.T, r *Record) string {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	delete(m, "hash")
	delete(m, "prev_hash")
	norm, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(norm)
}
