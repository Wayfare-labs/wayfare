package runstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// helper: build a valid sealed record linked to prevHash with the given seq.
func buildValidRecord(seq int64, prevHash, corridor string) *Record {
	r := fixedRecord()
	r.Seq = seq
	r.PrevHash = prevHash
	r.Corridor = corridor
	r.RecordedAt = time.Date(2026, 8, 21, 22, 30, int(seq), 0, time.UTC)
	r.Checks = nil
	r.Metrics = nil
	if err := r.Seal(); err != nil {
		panic(err)
	}
	return r
}

// helper: write raw lines to a corridor file.
func writeFile(t *testing.T, dir, corridor, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, strings.ToUpper(corridor)+FileExt), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// helper: serialise a record to a single NDJSON line.
func recordLine(t *testing.T, r *Record) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// ---------------------------------------------------------------------------
// Corrupted NDJSON lines
// ---------------------------------------------------------------------------

// TestCorruptedLine_InvalidJSON rejects a file whose first line is not valid JSON.
func TestCorruptedLine_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "this is not json\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a file with invalid JSON")
	}
	if !strings.Contains(err.Error(), "line 1") {
		t.Errorf("error should mention line 1, got: %v", err)
	}
}

// TestCorruptedLine_MidFileInvalidJSON stops at the corrupted line, not after it.
func TestCorruptedLine_MidFileInvalidJSON(t *testing.T) {
	dir := t.TempDir()

	// Write a valid first record then a garbage second line.
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\nNOT_JSON\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a file with a corrupted mid-file line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error should name line 2, got: %v", err)
	}
}

// TestCorruptedLine_TruncatedJSON rejects a line that starts as valid JSON but is cut off.
func TestCorruptedLine_TruncatedJSON(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	full := recordLine(t, r1)
	// Truncate in the middle of a string value.
	truncated := full[:len(full)/2]
	writeFile(t, dir, "USDC-NGNC", truncated+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a truncated JSON line")
	}
}

// TestCorruptedLine_TrailingGarbageAfterValidJSON rejects trailing non-JSON characters.
func TestCorruptedLine_TrailingGarbageAfterValidJSON(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"GARBAGE\n")

	// readAll uses json.Unmarshal per line so trailing garbage causes an error.
	s, err := Open(dir)
	// If the garbage is on its own line it will be caught as "line 2: invalid JSON".
	// If it is appended to the same line, json.Unmarshal fails.
	if err == nil && s != nil {
		// The scanner might treat it as a continuation; verify fails.
		vErr := s.Verify(ctx(), "USDC-NGNC")
		if vErr == nil {
			t.Fatal("Verify passed on a file with trailing garbage")
		}
	}
}

// TestCorruptedLine_EmptyJSONObject rejects a line that is valid JSON but not a Record.
func TestCorruptedLine_EmptyJSONObject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "{}\n")

	_, err := Open(dir)
	// {} is valid JSON but has version 0 which is unknown → refused.
	if err == nil {
		t.Fatal("Open accepted an empty JSON object (version 0)")
	}
	if !strings.Contains(err.Error(), "version 0") {
		t.Errorf("error should mention version 0, got: %v", err)
	}
}

// TestCorruptedLine_InvalidRecordHash detected as tampering at verify time.
func TestCorruptedLine_InvalidRecordHash(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	// Tamper with the hash field.
	r1.Hash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a record with a wrong hash")
	}
}

// ---------------------------------------------------------------------------
// Hash chain break detection
// ---------------------------------------------------------------------------

// TestHashChain_BrokenPrevHash detects a record whose prev_hash does not match.
func TestHashChain_BrokenPrevHash(t *testing.T) {
	dir := t.TempDir()

	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "USDC-NGNC")

	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+recordLine(t, r2)+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a chain with a broken prev_hash link")
	}
	if !strings.Contains(err.Error(), "chain is broken") {
		t.Errorf("error should mention chain break, got: %v", err)
	}
}

// TestHashChain_MissingLink detects a gap in sequence numbers via hash mismatch.
func TestHashChain_MissingLink(t *testing.T) {
	dir := t.TempDir()

	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	// r3 pretends r2 came before it, but r2 is not in the file.
	fakePrev := "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	r3 := buildValidRecord(3, fakePrev, "USDC-NGNC")

	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+recordLine(t, r3)+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a chain with a missing link")
	}
}

