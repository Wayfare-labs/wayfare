# The local development loop

What each `make` target actually runs, what it needs installed, and how the
loop maps onto what CI does. `CONTRIBUTING.md` lists the targets; this document
explains them.

**Checked against the code at commit `c9bfb75`, 2026-09-24.** Every command
below was read from [`Makefile`](../Makefile) and
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml), not from memory.
Where a target needs something installed that the repository does not vendor,
it says so.

---

## What you need

| | Required for | Notes |
|:---|:---|:---|
| **Go 1.22 or later** | everything | `go.mod` pins `go 1.22.2`; CI pins `1.22`; the image builds on `golang:1.22-alpine`. |
| **`golangci-lint` v2** | `make lint` only | Not a Go dependency and not installed by anything here. `.golangci.yml` declares `version: "2"`, so a v1 binary rejects the config outright. CI pins `v2.1.6`. |
| **`docker`** | `make docker-build` only | Read from the target; not exercised by `make all` or by the pre-PR sequence. |
| **`unshare` (Linux)** | `make offline-test` only | Ships with `util-linux`. On macOS and Windows this target cannot run — see [docs/offline-testing.md](offline-testing.md). |

The only module dependencies are `shopspring/decimal` and `BurntSushi/toml`
(`go.mod`). Nothing else is fetched at run time, and no test reaches the
network — every test that would reach out replays recorded bytes or stands up
an in-process `httptest` server. That is why `make test` works with no connection
at all; [docs/offline-testing.md](offline-testing.md) is the full argument.

---

## The targets

`make` with no argument is the `all` target, because it is declared first:
`fmt vet test build`. It does not run the race detector or the linter.

| Target | Runs | Needs | When to run it |
|:---|:---|:---|:---|
| `make` / `make all` | `go fmt ./...`, `go vet ./...`, `go test ./...`, `go build -o bin/ ./...` | — | The quick "is this tree sane" pass. |
| `make build` | `go build -o bin/ ./...` | — | Compile every package and binary into `bin/` without testing. `bin/` is created if absent. |
| `make test` | `go test ./...` | — | The full unit suite. Serial: no `-race`, no `-count`. |
| `make race` | `go test -race ./...` | cgo-capable toolchain | Anything touching concurrency. `refrate.Cached` and `route.Engine.Ladder` both price concurrently, so a change there belongs here. |
| `make vet` | `go vet ./...` | — | Cheap static check; part of `make all`. |
| `make fmt` | `go fmt ./...` | — | **Mutates files.** Run it before `git add`, not after. |
| `make lint` | `golangci-lint run` | `golangci-lint` v2 | The linter CI runs. The target checks for the binary first and exits 1 with a link to the install page if it is missing, so a missing linter fails loudly rather than silently passing. |
| `make offline-test` | `go build ./...`, then `go test -count=1 ./...` inside `unshare -rn` | Linux user namespaces | The structural proof that no test needs the network. Mirrors the CI job of the same name. |
| `make cover` | `go test -coverprofile=coverage.out ./...`, then `go tool cover -html` into `coverage.html`, then prints the total | — | When you want to see which branches a change left uncovered. Writes two files at the repo root. |
| `make run` | `go run ./cmd/ladder` | **Live network** | Measuring the default corridor against mainnet. See the exit-code warning below. |
| `make docker-build` | `docker build -t wayfare:local .` | `docker` | Before changing anything about how the monitor is packaged or deployed. |
| `make clean` | `rm -rf bin coverage.out coverage.html` | — | Removes everything the loop writes: the build output and both coverage files. Nothing else. |
| `make help` | grep over the Makefile's `## ` comments | — | Lists the targets with their one-line descriptions. The list is generated from the file, so it cannot drift from it. |

`GO` is a variable, not a constant: `make test GO=go1.23` runs the suite under
a different toolchain without editing the file.

### `-count=1` and what it is for

`make test` and `make race` do not pass `-count=1`, so the Go test cache may
answer a second identical run without re-running it. `make offline-test` does
pass it, deliberately: the CI job wants the tests to actually execute inside
the network blackout, and a cached result would make the job pass without
anything having run.

If you are debugging a test that "passes when it should not", run
`go test -count=1 <package>` by hand.

---

## `make run` measures live, and exits 1 on a broken corridor

`make run` is `go run ./cmd/ladder`, which prices USDC → NGNC against live
mainnet Horizon and a live reference rate. Two things about it surprise people
the first time:

**It needs the network.** There are no cached figures to fall back on, by
design. With no route out it fails rather than returning a plausible number.
What the failure looks like, and what to do about it, is
[docs/live-measurement-failures.md](live-measurement-failures.md).

