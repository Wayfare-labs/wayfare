package refrate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// This file covers the uncorroborated measurement: the rate produced when only
// one reference provider answers. It is the case a corridor is most likely to
// be scored against in production — a free-tier feed that is rate-limited,
// timing out, or the only one a deployment configured — and it is the case
// where a wrong answer is least visible, because there is no second figure
// beside it to look wrong against.
//
// The contract pinned here comes from docs/adr-reference-mids.md item 4
// ("SINGLE: when only one provider answers, use it and explicitly identify the
// result as uncorroborated") and from the two constraints in CONTRIBUTING.md
// that the case keeps tripping over: an unavailable quantity is unknown and
// never zero, and nothing is synthesised to fill a gap.

// stubSource answers with a rate assembled the way a real provider assembles
// one, and can be made to carry cross-check fields already, so the
// single-source paths can be shown to leave a claim of a cross-check behind
// only when a cross-check actually ran at this level.
type stubSource struct {
	name      string
	mid       string
	err       error
	asOf      time.Time
	fetchedAt time.Time

	secondaryMid    string
	secondarySource string
	divergencePct   string
	note            string

	// secondaryAsOf is carried as a time rather than a string; the stub sets
	// it from a fixed instant so the clearing of that field is observable.
	secondaryAsOf time.Time
}

func (s *stubSource) Name() string { return s.name }

func (s *stubSource) Rate(_ context.Context, base, quote string) (Rate, error) {
	if s.err != nil {
		return Rate{}, s.err
	}
	r := Rate{
		Base:            base,
		Quote:           quote,
		Source:          s.name,
		SecondarySource: s.secondarySource,
		Note:            s.note,
	}
	if s.mid != "" {
		r.Mid = decimal.RequireFromString(s.mid)
	}
	if !s.asOf.IsZero() {
		r.AsOf = s.asOf
	}
	if !s.fetchedAt.IsZero() {
		r.FetchedAt = s.fetchedAt
	}
	if s.secondaryMid != "" {
		r.SecondaryMid = decimal.RequireFromString(s.secondaryMid)
	}
	if !s.secondaryAsOf.IsZero() {
		r.SecondaryAsOf = s.secondaryAsOf
	}
	if s.divergencePct != "" {
		r.DivergencePct = decimal.RequireFromString(s.divergencePct)
	}
	return r, nil
}

// singleDown is the failure shape a secondary or primary produces when it is
// simply not answering.
var singleDown = &ErrUnavailable{Source: "down", Err: errors.New("dial tcp: connection refused")}

// assertNoCrossCheckClaimed pins the absent side of the uncorroborated state.
//
// Every field here would otherwise read as a completed cross-check: a secondary
// mid, a secondary source, a secondary as-of, and a divergence. Leaving any of
// them populated alongside AgreementSingle publishes a comparison that never
// happened, and — because the run store reconstructs the agreement from
// exactly these fields (server.staleAgreement treats a record with no secondary
// mid and no secondary source as SINGLE) — it also silently changes what a
// stored reading says about itself when it is served back later.
func assertNoCrossCheckClaimed(t *testing.T, r Rate) {
	t.Helper()

	if !r.SecondaryMid.IsZero() {
		t.Errorf("SecondaryMid = %s, want no second mid: only one provider answered", r.SecondaryMid)
	}
	if r.SecondarySource != "" {
		t.Errorf("SecondarySource = %q, want empty: no second provider was heard from", r.SecondarySource)
	}
	if !r.SecondaryAsOf.IsZero() {
		t.Errorf("SecondaryAsOf = %s, want the zero time: there is no second observation to date", r.SecondaryAsOf)
	}
	// A zero divergence and an unmeasured divergence are different facts.
	// The decimal zero value is this project's sentinel for "absent": the
	// wire and the run store both omit it (route/wire.go and
	// monitor/monitor.go each guard on IsZero before writing a figure), so a
	// populated divergence here would publish "0.0000%" — read as the two
	// providers agreeing exactly — for a corridor where one of them never
	// spoke.
	if !r.DivergencePct.IsZero() {
		t.Errorf("DivergencePct = %s, want the absent sentinel: with one provider there is nothing to diverge from",
			r.DivergencePct)
	}
}

