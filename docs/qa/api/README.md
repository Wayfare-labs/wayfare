# HTTP API QA

Reusable, consumer-perspective QA for the Wayfare HTTP API. Backlog section
**H — Cross-cutting: QA and reproducibility**:

| Issue | Question | Script | Recorded result |
|:---|:---|:---|:---|
| [#259](https://github.com/Wayfare-labs/wayfare/issues/259) | Does the deployed instance serve the history the repository claims? | [`run-history-verification.mjs`](run-history-verification.mjs) | [`results/history-verification.json`](results/history-verification.json) |
| [#260](https://github.com/Wayfare-labs/wayfare/issues/260) | Does every documented endpoint and parameter behave as `docs/api.md` says? | [`run-endpoints.mjs`](run-endpoints.mjs) | [`results/endpoints.json`](results/endpoints.json) |
| [#258](https://github.com/Wayfare-labs/wayfare/issues/258) | What does a cold start actually do, across more than one sample? | [`run-cold-start.mjs`](run-cold-start.mjs) | [`results/cold-start.json`](results/cold-start.json) |
| [#262](https://github.com/Wayfare-labs/wayfare/issues/262) | What does `/healthz` return while the instance is waking? | [`run-cold-start.mjs`](run-cold-start.mjs) | [`results/cold-start.json`](results/cold-start.json) |

The written findings are in [`../artifacts/`](../artifacts/).

Everything here drives the **deployed service over HTTP**. Nothing is a mock:
the bytes recorded are the bytes `server/api.go` and `server/trend.go`
produced, and the history comparison is against the committed `data/` chain.

## Requirements

Node 18+ (for the global `fetch` used by all three scripts) and Go, because
`run-history-verification.mjs` runs the repository's own verifier
(`cmd/wayfared -verify-store`) against `data/`. No `npm install` is needed —
the scripts have no dependencies.

## Run

```bash
# #260 — record every documented endpoint and parameter as a fixture
node docs/qa/api/run-endpoints.mjs

# #259 — verify the served history against the committed chain
node docs/qa/api/run-history-verification.mjs

# #258 / #262 — measure a cold start (idles the instance first; see below)
node docs/qa/api/run-cold-start.mjs --idle=900 --cycles=3
```

All scripts accept `--base=URL` (default
`https://wayfare-cdb9.onrender.com`, or `WAYFARE_BASE` in the environment).

`run-history-verification.mjs` also accepts `--data=DIR` (default the repo's
`data/`).

A script exits non-zero only on an internal error. A mismatch between what was
observed and what the documentation says is **recorded, not fatal** — the
mismatch is the finding, and the script prints a summary of the mismatches at
the end.

## What the cold-start script does, and its limits

Render's free plan sleeps an idle instance after fifteen minutes without
traffic. `run-cold-start.mjs` therefore **sends no traffic for `--idle`
seconds**, then issues a fixed six-request timeline and records each request's
status, wall time and transport outcome. It repeats for `--cycles` cycles and
writes `results/cold-start.json` after every cycle, so an interrupted run still
records what it saw.

Two limits are recorded in the artifact rather than papered over:

- **Sleeping is the platform's behaviour, not the script's.** A cycle whose
  first request answers instantly is recorded as "no cold start observed" — it
  does not prove the instance stayed awake and is not treated as a failure.
- **The sample size is the cycle count.** A distribution over three cycles is
  three samples, not thirty; the artifact says so.

Do not call the deployment from anything else while this script is idling, or
the instance will not sleep.

## Scope limits

- The comparison in `run-history-verification.mjs` is limited to the fields the
  public API exposes. The record hash itself is not on the wire, so the served
  bytes cannot be re-hashed from outside; the committed chain is verified
  locally instead, and `embed_test.go` verifies the embedded copy in CI.
- Which commit the running image was built from is not externally visible. The
  script proves the served history matches the repository's committed chain; it
  cannot, from outside, prove the image came from a particular commit.
- The harness never writes to anything but `docs/qa/api/results/`.
