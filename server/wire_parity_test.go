package server

// This file is one half of the pair covering GitHub issue #113 (backlog #5):
// ToCorridorJSON, staleJSON and cmd/ladder -json must agree on the field set,
// and today none of them is compared with another.
//
// The three producers live in three different packages — route.ToCorridorJSON
// is exported, staleJSON here is not, and cmd/ladder is package main, which
// nothing outside it can import — so one test function spanning all three
// cannot exist as an ordinary Go unit test. The comparison is split instead
// into two tests that share the same wire type and the same documented
// exceptions:
//
//   - TestLiveAndStaleAgreeOnFieldSet (this file) compares the live producer,
//     route.ToCorridorJSON, against the stale producer, staleJSON.
//   - TestLadderJSONMatchesToCorridorJSON (cmd/ladder/json_test.go) pins that
//     cmd/ladder's -json output is byte-identical to route.ToCorridorJSON's
//     own encoding for the same result, which is what makes it safe to treat
//     "cmd/ladder agrees" as already covered by the first test rather than
//     needing its own independent field-set walk.
//
// Together they are the comparison the issue asks for. If a reviewer wants a
// literal single test — e.g. by shelling out to `go run ./cmd/ladder -json`
// against a mocked upstream — say so in the issue and this can be replaced;
// that was judged more machinery than the field-set claim needs.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/checks"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
	"github.com/Wayfare-labs/wayfare/runstore"
)

// wellPopulatedLadderResult builds a LadderResult with every optional field
// set, so the JSON it produces exercises every key the wire shape can emit —
// a sparse fixture would let a dropped field hide behind omitempty.
func wellPopulatedLadderResult() *route.LadderResult {
	send, recv := asset.USDC(), asset.NGNC()
	quotedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

	recommended := route.Quote{
		Kind:            route.KindDEX,
		Description:     "USDC -> XLM -> NGNC",
		Source:          "stellar-dex",
		SendAsset:       send,
		SendAmount:      decimal.RequireFromString("100"),
		ReceiveAsset:    recv,
		ReceiveAmount:   decimal.RequireFromString("129000"),
		EffectiveRate:   decimal.RequireFromString("1290"),
		ReferenceMid:    decimal.RequireFromString("1350.2568"),
		ReferenceSource: "exchangerate-api",
		LossPct:         decimal.RequireFromString("4.46"),
		LossAmount:      decimal.RequireFromString("6025.68"),
		Verdict:         route.VerdictFair,
		Warnings:        []string{"derivative corridor: depends on NGNC"},
		QuotedAt:        quotedAt,
	}

	return &route.LadderResult{
		Request: route.LadderRequest{
			SendAsset:      send,
			ReceiveAsset:   recv,
			ReferenceBase:  "USD",
			ReferenceQuote: "NGN",
		},
		Rungs: []route.Rung{
			{
				SendAmount: decimal.RequireFromString("100"),
				Result: &route.Result{
					Quotes:    []route.Quote{recommended},
					Integrity: route.IntegrityDirect,
				},
			},
			{
				SendAmount: decimal.RequireFromString("5000"),
				Err:        errTransport,
			},
		},
		Integrity: route.IntegrityDirect,
		DependsOn: nil,

		ReferenceMid:    decimal.RequireFromString("1350.2568"),
		ReferenceSource: "exchangerate-api",
		Reference: refrate.Rate{
			Base:            "USD",
			Quote:           "NGN",
			Mid:             decimal.RequireFromString("1350.2568"),
			AsOf:            quotedAt,
			Source:          "exchangerate-api",
			FetchedAt:       quotedAt,
			SecondaryMid:    decimal.RequireFromString("1348.9000"),
			SecondarySource: "currency-api",
			DivergencePct:   decimal.RequireFromString("0.0931"),
			Agreement:       refrate.AgreementAgree,
			Note:            "both providers agree within tolerance",
		},
		Parallel: &refrate.Parallel{
			Status: refrate.ParallelReported,
			Mid:    decimal.RequireFromString("1620.00"),
			Source: "parallel-desk",
			GapPct: decimal.RequireFromString("19.98"),
		},

		Floor:     decimal.RequireFromString("4.46"),
		FloorSize: decimal.RequireFromString("100"),
		WorstLoss: decimal.RequireFromString("4.46"),
		WorstSize: decimal.RequireFromString("100"),

		Recommended:     &recommended,
		RecommendedSize: decimal.RequireFromString("100"),

		Finding: "USDC to NGNC prices directly; best observed loss 4.46% at 100 USDC.",
	}
}

