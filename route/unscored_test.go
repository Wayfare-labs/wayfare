package route_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// staticAt is a provider returning one fixed mid.
type staticAt struct {
	name string
	mid  string
}

func (s staticAt) Name() string { return s.name }

func (s staticAt) Rate(_ context.Context, base, quote string) (refrate.Rate, error) {
	return refrate.Rate{
		Base: base, Quote: quote,
		Mid:    decimal.RequireFromString(s.mid),
		Source: s.name,
	}, nil
}

// TestMalfunctioningBenchmarkWithholdsVerdicts is the end-to-end half of the
// three-band rule.
//
// refrate decides that two mids this far apart mean one feed is broken; this
// checks the engine acts on that rather than scoring anyway. A loss figure
// derived from a benchmark we cannot trust is closer to fabricated than
// measured, so the corridor comes back with its structure intact and no
// verdict at all.
func TestMalfunctioningBenchmarkWithholdsVerdicts(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")

	e := &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: &refrate.Cross{
			// A misplaced decimal in one feed.
			Primary:   staticAt{"primary", "1350.2568"},
			Secondary: staticAt{"secondary", "135.02568"},
		},
	}

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

	// The structural finding survives: whether a market exists does not
	// depend on the benchmark, and it is the thing still worth reporting.
	if res.Integrity != route.IntegrityDirect {
		t.Errorf("Integrity = %s, want DIRECT; corridor structure does not depend on the mid",
			res.Integrity)
	}

	// Nothing is graded, and nothing is recommended.
	if res.Recommended != nil {
		t.Error("a corridor with an untrustworthy benchmark must not be recommended")
	}
	for _, q := range res.Quotes {
		if q.Verdict != route.VerdictUnknown {
			t.Errorf("quote graded %s; no verdict may be derived from a malfunctioning benchmark",
				q.Verdict)
		}
		if !q.LossPct.IsZero() {
			t.Errorf("LossPct = %s, want zero rather than a figure computed from an untrusted mid",
				q.LossPct)
		}
	}

	notes := strings.Join(res.Notes, " ")
	if !strings.Contains(notes, "No verdict") {
		t.Errorf("notes should say plainly that no verdict was reached, got: %v", res.Notes)
	}
	if !strings.Contains(notes, "measuring different things") {
		t.Errorf("notes should carry the reason from the provider cross-check, got: %v", res.Notes)
	}

	if res.Reference.Scorable() {
		t.Error("Result.Reference must report the benchmark as unscorable")
	}
	// Both mids survive so a reader can see which feeds disagreed.
	if res.Reference.SecondaryMid.IsZero() {
		t.Error("the second provider's mid must be carried even when neither is scored against")
	}
}

// TestScorableBenchmarkStillGrades is the control. Without it, an engine that
// refused to grade anything would pass the test above.
func TestScorableBenchmarkStillGrades(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")

	e := &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: &refrate.Cross{
			Primary:   staticAt{"primary", "1348.0585"},
			Secondary: staticAt{"secondary", "1350.2568"},
		},
	}

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

	if len(res.Quotes) == 0 {
		t.Fatal("expected a priced quote")
	}
	if res.Quotes[0].Verdict == route.VerdictUnknown {
		t.Error("a corroborated benchmark must still produce a verdict")
	}
	if !res.Quotes[0].LossPct.IsPositive() {
		t.Errorf("LossPct = %s, want a real figure on a corridor measured at a loss",
			res.Quotes[0].LossPct)
	}
	if res.Reference.Agreement != refrate.AgreementAgree {
		t.Errorf("Agreement = %s, want AGREE", res.Reference.Agreement)
	}
}

func TestMalfunctioningBenchmarkLadderDoesNotPublishLoss(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")
	e := &route.Engine{
		DEX: &dex.Client{
			HorizonURL: "https://horizon.stellar.org",
			HTTPClient: m.HTTPClient(),
		},
		RefRate: &refrate.Cross{
			Primary:   staticAt{"primary", "1350.2568"},
			Secondary: staticAt{"secondary", "135.02568"},
		},
	}

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("0.1"), decimal.RequireFromString("100")},
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	if res.Reference.Scorable() {
		t.Fatal("malfunctioning reference cross-check was marked scorable")
	}
	for _, forbidden := range []string{"Unusable", "loss against"} {
		if strings.Contains(res.Finding, forbidden) {
			t.Errorf("unscored finding contains %q: %s", forbidden, res.Finding)
		}
	}
	for _, required := range []string{"No verdict", "DIRECT", res.Reference.Note} {
		if required != "" && !strings.Contains(res.Finding, required) {
			t.Errorf("unscored finding is missing %q: %s", required, res.Finding)
		}
	}
	if !res.Floor.IsZero() || !res.WorstLoss.IsZero() || !res.FloorSize.IsZero() || !res.WorstSize.IsZero() {
		t.Errorf("unscored ladder contains summary measurements: floor=%s/%s worst=%s/%s",
			res.Floor, res.FloorSize, res.WorstLoss, res.WorstSize)
	}
	for i, rung := range res.Rungs {
		if len(rung.Decomposition.Parts) != 0 {
			t.Errorf("rung %d has a cost decomposition despite the unscored reference: %+v", i, rung.Decomposition)
		}
		if rung.MarginalCost != nil {
			t.Errorf("rung %d has a reference-dependent marginal cost: %+v", i, rung.MarginalCost)
		}
	}

	raw, err := json.Marshal(route.ToCorridorJSONAt(res, "USD/NGN", time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("marshal unscored ladder: %v", err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode unscored ladder: %v", err)
	}
	for _, field := range []string{"floor_loss_pct", "floor_size", "worst_loss_pct", "worst_size"} {
		if _, ok := doc[field]; ok {
			t.Errorf("unscored corridor contains %q: %s", field, doc[field])
		}
	}
	var rungs []map[string]json.RawMessage
	if err := json.Unmarshal(doc["rungs"], &rungs); err != nil {
		t.Fatalf("decode rungs: %v", err)
	}
	for i, rung := range rungs {
		for _, field := range []string{"marginal_cost", "cost"} {
			if _, ok := rung[field]; ok {
				t.Errorf("unscored rung %d contains %q", i, field)
			}
		}
		if quote, ok := rung["quote"]; ok && string(quote) != "null" {
			var q map[string]json.RawMessage
			if err := json.Unmarshal(quote, &q); err != nil {
				t.Fatalf("decode rung %d quote: %v", i, err)
			}
			for _, field := range []string{"loss_pct", "loss_amount"} {
				if _, ok := q[field]; ok {
					t.Errorf("unscored rung %d quote contains %q", i, field)
				}
			}
		}
	}
}
