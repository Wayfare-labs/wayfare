package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/snapshot"
)

// loadReverseSnapshot finds the recorded NGNC -> USDC exit-leg snapshot.
func loadReverseSnapshot(t *testing.T) *snapshot.Manifest {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join("..", "..", "testdata", "snapshots", "ngnc-usdc-*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no ngnc-usdc snapshot (err=%v); capture one with: "+
			"go run ./cmd/ladder -from NGNC -to USDC -record testdata/snapshots", err)
	}
	m, err := snapshot.Load(matches[0])
	if err != nil {
		t.Fatalf("loading %s: %v", matches[0], err)
	}
	return m
}

// TestReverseCorridorReplay pins the recorded exit leg (#596).
//
// The corridor is real: Horizon answers strict-send at every rung, routing
// NGNC through XLM into USDC, so its state is DIRECT — not NO-MARKET. What the
// measurement shows is different from a missing market: the exit leg exists
// and is ruinously expensive, which is exactly the distinction the issue asks
// a reverse corridor to be able to express. NO-MARKET stays reserved for a
// corridor Horizon answers with no path at all; the assertions below fix both
// the state and the loss curve against the recorded bytes.
func TestReverseCorridorReplay(t *testing.T) {
	m := loadReverseSnapshot(t)

	client := &dex.Client{HorizonURL: dex.DefaultHorizonURL, HTTPClient: m.HTTPClient()}
	ref := &refrate.ExchangeRateAPI{Client: m.HTTPClient()}
	eng := &route.Engine{DEX: client, RefRate: &refrate.Checked{Inner: ref}}

	sizes, err := parseSizes(strings.Join(m.Sizes, ","))
	if err != nil {
		t.Fatalf("parsing recorded sizes: %v", err)
	}

	result, err := eng.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.NGNC(),
		ReceiveAsset:   asset.USDC(),
		Sizes:          sizes,
		ReferenceBase:  "NGN",
		ReferenceQuote: "USD",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}

	// Every rung was answered by Horizon, so nothing is unmeasured.
	if result.Failed() || result.PartiallyFailed() {
		t.Fatalf("recorded ladder has holes: failed=%v partial=%v", result.Failed(), result.PartiallyFailed())
	}
	if result.Integrity != route.IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT: Horizon priced every size through XLM", result.Integrity)
	}

	// The exit leg is executable, and the measurement says how badly. Only
	// the two smallest-rung verdicts clear UNUSABLE; everything from 37,500
	// NGNC up is graded UNUSABLE. The floor therefore belongs to the 150
	// NGNC rung — the structural cost of leaving NGNC — not to a comfortable
	// size, and the worst case is the largest rung.
	if !result.Viable() {
		t.Fatal("Viable() = false; the recorded 150 NGNC rung was graded POOR and recommended")
	}
	if result.RecommendedSize.String() != "150" {
		t.Errorf("RecommendedSize = %s, want 150 NGNC: the only recommendable rung is the smallest",
			result.RecommendedSize)
	}
	if got := result.Recommended.LossPct.StringFixed(2); got != "10.03" {
		t.Errorf("Recommended loss = %s%%, want 10.03%%", got)
	}
	if result.FloorSize.String() != "150" {
		t.Errorf("FloorSize = %s, want the smallest rung 150 NGNC", result.FloorSize)
	}
	if got := result.Floor.StringFixed(2); got != "10.03" {
		t.Errorf("Floor = %s%%, want 10.03%% from the recorded 150 NGNC rung", got)
	}
	if result.WorstSize.String() != "7500000" {
		t.Errorf("WorstSize = %s, want the largest rung 7500000 NGNC", result.WorstSize)
	}
	if got := result.WorstLoss.StringFixed(2); got != "97.57" {
		t.Errorf("WorstLoss = %s%%, want 97.57%% at the largest rung", got)
	}

	// The sentence a reader sees must carry the size dependence: the best
	// available rung is a tiny one, and the worst is catastrophic.
	if !strings.Contains(result.Finding, "Best available") {
		t.Errorf("Finding = %q, want it to state the best available rung", result.Finding)
	}
	if !strings.Contains(result.Finding, "150") || !strings.Contains(result.Finding, "97.57") {
		t.Errorf("Finding = %q, want it to name the best size and the worst loss", result.Finding)
	}

	for _, r := range result.Rungs {
		if r.Err != nil {
			t.Errorf("%s NGNC errored: %v", r.SendAmount, r.Err)
			continue
		}
		if !r.Priced() {
			t.Errorf("%s NGNC did not price; the recording answered every size", r.SendAmount)
			continue
		}
		q := r.Result.Quotes[0]
		t.Logf("%9s NGNC -> %12s USDC  rate %s  loss %s%%  %s",
			r.SendAmount, q.ReceiveAmount.StringFixed(2), q.EffectiveRate.StringFixed(8),
			q.LossPct.StringFixed(2), q.Verdict)
	}
}
