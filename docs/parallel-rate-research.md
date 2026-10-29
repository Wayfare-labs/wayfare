# Research: does a usable parallel/street-rate data source exist for NGN?

**Status: completed.** Investigated three sources; no usable source found.

---

## Why this matters

Nigeria has a well-known gap between the CBN official rate and the parallel
(street) market rate. If the providers used by `refrate/` track only the
official rate, the loss-floor finding is measured against a rate that may not
reflect what a user can actually get on the street.

If a parallel-rate source exists, the project could report both gaps — giving
readers a more complete picture of the cost landscape. But the source must be
defensible: it must document its methodology, be consistently available, and
be free of API keys the project does not hold.

---

## Sources investigated

### 1. abokiFX (abokifx.com)

- **URL:** https://www.abokifx.com/
- **What it provides:** NGN parallel market rate, updated daily. Also provides
  official CBN rate, money transfer rates, and crypto prices.
- **Cost:** Free website; no documented API.
- **Access method:** Web scraping only. The site does not publish a public API.
  There is no JSON endpoint, no documented rate limit, no terms of service
  permitting programmatic access.
- **Methodology:** Not documented. The site does not explain where the
  parallel rate comes from — whether it is collected from street traders,
  aggregated from money transfer operators, or estimated.
- **Verdict: Rejected.** No API, no documented methodology, and web scraping
  a site that does not permit it would be both fragile and outside the
  project's evidence standard. A rate whose source is undocumented is
  unverifiable.

### 2. parallel.ng

- **URL:** https://parallel.ng/
- **What it provides:** NGN parallel market rate, updated in near-real-time.
  Shows buy and sell rates for USD, GBP, EUR against NGN.
- **Cost:** Free website; no documented API.
- **Access method:** Web scraping. The site renders rates in JavaScript, so
  a simple HTTP GET does not return the data. A headless browser or API
  reverse-engineering would be required.
- **Methodology:** Partially documented. The site claims rates are sourced
  from "verified exchangers" but does not name them, does not publish
  sample data, and does not explain how rates are validated.
- **Verdict: Rejected.** No API, requires JavaScript rendering, and the
  methodology is insufficiently documented for the project's evidence
  standard. "Verified exchangers" without naming them is an assertion, not
  a defensible source.

### 3. ExchangeRate-API / fawazahmed0/currency-api

- **URL:** https://latest.currency-api.pages.dev/
- **What it provides:** Official/interbank rates for fiat currencies. Already
  used by the project as a reference rate provider.
- **Cost:** CC0-1.0, free.
- **Access method:** JSON API. Already integrated in `refrate/`.
- **Methodology:** Aggregates from multiple official sources. Does not provide
  parallel/street rates.
- **Verdict: Not applicable.** This is the project's existing official-rate
  provider. It does not carry parallel rates and is not a candidate for one.

---

## Additional sources considered but not investigated

- **Paga, Flutterwave, Chipper Cash:** These are payment platforms, not rate
  data providers. Their rates are product-specific (the rate you get when
  sending through their rails) and not a general market benchmark.
- **Google Finance / XE.com:** These track the official/interbank rate, not
  the parallel market.
- **Bureau de change APIs:** No publicly documented, free, methodology-
  transparent API was found for Nigerian bureau de change rates.

---

## Recommendation

**No usable parallel-rate source found.** The sources that carry the data
either lack an API, lack documented methodology, or both. The project cannot
publish a number it cannot defend, and a parallel rate without a defensible
source would be an assertion dressed as a measurement.

### What would need to change

A source would become usable if it:

1. **Publishes a documented API** — a JSON endpoint with a clear contract,
   rate limits, and terms of service permitting programmatic access.
2. **Documents its methodology** — where rates come from, how they are
   collected, how frequently they update, and what "parallel rate" means
   precisely (buy side, sell side, mid).
3. **Is consistently available** — not a site that goes down during
   peak hours or changes its URL without notice.
4. **Is free or within the project's budget** — the project uses only
   free-tier providers today.

If such a source appears, issue #57 can proceed. Until then, the gap between
official and parallel rates remains an observation the project notes but
does not quantify.

---

## Related

- Issue [#56](https://github.com/Wayfare-labs/wayfare/issues/56) — this
  research task
- Issue [#57](https://github.com/Wayfare-labs/wayfare/issues/57) — blocked
  on this research
- `refrate/` — the project's reference rate infrastructure
