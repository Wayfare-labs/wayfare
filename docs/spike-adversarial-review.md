# Spike: how would someone game a Wayfare verdict?

Issue [#225](https://github.com/Wayfare-labs/wayfare/issues/225), backlog
`#165`.

**Status: completed.** An issuer wanting a better grade (or a competitor
wanting a worse one) is the adversary here. The verdict number itself is well
defended — it is arithmetic over two inputs, a quote from Horizon pathfinding
and a reference mid from two independent providers, neither of which the
issuer controls, and the reference layer is deliberately biased *against*
flattering a corridor. The gameable corners are not the loss number but the
**integrity classification** (the cheap unregistered-hop and direct-pair
paths), the **registry** that gates asset identity (maintainer review is the
only gate), and a **not-yet-live SEP-38 quote path** that would turn an
anchor into its own bookmaker. No code was changed; findings beat on the tree
as it is.

---

## The adversary model

The party with motive is an issuer — or someone acting for one. "Better
grade" means one or more of: a lower `loss_pct` at the sizes that matter, a
`GOOD`/`FAIR` verdict where today there is `UNUSABLE`, an `integrity` of
`DIRECT` where today there is `DERIVATIVE`, or no adverse finding on their
corridor. A separate, weaker motive is making a *competitor's* corridor look
worse.

A verdict is a pure function of two facts:

```
loss_pct  = (reference_mid − effective_rate) / reference_mid            route/route.go:237
verdict   = thresholds over loss_pct                                     route/route.go:98-109, 71-75
```

- `effective_rate` comes from Horizon `/paths/strict-send`,
  `route/route.go:593-623` (`quoteDEX`), the same engine that would settle
  the payment (`dex/dex.go:177-189`).
- `reference_mid` comes from two independent providers, cross-checked
  (`refrate/cross.go:171-209`), reconciled by rule (`refrate/cross.go:261-329`).

Integrity is a third, separate output — a structural classification, not a
loss figure (`route/route.go:538-589`, `classify`).

The enumeration below works through everything an adversary could touch and
what each one can actually move. Where a defence already exists in the tree it
is cited; where a corner is open it is reported as open.

---

## 1. Facts the issuer controls directly

### 1a. Their own `stellar.toml` (SEP-1)

The issuer publishes `status`, `CURRENCIES`, `WEB_AUTH_ENDPOINT`,
`ANCHOR_QUOTE_SERVER`, `KYC_SERVER`, home domain, and the rest
(`anchor/anchor.go:55-69`, `profileFrom` at `anchor/anchor.go:308-320`). All
of it is self-asserted — an issuer can declare `status="live"` for an asset
that is not in service (`anchor/anchor.go:51-53` treats any non-`live` status
as not in service, but "live" is their word).

**What it can move:** nothing that changes a verdict. Everything read from the
TOML flows into the checks layer, and the checks layer has no path back into
the engine: `Findings` exposes no way to influence integrity or a verdict, by
construction (`checks/checks.go:22-29`, `checks/checks.go:505-514`). The one
thing a self-asserted status does change is the **finding** a reader sees
("issuer declares this pending" vs "live"), and the identity layer below
cross-checks claims against the ledger.

The exception that proves the rule: an issuer that genuinely changes the
facts — publishes a real SEP-38 server, or flips a real ledger flag — *is*
reflected in the measurement. That is the tool working, not being gamed; the
only way to fake it would be to have the claim verified against an
independent source, and the claim already is (see §2).

### 1b. Issuer-held ledger keys

The issuer holds the keys to their issuing account, so they control:

- **Auth flags** — `AUTHORIZATION_REVOCABLE`, `CLAWBACK_ENABLED`,
  immutability — observed by `IssuerAuthFlags` and `IssuerFlagImmutability`
  (`checks/runner.go:49-51`, and the seven-check `Default()` set at
  `checks/runner.go:43-53`). These are on the ledger, so they are the truth;
  to "pass" a clawback check an issuer must genuinely disable clawback.
  Results are findings only; they never move the headline
  (`checks/checks.go:508-514`).
- **Liquidity** — offers and AMM pools in their own token, which is what
  Horizon prices. This is the *only* part of the loss figure the issuer can
  touch, and it is the honest mechanism: to reduce the reported loss the
  issuer must genuinely improve the achieved rate. That costs them real money
  (see §3a), and it is capped: a route better than mid is clamped to zero
  loss, never reported as profit (`route/route.go:240-244`).

### 1c. Their own endpoints (SSRF / resource abuse)

Every URL Wayfare follows on an issuer's say-so — TOML, auth endpoint, quote
server — is fetched by the server on behalf of anyone who asks for a corridor.
This input is treated as adversarial and already defended:

- `GuardedClient` refuses to dial loopback, private, link-local, multicast and
  unique-local IPv6 on the *resolved* address, on the initial request and
  every redirect (`checks/transport.go:11-35`, `44-80`, `88-119`). The
  motivating example in the source comment is an anchor publishing
  `WEB_AUTH_ENDPOINT = "http://169.254.169.254/latest/meta-data/"` to read the
  probing server's cloud metadata — the exact attack, documented where the
  defence lives (`checks/transport.go:18-24`).
- Error bodies from non-OK responses are capped at 64 KiB so a hostile
  endpoint cannot exhaust probing memory (`checks/transport.go:38-41`).
- Redirect chains are bounded at 5 hops and the scheme is re-checked per hop
  (`checks/transport.go:106-117`).

---

## 2. The identity layer: the registry is the gate nobody passes alone

Asset identity — who issues "USDC", which corridor a request measures — is the
most important "attack surface" that *isn't* open:

- Every asset on the wire resolves through the verified registry:
  `asset.Lookup` is the only resolution path on the API and trend endpoints
  (`server/api.go:139-148`, `server/trend.go:241-248`), and `cmd/ladder`
  accepts only the four registered corridor destinations (`cmd/ladder/main.go:51-56`,
  `77-81`). `?to=SCAMC` is a `400 unknown_receive_asset`
  (`server/api.go:146-149`, `408`), not a measurement of someone's lookalike.
- Registration is maintainer-reviewed: `asset/known.go`'s registry is verified
  by hand against each issuer's own `stellar.toml`, with the verification date
  recorded, and code+issuer collisions are refused at init
  (`asset/known.go:9-16`, `161-198`, `325-337`). A token named "USDC" from a
  stranger account cannot be measured because the request stops at the
  registry.
- **The residual here is social, not technical.** The way "in" is a PR to
  `asset/known.go` inserting an issuer, and the way to game it is a maintainer
  accepting a flattering-but-false claim. The documented rule is
  "verify against live sources; do not encode remembered values"
  (`CONTRIBUTING.md:86-89`), which is code for "read the issuer's own
  `stellar.toml` yourself and record the date." Nothing in the tree automates
  that reviewer step; it is the identity kernel's manual, trusting part.

---

## 3. What an issuer can actually push on, ranked by cost

### 3a. Subsidise their own market (expensive, honest, capped)

An issuer can list offers or seed AMM pools so Horizon returns better paths.
Because the reference mid is independent, this only helps them by genuinely
improving the achieved rate — they are paying real capital to reduce a real
loss figure. Two things bound it:

- The **charitable floor**: better-than-mid is *zero*, so the upside of a
  genuinely good corridor cannot be reported as profit (`route/route.go:240-244`).
- The **ladder is per-size** (`route/ladder.go`): a corridor is graded at
  twelve sizes (0.1 → 5000, `cmd/ladder/main.go:61-62`). Subsidising only the
  small rungs buys a single GOOD rung but leaves every other size untouched,
  so a request at another size still gets the honest verdict. The overall
  "viable" signal is weaker: `Viable` is true when *any* size produced a
  recommendable quote (`route/ladder.go:132-133`), and `Recommended` is just
  the best acceptable quote across sizes (`route/ladder.go:120-123`), so a
  targeted one-rung subsidy is technically enough to flip ladder `viable` —
  flagged, not dismissed, below in §5.

### 3b. Add a direct pair — the cheap integrity play

`DERIVATIVE` → `DIRECT` requires a single path that avoids traversing another
registered fiat token (`route/route.go:568-571`: any fiat-free path sets
`independent`). The cheapest way to buy that is to provide liquidity for a
direct `USDC:NGNC`-style pair, which costs pool capital — an order of
magnitude less than subsidising every rung of the ladder. This moves
**integrity only**, not loss; a corridor that was derivative because it had no
market of its own is now genuinely direct, and the classification follows.
This is the honest-mechanism case again: the ledger really does have that
market now. It is listed as a game *only* because it is cheap compared with
3a and buys a headline property.

### 3c. The unregistered-hop hole — the cheapest integrity play of all
`asset/known.go:117-127` states the rule: a hop that is neither native nor in
the registry is "unknown", and an unknown hop is classified exactly like a
bridge (XLM): **not a fiat dependency** (`asset.ClassifyHop` at
`asset/known.go:499-508`; `route/route.go:554-566`). So a DERIVATIVE corridor
routed through a fiat token that happens *not to be registered* is graded
`DIRECT`.

To exploit: issue a fresh, unregistered naira token, seed liquidity so
pathfinding routes through it, and the corridor's `DIRECT` claim is now
"true" per the classifier and false per reality. The project knows this is a
false-negative: the comment calls it "a known, bounded false-negative"
(`asset/known.go:117-127`), and every unregistered hop is surfaced as a
coverage note on the wire (`route/route.go:504-525`, `unknownHopNote`) — but
the note is informational, and registration (maintainer-reviewed) is the only
fix. **This is the cheapest open integrity game in the tree.**

The reverse — making a competitor look *more* derivative — does not work
through this hole: a hop being unregistered never creates a fiat dependency;
only a *registered* peg does (`asset/known.go:494-497`).

### 3d. Paint the tape on a scheduled measurement

Measurements are 6-hourly (`monitor/monitor.go`, `.github/workflows/measure.yml:14-19`),
and `live=1` measures on demand (`docs/api.md`). An issuer who knows the
schedule (it is a public cron) could stand up transient favourable liquidity
for the minutes each sweep runs and tear it down afterwards. The ladder is
fast — sub-4s for a full sweep, observed 2026-09-24
(`docs/qa/artifacts/261-live-ladder-timeout.md`) — so the window is small, but
the recorded run would still reflect the painted market. The defences are
exactly the parts gameless, ordinary measurement relies on: the run is
hash-chained and re-verifiable (`docs/verify-store.md`), snapshots pin the
verbatim bytes and the code revision that captured them
(`cmd/ladder/main.go:328-370`, `snapshot/snapshot.go:132-138`), and the trend
endpoint records every run so an anomaly is visible as a one-off against the
rest of the history (`docs/api.md`, trend). Painting leaves forensic evidence
and no way to delete it from the chain.

---

## 4. What an issuer cannot touch

- **The reference mid.** The issuer does not select, host, or influence the
  two reference providers. The reconciliation is biased against flattery by
  design: on disagreement it scores the **larger** mid — the one producing the
  higher loss (`refrate/cross.go:146-151`, `312-323`); beyond 10% divergence
  it refuses to score at all (`refrate/cross.go:286-294`); a zero mid from
  either side is a broken feed, refused (`refrate/cross.go:269-275`,
  `239-258`). To "game" the benchmark down — the direction a flatterer needs —
  the code never moves that way. The one uncorroborated state, `SINGLE`
  (`refrate/cross.go:239-258`), still scores a lone provider's mid, but only
  when a provider is absent or down, which is an operator-visible condition
  the issuer does not choose, and the record says no cross-check happened.
- **The verdict thresholds.** Constants in code, breaking-if-altered
  (`route/route.go:65-75`; README "Verdict thresholds" contract).
- **Recorded or snapshot history.** Hash-chained store with re-verification
  in CI and before every measure PR merges (`docs/verify-store.md:160-173`);
  provenance refuses a dirty tree (`cmd/ladder/main.go:346-370`). Physical
  tampering with committed bytes is out of the issuer's adversarial model.
- **The recommendation rule.** "When no size produces a verdict of `POOR` or
  better, the monitor recommends nothing" — a ranking's winner is not
  automatically the product (`README.md:242-252`, `route/route.go:408-413`).
  An issuer whose corridor never clears the threshold cannot make
  `recommended` appear by having lots of quotes.

---

## 5. The summary

| Attack (gamer's tool) | Can move | Cost to gamer | Current defence | Residual |
|:---|:---|:---|:---|:---|
| Publish favourable `stellar.toml` claims | Findings only | free | checks never move the headline (`checks.go:508-514`); identity registry is separately verified | issuer can look better in the findings block |
| Flip ledger auth flags | Findings only | real (genuine change) | flags are public ledger truth | none; that *is* the honest fact |
| Subsidise their own market | `loss_pct`, verdict | high (real capital) | independent mid; per-size ladder; zero-loss cap (`route.go:240-244`) | one thin rung can flip ladder `viable` |
| Add a direct pair | `integrity` | medium (pool capital) | classification follows the ledger | none; the market genuinely exists |
| **Route through an unregistered fiat token** | **`integrity` (as `DIRECT`)** | **low (fresh token + liquidity)** | **surfaced as a note, not a gate (`known.go:117-127`, `route.go:512-525`)** | **open — the cheapest game in the tree** |
| Paint the tape during a scheduled sweep | whatever the ladder records that run | medium, transient | hash chain + snapshots + trend history keep the lie visible and undeletable | verdicts describe the ledger at the moment measured |
| PR a flattering issuer into the registry | the issuer's own corridor definition | free (social) | documented reviewer rule: verify live, record the date (`CONTRIBUTING.md:86-89`) | manual, trusting step; nothing automates it |
| SSRF / memory abuse via own URLs | Wayfare's host, not the verdict | low | `GuardedClient` blocks private/loopback on every hop (`transport.go:44-119`); 64 KiB error cap | none known |
| Steer the reference providers | nothing | — | two independent providers; conservative disagreement; malfunction refuses (`cross.go:286-294`) | `SINGLE` uncorroborated mid scores when a provider is absent |
| Issue a lookalike token | nothing broadcast | — | registry-gated resolution (`server/api.go:139-148`); code+issuer conflation refused (`known.go:325-337`) | none over the API |

---

## 6. The future surface, flagged: SEP-38 quotes

`route.Kind` has an `anchor-sep38` value today, but no corridor is priced
through it — every quote this project has ever published is `"dex"`
(`route/route.go:184-187`; README "Every quote in a response is `"dex"` today"):
live SEP-38 pricing is `#180` and none of the measured anchors publishes an
`ANCHOR_QUOTE_SERVER` (e.g. `asset/known.go:41-42`). When it lands, the
adversary model changes materially: an anchor's own quote server becomes both
the rail and the price, so the issuer's server would be supplying the very
number being scored. Basic sanity already exists — a quote whose buy amount
exceeds the gross implied by its own price is refused, never presented as a
bonus (`sep38/sep38.go:140-147`) — but "the anchor quotes its own corridor" is
a different trust question from the current one and should be treated as its
own adversarial review when the feature is designed.
**This spike makes no judgement on it now; it records the boundary.**

---

## 7. Findings

1. **The loss number is not directly gameable.** It is scored against a
   benchmark the would-be gamer does not control, and the benchmark layer is
   biased the safe way. No issuer action can move `loss_pct` without genuinely
   changing the achieved rate on the ledger.
2. **The integrity classification is the softest live target.** The
   unregistered-hop rule (`asset/known.go:117-127`) lets a low-cost actor turn
   a derivative corridor into a "direct" one, surfaced as a note rather than
   gated. A follow-up could insist an unregistered hop produces `UNKNOWN`
   integrity until registered (with the note already carrying the gap), or
   require registration before a corridor's `DIRECT` claim stands. That is a
   taxonomy change and is a separate issue with a separate review bar, not
   something this spike implemented.
3. **Identity is the kernel and its weak spot is a human.** The registry
   gate is strong on the wire; its single trusting point is a maintainer
   accepting a claim at review time.
4. **SEP-38 pricing is the boundary to re-review when it ships.** Recorded
   here as prospective, nothing more.

## What this spike did not do

- Did not change the integrity taxonomy, the recommendation rule, or any
  threshold — findings 2–4 above are flagged, not implemented.
- Did not add or change any check, metric, or registry entry.
- Did not probe any live issuer.

## Related

- [checks.md](checks.md), [`checks/transport.go`](../checks/transport.go) —
  the check contract and the SSRF guard
- [run-store.md](run-store.md), [verify-store.md](verify-store.md) — why
  recorded history cannot be retroactively cleaned
- [snapshot-record-replay.md](snapshot-record-replay.md) — byte-pinned
  provenance including the dirty-tree refusal
- [asset](../asset/), [route/route.go](../route/route.go) — the registry gate
  and the classify false-negative at `asset/known.go:117-127`
- [spike-second-maintainer-verification.md](spike-second-maintainer-verification.md)
  — the trust claims a second maintainer can actually re-check, which is who
  else an issuer would have to fool
- [spike-sdk-surface.md](spike-sdk-surface.md), [spike-api-consumers.md](spike-api-consumers.md)
  — where a gamer could point a consuming client instead of the measurement