# Snapshot format

A snapshot is one ladder run's upstream traffic, recorded verbatim, so the
measurement can be reparsed later without touching the network.

This document is the contract. The `snapshot` package implements it, and
issues #20 and #21 build on it — so it is written as a spec, not as a
description of the code.

**Format:** `wayfare.snapshot`, **version 1**.

---

## Why the bytes and not the structs

The obvious fixture is a Go struct: build a `wirePathRecord`, marshal it, test
against that. It is also worthless. A fixture derived from the parser can only
ever confirm the parser agrees with itself.

Real upstream bytes catch things a hand-built fixture cannot:

- **Precision.** Horizon returns `"65100.1379550"`. A struct round-trip loses
  the trailing zero and the question of whether it survived.
- **Shape variance.** A native XLM hop appears as `{"asset_type":"native"}`
  with no code or issuer, unlike every other hop.
- **The meaningful empty.** `"_embedded":{"records":[]}` is a `200 OK` and
  means *no market exists*. A hand-written fixture author reaches for an error
  or a null, and the `NO-MARKET` state gets tested against a shape the network
  never sends.

Every published figure in `docs/corridor-measurements.md` comes from parsing
these bytes. The snapshot is what makes that arithmetic testable.

---

## Layout

```
testdata/snapshots/<send>-<recv>-<YYYYMMDDThhmmssZ>/
    manifest.json
    responses/
        001-v1-currencies-usd.json
        002-paths-strict-send-0.1.json
        ...
        013-paths-strict-send-5000.json
```

Directory name is `<send code>-<recv code>-<recorded_at>`, lowercased, with the
timestamp in RFC 3339 basic format, UTC: `usdc-ngnc-20260821T223040Z`.
Corridor first so a listing groups by corridor; timestamp second so it sorts
chronologically within one.

**One directory is exactly one ladder run** — the unit `cmd/ladder` produces
and the unit `docs/corridor-measurements.md` publishes.

Response files are `<seq>-<slug>.json` holding the upstream body **verbatim**.
The sequence prefix keeps capture order visible in a listing. The slug is for
humans and carries no meaning to the loader, which resolves files only through
the manifest — so renaming a body file breaks nothing, and editing one is
caught by its hash.

---

## manifest.json

```json
{
  "format": "wayfare.snapshot",
  "version": 1,
  "recorded_at": "2026-08-21T22:30:40Z",
  "git_revision": "b36a3af",
  "corridor": {
    "send":           {"code": "USDC", "issuer": "GA5ZSEJ…K4KZVN"},
    "receive":        {"code": "NGNC", "issuer": "GASBV6W…FQGXZY6"},
    "reference_pair": "USD/NGN"
  },
  "sizes": ["0.1","1","5","10","25","50","100","250","500","1000","2500","5000"],
  "sources": {
    "horizon":   {"base_url": "https://horizon.stellar.org"},
    "reference": {"provider": "currency-api",
                  "base_url": "https://latest.currency-api.pages.dev/v1/currencies/"}
  },
  "interactions": [
    {
      "kind":         "horizon",
      "method":       "GET",
      "key":          "GET /paths/strict-send?destination_assets=NGNC%3AGASBV6W…&source_amount=100&source_asset_code=USDC&…",
      "url":          "https://horizon.stellar.org/paths/strict-send?…",
      "status":       200,
      "content_type": "application/hal+json; charset=utf-8",
      "recorded_at":  "2026-08-21T22:30:40Z",
      "body_file":    "responses/007-paths-strict-send-100.json",
      "body_sha256":  "9f2c…"
    }
  ]
}
```

`dirty` appears only when true — see rule 9.

---

## The nine rules

### 1. An unknown `version` is refused, loudly

`version` is an integer. A replayer **must** refuse a version it does not
know, naming the version it found and the versions it supports. Never
best-effort parse.

This is the whole reason the field exists: the format will change, and an old
snapshot misparsing silently would republish a wrong figure under Wayfare's
name.

### 2. Bodies are raw upstream responses

Never parsed structs, never reformatted, never pretty-printed. The exact bytes
off the wire. See *Why the bytes and not the structs* above.

### 3. `body_sha256` is verified on load

An edited fixture fails the load rather than quietly changing a published
number. This is what makes a committed snapshot evidence rather than an
assertion.

### 4. The request key is host-independent

The key canonicalises to `METHOD SP path [?query]`, where the query is
`url.Values.Encode()` — keys sorted, values percent-encoded.

The host is deliberately excluded so one snapshot replays against an
`httptest.Server`, against a deployment, and against mainnet without being
re-cut.

### 5. An unrecorded request is a loud error, never a network passthrough

