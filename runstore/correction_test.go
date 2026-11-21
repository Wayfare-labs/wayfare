package runstore

import (
	"context"
	"testing"
	"time"
)

func TestCorrectionRecord_AppendAndVerify(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	corridor := "USDC-NGNC"

	// Record 1: normal run
	r1 := &Record{
		Corridor:     corridor,
		RecordedAt:   time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC),
		Integrity:    "DIRECT",
		FloorLossPct: "25.00",
		WorstLossPct: "30.00",
		Finding:      "Initial measurement",
	}
	if err := store.Append(ctx, r1); err != nil {
		t.Fatalf("Append r1: %v", err)
	}

	// Record 2: buggy run that reported wrong loss
	r2 := &Record{
		Corridor:     corridor,
		RecordedAt:   time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC),
		Integrity:    "DIRECT",
		FloorLossPct: "95.00", // bug!
		WorstLossPct: "99.00",
		Finding:      "Wrong calculation due to fee denominator bug",
	}
	if err := store.Append(ctx, r2); err != nil {
		t.Fatalf("Append r2: %v", err)
	}

	// Record 3: subsequent normal run
	r3 := &Record{
		Corridor:     corridor,
		RecordedAt:   time.Date(2026, 11, 20, 22, 0, 0, 0, time.UTC),
		Integrity:    "DIRECT",
		FloorLossPct: "24.50",
		WorstLossPct: "29.00",
		Finding:      "Subsequent measurement",
	}
	if err := store.Append(ctx, r3); err != nil {
		t.Fatalf("Append r3: %v", err)
	}

	// Record 4: Correction record targeting seq 2 (issue #729)
	r4 := &Record{
		Corridor:     corridor,
		RecordedAt:   time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC),
		Integrity:    "DIRECT",
		FloorLossPct: "24.80", // recomputed correct figure
		WorstLossPct: "29.50",
		Finding:      "Corrected: fixed fee denominator in rung pricing",
		Correction: &Correction{
			TargetSeq: 2,
			Reason:    "fee denominator bug in route engine was miscomputing effective rate",
		},
	}
	if err := store.Append(ctx, r4); err != nil {
		t.Fatalf("Append correction r4: %v", err)
	}

	// Verification of entire chain must succeed
	if err := store.Verify(ctx, corridor); err != nil {
		t.Fatalf("Verify chain with correction record failed: %v", err)
	}

	all, err := store.All(ctx, corridor)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 records on disk, got %d", len(all))
	}
	if all[3].Version != Version {
		t.Errorf("expected correction record version %d, got %d", Version, all[3].Version)
	}
	if !all[3].IsCorrection() {
		t.Errorf("expected all[3].IsCorrection() == true")
	}

	// Test EffectiveRecords
	effective := EffectiveRecords(all)
	if len(effective) != 3 {
		t.Fatalf("expected 3 effective records (seq 1, corrected seq 2, seq 3), got %d", len(effective))
	}
	if effective[1].Seq != 2 {
		t.Errorf("expected effective record seq 2, got %d", effective[1].Seq)
	}
	if effective[1].FloorLossPct != "24.80" {
		t.Errorf("expected effective floor loss 24.80, got %s", effective[1].FloorLossPct)
	}
	if !effective[1].RecordedAt.Equal(r2.RecordedAt) {
		t.Errorf("expected original timestamp preserved, got %v vs %v", effective[1].RecordedAt, r2.RecordedAt)
	}

	// Re-open store from disk and assert clean startup verification
	store2, err := Open(dir)
	if err != nil {
		t.Fatalf("Re-opening store with correction records failed: %v", err)
	}
	if err := store2.Verify(ctx, corridor); err != nil {
		t.Fatalf("store2.Verify failed: %v", err)
	}
}

func TestCorrectionRecord_RejectsInvalidTargetSeq(t *testing.T) {
	r := &Record{
		Version:  5,
		Seq:      2,
		Corridor: "USDC-NGNC",
		Correction: &Correction{
			TargetSeq: 5, // cannot target a future seq >= r.Seq!
			Reason:    "invalid forward reference",
		},
	}
	err := verifyChain("USDC-NGNC", []*Record{r})
	if err == nil {
		t.Errorf("expected error for correction targeting future seq, got nil")
	}
}

func TestCorrectionRecord_RejectsEmptyReason(t *testing.T) {
	r := &Record{
		Version:  5,
		Seq:      2,
		Corridor: "USDC-NGNC",
		Correction: &Correction{
			TargetSeq: 1,
			Reason:    "   ",
		},
	}
	err := verifyChain("USDC-NGNC", []*Record{r})
	if err == nil {
		t.Errorf("expected error for empty correction reason, got nil")
	}
}
