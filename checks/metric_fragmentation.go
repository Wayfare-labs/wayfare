// FragmentationMetric measures whether a corridor's executable value at one
// trade size is spread across several paths or concentrated in one.
//
// Two corridors with the same best rate are not equally fragile. If one path
// delivers nearly all the value Horizon can find and every alternative
// delivers a fraction of it, losing that path — an offer pulled, a pool
// drained, a bridge asset delisted — moves the corridor's price at once. If
// several paths deliver close to the best, the corridor degrades instead of
// breaking. That difference is observable from a single strict-send response
// and needs no inference: it is the distribution of the destination amounts
// Horizon returned.
//
// # What a share is here
//
// Each path Horizon returns is an alternative way to send the *same* amount,
// so a path's share is its destination amount over the sum of every distinct
// path's destination amount. That is a share of the alternatives' outcomes,
// not a share of liquidity or of volume: the paths are not additive, and two
// of them may draw on the same offers or the same pool. The figure says how
// unevenly the alternatives Horizon found would pay, and nothing about who
// supplies them.
//
// # Over time
//
// The metric is computed per run and lands in the run record's metrics block
// (Version 2 and later) like any other metric, so a corridor's stored history
// is its series. runstore.FragmentationHistory reads it back; whether the
// value is concentrating is a comparison across runs, never something one
// response can say.
//
// This is a metric, not a check. No threshold exists, and none of its output
// can move a verdict or an integrity state.
package checks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// FragmentationMetricID is the identifier the path-concentration metric
// publishes under. Exported so a reader of stored findings can pick the
// figure out of a record without retyping the string.
const FragmentationMetricID = "fragmentation.paths"

// fragmentationRatioScale is the number of decimal places the HHI and the
// top-path share are rounded to. Fixed here rather than inherited from
// decimal.DivisionPrecision, which is a package-level variable any importer
// can change: a stored series must not shift because another package did.
const fragmentationRatioScale int32 = 16

// defaultFragmentationSize is the send amount the metric probes when none is
// configured. One size, fixed, because the figure is compared across runs:
// a series that probed 1 USDC one day and 5000 the next would be comparing
// two different questions. 100 is a size every recorded corridor ladder
// prices (see dex.DefaultSizes).
var defaultFragmentationSize = decimal.NewFromInt(100)

// ErrNoPathValues is returned by ConcentrationOfPaths when it is given no
// path values at all. No paths means no executable value to distribute, so
// the concentration is unknown — never zero, which would read as "perfectly
// dispersed", and never one, which would read as "one path".
var ErrNoPathValues = errors.New("checks: no path values to measure concentration over")

// PathConcentration is how executable value at one size is distributed
// across the distinct paths that priced it.
type PathConcentration struct {
	// PathCount is how many distinct paths the figure covers.
	PathCount int

	// Total is the sum of every path's destination amount. It is the
	// denominator of each share, carried so a reader can recompute them.
	Total decimal.Decimal

	// Top is the largest single destination amount among the paths — the
	// best path's value.
	Top decimal.Decimal

	// TopShare is Top / Total: the fraction of the alternatives' combined
	// value the best path alone delivers. 1 when there is one path; it
	// approaches 1/PathCount as the alternatives converge on the best.
	TopShare decimal.Decimal

	// HHI is the Herfindahl-Hirschman index over the shares:
	// Σ valueᵢ² / (Σ valueᵢ)². 1 is all value in one path; 1/n is n paths
	// paying exactly the same.
	HHI decimal.Decimal
}

// ConcentrationOfPaths computes the concentration of executable value across
// a set of path values, each the destination amount one distinct path
// delivers for the same send amount.
//
// It is pure: no I/O, no clock, so the same arithmetic can be applied to a
// live strict-send response, a recorded one, or — once the ladder retains its
// alternatives (issue #99) — the paths every rung already priced.
//
// Every value must be positive. A zero or negative destination amount is not
// a path that pays nothing; it is a corrupted figure, and folding it in would
// lower every other path's share by an amount nobody measured. An empty input
// returns ErrNoPathValues.
//
// The HHI is computed as one division of exact sums, Σv² over (Σv)², rather
// than by summing rounded shares, so the only rounding in the figure is the
// final one.
func ConcentrationOfPaths(values []decimal.Decimal) (PathConcentration, error) {
	if len(values) == 0 {
		return PathConcentration{}, ErrNoPathValues
	}

	total := decimal.Zero
	sumSquares := decimal.Zero
	top := values[0]
	for i, v := range values {
		if !v.IsPositive() {
			return PathConcentration{}, fmt.Errorf(
				"checks: path value %d is %s; a path that delivers a non-positive amount is a "+
					"corrupted figure, not a path that pays nothing", i, v)
		}
		total = total.Add(v)
		sumSquares = sumSquares.Add(v.Mul(v))
		if v.GreaterThan(top) {
			top = v
		}
	}

	return PathConcentration{
		PathCount: len(values),
		Total:     total,
		Top:       top,
		TopShare:  top.DivRound(total, fragmentationRatioScale),
		HHI:       sumSquares.DivRound(total.Mul(total), fragmentationRatioScale),
	}, nil
}

// FragmentationMetric measures the concentration of executable value across
// the distinct paths Horizon's strict-send pathfinding returns at one size.
type FragmentationMetric struct {
	DEX *dex.Client

	// Size is the send amount probed. Zero means defaultFragmentationSize.
	// Keep it fixed for a corridor: the stored series is only comparable
	// between runs that probed the same size, and the evidence source names
	// the size so a reader can tell.
	Size decimal.Decimal
}