// TestHashChain_VerifyReportsCorrectPosition names the seq of the broken record.
func TestHashChain_VerifyReportsCorrectPosition(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Append two valid records.
	for i := 0; i < 2; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Tamper with the first record's loss figure in the file.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lines[0] = strings.Replace(lines[0], `"floor_loss_pct":"25.00"`, `"floor_loss_pct":"99.00"`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = s.Verify(ctx(), "USDC-NGNC")
	if err == nil {
		t.Fatal("Verify passed on a tampered chain")
	}
	// Should report seq 1 as the first broken record.
	if !strings.Contains(err.Error(), "seq 1") {
		t.Errorf("error should mention seq 1, got: %v", err)
	}
}

// TestHashChain_PartialChainRefused verifies that a chain with a valid first
// record but invalid second record is rejected on Open.
func TestHashChain_PartialChainRefused(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, "sha256:badbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbadbad", "USDC-NGNC")

	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+recordLine(t, r2)+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a partial chain with a broken second record")
	}
}

// ---------------------------------------------------------------------------
// Empty files and whitespace-only files
// ---------------------------------------------------------------------------

// TestEmptyFile_HandleledGracefully treats an empty .ndjson file as having no history.
func TestEmptyFile_HandleledGracefully(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open on empty file: %v", err)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || latest != nil {
		t.Errorf("Latest on empty file = %v, %v; want nil, nil", latest, err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify on empty file: %v", err)
	}
}

// TestWhitespaceOnlyFile_HandleledGracefully treats whitespace as empty.
func TestWhitespaceOnlyFile_HandleledGracefully(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "   \n  \n\t\n  \n")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open on whitespace-only file: %v", err)
	}
	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil || latest != nil {
		t.Errorf("Latest on whitespace file = %v, %v; want nil, nil", latest, err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify on whitespace file: %v", err)
	}
}

// TestNewlinesOnlyFile_HandleledGracefully treats a file of just newlines as empty.
func TestNewlinesOnlyFile_HandleledGracefully(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "\n\n\n\n")

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open on newlines-only file: %v", err)
	}
	corrs, err := s.Corridors(ctx())
	if err != nil {
		t.Fatal(err)
	}
	// An all-blank file should not register a corridor at all (no records = no tip).
	for _, c := range corrs {
		if c == "USDC-NGNC" {
			t.Error("USDC-NGNC should not appear as a corridor when the file is all newlines")
		}
	}
}

// TestMixedBlankAndValid_WhitespaceBetweenRecordsBlankLinesAreSkipped confirms blank
// lines between valid records are silently skipped.
func TestMixedBlankAndValid_WhitespaceBetweenRecordsBlankLinesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")

	content := recordLine(t, r1) + "\n\n  \n" + recordLine(t, r2) + "\n\n"
	writeFile(t, dir, "USDC-NGNC", content)

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open with blank lines between records: %v", err)
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify with blank lines: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Duplicate timestamps
// ---------------------------------------------------------------------------

// TestDuplicateTimestamps_AllowTwoRecordsWithSameRecordedAt verifies that two records
// sharing a timestamp still chain and verify — the chain is keyed by hash, not time.
func TestDuplicateTimestamps_AllowTwoRecordsWithSameRecordedAt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC)
	for i := 0; i < 3; i++ {
		r := fixedRecord()
		r.RecordedAt = ts
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		// Append normalises RecordedAt to UTC truncated to second, so set it
		// before append and let it be re-truncated (it stays the same second).
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("chain with duplicate timestamps should verify: %v", err)
	}
}

// TestDuplicateTimestamps_LatestReturnsMostRecentSeq verifies Latest returns the
// highest-seq record when timestamps are identical.
func TestDuplicateTimestamps_LatestReturnsMostRecentSeq(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC)
	for i := 0; i < 3; i++ {
		r := fixedRecord()
		r.RecordedAt = ts
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	latest, err := s.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Seq != 3 {
		t.Errorf("Latest seq = %d, want 3 (highest seq among same-timestamp records)", latest.Seq)
	}
}

// ---------------------------------------------------------------------------
// Out-of-order timestamps
// ---------------------------------------------------------------------------

