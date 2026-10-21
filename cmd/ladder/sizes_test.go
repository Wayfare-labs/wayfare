package main

import (
	"testing"

	"github.com/Wayfare-labs/wayfare/route"
)

// TestDefaultSizesFlagMatchesSharedLadder pins the -sizes default to
// route.DefaultSizes (issue #758).
//
// The CLI used to repeat the ladder as a literal. If the shared definition
// changed, the CLI would keep measuring the old sizes while the server and the
// depth metric measured the new ones, and the difference would only show up as
// an unexplained change in the numbers. The flag default is now derived, so
// this test can compare what the flag accepts against the shared ladder
// directly.
func TestDefaultSizesFlagMatchesSharedLadder(t *testing.T) {
	raw := defaultSizesFlag()

	sizes, err := parseSizes(raw)
	if err != nil {
		t.Fatalf("parseSizes(defaultSizesFlag()): %v", err)
	}
	if len(sizes) != len(route.DefaultSizes) {
		t.Fatalf("the -sizes default has %d rungs, route.DefaultSizes has %d; "+
			"the CLI and the server would measure different ladders",
			len(sizes), len(route.DefaultSizes))
	}
	for i := range route.DefaultSizes {
		if !sizes[i].Equal(route.DefaultSizes[i]) {
			t.Errorf("default rung %d = %s, route.DefaultSizes[%d] = %s",
				i, sizes[i], i, route.DefaultSizes[i])
		}
	}
}
