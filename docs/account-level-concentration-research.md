# Research: can account-level concentration be measured at all?

**Status: completed.** One viable path exists (`/offers`), but at significant cost
and with important limitations. A negative finding on the primary endpoint
(`/order_book`) and a qualified positive on a secondary one.

---

## Why this matters

The current `ConcentrationMetric` (`checks/metric_concentration.go`) measures
HHI over **price levels**, not over **participants**. Two books with identical
level-count HHI can have very different risk profiles: one might have ten
equal-sized participants, the other one participant holding every level.
Account-level concentration answers "is this market dominated by a single
maker?" — a question the level-based HHI cannot touch.

The backlog entry (#71 / issue #163) asks whether the Stellar Horizon API
exposes the data needed to answer this question, and at what cost.

---

## What was investigated

### 1. Horizon `/order_book` — the endpoint Wayfare already uses

- **Endpoint:** `GET /order_book?selling_asset_type=...&buying_asset_type=...`
- **What it returns:** Aggregated price levels — `PriceLevel` objects containing
  only `Price` and `Amount` fields (confirmed via the Go SDK struct:
  `type PriceLevel struct { PriceR Price; Price string; Amount string }`).
- **Account identity:** Not exposed. The endpoint aggregates all offers at each
  price level into a single sum. The offering account is discarded by design.
- **Date checked:** 2026-08-28, against `horizon.stellar.org` mainnet and the
  Go SDK `github.com/stellar/go/protocols/horizon` package documentation.
- **Verdict: Cannot provide account-level concentration.** This is a deliberate
  design choice in Horizon, not a missing feature — the order book endpoint
  is an aggregation, not a listing.

### 2. Horizon `/offers` — the global offers endpoint

- **Endpoint:** `GET /offers?selling_asset_type=...&selling_asset_issuer=...&selling_asset_code=...&buying_asset_type=...&buying_asset_issuer=...&buying_asset_code=...`
- **What it returns:** Individual offer objects, each containing:
  - `id` — the offer ID
  - `account_id` — the Stellar account that created the offer
  - `selling` / `buying` — the asset pair
  - `amount` — the amount of the selling asset
  - `price` — the price ratio
  - `last_modified_ledger` / `last_modified_time`
- **Account identity:** Exposed. Each offer carries its creator's `account_id`.
- **Filtering:** Server-side filtering by selling and/or buying asset is supported
  via query parameters (`selling_asset_type`, `selling_asset_issuer`,
  `selling_asset_code`, and the corresponding `buying_*` fields). You must
  supply all three fields for each side (type + code + issuer).
- **Pagination:** Results are paginated (default 100 records per page). A market
  with thousands of offers requires multiple round trips.
- **Date checked:** 2026-08-28, against Horizon API documentation at
  `developers.stellar.org/docs/data/apis/horizon/api-reference/get-all-offers`.
- **Verdict: Can provide account-level concentration, with caveats** — see below.

### 3. Horizon `/accounts/{account_id}/offers` — per-account offers

- **Endpoint:** `GET /accounts/{account_id}/offers`
- **What it returns:** All open offers for a single account.
- **Use case:** Reverse lookup — given an account, find its offers. Not useful
  for concentration measurement (which starts from a pair, not an account).
- **Date checked:** 2026-08-28.
- **Verdict: Not applicable** to the concentration question.

### 4. Stellar RPC

- **Status:** Horizon is nearing end-of-life in favour of Stellar RPC and
  Portfolio APIs (noted in Horizon documentation as of 2026-08-28).
- **Order book query:** Stellar RPC does not currently expose a dedicated order
  book endpoint. Ledger entries can be queried, but aggregating offers into
  price levels or grouping by account would require client-side ledger
  inspection.
- **Date checked:** 2026-08-28, against `developers.stellar.org` documentation.
- **Verdict: Not currently viable** for account-level concentration. May change
  as the RPC API matures.

### 5. Stellar Expert API (third-party)

- **URL:** `https://stellar.expert/openapi`
- **What it provides:** Indexing and search over Stellar ledger data, including
  offers. May offer aggregation endpoints.
- **Cost:** Third-party service; terms and rate limits not audited.
- **Date checked:** 2026-08-28 (endpoint listed, not called).
- **Verdict: Outside scope.** The project uses only first-party Horizon data.
  A third-party dependency would introduce availability and trust assumptions
  the project does not currently carry.

---

## Cost analysis of the `/offers` path

To measure account-level concentration for one pair (e.g. XLM/NGNC):

1. **One request** to `/offers` with selling and buying asset filters, returning
   up to 100 offers per page.
2. **N additional requests** to paginate through all matching offers, where
   N = ⌈total_matching_offers / 100⌉ − 1.
3. **Client-side aggregation:** group offers by `account_id`, sum amounts per
   account, compute HHI over account shares.

For a thin market (tens of offers), this is 1–3 requests. For a market with
hundreds of offers, it is 5–10. The latency cost is O(paginated_requests),
compared to O(1) for the current `/order_book` call.

**AMM liquidity is excluded.** Neither `/order_book` nor `/offers` includes
automated market maker pools. AMMs are a significant source of liquidity on
Stellar (as documented in `dex/dex.go`), so any account-level concentration
figure would describe only the order-book portion of the market.

---

## Limitations

1. **Order-book offers only.** AMM pools are invisible to both endpoints. A
   market that appears concentrated in order-book offers may actually be
   dominated by AMM liquidity — the opposite conclusion from what the
   figure suggests.

2. **Pagination cost.** Thin markets are cheap; deep markets are expensive.
   A corridor with 500 offers requires 5 round trips, each returning 100
   records. This is not prohibitive but is materially more expensive than
   the current single `/order_book` call.

3. **Stale offers.** An offer remains in `/offers` until it is consumed or
   cancelled. An offer placed months ago at an uncompetitive price still
   counts as a "participant" — inflating the participant count and
   understating concentration. The `last_modified_ledger` field can filter
   stale offers, but choosing a threshold is a judgement call.

4. **Multiple offers per account.** A single market maker may place dozens of
   offers at different prices. Counting offers overstates fragmentation;
   summing amounts per account and computing HHI over those sums is the
   correct approach, but requires the full offer listing.

5. **Horizon end-of-life.** Horizon is nearing deprecation in favour of
   Stellar RPC. Any implementation depending on `/offers` would need a
   migration path.

---

## Recommendation

**Account-level concentration can be measured, but not cheaply and not fully.**

The `/offers` endpoint provides the data, but at a cost that scales with
market depth and with limitations that make the result a partial picture
(order-book only, excluding AMMs). The finding is:

- **Negative on `/order_book`:** confirmed, no account identity exposed.
- **Qualified positive on `/offers`:** viable for thin-to-medium markets,
  expensive for deep ones, and blind to AMM liquidity.

Whether this is worth implementing depends on whether the additional insight
(who is providing the liquidity) justifies the additional cost (multiple
paginated requests per measurement) and the partial coverage (order-book
only). For the project's current corridors — which are thin enough that
the offer count is low — the cost would be manageable. For deeper markets
on other corridors, it would not.

### What would need to change

If the project decided to proceed:

1. **A new metric** (e.g. `concentration.account`) that fetches `/offers`
   filtered by the corridor's asset pair, aggregates by `account_id`, and
   computes HHI over account-level amount shares.
2. **A `CostExpensive` classification** reflecting the paginated requests.
3. **A `CannotDetermine` caveat** stating that AMM liquidity is excluded.
4. **A staleness filter** on `last_modified_ledger` to exclude abandoned
   offers, with a documented threshold.

If the project decided not to proceed, the current level-based HHI should
remain documented as what it is: a measure of price-level distribution,
not participant concentration.

---

## Related

- Issue [#163](https://github.com/Wayfare-labs/wayfare/issues/163) — this
  research task
- Issue [#162](https://github.com/Wayfare-labs/wayfare/issues/162) — the
  concentration metric's current limitation (levels, not participants)
- `checks/metric_concentration.go` — the current implementation
- `dex/health.go` — the `wireOrderBook` struct confirming `/order_book` fields
- `dex/dex.go` — documentation of AMM vs order-book pricing discrepancy
