package refrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// fakeProvider answers with a fixed rate or a fixed error.
type fakeProvider struct {
	name string
	mid  string
	asOf time.Time
	err  error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Rate(_ context.Context, base, quote string) (Rate, error) {
	if f.err != nil {
		return Rate{}, f.err
	}
	asOf := f.asOf
	if asOf.IsZero() {
		asOf = time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	}
	return Rate{
		Base: base, Quote: quote,
		Mid:    decimal.RequireFromString(f.mid),
		AsOf:   asOf,
		Source: f.name,
	}, nil
}

func crossOf(primaryMid, secondaryMid string) *Cross {
	return &Cross{
		Primary:   &fakeProvider{name: "primary", mid: primaryMid},
		Secondary: &fakeProvider{name: "secondary", mid: secondaryMid},
	}
}

func rateOf(t *testing.T, c *Cross) Rate {
	t.Helper()
	r, err := c.Rate(context.Background(), "USD", "NGN")
	if err != nil {
		t.Fatalf("Cross.Rate: %v", err)
	}
	return r
}

// TestAgreementBand covers the ordinary case, calibrated on the real
// divergence measured between the two live providers on 2026-08-21.
func TestAgreementBand(t *testing.T) {
	r := rateOf(t, crossOf("1348.0585", "1350.2568"))

	if r.Agreement != AgreementAgree {
		t.Errorf("Agreement = %s, want AGREE", r.Agreement)
	}
	// (1350.2568 - 1348.0585) / 1348.0585 = 0.163%
	if got := r.DivergencePct.StringFixed(2); got != "0.16" {
		t.Errorf("DivergencePct = %s, want 0.16", got)
	}
	if r.Source != "primary" {
		t.Errorf("Source = %s, want the primary when the providers agree", r.Source)
	}
	if !r.Scorable() {
		t.Error("an agreed rate must be scorable")
	}
	// Both mids are carried even on agreement, so a later reader can tell a
	// benchmark change from a corridor change.
	if r.SecondaryMid.IsZero() || r.SecondarySource == "" {
		t.Error("the secondary mid and source must be recorded even when they agree")
	}
}

// TestDisagreementScoresConservatively pins the rule that a genuine
// disagreement is resolved toward the reading that makes the corridor look
// worse, since flattering a corridor is the failure this project exists to
// avoid.
func TestDisagreementScoresConservatively(t *testing.T) {
	// 5% apart: past agreement, well inside malfunction.
	r := rateOf(t, crossOf("1300", "1365"))

	if r.Agreement != AgreementDisagree {
		t.Fatalf("Agreement = %s, want DISAGREE (divergence %s)", r.Agreement, r.DivergencePct)
	}
	// Loss is measured as how far below the benchmark a route lands, so the
	// larger mid is the more conservative one.
	if !r.Mid.Equal(decimal.RequireFromString("1365")) {
		t.Errorf("scored against %s, want 1365 — the mid producing the higher loss", r.Mid)
	}
	if r.Source != "secondary" {
		t.Errorf("Source = %s, want secondary; scored_against must name the mid used", r.Source)
	}
	if r.SecondaryMid.String() != "1300" {
		t.Errorf("SecondaryMid = %s, want the unused mid 1300", r.SecondaryMid)
	}
	if !r.Scorable() {
		t.Error("a disagreement inside the malfunction bound is still scorable")
	}
	if !strings.Contains(r.Note, "conservative") {
		t.Errorf("Note = %q, want it to explain the conservative choice", r.Note)
	}
}

// TestMalfunctionBandRefusesToScore is the band added because conservative
// scoring has a failure mode: a broken feed always wins it.
//
// A provider that is weeks stale, quoting the wrong pair, or off by a decimal
// place would be selected every time, and the project's bias against
// flattering a corridor would become a mechanism for exaggerating one. Beyond
// the bound, nothing is scored.
func TestMalfunctionBandRefusesToScore(t *testing.T) {
	// A misplaced decimal: one feed quoting 134.8 instead of 1348.
	r := rateOf(t, crossOf("1348.0585", "134.80585"))

	if r.Agreement != AgreementMalfunction {
		t.Fatalf("Agreement = %s, want MALFUNCTION (divergence %s)",
			r.Agreement, r.DivergencePct)
	}
	if r.Scorable() {
		t.Error("a malfunctioning benchmark must not be scorable")
	}
	// Both mids and both sources survive, so a reader can see what happened
	// rather than only that something did.
	if r.Mid.IsZero() || r.SecondaryMid.IsZero() {
		t.Error("both mids must be reported even when neither is scored against")
	}
	if r.SecondarySource == "" {
		t.Error("both sources must be reported")
	}
	for _, want := range []string{"not scored", "measuring different things"} {
		if !strings.Contains(r.Note, want) {
			t.Errorf("Note = %q, want it to contain %q", r.Note, want)
		}
	}
}

// TestBandBoundaries pins where the three bands meet.
func TestBandBoundaries(t *testing.T) {
	cases := []struct {
		name              string
		primary, secondar string
		want              Agreement
	}{
		{"identical", "1000", "1000", AgreementAgree},
		{"just inside agreement", "1000", "1020", AgreementAgree},      // exactly 2%
		{"just past agreement", "1000", "1020.01", AgreementDisagree},  // 2.001%
		{"just inside malfunction", "1000", "1100", AgreementDisagree}, // exactly 10%
		{"just past malfunction", "1000", "1100.01", AgreementMalfunction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := rateOf(t, crossOf(tc.primary, tc.secondar))
			if r.Agreement != tc.want {
				t.Errorf("Agreement = %s, want %s (divergence %s%%)",
					r.Agreement, tc.want, r.DivergencePct.StringFixed(4))
			}
		})
	}
}

