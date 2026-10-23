package checks

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// unregistered is an asset with no verified home domain, so ForAsset does not
// attempt to resolve a stellar.toml — the runner behaviour tests below are
// about the runner, not about the resolver.
func unregistered() asset.Asset { return asset.Stellar("TESTC", "GISSUER") }

// stubCheck is a contributed check whose result the test controls. It records
// the subjects it was handed (saw) and the order it ran (log) so a test can
// assert what the runner resolved and when.
type stubCheck struct {
	id     string
	result CheckResult
	saw    *[]Subject
	log    *[]string
}

func (c stubCheck) Describe() Descriptor {
	return Descriptor{
		ID: c.id, Title: c.id,
		CanDetermine: "the stub decides", CannotDetermine: "the stub decides not to",
	}
}

func (c stubCheck) Run(_ context.Context, s Subject) CheckResult {
	if c.saw != nil {
		*c.saw = append(*c.saw, s)
	}
	if c.log != nil {
		*c.log = append(*c.log, c.id)
	}
	return c.result
}

// TestRunnerAggregatesEveryResultState is the runner's equivalent of the
// contract-level tests: a mock check that returns each of the three states —
// passed, failed, undetermined — and the runner must aggregate all three
// without collapsing the undetermined one into a failure.
func TestRunnerAggregatesEveryResultState(t *testing.T) {
	d := func(id string) Descriptor {
		return Descriptor{ID: id, Title: id, CanDetermine: "x", CannotDetermine: "y"}
	}
	s := Subject{Domain: "example.test"}

	r := &Runner{Checks: []Check{
		stubCheck{id: "test.pass", result: Pass(d("test.pass"), s, "ok")},
		stubCheck{id: "test.fail", result: Fail(d("test.fail"), s, "broken")},
		stubCheck{id: "test.unknown", result: Undetermined(d("test.unknown"), s, "nothing was published")},
	}}
	f := r.ForAsset(ctx(), unregistered())

	p, failed, u := f.Counts()
	if p != 1 || failed != 1 || u != 1 {
		t.Fatalf("counts = %d passed, %d failed, %d undetermined; want 1/1/1",
			p, failed, u)
	}

	// An undetermined result must never surface as a failure through the
	// runner, and a failed one must.
	for _, res := range f.Checks {
		switch res.ID {
		case "test.fail":
			if !res.Failed() {
				t.Error("test.fail was not reported as failed")
			}
		case "test.unknown":
			if res.Failed() {
				t.Error("an undetermined result was reported as failed")
			}
		}
	}
}

// TestRunnerRunsChecksInDeclarationOrder pins the ordering contract the
// runner inherits from RunAll: results come back in the order the checks were
// declared, so a reader's "first result" is deterministic.
func TestRunnerRunsChecksInDeclarationOrder(t *testing.T) {
	var order []string
	s := Subject{Domain: "example.test"}

	r := &Runner{Checks: []Check{
		stubCheck{id: "test.first", result: Pass(Descriptor{ID: "test.first"}, s, "1"), log: &order},
		stubCheck{id: "test.second", result: Pass(Descriptor{ID: "test.second"}, s, "2"), log: &order},
		stubCheck{id: "test.third", result: Pass(Descriptor{ID: "test.third"}, s, "3"), log: &order},
	}}
	f := r.ForAsset(ctx(), unregistered())

	want := []string{"test.first", "test.second", "test.third"}
	for i, id := range want {
		if f.Checks[i].ID != id {
			t.Errorf("result %d = %q, want %q — results must follow declaration order",
				i, f.Checks[i].ID, id)
		}
		if order[i] != id {
			t.Errorf("run order %d = %q, want %q", i, order[i], id)
		}
	}
}

// TestRunnerUsesSuppliedChecks covers the configured path: when the runner is
// given checks it must run exactly those and nothing from the defaults.
func TestRunnerUsesSuppliedChecks(t *testing.T) {
	s := Subject{Domain: "example.test"}
	r := &Runner{Checks: []Check{
		stubCheck{id: "test.only", result: Pass(Descriptor{ID: "test.only"}, s, "ok")},
	}}
	f := r.ForAsset(ctx(), unregistered())

	if len(f.Checks) != 1 || f.Checks[0].ID != "test.only" {
		t.Errorf("runner ran %d checks (%v), want exactly the one supplied",
			len(f.Checks), idsOf(f))
	}
}

