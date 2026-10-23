package runstore

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jsonMarshal serialises a record to one NDJSON line, the same encoding
// Append uses.
func jsonMarshal(r *Record) ([]byte, error) {
	return json.Marshal(r)
}

// These tests exercise the rollback path documented in docs/rollback.md
// (issue #318): a deployment returned to an earlier image must be able to
// prove, with one -verify-store run, that the chain it now serves is the one
// that was written.
//
// The load path is the thing under test — a rolled-back image does not
// "migrate" anything, it simply opens the store it finds. So every test here
// drives Open (or OpenFS) over chains written by another build shape, the
// same bytes the older process would read from disk.

// rollbackFixture is a chain-shape helper: it writes records sequentially
// with correct prev_hash links, exactly as Append would, but without needing
// a live store — the records are sealed against each other directly. This is
// the byte-level truth of what lands on disk, which is what an older image
// actually reads.
type rollbackFixture struct {
	t       *testing.T
	dir     string
	corridr string
	prev    string
	seq     int64
}

func newRollbackFixture(t *testing.T, corridor string) *rollbackFixture {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return &rollbackFixture{t: t, dir: dir, corridr: corridor, prev: GenesisPrevHash}
}

// append seals and writes one record continuing the chain, returning it.
func (f *rollbackFixture) append(version int) *Record {
	f.t.Helper()
	r := fixedRecord()
	r.Version = version
	r.Seq = f.seq + 1
	r.PrevHash = f.prev
	r.Corridor = f.corridr
	r.RecordedAt = time.Date(2026, 9, 1, 0, 0, int(r.Seq), 0, time.UTC)
	r.Checks = nil
	r.Metrics = nil
	if version < Version {
		// Shape the record back to the legacy schema: fields the older
		// format did not carry are empty, which is how they were encoded
		// when that version was current.
		r.Reference.FetchedAt = ""
	}
	if err := r.Seal(); err != nil {
		f.t.Fatalf("sealing version %d record: %v", version, err)
	}
	line, err := jsonMarshal(r)
	if err != nil {
		f.t.Fatalf("encoding record: %v", err)
	}
	path := filepath.Join(f.dir, strings.ToUpper(f.corridr)+FileExt)
	// Open in append mode so successive records extend the file the way the
	// real writer does.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		f.t.Fatalf("opening chain file: %v", err)
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		f.t.Fatalf("appending record: %v", err)
	}
	if err := file.Close(); err != nil {
		f.t.Fatalf("closing chain file: %v", err)
	}
	f.prev = r.Hash
	f.seq = r.Seq
	return r
}

// TestRollbackOlderBuildLoadsWithNewerRecords is situation 1 of the rollback
// doc: the older image was built from a commit whose schema matches, so the
// chain on disk — including records written after that image existed — must
// load and verify through a plain Open. A rollback never re-writes history;
// it re-reads it.
func TestRollbackOlderBuildLoadsWithNewerRecords(t *testing.T) {
	// Write a chain with the current build's writer, the way the measure
	// workflow does: three records over two "deploys".
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := s.Append(context.Background(), buildValidRecord(int64(i+1), "", "USDC-NGNC")); err != nil {
			// buildValidRecord carries its own prev hash; Append re-links.
			t.Fatalf("append %d: %v", i+1, err)
		}
	}
	if err := s.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Fatalf("pre-rollback verify: %v", err)
	}

	// The "older image": a brand-new store object over the same bytes. That
	// is all a rollback is to the chain — a fresh reader over records it did
	// not write.
	reopened, err := Open(dir)
	if err != nil {
		t.Fatalf("post-rollback open refused a chain written by the same schema: %v", err)
	}
	if err := reopened.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("post-rollback verify: %v", err)
	}
	latest, err := reopened.Latest(context.Background(), "USDC-NGNC")
	if err != nil || latest == nil {
		t.Fatalf("post-rollback latest: %v, %v", latest, err)
	}
	if latest.Seq != 3 {
		t.Errorf("post-rollback tip seq = %d, want 3; a rollback must not lose records", latest.Seq)
	}
}

