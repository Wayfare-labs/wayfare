package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/refrate"
	"github.com/shopspring/decimal"
)

// The scale guard for issue #617.
//
// docs/api.md declares, money field by money field, how many decimals reach
// the wire and what rounding produced them. A declaration a renderer can
// drift away from is worse than none, because it reads as verified. So this
// file prices one corridor in which every money field carries the same value,
// reads the documented scale for each field out of docs/api.md, and compares
// the two. Three mutations fail it: changing a StringFixed(n) argument,
// swapping a renderer to String() or back, and adding a money field to the
// wire shape without documenting it.

const (
	// probeMoney is the value every money field in the fixture carries. Its
	// nine distinct decimals make each scale class render to a different,
	// unambiguous string, so a field that changed scale cannot produce a
	// value another field already produced.
	probeMoney = "0.123456789"

	// renderedAtFull is what probeMoney looks like unrounded. It is spelled
	// out rather than derived because the absence tests below compare against
	// it, and a constant that a reader can check by eye is the point.
	renderedAtFull = "0.123456789"
)

func probe(t *testing.T) decimal.Decimal {
	t.Helper()
	d, err := decimal.NewFromString(probeMoney)
	if err != nil {
		t.Fatalf("probe money %q does not parse: %v", probeMoney, err)
	}
	return d
}

// scaleQuote is one priced quote whose every money field is the probe value.
func scaleQuote(q *Quote, p decimal.Decimal) *Quote {
	q.ReceiveAmount = p
	q.EffectiveRate = p
	q.LossPct = p
	q.LossAmount = p
	q.Verdict = VerdictFair
	return q
}

// scaleFixture renders the corridor document the guard reads. It is built by
// hand rather than measured so that each money field is reachable at the same
// value: the conditional branches in ToCorridorJSON (a second provider that
// answered, a diagnostic, a reported parallel mid, a priced curve point, a
// determined cost part) are the reason some money fields are usually absent,
// and absent fields cannot be checked for scale.
func scaleFixture(t *testing.T) CorridorJSON {
	t.Helper()
	p := probe(t)

	send := asset.Asset{Kind: asset.KindStellar, Code: "USDC", Issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"}
	receive := asset.Asset{Kind: asset.KindStellar, Code: "NGNC", Issuer: "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"}

	recommended := scaleQuote(&Quote{
		Kind:         KindDEX,
		Source:       "stellar-dex",
		Description:  "USDC -> NGNC",
		SendAsset:    send,
		SendAmount:   p,
		ReceiveAsset: receive,
	}, p)

	rungQuote := scaleQuote(new(Quote), p)
	rungQuote.Kind = KindDEX
	rungQuote.Source = "stellar-dex"
	rungQuote.SendAsset = send
	rungQuote.SendAmount = p
	rungQuote.ReceiveAsset = receive

	rung := Rung{
		SendAmount: p,
		Result: &Result{
			Integrity: IntegrityDirect,
			Quotes:    []Quote{*rungQuote},
		},
		MarginalCost: &MarginalCost{From: p, To: p, Cost: p},
		Decomposition: CostDecomposition{
			TotalLossPct: p,
			Parts: []CostPart{
				{Component: CostFXLoss, Amount: p, Pct: p, Determined: true},
				{Component: CostNetworkFees, Determined: false, Reason: "not observable from a priced path"},
			},
		},
	}

	l := &LadderResult{
		Request: LadderRequest{
			SendAsset:      send,
			ReceiveAsset:   receive,
			ReferenceBase:  "USD",
			ReferenceQuote: "NGN",
		},
		Integrity:       IntegrityDirect,
		ReferenceMid:    p,
		ReferenceSource: "exchangerate-api",
		Reference: refrate.Rate{
			Base:            "USD",
			Quote:           "NGN",
			Mid:             p,
			Source:          "exchangerate-api",
			SecondaryMid:    p,
			SecondarySource: "stripe",
			DivergencePct:   p,
			Agreement:       refrate.AgreementDisagree,
			Diagnostic: &refrate.Diagnostic{
				Source:             "xe",
				Mid:                p,
				Outlier:            "stripe",
				OutlierDistancePct: p,
				OtherDistancePct:   p,
			},
		},
		Parallel: &refrate.Parallel{
			Status: refrate.ParallelReported,
			Mid:    p,
			Source: "street-market",
			GapPct: p,
		},
		Floor:           p,
		FloorSize:       p,
		WorstLoss:       p,
		WorstSize:       p,
		Recommended:     recommended,
		RecommendedSize: p,
		Finding:         "finding under test",
		Rungs:           []Rung{rung},
		Curve: &ExecutionRateCurve{
			Points: []ExecutionRatePoint{
				{Size: p, Rate: p, Priced: true},
				{Size: p, Priced: false, Reason: "no path at this size"},
			},
			PricedCount:      1,
			ObservationCount: 2,
		},
	}

	return ToCorridorJSON(l, l.Reference.Pair())
}

// wireMoney walks a rendered corridor document and returns every money string
// it publishes, keyed by wire path with array indices collapsed to "[]" — the
// same paths docs/api.md names, so the two sides of the comparison are
// addressable in the same words.
//
// "Money" here is decided by the value, not by the field name: a string that
// parses as a decimal. In this fixture that is exact — the only strings the
// probe produces are the three scale renderings, and every other string in
// the document is a code, a source, a verdict or a timestamp, none of which
// parse.
func wireMoney(t *testing.T, c CorridorJSON) map[string]string {
	t.Helper()

	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal corridor document: %v", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal corridor document: %v", err)
	}

	out := map[string]string{}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch node := v.(type) {
		case map[string]any:
			for k, child := range node {
				next := k
				if path != "" {
					next = path + "." + k
				}
				walk(next, child)
			}
		case []any:
			for _, child := range node {
				walk(path+"[]", child)
			}
		case string:
			if _, err := decimal.NewFromString(node); err != nil {
				return
			}
			out[path] = node
		}
	}
	walk("", doc)
	return out
}

