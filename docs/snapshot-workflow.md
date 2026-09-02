# Snapshot record-and-replay workflow

This document walks through the complete lifecycle of a snapshot: from
recording upstream responses, through the directory layout and manifest
structure, to using one in a test that runs offline. It is the practical
companion to [docs/snapshot-format.md](snapshot-format.md), which specifies
the format itself.

**Verified against:** the code at commit `b36a3af`, 2026-08-25.

---

## Why snapshots exist

The project's invariant is *never show a number whose origin you cannot name.*
A live call satisfies this at measurement time, but a test that hits the
network again is no longer deterministic — it passes intermittently and
publishes whatever the network returns that day, not what was measured.

A snapshot records the verbatim upstream bytes from one ladder run so the
measurement can be reparsed later without touching the network. Every published
figure in `docs/corridor-measurements.md` comes from parsing these bytes.

---

## Overview

```
 ┌──────────────────────────────────────────────────────────────┐
 │  RECORDING                      testdata/snapshots/          │
 │                                                              │
 │  cmd/ladder -record       ──►   usdc-ngnc-20260821T223040Z/ │
 │    hits Horizon & ref provider    manifest.json              │
 │    recorder captures bodies       responses/001-…013.json    │
 │    writes manifest + files                                   │
 └──────────────────────────────────────────────────────────────┘

 ┌──────────────────────────────────────────────────────────────┐
 │  REPLAYING (in tests)                                        │
 │                                                              │
 │  snapshot.Load("…/usdc-ngnc-20260821T223040Z")               │
 │    reads manifest, verifies every body_sha256                │
 │    returns *Manifest                                         │
 │                                                              │
 │  snap.HTTPClient()  ──►  *http.Client that answers from     │
 │                           the snapshot, never the network    │
 │                                                              │
 │  Wire into dex.Client, refrate, route.Engine as usual        │
 └──────────────────────────────────────────────────────────────┘
```

---

## Step 1 — Record a snapshot

Recording is opt-in and off by default. Nothing writes a snapshot during a
normal `wayfared` request; capture is a deliberate act.

### Prerequisites

- A clean git working tree (the recorder refuses a dirty tree unless you pass
  `-allow-dirty`).
- Network access to Horizon and a reference-rate provider.
- Go 1.22+.

### Run the ladder with `-record`

```bash
# Default corridor (USDC → NGNC), using currency-api:
go run ./cmd/ladder -to NGNC -ref currency-api -record testdata/snapshots

# A different corridor:
go run ./cmd/ladder -to GHSC -ref currency-api -record testdata/snapshots

# Custom sizes:
go run ./cmd/ladder -to NGNC -sizes "10,100,1000" -record testdata/snapshots
```

What happens during recording:

1. The `-record` flag creates a `snapshot.Recorder` and wraps the HTTP client.
   Every upstream call passes through the recorder before reaching the
   network.
2. The ladder runs normally — the recorder is transparent to the measurement
   logic.
3. After the run completes, the recorder writes:
   - A directory named `<send>-<recv>-<YYYYMMDDThhmmssZ>/` under the
     specified parent.
   - `manifest.json` — the snapshot's self-description (no measured values;
     provenance only).
   - `responses/<seq>-<slug>.json` — one file per recorded upstream response,
     containing the exact bytes from the wire.
4. If any size failed to reach an upstream (transport error, not NO-MARKET),
   the recorder refuses to write and the command exits with an error. A
   snapshot with a hole would replay as a corridor that is partly unpriced
   when the market was fine and the network was not — a fabricated finding.

### The dirty-tree check

```bash
$ go run ./cmd/ladder -to NGNC -record testdata/snapshots
refusing to record a snapshot from a modified working tree.
git_revision would name a tree that did not produce these bytes...

Modified:
  route/route.go
```

`git_revision` is the only link between a committed fixture and the code that
produced it. Recorded from a modified tree it names a revision that did not
generate the bytes — worse than recording nothing, because the field looks
like provenance.

Two workarounds:

- **Commit or stash first.** This is the correct choice for committed
  snapshots.
- **`-allow-dirty`.** Records anyway and sets `"dirty": true` in the
  manifest. For local experiments only.

