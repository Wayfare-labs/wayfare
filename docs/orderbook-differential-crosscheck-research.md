# Research: An Independent Cross-Check of Horizon's Order Book

**Status: Completed.** Investigated whether a second source (Stellar RPC ledger entries, or an independent indexer like StellarExpert) can confirm the direct order book that Horizon reports, as a differential check (issue #727).

**Headline Finding:**
1. **Direct full-book reconstruction solely via Stellar RPC is structurally infeasible without maintaining an external offer index.** Stellar RPC's `getLedgerEntries` requires an exact `LedgerKey` for every query. In the Stellar ledger state, an `OfferEntry` key requires both `sellerID` (AccountID) and `offerID` (int64). Ledger state contains no secondary index on `(selling_asset, buying_asset)`, and RPC provides no table scan or prefix query. Proving that an order book is empty (`NO-MARKET`) or discovering unindexed offers cannot be done via RPC without scanning the entire network's offer table.
2. **Differential verification of Horizon's reported offers via Stellar RPC is feasible and cheap.** Given the list of active offers that Horizon returns, a checker can construct the exact `LedgerKeyOffer` for each offer and query `getLedgerEntries` via Stellar RPC. This confirms bit-for-bit that the seller exists, the offer is currently live on-ledger, and the recorded amount and price ratio match ledger state at the current ledger sequence.
3. **An independent second-indexer cross-check (e.g. StellarExpert) is feasible for full book comparison.** StellarExpert maintains its own ingestion pipeline from Stellar Core ledgers and exposes order book health endpoints. Comparing Horizon's `/order_book` against StellarExpert's book allows automated detection of mid-price divergence, phantom liquidity, and one-sided order book anomalies.

---

## 1. Why this research was conducted

Every DEX book metric in Wayfare (`checks/metric_spread.go`, `checks/metric_depth.go`, `checks/metric_concentration.go`) trusts Horizon's `/order_book`. While Assay demonstrated that differential reads of account state via Stellar RPC are feasible (because `LedgerKeyAccount` requires only the account public key `G...`), order book verification is fundamentally different because offers cannot be looked up by trading pair on raw ledger state.

This investigation evaluates:
- What Stellar RPC can and cannot prove about DEX offers.
- The cost, latency, and data requirements of RPC verification.
- Whether secondary indexers (e.g. StellarExpert) provide a viable differential check.
- A functional prototype implementation.

---

## 2. Technical Evidence: Ledger Key Architecture

### The LedgerKeyOffer XDR Schema

In Stellar Core and Stellar RPC (Soroban RPC Protocol 20+), ledger entries are queried via `getLedgerEntries(keys: string[])` where each key is a base64-encoded `LedgerKey` XDR:

```xdr
enum LedgerEntryType
{
    ACCOUNT = 0,
    TRUSTLINE = 1,
    OFFER = 2,
    DATA = 3,
    CLAIMABLE_BALANCE = 4,
    LIQUIDITY_POOL = 5,
    ...
};

struct
{
    AccountID sellerID;
    int64 offerID;
} offer;
```

Notice the key fields:
- `sellerID`: The Stellar public key (`G...`) of the account that created the offer.
- `offerID`: The sequential 64-bit integer assigned by Stellar Core when the offer transaction was executed.

### What RPC Can Do
- If `(sellerID, offerID)` is known, `getLedgerEntries` returns the current `OfferEntry` containing:
  - `selling`: Asset being sold
  - `buying`: Asset being bought
  - `amount`: Remaining available balance
  - `price`: Exact rational price (numerator `n` / denominator `d`)
  - `flags`: Authorization flags (e.g., passive offer)
  - `lastModifiedLedgerSeq`: Ledger sequence where this offer was last updated.

### What RPC Cannot Do
- **Query by asset pair:** There is no RPC method `getOffers(selling, buying)`.
- **Scan keys:** `getLedgerEntries` requires exact keys; wildcard or prefix searches (e.g., `keys=["offer:*"]`) return JSON-RPC errors.
- **Prove absence of offers:** If Horizon reports `bids: []`, Stellar RPC cannot confirm that zero offers exist on the ledger without knowing all active offer IDs across all Stellar accounts.

---

## 3. Differential Cross-Check Architectures

| Approach | Can verify reported offers? | Can detect phantom liquidity? | Can prove empty book (NO-MARKET)? | Engineering & operational cost |
|:---|:---:|:---:|:---:|:---|
| **Approach 1: Stellar RPC Spot Verification** |  Yes |  Yes |  No | Cheap; 1 JSON-RPC call per top offer level |
| **Approach 2: Secondary Indexer (StellarExpert)** |  Yes |  Yes |  Yes | Zero infrastructure; read-only HTTP call |
| **Approach 3: Self-Hosted Captive Core / Galexie** |  Yes |  Yes |  Yes | High; continuous ledger ingestion (~$160+/mo + DB maintenance) |

### Approach 1: Stellar RPC Spot Verification
When Horizon answers `/order_book` or `/offers?selling_asset=...&buying_asset=...`:
1. Horizon provides the top bid and ask offers with their `seller` and `id`.
2. The differential verifier encodes `LedgerKeyOffer` for the best bid and best ask.
3. Verifier calls Stellar RPC `getLedgerEntries([key1, key2])`.
4. If the entry is missing from RPC `result.entries`: Horizon reported phantom or already-filled liquidity.
5. If the entry exists: the on-ledger `amount` and `price` are compared with Horizon's response.

### Approach 2: Secondary Indexer (StellarExpert)
StellarExpert maintains an independent indexer of Stellar Core ledgers:
1. Fetch primary order book health from Horizon (`/order_book`).
2. Fetch secondary order book health from StellarExpert (`/explorer/public/asset`).
3. Compute differential statistics: `BidDiffPct`, `AskDiffPct`, `MidDiffPct`, and `SpreadDiffPp`.
4. Evaluate verdict: `AGREE`, `DISAGREE` (when mid diverges > 2%), `PRIMARY_EMPTY`, or `SECONDARY_EMPTY`.

---

## 4. Prototype Implementation

A working implementation is provided in [`dex/crosscheck.go`](../dex/crosscheck.go) and verified by [`dex/crosscheck_test.go`](../dex/crosscheck_test.go):

1. **`CompareOrderBooks(primary, secondary, maxDivergence)`**:
   Computes price divergence and flags discrepancies:
   - Evaluates whether best bid, best ask, mid price, and bid-ask spread agree within a bounded tolerance (default 2.0%).
   - Surfaces state mismatches (such as one source reporting an active book while the other reports no market).
2. **`RPCOfferCheck.VerifyOfferOnLedger(ctx, seller, offerID, keyXDR)`**:
   Executes a differential check against Stellar RPC `getLedgerEntries`, asserting that the claimed offer actually exists in the current ledger state.

---

## 5. Conclusion and Recommendations

1. **Adopt Approach 1 + Approach 2 as a hybrid differential check:**
   - Use StellarExpert / secondary Horizon endpoint for book-level differential checks (`CompareOrderBooks`).
   - Use Stellar RPC `getLedgerEntries` for cryptographically grounded spot checks of specific large offers reported by Horizon.
2. **Do not attempt full order book reconstruction from Stellar RPC alone:**
   - Full reconstruction without an indexer requires full-ledger state scanning, which SDF explicitly does not support on Stellar RPC. The reasoning and architectural boundaries documented here prevent future maintainers from attempting an infeasible RPC-only book search.
