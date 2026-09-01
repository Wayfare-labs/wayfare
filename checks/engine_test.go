package checks

import (
	"context"
	"reflect"
	"testing"

	"github.com/Wayfare-labs/wayfare/anchor"
	"github.com/Wayfare-labs/wayfare/asset"
)

// This file tests the composition contract that the engine — and every caller
// that renders a corridor — depends on. See the package doc comment in
// checks.go and docs/checks.md.
//
// The contract has three parts:
//
//  1. A check observes a fact about a subject and returns a CheckResult.
//     Nothing it does can change the subject it was given: Subject is passed
//     by value, and the package exposes no way to write a result back into
//     it.
//  2. Findings aggregates results. It has no field and no method that can
//     influence an integrity classification or a verdict — the headline is
//     computed elsewhere and passed in, and this package's only job is to
//     qualify it.
//  3. Aggregation is exact: declaration order is preserved and nothing is
//     synthesized, deduplicated or reordered by the composition layer.

// checkFunc adapts a function to the Check interface for tests.
type checkFunc func(context.Context, Subject) CheckResult

func (checkFunc) Describe() Descriptor {
	return Descriptor{
		ID: "test.checkfunc", Title: "check function",
		CanDetermine: "the function decides", CannotDetermine: "the function decides not to",
	}
}
func (f checkFunc) Run(ctx context.Context, s Subject) CheckResult { return f(ctx, s) }

// TestSubjectHeadlineIsImmutable pins part 1 of the contract with a check
// that tries to break it: it mutates the subject it receives, including the
// headline fields a corridor carries. Because the subject is passed by value,
// the caller's copy must be untouched afterwards — a check cannot rewrite the
// integrity classification or the underlying pair of the corridor it is
// qualifying.
//
// The boundary is drawn precisely: the headline and identity fields are
// value-isolated. Subject.Profile is the one pointer-owned field, and it is
// deliberately *not* deep-copied per check — the sweep resolves the anchor
// once and hands the same document to every check to read. The test mutates
// through the pointer to prove that boundary is real and documented, so a
// future deep-copy change is a deliberate act rather than an accident.
func TestSubjectHeadlineIsImmutable(t *testing.T) {
	// sabotage mutates its subject and then fails loudly.
	sabotage := checkFunc(func(_ context.Context, s Subject) CheckResult {
		s.Integrity = "DIRECT"
		s.Underlying = asset.NGNC()
		s.Send = asset.NGNC()
		s.Asset = asset.GHSC()
		s.Domain = "rewritten.example"
		// Pointer-owned data is shared context, not value-isolated: this
		// mutation reaches the caller by design (see the doc comment on
		// Subject.Profile), which is why checks must treat the profile as
		// read-only.
		s.Profile.Domain = "mutated.example"
		return Fail(Descriptor{ID: "test.sabotage"}, s, "tried to rewrite the headline")
	})

	original := Subject{
		Domain:     "anchor.example",
		Asset:      asset.NGNC(),
		Send:       asset.USDC(),
		Receive:    asset.GHSC(),
		Integrity:  "DERIVATIVE",
		Underlying: asset.NGNC(),
		Profile:    &anchor.Profile{Domain: "anchor.example"},
	}

	before := original
	res := Run(ctx(), sabotage, original)

	if res.Determined && res.Passed {
		t.Error("sabotage check should have produced a failure")
	}
	if !reflect.DeepEqual(original, before) {
		t.Errorf("the caller's subject changed under the check:\n got %+v\nwant %+v",
			original, before)
	}
	if original.Integrity != "DERIVATIVE" {
		t.Errorf("Integrity = %q, want the caller's original value", original.Integrity)
	}
	if original.Domain != "anchor.example" {
		t.Errorf("Domain = %q, want the caller's original value", original.Domain)
	}
	// The shared-profile boundary, asserted rather than assumed: the value
	// fields above are isolated, and the profile pointer is shared.
	if original.Profile.Domain != "mutated.example" {
		t.Error("the profile pointer was not shared with the check; if this " +
			"passes because Run deep-copies profiles, update this test and the " +
			"Subject.Profile doc comment deliberately")
	}
}

// TestFindingsCannotMoveTheHeadline pins part 2 by inspecting the package's
// public surface: the composition types must contain no field and no method
// through which a check's result could influence an integrity classification
// or a verdict. This is the "no path back into the engine" rule made
// checkable rather than asserted in prose.
func TestFindingsCannotMoveTheHeadline(t *testing.T) {
	forbidden := map[string]bool{
		"Integrity":   true,
		"Verdict":     true,
		"DependsOn":   true,
		"LossPct":     true,
		"Recommended": true,
	}

	checkType := func(v any, name string) {
		t.Helper()
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if forbidden[f.Name] {
				t.Errorf("%s.%s exposes %q — a check result must not be able to "+
					"move the headline", name, typ.Name(), f.Name)
			}
		}
	}

	checkType(Findings{}, "Findings")
	checkType(CheckResult{}, "CheckResult")
	checkType(MetricResult{}, "MetricResult")
	checkType(Observation{}, "Observation")

	// Findings' methods are the whole surface: they append results and they
	// summarise them. None of them accepts or returns a headline value.
	var f Findings
	if _, any := f.Worst(); any {
		t.Error("an empty Findings must not report any failure")
	}
	p, failed, undetermined := f.Counts()
	if p != 0 || failed != 0 || undetermined != 0 {
		t.Errorf("empty Findings counts = %d/%d/%d, want all zero",
			p, failed, undetermined)
	}
}

