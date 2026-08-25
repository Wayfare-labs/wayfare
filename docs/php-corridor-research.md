# Research: does a usable Stellar-native corridor exist for USDC → PHP?

**Status: completed. Finding: NO-MARKET.** Three candidate PHP issuers
investigated. None is a verifiable, live issuer with usable Horizon path data.
No code changes.

Investigated 2026-08-25 against `horizon.stellar.org`. Reference USD/PHP
mid on that date was **61.74 PHP/USD** (the project's existing provider,
`latest.currency-api.pages.dev`, `usd.php`).

---

## Why this matters

The Philippines is one of the world's largest remittance-receiving markets,
and remittance cost to the Philippines is a well-documented problem. If a real
Stellar-native PHP issuer exists with Horizon path data, USDC → PHP is one of
the highest-value corridors this project could measure.

But "Stellar has had activity in the Philippines" is not the same as "a usable
corridor with a real issuer." The standard applied to every corridor here
(see `asset/known.go`) is that an issuer is identified from its own published
`stellar.toml` (SEP-1), not from a code match or a blog post — anyone can issue
a token called `PHP`, and Horizon will price a worthless lookalike as happily
as a real one. This research applies that standard before any liquidity claim
is made.

---

## Candidates investigated

Horizon's `/assets?asset_code=PHP` returns 20+ distinct issuers of a token
coded `PHP`, plus scattered variants (`PHPT`, `PHPC`, `PHT`, `XPHP`). Ranked by
authorized trustlines, only three issuers have a non-trivial holder base and a
declared `home_domain`. Each was checked for SEP-1 resolution, Horizon path
data from Circle USDC (`GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN`),
and a SEP-38 quote endpoint.

### 1. PHP — issuer `GANDGDF7ZHF7RVXMI53PUSCMVWEFANGTZ4RLEH2DGPDFI5BGWYOLAXRR`

- **home_domain:** `sendwise.org`
- **Holders:** 221 authorized trustlines — the largest PHP token on the network.
- **`stellar.toml` (SEP-1):** **Unreachable.** The host resolves
  (`52.60.87.163`) but no TLS/HTTP service answers `/.well-known/stellar.toml`
  (connection refused). The issuer's identity, peg claim, asset `status`, and
  `anchor_asset_type` therefore **cannot be verified**. Under this project's
  evidence standard an issuer that cannot be resolved via SEP-1 is not a
  confirmed issuer, regardless of trustline count.
- **Horizon path data:** This is the **only** PHP token with any path from
  USDC — but the path is a single **XLM-bridged** hop
  (`USDC → XLM → PHP`), not a direct market. There is **no direct USDC/PHP
  order book** (`/order_book` returns 0 bids, 0 asks). The bridged path prices:

  | Send (USDC) | Receive (PHP) | Implied PHP/USD | vs 61.74 mid |
  |---:|---:|---:|---:|
  | 1 | ~516 | ~516 | ~8.4× off |
  | 10 | ~5,133 | ~513 | ~8.3× off |
  | 100 | ~46,190 | ~462 | ~7.5× off |
  | 1,000 | **~275** | ~0.3 | liquidity collapses |

  The token trades roughly 7–8× away from the real peg at small size, and at
  1,000 USDC the delivered amount **collapses to ~275 PHP total** — near-total
  loss. This is the archetypal "a path exists but it delivers nonsense" case
  the monitor is built to flag, not a usable corridor.
- **SEP-38:** None. No `stellar.toml`, so no `ANCHOR_QUOTE_SERVER`.
- **Verdict: Rejected.** Unverifiable issuer; the only available path is a thin
  XLM bridge trading far off peg with no depth.

### 2. PHP — issuer `GDWVRPDJHM6QUGOP5CGU77AWLQYUOVDRU5M2AAZFIOBJ6FVRAUOWBVKH`

- **home_domain:** `denniscaba.com`
- **Holders:** 32 authorized trustlines.
- **`stellar.toml` (SEP-1):** Resolves (HTTP 200), but the file is a
  lightly-edited copy of Stellar's example `sample.toml`. It retains the
  "Sample stellar.toml" header, the placeholder `api.stellar.org` federation
  and auth servers, the `$sdf_watcher` account placeholders, and a fictional
  second currency (`QCR`, "Quantum Crowd Rewards", with a `denniscaba.com`
  logo). Its `[[CURRENCIES]]` block does declare `code="PHP"`,
  `issuer=GDWVRP…`, `display_decimals=2` — but with **no `status`, no
  `anchor_asset`, no `anchor_asset_type`, and no `is_asset_anchored`**. Nothing
  in the file asserts a PHP peg or that the asset is in service. This is a
  developer/test artifact, not a production anchor declaration.