### What the recorder captures

The recorder intercepts every `http.RoundTripper.RoundTrip` call and stores:

| Field | Source |
|:---|:---|
| `kind` | `"horizon"` or `"reference"`, classified by URL path |
| `method` | `GET` (all current upstreams) |
| `key` | `METHOD SP path?[sorted query]` — host-independent |
| `url` | The full URL as requested |
| `status` | HTTP status code |
| `content_type` | Response Content-Type header |
| `recorded_at` | Timestamp, truncated to the second |
| `body_file` | Path to the response file relative to the snapshot dir |
| `body_sha256` | `sha256:<hex>` of the verbatim response body |

The key is host-independent by design. A snapshot recorded against
`horizon.stellar.org` replays unchanged against an `httptest.Server` — this
is what makes offline tests possible.

---

## Step 2 — Understand the directory layout

After recording, you get:

```
testdata/snapshots/usdc-ngnc-20260821T223040Z/
    manifest.json
    responses/
        001-v1-currencies-usd.json
        002-paths-strict-send-250.json
        003-paths-strict-send-25.json
        004-paths-strict-send-2500.json
        005-paths-strict-send-5000.json
        006-paths-strict-send-10.json
        007-paths-strict-send-100.json
        008-paths-strict-send-500.json
        009-paths-strict-send-5.json
        010-paths-strict-send-0.1.json
        011-paths-strict-send-1.json
        012-paths-strict-send-1000.json
        013-paths-strict-send-50.json
```

**Directory name convention:** `<send code>-<recv code>-<RFC3339 basic, UTC>`.
Corridor first so a listing groups by corridor; timestamp second so it sorts
chronologically within one.

**Response file naming:** `<seq>-<slug>.json`. The sequence prefix keeps
capture order visible. The slug is for humans and carries no meaning to the
loader, which resolves files only through the manifest. Renaming a body file
breaks nothing; editing one is caught by its SHA-256 hash.

### manifest.json structure

```json
{
  "format": "wayfare.snapshot",
  "version": 1,
  "recorded_at": "2026-08-21T22:30:40Z",
  "git_revision": "b36a3af",
  "corridor": {
    "send":    {"code": "USDC", "issuer": "GA5ZSEJ…K4KZVN"},
    "receive": {"code": "NGNC", "issuer": "GASBV6W…FQGXZY6"},
    "reference_pair": "USD/NGN"
  },
  "sizes": ["0.1","1","5","10","25","50","100","250","500","1000","2500","5000"],
  "sources": {
    "horizon":   {"base_url": "https://horizon.stellar.org"},
    "reference": {"provider": "currency-api",
                  "base_url": "https://latest.currency-api.pages.dev/v1/currencies/"}
  },
  "interactions": [ ... ]
}
```

Key properties:

- **No money in the manifest.** Every figure is derived by reparsing the
  bodies. `sizes` are request inputs (decimal strings), not measurements.
- **`dirty` appears only when true.** Absent means the tree was clean.
- **`git_revision`** is the short commit hash at capture time. Refused if the
  tree is dirty (unless `-allow-dirty`).

---

## Step 3 — Load a snapshot in a test

### The load call

```go
snap, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
if err != nil {
    t.Fatal(err)
}
```

`Load` does the following:

1. Reads `manifest.json` and parses it.
2. Checks `format` is `"wayfare.snapshot"` and `version` is `1`. Unknown
   versions are **refused loudly**, never best-effort parsed.
3. Reads every response body file and verifies its SHA-256 hash against the
   `body_sha256` in the manifest. An edited fixture fails here rather than
   downstream.
4. Returns a `*Manifest` with the bodies indexed by key.

### Getting an HTTP client

```go
dexClient := &dex.Client{
    HorizonURL: "https://horizon.stellar.org",
    HTTPClient: snap.HTTPClient(),  // answers only from the snapshot
}
```

`snap.HTTPClient()` returns an `*http.Client` whose transport is a
`snapshot.Replayer`. Every HTTP call through this client:

- Looks up the request key (`METHOD SP path?[sorted query]`).
- Returns the recorded body if found.
- Returns `*snapshot.ErrNotRecorded` if not found — **never falls through
  to the network**. A test that drifts into hitting the network fails rather
  than passing intermittently.

