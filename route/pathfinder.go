package route

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/asset"
	"github.com/Wayfare-labs/wayfare/dex"
)

// Pathfinder is the pathfinding contract the engine prices against.
//
// It exists so route depends on what it calls rather than on the Horizon
// client that currently answers: Horizon has no successor for pathfinding,
// and keeping the current implementation behind an interface lets a
// replacement be slotted in without touching the engine (see
// docs/horizon-rpc-migration.md, Option E).
//
// # The contract is larger than the signatures
//
// StrictSendPaths must return the complete set of paths, not only the best
// one. classify decides DIRECT / DERIVATIVE / NO-MARKET from every path, so a
// "best path only" implementation would compile and silently change corridor
// integrity verdicts. It must also include liquidity reached through AMM
// pools: Stellar settles path payments against offers and pools together, and
// order-book-only pricing was measured wrong on USDC/NGNC on 2026-08-04 (see
// the dex package comment). Those requirements cannot be expressed in Go's
// type system, so they are verified by the pathfinder contract tests rather
// than assumed from a successful compile.
//
// MeasureSlippage is part of the same contract because it is two pathfinding
// calls with a probe: a replacement must support the size-dependent pricing
// the engine warns about.
//
// Nothing else belongs here. OrderBook is a market-health diagnostic for
// checks, a separate consumer, and ChainHead is a freshness lookup for the
// server; neither is needed to price a corridor, so neither widens the
// surface a replacement must reproduce.
type Pathfinder interface {
	StrictSendPaths(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset) ([]dex.Path, error)
	MeasureSlippage(ctx context.Context, source asset.Asset, amount decimal.Decimal, dest asset.Asset, probe decimal.Decimal) (*dex.Slippage, error)
}

// The Horizon client is the only implementation today. This assertion makes
// that a build-time fact: a change to either method on dex.Client fails here
// before it can drift from the seam route depends on.
var _ Pathfinder = (*dex.Client)(nil)