// TestAgreementStringNamesEveryBand covers the renderer the published claim
// goes through.
//
// reference_agreement on the wire, the monitor's log line and a replayed
// reading are all produced by this one method, and the word for an
// uncorroborated rate is the difference between a reader understanding "one
// feed said so" and understanding "two feeds agreed".
func TestAgreementStringNamesEveryBand(t *testing.T) {
	cases := []struct {
		agreement Agreement
		want      string
	}{
		// The zero value has to render as SINGLE rather than as an empty
		// string: a Rate that was never cross-checked is a single-source
		// rate, and a consumer switching on this field must not see "".
		{AgreementSingle, "SINGLE"},
		{AgreementAgree, "AGREE"},
		{AgreementDisagree, "DISAGREE"},
		{AgreementStale, "STALE"},
		{AgreementMalfunction, "MALFUNCTION"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.agreement.String(); got != tc.want {
				t.Errorf("Agreement(%d).String() = %q, want %q", int(tc.agreement), got, tc.want)
			}
		})
	}
}

// TestSingleWhenNoSecondaryIsConfigured covers the deployment shape a
// single-provider install actually has.
//
// There is no failed provider to report here, so the honest Note is empty: the
// rate is uncorroborated because nothing else was asked, not because something
// was asked and did not answer. A note inventing a failure would describe an
// event that did not happen.
func TestSingleWhenNoSecondaryIsConfigured(t *testing.T) {
	primary := &stubSource{
		name:      "exchangerate-api",
		mid:       "1348.0585",
		asOf:      time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		fetchedAt: time.Date(2026, 8, 21, 6, 30, 0, 0, time.UTC),
	}
	c := &Cross{Primary: primary}

	r := rateOf(t, c)

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	if r.Note != "" {
		t.Errorf("Note = %q, want empty: no provider failed, so there is nothing to explain", r.Note)
	}
	assertNoCrossCheckClaimed(t, r)
	if !r.Scorable() {
		t.Error("a single-source rate is scorable; it is simply not corroborated")
	}
	if got := c.Name(); got != "exchangerate-api" {
		t.Errorf("Name() = %q, want the lone provider's name with no composite separator", got)
	}

	// The survivor's own identity must arrive unaltered — the figure a reader
	// audits has to be the figure the provider published.
	if want := decimal.RequireFromString("1348.0585"); !r.Mid.Equal(want) {
		t.Errorf("Mid = %s, want %s", r.Mid, want)
	}
	if r.Source != "exchangerate-api" {
		t.Errorf("Source = %q, want the provider that answered", r.Source)
	}
	if r.Base != "USD" || r.Quote != "NGN" {
		t.Errorf("pair = %s/%s, want USD/NGN", r.Base, r.Quote)
	}
	if !r.AsOf.Equal(primary.asOf) {
		t.Errorf("AsOf = %s, want the provider's own stamp %s", r.AsOf, primary.asOf)
	}
	if !r.FetchedAt.Equal(primary.fetchedAt) {
		t.Errorf("FetchedAt = %s, want %s: when we asked is a separate fact from when it was set",
			r.FetchedAt, primary.fetchedAt)
	}
}

// TestSingleWithNoSecondaryAndAFailingProviderIsAnError pins the other half of
// a one-provider deployment: when the only provider does not answer, there is
// no benchmark, and the correct output is the provider's own error rather than
// a Rate at all.
//
// This is the case that turns into a silent fabrication if it is written
// carelessly — returning a zero Rate with a nil error would hand the routing
// engine a benchmark of nothing, which scores every route as a total loss or a
// total gain depending on sign.
func TestSingleWithNoSecondaryAndAFailingProviderIsAnError(t *testing.T) {
	sentinel := errors.New("dial tcp: i/o timeout")
	c := &Cross{Primary: &stubSource{name: "exchangerate-api", err: sentinel}}

	r, err := c.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatalf("want an error when the only provider fails, got rate %+v", r)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want the provider's own error passed through unchanged", err)
	}
	if r != (Rate{}) {
		t.Errorf("rate = %+v, want the zero Rate: nothing usable came back, so nothing may be returned", r)
	}
}