// TestRollbackNewerBuildLoadsWithOlderRecords is the safe direction of
// situation 2: a newer build reads a chain containing records shaped for
// older schemas. Every migration so far added its fields with omitempty after
// every earlier field, so a legacy record encodes byte-for-byte as it did and
// verifies unchanged. If this stops holding, a rollback forward past a schema
// change would strand every deployment still running the old chain — which is
// why the guarantee is pinned rather than assumed.
func TestRollbackNewerBuildLoadsWithOlderRecords(t *testing.T) {
	f := newRollbackFixture(t, "USDC-NGNC")
	legacy := f.append(1)
	f.append(Version)

	store, err := OpenFS(os.DirFS(f.dir), ".")
	if err != nil {
		t.Fatalf("Open refused a mixed legacy/current chain: %v", err)
	}
	if err := store.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Fatalf("mixed chain does not verify: %v", err)
	}
	all, err := store.All(context.Background(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("chain holds %d records, want 2", len(all))
	}
	if all[0].Hash != legacy.Hash {
		t.Error("the legacy record's hash changed on load; an older schema must verify unchanged")
	}
	if all[1].Version != Version || all[0].Version != 1 {
		t.Errorf("record versions = %d, %d; a rollback reader must not relabel what it reads", all[0].Version, all[1].Version)
	}
}

// TestRollbackOlderBuildRefusesUnknownFutureVersion is the refusal half of
// situation 2, asserted rather than narrated: when the chain carries a schema
// the reading build does not know, Open fails with a message that names the
// version — the operator sees the mismatch, not a guess.
func TestRollbackOlderBuildRefusesUnknownFutureVersion(t *testing.T) {
	f := newRollbackFixture(t, "USDC-NGNC")
	f.append(Version)
	// A record from a hypothetical future schema.
	future := f.append(Version)
	future.Version = Version + 1
	if err := future.Seal(); err != nil {
		t.Fatal(err)
	}
	line, err := jsonMarshal(future)
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite the second record's line with the future-shaped one.
	path := filepath.Join(f.dir, "USDC-NGNC.ndjson")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)
	if len(lines) != 2 {
		t.Fatalf("fixture expected two lines, got %d", len(lines))
	}
	_ = os.WriteFile(path, []byte(lines[0]+"\n"+strings.TrimSpace(string(line))+"\n"), 0o644)

	_, err = Open(f.dir)
	if err == nil {
		t.Fatal("Open accepted a record version it does not know; the version-mismatch rule failed")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("refusal should name the version, got: %v", err)
	}
}

// TestRollbackFreshVolumeStartsClean covers the rolled-back image pointed at
// an empty volume: the chain starts from genesis, and the first append after
// the rollback continues it. Nothing about a rollback may prevent a store
// from recording again.
func TestRollbackFreshVolumeStartsClean(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open on a fresh volume: %v", err)
	}
	corridors, err := s.Corridors(context.Background())
	if err != nil || len(corridors) != 0 {
		t.Fatalf("fresh volume reported corridors %v (%v), want none", corridors, err)
	}
	rec := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	if err := s.Append(context.Background(), rec); err != nil {
		t.Fatalf("first append after rollback: %v", err)
	}
	if rec.Seq != 1 || rec.PrevHash != GenesisPrevHash {
		t.Errorf("post-rollback first record = seq %d prev %s, want seq 1 from genesis", rec.Seq, rec.PrevHash)
	}
	if err := s.Verify(context.Background(), "USDC-NGNC"); err != nil {
		t.Errorf("chain after first post-rollback append: %v", err)
	}
}

// TestRollbackPreservesServedProvenance ties the rollback to the reader's
// contract: the record an older image serves must carry the recorded_at and
// measured fields the newer image served, so staleness after a rollback is
// visible in stale.age_human rather than disguised. The measured fields of a
// record are byte-identical across load/save; this pins the round trip.
func TestRollbackPreservesServedProvenance(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	if err := s.Append(context.Background(), want); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Latest(context.Background(), "USDC-NGNC")
	if err != nil || got == nil {
		t.Fatalf("latest after rollback: %v, %v", got, err)
	}
	if !got.RecordedAt.Equal(want.RecordedAt) {
		t.Errorf("recorded_at = %v, want %v; a rollback must not shift provenance", got.RecordedAt, want.RecordedAt)
	}
	if got.Integrity != want.Integrity || got.FloorLossPct != want.FloorLossPct {
		t.Error("measured fields changed across a rollback round trip")
	}
}
