package route_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/route"
)

// The composition rule, enforced rather than documented.
//
// Checks qualify the headline; they never move it. Letting observations about
// third parties rewrite states derived arithmetically would make the headline
// unfalsifiable — a reader could no longer tell whether a corridor was
// downgraded because its liquidity moved or because somebody added a check.
//
// These tests exist because that rule is the kind a future contributor will
// break with good intentions: wiring a critical check failure into the verdict
// looks like an improvement right up until a published figure changes for a
// reason no measurement supports.

// failingFindings builds the worst possible check set: every check failed, one
// of them critical.
func failingFindings() *checks.Findings {
	s := checks.Subject{Domain: "example.test", Asset: asset.NGNC()}
	crit := checks.IssuerAuthFlags{}.Describe()
	warn := checks.SEP10EndpointResponds{}.Describe()
	note := checks.AnchorAssetISO4217{}.Describe()

	var f checks.Findings
	f.Add(checks.Fail(crit, s, "the issuer can claw the asset back"))
	f.Add(checks.Fail(warn, s, "the declared SEP-10 endpoint did not respond"))
	f.Add(checks.Fail(note, s, "anchor_asset repeats the token's own code"))
	return &f
}

// TestFindingsDoNotMoveTheHeadline is the rule.
//
// The same ladder is rendered twice — once with no findings, once with every
// check failing at critical severity — and every headline field must be
// identical.
func TestFindingsDoNotMoveTheHeadline(t *testing.T) {
	m := loadSnap(t, "usdc-ngnc")

	// A mid the corridor can actually meet, so the ladder produces a
	// recommendation. Against the real mid nothing is recommended, and a
	// test whose Recommended is already nil cannot detect a check nulling
	// it — which is the single most tempting way to break this rule.
	e := engineOver(m, "USD/NGN", "660")

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          route.DefaultSizes,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}

	clean := route.ToCorridorJSON(res, "USD/NGN")
	if clean.Recommended == nil {
		t.Fatal("test setup is wrong: this corridor must have a recommendation " +
			"for the nulling case to be detectable")
	}

	// Composed through the real function, so a change inside it is what
	// this test is attacking. Attaching the block by hand afterwards would
	// exercise nothing.
	withFindings := route.WithFindings(route.ToCorridorJSON(res, "USD/NGN"), failingFindings())

	if withFindings.Findings == nil {
		t.Fatal("WithFindings dropped the findings block")
	}
	if withFindings.Findings.WorstSeverity != "critical" {
		t.Fatalf("test setup is wrong: worst severity is %q, want critical",
			withFindings.Findings.WorstSeverity)
	}

	// Every field that states what the corridor *is*.
	if withFindings.Integrity != clean.Integrity {
		t.Errorf("integrity moved from %s to %s once checks failed",
			clean.Integrity, withFindings.Integrity)
	}
	if withFindings.Floor != clean.Floor || withFindings.WorstLoss != clean.WorstLoss {
		t.Error("loss figures changed once checks failed")
	}
	if withFindings.Scored != clean.Scored {
		t.Error("scorability changed once checks failed")
	}

	// The recommendation is the sharpest case: a critical check failure is
	// exactly what someone would be tempted to wire into it.
	if (withFindings.Recommended == nil) != (clean.Recommended == nil) {
		t.Error("the recommendation changed once checks failed; checks must never " +
			"decide what is recommended")
	}

	// And every per-size verdict.
	if len(withFindings.Rungs) != len(clean.Rungs) {
		t.Fatalf("rung count changed: %d vs %d", len(withFindings.Rungs), len(clean.Rungs))
	}
	for i := range clean.Rungs {
		a, b := clean.Rungs[i], withFindings.Rungs[i]
		if a.Integrity != b.Integrity || a.Priced != b.Priced {
			t.Errorf("rung %s changed state once checks failed", a.SendAmount)
		}
		if (a.Quote == nil) != (b.Quote == nil) {
			t.Errorf("rung %s gained or lost its quote", a.SendAmount)
			continue
		}
		if a.Quote != nil && (a.Quote.Verdict != b.Quote.Verdict || a.Quote.LossPct != b.Quote.LossPct) {
			t.Errorf("rung %s verdict moved from %s to %s",
				a.SendAmount, a.Quote.Verdict, b.Quote.Verdict)
		}
	}
}

// TestFindingsAreOmittedWhenAbsent keeps the wire honest for a corridor that
// was never checked. An empty findings block would read as "checked, nothing
// found", which is a different claim from "not checked".
func TestFindingsAreOmittedWhenAbsent(t *testing.T) {
	m := loadSnap(t, "usdc-kesc")
	e := engineOver(m, "USD/KES", "129.4263")

	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.KESC(),
		Sizes:          []decimal.Decimal{decimal.RequireFromString("100")},
		ReferenceBase:  "USD",
		ReferenceQuote: "KES",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Through the composition point with nothing to attach.
	raw, err := json.Marshal(route.WithFindings(route.ToCorridorJSON(res, "USD/KES"), nil))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if _, present := body["findings"]; present {
		t.Error("an unchecked corridor carries a findings block; absent and " +
			"empty must not look the same")
	}

	// An empty (but non-nil) Findings must behave the same way.
	empty := &checks.Findings{}
	if got := route.WithFindings(route.ToCorridorJSON(res, "USD/KES"), empty); got.Findings != nil {
		t.Error("an empty findings set produced a findings block")
	}
}

// TestFindingsWireKeepsUnknownDistinct pins the field a client depends on.
//
// A wire shape with only `passed` would render "this anchor publishes no
// SEP-10 endpoint" identically to "this anchor's SEP-10 endpoint is dead".
func TestFindingsWireKeepsUnknownDistinct(t *testing.T) {
	s := checks.Subject{Domain: "example.test"}
	d := checks.SEP10EndpointResponds{}.Describe()

	var f checks.Findings
	f.Add(checks.Undetermined(d, s, "the anchor declares no WEB_AUTH_ENDPOINT"))
	f.Add(checks.Fail(d, s, "the declared endpoint did not respond"))

	out := f.ToJSON()

	if out.Undetermined != 1 || out.Failed != 1 {
		t.Fatalf("counts = %d undetermined, %d failed; want 1 and 1",
			out.Undetermined, out.Failed)
	}

	var sawUnknown, sawFailed bool
	for _, c := range out.Checks {
		switch {
		case !c.Determined:
			sawUnknown = true
			if c.Passed {
				t.Error("an undetermined check reports passed=true on the wire")
			}
			if c.Reason == "" {
				t.Error("an undetermined check reaches the wire with no reason")
			}
		case !c.Passed:
			sawFailed = true
		}
	}
	if !sawUnknown || !sawFailed {
		t.Error("the wire collapsed unknown and failed into one state")
	}

	// Only the genuine failure sets the worst severity.
	if out.WorstSeverity != "warning" {
		t.Errorf("WorstSeverity = %q, want warning from the one real failure",
			out.WorstSeverity)
	}
}