// documentedScales reads the scale docs/api.md declares for each money field
// of the corridor document, from the "Money fields: scale and rounding"
// section up to the next heading. Trend figures are documented in their own
// subsection because route/wire.go does not render them.
func documentedScales(t *testing.T) map[string]string {
	t.Helper()

	path := filepath.Join("..", "docs", "api.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	const heading = "### Money fields: scale and rounding"
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, heading) {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s: no %q section — the scale documentation was renamed or deleted", path, heading)
	}

	fixedScale := regexp.MustCompile(`^exactly (\d+)$`)
	out := map[string]string{}
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "#") {
			break
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) < 3 {
			continue
		}
		field := strings.TrimSpace(cells[0])
		if !strings.HasPrefix(field, "`") {
			// The header row and any row that is not naming one field.
			continue
		}
		if strings.Count(field, "`") != 2 {
			t.Errorf("%s: row %q names more than one field; the guard reads one backticked name per row", path, line)
			continue
		}
		name := strings.Trim(field, "`")
		scale := strings.TrimSpace(cells[1])
		if _, dup := out[name]; dup {
			t.Errorf("%s: field %q is documented in two money rows; the guard reads one scale per field", path, name)
		}
		switch {
		case scale == "full precision":
			out[name] = scale
		case fixedScale.MatchString(scale):
			out[name] = scale
		default:
			t.Errorf("%s: field %q declares an unrecognised scale %q", path, name, scale)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: the money tables parsed to zero fields", path)
	}
	return out
}

// declaredExpected renders the probe value the way the documented scale says
// the wire should. Deriving it from decimal rather than hardcoding it is the
// point: the guard tests that route/wire.go and docs/api.md agree, so a change
// to one side alone fails, and a change to both sides together is a deliberate
// re-documentation the guard has nothing left to complain about.
func declaredExpected(t *testing.T, scale string) string {
	t.Helper()
	if scale == "full precision" {
		return probe(t).String()
	}
	m := regexp.MustCompile(`^exactly (\d+)$`).FindStringSubmatch(scale)
	if m == nil {
		t.Fatalf("scale %q is not a form documentedScales accepts", scale)
	}
	places, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("scale %q has an unreadable width: %v", scale, err)
	}
	return probe(t).StringFixed(int32(places))
}

