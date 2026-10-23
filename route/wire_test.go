package route

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/refrate"
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
func TestToCorridorJSONCarriesPathCount(t *testing.T) {
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
		PathCount:     3,
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
	if got := out.Rungs[0].PathCount; got != 3 {
		t.Fatalf("rung path_count = %d, want 3", got)
	}
	if out.Rungs[0].Quote == nil || out.Rungs[0].Quote.PathCount != 3 {
		t.Fatalf("rung quote path_count = %+v, want 3", out.Rungs[0].Quote)
	}
	if out.Recommended == nil || out.Recommended.PathCount != 3 {
		t.Fatalf("recommended path_count = %+v, want 3", out.Recommended)
	}
}

// TestToCorridorJSONCarriesReferenceDiagnostic pins the wire shape for the
// third-provider attribution: it appears only when a diagnostic exists, names
// the provider and the outlier, and carries the figures that justify the
// attribution as decimal strings.
func TestToCorridorJSONCarriesReferenceDiagnostic(t *testing.T) {
	asOf := time.Date(2026, 10, 19, 7, 55, 4, 0, time.UTC)
	lr := &LadderResult{
		Reference: refrate.Rate{
			Base:            "USD",
			Quote:           "NGN",
			Mid:             decimal.RequireFromString("1365"),
			Source:          "secondary",
			Agreement:       refrate.AgreementDisagree,
			SecondaryMid:    decimal.RequireFromString("1300"),
			SecondarySource: "primary",
			DivergencePct:   decimal.RequireFromString("5"),
			Diagnostic: &refrate.Diagnostic{
				Source:             "floatrates",
				Mid:                decimal.RequireFromString("1350"),
				AsOf:               asOf,
				Outlier:            "primary",
				OutlierDistancePct: decimal.RequireFromString("3.8461538"),
				OtherDistancePct:   decimal.RequireFromString("1.1111111"),
			},
		},
	}

	out := ToCorridorJSON(lr, "USD/NGN")
	d := out.ReferenceDiagnostic
	if d == nil {
		t.Fatal("reference_diagnostic is nil; a Diagnostic must reach the wire")
	}
	if d.Source != "floatrates" || d.Outlier != "primary" {
		t.Errorf("diagnostic = %+v, want source floatrates naming primary as the outlier", d)
	}
	if d.Mid != "1350" {
		t.Errorf("diagnostic mid = %q, want 1350", d.Mid)
	}
	if d.AsOf != asOf.Format(time.RFC3339) {
		t.Errorf("diagnostic as_of = %q, want %q", d.AsOf, asOf.Format(time.RFC3339))
	}
	if d.OutlierDistancePct != "3.8462" || d.OtherDistancePct != "1.1111" {
		t.Errorf("diagnostic distances = %q/%q, want 3.8462/1.1111",
			d.OutlierDistancePct, d.OtherDistancePct)
	}
	if d.Reason != "" {
		t.Errorf("diagnostic reason = %q, want empty when an outlier was named", d.Reason)
	}

	// The diagnostic's own figures are money and must be walked at the
	// boundary like every other decimal string.
	values := MoneyStrings(out)
	for _, want := range []string{"1350", "3.8462", "1.1111"} {
		found := false
		for _, got := range values {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MoneyStrings omits the diagnostic value %q", want)
		}
	}
}