// TestSecondaryFailureDegradesToThePrimaryUncorroborated is the production
// case the backlog entry calls the likeliest: one feed answering, the other
// not, and the corridor still being measured.
func TestSecondaryFailureDegradesToThePrimaryUncorroborated(t *testing.T) {
	primary := &stubSource{
		name:      "exchangerate-api",
		mid:       "1348.0585",
		asOf:      time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		fetchedAt: time.Date(2026, 8, 21, 6, 30, 0, 0, time.UTC),
	}
	c := &Cross{Primary: primary, Secondary: &stubSource{name: "currency-api", err: singleDown}}

	r := rateOf(t, c)

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	assertNoCrossCheckClaimed(t, r)
	if !r.Scorable() {
		t.Error("a degraded single-source rate must still be scored against; refusing to score it would discard a usable benchmark")
	}
	if !r.Mid.Equal(decimal.RequireFromString("1348.0585")) || r.Source != "exchangerate-api" {
		t.Errorf("scored against %s from %s, want 1348.0585 from the provider that answered", r.Mid, r.Source)
	}
	if !r.FetchedAt.Equal(primary.fetchedAt) {
		t.Errorf("FetchedAt = %s, want the primary's own fetch time preserved through the degradation", r.FetchedAt)
	}

	// The note is the only thing that distinguishes "degraded" from
	// "never cross-checked", and it has to name the provider that is missing
	// — not the one that answered.
	if r.Note == "" {
		t.Fatal("Note is empty: a degraded rate must say it is uncorroborated and why")
	}
	if !strings.HasPrefix(r.Note, "uncorroborated: ") {
		t.Errorf("Note = %q, want it to open by stating the rate is uncorroborated", r.Note)
	}
	if !strings.Contains(r.Note, "currency-api") {
		t.Errorf("Note = %q, want it to name the provider that did not answer", r.Note)
	}
	if strings.Contains(r.Note, "exchangerate-api") {
		t.Errorf("Note = %q, want it to describe the missing provider, not the surviving one", r.Note)
	}
}

// TestPrimaryFailureDegradesToTheSecondaryUncorroborated is the mirror, and
// the direction where an error would be the lazy answer.
func TestPrimaryFailureDegradesToTheSecondaryUncorroborated(t *testing.T) {
	secondary := &stubSource{
		name:      "currency-api",
		mid:       "1350.2568",
		asOf:      time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
		fetchedAt: time.Date(2026, 8, 21, 7, 0, 0, 0, time.UTC),
	}
	c := &Cross{Primary: &stubSource{name: "exchangerate-api", err: singleDown}, Secondary: secondary}

	r := rateOf(t, c)

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
	}
	assertNoCrossCheckClaimed(t, r)
	if !r.Mid.Equal(decimal.RequireFromString("1350.2568")) || r.Source != "currency-api" {
		t.Errorf("scored against %s from %s, want 1350.2568 from the secondary", r.Mid, r.Source)
	}
	if !r.FetchedAt.Equal(secondary.fetchedAt) {
		t.Errorf("FetchedAt = %s, want the secondary's fetch time to survive the substitution", r.FetchedAt)
	}
	if !strings.Contains(r.Note, "exchangerate-api was unavailable") {
		t.Errorf("Note = %q, want it to name the failed primary and its failure class", r.Note)
	}
}

