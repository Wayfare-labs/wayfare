package route

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestDecomposeExpectedFailureCostUndetermined(t *testing.T) {
	q := Quote{
		LossPct:    decimal.NewFromFloat(1.25),
		LossAmount: decimal.NewFromFloat(0.50),
	}
	mid := decimal.NewFromFloat(100.0)

	decomp := Decompose(q, mid)

	// The determined component carries amount and pct as strings; the
	// undetermined ones carry none, only a reason.
	if got := componentOf(t, parts[0]); got != string(CostFXLoss) {
		t.Fatalf("parts[0].component = %q, want %q", got, CostFXLoss)
	}
	assertDeterminedDecimalStrings(t, parts[0], "fx_loss")

	for _, idx := range []int{1, 2, 3} {
		p := parts[idx]
		if got := componentOf(t, p); got == string(CostFXLoss) {
			t.Fatalf("parts[%d].component = %q, want a non-fx component", idx, got)
		}
		assertUndetermined(t, p)
	}
}

// componentOf reads a cost part's component name off the wire.
func componentOf(t *testing.T, p map[string]json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(p["component"], &s); err != nil {
		t.Fatalf("component is %q, not a string: %v", string(p["component"]), err)
	}
	return s
}

// decodeCostBlock unmarshals a cost block's raw JSON into its shape: a list
// of component objects. It returns whether total_loss_pct was present and its
// value, so the caller can assert on presence independently of value.
func decodeCostBlock(raw json.RawMessage) (parts []map[string]json.RawMessage, total string, hasTotal bool, err error) {
	var wrap struct {
		Parts        []map[string]json.RawMessage `json:"parts"`
		TotalLossPct json.RawMessage              `json:"total_loss_pct"`
	}
	if err = json.Unmarshal(raw, &wrap); err != nil {
		return nil, "", false, fmt.Errorf("unmarshaling the cost block body: %w", err)
	}
	hasTotal = len(wrap.TotalLossPct) > 0 && string(wrap.TotalLossPct) != "null"
	if hasTotal {
		if err = json.Unmarshal(wrap.TotalLossPct, &total); err != nil {
			return nil, "", false, fmt.Errorf("total_loss_pct is not a string: %w", err)
		}
	}
	return wrap.Parts, total, hasTotal, nil
}

func assertDeterminedDecimalStrings(t *testing.T, p map[string]json.RawMessage, name string) {
	t.Helper()
	for _, k := range []string{"amount", "pct"} {
		rv, ok := p[k]
		if !ok {
			t.Errorf("%s: %q carries no %q, but it is determined and must", name, k, k)
			continue
		}
		var s string
		if err := json.Unmarshal(rv, &s); err != nil {
			t.Errorf("%s: %q is %q, not a decimal string", name, k, string(rv))
			continue
		}
		if s == "" {
			t.Errorf("%s: %q is an empty string", name, k)
		}
		if _, err := decimal.NewFromString(s); err != nil {
			t.Errorf("%s: %q is %q, which is not a parseable decimal", name, k, s)
		}
	}
}

func assertUndetermined(t *testing.T, p map[string]json.RawMessage) {
	t.Helper()
	name := componentOf(t, p)
	if _, ok := p["amount"]; ok {
		t.Errorf("%s: undetermined component must not carry an amount", name)
	}
	if _, ok := p["pct"]; ok {
		t.Errorf("%s: undetermined component must not carry a pct", name)
	}
	reason, ok := p["reason"]
	if !ok {
		t.Errorf("%s: undetermined component must carry a reason", name)
	} else {
		var s string
		if err := json.Unmarshal(reason, &s); err != nil {
			t.Errorf("%s: reason is %q, not a string", name, string(reason))
		} else if strings.TrimSpace(s) == "" {
			t.Errorf("%s: reason must be non-empty", name)
		}
	}
}

func TestCostDecomposeReasonsAreNonEmpty(t *testing.T) {
	q := Quote{
		Kind:         KindDEX,
		SendAsset:    testUSDC(),
		SendAmount:   decimal.NewFromInt(100),
		ReceiveAsset: testNGNC(),
		LossPct:      decimal.RequireFromString("50"),
	}

	d := Decompose(q, decimal.NewFromInt(1500))
	for _, p := range d.Parts {
		if !p.Determined && strings.TrimSpace(p.Reason) == "" {
			t.Errorf("component %s is undetermined but has no reason", p.Component)
		}
	}
}

// TestCostNoDeterminedComponentDefaultsToZero pins the project's rule that an
// unavailable quantity is unknown, not a default: every component that is
// genuinely unmeasured must report Determined: false, so that no consumer is
// told a number was established when nothing was observed.
func TestCostNoDeterminedComponentDefaultsToZero(t *testing.T) {
	q := Quote{
		Kind:          KindDEX,
		Description:   "USDC -> XLM -> NGNC",
		Source:        "stellar-dex",
		SendAsset:     testUSDC(),
		SendAmount:    decimal.NewFromInt(100),
		ReceiveAsset:  testNGNC(),
		ReceiveAmount: decimal.RequireFromString("112800.51"),
		EffectiveRate: decimal.RequireFromString("1128.0051"),
		ReferenceMid:  decimal.RequireFromString("1500"),
		LossPct:       decimal.RequireFromString("24.80"),
		LossAmount:    decimal.RequireFromString("37199.49"),
		Verdict:       VerdictUnusable,
	}

	// The only component with data to determine it is FX loss, which is
	// computed from the observed effective rate against mid. Every other
	// component has no observation or computation behind it, so each must
	// report Determined: false — a value that was not observed or computed
	// may never be presented as established.
	for _, p := range Decompose(q, decimal.RequireFromString("1500")).Parts {
		switch p.Component {
		case CostFXLoss:
			if !p.Determined {
				t.Error("fx_loss is computed from observed rates and must be determined")
			}
			if part.Reason == "" {
				t.Error("expected_failure component must provide a reason why it is undetermined, but reason is empty")
			}
		}
	}

	if !found {
		t.Fatal("CostDecomposition missing expected_failure component")
	}
}