A replayer that falls through to the network makes a "deterministic" test
intermittently live — which is worse than a flaky test, because it passes.

`snapshot.Replayer` returns `ErrNotRecorded`. A test that reaches for an
unrecorded key fails; it does not quietly succeed against production.

### 6. No money in the manifest

The manifest carries provenance only. Every figure is derived by reparsing the
bodies, so no number in a published measurement can come from a hand-edited
metadata field.

`sizes` is the one numeric field. Sizes are request *inputs*, not
measurements, and are decimal strings.

### 7. Money is a decimal string end to end

Never a JSON number, never a `float64`, on the way in or out. This holds
through the whole pipeline; see CONTRIBUTING.md.

### 8. A replay is pinned, and says it is a replay

On replay, `measured_at` is set to the snapshot's `recorded_at` — the
measurement really did happen then, and that is the honest value. The wire
gains:

```json
"replay": {"snapshot": "usdc-ngnc-20260821T223040Z",
           "recorded_at": "2026-08-21T22:30:40Z"}
```

Replaying twice is then byte-identical. **Any surface rendering a replay shows
its recorded-at, and a replay is never presentable as a live reading.**

### 9. `git_revision` must not lie

`git_revision` is the only link between a committed fixture and the code that
produced it. Recorded from a modified working tree it names a revision that
did not generate the bytes — worse than recording nothing, because the field
looks like provenance and is consulted as though it were.

**Recording refuses a modified working tree**, naming the modified files. The
check runs before the ladder, so a refusal costs nothing.

`-allow-dirty` records anyway and sets `"dirty": true` in the manifest. The
caveat then lives in the artifact rather than only in the shell that made it.
A consumer treating a `dirty` snapshot as authoritative provenance is ignoring
a field that says otherwise.

This rule exists because the repository already contained the failure: the
first GHSC and KESC snapshots recorded `e2be414`, a commit predating the
`snapshot` package that wrote them. Both were re-captured once the check
existed.

Corollary: `recorded_at` is truncated to the second and the directory name is
derived from it, so a snapshot's directory can never disagree with its own
manifest.

---

## Capturing

```bash
go run ./cmd/ladder -to NGNC -ref currency-api -record testdata/snapshots
```

Recording is **opt-in and off by default**. Nothing writes a snapshot during a
`wayfared` request: a monitor that scribbles to disk on every page view is a
different program with different failure modes, and capture should be a
deliberate act by someone who intends to publish the result.

Two things the capture path refuses:

- **A modified working tree** (rule 9).
- **A run with a hole in it.** If any size failed to reach an upstream, no
  snapshot is written. Replaying a partial recording renders a corridor as
  partly unpriced when the market was fine and the network was not — a
  fabricated finding. Re-run instead.

Note the second is about *transport errors*, not about `NO-MARKET`. A size
that returns zero paths is a finding and records normally; a size whose
request never returned is an absence of information.

### Licensing

Check the reference provider's terms permit storing responses before
committing one. The committed snapshots use
[`@fawazahmed0/currency-api`](https://latest.currency-api.pages.dev), which is
CC0-1.0 and therefore redistributable.

If a provider's terms do not permit storage, record the fetch in the manifest
and replay the rate leg from a `refrate.Static` pinned to the recorded mid,
**stating the substitution in the manifest** — never silently.

---

## The committed set

Three snapshots, one per integrity state, because each is a shape the others
cannot exercise:

| Corridor | State | Why it is a required fixture |
|:---|:---|:---|
| USDC → NGNC | `DIRECT` | The corridor every published figure comes from |
| USDC → GHSC | `DERIVATIVE` | Every path routes through NGNC — the case a loss number hides |
| USDC → KESC | `NO-MARKET` | Zero paths; `_embedded.records: []`, not an error |

---

## Using one in a test

```go
snap, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
if err != nil {
    t.Fatal(err)
}
dexClient := &dex.Client{HTTPClient: snap.HTTPClient()}
```

`Load` verifies every `body_sha256` as it reads, so a tampered fixture fails
here rather than downstream. `snap.HTTPClient()` returns a client that answers
only from the snapshot; `snap.Replay()` gives the bare `http.RoundTripper` if
you need to wrap it.

The seam is an `http.RoundTripper` because `dex.Client.get` is the single
funnel for every Horizon call and both clients already accept an
`*http.Client`. No new interface, and no way for a test to bypass it.

## Related

- [docs/glossary.md](glossary.md) — every state a reader can meet (verdicts,
  integrity, agreement, checks, freshness)
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
- [docs/corridor-measurements.md](corridor-measurements.md) — the published
  figures these snapshots make testable
