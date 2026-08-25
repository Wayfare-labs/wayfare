// Package refrate supplies an independent reference mid-market rate.
//
// This package exists because of a measurement, not a feature request. On
// 2026-08-04 the best on-chain route for USDC to NGNC returned roughly 651
// NGN per USD while the real-world rate sat near 1,500. Presented on its own,
// "651" looks like a perfectly reasonable number. It is only recognisable as
// a catastrophic loss when compared against an outside source.
//
// So the reference rate is not decoration on a quote. It is the only thing
// standing between a user and a route that silently destroys half their
// money, and the routing engine treats a missing reference rate as a hard
// failure rather than degrading to "no spread shown".
package refrate

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Rate is a mid-market rate for a currency pair at a point in time.
//
// Mid-market means no spread: the midpoint between what buyers pay and
// sellers accept. It is the honest benchmark to price a route against,
// because it is the rate nobody actually gets — every real route sits some
// distance from it, and that distance is the true cost.
type Rate struct {
	Base   string          // ISO-4217, e.g. "USD"
	Quote  string          // ISO-4217, e.g. "NGN"
	Mid    decimal.Decimal // units of Quote per one unit of Base
	AsOf   time.Time
	Source string // provider identity, surfaced so users can audit it

	// FetchedAt is when this project last obtained the rate from the
	// provider, which is a different question from AsOf: AsOf is when the
	// upstream says the rate was set, FetchedAt is when we asked. A cached
	// rate has an older FetchedAt and an unchanged AsOf, and a reader needs
	// both to judge whether a figure is current.
	FetchedAt time.Time

	// Cross-check fields, populated by Cross when a second provider
	// answered. Mid and Source above always name the rate that was scored
	// against; these name the other one.
	//
	// Both are carried even when the providers agree. A corridor's figures
	// moving because the benchmark changed is a different event from the
	// corridor moving, and a record keeping only the mid it scored against
	// cannot distinguish them afterwards.
	SecondaryMid    decimal.Decimal
	SecondarySource string
	SecondaryAsOf   time.Time

	// DivergencePct is how far apart the two mids are, relative to the
	// smaller, so the figure does not depend on which is primary.
	DivergencePct decimal.Decimal

	// Agreement records how the two providers related. The zero value,
	// AgreementSingle, is correct for a rate that was never cross-checked.
	Agreement Agreement

	// Note explains a non-trivial Agreement in prose, for surfaces that
	// show a reason rather than a state.
	Note string
}

// Scorable reports whether a verdict may be derived from this rate.
//
// False means the two providers disagreed so far apart that one of them is
// broken rather than merely different. Scoring against either would produce a
// figure whose value depends on which feed was believed, which is not a
// measurement — see the threshold rationale in cross.go.
func (r Rate) Scorable() bool { return r.Agreement != AgreementMalfunction }

// Pair renders the currency pair, e.g. "USD/NGN".
func (r Rate) Pair() string { return r.Base + "/" + r.Quote }

// Age reports how stale the rate is relative to now.
func (r Rate) Age() time.Duration { return time.Since(r.AsOf) }

// Provider retrieves a reference rate for a pair.
//
// Implementations must return an error rather than a zero or guessed rate
// when they cannot answer. A wrong reference rate is worse than none: it
// would validate a bad route instead of flagging it.
type Provider interface {
	Rate(ctx context.Context, base, quote string) (Rate, error)
	Name() string
}

// ErrNoRate reports that a provider has no rate for the requested pair.
type ErrNoRate struct {
	Base, Quote, Source string
}

func (e *ErrNoRate) Error() string {
	return fmt.Sprintf("refrate: %s has no rate for %s/%s", e.Source, e.Base, e.Quote)
}

// ErrRateLimited reports that a provider refused because the caller has spent
// its quota.
//
// It is distinct from a generic failure because the remedy is different and
// the caller can act on it: a rate limit says "ask again later", where a
// decode failure says "this provider is broken". Collapsing the two would hide
// the single most likely way a free-tier feed fails under a schedule.
type ErrRateLimited struct {
	Source     string
	RetryAfter time.Duration
	Message    string
}

func (e *ErrRateLimited) Error() string {
	msg := fmt.Sprintf("refrate: %s rate-limited this request", e.Source)
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf("; retry after %s", e.RetryAfter)
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// Static is a Provider backed by hardcoded rates.
//
// Its purpose is tests and offline operation, and it is deliberately awkward
// to use in production: MaxAge is not applied to it, and callers are expected
// to surface its name so a stale hardcoded rate can never masquerade as live
// market data.
type Static struct {
	Rates map[string]decimal.Decimal // keyed "USD/NGN"
	Stamp time.Time
}

// NewStatic builds a Static provider from pair strings to rates.
func NewStatic(rates map[string]decimal.Decimal) *Static {
	return &Static{Rates: rates, Stamp: time.Now()}
}

// Name identifies the provider.
func (s *Static) Name() string { return "static" }

// Rate implements Provider.
func (s *Static) Rate(_ context.Context, base, quote string) (Rate, error) {
	key := base + "/" + quote
	v, ok := s.Rates[key]
	if !ok {
		return Rate{}, &ErrNoRate{Base: base, Quote: quote, Source: s.Name()}
	}
	stamp := s.Stamp
	if stamp.IsZero() {
		stamp = time.Now()
	}
	return Rate{
		Base:   base,
		Quote:  quote,
		Mid:    v,
		AsOf:   stamp,
		Source: s.Name(),
	}, nil
}

// Checked wraps a Provider and rejects rates older than MaxAge.
//
// FX moves, and a rate from last week would quietly turn the spread
// calculation into fiction. Wrapping is preferred over each provider policing
// itself so the staleness policy lives in one auditable place.
type Checked struct {
	Inner  Provider
	MaxAge time.Duration
}

// Name identifies the wrapped provider.
func (c *Checked) Name() string { return c.Inner.Name() }

// Rate implements Provider, rejecting stale results.
func (c *Checked) Rate(ctx context.Context, base, quote string) (Rate, error) {
	r, err := c.Inner.Rate(ctx, base, quote)
	if err != nil {
		return Rate{}, err
	}
	if c.MaxAge > 0 && r.Age() > c.MaxAge {
		return Rate{}, fmt.Errorf(
			"refrate: %s rate for %s is %s old, exceeds max age %s",
			r.Source, r.Pair(), r.Age().Round(time.Second), c.MaxAge)
	}
	return r, nil
}