// scaleByField is the one place that knows both the name docs/api.md uses and
// the path the wire emits. It is deliberately two-directional: a field that
// appears in only one of the two documents fails the guard.
var scaleByField = map[string][]string{
	"reference_mid":                             {"reference_mid"},
	"reference_secondary_mid":                   {"reference_secondary_mid"},
	"reference_divergence_pct":                  {"reference_divergence_pct"},
	"reference_diagnostic.mid":                  {"reference_diagnostic.mid"},
	"reference_diagnostic.outlier_distance_pct": {"reference_diagnostic.outlier_distance_pct"},
	"reference_diagnostic.other_distance_pct":   {"reference_diagnostic.other_distance_pct"},
	"parallel_mid":                              {"parallel_mid"},
	"parallel_gap_pct":                          {"parallel_gap_pct"},
	"floor_loss_pct":                            {"floor_loss_pct"},
	"worst_loss_pct":                            {"worst_loss_pct"},
	"floor_size":                                {"floor_size"},
	"worst_size":                                {"worst_size"},
	"recommended_size":                          {"recommended_size"},

	// One shape rendered in two places: the recommendation and each rung's
	// quote are the same struct, so both must carry the documented scale.
	"receive_amount":        {"recommended.receive_amount", "rungs[].quote.receive_amount"},
	"effective_rate":        {"recommended.effective_rate", "rungs[].quote.effective_rate"},
	"loss_pct":              {"recommended.loss_pct", "rungs[].quote.loss_pct"},
	"loss_amount":           {"recommended.loss_amount", "rungs[].quote.loss_amount"},
	"total_loss_pct":        {"rungs[].cost.total_loss_pct"},
	"parts[].amount":        {"rungs[].cost.parts[].amount"},
	"parts[].pct":           {"rungs[].cost.parts[].pct"},
	"size":                  {"curve.points[].size"},
	"rate":                  {"curve.points[].rate"},
	"rungs[].send_amount":   {"rungs[].send_amount"},
	"rungs[].marginal_cost": {"rungs[].marginal_cost"},
	"rungs[].marginal_from": {"rungs[].marginal_from"},
	"rungs[].marginal_to":   {"rungs[].marginal_to"},
}

func TestDocumentedMoneyScaleMatchesWire(t *testing.T) {
	doc := documentedScales(t)
	c := scaleFixture(t)
	wire := wireMoney(t, c)

	for field, scale := range doc {
		paths, ok := scaleByField[field]
		if !ok {
			t.Errorf("docs/api.md declares a scale for %q but the guard has no wire path for it: the field was renamed or added to the documentation only", field)
			continue
		}
		want := declaredExpected(t, scale)
		for _, p := range paths {
			got, present := wire[p]
			if !present {
				t.Errorf("%s: documented as money on the wire but the fixture never emits it — a branch in ToCorridorJSON stopped rendering it, so its scale is now unguarded", p)
				continue
			}
			if got != want {
				t.Errorf("%s: docs/api.md declares %q, which renders %q, but route/wire.go emitted %q", p, scale, want, got)
			}
			delete(wire, p)
		}
	}

	for p, got := range wire {
		t.Errorf("%s: money reaches the wire (%s) but docs/api.md never names it — document its scale and add it to route/money.go's walker", p, got)
	}

	for field, paths := range scaleByField {
		if _, ok := doc[field]; !ok {
			t.Errorf("%q is guarded at %s but docs/api.md has no row for it", field, strings.Join(paths, ", "))
		}
	}
}

// TestUndeterminedMoneyIsAbsentNotZero pins the sentence every one of the
// tables rests on: a money field the engine did not determine is missing from
// the document, never a rounded zero. An undetermined cost part is the case a
// producer could get wrong silently, because StringFixed(2) of a zero renders
// "0.00" — a plausible fee for a fee that was never measured.
func TestUndeterminedMoneyIsAbsentNotZero(t *testing.T) {
	c := scaleFixture(t)

	if len(c.Rungs) != 1 || c.Rungs[0].Cost == nil || len(c.Rungs[0].Cost.Parts) != 2 {
		t.Fatalf("fixture lost its cost block: %+v", c.Rungs)
	}
	undetermined := c.Rungs[0].Cost.Parts[1]
	if undetermined.Determined {
		t.Fatal("fixture's second cost part is determined; the guard needs the undetermined shape")
	}
	if undetermined.Amount != "" || undetermined.Pct != "" {
		t.Errorf("undetermined cost part published amount %q and pct %q, want neither", undetermined.Amount, undetermined.Pct)
	}

	wire := wireMoney(t, c)
	for _, p := range []string{"rungs[].cost.parts[].amount", "rungs[].cost.parts[].pct"} {
		if _, ok := wire[p]; !ok {
			// The determined part still emits it, so this only fails if both
			// parts vanished; say which.
			t.Errorf("%s disappeared from the document entirely — the determined part must still publish it", p)
		}
	}

	// An unpriced curve point is the other hole: size with no rate.
	if c.Curve == nil || len(c.Curve.Points) != 2 {
		t.Fatalf("fixture lost its curve: %+v", c.Curve)
	}
	hole := c.Curve.Points[1]
	if hole.Priced {
		t.Fatal("fixture's second curve point became priced")
	}
	if hole.Rate != "" {
		t.Errorf("unpriced curve point published rate %q, want it omitted", hole.Rate)
	}
	if hole.Size != renderedAtFull {
		t.Errorf("unpriced curve point published size %q, want %q", hole.Size, renderedAtFull)
	}
}

