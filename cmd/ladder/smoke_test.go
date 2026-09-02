package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/route"
)

// ---------------------------------------------------------------------------
// parseSizes
// ---------------------------------------------------------------------------

func TestParseSizesValid(t *testing.T) {
	sizes, err := parseSizes("1,5,10")
	if err != nil {
		t.Fatalf("parseSizes: %v", err)
	}
	if len(sizes) != 3 {
		t.Fatalf("got %d sizes, want 3", len(sizes))
	}
	for _, want := range []string{"1", "5", "10"} {
		found := false
		for _, s := range sizes {
			if s.String() == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected size %s not found in %v", want, sizes)
		}
	}
}

func TestParseSizesTrimsWhitespace(t *testing.T) {
	sizes, err := parseSizes(" 1 , 5 , 10 ")
	if err != nil {
		t.Fatalf("parseSizes: %v", err)
	}
	if len(sizes) != 3 {
		t.Fatalf("got %d sizes, want 3", len(sizes))
	}
}

func TestParseSizesSkipsBadEntries(t *testing.T) {
	sizes, err := parseSizes("1,abc,10")
	if err != nil {
		t.Fatalf("parseSizes: %v", err)
	}
	// abc should be skipped; 1 and 10 remain
	if len(sizes) != 2 {
		t.Fatalf("got %d sizes, want 2 (bad entry skipped)", len(sizes))
	}
}

func TestParseSizesAllBad(t *testing.T) {
	_, err := parseSizes("abc,def")
	if err == nil {
		t.Fatal("expected error for all-bad input, got nil")
	}
}

func TestParseSizesEmpty(t *testing.T) {
	_, err := parseSizes("")
	if err == nil {
		t.Fatal("expected error for empty input, got nil")
	}
}

func TestParseSizesDecimalValues(t *testing.T) {
	sizes, err := parseSizes("0.1,0.01")
	if err != nil {
		t.Fatalf("parseSizes: %v", err)
	}
	if len(sizes) != 2 {
		t.Fatalf("got %d sizes, want 2", len(sizes))
	}
	if sizes[0].String() != "0.1" {
		t.Errorf("first size = %s, want 0.1", sizes[0])
	}
}

// ---------------------------------------------------------------------------
// erroredRungs
// ---------------------------------------------------------------------------

func TestErroredRungsNone(t *testing.T) {
	lr := &route.LadderResult{
		Rungs: []route.Rung{
			{SendAmount: decimal.NewFromInt(100), Result: &route.Result{}},
			{SendAmount: decimal.NewFromInt(250)},
		},
	}
	got := erroredRungs(lr)
	if len(got) != 0 {
		t.Errorf("expected 0 errored rungs, got %d: %v", len(got), got)
	}
}

func TestErroredRungsSome(t *testing.T) {
	lr := &route.LadderResult{
		Rungs: []route.Rung{
			{SendAmount: decimal.NewFromInt(100), Err: fmt.Errorf("network error")},
			{SendAmount: decimal.NewFromInt(250), Result: &route.Result{}},
			{SendAmount: decimal.NewFromInt(500), Err: errors.New("timeout")},
		},
	}
	got := erroredRungs(lr)
	if len(got) != 2 {
		t.Fatalf("expected 2 errored rungs, got %d: %v", len(got), got)
	}
	if got[0] != "100" || got[1] != "500" {
		t.Errorf("errored rungs = %v, want [100 500]", got)
	}
}

func TestErroredRungsAllErrored(t *testing.T) {
	lr := &route.LadderResult{
		Rungs: []route.Rung{
			{SendAmount: decimal.NewFromInt(100), Err: fmt.Errorf("err1")},
			{SendAmount: decimal.NewFromInt(250), Err: fmt.Errorf("err2")},
		},
	}
	got := erroredRungs(lr)
	if len(got) != 2 {
		t.Errorf("expected 2 errored rungs, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// referenceProvider
// ---------------------------------------------------------------------------

func TestReferenceProviderExchangeRateAPI(t *testing.T) {
	ref, err := referenceProvider("exchangerate-api")
	if err != nil {
		t.Fatalf("referenceProvider: %v", err)
	}
	if ref.provider == nil {
		t.Fatal("provider is nil")
	}
	if ref.baseURL == "" {
		t.Error("baseURL is empty")
	}
	if ref.setClient == nil {
		t.Error("setClient is nil")
	}
}

func TestReferenceProviderCurrencyAPI(t *testing.T) {
	ref, err := referenceProvider("currency-api")
	if err != nil {
		t.Fatalf("referenceProvider: %v", err)
	}
	if ref.provider == nil {
		t.Fatal("provider is nil")
	}
	if ref.baseURL == "" {
		t.Error("baseURL is empty")
	}
}

func TestReferenceProviderUnknown(t *testing.T) {
	_, err := referenceProvider("bogus")
	if err == nil {
		t.Fatal("expected error for unknown provider, got nil")
	}
}

func TestReferenceProviderEmptyDefaults(t *testing.T) {
	// empty string should default to exchangerate-api
	ref, err := referenceProvider("")
	if err != nil {
		t.Fatalf("referenceProvider(\"\"): %v", err)
	}
	if ref.provider == nil {
		t.Fatal("provider is nil for empty default")
	}
}

// ---------------------------------------------------------------------------
// sizeStrings
// ---------------------------------------------------------------------------

func TestSizeStrings(t *testing.T) {
	input := []decimal.Decimal{
		decimal.NewFromInt(100),
		decimal.NewFromInt(250),
		decimal.NewFromInt(5000),
	}
	got := sizeStrings(input)
	if len(got) != 3 {
		t.Fatalf("got %d strings, want 3", len(got))
	}
	want := []string{"100", "250", "5000"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("sizeStrings[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestSizeStringsEmpty(t *testing.T) {
	got := sizeStrings(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSizeStringsDecimalValues(t *testing.T) {
	input := []decimal.Decimal{decimal.RequireFromString("0.1")}
	got := sizeStrings(input)
	if len(got) != 1 || got[0] != "0.1" {
		t.Errorf("got %v, want [\"0.1\"]", got)
	}
}

// ---------------------------------------------------------------------------
// gitRevision
// ---------------------------------------------------------------------------

func TestGitRevisionNonEmpty(t *testing.T) {
	rev := gitRevision()
	if rev == "" {
		// git may not be available in some CI environments; skip rather than fail
		t.Skip("gitRevision returned empty; git may not be available")
	}
	// A short SHA is 7+ hex chars
	if len(rev) < 7 {
		t.Errorf("gitRevision = %q, expected at least 7 chars", rev)
	}
}

// ---------------------------------------------------------------------------
// dirtyFiles
// ---------------------------------------------------------------------------

func TestDirtyFilesReturnsSlice(t *testing.T) {
	// dirtyFiles should not panic, regardless of whether the tree is clean
	files, err := dirtyFiles()
	if err != nil {
		// Not in a git repo is acceptable
		t.Skipf("dirtyFiles error (may not be in a git repo): %v", err)
	}
	// files may be nil (clean tree) or populated; both are valid
	_ = files
}

// ---------------------------------------------------------------------------
// requireCleanTree
// ---------------------------------------------------------------------------

func TestRequireCleanTreeAllowDirtyAlwaysReturnsNil(t *testing.T) {
	dirty, err := requireCleanTree(true)
	if err != nil {
		t.Fatalf("requireCleanTree(allowDirty=true): %v", err)
	}
	// dirty may be true or false depending on working tree state; no error expected
	_ = dirty
}