// TestDivergenceIsSymmetric checks the figure does not depend on which
// provider happens to be configured as primary.
func TestDivergenceIsSymmetric(t *testing.T) {
	a := rateOf(t, crossOf("1300", "1365"))
	b := rateOf(t, crossOf("1365", "1300"))

	if !a.DivergencePct.Equal(b.DivergencePct) {
		t.Errorf("divergence %s vs %s: swapping the providers changed the figure",
			a.DivergencePct, b.DivergencePct)
	}
	// Both orderings must also settle on the same conservative mid.
	if !a.Mid.Equal(b.Mid) {
		t.Errorf("scored against %s vs %s: the conservative choice must not depend on order",
			a.Mid, b.Mid)
	}
}

// TestStaleIsReportedDistinctly covers rates describing different moments,
// where the gap measures lag rather than disagreement.
func TestStaleIsReportedDistinctly(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	c := &Cross{
		Primary:   &fakeProvider{name: "stale-one", mid: "1300", asOf: now.Add(-96 * time.Hour)},
		Secondary: &fakeProvider{name: "fresh-one", mid: "1365", asOf: now},
	}
	r := rateOf(t, c)

	if r.Agreement != AgreementStale {
		t.Fatalf("Agreement = %s, want STALE", r.Agreement)
	}
	// Scored against the fresher feed, not the more conservative one: the
	// question here is which rate is current, not which is pessimistic.
	if r.Source != "fresh-one" {
		t.Errorf("Source = %s, want the fresher provider", r.Source)
	}
	if !r.Scorable() {
		t.Error("a stale-but-close pair is still scorable")
	}
	if !strings.Contains(r.Note, "staleness") {
		t.Errorf("Note = %q, want it to name staleness rather than disagreement", r.Note)
	}
}