// TestRunnerDefaultsApplyWhenNoChecksSupplied documents the fallback: a runner
// with no checks configured runs the reference default set, so a corridor
// sweep never silently runs zero checks.
func TestRunnerDefaultsApplyWhenNoChecksSupplied(t *testing.T) {
	r := &Runner{}
	got := r.checks()
	if len(got) == 0 {
		t.Fatal("a runner with no checks must fall back to the default set")
	}
	want := map[string]bool{}
	for _, c := range r.Default() {
		want[c.Describe().ID] = true
	}
	if !want["issuer.drift"] {
		t.Fatal("issuer.drift must be part of the default check set")
	}
	if len(got) != len(want) {
		t.Fatalf("checks() returned %d checks, want the %d defaults",
			len(got), len(want))
	}
	for _, c := range got {
		if !want[c.Describe().ID] {
			t.Errorf("check %q is not in the default set", c.Describe().ID)
		}
	}
}

// countingTransport records how many requests passed through it, so a test
// can assert that the runner resolved the anchor exactly once.
type countingTransport struct {
	recordedTransport
	calls int
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.calls++
	return c.recordedTransport.RoundTrip(req)
}

// TestRunnerResolvesProfileOnce covers the reason the Runner exists: the
// anchor's stellar.toml is resolved once per corridor, and every check sees
// the same profile in its subject instead of fetching it themselves.
func TestRunnerResolvesProfileOnce(t *testing.T) {
	responses := &countingTransport{recordedTransport: recordedTransport{
		http.MethodGet + " https://ngnc.online/.well-known/stellar.toml": recordedResponse{
			body: `NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"
[[CURRENCIES]]
code="NGNC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"
anchor_asset="NGN"
`},
	}}

	var saw []Subject
	r := &Runner{
		Checks: []Check{stubCheck{id: "test.sees-profile", result: CheckResult{}, saw: &saw}},
		Resolver: &anchor.Resolver{
			HTTPClient: &http.Client{Transport: responses},
		},
	}
	f := r.ForAsset(ctx(), asset.NGNC())

	if len(saw) != 1 {
		t.Fatalf("check ran %d times, want 1", len(saw))
	}
	if saw[0].Domain != "ngnc.online" {
		t.Errorf("subject domain = %q, want ngnc.online", saw[0].Domain)
	}
	if saw[0].Profile == nil {
		t.Fatal("subject profile not resolved; the runner must resolve the anchor once")
	}
	if !saw[0].Profile.SupportsAsset(asset.NGNC()) {
		t.Error("resolved profile does not list NGNC")
	}
	if len(f.Checks) != 1 {
		t.Errorf("runner produced %d results, want 1", len(f.Checks))
	}
	if responses.calls != 1 {
		t.Errorf("the stellar.toml was fetched %d times, want exactly once "+
			"— the runner's whole purpose is to resolve the anchor once per corridor",
			responses.calls)
	}
}

// TestRunnerUnresolvableAnchorKeepsChecksRunning is the sweep's resilience
// contract: a stellar.toml that cannot be fetched must not stop the checks
// that do not need it. The profile-dependent check reports itself
// undetermined; the others still produce findings.
func TestRunnerUnresolvableAnchorKeepsChecksRunning(t *testing.T) {
	r := &Runner{
		// A resolver whose transport records nothing: every fetch fails.
		Resolver: &anchor.Resolver{
			HTTPClient: &http.Client{Transport: recordedTransport{}},
		},
		Checks: []Check{
			// The real TOML check, so the nil-profile path is exercised
			// end to end rather than stubbed.
			AnchorAssetISO4217{},
			stubCheck{id: "test.runs-anyway",
				result: Pass(Descriptor{ID: "test.runs-anyway"}, Subject{}, "I need no document")},
		},
	}
	f := r.ForAsset(ctx(), asset.NGNC())

	if len(f.Checks) != 2 {
		t.Fatalf("runner produced %d results, want 2 — a failed resolve stopped the sweep",
			len(f.Checks))
	}

	var profileDependent, independent CheckResult
	for _, res := range f.Checks {
		switch res.ID {
		case AnchorAssetISO4217{}.Describe().ID:
			profileDependent = res
		case "test.runs-anyway":
			independent = res
		}
	}

	if profileDependent.Determined {
		t.Error("a check that needs an unresolved profile must report undetermined")
	}
	if !strings.Contains(profileDependent.Reason, "no stellar.toml") {
		t.Errorf("reason = %q, want it to say no stellar.toml was available",
			profileDependent.Reason)
	}
	if !independent.Passed {
		t.Error("a check that needs no profile must still run and pass after a failed resolve")
	}
}

