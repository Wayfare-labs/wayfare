# Corridor measurements: the LINK.IO African-fiat asset set

Live mainnet measurements of USDC → NGNC, GHSC and KESC across trade size.
All three tokens are issued from a single account,
`GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6`, by LINK.IO LTD.

The [NGNC ladder](#run-of-2026-08-08) came first and established the method;
the [sister-corridor sweep](#sister-corridor-sweep-2026-08-08) extends it to
the rest of the issuer's set.

All figures are raw output from `cmd/ladder`, which prices each size through
the same `route.Engine` the product uses. Nothing here is rounded in a
direction that improves the result; percentages are truncated to two decimals
as reported.

---

## Run of 2026-08-08

- **Measured at:** 2026-08-08T12:53:56Z
- **Pricing source:** Horizon `/paths/strict-send` (mainnet), which accounts for
  both orderbook offers and liquidity pools
- **Reference mid:** 1364.0070 USD/NGN via exchangerate-api, as of
  2026-08-08T00:02:31Z
- **Send asset:** USDC (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`)
- **Receive asset:** NGNC (`GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6`)

| Send (USDC) | Receive (NGNC) | Effective rate (NGN/USD) | Loss vs mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | 102.78 | 1027.84 | 24.65% | UNUSABLE | USDC → NGNC |
| 1 | 1017.03 | 1017.03 | 25.44% | UNUSABLE | USDC → LSP → XLM → NGNC |
| 5 | 4956.57 | 991.31 | 27.32% | UNUSABLE | USDC → XLM → NGNC |
| 10 | 9621.33 | 962.13 | 29.46% | UNUSABLE | USDC → XLM → NGNC |
| 25 | 22101.59 | 884.06 | 35.19% | UNUSABLE | USDC → XLM → NGNC |
| 50 | 38937.39 | 778.75 | 42.91% | UNUSABLE | USDC → XLM → NGNC |
| 100 | 62890.83 | 628.91 | 53.89% | UNUSABLE | USDC → XLM → NGNC |
| 250 | 99685.40 | 398.74 | 70.77% | UNUSABLE | USDC → XLM → NGNC |
| 500 | 123835.60 | 247.67 | 81.84% | UNUSABLE | USDC → XLM → NGNC |
| 1000 | 140903.54 | 140.90 | 89.67% | UNUSABLE | USDC → XLM → NGNC |
| 2500 | 153606.23 | 61.44 | 95.50% | UNUSABLE | USDC → XLM → NGNC |
| 5000 | 158365.19 | 31.67 | 97.68% | UNUSABLE | USDC → XLM → NGNC |

Verdict thresholds are Good ≤3%, Fair ≤8%, Poor ≤20%, Unusable >20%.
**All twelve sizes are Unusable.** The engine declined to recommend a route at
every size tested.

---

## What the curve shows

### 1. There is a structural floor of roughly 24.65%, before any slippage

At 0.1 USDC the trade is small enough that price impact is negligible — it
routes direct, `USDC → NGNC`, with no bridge hop. It still loses 24.65%
against mid.

That floor is not liquidity. It is the price at which NGNC trades against its
own reference, and it already exceeds the 20% Unusable threshold on its own.
**No trade size can be acceptable, because the corridor's zero-size limit is
already unacceptable.**

### 2. Slippage stacks on top of the floor, and dominates quickly

From 24.65% at dust size, loss climbs monotonically to 97.68% at 5000 USDC.
There is no local minimum, no plateau, and no band where the curve dips back
toward usable. Every step up in size is strictly worse than the one below it.

### 3. Liquidity is exhausted around 160,000 NGNC

The receive amount asymptotes hard. Marginal return on each additional USDC
sent:

| Size step | Extra USDC sent | Extra NGNC received | Marginal rate (NGN/USD) |
|:---|---:|---:|---:|
| 500 → 1000 | 500 | 17,067.94 | 34.14 |
| 1000 → 2500 | 1500 | 12,702.69 | 8.47 |
| 2500 → 5000 | 2500 | 4,758.96 | 1.90 |

At the top of the ladder, an additional dollar buys 1.90 naira against a mid
of 1364. The pool is effectively empty; a sender putting in 5000 USDC receives
only 12.4% more naira than one putting in 1000.

### 4. The benchmark is the charitable one

The reference is the official/interbank USD/NGN rate. If the rate Nigerians
actually transact at is weaker than official (more naira per dollar), then
every loss figure in this document **understates** the true cost, because the
same NGNC output would be measured against a larger fair value. Choosing the
official rate does not flatter the corridor — it is the most generous
benchmark available, and the corridor fails against it at every size.

This does mean the absolute loss percentages carry the reference provider's
accuracy as a dependency. The ordering, the monotonicity, and the marginal-rate
collapse do not — those are properties of the on-chain data alone.

For what "fair value" means here — which rate the NGN providers actually track
(official/interbank, not the parallel/street rate), the evidence for it, and
why the official mid is the charitable benchmark — see
[docs/fair-value-ngn.md](fair-value-ngn.md).

---

## Comparison with the 2026-08-04 run

The measurement recorded in the `route` package doc, taken 2026-08-04:

| | 2026-08-04 | 2026-08-08 |
|:---|---:|---:|
| 100 USDC receives | 65,150.86 NGNC | 62,890.83 NGNC |
| Effective rate | 651.51 | 628.91 |
| Reference mid | 1364.77 | 1364.0070 |
| Loss | 52.3% | 53.89% |

Four days apart, the corridor is stable and slightly worse. This is not a
transient outage or a single bad snapshot.

---

## Sister-corridor sweep, 2026-08-08

The NGNC issuer also issues GHSC (Ghanaian cedi) and KESC (Kenyan shilling)
from the same account. This sweep prices all three in one window to test
whether the NGNC finding is asset-specific or issuer-wide.

- **Measured at:** 2026-08-08T14:26:33Z – 14:27:30Z (all three within 60s)
- **Pricing source:** Horizon `/paths/strict-send` (mainnet)
- **Reference source:** exchangerate-api, rates as of 2026-08-08T00:02:31Z

### Issuer's own declared status

Read live from `https://ngnc.online/.well-known/stellar.toml` on 2026-08-08:

| Asset | `status` | `anchor_asset` | In service per SEP-1? |
|:---|:---|:---|:---|
| NGNC | `live` | `NGN` | yes |
| GHSC | `pending` | `GHS` | **no** |
| KESC | `pending` | `KESC` | **no** |

Two defects in the published document, both reported here as read:

1. KESC sets `anchor_asset="KESC"`, naming its own token rather than the
   ISO-4217 code `KES` that SEP-1 intends.
2. The document is not valid TOML — a stray `s` follows the quoted KESC
   `image` URL. A conforming parser rejects the whole file, which is why
   `anchor/salvage.go` exists.

### USDC → GHSC

Reference mid: 11.7625 USD/GHS.

| Send (USDC) | Receive (GHSC) | Effective rate (GHS/USD) | Loss vs mid | Verdict | Best path |
|---:|---:|---:|---:|:---|:---|
| 0.1 | 0.30 | 3.04 | 74.14% | UNUSABLE | USDC → NGNC → GHSC |
| 1 | 3.00 | 3.00 | 74.48% | UNUSABLE | USDC → AQUA → NGNC → GHSC |
| 5 | 14.40 | 2.88 | 75.51% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 10 | 27.57 | 2.76 | 76.56% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 25 | 61.07 | 2.44 | 79.23% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 50 | 102.62 | 2.05 | 82.55% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 100 | 155.56 | 1.56 | 86.77% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 250 | 225.28 | 0.90 | 92.34% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 500 | 264.85 | 0.53 | 95.50% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 1000 | 290.35 | 0.29 | 97.53% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 2500 | 308.16 | 0.12 | 98.95% | UNUSABLE | USDC → XLM → NGNC → GHSC |
| 5000 | 314.59 | 0.06 | 99.47% | UNUSABLE | USDC → XLM → NGNC → GHSC |

**Every GHSC path routes through NGNC.** There is no independent USDC → GHSC
market; the corridor is a second hop bolted onto the broken naira one, so it
inherits NGNC's floor and adds its own on top. That is the mechanism behind
the 74.14% floor — roughly NGNC's 25.02% compounded with a further ~65% on the
NGNC → GHSC leg.

### USDC → KESC

Reference mid: 129.4263 USD/KES.

| Send (USDC) | Result |
|---:|:---|
| 0.1 – 5000 | **NO ROUTE** at every size tested |

Horizon returns zero paths from USDC to KESC at every size from 0.1 to 5000.
The token exists on the ledger and its issuer publishes it in stellar.toml,
but no sequence of orderbooks or liquidity pools connects it to USDC. It is
not a bad price; it is the absence of a market.

### USDC → NGNC, same window

Re-measured at 14:27:30Z for a same-window comparison. Matches the 12:53 run
to within normal drift — 0.1 USDC moved from 24.65% to 25.02% loss, 5000 USDC
unchanged at 97.68%.

| Send (USDC) | Receive (NGNC) | Effective rate (NGN/USD) | Loss vs mid | Verdict |
|---:|---:|---:|---:|:---|
| 0.1 | 102.27 | 1022.70 | 25.02% | UNUSABLE |
| 1 | 1011.77 | 1011.77 | 25.82% | UNUSABLE |
| 5 | 4922.86 | 984.57 | 27.82% | UNUSABLE |
| 10 | 9551.52 | 955.15 | 29.97% | UNUSABLE |
| 25 | 21943.08 | 877.72 | 35.65% | UNUSABLE |
| 50 | 38685.17 | 773.70 | 43.28% | UNUSABLE |
| 100 | 62566.54 | 625.67 | 54.13% | UNUSABLE |
| 250 | 99374.37 | 397.50 | 70.86% | UNUSABLE |
| 500 | 123615.29 | 247.23 | 81.87% | UNUSABLE |
| 1000 | 140786.71 | 140.79 | 89.68% | UNUSABLE |
| 2500 | 153591.41 | 61.44 | 95.50% | UNUSABLE |
| 5000 | 158395.64 | 31.68 | 97.68% | UNUSABLE |

---

## What the sweep shows

### Three distinct failure modes, which a single grade cannot express

The three corridors do not fail in the same way. They fail in three different
ways, and the distinction is the substantive result of this sweep:

| Mode | Corridor | Observation |
|:---|:---|:---|
| **Live, value-destroying** | USDC → NGNC | Issuer declares `status="live"`. A market exists and prices continuously. It loses 25.02% at 0.1 USDC and 97.68% at 5000. |
| **Derivative** | USDC → GHSC | No independent market. Every path at every size traverses NGNC (`USDC → XLM → NGNC → GHSC`), so the corridor carries NGNC's loss plus its own. |
| **No-market** | USDC → KESC | Horizon returns zero paths at every size tested. Not a poor price — no price. |

A loss percentage describes only the first mode. The second is a dependency
fact: GHSC's integrity is bounded above by NGNC's, so any statement about GHSC
in isolation is incomplete. The third has no loss percentage at all, because
there is no execution to measure.

Reporting all three as "Unusable" would be accurate and would discard the
reason each is unusable. The monitor therefore carries an integrity state
alongside the loss grade.

### The pattern holds across the issuer's whole set

| Corridor | Issuer status | Best result, any size | Structural floor | Verdict |
|:---|:---|:---|---:|:---|
| USDC → NGNC | live | 25.02% loss at 0.1 USDC | ~25% | Unusable at every size |
| USDC → GHSC | pending | 74.14% loss at 0.1 USDC | ~74% | Unusable at every size |
| USDC → KESC | pending | no route at any size | — | No market |

Not one of the twenty-four priced points across three corridors reached
Poor, let alone Fair or Good. The engine recommended nothing, anywhere.

### Total depth across all three corridors is roughly $143

Taking the largest receive amount observed on each corridor and valuing it at
the reference mid:

| Corridor | Max receive (5000 USDC in) | At mid | Value |
|:---|---:|---:|---:|
| NGNC | 158,395.64 NGNC | 1364.0070 | $116.13 |
| GHSC | 314.59 GHSC | 11.7625 | $26.75 |
| KESC | 0 | 129.4263 | $0.00 |
| **Total** | | | **$142.88** |

Across all three African-fiat tokens from this issuer, total on-chain
liquidity reachable from USDC is on the order of one hundred and forty
dollars. For context, the issuer's stellar.toml describes the organisation as
building cross-border payments infrastructure "for the next billion
Africans"; the measurement above is of on-chain DEX liquidity only, and says
nothing about volume settled through the issuer's own SEP-24 rails, which are
not machine-priceable and therefore not measurable here.

### On-chain reality agrees with the issuer's own declaration

This is the sweep's most useful methodological result. The issuer marks GHSC
and KESC `pending`, and the ledger independently corroborates it: GHSC has no
market of its own and is reachable only through NGNC, and KESC has no market
at all. Two independent sources — a SEP-1 document the issuer publishes, and
Horizon pathfinding over the ledger — agree.

The corollary is the uncomfortable one. NGNC is declared `live`, and it still
loses 25% at dust size and 97.68% at 5000 USDC. The asset its issuer considers
in service is, measured against its own peg, not usable either.

### Concentration risk

All three tokens are issued from one account. That account is the single point
of failure for the issuer's entire African-fiat set — one compromise, one
freeze, or one lost key takes NGN, GHS and KES exposure simultaneously. This
is a structural observation from the TOML, not a measurement.

---

## Reproducing

```
go run ./cmd/ladder            # USDC -> NGNC
go run ./cmd/ladder -to GHSC   # USDC -> GHSC
go run ./cmd/ladder -to KESC   # USDC -> KESC
```

Requires live network access to Horizon and the reference rate provider. The
figures will differ from those above — that is the point of measuring rather
than asserting.
