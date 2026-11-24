# Research: quoted versus realised path payments

**Status: preliminary, read-only.** This is an empirical check of the
strict-send quote against settled Horizon history. It does not submit a
transaction, and it does not claim that a trade leg is the same thing as the
sender's complete source-asset cost.

## Question and scope

Wayfare's published ladder asks Horizon what a fixed USDC amount would buy in
NGNC or NGNT. This note asks whether settled path-payment activity visible in
Horizon delivered a similar rate.

The assets are:

| Asset | Issuer |
|---|---|
| USDC | `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN` |
| NGNC | `GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6` |
| NGNT | `GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD` |

## Method

At `2026-11-23T14:14:29Z`, I fetched 200-row, descending Horizon `/trades`
responses for `NGNC/USDC` and `NGNT/USDC`, then fetched the linked operation
for each row. A row qualified only when its linked operation was:

1. `transaction_successful: true`;
2. `type: path_payment_strict_send`; and
3. whose operation-level destination asset was the target token, rather than
   the token merely appearing as an intermediate `path` hop.

The realised rate is the settled `base_amount / counter_amount` from the
target/USDC trade leg. This is deliberately labelled a **USDC trade-leg rate**:
the linked path payment can have another source asset and can route through
USDC. It is not silently presented as the complete source-to-destination
rate.

For the quote, I fetched fresh `/paths/strict-send` responses at 0.1, 1 and 5
USDC for each token. The comparison uses the first (best-ordered) response
record at the nearest rung by absolute USDC size. The quote and trade times
are not identical: the fresh quote was collected on November 23, while the
qualifying NGNC executions are from November 22. A quote is therefore a
nearest recorded comparator, not a contemporaneous guarantee.

## Results

### NGNC

| Trade date (UTC) | Realised USDC leg | Realised NGNC | Realised rate (NGNC/USDC) | Quote rung | Quoted rate | Difference `(realised / quoted - 1)` |
|---|---:|---:|---:|---:|---:|---:|
| 2026-11-22 18:20:46 | 0.1104605 | 179.2332074 | 1,622.6000 | 0.1 | 1,591.9585 | **+1.92%** |
| 2026-11-22 18:21:01 | 0.0884069 | 142.5336826 | 1,612.2461 | 0.1 | 1,591.9585 | **+1.27%** |

The two observations are one small-size bucket. They are modestly above the
fresh 0.1-USDC quote, but the sample is too small and too close in time to
support a systematic conclusion.

### NGNT

No qualifying terminal NGNT `path_payment_strict_send` operation appeared in
the captured 200-row `NGNT/USDC` trade window. The report therefore has no
realised NGNT row and makes no NGNT rate claim. The quote-only reference rates
were 13,801.1844, 13,569.8726 and 12,655.6361 NGNT/USDC at 0.1, 1 and 5 USDC
respectively.

## Committed response snapshots

The following compact JSON blocks are the exact fields used in the table. The
full 200-row response bodies were hashed at capture time; the hashes make a
later replacement detectable, while the compact records below make the
published arithmetic independently inspectable from this document.

### NGNC settled operations and trade rows

```json
[
  {
    "operation_id": "281823384458223617",
    "type": "path_payment_strict_send",
    "transaction_successful": true,
    "created_at": "2026-11-22T18:20:46Z",
    "source_amount": "0.8650563",
    "destination_amount": "179.2332074",
    "destination_asset": "NGNC:GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
    "path": ["USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"],
    "trade_base_amount": "179.2332074",
    "trade_counter_amount": "0.1104605",
    "transaction_hash": "c97fd7dbc68cdcde485cff94e3304c015146f3c45cdb83edc2e13dd579101a03"
  },
  {
    "operation_id": "281823397343674369",
    "type": "path_payment_strict_send",
    "transaction_successful": true,
    "created_at": "2026-11-22T18:21:01Z",
    "source_amount": "0.0813991",
    "destination_amount": "142.5336826",
    "destination_asset": "NGNC:GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6",
    "path": ["USDC:GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"],
    "trade_base_amount": "142.5336826",
    "trade_counter_amount": "0.0884069",
    "transaction_hash": "8254f11e76caa00e260752ee7b74761df03c85839128c9d31f238bdd1411356a"
  }
]
```

### Fresh quote records

```json
{
  "NGNC": {
    "0.1": "159.1958525",
    "1": "1576.6426001",
    "5": "7626.7534120"
  },
  "NGNT": {
    "0.1": "1380.1184419",
    "1": "13569.8726458",
    "5": "63278.1806033"
  }
}
```