`snap.Replay()` returns the bare `http.RoundTripper` if you need to wrap it
yourself.

### Full test example

Here is a complete test that measures a corridor offline using a snapshot:

```go
func TestNGNCLadderOffline(t *testing.T) {
    // 1. Load the snapshot.
    snap, err := snapshot.Load("../testdata/snapshots/usdc-ngnc-20260821T223040Z")
    if err != nil {
        t.Fatal(err)
    }

    // 2. Build a DEX client that answers from the snapshot.
    dexClient := &dex.Client{
        HorizonURL: "https://horizon.stellar.org",
        HTTPClient: snap.HTTPClient(),
    }

    // 3. Use a pinned reference rate (no network needed).
    refRate := refrate.NewStatic(map[string]decimal.Decimal{
        "USD/NGN": decimal.RequireFromString("1350.2568"),
    })

    // 4. Build the engine and run the ladder.
    eng := &route.Engine{
        DEX:     dexClient,
        RefRate: &refrate.Checked{Inner: refRate},
    }

    result, err := eng.Ladder(context.Background(), route.LadderRequest{
        SendAsset:      asset.USDC(),
        ReceiveAsset:   asset.NGNC(),
        Sizes:          route.DefaultSizes,
        ReferenceBase:  "USD",
        ReferenceQuote: "NGN",
    })
    if err != nil {
        t.Fatal(err)
    }

    // 5. Assert against the recorded measurement.
    if result.Integrity != route.IntegrityDirect {
        t.Errorf("Integrity = %s, want DIRECT", result.Integrity)
    }
    if result.Viable() {
        t.Error("a corridor with heavy loss must not be viable")
    }
}
```

The same pattern works for any package that accepts an `*http.Client`:

| Package | Field to set |
|:---|:---|
| `dex.Client` | `HTTPClient: snap.HTTPClient()` |
| `monitor.Scheduler` | Set `Engine.DEX.HTTPClient` |
| `checks.Check` (SEP-10, auth flags) | Pass the client to the check struct |
| `refrate` providers | Set `.Client` on the provider |

### Finding a snapshot by prefix

Tests commonly load a snapshot by corridor prefix rather than by exact
directory name, so re-recording a snapshot does not require editing every
test file:

```go
func loadSnapshot(t *testing.T, prefix string) *snapshot.Manifest {
    t.Helper()
    matches, err := filepath.Glob(filepath.Join(
        "../testdata/snapshots", prefix+"-*"))
    if err != nil || len(matches) == 0 {
        t.Fatalf("no snapshot matching %q; capture one with "+
            "cmd/ladder -record testdata/snapshots", prefix)
    }
    m, err := snapshot.Load(matches[0])
    if err != nil {
        t.Fatalf("loading snapshot: %v", err)
    }
    return m
}

// Usage:
m := loadSnapshot(t, "usdc-ngnc")
dexClient := &dex.Client{HTTPClient: m.HTTPClient()}
```

---

## Step 4 — Understand the replay guarantees

The replayer enforces several invariants that make snapshot-backed tests
reliable:

### Unrecorded requests error, never pass through

```go
_, err = snap.HTTPClient().Get(
    "https://horizon.stellar.org/paths/strict-send?source_amount=31337")
// err is *snapshot.ErrNotRecorded — the test fails rather than hitting
// the network.
```

This is the structural guarantee behind every snapshot-backed test. If the
replayer fell through to the network, tests would be live without saying so.

### Edited bodies fail on load

If someone edits a response file to flatter a corridor:

```go
snap, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
// err contains "the fixture has been edited since it was captured"
```

The SHA-256 pin catches this at load time rather than downstream.

### Unknown versions are refused

```go
// If manifest.json has "version": 2:
snap, err := snapshot.Load("…")
// err contains "version 2, but this build reads version 1; refusing to
// parse"
```

### Keys are host-independent

A key is `METHOD SP path?[sorted query]`. The host is deliberately excluded,
so one snapshot replays against an `httptest.Server`, against a deployment,
and against mainnet without being re-cut.