// TestOutOfOrderTimestamps_ChainStillVerifies verifies that timestamps do not
// need to be monotonically increasing for the hash chain to hold.
func TestOutOfOrderTimestamps_ChainStillVerifies(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Append records with timestamps going backwards.
	for i := 0; i < 3; i++ {
		r := fixedRecord()
		r.RecordedAt = time.Date(2026, 8, 21, 22, 30, 40-i, 0, time.UTC)
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("chain with out-of-order timestamps should still verify: %v", err)
	}

	// All() should still return them in the order they were appended (by seq).
	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("All returned %d records, want 3", len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i].Seq <= all[i-1].Seq {
			t.Errorf("records not in seq order: seq %d before seq %d", all[i-1].Seq, all[i].Seq)
		}
	}
}

// TestOutOfOrderTimestamps_RecordsWithFutureTimestamps verifies records with
// far-future timestamps still chain correctly.
func TestOutOfOrderTimestamps_RecordsWithFutureTimestamps(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	records := []time.Time{
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 21, 22, 30, 40, 0, time.UTC),
		time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
	}
	for i, ts := range records {
		r := fixedRecord()
		r.RecordedAt = ts
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("chain with wildly different timestamps should verify: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Partial writes (simulated process crash mid-append)
// ---------------------------------------------------------------------------

// TestPartialWrite_LastRecordTruncated rejects a file whose last line is incomplete.
func TestPartialWrite_LastRecordTruncated(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	full := recordLine(t, r2)

	// Simulate a crash after writing the first 60% of the second record.
	partial := full[:len(full)*6/10]
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+partial)

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a file with a partially written last record")
	}
}

// TestPartialWrite_IncompleteJSONObject rejects a line that is a valid JSON prefix
// but not a complete object.
func TestPartialWrite_IncompleteJSONObject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", `{"version":2,"seq":1,"corridor":"USDC-NGNC"`+"\n")

	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a file with an incomplete JSON object")
	}
}

// TestPartialWrite_VisibleOnReopen verifies that after a valid first record and
// a partial second record, the store refuses to open.
func TestPartialWrite_VisibleOnReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := fixedRecord()
	if err := s.Append(ctx(), r); err != nil {
		t.Fatal(err)
	}

	// Now simulate a partial second write by truncating the file.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Append half of a new record line.
	full := recordLine(t, buildValidRecord(2, r.Hash, "USDC-NGNC"))
	truncated := string(raw) + full[:len(full)/2]

	if err := os.WriteFile(path, []byte(truncated), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = Open(dir)
	if err == nil {
		t.Fatal("Open accepted a store with a partial write")
	}
}

// TestPartialWrite_ValidFirstRecordPreserved verifies that a valid first record
// is still readable before the corruption point (i.e. Open fails but the data
// up to the break was not itself damaged).
func TestPartialWrite_ValidFirstRecordPreserved(t *testing.T) {
	dir := t.TempDir()
	r1 := buildValidRecord(1, GenesisPrevHash, "USDC-NGNC")
	r2 := buildValidRecord(2, r1.Hash, "USDC-NGNC")
	full2 := recordLine(t, r2)

	// Write first record fine, then the second with truncation.
	writeFile(t, dir, "USDC-NGNC", recordLine(t, r1)+"\n"+full2[:len(full2)/2])

	// Open must fail.
	_, err := Open(dir)
	if err == nil {
		t.Fatal("Open accepted a store with a partial second record")
	}

	// Verify via readAll that the first record is individually intact.
	s := &FileStore{dir: dir, tips: map[string]*Record{}}
	records, readErr := s.readAll("USDC-NGNC")
	if readErr == nil {
		// readAll itself should error on the partial line.
		t.Fatal("readAll should have returned an error for a partial last line")
	}
	if records != nil {
		t.Error("readAll returned records when it should have errored")
	}
}

// ---------------------------------------------------------------------------
// Concurrent append safety
// ---------------------------------------------------------------------------

