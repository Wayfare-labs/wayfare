package checks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// Every corridor metric declares which liquidity source it observes, so a
// consumer can refuse to reconcile a book figure with a route figure by
// machine. Before this coverage existed the limitation lived only in the
// CannotDetermine prose, which a JSON consumer cannot act on. See issue #104.

// allCorridorMetricDescribers is the fixed set of corridor metric descriptors
// in this package. Listing them explicitly, rather than reflecting over the
// package, is the point of the coverage: adding a metric without giving it a
// venue would silently ship a wire figure with no venue tag, and the test
// only catches that when the new metric is added here.
func allCorridorMetricDescribers() []struct {
	name string
	d    Descriptor
} {
	return []struct {
		name string
		d    Descriptor
	}{
		{"spread.bid-ask", SpreadMetric{}.Describe()},
		{"depth.observed-executable", DepthMetric{}.Describe()},
		{"depth.observed", DepthMetric{}.RunObserved(context.Background(), Subject{}).describeShim()},
		{"depth.executable", DepthMetric{}.RunExecutable(context.Background(), Subject{}).describeShim()},
		{"concentration.liquidity", ConcentrationMetric{}.Describe()},
		{"price-impact.size", PriceImpactMetric{}.Describe()},
		{"deviation.book-vs-reference", DeviationMetric{}.Describe()},
	}
}

// describeShim recovers a descriptor's identifying fields from a MetricResult.
// The depth halves build their own descriptors inside the Run method; this
// lets the venue-coverage test compare them uniformly with the ones exposed
// via Describe on the metric type.
func (r MetricResult) describeShim() Descriptor {
	return Descriptor{
		ID:              r.ID,
		Scope:           r.Scope,
		Venue:           r.Venue,
		Title:           r.ID,
		CanDetermine:    "shim: recovered from MetricResult for venue coverage",
		CannotDetermine: "shim: recovered from MetricResult for venue coverage",
	}
}

func TestEveryCorridorMetricDeclaresAVenue(t *testing.T) {
	for _, tc := range allCorridorMetricDescribers() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.d.Scope != ScopeCorridor {
				t.Fatalf("test fixture wrong: %s is not corridor-scoped", tc.name)
			}
			switch tc.d.Venue {
			case VenueOrderBook, VenuePathfinding:
			default:
				t.Errorf("metric %s: Venue = %q, must be %q or %q — a corridor metric "+
					"with no venue leaves a wire consumer no way to tell which market "+
					"the figure describes (issue #104)",
					tc.name, tc.d.Venue, VenueOrderBook, VenuePathfinding)
			}
		})
	}
}

func TestValidateAsMetricRequiresVenueForCorridorMetric(t *testing.T) {
	d := Descriptor{
		ID: "test.no-venue", Scope: ScopeCorridor, Title: "t",
		CanDetermine: "c", CannotDetermine: "c",
	}
	err := d.ValidateAsMetric()
	if err == nil {
		t.Fatal("ValidateAsMetric accepted a corridor metric with no Venue; must reject")
	}
	if !strings.Contains(err.Error(), "must declare a Venue") {
		t.Errorf("error = %q, want it to mention the missing Venue", err)
	}
}

func TestValidateAsMetricRejectsVenueOnNonCorridorMetric(t *testing.T) {
	d := Descriptor{
		ID: "test.anchor-with-venue", Scope: ScopeAnchor, Venue: VenueOrderBook,
		Title: "t", CanDetermine: "c", CannotDetermine: "c",
	}
	err := d.ValidateAsMetric()
	if err == nil {
		t.Fatal("ValidateAsMetric accepted an anchor metric with a Venue; must reject")
	}
	if !strings.Contains(err.Error(), "must not declare a Venue") {
		t.Errorf("error = %q, want it to mention the extraneous Venue", err)
	}
}

func TestValidateAsMetricRejectsUnknownVenue(t *testing.T) {
	d := Descriptor{
		ID: "test.unknown-venue", Scope: ScopeCorridor, Venue: Venue("amm-only"),
		Title: "t", CanDetermine: "c", CannotDetermine: "c",
	}
	err := d.ValidateAsMetric()
	if err == nil {
		t.Fatal("ValidateAsMetric accepted an unknown Venue string; must reject")
	}
	if !strings.Contains(err.Error(), "unknown venue") {
		t.Errorf("error = %q, want it to name the unknown venue", err)
	}
}

func TestValidateAsMetricAcceptsAnchorMetricWithoutVenue(t *testing.T) {
	d := Descriptor{
		ID: "issuer.something", Scope: ScopeAnchor,
		Title: "t", CanDetermine: "c", CannotDetermine: "c",
	}
	if err := d.ValidateAsMetric(); err != nil {
		t.Errorf("anchor-scope metric without a Venue must validate, got: %v", err)
	}
}

