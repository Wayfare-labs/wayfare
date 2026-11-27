package checks

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/Wayfare-labs/wayfare/dex"
)

// AmmPoolReservesMetric measures the reserves of classic liquidity pools appearing
// on a corridor's route paths.
type AmmPoolReservesMetric struct {
	DEX     *dex.Client
	PoolIDs []string
}

// Describe implements Metric.
func (AmmPoolReservesMetric) Describe() Descriptor {
	return Descriptor{
		ID:    "amm.pool-reserves",
		Scope: ScopeCorridor,
		Cost:  CostExpensive,
		Venue: VenuePathfinding,
		Title: "Reserves of classic AMM liquidity pools on route paths",
		CanDetermine: "How many classic liquidity pools were read, with each pool's ID, fee and " +
			"per-asset reserves as evidence. Reserves are never summed across assets.",
		CannotDetermine: "Whether pool liquidity will be executed or bypassed by order-book " +
			"depth at settlement time, or pool behavior under large price slippage.",
	}
}

// poolURL is the URL the DEX client requests for a pool, for evidence.
func (m AmmPoolReservesMetric) poolURL(id string) string {
	base := m.DEX.HorizonURL
	if base == "" {
		base = dex.DefaultHorizonURL
	}
	return strings.TrimRight(base, "/") + "/liquidity_pools/" + url.PathEscape(id)
}

// Run implements Metric.
func (m AmmPoolReservesMetric) Run(ctx context.Context, s Subject) MetricResult {
	d := m.Describe()
	at := time.Now().UTC()

	if s.Send.Code == "" || s.Receive.Code == "" {
		return MetricUndetermined(d, s, "no send or receive asset specified")
	}
	if len(m.PoolIDs) == 0 {
		return MetricUndetermined(d, s, "path does not expose liquidity pool IDs")
	}
	if m.DEX == nil {
		return MetricUndetermined(d, s, "no DEX client available to fetch pool reserves")
	}

	var evidence []Evidence
	var summaries []string

	for _, poolID := range m.PoolIDs {
		pool, err := m.DEX.LiquidityPool(ctx, poolID)
		if err != nil {
			return MetricUndetermined(d, s, fmt.Sprintf("fetching liquidity pool %s: %v", poolID, err))
		}

		if len(pool.Reserves) != 2 {
			return MetricUndetermined(d, s, fmt.Sprintf("liquidity pool %s reports %d reserves, want 2", poolID, len(pool.Reserves)), evidence...)
		}
		var reserveStrs []string
		for _, r := range pool.Reserves {
			if _, err := decimal.NewFromString(r.Amount); err != nil || r.Asset == "" {
				return MetricUndetermined(d, s, fmt.Sprintf("liquidity pool %s has an unreadable reserve %q=%q", poolID, r.Asset, r.Amount), evidence...)
			}
			reserveStrs = append(reserveStrs, fmt.Sprintf("%s=%s", r.Asset, r.Amount))
		}

		obs := fmt.Sprintf("pool_id=%s fee_bp=%d reserves=[%s]", pool.ID, pool.FeeBP, strings.Join(reserveStrs, ", "))
		evidence = append(evidence, Evidence{
			Source:     m.poolURL(poolID),
			Observed:   obs,
			ObservedAt: at,
		})
		summaries = append(summaries, fmt.Sprintf("%s (fee_bp=%d, %s)", pool.ID, pool.FeeBP, strings.Join(reserveStrs, ", ")))
	}

	return MetricValue(d, s, decimal.NewFromInt(int64(len(m.PoolIDs))), UnitCount,
		fmt.Sprintf("%d classic AMM pool(s) on route: %s", len(m.PoolIDs), strings.Join(summaries, "; ")),
		evidence...)
}