// TestDegradationClearsACrossCheckInheritedFromANestedComposite keeps the
// single-source state self-consistent when a Cross wraps another provider that
// already reconciled two feeds of its own.
//
// The outer agreement is genuinely SINGLE — nothing answered the outer
// secondary — while the inner composite's secondary fields would otherwise
// survive into the published record. Kept, the wire would read
// reference_agreement: SINGLE beside a reference_secondary_mid, and the run
// store would reconstruct the band from those figures on replay and report
// something other than what was measured. Clearing them is what makes the
// stored reading say back what the live one said.
func TestDegradationClearsACrossCheckInheritedFromANestedComposite(t *testing.T) {
	nested := &stubSource{
		name:            "exchangerate-api",
		mid:             "1348.0585",
		secondaryMid:    "1349",
		secondarySource: "inner-secondary",
		secondaryAsOf:   time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
		divergencePct:   "0.07",
		note:            "providers disagree by 0.07%; scored against exchangerate-api (1348.0585), the more conservative of the two",
	}

	t.Run("outer secondary fails", func(t *testing.T) {
		r := rateOf(t, &Cross{Primary: nested, Secondary: &stubSource{name: "currency-api", err: singleDown}})

		if r.Agreement != AgreementSingle {
			t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
		}
		assertNoCrossCheckClaimed(t, r)
	})

	t.Run("no outer secondary configured", func(t *testing.T) {
		r := rateOf(t, &Cross{Primary: nested})

		if r.Agreement != AgreementSingle {
			t.Errorf("Agreement = %s, want SINGLE", r.Agreement)
		}
		assertNoCrossCheckClaimed(t, r)
	})
}

// TestSingleSourceZeroRateIsRefusedNotScored is the defect the thin coverage
// hid.
//
// A mid of exactly zero is a broken feed, not a rate, and reconcile has refused
// it from either side ever since the band rule was written. That guard only
// runs when both providers answer, so every single-source path returned ahead
// of it and handed the caller a zero benchmark marked usable and scorable —
// making the likeliest production path the only way a zero reached a verdict.
// Zero is the value CONTRIBUTING forbids in place of an unavailable figure.
func TestSingleSourceZeroRateIsRefusedNotScored(t *testing.T) {
	broken := &stubSource{
		name: "exchangerate-api",
		mid:  "0",
		asOf: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
	}

	cases := []struct {
		name string
		c    *Cross
	}{
		{"no secondary configured", &Cross{Primary: broken}},
		{"secondary failed", &Cross{Primary: broken, Secondary: &stubSource{name: "currency-api", err: singleDown}}},
		{
			"primary failed",
			&Cross{
				Primary: &stubSource{name: "exchangerate-api", err: singleDown},
				// The zero mid on the surviving side, not the failing one.
				Secondary: &stubSource{name: "currency-api", mid: "0",
					asOf: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := rateOf(t, tc.c)

			if r.Scorable() {
				t.Errorf("Scorable() = true for a zero mid (agreement %s); a benchmark of zero is not a benchmark",
					r.Agreement)
			}
			if r.Agreement != AgreementMalfunction {
				t.Errorf("Agreement = %s, want MALFUNCTION: this is the same broken feed reconcile already refuses",
					r.Agreement)
			}
			if r.Agreement == AgreementSingle {
				t.Error("a zero rate must never be reported as merely uncorroborated — that calls it usable")
			}
			// Refusing is not the same as repairing. The zero stays the
			// figure that was actually quoted, so the reason can name it.
			if !r.Mid.IsZero() {
				t.Errorf("Mid = %s, want the quoted 0 left as measured: nothing is substituted for a broken feed", r.Mid)
			}
			if !strings.Contains(r.Note, "a zero rate is a broken feed") {
				t.Errorf("Note = %q, want the reason reconcile gives for a zero mid", r.Note)
			}
			if !strings.Contains(r.Note, "not scored") {
				t.Errorf("Note = %q, want it to state that nothing was scored", r.Note)
			}
			assertNoCrossCheckClaimed(t, r)
		})
	}
}

// TestBothFailingReturnsNoPartialRate pins the shape of the failure, not just
// that one occurred.
//
// The rate a caller receives on the error path has to be empty. A Rate carrying
// the primary's Mid and a nil-error contract from the caller's point of view is
// how a missing benchmark ends up scored anyway.
func TestBothFailingReturnsNoPartialRate(t *testing.T) {
	c := &Cross{
		Primary:   &stubSource{name: "exchangerate-api", mid: "1348.0585", err: singleDown},
		Secondary: &stubSource{name: "currency-api", mid: "1350.2568", err: singleDown},
	}

	r, err := c.Rate(context.Background(), "USD", "NGN")
	if err == nil {
		t.Fatal("want an error when neither provider answered")
	}
	if r != (Rate{}) {
		t.Errorf("rate = %+v, want the zero Rate: no answer is not the same as an answer of nothing", r)
	}
}

// leakySource violates the Provider contract by returning a rate *and* an
// error. It exists to pin what Cross does with a half-answer.
//
// A provider that fails is allowed to hand back a populated Rate by accident —
// the zero Rate and a non-nil error are only a convention. If the composite
// passed that figure through on its error path, a caller that inspected the
// rate before the error, or a future refactor that swallowed the error, would
// be scoring against a number no provider stands behind. Discarding it is the
// cheap guarantee.
type leakySource struct {
	name string
	mid  string
	err  error
}

func (l *leakySource) Name() string { return l.name }

func (l *leakySource) Rate(_ context.Context, base, quote string) (Rate, error) {
	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    decimal.RequireFromString(l.mid),
		Source: l.name,
		AsOf:   time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
	}, l.err
}