// TestStaleSelectsFresherFeedRegardlessOfOrder pins the selection rule inside
// the STALE band: the fresher feed is scored against, whichever position it
// holds in the configuration.
//
// The stale band answers "which rate is current", not "which reading is more
// pessimistic", so the conservative-mid rule must not leak into it. Both
// orderings are asserted because the swap in reconcile() only runs when the
// secondary is the fresher one; an implementation that selected correctly by
// accident in one ordering would still fail here.
func TestStaleSelectsFresherFeedRegardlessOfOrder(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	fresh := &fakeProvider{name: "fresh", mid: "1350", asOf: now}
	stale := &fakeProvider{name: "stale", mid: "1300", asOf: now.Add(-96 * time.Hour)}

	cases := []struct {
		name               string
		primary, secondary *fakeProvider
	}{
		{"fresh primary", fresh, stale},
		{"fresh secondary", stale, fresh},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := rateOf(t, &Cross{Primary: tc.primary, Secondary: tc.secondary})

			if r.Agreement != AgreementStale {
				t.Errorf("Agreement = %s, want STALE", r.Agreement)
			}
			if r.Source != "fresh" {
				t.Errorf("Source = %s, want the fresher feed regardless of ordering", r.Source)
			}
			if !r.Mid.Equal(decimal.RequireFromString("1350")) {
				t.Errorf("Mid = %s, want the fresher mid 1350", r.Mid)
			}
			if !r.AsOf.Equal(now) {
				t.Errorf("AsOf = %s, want the fresher stamp %s", r.AsOf, now)
			}
			// The displaced feed survives in the secondary fields, so a
			// reader can see both moments rather than only the chosen one.
			if r.SecondarySource != "stale" || !r.SecondaryMid.Equal(decimal.RequireFromString("1300")) {
				t.Errorf("secondary = %s/%s, want the stale feed's 1300",
					r.SecondarySource, r.SecondaryMid)
			}
			if !r.Scorable() {
				t.Error("a stale-but-scored pair must be scorable")
			}
			if !strings.Contains(r.Note, now.UTC().Format(time.RFC3339)) ||
				!strings.Contains(r.Note, now.Add(-96*time.Hour).UTC().Format(time.RFC3339)) {
				t.Errorf("Note = %q, want both as-of stamps carried", r.Note)
			}
		})
	}
}

// TestStaleBeatsConservativeSelection pins the precedence between the bands.
//
// The same two mids (1300 vs 1365, 5% apart) appear in both the DISAGREE and
// the STALE case. Under DISAGREE the conservative rule picks 1365 — the larger
// mid, the higher loss. When the feeds are also stale-apart, the fresher feed
// is the one with the *smaller* mid here, and staleness must win: the question
// is which rate is current, not which is pessimistic. An implementation that
// applied the conservative selection inside the stale band scores against a
// four-day-old figure and fails this test.
func TestStaleBeatsConservativeSelection(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	c := &Cross{
		Primary:   &fakeProvider{name: "fresh-cheap", mid: "1300", asOf: now},
		Secondary: &fakeProvider{name: "stale-dear", mid: "1365", asOf: now.Add(-96 * time.Hour)},
	}
	r := rateOf(t, c)

	if r.Agreement != AgreementStale {
		t.Fatalf("Agreement = %s, want STALE (divergence %s%%)",
			r.Agreement, r.DivergencePct.StringFixed(2))
	}
	if r.Source != "fresh-cheap" || !r.Mid.Equal(decimal.RequireFromString("1300")) {
		t.Errorf("scored against %s (%s), want the fresher feed's 1300 — "+
			"not the conservative 1365", r.Source, r.Mid)
	}
}

// TestStaleGapBoundary pins where the stale band begins: exactly StaleGap
// apart is lag within one refresh cycle and reads as agreement or
// disagreement; beyond it the gap measures staleness. A provider with no
// as-of stamp at all can never be called stale on timestamps alone.
func TestStaleGapBoundary(t *testing.T) {
	now := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name                     string
		primaryGap, otherGap     time.Duration
		primaryStamp, otherStamp bool // false = leave AsOf zero
		mids                     string
		want                     Agreement
	}{
		{"exactly at the gap", 48 * time.Hour, 0, true, true, "1348|1348", AgreementAgree},
		{"one second past the gap", 48*time.Hour + time.Second, 0, true, true, "1348|1348", AgreementStale},
		{"no stamp never stale", 96 * time.Hour, 0, false, true, "1348|1350", AgreementAgree},
		{"other side missing its stamp", 0, 96 * time.Hour, true, false, "1348|1350", AgreementAgree},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p1 := &fakeProvider{name: "one", asOf: zeroOr(tc.primaryStamp, now.Add(-tc.primaryGap))}
			p2 := &fakeProvider{name: "two", asOf: zeroOr(tc.otherStamp, now.Add(-tc.otherGap))}

			mids := strings.SplitN(tc.mids, "|", 2)
			p1.mid, p2.mid = mids[0], mids[1]

			r := rateOf(t, &Cross{Primary: p1, Secondary: p2})
			if r.Agreement != tc.want {
				t.Errorf("Agreement = %s, want %s (divergence %s%%)",
					r.Agreement, tc.want, r.DivergencePct.StringFixed(4))
			}
		})
	}
}