The NGNC 0.1 quote rate is `159.1958525 / 0.1 = 1,591.9585`.

### Query register and response hashes

All URLs below are public Horizon endpoints. `sha256` is the response-body
hash captured locally.

| Purpose | Query | SHA-256 |
|---|---|---|
| NGNC trades | `/trades?base_asset_type=credit_alphanum4&base_asset_code=NGNC&base_asset_issuer=GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6&counter_asset_type=credit_alphanum4&counter_asset_code=USDC&counter_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN&order=desc&limit=200` | `2ba78bbdb8b60821fee947d44217d58ec54a9d7d3fd4747797fa23a7d8500023` |
| NGNT trades | `/trades?base_asset_type=credit_alphanum4&base_asset_code=NGNT&base_asset_issuer=GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD&counter_asset_type=credit_alphanum4&counter_asset_code=USDC&counter_asset_issuer=GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN&order=desc&limit=200` | `c309f8589ecba79b5fa33d69382cd1cc3bb0fbecaf7b008bb68fd6d34e28a654` |
| NGNC quote 0.1 | `/paths/strict-send?...&source_amount=0.1&destination_assets=NGNC:<issuer>` | `76c30eaac9cd901d492caa59fdea5b95151f7ab7470c20a566a0a1e3f99ad23a` |
| NGNC quote 1 | `/paths/strict-send?...&source_amount=1&destination_assets=NGNC:<issuer>` | `6e2b643ff3fc7845b1d9b94d5a582aa4778e381e6bf37d936c798a058598f20c` |
| NGNC quote 5 | `/paths/strict-send?...&source_amount=5&destination_assets=NGNC:<issuer>` | `ea96c17db486e3dae59b07deb11a4b8bed130365ce3f29be54c2ec8c8f51aec4` |
| NGNT quote 0.1 | `/paths/strict-send?...&source_amount=0.1&destination_assets=NGNT:<issuer>` | `b6707fb6c3b5fcf4232daaee588f4b28de84b065f811e440cad3dd886ca6b5a2` |
| NGNT quote 1 | `/paths/strict-send?...&source_amount=1&destination_assets=NGNT:<issuer>` | `ff36b3efa921604d211e1a880fcaf65156cffaaaf19b0ab012b6f7a56e33b322` |
| NGNT quote 5 | `/paths/strict-send?...&source_amount=5&destination_assets=NGNT:<issuer>` | `b4b4d3d8579f7a332a844c88c46d7e7d10b7a075846b1a596b251e233c2c115d` |

In the quote rows, `source_assets` is the repository's USDC source encoding:
`source_asset_type=credit_alphanum4`, `source_asset_code=USDC`, and the USDC
issuer shown above. `<issuer>` expands to the target issuer in that row.

## What this shows

- Two settled NGNC terminal strict-send operations in this window had a USDC
  trade leg roughly 1.3–1.9% above the fresh 0.1-USDC quote.
- The captured NGNT window contained no qualifying terminal strict-send
  observation, so it cannot validate or falsify the NGNT quote.
- A `/trades` record must be joined to its operation and destination fields;
  merely seeing NGNC or NGNT in `path` would count intermediate liquidity, not
  a payment into the token.

## Limitations

- **Selection bias:** this is a 200-row descending page of visible trades, not
  a random sample of all settled payments. It over-represents what Horizon
  returned at capture time and may omit older or less active executions.
- **Time gap:** the NGNC executions are November 22; fresh quotes were read on
  November 23. The book can move between quote and submit, and the quote was
  not reserved for either transaction.
- **Trade-leg versus end-to-end rate:** the NGNC operations have native and
  EURC source amounts while the selected trade leg is USDC. Comparing the USDC
  leg to a USDC quote is useful for the venue leg, but it is not a complete
  wallet execution measurement.
- **No NGNT realised row:** absence in this bounded capture is not evidence
  that NGNT never settles; it is only an honest no-observation result for this
  query window.
- **Horizon history is public ledger history, not wallet intent.** It does not
  show a user's quoted submission time, private liquidity, failed attempts,
  or the quote a wallet displayed before submitting.

## Reproduction

Run the two `/trades` queries and the six `/paths/strict-send` queries above at
the same commit, save the raw response bodies, verify their SHA-256 values,
then apply the stated operation filter and nearest-size rule. No transaction
submission or signing is involved.