**It exits 1 when nothing is recommendable, and on the default corridor that
is the expected result.** `cmd/ladder` exits non-zero whenever no size produced
a quote graded `POOR` or better (`cmd/ladder/main.go`, the `result.Viable()`
check at the end of `main`). USDC → NGNC is documented as `UNUSABLE` at all
twelve sizes ([docs/corridor-measurements.md](corridor-measurements.md)), so
`make run` finishes with exit status 1 and `go run` reports `exit status 1`.
That is the product thesis working, not a broken checkout: the exit code is
there so a script can detect a broken corridor without parsing prose.

Use it to see the shape of a measurement, and read the exit code as part of
the output:

```bash
make run            # USDC -> NGNC; 0 when a size is recommendable, 1 when none is
```

The other entry points into a live measurement:

```bash
go run ./cmd/ladder -to GHSC          # a different corridor
go run ./cmd/ladder -to GHSC -json    # the shared wire shape, on stdout
go run ./cmd/ladder -checks=false     # skip the counterparty checks
go run ./cmd/wayfared                 # serve on :8080 and measure every 6h
go run ./cmd/wayfared -serve=false    # scheduler only, no HTTP
```

`-json` is the same document `/api/corridor` returns — both go through
`route.ToCorridorJSON` and `route.WithFindings`, and a test
(`TestLadderJSONMatchesToCorridorJSON`) asserts it. `-checks=false` skips the
counterparty checks and the JSON then carries **no** findings block, which is a
different claim from "checked, nothing found".

---

## What CI runs, and the one command that mirrors it

CI is four jobs in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml),
all on Ubuntu with Go `1.22`:

| CI job | Steps |
|:---|:---|
| `build` | `gofmt -l .` (fails and prints a diff if any file is unformatted), `go vet ./...`, `go test -race ./...`, `go build ./...` |
| `golangci-lint` | `golangci-lint` `v2.1.6` via the pinned action |
| `tests run with no network` | `go mod download`, lift the AppArmor user-namespace restriction, then `unshare -rn bash -c 'ip link set lo up; go test -count=1 ./...'` |
| `container image builds` | `docker build -t wayfare:ci .`, then run the built binary with `-verify-store` to prove it starts |

Two consequences worth knowing:

- **`gofmt` is checked, not applied.** CI runs `gofmt -l` and fails on any
  output; it never reformats for you. `make fmt` is the fix, and it must be
  run before committing — a `gofmt` failure is the most common avoidable red
  build.
- **CI uses the race detector on every push** (`go test -race`), while
  `make all` does not. A change that only passes plain `go test` can still fail
  CI.

`CONTRIBUTING.md` gives the pre-PR sequence. Read as one line:

```bash
make fmt vet test race lint offline-test
```

That is the whole loop. `make all` is a strict subset of it (`fmt vet test
build`); `make run` is not part of it, because a live measurement is not a
verification step and its exit code is a finding about the corridor rather than
about your change.

### When a check is unavailable locally

- `make lint` fails immediately if `golangci-lint` is absent. Install it — the
  target prints the install link — or accept that the lint job is the one CI
  job you cannot reproduce locally.
- `make offline-test` fails with `unshare: unshare failed` on a host that
  restricts unprivileged user namespaces, and on macOS/Windows `unshare` does
  not exist at all. [docs/offline-testing.md](offline-testing.md) covers the
  sysctl for Ubuntu 24.04 and the containerised substitute
  (`docker run --net=none ...`) for everything else.

---

## Where the loop writes

Everything the loop produces is disposable and confined to the repo root:

| Path | Written by | Removed by |
|:---|:---|:---|
| `bin/` | `make build`, `make all` | `make clean` |
| `coverage.out`, `coverage.html` | `make cover` | `make clean` |

`make run` and `go run ./cmd/wayfared` are read-only against the repository
unless you pass a flag that writes. `cmd/ladder -record <parent>` is the one
exception: it writes a snapshot directory, and refuses to do so from a
modified working tree unless `-allow-dirty` is passed
([docs/snapshot-record-replay.md](snapshot-record-replay.md)).

---

## Related documents

- [CONTRIBUTING.md](../CONTRIBUTING.md) — the pre-PR checklist and the
  invariants a change must not break
- [docs/live-measurement-failures.md](live-measurement-failures.md) — what to
  do when `make run` fails, and how to tell which upstream refused
- [docs/offline-testing.md](offline-testing.md) — the offline requirement, how
  CI enforces it, and what a test that reaches out looks like
- [docs/contributor-faq.md](contributor-faq.md) — the questions that come up
  most, answered against the code
- [docs/backlog.md](backlog.md) — the contributor backlog; entry #168 (issue
  [#228](https://github.com/Wayfare-labs/wayfare/issues/228)) is this document
