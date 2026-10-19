# Offline testing requirement

Wayfare requires the entire automated test suite to run completely offline, with
no outbound network access. This document explains the requirement, the CI
enforcement mechanism, how to write compliant tests, and how to diagnose
failures when a test attempts network egress.

---

## Why offline testing is required

Wayfare is a corridor-integrity monitor. Its conclusions rest on deterministic,
reproducible calculations over financial rates and ledger observations. A test
suite that communicates with external servers across the public internet fails
this requirement for several reasons:

1. **Determinism and reproducibility.** Foreign exchange rates, order book
   liquidity, and issuer configurations change continuously. A test checking
   arithmetic against live Horizon endpoints or public exchange rate APIs would
   produce different results over time, turning tests flaky and masking regressions.
2. **Network independence.** Flaky DNS resolution, remote API rate limits (such
   as HTTP 429), or third-party outages must never break automated verification
   or block pull requests.
3. **Integrity against silent drift.** If a test falls back to live network
   endpoints when a fixture is missing or stale, it may quietly pass while
   evaluating something entirely different from what was intended.

Every test in Wayfare is therefore designed to execute purely from committed
recorded bytes (`testdata/snapshots/`) or local in-process HTTP test servers
(`httptest.NewServer`).

---

## How CI enforces offline testing

The offline requirement is not enforced solely by convention or code review. It is
structurally enforced in GitHub Actions by the dedicated `offline-tests` job in
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml).

### The network blackout namespace

The job runs on an `ubuntu-latest` runner using Linux network namespaces:

```bash
unshare -rn bash -c 'ip link set lo up 2>/dev/null; go test -count=1 ./...'
```

- **`unshare -rn`**: Creates a new, isolated network namespace (`-n`) and user
  namespace mapping the current user to root (`-r`). Inside this namespace, no
  external network interfaces (such as `eth0` or `ens3`) exist.
- **`ip link set lo up`**: Brings up the loopback interface (`127.0.0.1` / `::1`)
  inside the namespace. Loopback is explicitly kept active because Go tests stand
  up local in-process HTTP servers via `httptest.NewServer` or bind to `127.0.0.1`.
  Tearing down loopback would cause local tests to fail for reasons unrelated to
  external network egress.
- **No external route**: Because there are no outbound routes or default
  gateways, any connection attempt to an external IP or DNS resolver fails
  immediately at the OS level.

### Why namespacing instead of firewall rules

An earlier version of the CI job attempted to block outbound traffic by dropping
ports 80 and 443 with `iptables` on the `OUTPUT` chain.

In GitHub Actions, the runner agent communicates back to the GitHub control
plane over HTTPS (port 443). Blocking outbound traffic at the host level severed
the Actions agent itself: the runner stopped reporting status, the job hung
until the 45-minute communication timeout, and no logs were ever uploaded.

Network namespacing with `unshare -rn` isolates the network blackout exclusively
to the subshell running the test command, leaving the runner agent on the host
with uninterrupted network connectivity.

### Module warming and AppArmor restrictions

Running tests inside a blackout namespace requires two pre-flight considerations
checked in [`.github/workflows/ci.yml`](../.github/workflows/ci.yml):

1. **Module download (`go mod download`):** Go module resolution and downloads
   require network access. CI executes `go mod download` before entering the
   network blackout. Once module dependencies are present in the Go cache, Go
   compilation and execution run completely offline without touching the network.
2. **AppArmor user namespace restriction:** Ubuntu 24.04 runners restrict
   unprivileged user namespaces by default (`kernel.apparmor_restrict_unprivileged_userns=1`).
   Without adjusting this, `unshare -rn` fails with `EPERM` when configuring
   `uid_map`. CI lifts this restriction via:
   ```bash
   sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0
   ```
   Lifting this restriction is preferable to running tests under `sudo`, because
   running under `root` resolves `$HOME` to `/root`, missing the warmed user
   module cache and attempting an outbound download that the blackout blocks.

---

## Diagnosing failures: what happens when a test reaches out

When a test violates the offline requirement, the failure mode depends on whether
the request passed through `snapshot.Replayer` or bypassed it directly.

### 1. Replayer miss: `snapshot.ErrNotRecorded`

When code uses a snapshot-backed HTTP client (`manifest.HTTPClient()` or
`manifest.Replay()`), the transport looks up the requested method and URL in the
snapshot's recorded interactions.

