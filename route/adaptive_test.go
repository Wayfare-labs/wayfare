package route_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/Wayfare-labs/wayfare/route"
)

// adaptiveEngine prices a direct USDC -> NGNC path at whatever receive rate the
// supplied function returns for the requested send amount. Every rung is one
// HTTP request, so the counter is the ladder's upstream request cost.
func adaptiveEngine(t *testing.T, rateOf func(decimal.Decimal) decimal.Decimal) (*route.Engine, *atomic.Int32, func()) {
	t.Helper()
	var requests atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		amount := r.URL.Query().Get("source_amount")
		d, err := decimal.NewFromString(amount)
		if err != nil {
			http.Error(w, "bad amount", http.StatusBadRequest)
			return
		}
		dest := d.Mul(rateOf(d))
		_, _ = fmt.Fprintf(w, `{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":%q,
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":%q,"path":[]}]}}`, amount, dest.String())
	}))

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1500"),
		}),
	}
	return e, &requests, srv.Close
}

func adaptiveLadder(t *testing.T, e *route.Engine, rule *route.AdaptiveRule, sizes ...string) *route.LadderResult {
	t.Helper()
	amounts := make([]decimal.Decimal, 0, len(sizes))
	for _, s := range sizes {
		amounts = append(amounts, decimal.RequireFromString(s))
	}
	res, err := e.Ladder(context.Background(), route.LadderRequest{
		SendAsset:      asset.USDC(),
		ReceiveAsset:   asset.NGNC(),
		Sizes:          amounts,
		Adaptive:       rule,
		ReferenceBase:  "USD",
		ReferenceQuote: "NGN",
	})
	if err != nil {
		t.Fatalf("Ladder: %v", err)
	}
	return res
}

func adaptiveRungs(res *route.LadderResult) int {
	n := 0
	for _, r := range res.Rungs {
		if r.Adaptive {
			n++
		}
	}
	return n
}

// TestAdaptiveLadderAddsAMeasuredMidpoint pins the core rule: a loss jump above
// the threshold adds the midpoint size, the added rung is marked, and its
// figure comes from the upstream quote rather than from either neighbour.
func TestAdaptiveLadderAddsAMeasuredMidpoint(t *testing.T) {
	// Losses at 1 and 3 differ by 13.3 points, so the midpoint 2 qualifies.
	// The rate at 2 is deliberately unlike either neighbour: an implementation
	// that interpolated would report 1300, the measured value is 500.
	rate := func(d decimal.Decimal) decimal.Decimal {
		if d.Equal(decimal.RequireFromString("2")) {
			return decimal.RequireFromString("500")
		}
		return decimal.RequireFromString("1500").Sub(d.Mul(decimal.RequireFromString("100")))
	}
	e, requests, close := adaptiveEngine(t, rate)
	defer close()

	rule := route.DefaultAdaptiveRule()
	res := adaptiveLadder(t, e, &rule, "1", "3")

	if len(res.Rungs) != 3 {
		t.Fatalf("rungs = %d, want 3 (1, added midpoint, 3)", len(res.Rungs))
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("upstream requests = %d, want one per rung (3)", got)
	}
	if got := adaptiveRungs(res); got != 1 {
		t.Errorf("adaptive rungs = %d, want 1", got)
	}

	mid := res.Rungs[1]
	if !mid.SendAmount.Equal(decimal.RequireFromString("2")) {
		t.Fatalf("middle rung size = %s, want the midpoint 2", mid.SendAmount)
	}
	if !mid.Adaptive {
		t.Error("the added rung is not marked Adaptive")
	}
	if !mid.Priced() {
		t.Fatalf("the added rung did not price: %v", mid.Err)
	}
	if got := mid.Result.Quotes[0].EffectiveRate; !got.Equal(decimal.RequireFromString("500")) {
		t.Errorf("added rung effective rate = %s, want the measured 500 — not an interpolated value", got)
	}
	if res.Rungs[0].Adaptive || res.Rungs[2].Adaptive {
		t.Error("a requested rung was marked Adaptive")
	}
	if !res.Rungs[0].SendAmount.LessThan(res.Rungs[1].SendAmount) ||
		!res.Rungs[1].SendAmount.LessThan(res.Rungs[2].SendAmount) {
		t.Errorf("rungs are not in ascending size order: %s, %s, %s",
			res.Rungs[0].SendAmount, res.Rungs[1].SendAmount, res.Rungs[2].SendAmount)
	}
}

