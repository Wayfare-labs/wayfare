# Snapshot record-and-replay workflow

How to capture upstream responses from a live ladder run and replay them in
tests — the full path from `ladder -record` to `snapshot.Load` to a
deterministic test that never touches the network.

This document is a workflow guide. The format contract is
[snapshot-format.md](snapshot-format.md); this is how you use it.

---

## Why raw bytes

A fixture built from Go structs can only confirm the parser agrees with
itself. Real upstream bytes catch things a hand-built fixture cannot:
precision differences, shape variance like native XLM hops, and the
meaningful empty — `_embedded: { records: [] }` meaning *no market exists*
rather than an error. See
[snapshot-format.md](snapshot-format.md#why-the-bytes-and-not-the-structs).

---

## Recording a snapshot

```bash
go run ./cmd/ladder -to NGNC -ref currency-api -record testdata/snapshots
```

That is the entire command. The ladder measures USDC → NGNC against live
mainnet, and `-record` writes the verbatim upstream responses to a new
directory under `testdata/snapshots/`.

### What the command does

1. **Runs the full ladder** — all twelve sizes (0.1 → 5000 USDC), hitting
   Horizon for pathfinding and the reference provider for the mid-market rate.

2. **Records every HTTP response body** — the exact bytes off the wire,
   stored as individual JSON files. Bodies are pinned by `sha256` and
   verified on load.

3. **Checks for transport errors.** If any size failed to reach an upstream,
   the snapshot is **not written**. Replaying a recording with a hole in it
   would render a corridor as partly unpriced when the market was fine and
   the network was not — a fabricated finding. The command tells you which
   sizes failed and exits with code 1.

4. **Refuses to record from a modified working tree.** `git_revision` is
   the only link between a committed fixture and the code that produced it.
   Recording from a dirty tree would name a revision that did not generate
   the bytes. Commit or stash first, or pass `-allow-dirty` to mark the
   manifest.

5. **Writes the manifest and body files** to a directory named by corridor
   and timestamp: `usdc-ngnc-20260821T223040Z/`.

### Flags

| Flag | Default | Purpose |
|:-----|:--------|:--------|
| `-to` | `NGNC` | Destination asset code (NGNC, GHSC, KESC) |
| `-sizes` | `0.1,1,5,10,...,5000` | Comma-separated send amounts in USDC |
| `-ref` | `exchangerate-api` | Reference rate provider: `exchangerate-api` or `currency-api` |
| `-record` | (empty) | Record upstream responses as a snapshot; value is the parent directory |
| `-json` | `false` | Emit JSON on stdout instead of the text table |
| `-allow-dirty` | `false` | Record even though the working tree is modified, marking the manifest dirty |

### Output

The directory structure matches the layout in
[snapshot-format.md](snapshot-format.md#layout):

```
testdata/snapshots/usdc-ngnc-20260821T223040Z/
    manifest.json
    responses/
        001-v1-currencies-usd.json
        002-paths-strict-send-0.1.json
        ...
        013-paths-strict-send-5000.json
```

On success, the command prints to stderr:

```
recorded 24 upstream responses to testdata/snapshots/usdc-ngnc-20260821T223040Z
```

---

## Loading a snapshot

```go
m, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
if err != nil {
    t.Fatal(err)
}
```

`Load` reads the manifest, parses it, and verifies every `body_sha256` as
it reads. A tampered fixture fails here rather than downstream. It returns
a `*Manifest` that carries the corridor, sizes, sources, and the recorded
bodies in memory.

Unknown format versions are refused loudly:

```
snapshot: testdata/snapshots/usdc-ngnc-... is format version 3, but this
build reads version 1; refusing to parse it rather than risk misreading a
recorded measurement
```

This is rule 1 of the contract. A snapshot that misparses silently would
republish a wrong figure under Wayfare's own name.

---

## Replaying in tests

There are two ways to use a loaded snapshot, depending on what you need.

### As an `*http.Client` (most common)

```go
m, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
if err != nil {
    t.Fatal(err)
}

dexClient := &dex.Client{
    HorizonURL: "https://horizon.stellar.org",
    HTTPClient: m.HTTPClient(),
}
```

`HTTPClient()` returns an `*http.Client` that answers only from the
snapshot. Every call to `dex.Client` (and to `refrate.ExchangeRateAPI`,
and to any other upstream that accepts an `*http.Client`) is routed
through this client — no network involved.

### As an `http.RoundTripper` (when you need to wrap it)

```go
replayer := m.Replay() // returns *snapshot.Replayer
```

`Replay()` returns the bare `http.RoundTripper` if you need to compose it
with other transports. For example, the monitor tests build a handler that
replays some corridors and fails others:

```go
replay := m.Replay()
handler := func(w http.ResponseWriter, r *http.Request) {
    resp, err := replay.RoundTrip(r)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()
    w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
    w.WriteHeader(resp.StatusCode)
    io.Copy(w, resp.Body)
}
```

### What happens on a miss

If a request is made that the snapshot has no recorded answer for, the
replayer returns `snapshot.ErrNotRecorded` — a hard error, never a
fall-through to the network. This is rule 5 of the contract:

> A replayer that falls through to the network makes a "deterministic"
> test intermittently live — which is worse than a flaky test, because it
> passes.

The error includes the key that was requested and a sample of recorded keys:

```
snapshot: usdc-ngnc-20260821T223040Z has no recorded response for
"GET /paths/strict-send?destination_assets=USDC&...";
12 requests are recorded, including:
    GET /paths/strict-send?destination_assets=NGNC%3AGASBV6W…&source_amount=0.1&…
    GET /paths/strict-send?destination_assets=NGNC%3AGASBV6W…&source_amount=1&…
    ... and 10 more
```

---

## The three committed snapshots

| Corridor | State | Why it is a required fixture |
|:---|:---|:---|
| USDC → NGNC | `DIRECT` | The corridor every published figure comes from |
| USDC → GHSC | `DERIVATIVE` | Every path routes through NGNC — the case a loss number hides |
| USDC → KESC | `NO-MARKET` | Zero paths; `_embedded.records: []`, not an error |

Each exercises a shape the others cannot. A test that only uses the NGNC
snapshot never encounters an empty records array or a derivative integrity
state.

---

## Re-recording

Snapshots are pinned by `git_revision` and `body_sha256`. When the code
that parses upstream responses changes, or when the upstream format changes,
the snapshot must be re-captured against live mainnet:

```bash
go run ./cmd/ladder -to NGNC -ref currency-api -record testdata/snapshots
```

The new snapshot will have a new timestamp directory. Delete the old one
after the new one is committed. The `-record` flag refuses to overwrite an
existing manifest — a snapshot is a record of one moment, and overwriting
one silently would destroy the provenance of anything already derived from
it.

---

## Related

- [snapshot-format.md](snapshot-format.md) — the format contract and its nine rules
- [corridor-measurements.md](corridor-measurements.md) — the published figures
  these snapshots make testable
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