If the request was not recorded in the snapshot, `snapshot.Replayer.RoundTrip()`
deliberately **does not fall through to the network** (see
[`snapshot/replay.go`](../snapshot/replay.go) and rule 5 in
[`docs/snapshot-format.md`](snapshot-format.md#5-an-unrecorded-request-is-a-loud-error-never-a-network-passthrough)).
Instead, it returns a `*snapshot.ErrNotRecorded`:

```
snapshot: usdc-ngnc-20260821T223040Z has no recorded response for "GET /paths/strict-send?..."; 24 requests are recorded, including:
	GET /paths/strict-send?destination_account=...
	...
```

**Resolution:**
- If the test should use an existing recorded response, check that the request
  URL, path, and query parameters match the snapshot manifest exactly.
- If testing a new scenario, corridor, or query shape, record a new snapshot
  fixture using `cmd/ladder -record` following
  [`docs/snapshot-record-replay.md`](snapshot-record-replay.md).

### 2. Network bypass: `network is unreachable` or DNS timeout

If a test bypasses the snapshot replayer entirely (for example, by calling
`http.DefaultClient`, using `http.Get()`, or constructing a client that points
to `https://horizon.stellar.org` or a live reference rate API without a custom
`RoundTripper`), it will succeed when run in a standard development environment
with internet access.

However, in the CI `offline-tests` job or under `make offline-test`, the OS
network stack has no route out. The test will fail with low-level network errors:

- **DNS resolution failure:**
  ```
  dial tcp: lookup horizon.stellar.org: i/o timeout
  ```
  or
  ```
  dial udp [2606:4700:4700::1111]:53: connect: network is unreachable
  ```
- **Direct IP connection failure:**
  ```
  dial tcp 141.101.90.1:443: connect: network is unreachable
  ```

**Resolution:**
Inject a snapshot-backed transport or mock HTTP server into the client under
test instead of making direct external network calls.

---

## How to write tests that pass offline

All tests requiring external HTTP responses should use one of two patterns:

### Pattern A: Snapshot replayer (preferred for upstream ledger and rate data)

Use recorded responses from `testdata/snapshots/`:

```go
m, err := snapshot.Load("testdata/snapshots/usdc-ngnc-20260821T223040Z")
if err != nil {
    t.Fatalf("load snapshot: %v", err)
}

// manifest.HTTPClient() returns an *http.Client backed by manifest.Replay()
client := m.HTTPClient()

// Inject the client into the component being tested
c := dex.NewClient("https://horizon.stellar.org", client)
```

The replayer matches requests by HTTP method, path, and sorted query parameters,
returning the exact recorded response bytes without network interaction.

### Pattern B: In-process `httptest.Server` (for synthetic edge cases)

When testing specific HTTP status codes, malformed payloads, or transport errors
that are not captured in committed snapshots, use Go's standard `httptest` package:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte(`{"status":"ok"}`))
}))
defer srv.Close()

// srv.URL uses 127.0.0.1, which succeeds because loopback is up
client := srv.Client()
```

Because `ip link set lo up` is executed in the namespace, loopback connections to
`127.0.0.1` work normally.

---

## Running offline tests locally

Contributors can verify their changes locally before opening a pull request.

### On Linux (with user namespaces enabled)

Run the dedicated target in the [`Makefile`](../Makefile):

```bash
make offline-test
```

This target mirrors CI by compiling packages first and running:

```bash
unshare -rn bash -c 'ip link set lo up 2>/dev/null; go test -count=1 ./...'
```

*(Note: On systems where unprivileged user namespaces are restricted, such as
Ubuntu 24.04, ensure `kernel.apparmor_restrict_unprivileged_userns` is set to 0
via `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`.)*

### On macOS, Windows, or environments without user namespaces

The `unshare` utility and Linux network namespaces are specific to Linux. On
macOS and Windows:

1. **Standard test execution:** Running standard tests:
   ```bash
   go test -count=1 ./...
   ```
   exercises the `snapshot.Replayer` guarantees. Any request using the replayer
   that tries to hit an unrecorded URL will fail with `snapshot.ErrNotRecorded`.
2. **Containerized verification (optional):** To structurally verify complete
   network blackout on non-Linux platforms, run the tests inside Docker with
   networking disabled (`--net=none`):
   ```bash
   # Pre-download dependencies into a vendor directory or module cache volume
   docker run --rm -v "$PWD":/src -w /src --net=none golang:1.24 go test -count=1 ./...
   ```

---

## Scope boundary

This document describes the offline testing requirement and CI enforcement
contract as checked against the repository on 2026-09-11. It does not alter
verdict thresholds, integrity semantics, check composition rules, or the
run-record layout.

---

## Related documents

- [`snapshot-format.md`](snapshot-format.md) — format specification and invariance rules for recorded fixtures
- [`snapshot-record-replay.md`](snapshot-record-replay.md) — capturing and replaying snapshots end-to-end
- [`snapshot-workflow.md`](snapshot-workflow.md) — comprehensive snapshot lifecycle and test writing guide
- [`backlog.md`](backlog.md) — contributor backlog entry #171 (issue [#231](https://github.com/Wayfare-labs/wayfare/issues/231))
- [`CONTRIBUTING.md`](../CONTRIBUTING.md) — contributor invariants and pre-PR checklist