type transportError struct{ msg string }

func (e *transportError) Error() string { return e.msg }

var errTransport = &transportError{msg: "context deadline exceeded"}

// fieldSet marshals v and returns its top-level JSON keys.
func fieldSet(t *testing.T, v any) map[string]bool {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("unmarshaling %T into a map: %v", v, err)
	}
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// liveOnlyFields are keys the live producer, route.ToCorridorJSON, can emit
// that the stale producer, staleJSON, structurally cannot — because
// runstore.Record has nowhere to carry the underlying data.
//
// This is a known, named gap in the run-record layout, not an oversight: see
// CONTRIBUTING.md and runstore.Record's own field-addition rule. Widening
// Record to carry the parallel block is a schema migration with its own
// review bar — exactly the kind of change the issue this test covers says to
// stop and flag rather than fold in here.
//
// reference_fetched_at was on this list until Version 3 gave the record a
// home for it (Reference.FetchedAt); it now round-trips like every other
// field and is asserted in TestLiveAndStaleAgreeOnReferenceCrossCheckValues.
var liveOnlyFields = map[string]bool{
	// runstore.Reference has no field for the cross-check's prose note
	// either. Flag: add a Note field alongside DivergencePct, behind a
	// Record Version bump.
	"reference_note": true,

	// runstore.Record carries no parallel/street-market block at all.
	// Flag: give Record a Parallel field, behind a Version bump, before a
	// stale replay can report this dimension.
	"parallel_status":  true,
	"parallel_mid":     true,
	"parallel_source":  true,
	"parallel_gap_pct": true,

	// parallel_reason is itself conditional even on the live side — it is
	// only non-empty when Parallel.Status is unavailable, so a fixture with
	// a reported parallel mid never emits it. It belongs on this list on
	// the same structural grounds as the rest of the parallel block, but is
	// exempted below from the "must appear in the live fixture" check.
	"parallel_reason": true,
}

// liveOptionalFields are liveOnlyFields that are not guaranteed to appear in
// any single live fixture, so the self-check below does not require them to.
var liveOptionalFields = map[string]bool{
	"parallel_reason": true,
}

// staleOnlyFields are keys only the stale producer emits, by design: Stale
// is present exactly when Live is false, and ToCorridorJSON never produces a
// false Live.
var staleOnlyFields = map[string]bool{
	"stale": true,
}

// TestLiveAndStaleAgreeOnFieldSet is the test named in the issue: it builds
// one measurement, renders it through both route.ToCorridorJSON and the
// round trip a real corridor takes through storage (runstore.FromCorridorJSON
// then staleJSON), and asserts the two documents agree on their field set
// once the documented, structural exceptions above are accounted for.
//
// Mutate either producer's field list — rename a JSON tag, drop a field,
// add one to only one side — and this test fails; that is the whole point,
// see the issue's "each test can actually fail" requirement.
func TestLiveAndStaleAgreeOnFieldSet(t *testing.T) {
	lr := wellPopulatedLadderResult()
	pair := "USD/NGN"

	live := route.ToCorridorJSON(lr, pair)
	rec := runstore.FromCorridorJSON(live)
	stale := staleJSON(rec, pair, time.Now().UTC().Add(2*time.Hour))

	liveFields := fieldSet(t, live)
	staleFields := fieldSet(t, stale)

	for k := range liveFields {
		if staleFields[k] {
			continue
		}
		if liveOnlyFields[k] {
			continue
		}
		t.Errorf("field %q is emitted by the live producer (ToCorridorJSON) but not by "+
			"the stale producer (staleJSON), and is not in the documented liveOnlyFields "+
			"exception list; either restore it on the stale path or document why it cannot "+
			"round-trip", k)
	}

	for k := range staleFields {
		if liveFields[k] {
			continue
		}
		if staleOnlyFields[k] {
			continue
		}
		t.Errorf("field %q is emitted by the stale producer (staleJSON) but not by "+
			"the live producer (ToCorridorJSON), and is not in the documented "+
			"staleOnlyFields exception list — a second, independently-maintained shape "+
			"is exactly the drift this wire contract exists to catch", k)
	}

	// The inverse of each allowance: a field named in one exception list
	// must actually be absent on the side it is excused from, or the list
	// itself has gone stale and is hiding a real mismatch.
	for k := range liveOnlyFields {
		if !liveFields[k] && !liveOptionalFields[k] {
			t.Errorf("liveOnlyFields names %q, but the live producer's fixture does not "+
				"even emit it — the exception is untested and may no longer be true", k)
		}
		if staleFields[k] {
			t.Errorf("liveOnlyFields names %q as stale-incapable, but staleJSON emitted it "+
				"anyway; the gap has been closed and the exception should be removed", k)
		}
	}
	for k := range staleOnlyFields {
		if !staleFields[k] {
			t.Errorf("staleOnlyFields names %q, but the stale fixture does not emit it", k)
		}
		if liveFields[k] {
			t.Errorf("staleOnlyFields names %q as stale-only, but the live producer "+
				"emitted it too", k)
		}
	}
}