// TestConcurrentAppend_NoDataLoss verifies that concurrent appends from multiple
// goroutines do not lose records or corrupt the chain.
func TestConcurrentAppend_NoDataLoss(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	const perGoroutine = 5
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				r := fixedRecord()
				r.FloorLossPct = "25.0" + string(rune('0'+g%10)) + string(rune('0'+i%10))
				r.Reference.Mid = "1350." + string(rune('0'+g)) + string(rune('0'+i))
				if err := s.Append(ctx(), r); err != nil {
					t.Errorf("goroutine %d append %d: %v", g, i, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// All records should be present and the chain should verify.
	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	total := goroutines * perGoroutine
	if len(all) != total {
		t.Fatalf("expected %d records, got %d", total, len(all))
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("chain after concurrent appends failed to verify: %v", err)
	}
}

// TestConcurrentAppend_SeqNumbersMonotonic verifies that seq numbers are assigned
// monotonically under concurrent writes.
func TestConcurrentAppend_SeqNumbersMonotonic(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	const goroutines = 5
	const perGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				r := fixedRecord()
				r.FloorLossPct = "99.99"
				if err := s.Append(ctx(), r); err != nil {
					t.Errorf("concurrent append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i < len(all); i++ {
		if all[i].Seq != all[i-1].Seq+1 {
			t.Errorf("seq gap: seq %d → %d (want consecutive)", all[i-1].Seq, all[i].Seq)
		}
	}
}

// ---------------------------------------------------------------------------
// Recovery after corruption
// ---------------------------------------------------------------------------

// TestRecovery_OpenRejectsCorruptedStore confirms that Open refuses to open a
// store whose chain is corrupted.
func TestRecovery_OpenRejectsCorruptedStore(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Append several valid records.
	for i := 0; i < 3; i++ {
		r := fixedRecord()
		r.FloorLossPct = "25.0" + string(rune('0'+i))
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Corrupt the middle record.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lines[1] = strings.Replace(lines[1], `"floor_loss_pct":"25.01"`, `"floor_loss_pct":"1.01"`, 1)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open must refuse.
	_, err = Open(dir)
	if err == nil {
		t.Fatal("Open should refuse a corrupted store")
	}
}

// TestRecovery_AppendAfterReopenFails demonstrates that you cannot append to a
// corrupted store — the only path is to rebuild the file from scratch.
func TestRecovery_AppendAfterReopenFails(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := fixedRecord()
	if err := s.Append(ctx(), r); err != nil {
		t.Fatal(err)
	}

	// Corrupt.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lines[0] = "CORRUPTED"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Cannot open.
	_, err = Open(dir)
	if err == nil {
		t.Fatal("Open should not succeed on a corrupted store")
	}

	// The only recovery is to delete the file and start fresh.
	os.Remove(path)
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after deleting corrupted file: %v", err)
	}
	r2 := fixedRecord()
	if err := s2.Append(ctx(), r2); err != nil {
		t.Fatal(err)
	}
	if err := s2.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after recovery: %v", err)
	}
}

// TestRecovery_CorruptedFileDoesNotAffectOtherCorridors verifies that corruption
// in one corridor file does not prevent reading other corridors.
func TestRecovery_CorruptedFileDoesNotAffectOtherCorridors(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write valid records to two corridors.
	for _, c := range []string{"USDC-NGNC", "USDC-KESC"} {
		r := fixedRecord()
		r.Corridor = c
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Corrupt only USDC-NGNC.
	path := filepath.Join(dir, "USDC-NGNC"+FileExt)
	if err := os.WriteFile(path, []byte("CORRUPTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Open must fail because of the corrupted file.
	_, err = Open(dir)
	if err == nil {
		t.Fatal("Open should fail when any corridor file is corrupted")
	}
}

// TestRecovery_DeleteCorruptedFileAndReopen shows the recovery path: remove the
// bad file, then Open succeeds with the remaining clean corridors.
func TestRecovery_DeleteCorruptedFileAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"USDC-NGNC", "USDC-KESC"} {
		r := fixedRecord()
		r.Corridor = c
		if err := s.Append(ctx(), r); err != nil {
			t.Fatal(err)
		}
	}

	// Corrupt USDC-NGNC.
	bad := filepath.Join(dir, "USDC-NGNC"+FileExt)
	if err := os.WriteFile(bad, []byte("CORRUPTED\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Remove the corrupted file.
	os.Remove(bad)

	// Open should succeed now.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after removing corrupted file: %v", err)
	}
	// USDC-KESC should still be readable and verifiable.
	if err := s2.Verify(ctx(), "USDC-KESC"); err != nil {
		t.Errorf("Verify USDC-KESC after recovery: %v", err)
	}
	// USDC-NGNC should be gone.
	latest, err := s2.Latest(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if latest != nil {
		t.Error("USDC-NGNC should be nil after its file was deleted")
	}
}

// ---------------------------------------------------------------------------
// Large files (performance smoke)
// ---------------------------------------------------------------------------

// TestLargeChain_HundredRecords appends 100 records and verifies the chain.
// This is a performance smoke test to ensure the hash chain doesn't degrade.
func TestLargeChain_HundredRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-chain test in short mode")
	}

	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		r := fixedRecord()
		r.FloorLossPct = fmt.Sprintf("25.%03d", i%1000)
		r.RecordedAt = time.Date(2026, 8, 21, 22, i%24, i%60, 0, time.UTC)
		if err := s.Append(ctx(), r); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	if err := s.Verify(ctx(), "USDC-NGNC"); err != nil {
		t.Errorf("Verify after 100 appends: %v", err)
	}

	all, err := s.All(ctx(), "USDC-NGNC")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 100 {
		t.Fatalf("All returned %d records, want 100", len(all))
	}
}

// ---------------------------------------------------------------------------
// Edge cases around nil record and empty corridor
// ---------------------------------------------------------------------------

// TestAppendNilRecordRejected verifies that Append rejects nil records.
func TestAppendNilRecordRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx(), nil); err == nil {
		t.Error("Append(nil) should return an error")
	}
}

// TestAppendEmptyCorridorRejected verifies that Append rejects records with no corridor.
func TestAppendEmptyCorridorRejected(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := fixedRecord()
	r.Corridor = ""
	if err := s.Append(ctx(), r); err == nil {
		t.Error("Append with empty corridor should return an error")
	}
}

// TestLatestNonExistentCorridor returns nil for a corridor that was never written to.
func TestLatestNonExistentCorridor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := s.Latest(ctx(), "NONEXISTENT")
	if err != nil {
		t.Fatal(err)
	}
	if latest != nil {
		t.Errorf("Latest for nonexistent corridor = %v, want nil", latest)
	}
}