// TestFixedScaleRoundsHalfAwayFromZero pins the rounding rule the two fixed
// tables claim, at the width each table uses and on the signed field where a
// negative actually reaches the wire. Half away from zero, not half to even:
// the round-to-even neighbour would render 2.675 as 2.67 and 0.125 as 0.12.
func TestFixedScaleRoundsHalfAwayFromZero(t *testing.T) {
	cases := []struct {
		value string
		width int
		want  string
	}{
		{"2.675", 2, "2.68"},
		{"0.125", 2, "0.13"},
		{"-2.675", 2, "-2.68"},
		{"-2.67505", 4, "-2.6751"},
		{"0.00005", 4, "0.0001"},
	}
	for _, tc := range cases {
		d := decimal.RequireFromString(tc.value)
		if got := d.StringFixed(int32(tc.width)); got != tc.want {
			t.Errorf("%s.StringFixed(%d) = %q, want %q", tc.value, tc.width, got, tc.want)
		}
	}

	// The rule holds through the renderers, not only through decimal:
	// loss_amount is the 2-decimal field and parallel_gap_pct the signed
	// 4-decimal one.
	q := scaleQuote(&Quote{SendAmount: probe(t)}, probe(t))
	q.LossAmount = decimal.RequireFromString("2.675")
	if got := ToQuoteJSON(q).LossAmount; got != "2.68" {
		t.Errorf("ToQuoteJSON rendered loss_amount %q from 2.675, want %q", got, "2.68")
	}

	l := &LadderResult{Parallel: &refrate.Parallel{
		Status: refrate.ParallelReported,
		Mid:    probe(t),
		Source: "street-market",
		GapPct: decimal.RequireFromString("-2.67505"),
	}}
	if got := ToCorridorJSON(l, "USD/NGN").ParallelGapPct; got != "-2.6751" {
		t.Errorf("ToCorridorJSON rendered parallel_gap_pct %q from -2.67505, want %q", got, "-2.6751")
	}
}

// TestFixedScalePublishesZeroAtFullWidth backs the sentence that a fixed-scale
// field renders zero as 0.00 rather than 0, and that a full-precision field
// renders it as 0. A consumer that pads or compares on digit count needs both.
func TestFixedScalePublishesZeroAtFullWidth(t *testing.T) {
	q := ToQuoteJSON(scaleQuote(&Quote{SendAmount: decimal.Zero}, decimal.Zero))
	if q.LossAmount != "0.00" {
		t.Errorf("zero loss_amount renders %q, want %q", q.LossAmount, "0.00")
	}
	if q.LossPct != "0" {
		t.Errorf("zero loss_pct renders %q, want %q", q.LossPct, "0")
	}
}

// TestFullPrecisionTrimsTrailingZeros backs the claim that a full-precision
// field's width is not fixed: the same field arrives at different decimal
// counts as the underlying measurement changes.
func TestFullPrecisionTrimsTrailingZeros(t *testing.T) {
	if got := decimal.RequireFromString("5000.000").String(); got != "5000" {
		t.Errorf("5000.000 renders %q, want %q", got, "5000")
	}
	if got := decimal.RequireFromString("0.1000000").String(); got != "0.1" {
		t.Errorf("0.1000000 renders %q, want %q", got, "0.1")
	}
}