// TestStaleServesStoredFindings covers the reason issue #93 exists: findings
// taken with a measurement must survive into storage and come back on the
// stale path, so a history-served corridor shows the same counterparty facts
// the live one did.
func TestStaleServesStoredFindings(t *testing.T) {
	lr := wellPopulatedLadderResult()
	pair := "USD/NGN"

	live := route.ToCorridorJSON(lr, pair)
	// Attach findings the way a live measurement with a checks runner does.
	live = route.WithFindings(live, findingsFixture())
	if live.Findings == nil {
		t.Fatal("test setup: WithFindings dropped the findings block")
	}

	rec := runstore.FromCorridorJSON(live)
	if len(rec.Checks) == 0 && len(rec.Metrics) == 0 {
		t.Fatal("FromCorridorJSON did not carry the findings into the record")
	}

	stale := staleJSON(rec, pair, time.Now().UTC())

	if stale.Findings == nil {
		t.Fatal("staleJSON did not serve the stored findings")
	}
	if len(stale.Findings.Checks) != len(live.Findings.Checks) {
		t.Errorf("stale findings checks = %d, want %d",
			len(stale.Findings.Checks), len(live.Findings.Checks))
	}
	if len(stale.Findings.Metrics) != len(live.Findings.Metrics) {
		t.Errorf("stale findings metrics = %d, want %d",
			len(stale.Findings.Metrics), len(live.Findings.Metrics))
	}

	// Tri-state and reasons must survive word-for-word, including the
	// undetermined case — an unknown result read as absent or as failed
	// would recreate the collapse this contract exists to prevent.
	for i := range live.Findings.Checks {
		w, g := live.Findings.Checks[i], stale.Findings.Checks[i]
		if w.Determined != g.Determined || w.Reason != g.Reason || w.Summary != g.Summary {
			t.Errorf("stale check %d differs: live (determined=%v reason=%q summary=%q), "+
				"stale (determined=%v reason=%q summary=%q)",
				i, w.Determined, w.Reason, w.Summary, g.Determined, g.Reason, g.Summary)
		}
	}
	for i := range live.Findings.Metrics {
		w, g := live.Findings.Metrics[i], stale.Findings.Metrics[i]
		if w.Determined != g.Determined || w.Value != g.Value || w.Unit != g.Unit {
			t.Errorf("stale metric %d differs: live (determined=%v value=%q unit=%q), "+
				"stale (determined=%v value=%q unit=%q)",
				i, w.Determined, w.Value, w.Unit, g.Determined, g.Value, g.Unit)
		}
	}

	// The summary counts and worst severity derived on the stale path must
	// match the live block's, so a client renders the same summary.
	if stale.Findings.Passed != live.Findings.Passed ||
		stale.Findings.Failed != live.Findings.Failed ||
		stale.Findings.Undetermined != live.Findings.Undetermined {
		t.Errorf("stale counts (p/f/u)=%d/%d/%d, live = %d/%d/%d",
			stale.Findings.Passed, stale.Findings.Failed, stale.Findings.Undetermined,
			live.Findings.Passed, live.Findings.Failed, live.Findings.Undetermined)
	}
	if stale.Findings.WorstSeverity != live.Findings.WorstSeverity {
		t.Errorf("stale worst severity = %q, live = %q",
			stale.Findings.WorstSeverity, live.Findings.WorstSeverity)
	}
}

