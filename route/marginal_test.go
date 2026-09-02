package route

import (
	"testing"

	"github.com/shopspring/decimal"
)

func marginalLadder(receives ...string) *LadderResult {
	l := &LadderResult{ReferenceMid: decimal.NewFromInt(100), Rungs: make([]Rung, len(receives))}
	for i, receive := range receives {
		amount := decimal.NewFromInt(int64(i + 1))
		l.Rungs[i] = Rung{SendAmount: amount, Result: &Result{Integrity: IntegrityDirect, Quotes: []Quote{{SendAmount: amount, ReceiveAmount: decimal.RequireFromString(receive)}}}}
	}
	l.computeMarginalCosts()
	return l
}

func TestMarginalClassification(t *testing.T) {
	cases := []struct {
		name     string
		receives []string
		want     MarginalClassification
	}{
		{"improving", []string{"90", "181", "273"}, MarginalImproving},
		{"flat", []string{"90", "180", "270"}, MarginalFlat},
		{"worsening", []string{"90", "179", "267"}, MarginalWorsening},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := marginalLadder(tc.receives...)
			if l.MarginalClassification != tc.want {
				t.Fatalf("classification = %q, want %q", l.MarginalClassification, tc.want)
			}
			if l.Rungs[0].MarginalCost != nil {
				t.Fatal("first valid point must not have a marginal cost")
			}
		})
	}
}

func TestMarginalUndeterminedWithFewerThanTwoPoints(t *testing.T) {
	for _, receives := range [][]string{{}, {"90"}} {
		l := marginalLadder(receives...)
		if l.MarginalClassification != MarginalUndetermined {
			t.Errorf("classification = %q, want undetermined", l.MarginalClassification)
		}
	}
}
