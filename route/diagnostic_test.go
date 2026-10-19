package route_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// TestDiagnosticNeverMovesTheScoredMid is the end-to-end half of issue #597's
// second acceptance criterion.
//
// refrate's own tests pin that the third provider cannot change the mid it is
// given; this drives the same pair through the engine, from a recorded
// mainnet snapshot, and asserts the published result is identical with and
// without the diagnostic: same reference mid and source, same loss figure,
// same verdict, same recommendation. Only the attribution is new.
func TestDiagnosticNeverMovesTheScoredMid(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")

	engine := func(withDiagnostic bool) *route.Engine {
		cross := &refrate.Cross{
			// 5% apart: past the 2% agreement ceiling, well inside the 10%
			// malfunction bound, so the conservative rule decides.
			Primary:   staticAt{"primary", "1300"},
			Secondary: staticAt{"secondary", "1365"},
		}
		if withDiagnostic {
			// Closer to the secondary, so the primary is the outlier.
			cross.Diagnostic = staticAt{"diagnostic", "1350"}
		}
		return &route.Engine{
			DEX: &dex.Client{
				HorizonURL: "https://horizon.stellar.org",
				HTTPClient: m.HTTPClient(),
			},
			RefRate: cross,
		}
	}

	quote := func(e *route.Engine) *route.Result {
		t.Helper()
		res, err := e.Quote(context.Background(), route.Request{
			SendAsset:      asset.USDC(),
			SendAmount:     decimal.RequireFromString("100"),
			ReceiveAsset:   asset.NGNC(),
			ReferenceBase:  "USD",
			ReferenceQuote: "NGN",
		})
		if err != nil {
			t.Fatalf("Quote: %v", err)
		}
		return res
	}

	without := quote(engine(false))
	with := quote(engine(true))

	if without.Reference.Diagnostic != nil {
		t.Errorf("Diagnostic = %+v without a configured provider, want nil", without.Reference.Diagnostic)
	}
	if with.Reference.Diagnostic == nil {
		t.Fatal("Diagnostic is nil with a configured provider; the disagreement must be attributed")
	}
	if got := with.Reference.Diagnostic.Outlier; got != "primary" {
		t.Errorf("Diagnostic.Outlier = %q, want primary: it is farther from the independent mid", got)
	}

	// The scored benchmark and every derived figure are unchanged.
	if !with.ReferenceMid.Equal(without.ReferenceMid) {
		t.Errorf("ReferenceMid moved from %s to %s because a diagnostic was configured",
			without.ReferenceMid, with.ReferenceMid)
	}
	if with.ReferenceSource != without.ReferenceSource {
		t.Errorf("ReferenceSource moved from %q to %q", without.ReferenceSource, with.ReferenceSource)
	}
	if !with.Reference.DivergencePct.Equal(without.Reference.DivergencePct) {
		t.Errorf("DivergencePct moved from %s to %s",
			without.Reference.DivergencePct, with.Reference.DivergencePct)
	}
	if with.Integrity != without.Integrity {
		t.Errorf("Integrity moved from %s to %s", without.Integrity, with.Integrity)
	}
	if len(with.Quotes) != len(without.Quotes) {
		t.Fatalf("got %d quotes with a diagnostic and %d without", len(with.Quotes), len(without.Quotes))
	}
	for i := range without.Quotes {
		w, o := with.Quotes[i], without.Quotes[i]
		if !w.ReceiveAmount.Equal(o.ReceiveAmount) {
			t.Errorf("quote %d ReceiveAmount moved from %s to %s", i, o.ReceiveAmount, w.ReceiveAmount)
		}
		if !w.LossPct.Equal(o.LossPct) {
			t.Errorf("quote %d LossPct moved from %s to %s", i, o.LossPct, w.LossPct)
		}
		if w.Verdict != o.Verdict {
			t.Errorf("quote %d Verdict moved from %s to %s", i, o.Verdict, w.Verdict)
		}
	}
	if (with.Recommended == nil) != (without.Recommended == nil) {
		t.Errorf("recommendation presence changed: with=%v without=%v",
			with.Recommended, without.Recommended)
	}

	// The diagnostic itself is a figure carried for the reader.
	if !with.Reference.Diagnostic.Mid.Equal(decimal.RequireFromString("1350")) {
		t.Errorf("Diagnostic.Mid = %s, want 1350", with.Reference.Diagnostic.Mid)
	}
}
