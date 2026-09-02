package main

import (
	"strings"
	"testing"
)

// TestAnalyseReproducesDocFigures pins the exact numbers docs/native-xlm-routing.md
// prints, so a change in the classifier or in the fixture set fails the test
// rather than silently drifting the doc away from what the tool computes.
func TestAnalyseReproducesDocFigures(t *testing.T) {
	report, err := Analyse("../../testdata/snapshots")
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}

	byRecv := map[string]CorridorReport{}
	for _, c := range report.Corridors {
		byRecv[c.ReceiveCode] = c
	}

	// KESC — NO-MARKET, no paths at any size.
	kesc, ok := byRecv["KESC"]
	if !ok {
		t.Fatal("no KESC snapshot in report")
	}
	if kesc.SizesWithAnyPath != 0 {
		t.Errorf("KESC: SizesWithAnyPath = %d, want 0 (NO-MARKET)", kesc.SizesWithAnyPath)
	}
	if !strings.Contains(kesc.SummaryLine, "no paths at any") {
		t.Errorf("KESC: summary = %q, want it to name NO-MARKET explicitly", kesc.SummaryLine)
	}

	// NGNC — direct corridor, best-path uses XLM at 10 of 12 sizes.
	ngnc, ok := byRecv["NGNC"]
	if !ok {
		t.Fatal("no NGNC snapshot in report")
	}
	if ngnc.SizesMeasured != 12 {
		t.Errorf("NGNC: SizesMeasured = %d, want 12", ngnc.SizesMeasured)
	}
	if ngnc.SizesWithAnyPath != 12 {
		t.Errorf("NGNC: SizesWithAnyPath = %d, want 12", ngnc.SizesWithAnyPath)
	}
	if ngnc.SizesBestUsesXLM != 10 {
		t.Errorf("NGNC: SizesBestUsesXLM = %d, want 10 — doc says 10/12", ngnc.SizesBestUsesXLM)
	}
	if ngnc.SizesWithNonXLM != 12 {
		t.Errorf("NGNC: SizesWithNonXLM = %d, want 12 — a non-XLM alternative exists at every size",
			ngnc.SizesWithNonXLM)
	}

	// GHSC — derivative corridor via NGNC, best-path uses XLM at 11 of 12 sizes.
	ghsc, ok := byRecv["GHSC"]
	if !ok {
		t.Fatal("no GHSC snapshot in report")
	}
	if ghsc.SizesBestUsesXLM != 11 {
		t.Errorf("GHSC: SizesBestUsesXLM = %d, want 11 — doc says 11/12", ghsc.SizesBestUsesXLM)
	}

	// Spot-check the monotonic XLM advantage on the largest NGNC size, since
	// the doc's headline claim (500%+ at 5000 USDC) is what a reader is most
	// likely to be surprised by. The exact figure is 544.88%; a change here
	// means the doc must be re-generated.
	var top *SizeBreakdown
	for i := range ngnc.Sizes {
		if ngnc.Sizes[i].SendAmount == "5000" {
			top = &ngnc.Sizes[i]
			break
		}
	}
	if top == nil {
		t.Fatal("NGNC: no 5000 size in the recorded report")
	}
	if top.XLMAdvantagePc != "544.88" {
		t.Errorf("NGNC 5000: XLM advantage = %q, want %q (drift from the doc)",
			top.XLMAdvantagePc, "544.88")
	}
}

// TestAnalyseSkipsInvalidSiblings pins that a directory that is not a valid
// snapshot is skipped rather than aborting the whole run. Otherwise a single
// bad sibling in testdata/snapshots would hide every good corridor and turn
// the tool into a false negative for issue #101.
func TestAnalyseSkipsInvalidSiblings(t *testing.T) {
	// testdata/snapshots contains three real snapshots; Analyse should return
	// three CorridorReport entries and no error.
	report, err := Analyse("../../testdata/snapshots")
	if err != nil {
		t.Fatalf("Analyse: %v", err)
	}
	if len(report.Corridors) != 3 {
		t.Errorf("got %d corridors, want 3 (NGNC, GHSC, KESC)", len(report.Corridors))
	}
}
