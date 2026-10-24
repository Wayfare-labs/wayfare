package checks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/refrate"
)

// FloorLossMetric measures the loss at the smallest priced ladder rung
// against the corridor's reference mid.
type FloorLossMetric struct {
	Size          decimal.Decimal
	EffectiveRate decimal.Decimal
	Reference     refrate.Rate
}

// Describe implements Metric.
func (FloorLossMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "floor-loss.reference-mid",
		Scope: ScopeCorridor,
		Cost:  CostFree,
		Venue: VenuePathfinding,
		Title: "Loss at the smallest priced rung against the reference mid",
		CanDetermine: "The loss percentage at the smallest priced ladder rung, " +
			"measured against the scorable reference mid.",
		CannotDetermine: "Whether the loss is caused by market pricing or route " +
			"structure. This reports the observed loss at one rung and does not " +
			"attribute its cause.",
	}
}

// Run implements Metric using the rung already measured by the route ladder.
func (m FloorLossMetric) Run(_ context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if !m.Reference.Scorable() {
		reason := "reference cross-check could not be scored: the two providers disagreed too far apart to trust either"
		if m.Reference.Note != "" {
			reason += " (" + m.Reference.Note + ")"
		}
		return MetricUndetermined(d, s, reason)
	}
	if m.Reference.Mid.IsZero() || m.Reference.Mid.IsNegative() {
		return MetricUndetermined(d, s, "no positive reference mid available")
	}
	if strings.TrimSpace(m.Reference.Base) == "" || strings.TrimSpace(m.Reference.Quote) == "" {
		return MetricUndetermined(d, s, "reference currency pair is unavailable")
	}
	if strings.TrimSpace(m.Reference.Source) == "" {
		return MetricUndetermined(d, s, "reference provider is unavailable")
	}
	if !m.Size.IsPositive() {
		return MetricUndetermined(d, s, "no priced ladder rung is available")
	}
	if !m.EffectiveRate.IsPositive() {
		return MetricUndetermined(d, s, "smallest priced rung has no positive effective rate")
	}

	loss := m.Reference.Mid.Sub(m.EffectiveRate).Div(m.Reference.Mid).Mul(decimal.NewFromInt(100))
	if loss.IsNegative() {
		loss = decimal.Zero
	}

	rungEvidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s", s.Send.Code, s.Receive.Code),
		Observed:   fmt.Sprintf("size=%s, effective_rate=%s", m.Size, m.EffectiveRate),
		ObservedAt: at,
	}
	referenceObserved := fmt.Sprintf("mid=%s", m.Reference.Mid)
	if !m.Reference.AsOf.IsZero() {
		referenceObserved += ", as_of=" + m.Reference.AsOf.UTC().Format(time.RFC3339)
	}
	referenceEvidence := Evidence{
		Source:     fmt.Sprintf("refrate %s via %s", m.Reference.Pair(), m.Reference.Source),
		Observed:   referenceObserved,
		ObservedAt: at,
	}
	summary := fmt.Sprintf(
		"loss at the smallest priced rung (%s %s) is %s%% against reference mid %s (%s)",
		m.Size, s.Send.Code, loss.StringFixed(4), m.Reference.Mid, m.Reference.Source)

	return MetricValue(d, s, loss, UnitPercent, summary, rungEvidence, referenceEvidence)
}
