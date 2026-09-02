package route

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
)

// TestToQuoteJSONCarriesKind pins that ToQuoteJSON never drops Kind. The
// field existed on Quote before this test was written but was silently
// dropped at the wire boundary, which is exactly the conflation issue #181
// exists to fix: a client had no way to tell an on-chain DEX quote from an
// anchor's own SEP-38 rate once either reached JSON.
func TestToQuoteJSONCarriesKind(t *testing.T) {
	for _, kind := range []Kind{KindDEX, KindAnchorSEP38} {
		t.Run(string(kind), func(t *testing.T) {
			q := &Quote{
				Kind:          kind,
				Description:   "USDC -> NGNC",
				Source:        "stellar-dex",
				ReceiveAmount: decimal.RequireFromString("100"),
				EffectiveRate: decimal.RequireFromString("1000"),
				LossPct:       decimal.RequireFromString("1.5"),
				Verdict:       VerdictGood,
			}
			got := ToQuoteJSON(q)
			if got.Kind != string(kind) {
				t.Errorf("Kind = %q, want %q", got.Kind, kind)
			}
		})
	}
}

// TestToQuoteJSONNilIsNil guards the existing nil-safety of ToQuoteJSON,
// which the new field must not disturb.
func TestToQuoteJSONNilIsNil(t *testing.T) {
	if got := ToQuoteJSON(nil); got != nil {
		t.Errorf("ToQuoteJSON(nil) = %+v, want nil", got)
	}
}

// TestToCorridorJSONPropagatesQuoteKind builds a minimal LadderResult with a
// DEX-priced rung and asserts the kind survives all the way to both the
// per-rung quote and the recommended quote on the rendered CorridorJSON —
// the actual shape a client reads over HTTP.
func TestToCorridorJSONPropagatesQuoteKind(t *testing.T) {
	send, recv := asset.USDC(), asset.NGNC()
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     send,
		SendAmount:    decimal.RequireFromString("100"),
		ReceiveAsset:  recv,
		ReceiveAmount: decimal.RequireFromString("129000"),
		EffectiveRate: decimal.RequireFromString("1290"),
		LossPct:       decimal.RequireFromString("4.46"),
		Verdict:       VerdictFair,
		QuotedAt:      time.Now(),
	}

	lr := &LadderResult{
		Request: LadderRequest{SendAsset: send, ReceiveAsset: recv, ReferenceBase: "USD", ReferenceQuote: "NGN"},
		Rungs: []Rung{{
			SendAmount: decimal.RequireFromString("100"),
			Result:     &Result{Quotes: []Quote{q}, Integrity: IntegrityDirect},
		}},
		Integrity:       IntegrityDirect,
		ReferenceMid:    decimal.RequireFromString("1350"),
		ReferenceSource: "exchangerate-api",
		Recommended:     &q,
		RecommendedSize: decimal.RequireFromString("100"),
	}

	out := ToCorridorJSON(lr, "USD/NGN")

	if out.Rungs[0].Quote == nil {
		t.Fatal("expected the rung to carry a quote")
	}
	if out.Rungs[0].Quote.Kind != string(KindDEX) {
		t.Errorf("rung quote kind = %q, want %q", out.Rungs[0].Quote.Kind, KindDEX)
	}
	if out.Recommended == nil {
		t.Fatal("expected a recommended quote")
	}
	if out.Recommended.Kind != string(KindDEX) {
		t.Errorf("recommended kind = %q, want %q", out.Recommended.Kind, KindDEX)
	}

	// And on the actual wire bytes, not just the Go struct — a field present
	// on the struct but dropped by a stray json tag would pass every check
	// above and still ship broken.
	raw, err := json.Marshal(out.Recommended)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if wire["kind"] != "dex" {
		t.Errorf(`wire "kind" = %v, want "dex"`, wire["kind"])
	}
}