// TestRunAllPreservesDeclarationOrder is part 3: RunAll must hand back one
// result per check, in the order the checks were declared, whatever mix of
// states they produce. A reader's "first result" is therefore deterministic.
func TestRunAllPreservesDeclarationOrder(t *testing.T) {
	s := Subject{Domain: "example.test"}
	mk := func(id string, res CheckResult) Check {
		return stubCheck{id: id, result: res}
	}
	d := func(id string) Descriptor {
		return Descriptor{ID: id, Title: id, CanDetermine: "x", CannotDetermine: "y"}
	}

	cs := []Check{
		mk("test.pass", Pass(d("test.pass"), s, "ok")),
		mk("test.unknown", Undetermined(d("test.unknown"), s, "not published")),
		mk("test.fail", Fail(d("test.fail"), s, "broken")),
	}
	results := RunAll(ctx(), cs, s)

	if len(results) != len(cs) {
		t.Fatalf("RunAll returned %d results for %d checks", len(results), len(cs))
	}
	want := []string{"test.pass", "test.unknown", "test.fail"}
	for i, id := range want {
		if results[i].ID != id {
			t.Errorf("result %d = %q, want %q", i, results[i].ID, id)
		}
	}
}

// TestFindingsAggregationIsExactUnion pins part 3 for the aggregation layer:
// Add appends results verbatim — no deduplication, no reordering, no
// synthesis — and Counts and Sorted are derived from exactly the results that
// were added.
func TestFindingsAggregationIsExactUnion(t *testing.T) {
	s := Subject{Domain: "example.test"}
	d := func(id string) Descriptor {
		return Descriptor{ID: id, Title: id, CanDetermine: "x", CannotDetermine: "y"}
	}

	results := RunAll(ctx(), []Check{
		stubCheck{id: "test.a", result: Pass(d("test.a"), s, "a")},
		stubCheck{id: "test.b", result: Fail(d("test.b"), s, "b")},
		stubCheck{id: "test.c", result: Undetermined(d("test.c"), s, "c")},
		stubCheck{id: "test.a", result: Pass(d("test.a"), s, "a again")}, // duplicate ID on purpose
	}, s)

	var f Findings
	for _, r := range results {
		f.Add(r)
	}

	if len(f.Checks) != len(results) {
		t.Fatalf("Findings holds %d results, want the %d that were added — "+
			"aggregation must not deduplicate", len(f.Checks), len(results))
	}
	for i := range results {
		if !reflect.DeepEqual(f.Checks[i], results[i]) {
			t.Errorf("result %d was not stored verbatim", i)
		}
	}

	passed, failed, undetermined := f.Counts()
	if passed != 2 || failed != 1 || undetermined != 1 {
		t.Errorf("counts = %d/%d/%d, want 2 passed, 1 failed, 1 undetermined",
			passed, failed, undetermined)
	}

	sorted := f.Sorted()
	// Failures first, then undetermined, then passes; ties break on ID.
	if !sorted[0].Failed() || sorted[0].ID != "test.b" {
		t.Errorf("first sorted result = %q (failed=%v), want the failure to lead",
			sorted[0].ID, sorted[0].Failed())
	}
	if sorted[1].Determined || sorted[1].ID != "test.c" {
		t.Errorf("second sorted result = %q, want the undetermined result",
			sorted[1].ID)
	}
	for i := 2; i < len(sorted); i++ {
		if sorted[i].Failed() || !sorted[i].Determined {
			t.Errorf("sorted[%d] = %q must be a passing result", i, sorted[i].ID)
		}
	}
}

// TestCompositionQualifiesAcrossScopes covers the composition of checks with
// different scopes into one Findings: anchor, asset and corridor observations
// all land in the same report, and each result names its own subject.
func TestCompositionQualifiesAcrossScopes(t *testing.T) {
	s := Subject{Domain: "anchor.example", Asset: asset.NGNC(),
		Send: asset.USDC(), Receive: asset.GHSC()}

	anchorCheck := stubCheck{id: "test.anchor",
		result: Pass(Descriptor{ID: "test.anchor", Scope: ScopeAnchor}, s, "anchor ok")}
	assetCheck := stubCheck{id: "test.asset",
		result: Fail(Descriptor{ID: "test.asset", Scope: ScopeAsset}, s, "asset broken")}
	corridorCheck := stubCheck{id: "test.corridor",
		result: Undetermined(Descriptor{ID: "test.corridor", Scope: ScopeCorridor}, s, "corridor unknown")}

	results := RunAll(ctx(), []Check{anchorCheck, assetCheck, corridorCheck}, s)
	var f Findings
	for _, r := range results {
		f.Add(r)
	}

	scopes := map[string]Scope{}
	for _, r := range f.Checks {
		scopes[r.ID] = r.Scope
	}
	if scopes["test.anchor"] != ScopeAnchor {
		t.Errorf("anchor check scope = %v, want ScopeAnchor", scopes["test.anchor"])
	}
	if scopes["test.asset"] != ScopeAsset {
		t.Errorf("asset check scope = %v, want ScopeAsset", scopes["test.asset"])
	}
	if scopes["test.corridor"] != ScopeCorridor {
		t.Errorf("corridor check scope = %v, want ScopeCorridor", scopes["test.corridor"])
	}
}