// TestNoRateIsEverSurfacedOnTheErrorPath holds across every way the cross can
// fail.
//
// The constraint this serves is the project's central one: a rate that did not
// come from a live, answering source is never returned, so the error path has
// to yield an empty Rate and not the closest figure that happened to be in
// hand.
func TestNoRateIsEverSurfacedOnTheErrorPath(t *testing.T) {
	down := errors.New("dial tcp: i/o timeout")

	cases := []struct {
		name string
		c    *Cross
	}{
		{"both providers fail", &Cross{
			Primary:   &leakySource{name: "exchangerate-api", mid: "1348.0585", err: down},
			Secondary: &leakySource{name: "currency-api", mid: "1350.2568", err: down},
		}},
		{"only provider fails", &Cross{
			Primary: &leakySource{name: "exchangerate-api", mid: "1348.0585", err: down},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := tc.c.Rate(context.Background(), "USD", "NGN")
			if err == nil {
				t.Fatal("want an error")
			}
			if r != (Rate{}) {
				t.Errorf("rate = %+v, want the zero Rate: a rate returned beside an error is a rate that can be used by mistake", r)
			}
		})
	}
}

// TestCrossWithoutAPrimaryIsAnErrorNotAPanic pins the misconfigured composite.
//
// A Cross with no primary has no provider to degrade *to*, so it cannot
// produce a single-source rate; the honest answer is the error. Reaching for
// the surviving secondary here would be exactly the "synthesise something to
// fill the gap" move the constraints rule out, and leaving the nil dereference
// unguarded would turn a config mistake into a crash in the monitor's loop.
func TestCrossWithoutAPrimaryIsAnErrorNotAPanic(t *testing.T) {
	for _, c := range []*Cross{{}, {Secondary: &stubSource{name: "currency-api", mid: "1350"}}} {
		r, err := c.Rate(context.Background(), "USD", "NGN")
		if err == nil {
			t.Fatal("want an error when no primary is configured")
		}
		if !strings.Contains(err.Error(), "primary") {
			t.Errorf("error = %v, want it to name the missing primary", err)
		}
		if r != (Rate{}) {
			t.Errorf("rate = %+v, want the zero Rate", r)
		}
	}
}