// findingsFixture returns a findings set spanning all three check states and
// a determined + undetermined metric, so the round trip exercises every shape
// the wire can carry.
func findingsFixture() *checks.Findings {
	f := &checks.Findings{}
	f.Add(checkResult("anchor-asset-iso4217", checks.SeverityNotice, true, true,
		"anchor_asset names the shilling", "ngnc.online/.well-known/stellar.toml"))
	f.Add(checkResult("sep10.endpoint-responds", checks.SeverityWarning, false, true,
		"declared web_auth endpoint answered 200", "https://ngnc.online/.well-known/stellar.toml/web"))
	f.Add(checks.Undetermined(
		checks.Descriptor{ID: "home_domain.round-trip", Scope: checks.ScopeAnchor,
			Severity: checks.SeverityInfo, Title: "home_domain round-trips",
			CanDetermine: "to the same toml", CannotDetermine: "when the domain differs"},
		checks.Subject{Domain: "ngnc.online"},
		"no issuer-issued home_domain recorded"))

	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "spread.bid-ask", Scope: checks.ScopeAsset,
			At: time.Now().UTC(), Determined: true,
			Evidence: []checks.Evidence{{Source: "https://horizon.stellar.org/order_book", Observed: "0.0004"}},
		},
		Value: decimal.RequireFromString("0.0004"), Unit: checks.UnitRatio,
		Summary: "bid-ask spread on the USDC/NGNC book",
	})
	f.AddMetric(checks.MetricResult{
		Observation: checks.Observation{
			ID: "depth.observed-executable", Scope: checks.ScopeAsset,
			At: time.Now().UTC(), Determined: false, Reason: "no executable side at 5000 USDC",
		},
		Unit: checks.UnitAmount, Summary: "could not determine: no executable side at 5000 USDC",
	})
	return f
}

func checkResult(id string, sev checks.Severity, passed, determined bool, summary, source string) checks.CheckResult {
	r := checks.CheckResult{
		Observation: checks.Observation{
			ID: id, Scope: checks.ScopeAnchor, Subject: "ngnc.online",
			At: time.Now().UTC(), Determined: determined,
			Evidence: []checks.Evidence{{Source: source, Observed: "observed"}},
		},
		Passed: passed, Severity: sev, Summary: summary,
	}
	if !determined {
		r.Reason = "could not determine from available data"
	}
	return r
}

// TestLiveAndStaleAgreeOnReferenceCrossCheckValues goes one step further than
// the field set: for the fields that do round-trip, the values must survive
// too. A field present on both sides with a silently different value is a
// worse failure mode than a missing key, because nothing about the shape
// reveals it.
func TestLiveAndStaleAgreeOnReferenceCrossCheckValues(t *testing.T) {
	lr := wellPopulatedLadderResult()
	pair := "USD/NGN"

	live := route.ToCorridorJSON(lr, pair)
	rec := runstore.FromCorridorJSON(live)
	stale := staleJSON(rec, pair, time.Now().UTC())

	if stale.ReferenceSecondaryMid != live.ReferenceSecondaryMid {
		t.Errorf("stale ReferenceSecondaryMid = %q, want %q (the live value)",
			stale.ReferenceSecondaryMid, live.ReferenceSecondaryMid)
	}
	if stale.ReferenceSecondarySource != live.ReferenceSecondarySource {
		t.Errorf("stale ReferenceSecondarySource = %q, want %q",
			stale.ReferenceSecondarySource, live.ReferenceSecondarySource)
	}
	if stale.ReferenceDivergencePct != live.ReferenceDivergencePct {
		t.Errorf("stale ReferenceDivergencePct = %q, want %q",
			stale.ReferenceDivergencePct, live.ReferenceDivergencePct)
	}
	if stale.ReferenceFetchedAt != live.ReferenceFetchedAt {
		t.Errorf("stale ReferenceFetchedAt = %q, want %q (the live value, carried through storage)",
			stale.ReferenceFetchedAt, live.ReferenceFetchedAt)
	}
}
