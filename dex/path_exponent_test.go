package dex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestPathRecordExponentGuards kills mutation M16 of the pricing-path
// catalogue (docs/qa/artifacts/667-mutation-catalogue.md, #667).
//
// parsePathRecords refuses an amount whose exponent no Stellar asset can carry
// before any arithmetic touches it. The order-book decoder's equivalent guard
// was tested (#589); these were not, so deleting the destination-side guard
// survived the whole suite. A positive exponent is the case that matters: it
// passes the decimal-places refusal (which only rejects negative exponents
// beyond seven places) and would otherwise be priced as a real, astronomically
// large receive amount.
func TestPathRecordExponentGuards(t *testing.T) {
	src, dst := asset.USDC(), asset.NGNC()
	record := func(sourceAmount, destAmount string) json.RawMessage {
		return json.RawMessage(`{
			"source_asset_type": "credit_alphanum4", "source_asset_code": "USDC",
			"source_amount": "` + sourceAmount + `",
			"destination_asset_type": "credit_alphanum4", "destination_asset_code": "NGNC",
			"destination_amount": "` + destAmount + `",
			"path": []
		}`)
	}

	// 1e40 is inside the window the guard exists for: shopspring/decimal
	// parses it (its own limit is an exponent of 10000, and an input past
	// that fails at parse time with a different error), but it is beyond
	// maxDecimalExponent. An input past the library's limit would be refused
	// by the parse and would not exercise the guard at all — the first
	// version of this test used one and let M16 survive.
	const big = "1e40"

	cases := []struct {
		name, direction, source, dest, field string
	}{
		{"strict-send destination", "strict-send", "100.0000000", big, "destination_amount"},
		{"strict-send source", "strict-send", big, "100.0000000", "source_amount"},
		{"strict-receive destination", "strict-receive", "100.0000000", big, "destination_amount"},
		{"strict-receive source", "strict-receive", big, "100.0000000", "source_amount"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := parsePathRecords(tc.direction, []json.RawMessage{record(tc.source, tc.dest)}, src, dst)
			if err == nil {
				t.Fatalf("accepted a %s of %q as %d path(s); an exponent beyond ±%d is a corrupted response",
					tc.field, big, len(paths), maxDecimalExponent)
			}
			if !strings.Contains(err.Error(), tc.field) || !strings.Contains(err.Error(), "a Stellar asset cannot carry it") {
				t.Errorf("refused for the wrong reason: %v; want the %s exponent guard", err, tc.field)
			}
		})
	}

	// The guard is a bound, not a ban on exponents: an ordinary amount
	// written with one still prices.
	if _, err := parsePathRecords("strict-send", []json.RawMessage{record("100", "6.51e4")}, src, dst); err != nil {
		t.Errorf("refused an in-range exponent: %v", err)
	}
}