### Repeated requests are recorded once

The ladder prices each size and then re-prices some as slippage probes. The
same key genuinely recurs within one run. The recorder deduplicates by key,
keeping the directory readable.

---

## Step 5 — Commit the snapshot

### Verify before committing

```bash
# Run the offline test suite to confirm the snapshot is valid:
make offline-test

# Or run just the snapshot-related tests:
go test ./snapshot/ ./dex/ ./route/ ./monitor/ ./checks/
```

All of these run against the recorded bytes. If a body file was corrupted or
its hash does not match, `snapshot.Load` fails and the test reports it.

### Licensing

Check the reference provider's terms permit storing responses before
committing. The committed snapshots use
[`@fawazahmed0/currency-api`](https://latest.currency-api.pages.dev), which
is CC0-1.0 and therefore redistributable.

If a provider's terms do not permit storage, record the fetch in the manifest
and replay the rate leg from a `refrate.Static` pinned to the recorded mid,
**stating the substitution in the manifest** — never silently.

### What to commit

- The entire snapshot directory (`manifest.json` + `responses/`).
- Nothing else changes — snapshots are additive data, not code.

---

## Step 6 — How CI uses snapshots

### The offline-tests job

CI runs the full test suite inside a network namespace with no route out
(`.github/workflows/ci.yml`, job `offline-tests`):

```bash
unshare -rn bash -c 'ip link set lo up 2>/dev/null; go test -count=1 ./...'
```

This proves structurally that every test runs from recorded bytes. A test
that reaches out fails instead of passing intermittently.

### The full CI matrix

| Job | What it checks |
|:---|:---|
| `build` | `gofmt`, `go vet`, `go test -race`, `go build` |
| `lint` | `golangci-lint` (v2) |
| `offline-tests` | All tests in a network-blackout namespace |
| `docker` | Container image builds and the binary runs inside it |

---

## Recording a new corridor

When adding a new corridor to the test set:

1. **Research the corridor** following
   [docs/adding-a-corridor.md](adding-a-corridor.md).
2. **Record a snapshot:**
   ```bash
   go run ./cmd/ladder -to <CODE> -ref currency-api -record testdata/snapshots
   ```
3. **Verify the snapshot loads:**
   ```bash
   go test ./snapshot/ -run TestLoad
   ```
4. **Write tests** using the snapshot (see the test example in Step 3 above).
5. **Run the offline suite** to confirm everything passes without network:
   ```bash
   make offline-test
   ```

The three committed snapshots each exercise a different integrity state:

| Corridor | State | Why it is a required fixture |
|:---|:---|:---|
| USDC → NGNC | `DIRECT` | The corridor every published figure comes from |
| USDC → GHSC | `DERIVATIVE` | Every path routes through NGNC — the case a loss number hides |
| USDC → KESC | `NO-MARKET` | Zero paths; `_embedded.records: []`, not an error |

---

## Troubleshooting

### "no snapshot matching …"

The test helper could not find a snapshot directory matching the prefix.
Either:

- No snapshot has been recorded for this corridor yet. Record one with
  `cmd/ladder -record`.
- The prefix does not match the directory name. Check
  `testdata/snapshots/` for the exact name.

### "body does not match its recorded hash"

A response file was edited after recording. Either:

- Re-record the snapshot from a clean tree.
- Or, if the edit was intentional (e.g., testing a new parser), acknowledge
  that the fixture no longer represents a real upstream response.

### "version N, but this build reads version 1"

The snapshot was recorded against a newer version of the format than this
build supports. Update to the current code, or re-record against the
current version.

### "nothing was recorded"

The recorder captured zero interactions. This happens when:

- The HTTP client was not wired through the recorder.
- All requests were to hosts the recorder did not see (check the `Classify`
  function).

---

## Related documents

- [docs/snapshot-format.md](snapshot-format.md) — the format specification
  (nine rules, layout, manifest structure)
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants and conventions
- [docs/corridor-measurements.md](corridor-measurements.md) — published
  figures these snapshots make testable
- [docs/backlog.md](backlog.md) — contributor backlog, including snapshot-
  related issues (#30, #31, #54, #94)