// TestStalenessIsNotInferredFromASingleObservation keeps STALE a two-provider
// judgement.
//
// STALE means the two answers describe materially different moments, so with
// one answer there is no gap to read as lag. A single-source rate carrying an
// ancient as-of must therefore stay SINGLE, and the thing that rejects the
// stale figure is the age bound on Checked — not an improvised reclassification
// here. Reporting STALE from one stamp would claim a comparison that never
// happened; silently accepting the old figure would be the worse error, so the
// pairing of the two types is what this pins.
func TestStalenessIsNotInferredFromASingleObservation(t *testing.T) {
	ancient := time.Now().Add(-30 * 24 * time.Hour)
	survivor := &stubSource{name: "currency-api", mid: "1350", asOf: ancient}

	r := rateOf(t, &Cross{Primary: survivor, Secondary: &stubSource{name: "exchangerate-api", err: singleDown}})

	if r.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE: one answer cannot be stale *relative to* anything", r.Agreement)
	}
	if !r.AsOf.Equal(ancient) {
		t.Errorf("AsOf = %s, want the provider's own stamp left as measured, even though it is 30 days old", r.AsOf)
	}

	// The age bound, not the agreement band, is what refuses this rate.
	bounded := &Checked{Inner: &Cross{Primary: survivor}, MaxAge: 48 * time.Hour}
	if _, err := bounded.Rate(context.Background(), "USD", "NGN"); err == nil {
		t.Error("want Checked to refuse a 30-day-old rate; an uncorroborated rate is not a licence to be stale")
	}
}

// TestSingleIsDistinguishableFromAnExactAgreement holds apart the two states
// that both report a zero divergence.
//
// Two providers quoting an identical mid produce DivergencePct zero; one
// provider produces DivergencePct zero because nothing was measured. Both must
// survive the trip to the wire as different facts, which is why the secondary
// source and mid — not the divergence — are what separate them. Collapsing the
// two would let a benchmark nobody cross-checked read as a benchmark two feeds
// confirmed.
func TestSingleIsDistinguishableFromAnExactAgreement(t *testing.T) {
	exact := rateOf(t, &Cross{
		Primary:   &stubSource{name: "exchangerate-api", mid: "1348", asOf: time.Unix(0, 0).UTC()},
		Secondary: &stubSource{name: "currency-api", mid: "1348", asOf: time.Unix(0, 0).UTC()},
	})
	single := rateOf(t, &Cross{
		Primary:   &stubSource{name: "exchangerate-api", mid: "1348", asOf: time.Unix(0, 0).UTC()},
		Secondary: &stubSource{name: "currency-api", err: singleDown},
	})

	if exact.DivergencePct.IsZero() != single.DivergencePct.IsZero() {
		t.Errorf("both states report no divergence, so the zero is not what should distinguish them")
	}
	if exact.Agreement != AgreementAgree {
		t.Errorf("two providers quoting the same mid = %s, want AGREE", exact.Agreement)
	}
	if exact.SecondarySource == "" || exact.SecondaryMid.IsZero() {
		t.Error("an exact agreement still records the second observation it compared")
	}
	if single.Agreement != AgreementSingle {
		t.Errorf("one provider answering = %s, want SINGLE", single.Agreement)
	}
	if single.SecondarySource != "" {
		t.Error("an uncorroborated rate must not name a second source")
	}
	if exact.Agreement == single.Agreement {
		t.Error("AGREE and SINGLE must stay distinguishable on the wire")
	}
}

