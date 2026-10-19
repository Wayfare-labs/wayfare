package server

import (
	"strings"
	"testing"
)

// These tests pin issue #290: the corridor selector is driven by
// GET /api/assets — the verified destination set, the settlement asset and the
// peg all come from the response — and this file names no asset code of its
// own. They are source-text assertions on the single embedded UI file, the
// approach the other UI contracts use: the page has no build step and no JS
// test runner in CI, so the script's source is what is assertable.
//
// They cannot prove the endpoint answers; TestAssetsEndpointShape pins that
// side of the contract (code, issuer, can_be_destination, USDC the only
// non-destination).

// between returns src from the first occurrence of start to the first
// occurrence of end after it, so an assertion about one function cannot be
// satisfied by text elsewhere in the page. Missing anchors are test failures,
// not silent empty strings: a renamed anchor should be noticed, not skipped.
func between(t *testing.T, src, start, end string) string {
	t.Helper()
	i := strings.Index(src, start)
	if i < 0 {
		t.Fatalf("UI source is missing anchor %q", start)
	}
	j := strings.Index(src[i:], end)
	if j < 0 {
		t.Fatalf("UI source is missing end anchor %q after %q", end, start)
	}
	return src[i : i+j]
}

// selectorRegion is the whole asset/selector block, from its banner comment
// to the error-presentation section that follows it.
func selectorRegion(t *testing.T) string {
	t.Helper()
	return between(t, uiSource(t),
		"// --- Asset / corridor selector",
		"// --- Error presentation")
}

// bootRegion is the page-load IIFE at the end of the script.
func bootRegion(t *testing.T) string {
	t.Helper()
	return between(t, uiSource(t), "(async function boot()", "</script>")
}

// TestUISelectorReadsTheEndpoint asserts every part of the option label is
// derived from /api/assets: the destination set from can_be_destination, the
// send asset from the entry that is not a destination, and the peg from the
// entry itself. A corridor the endpoint adds must appear without a UI change;
// one it drops must disappear with it.
func TestUISelectorReadsTheEndpoint(t *testing.T) {
	region := selectorRegion(t)

	for _, want := range []string{
		"fetch('/api/assets')",                 // the list comes from the endpoint
		"filter(a => a.can_be_destination)",    // only verified destinations
		"all.find(a => !a.can_be_destination)", // settlement read from the response
		"a.peg",                                // the peg in the label
		"`${settlement} \\u2192 ${dest}`",      // send side of the label
		"a.code.localeCompare",                 // order from the response, not the file
		"a.state.last_integrity",               // the endpoint's stored state, not ours
	} {
		if !strings.Contains(region, want) {
			t.Errorf("selector block missing %q; the label would not be driven by /api/assets", want)
		}
	}
}

// TestUISelectorNamesNoAsset pins the absence side: no corridor code is
// written into the selector path. A display-name map keyed by code looks
// harmless until the endpoint returns a fourth corridor that renders as a bare
// code — and until the map and the endpoint disagree about what exists.
func TestUISelectorNamesNoAsset(t *testing.T) {
	page := uiSource(t)
	if strings.Contains(page, "CORRIDOR_NAMES") {
		t.Error("UI still carries CORRIDOR_NAMES; display names must come from the response, not a hardcoded map")
	}

	region := selectorRegion(t)
	for _, code := range []string{"NGNC", "GHSC", "KESC", "USDC"} {
		for _, quote := range []string{"'", `"`} {
			lit := quote + code + quote
			if strings.Contains(region, lit) {
				t.Errorf("selector block contains the literal %s; the corridor set must come from /api/assets", lit)
			}
		}
	}
}

// TestUIBootHasNoHardcodedDefaultCorridor pins the last piece of the issue:
// boot() measured 'NGNC' whenever the asset list failed to load, so a broken
// endpoint silently produced a corridor the reader was never offered. The
// selector's own value — set by the endpoint, or empty — is the only thing
// boot() may act on.
func TestUIBootHasNoHardcodedDefaultCorridor(t *testing.T) {
	boot := bootRegion(t)

	if strings.Contains(boot, "'NGNC'") || strings.Contains(boot, `"NGNC"`) {
		t.Error("boot() falls back to a hardcoded corridor; a failed /api/assets call must leave its own state on screen")
	}
	for _, want := range []string{
		"await loadAssets()",          // the list first
		"const ready =",               // the outcome is checked, not ignored
		"if (!ready) return;",         // and a failure stops the boot, not into a default
		"loadCorridor($('to').value)", // the corridor fetched is the one selected
	} {
		if !strings.Contains(boot, want) {
			t.Errorf("boot() missing %q", want)
		}
	}
}

// TestUILoadAssetsReportsFailure asserts loadAssets() tells the caller which
// outcome it had, so "could not load" and "no corridors" and "loaded" are
// three distinguishable states rather than one silent default.
func TestUILoadAssetsReportsFailure(t *testing.T) {
	fn := between(t, uiSource(t), "async function loadAssets()", "function buildSelector(")
	if !strings.Contains(fn, "return false;") {
		t.Error("loadAssets() does not report a failed load; boot() cannot tell it from success")
	}
	if !strings.Contains(fn, "return $('to').value !== '';") {
		t.Error("loadAssets() does not report whether a corridor is selectable")
	}
	if !strings.Contains(fn, "Could not load corridors") {
		t.Error("loadAssets() no longer tells the reader the list could not be loaded")
	}
}
