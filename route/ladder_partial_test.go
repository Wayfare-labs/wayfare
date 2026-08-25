package route_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// The contract for a half-measured ladder.
//
// Ladder returns a result when some rungs error; Failed() only catches the
// all-nothing case. These tests pin what the in-between means: the figures
// describe only the sizes that were measured, an unmeasured size is unknown
// rather than zero, integrity still reflects every rung that answered, and
// the Finding qualifies itself so no reader mistakes a partial curve for a
// complete one.

// partialEngine builds an engine whose Horizon prices sizes below failAt and
// returns HTTP 500 above it, so one ladder contains both priced and failed
// rungs against a single deterministic upstream.
func partialEngine(t *testing.T, failAt string) (*route.Engine, func()) {
	t.Helper()
	cutoff := decimal.RequireFromString(failAt)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		d, err := decimal.NewFromString(amount)
		if err != nil {
			http.Error(w, "bad amount", http.StatusBadRequest)
			return
		}
		if d.GreaterThanOrEqual(cutoff) {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		dest := d.Mul(decimal.RequireFromString("1300"))
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":"` + amount + `",
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":"` + dest.String() + `","path":[]}]}}`))
	}))

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1350"),
		}),
	}
	return e, func() { srv.Close() }
}

func ladderOver(t *testing.T, e *route.Engine, sizes ...string) *route.LadderResult {
	t.Helper()
	amt := make([]decimal.Decimal, 0, len(sizes))
	for _, s := range sizes {
		amt = append(amt, decimal.RequireFromString(s))
	}
	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          amt,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	return res
}

// TestPartialLadderMeasuresOnlyWhatAnswered pins the core of the contract:
// some sizes price, some fail, and the result is a real measurement of the
// former with the latter recorded as unknown — not zero loss, not no-market,
// and not a discarded ladder.
func TestPartialLadderMeasuresOnlyWhatAnswered(t *testing.T) {
	e, close := partialEngine(t, "10")
	defer close()

	res := ladderOver(t, e, "0.1", "1", "10", "100")

	if res.Failed() {
		t.Error("some sizes were measured, but the ladder reports total failure")
	}
	if !res.PartiallyFailed() {
		t.Error("PartiallyFailed = false, want true: 2 of 4 sizes could not be measured")
	}

	got := res.UnmeasuredSizes()
	if len(got) != 2 || !got[0].Equal(decimal.RequireFromString("10")) ||
		!got[1].Equal(decimal.RequireFromString("100")) {
		t.Errorf("UnmeasuredSizes = %v, want [10 100]", got)
	}

	// The figures come only from the surviving rungs: effective rate 1300
	// against mid 1350 at every priced size, so floor and worst are both
	// ~3.7% and both are attributed to measured sizes alone. An
	// implementation that folded errored rungs in as zero-loss or as
	// no-market changes these.
	if !res.FloorSize.Equal(decimal.RequireFromString("0.1")) ||
		!res.WorstSize.Equal(decimal.RequireFromString("1")) {
		t.Errorf("curve spans %s..%s, want only the measured sizes 0.1..1",
			res.FloorSize, res.WorstSize)
	}
	want := decimal.RequireFromString("50").Div(decimal.RequireFromString("1350")).Mul(decimal.NewFromInt(100))
	if !res.Floor.Equal(want.Round(4)) && !res.Floor.Equal(want) {
		t.Errorf("Floor = %s, want %s (the loss at the smallest measured size)",
			res.Floor, want)
	}

	// One dead size cannot erase what another size learned.
	if res.Integrity != route.IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT: a path was found at a size that answered",
			res.Integrity)
	}
	if res.ReferenceMid.IsZero() {
		t.Error("the reference benchmark was lost along with the failed rungs")
	}
}

// TestPartialLadderFindingQualifiesItself pins the prose half of the
// contract: a reader of the headline must be able to tell a partial curve
// from a full one.
func TestPartialLadderFindingQualifiesItself(t *testing.T) {
	e, close := partialEngine(t, "10")
	defer close()

	partial := ladderOver(t, e, "0.1", "10")
	for _, want := range []string{"1 of 2", "could not be measured"} {
		if !strings.Contains(partial.Finding, want) {
			t.Errorf("finding = %q, want it to contain %q", partial.Finding, want)
		}
	}

	full := ladderOver(t, e, "0.1", "1")
	if strings.Contains(full.Finding, "could not be measured") {
		t.Errorf("a fully-measured ladder carries a partial-measurement "+
			"qualification: %q", full.Finding)
	}
}

// TestFullyMeasuredAndFullyFailedAreNotPartial keeps the new signal from
// blurring the two states it sits between.
func TestFullyMeasuredAndFullyFailedAreNotPartial(t *testing.T) {
	e, close := partialEngine(t, "10") // everything below 10 succeeds
	defer close()

	full := ladderOver(t, e, "0.1", "1", "5")
	if full.PartiallyFailed() || full.Failed() || len(full.UnmeasuredSizes()) != 0 {
		t.Errorf("every size was measured; reported partial=%v failed=%v unmeasured=%v",
			full.PartiallyFailed(), full.Failed(), full.UnmeasuredSizes())
	}

	allDead, closeDead := partialEngine(t, "0.1") // every requested size fails
	defer closeDead()

	dead := ladderOver(t, allDead, "0.1", "5")
	if !dead.Failed() {
		t.Error("no size was measured, but Failed() is false")
	}
	if dead.PartiallyFailed() {
		t.Error("an all-failed ladder is a failure, not a partial success")
	}
	if len(dead.UnmeasuredSizes()) != 2 {
		t.Errorf("UnmeasuredSizes = %v, want both sizes named", dead.UnmeasuredSizes())
	}
}

// TestPartialLadderStillRecommendsWhatItMeasured checks the recommendation
// rule survives partiality: a viable quote at a measured size is still
// recommendable, because the failure of other sizes says nothing about it.
func TestPartialLadderStillRecommendsWhatItMeasured(t *testing.T) {
	// Mid set above the achieved rate so the measured size grades GOOD.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		amount := r.URL.Query().Get("source_amount")
		d, _ := decimal.NewFromString(amount)
		if d.GreaterThanOrEqual(decimal.NewFromInt(10)) {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":"` + amount + `",
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":"1349.0000000","path":[]}]}}`))
	}))
	defer srv.Close()

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1350"),
		}),
	}
	res := ladderOver(t, e, "1", "100")

	if !res.PartiallyFailed() {
		t.Fatal("expected a partial ladder")
	}
	if res.Recommended == nil {
		t.Fatal("the measured size graded GOOD but nothing was recommended")
	}
	if !res.RecommendedSize.Equal(decimal.RequireFromString("1")) {
		t.Errorf("RecommendedSize = %s, want the measured size 1", res.RecommendedSize)
	}
}