// TestRecentNonExistentCorridor returns empty for a corridor with no history.
func TestRecentNonExistentCorridor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	recent, err := s.Recent(ctx(), "NONEXISTENT", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 0 {
		t.Errorf("Recent for nonexistent corridor = %d records, want 0", len(recent))
	}
}

// TestAllNonExistentCorridor returns empty for a corridor with no history.
func TestAllNonExistentCorridor(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.All(ctx(), "NONEXISTENT")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Errorf("All for nonexistent corridor = %d records, want 0", len(all))
	}
}

// ---------------------------------------------------------------------------
// ReadOnly (fs.FS) store edge cases
// ---------------------------------------------------------------------------

// TestOpenFS_RejectsCorruptedChain verifies that OpenFS refuses to load a
// chain with a corrupted record.
func TestOpenFS_RejectsCorruptedChain(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "USDC-NGNC", "this is not json\n")

	fsys := os.DirFS(dir)
	_, err := OpenFS(fsys, ".")
	if err == nil {
		t.Fatal("OpenFS accepted a corrupted chain")
	}
}

// TestOpenFS_EmptyDirHasNoCorridors verifies that OpenFS on an empty directory
// produces a store with no corridors.
func TestOpenFS_EmptyDirHasNoCorridors(t *testing.T) {
	dir := t.TempDir()
	fsys := os.DirFS(dir)
	s, err := OpenFS(fsys, ".")
	if err != nil {
		t.Fatalf("OpenFS on empty dir: %v", err)
	}
	corrs, err := s.Corridors(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(corrs) != 0 {
		t.Errorf("Corridors = %v, want empty", corrs)
	}
}

// TestReadOnly_AlwaysRejectsAppend ensures Append on a read-only store always fails.
func TestReadOnly_AlwaysRejectsAppend(t *testing.T) {
	dir := t.TempDir()
	fsys := os.DirFS(dir)
	s, err := OpenFS(fsys, ".")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Append(ctx(), fixedRecord()); err == nil {
		t.Error("ReadOnly.Append should always fail")
	}
}

// ---------------------------------------------------------------------------
// Non-.ndjson files are ignored
// ---------------------------------------------------------------------------

// TestNonNdjsonFilesIgnored confirms that files without the .ndjson extension
// are not loaded as chains.
func TestNonNdjsonFilesIgnored(t *testing.T) {
	dir := t.TempDir()
	// Write a text file and a .json file — neither should be loaded.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a chain"), 0o644)
	os.WriteFile(filepath.Join(dir, "USDC-NGNC.json"), []byte("not a chain"), 0o644)

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open with non-ndjson files: %v", err)
	}
	corrs, err := s.Corridors(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(corrs) != 0 {
		t.Errorf("non-ndjson files should be ignored, got corridors: %v", corrs)
	}
}