// runMetricRejectsInvalidVenueOnCorridorMetric captures the end-to-end effect:
// a corridor metric that forgot its venue produces an undetermined result
// naming that as the reason, rather than a determined value with no venue tag.
type venuelessMetric struct{}

func (venuelessMetric) Describe() Descriptor {
	return Descriptor{
		ID: "test.venueless", Scope: ScopeCorridor,
		Title: "t", CanDetermine: "c", CannotDetermine: "c",
	}
}
func (venuelessMetric) Run(_ context.Context, s Subject) MetricResult {
	// If validation somehow lets this pass, the assertion below fails.
	return MetricValue(venuelessMetric{}.Describe(), s,
		decimal.NewFromInt(1), UnitCount, "unreachable")
}

func TestRunMetricRejectsCorridorMetricWithoutVenue(t *testing.T) {
	r := RunMetric(context.Background(), venuelessMetric{},
		Subject{Send: asset.USDC(), Receive: asset.NGNC()})
	if r.Determined {
		t.Fatal("RunMetric produced a determined result from a corridor metric with no Venue; must reject")
	}
	if !strings.Contains(r.Reason, "must declare a Venue") {
		t.Errorf("Reason = %q, want it to explain the missing Venue", r.Reason)
	}
}

// TestMetricResultCarriesVenueFromDescriptor pins that a caller reading a
// result off the wire sees the same venue Describe declared, without going
// back to the descriptor.
func TestMetricResultCarriesVenueFromDescriptor(t *testing.T) {
	d := SpreadMetric{}.Describe()
	got := MetricValue(d, Subject{Send: asset.USDC(), Receive: asset.NGNC()},
		decimal.RequireFromString("1.5"), UnitPercent, "test")
	if got.Venue != VenueOrderBook {
		t.Errorf("MetricValue: Venue = %q, want %q", got.Venue, VenueOrderBook)
	}
	undet := MetricUndetermined(d, Subject{Send: asset.USDC(), Receive: asset.NGNC()},
		"test reason")
	if undet.Venue != VenueOrderBook {
		t.Errorf("MetricUndetermined: Venue = %q, want %q", undet.Venue, VenueOrderBook)
	}
}

// TestFindingsJSONExposesVenue pins the wire shape: a rendered corridor
// metric carries its venue in a machine-readable field, not only in the
// descriptor prose an end consumer never sees.
func TestFindingsJSONExposesVenue(t *testing.T) {
	f := &Findings{}
	f.AddMetric(MetricResult{
		Observation: Observation{
			ID: "spread.bid-ask", Scope: ScopeCorridor, Subject: "USDC -> NGNC",
			At: time.Now().UTC(), Determined: true,
			Evidence: []Evidence{{
				Source: "/order_book USDC/NGNC", Observed: "spread=1.5%",
				ObservedAt: time.Now().UTC(),
			}},
		},
		Value: decimal.RequireFromString("1.5"), Unit: UnitPercent,
		Venue: VenueOrderBook, Summary: "spread 1.50%",
	})
	f.AddMetric(MetricResult{
		Observation: Observation{
			ID: "price-impact.size", Scope: ScopeCorridor, Subject: "USDC -> NGNC",
			At: time.Now().UTC(), Determined: true,
			Evidence: []Evidence{{
				Source: "/paths/strict-send USDC/NGNC", Observed: "impact=3%",
				ObservedAt: time.Now().UTC(),
			}},
		},
		Value: decimal.RequireFromString("3"), Unit: UnitPercent,
		Venue: VenuePathfinding, Summary: "price impact 3%",
	})

	b, err := json.Marshal(f.ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"venue":"order-book"`) {
		t.Errorf("JSON missing venue=order-book: %s", s)
	}
	if !strings.Contains(s, `"venue":"pathfinding"`) {
		t.Errorf("JSON missing venue=pathfinding: %s", s)
	}
}

// TestFindingsJSONOmitsVenueForNonCorridorMetric pins that omitempty is
// honoured: a non-corridor metric leaves the venue field out entirely rather
// than emitting an empty string that reads as "no venue applies here" in
// some code paths but as "venue unknown" in others.
func TestFindingsJSONOmitsVenueForNonCorridorMetric(t *testing.T) {
	f := &Findings{}
	f.AddMetric(MetricResult{
		Observation: Observation{
			ID: "anchor.something", Scope: ScopeAnchor, Subject: "ngnc.online",
			At: time.Now().UTC(), Determined: true,
			Evidence: []Evidence{{
				Source: "toml", Observed: "x", ObservedAt: time.Now().UTC(),
			}},
		},
		Value: decimal.NewFromInt(1), Unit: UnitCount, Summary: "x",
	})
	b, err := json.Marshal(f.ToJSON())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"venue"`) {
		t.Errorf("non-corridor metric emitted a venue field: %s", b)
	}
}