// TestToCorridorJSONOmitsAFailedDiagnosticsMid pins the unknown-versus-zero
// rule on the diagnostic: a provider that failed has no mid, so the field is
// absent rather than published as "0", which a client would read as a quoted
// rate of zero.
func TestToCorridorJSONOmitsAFailedDiagnosticsMid(t *testing.T) {
	lr := &LadderResult{
		Reference: refrate.Rate{
			Mid:       decimal.RequireFromString("1365"),
			Agreement: refrate.AgreementDisagree,
			Diagnostic: &refrate.Diagnostic{
				Source: "floatrates",
				Reason: "diagnostic provider unavailable: timeout",
			},
		},
	}

	out := ToCorridorJSON(lr, "USD/NGN")
	d := out.ReferenceDiagnostic
	if d == nil {
		t.Fatal("reference_diagnostic is nil; the attempted provider must be named")
	}
	if d.Mid != "" {
		t.Errorf("diagnostic mid = %q, want empty: a failed provider quoted no rate", d.Mid)
	}
	if d.Source != "floatrates" || d.Outlier != "" || !strings.Contains(d.Reason, "unavailable") {
		t.Errorf("diagnostic = %+v, want floatrates named with a failure reason and no outlier", d)
	}
	// On the wire the key must be absent entirely: a "0" would read as a
	// quoted rate of zero rather than as an unavailable observation.
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	var doc struct {
		Diagnostic map[string]json.RawMessage `json:"reference_diagnostic"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}
	if _, ok := doc.Diagnostic["mid"]; ok {
		t.Errorf("reference_diagnostic carries a mid key: %s", raw)
	}
}

func TestToCorridorJSONOmitsAbsentReferenceDiagnostic(t *testing.T) {
	out := ToCorridorJSON(&LadderResult{
		Reference: refrate.Rate{Mid: decimal.RequireFromString("1350")},
	}, "USD/NGN")
	if out.ReferenceDiagnostic != nil {
		t.Errorf("reference_diagnostic = %+v, want absent when no diagnostic ran", out.ReferenceDiagnostic)
	}
}

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

// assertHopKeys fails unless the marshaled hop carries exactly the named
// keys — no dropped field, and no new one appearing silently. This is the
// shape pin of the hops schema: values are asserted alongside it, but the
// key set is the contract a client decodes against.
func assertHopKeys(t *testing.T, i int, got map[string]json.RawMessage, want ...string) {
	t.Helper()
	wantSet := make(map[string]bool, len(want))
	for _, k := range want {
		wantSet[k] = true
	}
	for k, v := range got {
		if !wantSet[k] {
			t.Errorf("hop %d carries unexpected key %q (value %s)", i, k, v)
		}
	}
	for k := range wantSet {
		if _, ok := got[k]; !ok {
			t.Errorf("hop %d is missing key %q", i, k)
		}
	}
}

// hopsOf marshals a rendered QuoteJSON and returns its hops array as raw
// JSON per entry, failing the test when the key is absent or is not an
// array — the shape check runs on the actual bytes, not the Go struct.
func hopsOf(t *testing.T, j *QuoteJSON) []map[string]json.RawMessage {
	t.Helper()
	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(buf, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	raw, ok := top["hops"]
	if !ok {
		t.Fatalf("marshaled quote has no \"hops\" key — the route must publish as data, not only as the description string: %s", buf)
	}
	if !strings.HasPrefix(string(raw), "[") {
		t.Fatalf("hops = %s, want an array", raw)
	}
	var hops []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &hops); err != nil {
		t.Fatalf("unmarshal hops: %v", err)
	}
	return hops
}

// TestQuoteJSONHopsSchema is the schema test issue #100 asks for: it pins
// the exact wire shape of quote.hops (backlog #84) — an array beside the
// description holding every asset of the path in execution order with both
// endpoints, each entry carrying code, issuer where established, peg where
// known, and the SEP-38 wire form, and no other keys.
//
// The path is the recorded USDC -> GHSC shape documented in route/route.go:
// every priced path ran through XLM and NGNC. Exact key sets and exact
// values are both asserted, so a dropped field, a renamed tag, a reordered
// hop or an issuer quietly going missing all fail this test — as does any
// change to the description string, which must stay byte-for-byte display
// text beside the new data.
func TestQuoteJSONHopsSchema(t *testing.T) {
	const wantDescription = "USDC -> XLM -> NGNC -> GHSC"
	q := &Quote{
		Kind:          KindDEX,
		Description:   wantDescription,
		Source:        "stellar-dex",
		SendAsset:     asset.USDC(),
		SendAmount:    decimal.RequireFromString("100"),
		Hops:          []asset.Asset{asset.Native(), asset.NGNC()},
		ReceiveAsset:  asset.GHSC(),
		ReceiveAmount: decimal.RequireFromString("600"),
		Verdict:       VerdictFair,
	}

	j := ToQuoteJSON(q)

	// The description string is unchanged — it stays for display and for
	// compatibility, on the wire as well as on the struct.
	buf, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(buf, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Decode rather than compare raw bytes: encoding/json escapes '>' as
	// \u003e inside strings, so the raw message is not the string itself.
	var description string
	if err := json.Unmarshal(top["description"], &description); err != nil {
		t.Fatalf("unmarshal description: %v", err)
	}
	if description != wantDescription {
		t.Errorf("description = %q, want %q unchanged", description, wantDescription)
	}

	hops := hopsOf(t, j)
	if len(hops) != 4 {
		t.Fatalf("hops has %d entries, want 4 (send, two intermediates, receive): %s", len(hops), hops)
	}

	type wantHop struct {
		code, issuer, peg, asset string
		keys                     []string
	}
	want := []wantHop{
		{
			code: "USDC", issuer: asset.USDCIssuer,
			asset: "stellar:USDC:" + asset.USDCIssuer,
			keys:  []string{"code", "issuer", "asset"},
		},
		{
			// Native XLM: no issuer by nature, identified in full by
			// stellar:native — and not marked issuer_unknown.
			code: "XLM", asset: "stellar:native",
			keys: []string{"code", "asset"},
		},
		{
			code: "NGNC", issuer: asset.LinkIOIssuer, peg: "NGN",
			asset: "stellar:NGNC:" + asset.LinkIOIssuer,
			keys:  []string{"code", "issuer", "peg", "asset"},
		},
		{
			// GHSC is issued by the same account as NGNC; its wire form
			// names its own code.
			code: "GHSC", issuer: asset.LinkIOIssuer, peg: "GHS",
			asset: "stellar:GHSC:" + asset.LinkIOIssuer,
			keys:  []string{"code", "issuer", "peg", "asset"},
		},
	}

	for i, w := range want {
		h := hops[i]
		assertHopKeys(t, i, h, w.keys...)

		if got := strings.Trim(string(h["code"]), `"`); got != w.code {
			t.Errorf("hop %d code = %q, want %q (execution order: send, intermediates, receive)", i, got, w.code)
		}
		// The identity rule: an issuer in the want is present in the
		// JSON, and an issuer absent from the want is absent from it —
		// never a code published alone for a reader to guess at.
		issuer, hasIssuer := h["issuer"]
		if w.issuer == "" {
			if hasIssuer {
				t.Errorf("hop %d issuer = %s, want no issuer key (native asset)", i, issuer)
			}
		} else if !hasIssuer {
			t.Errorf("hop %d (%s) is missing its issuer — an asset code identifies nothing without it", i, w.code)
		} else if got := strings.Trim(string(issuer), `"`); got != w.issuer {
			t.Errorf("hop %d issuer = %s, want %q", i, got, w.issuer)
		}

		if w.peg == "" {
			if _, ok := h["peg"]; ok {
				t.Errorf("hop %d carries a peg %s it does not have", i, h["peg"])
			}
		} else if got := strings.Trim(string(h["peg"]), `"`); got != w.peg {
			t.Errorf("hop %d peg = %s, want %q — peg where known", i, got, w.peg)
		}

		if got := strings.Trim(string(h["asset"]), `"`); got != w.asset {
			t.Errorf("hop %d asset = %s, want %q", i, got, w.asset)
		}
	}
}