func TestRunnerHostileTOMLIsUndetermined(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{
			name: "oversized",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, strings.Repeat("x", 100*1024+1024))
			},
		},
		{
			name: "streaming",
			handler: func(w http.ResponseWriter, r *http.Request) {
				flusher := w.(http.Flusher)
				for r.Context().Err() == nil {
					if _, err := io.WriteString(w, strings.Repeat("x", 4096)); err != nil {
						return
					}
					flusher.Flush()
				}
			},
		},
		{
			name: "slow-loris",
			handler: func(w http.ResponseWriter, r *http.Request) {
				flusher := w.(http.Flusher)
				ticker := time.NewTicker(20 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-r.Context().Done():
						return
					case <-ticker.C:
						if _, err := io.WriteString(w, "x"); err != nil {
							return
						}
						flusher.Flush()
					}
				}
			},
			timeout: 100 * time.Millisecond,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(tc.handler)
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			client := server.Client()
			client.Transport = hostRewriteTransport{base: client.Transport, target: target}
			client.Timeout = tc.timeout

			independent := Descriptor{ID: "test.independent", Title: "independent check",
				CanDetermine: "the check runs", CannotDetermine: "the check does not run"}
			runner := &Runner{
				Checks: []Check{
					AnchorAssetISO4217{},
					stubCheck{id: independent.ID, result: Pass(independent, Subject{}, "ran")},
				},
				Resolver: &anchor.Resolver{HTTPClient: client},
			}
			findings := runner.ForAsset(ctx(), asset.NGNC())
			if len(findings.Checks) != 2 {
				t.Fatalf("runner returned %d checks, want both checks to run", len(findings.Checks))
			}
			if result := findings.Checks[0]; result.Determined || result.Failed() || result.Reason == "" {
				t.Errorf("anchor check = determined %v, failed %v, reason %q; want undetermined with a reason",
					result.Determined, result.Failed(), result.Reason)
			}
			if !findings.Checks[1].Passed {
				t.Error("resolution failure stopped the independent check from running")
			}
		})
	}
}

type hostRewriteTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (t hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	urlCopy := *req.URL
	urlCopy.Scheme = t.target.Scheme
	urlCopy.Host = t.target.Host
	clone.URL = &urlCopy
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

// TestRunnerTimeoutBoundsTheSweep pins that the whole sweep for one corridor
// is bounded: a check that would outlive the runner's timeout is cancelled by
// the context the runner derives, and reports undetermined with the cause
// rather than blocking forever.
func TestRunnerTimeoutBoundsTheSweep(t *testing.T) {
	// waited records how long the context the check received actually lived.
	waited := make(chan time.Duration, 1)
	blocker := timeoutCheck{waited: waited}

	r := &Runner{
		Checks:  []Check{blocker},
		Timeout: 50 * time.Millisecond,
	}
	start := time.Now()
	f := r.ForAsset(ctx(), unregistered())
	elapsed := time.Since(start)

	if len(f.Checks) != 1 {
		t.Fatalf("runner produced %d results, want 1", len(f.Checks))
	}
	res := f.Checks[0]
	if res.Determined {
		t.Error("a check cancelled by the sweep timeout must not produce a determined result")
	}
	if !strings.Contains(res.Reason, "context") {
		t.Errorf("reason = %q, want it to name the context as the cause", res.Reason)
	}

	select {
	case d := <-waited:
		if d > 250*time.Millisecond {
			t.Errorf("check's context lived %s, far beyond the %s timeout",
				d, r.Timeout)
		}
	case <-time.After(time.Second):
		t.Fatal("the check never saw its context cancelled — the sweep hung")
	}
	if elapsed > 2*time.Second {
		t.Errorf("ForAsset took %s; the timeout did not bound the sweep", elapsed)
	}
}

// timeoutCheck reports undetermined only once its context is cancelled,
// recording how long the runner let it live.
type timeoutCheck struct {
	waited chan time.Duration
}

func (timeoutCheck) Describe() Descriptor {
	return Descriptor{
		ID: "test.timeout", Title: "waits for cancellation",
		CanDetermine: "nothing", CannotDetermine: "the context",
	}
}

func (c timeoutCheck) Run(ctx context.Context, s Subject) CheckResult {
	start := time.Now()
	<-ctx.Done()
	c.waited <- time.Since(start)
	return Undetermined(c.Describe(), s, "context cancelled: "+ctx.Err().Error())
}

// TestRunnerHandsTheClientToDefaultChecks documents the wiring that makes a
// whole sweep replayable: the HTTP client the runner was given is the client
// its default checks probe with, so a snapshot-backed client covers the sweep
// with no network at all.
func TestRunnerHandsTheClientToDefaultChecks(t *testing.T) {
	client := &http.Client{}
	r := &Runner{HTTPClient: client}

	for _, c := range r.Default() {
		switch c := c.(type) {
		case SEP10EndpointResponds:
			if c.HTTPClient != client {
				t.Error("SEP10EndpointResponds did not receive the runner's client")
			}
		case SEP24InfoListsAsset:
			if c.HTTPClient != client {
				t.Error("SEP24InfoListsAsset did not receive the runner's client")
			}
		case SEP6InfoListsAsset:
			if c.HTTPClient != client {
				t.Error("SEP6InfoListsAsset did not receive the runner's client")
			}
		case IssuerAuthFlags:
			if c.HTTPClient != client {
				t.Error("IssuerAuthFlags did not receive the runner's client")
			}
		}
	}

	// Without a client the runner supplies its own guarded one, so a URL an
	// audited anchor publishes can never reach the server's own network.
	plain := &Runner{}
	if got := plain.client(); got == nil {
		t.Fatal("runner with no HTTPClient must supply a client")
	} else if _, err := got.Get("http://127.0.0.1:1/"); err == nil ||
		!strings.Contains(err.Error(), "refusing to") {
		t.Errorf("the default client must be the guarded one; got %v", err)
	}
}

// stubMetric is a corridor metric whose result the test controls. It records
// the subjects it saw so a test can assert what the runner populated.
type stubMetric struct {
	id     string
	result MetricResult
	saw    *[]Subject
}

func (m stubMetric) Describe() Descriptor {
	return Descriptor{
		ID: m.id, Scope: ScopeCorridor, Venue: VenueOrderBook, Title: m.id,
		CanDetermine: "the stub decides", CannotDetermine: "the stub decides not to",
	}
}

func (m stubMetric) Run(_ context.Context, s Subject) MetricResult {
	if m.saw != nil {
		*m.saw = append(*m.saw, s)
	}
	res := m.result
	res.ID = m.id
	return res
}

// TestRunnerForCorridorRunsMetrics covers the wiring #577 was filed about: a
// corridor sweep must reach a Metric, and findings.metrics must be non-empty.
func TestRunnerForCorridorRunsMetrics(t *testing.T) {
	var saw []Subject
	r := &Runner{
		Checks: []Check{stubCheck{id: "test.check", result: Pass(Descriptor{ID: "test.check"}, Subject{}, "ok")}},
		Metrics: []Metric{stubMetric{
			id:  "test.metric",
			saw: &saw,
			result: MetricValue(Descriptor{ID: "test.metric"}, Subject{},
				decimal.NewFromInt(7), UnitCount, "seven"),
		}},
	}

	f := r.ForCorridor(ctx(), Corridor{Send: asset.USDC(), Receive: unregistered()})

	if len(f.Metrics) != 1 || f.Metrics[0].ID != "test.metric" {
		t.Fatalf("ForCorridor produced %d metrics; a corridor sweep must run the "+
			"configured metric", len(f.Metrics))
	}
	if !f.Metrics[0].Determined || f.Metrics[0].Value.IntPart() != 7 {
		t.Errorf("metric result = %+v, want determined 7", f.Metrics[0])
	}
	if len(f.Checks) != 1 {
		t.Errorf("ForCorridor produced %d checks, want 1 — checks still run", len(f.Checks))
	}

	// The subject a corridor metric receives is the pair, not the receive
	// asset alone: the metric layer was unusable until Send/Receive were
	// populated.
	if len(saw) != 1 {
		t.Fatalf("metric saw %d subjects, want 1", len(saw))
	}
	if saw[0].Send.Code != "USDC" || saw[0].Receive.Code != "TESTC" {
		t.Errorf("metric subject pair = %s -> %s, want USDC -> TESTC",
			saw[0].Send.Code, saw[0].Receive.Code)
	}
}

// TestRunnerForCorridorDefaultsToAMetricSet pins the fallback: a runner with no
// metrics configured measures the default set, so an unconfigured deployment
// still publishes quantities rather than an empty block.
func TestRunnerForCorridorDefaultsToAMetricSet(t *testing.T) {
	r := &Runner{}
	got := r.DefaultMetrics(Corridor{})
	if len(got) == 0 {
		t.Fatal("a runner with no metrics must fall back to the default set")
	}
	for _, m := range got {
		if err := m.Describe().ValidateAsMetric(); err != nil {
			t.Errorf("default metric %q is invalid: %v", m.Describe().ID, err)
		}
	}
}

// TestRunnerForAssetDoesNotMeasureMetrics keeps the single-asset path
// checks-only. A corridor metric needs a pair, and ForAsset has none, so it
// must not invent one or silently run a metric against a half-populated
// subject.
func TestRunnerForAssetDoesNotMeasureMetrics(t *testing.T) {
	r := &Runner{
		Checks:  []Check{stubCheck{id: "test.check", result: Pass(Descriptor{ID: "test.check"}, Subject{}, "ok")}},
		Metrics: []Metric{stubMetric{id: "test.metric", result: MetricValue(Descriptor{ID: "test.metric"}, Subject{}, decimal.Zero, UnitCount, "zero")}},
	}
	if f := r.ForAsset(ctx(), unregistered()); len(f.Metrics) != 0 {
		t.Errorf("ForAsset ran %d metrics; the single-asset path must stay "+
			"checks-only", len(f.Metrics))
	}
}

// TestRunnerMetricsAreOptOutable covers the explicit empty slice: a caller that
// has its own request budget must be able to measure no metrics at all, and
// that is distinct from "use the defaults".
func TestRunnerMetricsAreOptOutable(t *testing.T) {
	r := &Runner{
		Checks:  []Check{stubCheck{id: "test.check", result: Pass(Descriptor{ID: "test.check"}, Subject{}, "ok")}},
		Metrics: []Metric{},
	}
	if f := r.ForCorridor(ctx(), Corridor{Send: asset.USDC(), Receive: unregistered()}); len(f.Metrics) != 0 {
		t.Errorf("an explicitly empty metric set produced %d metrics", len(f.Metrics))
	}
}

// TestRunnerForCorridorPassesTheLadderContext pins the fields the metrics were
// written against. A book metric cannot tell a structural absence from an idle
// market without Integrity, and a reference-derived metric must not score
// against a malfunctioning benchmark.
func TestRunnerForCorridorPassesTheLadderContext(t *testing.T) {
	var saw []Subject
	r := &Runner{
		Checks: []Check{stubCheck{id: "test.check", result: Pass(Descriptor{ID: "test.check"}, Subject{}, "ok")}},
		Metrics: []Metric{stubMetric{
			id: "test.metric", saw: &saw,
			result: MetricValue(Descriptor{ID: "test.metric"}, Subject{}, decimal.Zero, UnitCount, "zero"),
		}},
	}
	r.ForCorridor(ctx(), Corridor{
		Send:       asset.USDC(),
		Receive:    unregistered(),
		Integrity:  "DERIVATIVE",
		Underlying: asset.NGNC(),
		Reference:  refrate.Rate{Agreement: refrate.AgreementMalfunction},
	})

	if len(saw) != 1 {
		t.Fatalf("metric saw %d subjects, want 1", len(saw))
	}
	if saw[0].Integrity != "DERIVATIVE" {
		t.Errorf("Integrity = %q, want DERIVATIVE", saw[0].Integrity)
	}
	if saw[0].Underlying.Code != "NGNC" {
		t.Errorf("Underlying = %q, want NGNC", saw[0].Underlying.Code)
	}
	if saw[0].ReferenceAgreement != "MALFUNCTION" {
		t.Errorf("ReferenceAgreement = %q, want MALFUNCTION", saw[0].ReferenceAgreement)
	}
}

// idsOf is a helper for failure messages.
func idsOf(f *Findings) []string {
	out := make([]string, len(f.Checks))
	for i, r := range f.Checks {
		out[i] = r.ID
	}
	return out
}

// nairaTOML is a minimal valid stellar.toml for ngnc.online, the single home
// domain shared by NGNC, GHSC and KESC (one issuer, one domain).
const nairaTOML = `NETWORK_PASSPHRASE="Public Global Stellar Network ; September 2015"

[[CURRENCIES]]
code="NGNC"
issuer="GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
status="live"
anchor_asset="NGN"
`

// TestRunnerCachesProfileAcrossSharedDomain is issue #600: NGNC, GHSC and
// KESC share one issuer and domain, so a sweep measuring all three must
// resolve the stellar.toml once instead of once per asset.
func TestRunnerCachesProfileAcrossSharedDomain(t *testing.T) {
	responses := &countingTransport{recordedTransport: recordedTransport{
		http.MethodGet + " https://ngnc.online/.well-known/stellar.toml": recordedResponse{
			body: nairaTOML,
		},
	}}

	r := &Runner{
		Resolver: &anchor.Resolver{HTTPClient: &http.Client{Transport: responses}},
	}

	// Capture the subjects each asset's check saw, via a shared slice.
	var saw []Subject
	r.Checks = []Check{stubCheck{id: "test.sees-profile", saw: &saw}}

	for _, a := range []asset.Asset{asset.NGNC(), asset.GHSC(), asset.KESC()} {
		f := r.ForAsset(ctx(), a)
		if len(f.Checks) != 1 {
			t.Fatalf("ForAsset(%s) produced %d results, want 1", a.Code, len(f.Checks))
		}
	}

	if len(saw) != 3 {
		t.Fatalf("check ran %d times, want 3 (once per asset)", len(saw))
	}
	if responses.calls != 1 {
		t.Errorf("stellar.toml fetched %d times, want exactly once per domain per sweep",
			responses.calls)
	}
	for _, s := range saw {
		if s.Profile == nil || s.Profile.Domain != "ngnc.online" {
			t.Error("subject did not carry the resolved ngnc.online profile")
		}
	}
}

// TestRunnerCachedProfilePreservesFetchTime is issue #600: when a profile is
// served from the sweep cache, the evidence it produces carries the original
// fetch time rather than being re-timestamped to now.
func TestRunnerCachedProfilePreservesFetchTime(t *testing.T) {
	responses := &countingTransport{recordedTransport: recordedTransport{
		http.MethodGet + " https://ngnc.online/.well-known/stellar.toml": recordedResponse{
			body: nairaTOML,
		},
	}}

	r := &Runner{
		Checks:   []Check{AnchorAssetISO4217{}},
		Resolver: &anchor.Resolver{HTTPClient: &http.Client{Transport: responses}},
	}

	// First asset on the domain: fetch happens and the profile is cached.
	first := r.ForAsset(ctx(), asset.NGNC())
	// Second asset on the same domain: cache hit, no additional fetch.
	second := r.ForAsset(ctx(), asset.GHSC())

	if responses.calls != 1 {
		t.Errorf("stellar.toml fetched %d times, want exactly once", responses.calls)
	}

	evidenceOf := func(f *Findings) time.Time {
		for _, res := range f.Checks {
			if len(res.Evidence) > 0 {
				return res.Evidence[0].ObservedAt
			}
		}
		return time.Time{}
	}

	firstAt, secondAt := evidenceOf(first), evidenceOf(second)
	if firstAt.IsZero() {
		t.Fatal("first result carried no evidence timestamp")
	}
	if !secondAt.Equal(firstAt) {
		t.Errorf("cached profile evidence = %v, want original fetch time %v", secondAt, firstAt)
	}
}
