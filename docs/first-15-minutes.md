# First 15 minutes

Issue [#227](https://github.com/Wayfare-labs/wayfare/issues/227), backlog
`#167`.

From a fresh clone to a reproduced measurement in about fifteen minutes. Every
step either needs no network at all or performs one live measurement, and the
commands and expected output below were checked against the tree at `c9bfb75`
on 2026-09-24. If a step's output does not look like what is shown, see
[docs/troubleshooting.md](troubleshooting.md) before anything else.

The short version: **the whole test suite runs offline from recorded bytes
(step 2), the recorded store and snapshots reproduce on command (steps 3–4),
and one live `cmd/ladder` run is the only part that needs the network
(step 5).**

---

## Before you start

- Go 1.22 or later (nothing else is needed for the test suite; `go.mod` pins
  `go 1.22.2`, CI runs Go 1.22 — `docs/contributor-faq.md`).
- Internet access, needed only for step 5 and the checks it runs. Steps 2–4
  are fully offline by design (`docs/offline-testing.md`).

## Step 1 — clone and look around

```bash
git clone https://github.com/Wayfare-labs/wayfare
cd wayfare

ls                        # cmd/, route/, dex/, refrate/, checks/, server/, docs/
ls docs                   # the documents that answer "what, why, how"
```

Two minutes in, read the two documents a contributor is expected to read
before writing code:
[CONTRIBUTING.md](../CONTRIBUTING.md) (the invariants — hard constraints, not
style) and the [contributor FAQ](contributor-faq.md). You are now further
along than the README assumes.

## Step 2 — run the test suite (offline, no network)

```bash
make test
```

This is `go test ./...` and it runs entirely from recorded bytes and local
HTTP test servers — there is no network dependency, and CI structurally
enforces that in a network blackout namespace (`docs/offline-testing.md`).

**Expected output:** one `ok` line per package, in package order, e.g. the
final lines look like:

```
ok   github.com/Wayfare-labs/wayfare/server
ok   github.com/Wayfare-labs/wayfare/snapshot
ok   github.com/Wayfare-labs/wayfare/route
```

Nothing should reach out to the network (that would be a bug in the tree). If
a test fails here, fix your Go version first — a missing `go 1.22` toolchain
fails early with a language-version error, not a test failure.

What you have just proven: the arithmetic, verdict thresholds, reference
reconciliation, and replay fixtures all still agree with the committed recorded
bytes. That is the reproducibility claim stated in
[docs/snapshot-record-replay.md](snapshot-record-replay.md).

## Step 3 — verify the recorded measurement history (offline)

```bash
go run ./cmd/wayfared -verify-store -data ./data
```

This walks every corridor's hash chain and proves no recorded figure was
edited after it was written (`docs/verify-store.md`). In this checkout the
store holds exactly one record per corridor, so the expected output is:

```
ok   USDC-GHSC: 1 records, latest 2026-08-22T12:10:05Z
ok   USDC-KESC: 1 records, latest 2026-08-22T12:10:09Z
ok   USDC-NGNC: 1 records, latest 2026-08-22T12:09:59Z
```

Exit code `0` means every chain is clean; `1` means a chain is broken and the
offending corridor is named (`docs/verify-store.md`). The corridor order is
sorted, so it is stable across runs.

Each record's `hash` is the SHA-256 of the record with the hash field omitted,
and `prev_hash` links each record to its predecessor — editing any past line
breaks every line after it (`docs/run-store.md`).

## Step 4 — reproduce a published figure from recorded bytes (offline)

This is the shortest route from "clone" to "a number I re-derived myself".
`cmd/hop-analysis` replays the recorded snapshots — never touching the
network — and derives the hop-composition finding published in
[docs/native-xlm-routing.md](native-xlm-routing.md):

```bash
go run ./cmd/hop-analysis -snapshots ./testdata/snapshots
```

**Expected output:** a table per recorded corridor, ending with a summary
line in the exact shape `SEND -> RECEIVE: N/M sizes priced; best path
traverses XLM at K/N, and a non-XLM alternative exists at J/N.` For the
recorded USDC → NGNC snapshot (revision `b36a3af`, see
[docs/native-xlm-routing.md](native-xlm-routing.md)) that line is:

```
USDC -> NGNC: 12/12 sizes priced; best path traverses XLM at 10/12, and a non-XLM alternative exists at 12/12.
```

(The finding that doc publishes — native XLM routing beats the best non-XLM
alternative monotonically as size grows — is what this command re-derives from
the same bytes, so "clone → reproduce this figure" is now done with zero
trust.)

`testdata/snapshots/` deliberately carries one malformed fixture
(`strictsend-malformed-…`) that must be rejected; the tool skips it with a
`skip …: not one probe parsed` note on stderr and keeps going — that is the
fixture at work, not a problem (`cmd/hop-analysis/main.go:147-162`).

The snapshots' byte hashes are re-verified on load; recording a new snapshot
is refused on a dirty working tree unless `-allow-dirty` is passed, so the
provenance chain stays honest (`cmd/ladder/main.go:346-370`,
`docs/snapshot-record-replay.md`).

## Step 5 — one live measurement (needs network)

```bash
go run ./cmd/ladder -to NGNC -checks=false
```

Prices USDC → NGNC across twelve sizes (0.1 → 5000 USDC) against a live
reference mid. Both binaries need live network access by design — there are no
cached figures to fall back on (`README.md:424-426`). Expected shape of the
table header:

```
corridor USDC -> NGNC, benchmarked against USD/NGN
run at 2026-<date>T<time>Z

SEND     RECEIVE        RATE       LOSS%  VERDICT    INTEGRITY   PATH
0.1      ...            ...        ...    UNUSABLE   DIRECT      USDC -> ...
...
reference mid: <mid> USD/NGN via exchangerate-api, as of <timestamp>
```

The corridor historically is UNUSABLE at every size (documented, with the raw
table, in [docs/corridor-measurements.md](corridor-measurements.md)) — so do
not expect a `GOOD` verdict; the *finding* is that nothing is recommendable.
Three things to note:

- `-checks=false` skips the counterparty checks (anchor toml, SEP-10/SEP-24,
  issuer flags). With checks skipped the document carries **no** checks line
  at all — "not checked" deliberately must not look like "checked, nothing
  found" (`cmd/ladder/main.go:133-139`, `166-173`). Leave checks on the first
  time with `go run ./cmd/ladder -to NGNC` if you want the full document.
- The exit code is `1` when no size is worth recommending — a corridor that is
  unusable at every size is the normal, intended outcome, not an error
  (`cmd/ladder/main.go:208-213`).

JSON is the same document the HTTP API serves: `go run ./cmd/ladder -to NGNC
-json | jq '.integrity, .recommended'` — expect `recommended` to be `null`.

## If you want a server up

```bash
go run ./cmd/wayfared
```

serves the same engine at http://127.0.0.1:8080/ (measures live against
mainnet — a fresh sweep takes a few seconds). Endpoints:
[api.md](api.md).

## What was reproduced

| Claim | How you just reproduced it | Needs network |
|:---|:---|:---|
| The test suite is offline and green | `make test` | no |
| Recorded history was not edited | `-verify-store` (exit 0) | no |
| A published figure derives from recorded bytes | `hop-analysis` | no |
| The measurement pipeline runs live | `cmd/ladder` | yes |

## Where to go next

- [contributor-faq.md](contributor-faq.md) — what is and is not built
- [adding-a-corridor.md](adding-a-corridor.md) — the highest-value first
  contribution, and its own worked guide
- [backlog.md](backlog.md) — every known gap with evidence
- [troubleshooting.md](troubleshooting.md) — when something above did not look
  like it should

## Related

- [verify-store.md](verify-store.md), [run-store.md](run-store.md) — the hash
  chain and its verification
- [native-xlm-routing.md](native-xlm-routing.md) — the figure step 4
  reproduces
- [offline-testing.md](offline-testing.md) — why the suite is offline and how
  CI enforces it
- [snapshot-record-replay.md](snapshot-record-replay.md) — the record/replay
  loop