// TestAdaptiveLadderRespectsItsRequestCostBound pins the maximum: the added
// rung count never exceeds the rule, the hard ceiling caps an excessive rule,
// and every added rung costs exactly one more upstream request.
func TestAdaptiveLadderRespectsItsRequestCostBound(t *testing.T) {
	rate := func(d decimal.Decimal) decimal.Decimal {
		return decimal.RequireFromString("1500").Sub(d.Mul(decimal.RequireFromString("100")))
	}
	sizes := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}

	t.Run("rule maximum", func(t *testing.T) {
		e, requests, close := adaptiveEngine(t, rate)
		defer close()
		rule := route.AdaptiveRule{MaxExtraRungs: 3, ThresholdPct: decimal.NewFromInt(5)}
		res := adaptiveLadder(t, e, &rule, sizes...)

		if got := adaptiveRungs(res); got != 3 {
			t.Errorf("adaptive rungs = %d, want the rule's maximum of 3", got)
		}
		if got := len(res.Rungs); got != len(sizes)+3 {
			t.Errorf("rungs = %d, want %d requested plus 3 added", got, len(sizes))
		}
		if got := requests.Load(); got != int32(len(sizes)+3) {
			t.Errorf("upstream requests = %d, want %d (requested + added)", got, len(sizes)+3)
		}
		if got := int(requests.Load()); got > len(sizes)+rule.MaxExtraRungs {
			t.Errorf("request cost %d exceeded the bound %d", got, len(sizes)+rule.MaxExtraRungs)
		}
	})

	t.Run("hard ceiling", func(t *testing.T) {
		e, requests, close := adaptiveEngine(t, rate)
		defer close()
		// All eight adjacent gaps qualify, and the rule asks for far more
		// than the package will ever add.
		rule := route.AdaptiveRule{MaxExtraRungs: 100, ThresholdPct: decimal.NewFromInt(5)}
		res := adaptiveLadder(t, e, &rule, sizes...)

		if got := adaptiveRungs(res); got != route.MaxAdaptiveExtraRungs {
			t.Errorf("adaptive rungs = %d, want the hard ceiling %d", got, route.MaxAdaptiveExtraRungs)
		}
		if got := int(requests.Load()); got > len(sizes)+route.MaxAdaptiveExtraRungs {
			t.Errorf("request cost %d exceeded the hard bound %d", got, len(sizes)+route.MaxAdaptiveExtraRungs)
		}
	})
}

// TestAdaptiveLadderOnlyTriggersBeyondTheThreshold pins "more than": a pair at
// exactly the threshold is not sampled, so the rule cannot creep toward
// sampling every rung.
func TestAdaptiveLadderOnlyTriggersBeyondTheThreshold(t *testing.T) {
	rate := func(d decimal.Decimal) decimal.Decimal {
		if d.Equal(decimal.RequireFromString("1")) {
			return decimal.RequireFromString("1200") // 20.00% loss
		}
		return decimal.RequireFromString("1125") // 25.00% loss, exactly 5 points away
	}
	e, requests, close := adaptiveEngine(t, rate)
	defer close()

	rule := route.AdaptiveRule{MaxExtraRungs: 4, ThresholdPct: decimal.NewFromInt(5)}
	res := adaptiveLadder(t, e, &rule, "1", "2")

	if got := len(res.Rungs); got != 2 {
		t.Errorf("rungs = %d, want 2: a difference equal to the threshold must not add a rung", got)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2", got)
	}
	if got := adaptiveRungs(res); got != 0 {
		t.Errorf("adaptive rungs = %d, want 0", got)
	}
}