// zeroOr returns fallback when keep is false, standing in for a Rate whose
// AsOf was never stamped.
func zeroOr(keep bool, fallback time.Time) time.Time {
	if keep {
		return fallback
	}
	return time.Time{}
}

// TestSingleProviderDegradesAndSaysSo covers one provider failing. The rate is
// usable and uncorroborated, and must not read as though a cross-check
// happened.
func TestSingleProviderDegradesAndSaysSo(t *testing.T) {
	c := &Cross{
		Primary:   &fakeProvider{name: "primary", mid: "1348"},
		Secondary: &fakeProvider{name: "secondary", err: errors.New("connection refused")},
	}
	r := rateOf(t, c)

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	if !r.Mid.Equal(decimal.RequireFromString("1348")) {
		t.Errorf("Mid = %s, want the surviving provider's 1348", r.Mid)
	}
	if !r.SecondaryMid.IsZero() {
		t.Errorf("SecondaryMid = %s, want zero when the secondary failed", r.SecondaryMid)
	}
	if !strings.Contains(r.Note, "uncorroborated") {
		t.Errorf("Note = %q, want it to say the rate is uncorroborated", r.Note)
	}
	if !r.Scorable() {
		t.Error("a single-source rate is still scorable; it is just not corroborated")
	}
}

// TestPrimaryFailureFallsBackToSecondary is the other half of degrading.
func TestPrimaryFailureFallsBackToSecondary(t *testing.T) {
	c := &Cross{
		Primary:   &fakeProvider{name: "primary", err: errors.New("timeout")},
		Secondary: &fakeProvider{name: "secondary", mid: "1350"},
	}
	r := rateOf(t, c)

	if r.Source != "secondary" || !r.Mid.Equal(decimal.RequireFromString("1350")) {
		t.Errorf("got %s from %s, want 1350 from secondary", r.Mid, r.Source)
	}
	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
}

// TestBothFailingIsAnError pins that a missing benchmark is never substituted
// for. No rate means no measurement.
func TestBothFailingIsAnError(t *testing.T) {
	c := &Cross{
		Primary:   &fakeProvider{name: "primary", err: errors.New("timeout")},
		Secondary: &fakeProvider{name: "secondary", err: errors.New("connection refused")},
	}
	_, err := c.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("expected an error when neither provider answered")
	}
	for _, want := range []string{"primary", "secondary", "timeout", "connection refused"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestZeroMidIsMalfunctionNotTotalDisagreement guards the division: a zero
// from either feed is a broken provider, not a 100% disagreement.
func TestZeroMidIsMalfunctionNotTotalDisagreement(t *testing.T) {
	r := rateOf(t, crossOf("1348", "0"))

	if r.Agreement != AgreementMalfunction {
		t.Errorf("Agreement = %s, want MALFUNCTION for a zero mid", r.Agreement)
	}
	if r.Scorable() {
		t.Error("a zero mid must not be scorable")
	}
	if !strings.Contains(r.Note, "broken feed") {
		t.Errorf("Note = %q, want it to name the zero rate as a broken feed", r.Note)
	}
}

// TestNoSecondaryIsSingleSource covers a Cross configured with one provider,
// which is the shape a deployment falls back to.
func TestNoSecondaryIsSingleSource(t *testing.T) {
	c := &Cross{Primary: &fakeProvider{name: "only", mid: "1348"}}
	r := rateOf(t, c)

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	if c.Name() != "only" {
		t.Errorf("Name() = %s, want just the primary's name", c.Name())
	}
}