- **Horizon path data:** **Zero paths** from USDC (strict-send returns no
  records). Not tradeable from the settlement asset.
- **SEP-38:** None. The domain exposes no `TRANSFER_SERVER` and no
  `ANCHOR_QUOTE_SERVER`.
- **Verdict: Rejected.** A sample-file artifact with no live status, no peg
  declaration, and no path data.

### 3. PHPT — issuer `GCADSE5JJJPNC4HPOZTSUOYOFLR4JPG6N5METWZH3ZLZGSI3MALLWXHW`

- **home_domain:** `trade.bloom.solutions`
- **Holders:** 10 authorized trustlines.
- **Why it is the most credible name:** Bloom Solutions is a real,
  Philippine-based blockchain remittance company and a long-standing name in
  Stellar's Philippines activity. If any PHP corridor were live, this is where
  it would be expected.
- **`stellar.toml` (SEP-1):** **Unreachable.** `trade.bloom.solutions` resolves
  (`35.193.240.138`) but `/.well-known/stellar.toml` times out with no
  response. The apex `bloom.solutions` is a Squarespace marketing site that
  returns **HTTP 404** for `/.well-known/stellar.toml`. No SEP-1 document is
  currently served for this asset.
- **Horizon path data:** **Zero paths** from USDC. `PHPT` is not reachable from
  the settlement asset on-chain.
- **SEP-38:** None reachable (no `stellar.toml`).
- **Verdict: Rejected.** The most credible name, but no reachable SEP-1
  document and no on-chain path data. Whatever Bloom operates today is not an
  observable USDC → PHP corridor on the public ledger.

---

## Variants considered and dismissed

`/assets` lookups for `PHPX` (0 issuers), `PHPS` (0), `PHPC` (1 holder, a
vanity-address token with no anchor), `PHT` (4 holders, unrelated), and `XPHP`
(1 holder) turn up nothing with an anchor, a peg declaration, or path data.
None warranted a SEP-1 or path check.

---

## Recommendation

**NO-MARKET.** As of 2026-08-25 there is no verifiable, live, Stellar-native
PHP issuer with usable Horizon path data from USDC.

- The most-held PHP token (`sendwise.org`) cannot be resolved via SEP-1, and
  its only path is a thin XLM bridge trading ~7–8× off peg that collapses to a
  near-total loss by 1,000 USDC.
- The one PHP token whose `stellar.toml` resolves (`denniscaba.com`) is a
  sample-file artifact with no live status, no peg, and no path data.
- The most credible operator (`Bloom Solutions` / `PHPT`) serves no reachable
  SEP-1 document and has no on-chain path from USDC.

This corridor must **not** be registered in `asset/known.go` or priced by the
measure workflow. Registering it would price a token whose issuer this project
cannot verify, against a path that delivers nonsense — exactly the outcome the
monitor exists to prevent.

### What would need to change

USDC → PHP would become a candidate corridor if:

1. **A real issuer publishes a resolvable `stellar.toml`** declaring PHP with
   `status="live"`, an `anchor_asset` / `anchor_asset_type="fiat"`, and a
   verifiable issuing account — the same standard NGNC met in `asset/known.go`.
2. **Horizon carries a path from USDC with real depth** — ideally a direct
   USDC/PHP order book rather than a single XLM-bridged hop, priced within a
   defensible band of the USD/PHP mid.
3. **A SEP-38 `ANCHOR_QUOTE_SERVER`** (or SEP-24/SEP-6 on/off-ramp) is exposed,
   so the on-chain leg connects to an actual fiat cash-out.

Until then, the honest finding is that no measurable USDC → PHP corridor
exists on Stellar, and the project should say so plainly rather than price a
lookalike.

---

## Related

- Issue [#60](https://github.com/Wayfare-labs/wayfare/issues/60) — this
  research task.
- Issue [#33](https://github.com/Wayfare-labs/wayfare/issues/33) — the
  reference standard for detail.
- `asset/known.go` — the verified-issuer registry a real PHP issuer would join.
- `docs/adding-a-corridor.md` — the process a confirmed corridor follows.
- `docs/parallel-rate-research.md` — the prior research finding this write-up
  follows in form.