// TestAdaptiveLadderDoesNotBridgeAHole pins "adjacent": an unmeasured rung
// breaks the comparison, because the two priced sizes on either side were not
// neighbours in the requested ladder.
func TestAdaptiveLadderDoesNotBridgeAHole(t *testing.T) {
	rate := func(d decimal.Decimal) decimal.Decimal {
		if d.Equal(decimal.RequireFromString("1")) {
			return decimal.Zero // never used: the handler below rejects 1
		}
		if d.LessThan(decimal.NewFromInt(1)) {
			return decimal.RequireFromString("1400")
		}
		return decimal.RequireFromString("500")
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		amount := r.URL.Query().Get("source_amount")
		d, err := decimal.NewFromString(amount)
		if err != nil {
			http.Error(w, "bad amount", http.StatusBadRequest)
			return
		}
		if d.Equal(decimal.NewFromInt(1)) {
			http.Error(w, "internal", http.StatusInternalServerError)
			return
		}
		dest := d.Mul(rate(d))
		_, _ = fmt.Fprintf(w, `{"_embedded":{"records":[{
			"source_asset_type":"credit_alphanum4","source_asset_code":"USDC",
			"source_amount":%q,
			"destination_asset_type":"credit_alphanum4","destination_asset_code":"NGNC",
			"destination_amount":%q,"path":[]}]}}`, amount, dest.String())
	}))
	defer srv.Close()

	e := &route.Engine{
		DEX: &dex.Client{HorizonURL: srv.URL},
		RefRate: refrate.NewStatic(map[string]decimal.Decimal{
			"USD/NGN": decimal.RequireFromString("1500"),
		}),
	}
	rule := route.DefaultAdaptiveRule()
	res := adaptiveLadder(t, e, &rule, "0.1", "1", "10")

	if len(res.Rungs) != 3 {
		t.Fatalf("rungs = %d, want the three requested sizes", len(res.Rungs))
	}
	if got := adaptiveRungs(res); got != 0 {
		t.Errorf("adaptive rungs = %d, want 0: a hole is not an adjacent pair", got)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("upstream requests = %d, want only the three requested rungs", got)
	}
}

// TestAdaptiveLadderIsOffByDefault guards the opt-in property.
func TestAdaptiveLadderIsOffByDefault(t *testing.T) {
	rate := func(d decimal.Decimal) decimal.Decimal {
		return decimal.RequireFromString("1500").Sub(d.Mul(decimal.RequireFromString("300")))
	}
	e, requests, close := adaptiveEngine(t, rate)
	defer close()

	res := adaptiveLadder(t, e, nil, "1", "2")
	if got := len(res.Rungs); got != 2 {
		t.Errorf("rungs = %d, want the 2 requested with no rule", got)
	}
	if got := adaptiveRungs(res); got != 0 {
		t.Errorf("adaptive rungs = %d, want 0 with no rule", got)
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2", got)
	}
}

// TestAdaptiveRungsAreMarkedOnTheWire pins the published contract: an added
// rung carries adaptive:true on the rung and on its curve point, and a
// requested rung's object is not changed by the new field at all.
func TestAdaptiveRungsAreMarkedOnTheWire(t *testing.T) {
	rate := func(d decimal.Decimal) decimal.Decimal {
		return decimal.RequireFromString("1500").Sub(d.Mul(decimal.RequireFromString("300")))
	}
	e, _, close := adaptiveEngine(t, rate)
	defer close()

	rule := route.DefaultAdaptiveRule()
	res := adaptiveLadder(t, e, &rule, "1", "3")
	out := route.ToCorridorJSON(res, "USD/NGN")

	if len(out.Rungs) != 3 {
		t.Fatalf("wire rungs = %d, want 3", len(out.Rungs))
	}
	if !out.Rungs[1].Adaptive {
		t.Error("the added rung is not marked adaptive on the wire")
	}
	if out.Rungs[0].Adaptive || out.Rungs[2].Adaptive {
		t.Error("a requested rung is marked adaptive on the wire")
	}

	marshaled, err := json.Marshal(out.Rungs[1])
	if err != nil {
		t.Fatalf("marshal added rung: %v", err)
	}
	var added map[string]any
	if err := json.Unmarshal(marshaled, &added); err != nil {
		t.Fatalf("unmarshal added rung: %v", err)
	}
	if added["adaptive"] != true {
		t.Errorf("added rung JSON = %s, want adaptive:true", marshaled)
	}

	fixed, err := json.Marshal(out.Rungs[0])
	if err != nil {
		t.Fatalf("marshal requested rung: %v", err)
	}
	var requested map[string]any
	if err := json.Unmarshal(fixed, &requested); err != nil {
		t.Fatalf("unmarshal requested rung: %v", err)
	}
	if _, ok := requested["adaptive"]; ok {
		t.Errorf("requested rung JSON = %s, want no adaptive key when false", fixed)
	}

	if out.Curve == nil {
		t.Fatal("curve is nil; an adaptive point must be representable there too")
	}
	found := false
	for _, p := range out.Curve.Points {
		if p.Size == "2" {
			if !p.Adaptive {
				t.Error("the curve point at the added size is not marked adaptive")
			}
			found = true
		}
	}
	if !found {
		t.Errorf("curve points = %+v, want a point at the added midpoint size", out.Curve.Points)
	}
}
