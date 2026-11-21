package checks

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
	"github.com/Wayfare-labs/wayfare/refrate"
)

// ExitLiquidityMetric measures the cost of converting the fiat token back to
// USDC — the reverse strict-send loss at a small and a medium size — as a
// metric attached to the forward corridor.
//
// Why this is a corridor property: a recipient who cannot exit holds a token
// whose value depends on the issuer's redemption, so exit cost belongs on the
// forward corridor a sender is evaluating. The reverse direction prices the
// destination token (what the recipient holds) back against USDC (what the
// sender sent) at two sizes, and reports the loss against the inverse of the
// reference mid.
//
// Both rungs are carried as evidence. The headline value is the medium-size
// loss when it prices, otherwise the small-size loss. When no reverse path
// exists at either size the corridor reports NO-MARKET as a determined result:
// the exit was measured and found impossible, which is different from not
// being able to measure at all.
//
// Metrics qualify the headline and never move it: this result is reported in
// findings.metrics and never changes the corridor's verdict, integrity or
// recommendation (ADR 002).
type ExitLiquidityMetric struct {
	// DEX is the Horizon client used to price the reverse rungs.
	DEX *dex.Client

	// Reference is the independent reference rate for the corridor's fiat
	// pair, already resolved and cross-checked by the caller. The inverse of
	// its mid prices the reverse direction.
	Reference refrate.Rate

	// ReferenceUnavailable is set by the caller when no reference rate could
	// be obtained at all. Non-empty means Reference is meaningless and must
	// not be read.
	ReferenceUnavailable string
}

// reverseRungSizes are the two sizes at which the reverse direction is priced:
// a small (dust) size that isolates the spread, and a medium size that
// exposes depth.
var reverseRungSizes = []decimal.Decimal{
	decimal.RequireFromString("0.1"),
	decimal.NewFromInt(10),
}

// Describe implements Metric.
func (ExitLiquidityMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "exit-liquidity.reverse-send-loss",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Venue: VenuePathfinding,
		Title: "Reverse strict-send loss: cost of converting the fiat token back to USDC",
		CanDetermine: "The loss at a small and a medium reverse size (fiat token " +
			"to USDC) against the inverse reference mid, with both rungs, the " +
			"inverse mid and the reference mid as evidence.",
		CannotDetermine: "Whether the recipient can actually redeem the token " +
			"for fiat through the anchor — that redemption step is off-chain and " +
			"not priced here. The reference cross-check must be scorable: an " +
			"unscorable benchmark is never treated as a valid inverse mid, and a " +
			"missing reference produces undetermined rather than a guessed loss.",
	}
}

// Run implements Metric.
//
// The reverse direction is the corridor's own direct pair (fiat token to USDC)
// with no substitution: pathfinding routes multi-hop when a direct book is
// thin. A rung that returns no path is simply unpriced; both rungs are
// reported regardless.
func (m ExitLiquidityMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if m.ReferenceUnavailable != "" {
		return MetricUndetermined(d, s, "reference rate unavailable: "+m.ReferenceUnavailable)
	}
	if !m.Reference.Scorable() {
		reason := "reference cross-check could not be scored: the two providers " +
			"disagreed too far apart to trust either"
		if m.Reference.Note != "" {
			reason += " (" + m.Reference.Note + ")"
		}
		return MetricUndetermined(d, s, reason)
	}
	if m.Reference.Mid.IsZero() || m.Reference.Mid.IsNegative() {
		return MetricUndetermined(d, s, "no positive reference mid available")
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to price the reverse rungs")
	}

	inverseMid := decimal.NewFromInt(1).Div(m.Reference.Mid)

	// Reverse direction: the fiat token the recipient holds (s.Receive) sent
	// back for USDC (s.Send). No stellar.toml substitution — the corridor's
	// own pair is what matters, and pathfinding routes multi-hop when a direct
	// book is thin.
	reverseSend := s.Receive
	reverseReceive := s.Send

	type rungResult struct {
		size      decimal.Decimal
		dest      decimal.Decimal
		effective decimal.Decimal
		loss      decimal.Decimal
		priced    bool
		reason    string
	}
	rungs := make([]rungResult, 0, len(reverseRungSizes))
	for _, size := range reverseRungSizes {
		r := rungResult{size: size}
		path, err := m.DEX.BestPath(ctx, reverseSend, size, reverseReceive)
		if err != nil || path == nil {
			r.priced = false
			if err != nil {
				r.reason = err.Error()
			} else {
				r.reason = "no path found"
			}
			rungs = append(rungs, r)
			continue
		}
		r.dest = path.DestAmount
		r.effective = path.DestAmount.Div(path.SourceAmount)
		loss := inverseMid.Sub(r.effective).Div(inverseMid).Mul(decimal.NewFromInt(100))
		if loss.IsNegative() {
			loss = decimal.Zero
		}
		r.loss = loss
		r.priced = true
		rungs = append(rungs, r)
	}

	// Headline value: the medium-size loss when it prices, otherwise the
	// small-size loss. Neither pricing is NO-MARKET.
	var (
		value  decimal.Decimal
		priced bool
	)
	for i := len(rungs) - 1; i >= 0; i-- { // medium first
		if rungs[i].priced {
			value = rungs[i].loss
			priced = true
			break
		}
	}

	refEvidence := Evidence{
		Source:     fmt.Sprintf("refrate %s via %s", m.Reference.Pair(), m.Reference.Source),
		Observed:   fmt.Sprintf("mid=%s, inverse_mid=%s, as_of=%s", m.Reference.Mid, inverseMid.StringFixed(6), m.Reference.AsOf.UTC().Format(time.RFC3339)),
		ObservedAt: at,
	}
	evidence := []Evidence{refEvidence}

	var rungsObserved []string
	for _, r := range rungs {
		obs := fmt.Sprintf("size=%s", r.size)
		if r.priced {
			obs += fmt.Sprintf(", receive=%s, effective_rate=%s, loss=%s%%", r.dest, r.effective.StringFixed(6), r.loss.StringFixed(4))
		} else {
			obs += ", unpriced: " + r.reason
		}
		rungsObserved = append(rungsObserved, obs)
		rungEvidence := Evidence{
			Source:     fmt.Sprintf("/paths/strict-send %s/%s", reverseSend.Code, reverseReceive.Code),
			Observed:   obs,
			ObservedAt: at,
		}
		evidence = append(evidence, rungEvidence)
	}

	if !priced {
		summary := fmt.Sprintf(
			"no reverse path from %s to %s at %s: the token cannot be exited (NO-MARKET); inverse reference mid %s (%s)",
			reverseSend.Code, reverseReceive.Code, rungsObserved[0], inverseMid.StringFixed(6), m.Reference.Source)
		return MetricValue(d, s, decimal.Zero, UnitPercent, summary, evidence...)
	}

	summary := fmt.Sprintf(
		"reverse exit loss at %s %s is %s%% of the inverse reference mid (%s via %s); %s",
		reverseRungSizes[len(reverseRungSizes)-1], reverseReceive.Code, value.StringFixed(4),
		m.Reference.Mid, m.Reference.Source, rungsObserved[0])
	if len(rungs) > 1 {
		summary += "; " + rungsObserved[1]
	}

	return MetricValue(d, s, value, UnitPercent, summary, evidence...)
}
