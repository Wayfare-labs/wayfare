package refrate

import (
	"context"

	"github.com/shopspring/decimal"
)

// ParallelStatus records whether a parallel/street-market mid could be
// reported at all.
//
// The zero value is ParallelUnavailable, which is the correct default: a
// corridor that was never given a parallel-rate source has no defensible
// parallel mid, and must say so rather than imply one exists.
type ParallelStatus int

const (
	// ParallelUnavailable means no parallel mid could be reported — either no
	// source is configured, the source failed, or the number it returned
	// cannot be defended. Reason on the Parallel value explains which.
	ParallelUnavailable ParallelStatus = iota

	// ParallelReported means a parallel source answered with a mid the project
	// is willing to publish, and GapPct is meaningful.
	ParallelReported
)

func (s ParallelStatus) String() string {
	if s == ParallelReported {
		return "REPORTED"
	}
	return "UNABLE-TO-DETERMINE"
}

// Parallel is a parallel/street-market reference reported alongside the
// official mid — never blended into it, never replacing it.
//
// The official rate and the parallel rate answer two different questions: a
// user converting through a bank cares about the official rate, and a user
// converting on the street cares about the parallel one. Averaging the two
// into a single number would destroy exactly the information the reader needs,
// so the two are carried and reported separately, and the gap between them is
// derived rather than hidden.
type Parallel struct {
	// Status is REPORTED only when a defensible parallel mid was obtained.
	Status ParallelStatus

	// Mid is the parallel mid-market rate, in units of Quote per one unit of
	// Base, matching the official Rate. Zero when Status is
	// ParallelUnavailable.
	Mid decimal.Decimal

	// Source names the parallel provider, so a reader can audit it.
	Source string

	// GapPct is how far the parallel mid sits from the official mid, as a
	// signed percentage of the official mid: positive when the parallel rate
	// quotes more units of Quote per Base than the official rate does — the
	// usual direction when a currency's street value is weaker than its
	// official one. It is derived, not blended: both mids remain reportable on
	// their own.
	GapPct decimal.Decimal

	// Reason explains a ParallelUnavailable status in prose, for surfaces that
	// show why a field is absent rather than a bare state.
	Reason string
}

// Reported reports whether a parallel mid may be shown to a reader.
func (p Parallel) Reported() bool { return p.Status == ParallelReported }

// ParallelAgainst obtains a parallel mid from p for the same pair as official
// and measures the gap between the two.
//
// It never fabricates a number. A nil provider, a provider that fails, or a
// zero mid on either side all yield an UNABLE-TO-DETERMINE result carrying the
// reason — because the project cannot publish a parallel figure it cannot
// defend, and a plausible-looking guess is worse than an honest absence.
//
// The official rate is passed in rather than re-fetched so the gap is measured
// against the exact mid a verdict was scored on, not a second reading of it.
func ParallelAgainst(ctx context.Context, p Provider, official Rate) Parallel {
	if p == nil {
		return Parallel{
			Status: ParallelUnavailable,
			Reason: "no parallel-rate source configured",
		}
	}

	pr, err := p.Rate(ctx, official.Base, official.Quote)
	if err != nil {
		return Parallel{
			Status: ParallelUnavailable,
			Source: p.Name(),
			Reason: err.Error(),
		}
	}

	// A zero mid on either side is a broken feed, not a 100% gap: dividing by
	// it would manufacture a number, which is the one thing this must not do.
	if pr.Mid.IsZero() || official.Mid.IsZero() {
		return Parallel{
			Status: ParallelUnavailable,
			Source: pr.Source,
			Reason: "a zero mid cannot be scored",
		}
	}

	return Parallel{
		Status: ParallelReported,
		Mid:    pr.Mid,
		Source: pr.Source,
		GapPct: pr.Mid.Sub(official.Mid).Div(official.Mid).Mul(decimal.NewFromInt(100)),
	}
}