// Describe implements Metric.
func (FragmentationMetric) Describe() Descriptor {
	return Descriptor{
		ID:    FragmentationMetricID,
		Scope: ScopeCorridor,
		Cost:  CostOneRequest,
		Venue: VenuePathfinding,
		Title: "Concentration of executable value across paths",
		CanDetermine: "At one send size, how unevenly the distinct paths Horizon's " +
			"strict-send pathfinding returned would pay: the Herfindahl-Hirschman " +
			"index of each path's destination amount as a share of their sum " +
			"(1 = all value in one path, 1/n = n paths paying the same), with the " +
			"path count and the best path's share in the evidence. Recorded per " +
			"run, so a corridor's history shows whether that value is " +
			"concentrating into fewer paths.",
		CannotDetermine: "Whether the paths are independent — alternatives can " +
			"draw on the same offers or the same pool, so a share is of the " +
			"alternatives' outcomes, not of liquidity or volume — anything about " +
			"paths Horizon did not return, and the distribution at any other size.",
	}
}

func (m FragmentationMetric) size() decimal.Decimal {
	if m.Size.IsZero() {
		return defaultFragmentationSize
	}
	return m.Size
}

// Run implements Metric.
func (m FragmentationMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	// DERIVATIVE is not short-circuited: its paths exist, they just route
	// through an intermediate, and how many of them there are is exactly the
	// fragility question. NO-MARKET is: the ladder found no path at any
	// size, so there is no value to distribute and asking again would only
	// re-confirm a structural fact.
	if s.Integrity == integrityNoMarket {
		return MetricUndetermined(d, s, fmt.Sprintf(
			"%s has no path to %s by construction (NO-MARKET): there is no executable "+
				"value to distribute across paths, so its concentration is unknown, not zero",
			s.Send.Code, s.Receive.Code))
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available")
	}

	size := m.size()
	if !size.IsPositive() {
		return MetricUndetermined(d, s, fmt.Sprintf(
			"probe size %s is not positive; there is no send amount to price", size))
	}

	evidence := Evidence{
		Source:     fmt.Sprintf("/paths/strict-send %s/%s source_amount=%s", s.Send.Code, s.Receive.Code, size),
		ObservedAt: at,
	}

	paths, err := m.DEX.StrictSendPaths(ctx, s.Send, size, s.Receive)
	if err != nil {
		return MetricUndetermined(d, s, fmt.Sprintf("pathfinding failed: %v", err))
	}
	if len(paths) == 0 {
		evidence.Observed = "no path returned"
		return MetricUndetermined(d, s, fmt.Sprintf(
			"Horizon returned no path from %s to %s at %s: there is no executable value "+
				"to distribute, so its concentration is unknown, not zero",
			s.Send.Code, s.Receive.Code, size), evidence)
	}

	values, duplicates, err := distinctPathValues(paths)
	if err != nil {
		evidence.Observed = fmt.Sprintf("%d path record(s) returned", len(paths))
		return MetricUndetermined(d, s, err.Error(), evidence)
	}

	c, err := ConcentrationOfPaths(values)
	if err != nil {
		evidence.Observed = fmt.Sprintf("%d distinct path(s) returned", len(values))
		return MetricUndetermined(d, s, err.Error(), evidence)
	}

	rendered := make([]string, len(values))
	for i, v := range values {
		rendered[i] = v.String()
	}
	evidence.Observed = fmt.Sprintf(
		"paths=%d, duplicates_dropped=%d, total_dest=%s, top_dest=%s, top_share=%s, hhi=%s, dest_amounts=[%s]",
		c.PathCount, duplicates, c.Total, c.Top, c.TopShare.StringFixed(6),
		c.HHI.StringFixed(6), strings.Join(rendered, ", "))

	summary := fmt.Sprintf(
		"path concentration HHI %s across %d path(s) at %s %s; the best path delivers %s%% of their combined value",
		c.HHI.StringFixed(4), c.PathCount, size, s.Send.Code,
		c.TopShare.Mul(decimal.NewFromInt(100)).StringFixed(1))

	return MetricValue(d, s, c.HHI, UnitRatio, summary, evidence)
}

// distinctPathValues reduces a strict-send response to one destination amount
// per distinct path, in the order Horizon returned them.
//
// A path is identified by its hops with their issuers, not by
// dex.Path.Describe: Describe renders codes only, and two hops with the same
// code from different issuers are different assets and different paths.
//
// The same path listed twice with the same amount is one path, and counting
// it twice would invent an alternative; duplicates is how many such repeats
// were dropped, so the evidence can say so. The same path listed twice with
// different amounts is a contradiction in the response, and choosing either
// figure would be a guess, so it is an error.
func distinctPathValues(paths []dex.Path) (values []decimal.Decimal, duplicates int, err error) {
	seen := make(map[string]decimal.Decimal, len(paths))
	values = make([]decimal.Decimal, 0, len(paths))
	for _, p := range paths {
		key := pathIdentity(p)
		if prior, ok := seen[key]; ok {
			if !prior.Equal(p.DestAmount) {
				return nil, 0, fmt.Errorf(
					"the same path (%s) is listed twice with different destination amounts, "+
						"%s and %s; choosing either would be a guess", p.Describe(), prior, p.DestAmount)
			}
			duplicates++
			continue
		}
		seen[key] = p.DestAmount
		values = append(values, p.DestAmount)
	}
	return values, duplicates, nil
}

// pathIdentity renders a path's hops by asset identity. A direct path has no
// hops and renders as the empty string, which is still a distinct key.
func pathIdentity(p dex.Path) string {
	parts := make([]string, len(p.Hops))
	for i, h := range p.Hops {
		if h.IsNative() {
			parts[i] = "native"
			continue
		}
		parts[i] = h.Code + ":" + h.Issuer
	}
	return strings.Join(parts, ">")
}