// TestQuoteJSONHopsSayWhenIssuerUnknown is the negative test behind the
// issue's fourth criterion: a hop whose issuing account cannot be
// established must say so on the wire instead of omitting the field
// silently. BLND is the real case — observed as a hop in recorded live
// paths and never verified against an issuer's stellar.toml (see
// asset/known.go) — so a consumer asking whether a route crosses a
// particular issuer is told "unknown" rather than left to guess an issuer
// from the code alone, which is the exact error this project refuses.
func TestQuoteJSONHopsSayWhenIssuerUnknown(t *testing.T) {
	q := &Quote{
		Kind:          KindDEX,
		Description:   "USDC -> BLND -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     asset.USDC(),
		Hops:          []asset.Asset{asset.Stellar("BLND", "")},
		ReceiveAsset:  asset.NGNC(),
		ReceiveAmount: decimal.RequireFromString("1000"),
		Verdict:       VerdictFair,
	}

	hops := hopsOf(t, ToQuoteJSON(q))
	if len(hops) != 3 {
		t.Fatalf("hops has %d entries, want 3: %s", len(hops), hops)
	}

	// The BLND hop says its issuer was not established: the marker is
	// present and true, and it is not papered over with a guessed wire
	// form or a made-up issuer.
	unknown := hops[1]
	assertHopKeys(t, 1, unknown, "code", "issuer_unknown")
	if got := string(unknown["issuer_unknown"]); got != "true" {
		t.Errorf("BLND hop issuer_unknown = %s, want true — the hop must say its issuer could not be established", got)
	}
	if got := strings.Trim(string(unknown["code"]), `"`); got != "BLND" {
		t.Errorf("BLND hop code = %q, want \"BLND\"", got)
	}

	// The neighbours are established and unaffected: real issuers, no
	// marker on either, and each one's own key set.
	for i, w := range []struct {
		code, issuer string
		keys         []string
	}{
		{"USDC", asset.USDCIssuer, []string{"code", "issuer", "asset"}},
		{"NGNC", asset.LinkIOIssuer, []string{"code", "issuer", "peg", "asset"}},
	} {
		idx := i * 2 // 0 and 2
		h := hops[idx]
		assertHopKeys(t, idx, h, w.keys...)
		if got := strings.Trim(string(h["code"]), `"`); got != w.code {
			t.Errorf("hop %d code = %q, want %q", idx, got, w.code)
		}
		if got := strings.Trim(string(h["issuer"]), `"`); got != w.issuer {
			t.Errorf("hop %d issuer = %q, want %q", idx, got, w.issuer)
		}
	}
}

// TestToQuoteJSONHopsIncludeBothEndpointsOnADirectPath covers the empty-
// intermediate case: a direct market still publishes its full route as two
// hops in execution order. An empty Hops means direct, not "route
// unavailable" — a consumer must never have to tell those apart by the
// absence of a field.
func TestToQuoteJSONHopsIncludeBothEndpointsOnADirectPath(t *testing.T) {
	q := &Quote{
		Kind:          KindDEX,
		Description:   "USDC -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     asset.USDC(),
		ReceiveAsset:  asset.NGNC(),
		ReceiveAmount: decimal.RequireFromString("1000"),
	}

	j := ToQuoteJSON(q)
	if len(j.Hops) != 2 {
		t.Fatalf("hops = %+v, want exactly the two endpoints of a direct path", j.Hops)
	}
	if j.Hops[0].Code != "USDC" || j.Hops[0].Issuer != asset.USDCIssuer {
		t.Errorf("hop 0 = %+v, want USDC with its issuer first", j.Hops[0])
	}
	if j.Hops[1].Code != "NGNC" || j.Hops[1].Issuer != asset.LinkIOIssuer {
		t.Errorf("hop 1 = %+v, want NGNC with its issuer last", j.Hops[1])
	}
}