// TestUncorroboratedSurvivesTheCacheAndIsNotUpgraded is the case a scheduled
// monitor actually runs: both providers answered an hour ago, one is down now,
// and the surviving figure is still inside its own age bound.
//
// Two claims matter here. The degradation has to be stable across repeated
// calls — a corridor must not flicker between AGREE and SINGLE as the cache
// turns over — and the reused rate has to keep the older FetchedAt. Resetting
// it while the second feed is down would present a stale figure as freshly
// fetched, which is the one thing the cache's own contract forbids.
func TestUncorroboratedSurvivesTheCacheAndIsNotUpgraded(t *testing.T) {
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	p1 := &countingProvider{mid: "1348"}
	p2 := &countingProvider{mid: "1350"}

	stack := &Cross{
		// The primary stays inside its bound across the whole test; only the
		// secondary's cache ages out.
		Primary:   &Cached{Inner: p1, TTL: 24 * time.Hour, Clock: clock},
		Secondary: &Cached{Inner: p2, TTL: time.Hour, Clock: clock},
	}

	first := rateOf(t, stack)
	if first.Agreement != AgreementAgree {
		t.Fatalf("Agreement = %s on the first call, want AGREE while both feeds answer", first.Agreement)
	}
	if first.FetchedAt.IsZero() {
		t.Fatal("FetchedAt is zero: a fetched rate has to carry when it was fetched")
	}
	fetchedAt := first.FetchedAt

	p2.err = context.DeadlineExceeded
	now = now.Add(2 * time.Hour)

	second := rateOf(t, stack)
	if second.Agreement != AgreementSingle {
		t.Errorf("Agreement = %s, want SINGLE once the secondary's cache aged out and its provider timed out",
			second.Agreement)
	}
	assertNoCrossCheckClaimed(t, second)
	if !strings.Contains(second.Note, "uncorroborated") {
		t.Errorf("Note = %q, want the degraded reading to say so", second.Note)
	}
	if !second.FetchedAt.Equal(fetchedAt) {
		t.Errorf("FetchedAt = %s, want the original %s: the rate was reused from cache, not refetched",
			second.FetchedAt, fetchedAt)
	}

	// A third call on the same degraded footing must report the identical
	// state, and must not re-probe the primary's live cache.
	third := rateOf(t, stack)
	if third.Agreement != AgreementSingle || third.Note != second.Note {
		t.Errorf("third call = %s (%q), want the same SINGLE state and reason as the second (%s, %q)",
			third.Agreement, third.Note, second.Agreement, second.Note)
	}
	if got := p1.calls.Load(); got != 1 {
		t.Errorf("primary was probed %d times, want 1: a secondary's outage must not re-fetch the surviving feed", got)
	}

	// When the secondary comes back, the next measurement is corroborated
	// again — the degradation is a state, not a verdict stamped on the pair.
	p2.err = nil
	now = now.Add(time.Hour)
	// Drop both entries so the recovery is a real re-query and not a hit
	// served from the pre-outage cache.
	stack.Primary.(*Cached).Invalidate("USD", "NGN")
	stack.Secondary.(*Cached).Invalidate("USD", "NGN")
	recovered := rateOf(t, stack)
	if recovered.Agreement != AgreementAgree {
		t.Errorf("Agreement = %s after the secondary recovered, want AGREE", recovered.Agreement)
	}
	if recovered.SecondarySource == "" {
		t.Error("a recovered cross-check must record the second observation again")
	}
}

// TestSingleNameStillCompositesTheComposite keeps provider identity intact
// when the cross-check is configured.
//
// Name is how a reader finds the source to audit, and the single-provider form
// and the two-provider form differ by more than cosmetics: a trailing "+" with
// nothing after it, or a composite name on a run that only ever asked one
// feed, both point a reader at a provider nobody queried.
func TestSingleNameStillCompositesTheComposite(t *testing.T) {
	t.Run("two providers", func(t *testing.T) {
		c := &Cross{
			Primary:   &stubSource{name: "exchangerate-api"},
			Secondary: &stubSource{name: "currency-api"},
		}
		if got, want := c.Name(), "exchangerate-api+currency-api"; got != want {
			t.Errorf("Name() = %q, want %q", got, want)
		}
	})

	t.Run("one provider", func(t *testing.T) {
		c := &Cross{Primary: &stubSource{name: "exchangerate-api"}}
		if got := c.Name(); got != "exchangerate-api" {
			t.Errorf("Name() = %q, want no separator when no secondary is configured", got)
		}
	})
}