// TestToAssetJSONPublishesSEP38WireForm pins GitHub issue #178: the README's
// "Asset identity" section specifies the wire form as stellar:CODE:ISSUER,
// stellar:native, or iso4217:CODE, and AssetJSON must carry it — not just the
// separate code and issuer fields the shape already had.
//
// Each case round-trips through asset.ParseSEP38 back to an equal asset.Asset,
// which is what makes this test able to fail: a wrong or dropped Asset field
// fails to parse, or parses back to a different asset than the one that went
// in.
func TestToAssetJSONPublishesSEP38WireForm(t *testing.T) {
	cases := []struct {
		name string
		a    asset.Asset
		want string
	}{
		{"issued Stellar asset", asset.USDC(), "stellar:USDC:" + asset.USDCIssuer},
		{"native XLM", asset.Native(), "stellar:native"},
		{"fiat", asset.Fiat("NGN"), "iso4217:NGN"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := ToAssetJSON(tc.a)

			if j.Asset != tc.want {
				t.Fatalf("Asset = %q, want %q", j.Asset, tc.want)
			}
			if j.Code != tc.a.Code {
				t.Errorf("Code = %q, want %q — the separate field must survive alongside the new one",
					j.Code, tc.a.Code)
			}
			if j.Issuer != tc.a.Issuer {
				t.Errorf("Issuer = %q, want %q", j.Issuer, tc.a.Issuer)
			}

			back, err := asset.ParseSEP38(j.Asset)
			if err != nil {
				t.Fatalf("asset.ParseSEP38(%q) failed: %v", j.Asset, err)
			}
			if !back.Equal(tc.a) {
				t.Errorf("round trip produced %+v, want %+v", back, tc.a)
			}
		})
	}
}

// TestAssetJSONMarshalsTheWireFormField covers the actual bytes on the wire,
// not just the Go struct: a client reading raw JSON must see an "asset" key
// carrying the SEP-38 string.
func TestAssetJSONMarshalsTheWireFormField(t *testing.T) {
	j := ToAssetJSON(asset.NGNC())

	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(buf, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got, ok := m["asset"]
	if !ok {
		t.Fatal(`marshaled AssetJSON has no "asset" key`)
	}
	want := "stellar:NGNC:" + asset.LinkIOIssuer
	if got != want {
		t.Errorf(`"asset" = %v, want %q`, got, want)
	}
	if !strings.Contains(string(buf), `"code":"NGNC"`) {
		t.Errorf("marshaled output %s no longer carries the separate code field", buf)
	}
}

// TestAssetJSONBareCodeOmitsTheWireForm covers a producer that has only a
// bare code to work from — e.g. a stale-path fallback for an asset the
// verified registry does not recognise — where Kind and Issuer are not known.
// The wire form must be absent rather than a guess built from a bare code,
// per the project's rule that an unavailable identity is never synthesised.
// This constructs AssetJSON directly, the shape such a fallback actually
// builds (see server/api.go's route.AssetJSON{Code: code} call sites) —
// there is no asset.Asset to convert in that case, which is exactly the
// situation this test exists to cover.
func TestAssetJSONBareCodeOmitsTheWireForm(t *testing.T) {
	j := AssetJSON{Code: "UNKNOWN"}

	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(buf), `"asset"`) {
		t.Errorf("marshaled output %s carries an \"asset\" key with no verified identity to back it", buf)
	}
}

// TestToAssetJSONOmitsWireFormForIncompleteAsset covers the case
// TestAssetJSONBareCodeOmitsTheWireForm does not: an asset.Asset that exists
// but is not Identifiable, passed through ToAssetJSON itself. Before this
// fix, asset.Asset.SEP38() was called unconditionally, so an issued Stellar
// asset with no issuer produced "stellar:CODE:" — a string that looks like a
// complete wire form but names no issuer at all — and a zero-value
// asset.Asset produced "stellar::". Both are exactly the kind of guessed
// identity the project's "never synthesised" rule refuses.
func TestToAssetJSONOmitsWireFormForIncompleteAsset(t *testing.T) {
	cases := []struct {
		name string
		a    asset.Asset
	}{
		{"issued Stellar asset with no issuer", asset.Stellar("USDC", "")},
		{"zero-value asset", asset.Asset{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			j := ToAssetJSON(tc.a)

			if j.Asset != "" {
				t.Errorf("Asset = %q, want empty — %+v has no issuer to identify it", j.Asset, tc.a)
			}
			if j.Code != tc.a.Code {
				t.Errorf("Code = %q, want %q — Code must still survive", j.Code, tc.a.Code)
			}

			buf, err := json.Marshal(j)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if strings.Contains(string(buf), `"asset"`) {
				t.Errorf("marshaled output %s carries an \"asset\" key for an unidentifiable asset", buf)
			}
		})
	}
}
