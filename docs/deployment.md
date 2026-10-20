# Deployment

## The Staging Story

Wayfare does not maintain a dedicated persistent staging environment running automated background schedulers, because corridor monitoring relies on deterministic recorded histories or live probes against production upstreams. 

When testing changes in a staging capacity:
- **Fly.io Staging**: Deploy with a separate app name (e.g., `wayfare-staging`) using `fly.toml` with `auto_stop_machines = true` if continuous persistence is not required for the transient test.
- **Render Staging**: Spin up a separate web service on Render pointing to the repository, utilizing the same immutable container image (`Dockerfile`) and running with `-schedule=0 -history-first`.



How to run Wayfare continuously, what it costs, and what to check afterwards.

The target is [Fly.io](https://fly.io). Nothing here is Fly-specific except
`fly.toml` — the binary is a static Go executable with one writable directory,
and runs anywhere that offers those.

---

## Why it must run continuously

A monitor that measures only while somebody has a page open is not a monitor.
Its history would have holes exactly where nobody was looking, which is
precisely when a corridor breaking is most worth having recorded.

That is why `monitor.Scheduler` imports nothing from `server` and runs with no
server at all. **The same failure can arrive through infrastructure**, and the
settings below exist to prevent it.

---

## Two ways to run it continuously

**Scheduled CI** (`.github/workflows/measure.yml`) — a workflow measures every
six hours and commits the hash-chained store back to the repository. No host,
no bill, and one repository secret ([below](#running-the-ci-path)).

**A hosted instance** (the rest of this document) — serves the API and UI at a
public URL, and measures on its own schedule.

They are not alternatives so much as different products. Pick on what you need:

| | Scheduled CI | Hosted |
|:---|:---|:---|
| Measures continuously | yes | yes |
| History is publicly auditable | **yes — it is in the repo** | only via the running instance |
| Live `/api/corridor` and UI | no | yes |
| Cost | none | a few dollars a month |
| Credentials to manage | one repository secret (`MEASURE_TOKEN`) | a Fly account |

The CI path has one genuine advantage over hosting, and it is not cost.
**Anyone can clone the repository and run `wayfared -verify-store -data ./data`
themselves.** The chain is the evidence, and it is in their hands rather than
on a server they have to trust. A hosted instance asks you to believe its
history; a committed chain lets you check it.

Its limitation is equally real: a scheduled workflow is best-effort. GitHub
delays or drops cron runs under load, so the cadence is approximate and gaps
are possible. Every record still carries its own `recorded_at`, so a gap is
visible rather than silently smoothed over — but do not read the schedule as a
guarantee.

### Running the CI path

**One secret, `MEASURE_TOKEN`, and nothing else.** The CI path used to
advertise no credentials at all; that turned out to be the reason it did not
work. [#555](https://github.com/Wayfare-labs/wayfare/issues/555) ran red on
every scheduled sweep from 2026-08-22 to 2026-10-17 — *GitHub Actions is not
permitted to create or approve pull requests* — and the setting that would
silence that message is not sufficient on its own.

#### The route, and why it is this one

Measurements land on `main` through one standing pull request from
`automation/measure`. [ADR 008](adr/008-why-measurements-stay-on-main.md)
records why the data is not on a branch of its own instead.

Opening that pull request needs a token that is **not** `GITHUB_TOKEN`, for two
independent reasons:

1. Without the repository setting *Allow GitHub Actions to create and approve
   pull requests*, the API refuses the call outright.
2. Events raised by `GITHUB_TOKEN` do not start workflow runs. A pull request
   opened — or a head pushed — by `GITHUB_TOKEN` never gets a CI run, so the
   checks `main` requires stay pending forever and the measurement can never
   merge. Enabling the setting does not change this.

The second reason is the one that decides it. The workflow checks out, pushes
and calls `gh` as `secrets.MEASURE_TOKEN`.

#### The token and its scope

Either a **GitHub App installation token** (preferred — short-lived, bound to
the installation, nothing to rotate by hand) or a **fine-grained personal
access token**. Scoped to this repository only:

| Permission | Level | What needs it |
|:---|:---|:---|
| Contents | read and write | pushing `automation/measure` |
| Pull requests | read and write | opening and updating the standing pull request |

Nothing else. In particular **not** Actions, Administration, Workflows, or any
organization-level permission. Store it as the repository secret
`MEASURE_TOKEN`. A fine-grained PAT expires; set a reminder, because an expired
token presents as the same red run as a missing one — and the alert described
below is what makes that visible.

Why a scoped secret rather than the repository setting: the setting is
ambient. It grants *every* workflow in the repository the ability to create and
approve pull requests, including ones that run with a write token on
`pull_request_target` — `auto-merge.yml` does. A secret is granted only to the
workflow that names it, and is never available to a run from a fork.

#### With the secret absent

The workflow falls back to `GITHUB_TOKEN`. The sweep still measures, verifies
and **pushes to `automation/measure`**, so no record is lost; it then fails on
the pull request with an error naming this section. A fork that only wants the
measurements in its own history needs nothing configured.

#### What the branch does

Every run fetches `automation/measure` and adopts its `data/` before
measuring, so a sweep extends the chain the previous run left there rather
than replacing it ([#556](https://github.com/Wayfare-labs/wayfare/issues/556)).
The branch is then rewritten as one commit on top of `main` and force-pushed
under a lease pinned to the SHA that run fetched: non-destructive, because the
tree being pushed contains every record the branch carried, and a branch that
moved under the run fails the push rather than overwriting whoever moved it.

The inherited chain is verified before the sweep appends to it. A chain that
does not verify is a **hard stop**, not a reason to start again from `main` —
starting over would discard the pending sweeps, and appending would bury the
break under later valid records.

To measure on demand:

```bash
gh workflow run measure --repo <owner>/<repo>
```

To see where the pipeline stands:

```bash
gh run list --workflow measure.yml --limit 5
gh pr list --head automation/measure
```

The chain is verified **before** anything is committed. A chain that does not
verify must never reach the repository: appending to a broken one buries the
break under later valid records, and `runstore.Open` refuses to load it
afterwards.

---

### Redeploying when measurement data lands

The hosted Render deployment serves history embedded at build time
(`-schedule=0 -history-first`, docs/embedded-history.md), so a committed sweep
does **not** freshen the served data by itself: it only reaches readers once a
new image is deployed. `.github/workflows/deploy.yml` closes that gap — on
every push to `data/` on `main` it calls Render's deploy hook, which builds and
deploys a fresh image embedding the new `data/`.

The hook is kept as the repository secret `RENDER_DEPLOY_HOOK` and is never
logged: the workflow posts to it with `curl --silent` and never echoes the URL,
and GitHub Actions redacts any secret that leaks into job output. Add the
Render deploy hook URL as that secret in the repository settings; the workflow
needs no other credentials.

Between redeploys the instance serves the history embedded in the image it is
running, so `/healthz`'s `freshness.newest_record_at` advances only when a
data commit is deployed — check it after a sweep lands:

```bash
curl -s https://<app>.onrender.com/healthz | jq '.freshness.newest_record_at'
```

---

## First deploy

```bash
fly auth login
fly launch --no-deploy          # accept the existing fly.toml
fly volumes create wayfare_data --region fra --size 1
fly deploy
```

Then verify it is actually working:

```bash
curl https://<app>.fly.dev/healthz
curl "https://<app>.fly.dev/api/corridor?to=KESC" | jq '.integrity, .live'
```

`KESC` should report `"NO-MARKET"` and `true`. Come back a few hours later
**without opening the page in between** and check that history grew:

```bash
fly ssh console -C "/wayfared -verify-store -data /data"
```

If the record count increased while nobody was watching, the scheduler is
genuinely running. That is the one check that distinguishes a monitor from a
web page with a calculator behind it.

---

## Settings that are load-bearing

### Auto-stop must stay off

```toml
auto_stop_machines = false
min_machines_running = 1
```

Fly's auto-stop suspends idle machines. For a request-driven app that is
correct and saves real money. Here it is fatal: **a sleeping machine runs no
scheduler.** Enabling it would produce a history with gaps that correlate
exactly with nobody watching.

Treat this as a requirement, not a default.

### Region: `fra` or `ams`, not Africa

Fly egress is **$0.02/GB in Europe and North America** and **$0.12/GB in
Africa** — six times the price. Deploying near Lagos buys nothing: the readers
are anywhere, and the upstreams (Horizon, the rate feeds) are not in Africa
either. The corridor being measured is Nigerian; the machine measuring it does
not need to be.

### No dedicated IPv4

A dedicated IPv4 is about **$2/month** against a total bill of a few dollars —
a material fraction of the cost for no benefit. Shared IPv4 serves standard
HTTPS fine. Only allocate one if something specifically needs it.

### Back the run store up off Fly

The hash chain proves nothing was edited. **It does not survive the volume
dying.**

Fly volume snapshots are around $0.08/GB-month with five-day default
retention, and they live on Fly — same provider, same account, same blast
radius. Copy the NDJSON somewhere else on a schedule:

```bash
fly ssh console -C "cat /data/USDC-NGNC.ndjson" > backup/USDC-NGNC.ndjson
```

Object storage or a commit to a separate repository both work; pick whichever
is simpler to operate, because a backup that is annoying to run does not get
run.

**Document and rehearse the restore.** A backup nobody has restored from is a
hypothesis:

```bash
# Restore, then prove the chain survived the round trip.
fly ssh sftp shell
put backup/USDC-NGNC.ndjson /data/USDC-NGNC.ndjson

fly ssh console -C "/wayfared -verify-store -data /data"
```

A restore that does not verify is a corrupted history, and appending to it
would bury the break under later valid records — `Open` refuses for exactly
this reason.

### Set a spend alert before the first deploy

Fly bills several meters separately — machines, volumes, snapshots, IPv4, and
egress — and **volumes bill even while the machine is stopped**. The
compounding is what surprises people, not any single line. Set the alert first;
it costs nothing and the failure it prevents is discovering the shape of your
bill a month late.

---

## What it costs

Roughly, at the settings above:

| Item | Approximate |
|:---|---:|
| `shared-cpu-1x`, 256MB, always on | ~$2/month |
| 1GB volume | ~$0.15/month |
| Egress | negligible at this traffic |
| **Total** | **a few dollars a month** |

Sized for a service whose entire job is a few dozen HTTP requests every six
hours.

---

## Load on upstreams

One sweep is roughly three dozen Horizon calls per corridor — twelve sizes,
each with pathfinding plus a slippage probe — across three corridors, so about
110 requests every six hours. That is negligible for Horizon at this cadence.

The reference providers are hit roughly **once per run** rather than once per
rung, because `refrate.Cached` collapses a ladder's twelve identical rate
fetches into one. Without it a single sweep would make 36 rate requests, and a
free tier would notice. That works out to a few hundred requests a month across
both providers.

Neither provider needs an API key. That is deliberate: a keyed provider would
put a secret into the deployment, and this service holds none.

---

## Configuration

| Flag | Env | Default | Meaning |
|:---|:---|:---|:---|
| `-addr` | `PORT` | `:8080` | Listen address; defaults to `0.0.0.0:$PORT` when `PORT` is set, and the flag overrides it |
| `-data` | `WAYFARE_DATA_DIR` | none | Run store directory; empty disables history |
| `-schedule` | | `6h` | Measurement interval; `0` disables the scheduler |
| `-serve` | | `true` | Serve HTTP; `false` runs the scheduler alone |
| `-timeout` | | `90s` | Ceiling on a live measurement. Each request's budget is 10s plus 6s per rung, capped at this value, and is returned in the `X-Measurement-Budget` header. A timeout returns `504` with code `upstream_timeout`, the `budget`, and `rungs_completed`. |
| `-shutdown-grace` | | `30s` | On SIGTERM, how long in-flight requests (a live ladder) get to finish before the server closes |
| `-verify-store` | | | Walk every chain and exit |
| `-export` | | | Export one corridor's verified history to stdout and exit, e.g. `-export USDC-NGNC` |
| `-format` | | `csv` | Export format for `-export`; `csv` is the only implemented format |
| `-rotate-store` | | | Trim every chain over the window ceiling and exit |
| `-rotate-records` | | `366` | Per-corridor record ceiling for `-rotate-store` |
| `-check-fresh` | | | Report corridors whose newest record is older than `-max-age` (or missing) and exit |
| `-max-age` | | `12h` | Staleness ceiling for `-check-fresh`; default is two missed sweeps |
| `-log-level` | `WAYFARE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

No secrets. No credentials to rotate — the store rotates itself, trimming chains
over the window ceiling; see [run-store.md](run-store.md#rotating-a-chain).

**A store that fails to open is fatal.** On a deployment with a volume
attached, that means the volume did not mount, and running anyway would record
nothing while the health check stayed green.

---

## Alerting when a scheduled measurement fails

A scheduled sweep that stops is the one failure mode that is invisible from
the outside: the server keeps answering from history, the workflow that
didn't run produces no failure log, and the gap is noticed days later. So
detection has two independent halves.

**In-process.** `wayfared -once` exits non-zero when the sweep itself fails
(all corridors failed). The scheduled `measure` workflow gates on that, so a
failing sweep is a red run.

**Out-of-process.** A red run only exists if the schedule fired at all. To
catch the cases no in-process failure can report — the cron never ran, the
workflow was disabled, or the measurement PR was rejected — run the
staleness detector on any independent clock (cron, another workflow, an
uptime check):

```bash
go run ./cmd/wayfared -check-fresh -data ./data -max-age 12h
```

It reads only the store — no network — and exits `0` when every corridor has
a record within `-max-age`, `1` when any corridor is stale or has never
recorded, and `2` when the check itself could not run (unreadable store,
non-positive `-max-age`). The checked set is the scheduler's expected
corridors unioned with whatever the store holds, so a corridor whose history
vanished fails exactly like a stale one: unknown is never a silent pass. A
non-zero exit from either half should be made loud by the runner.

The `measure` workflow does this itself
([#558](https://github.com/Wayfare-labs/wayfare/issues/558)): its last two
steps keep **exactly one** tracking issue open while the sweep is failing, and
close it on the next success.

| | |
|:---|:---|
| Opens at | two consecutive failures — twelve hours, the same ceiling `-check-fresh` uses by default, and the point at which a gap stops being explainable by one transient upstream error |
| Counts by | reading the workflow's own run conclusions back from the Actions API, so the count cannot drift from what happened. A success ends the streak; a cancelled or skipped run is stepped over rather than counted or treated as a reset |
| Finds the issue by | a `<!-- measure-alert -->` marker in its body, not a title or a label — so a renamed issue is still found and a second one is never opened |
| Updates | that issue's body, listing the most recent failing runs and where to look first |
| Closes | on the next successful run, with a comment naming it |

It uses `gh` and `GITHUB_TOKEN` with `issues: write`: no third-party action,
and no state stored outside the issue itself. Detection stays idempotent in
the sense [spike-alerting-semantics.md](spike-alerting-semantics.md) requires
— the same state produces the same single issue however many times it is
evaluated.

---

## Serving when a measurement fails

If a live measurement fails and history exists, the API serves the most recent
stored run, explicitly labelled:

```json
{ "live": false,
  "stale": { "recorded_at": "…", "age_seconds": 21600, "age_human": "6h ago" } }
```

`live` is on **every** response — `true` on a fresh measurement — so a client
that ignores the field cannot mistake a stored reading for a current one by its
absence.

With no stored run, the request **errors**. Nothing is ever synthesised to fill
the gap. This is the one place a continuous monitor can quietly betray the
project, so it has explicit tests: one asserting a stale response is labelled,
and one asserting a missing one is an error rather than a plausible number. On
a `-history-first` deployment the missing-history case is answered before any
upstream is contacted — `404` with `code: "no_history"` — so an unswept corridor
is never reported as an outage (`docs/errors.md`).

---

## Structured logging and retention

`wayfared` emits structured key-value logs using Go's standard `log/slog` library to `stderr`. Logging verbosity is controlled by `-log-level` or the `WAYFARE_LOG_LEVEL` environment variable (`debug`, `info`, `warn`, `error`; defaults to `info`).

### Emitted log fields by subsystem

Log lines follow standard `slog` key-value format (`time=... level=... msg="..." key=value`).

#### 1. HTTP server request boundary (`server/api.go`)

Every corridor measurement query writes a boundary log attributing the measurement to the HTTP request:

| Field | Meaning | Example |
|:---|:---|:---|
| `msg` | Event description | `"corridor measured"` |
| `method` | HTTP request method | `GET` |
| `path` | Request URL path | `/api/corridor` |
| `from` | Send asset code | `USDC` |
| `to` | Receive asset code | `NGNC` |
| `sizes` | Rung sizes query parameter | `0.1,1,10,100` (or empty for default) |
| `live` | Fresh live measurement (`true`) vs served from history (`false`) | `true` |
| `integrity` | Structural classification | `DIRECT`, `DERIVATIVE`, `NO-MARKET`, `UNKNOWN` |
| `duration` | Total elapsed request handling time | `245ms` |

#### 2. Rate limiter (`server/ratelimit.go`)

When a client exceeds the per-client request threshold:

| Field | Meaning | Example |
|:---|:---|:---|
| `msg` | Event description | `"request rate limited"` |
| `client` | Client IP address or masked identifier | `127.0.0.1` |
| `path` | Requested URL path | `/api/corridor` |
| `retry_after` | Cooldown interval before next permitted request | `1.2s` |

#### 3. Scheduler sweeps (`monitor/monitor.go`)

Background monitoring sweeps record corridor health and storage outcomes:

| Field | Meaning | Example |
|:---|:---|:---|
| `msg` | Event description | `"corridor measured"`, `"corridor measurement failed"`, `"recording measurement failed"` |
| `corridor` | Corridor identifier key | `USDC-NGNC` |
| `integrity` | Structural classification from ladder | `DIRECT` |
| `floor_loss_pct` | Lowest loss percentage measured across rungs | `4.31` |
| `worst_loss_pct` | Highest loss percentage measured across rungs | `97.23` |
| `scored` | Whether reference benchmarks agreed | `true` |
| `reference` | Primary benchmark rate provider | `exchangerate-api` |
| `agreement` | Cross-benchmark comparison outcome | `AGREE`, `DIVERGED`, `UNAVAILABLE` |
| `took` | Ladder measurement latency | `1.85s` |
| `error` | Upstream network or storage error | `measuring USDC-NGNC: context deadline exceeded` |

#### 4. Process lifecycle and storage management (`cmd/wayfared`)

Startup, port binding, and maintenance commands emit lifecycle events:

- `msg="binding the port assigned by the platform"`: `port` (e.g. `8080`)
- `msg="listening"`: `addr`, `schedule`
- `msg="run store open"`: `dir`, `corridors`
- `msg="serving embedded history (read-only)"`: `corridors`
- `msg="scheduler starting"`: `interval`, `corridors`
- Maintenance failures: `msg="opening run store"`, `msg="listing corridors"`, `msg="rotating store"`, `msg="-check-fresh requires a store; none is configured"`

### Destination and retention on Render

- **Log destination:** On Render, `wayfared` runs inside a Docker container. All `stdout` and `stderr` output is automatically captured by Render's log aggregation pipeline and streamed in real-time in the **Logs** tab of the service dashboard.
- **Log streams (forwarding):** For long-term archiving or external querying, Render supports configuring **Log Streams** in the dashboard settings to forward syslog-formatted logs over TLS to external aggregators (Better Stack, Datadog, Papertrail, Logtail).
- **Retention periods by Render workspace plan:**
  - **Hobby (Free):** **7 days** retention. Older logs are permanently deleted.
  - **Pro:** **14 days** retention.
  - **Scale / Enterprise:** **30 days** retention.

---

## Checking for deployment drift against `render.yaml`

README's description of the live instance (`-schedule=0 -history-first`) is only true if the deployed service settings match `render.yaml`. Manual edits made in the Render dashboard drift silently over time.

To verify that the live service matches `render.yaml`:

### Automated check via `scripts/render-drift-check.sh`

The read-only drift checker compares start command flags (`dockerCommand`), environment variables (`WAYFARE_LOG_LEVEL`), plan, health check path, and region:

```bash
# Using an API key stored in an environment variable or secret:
export RENDER_API_KEY="rnd_..."
./scripts/render-drift-check.sh

# Or by explicitly specifying the service ID:
./scripts/render-drift-check.sh --service-id srv-c... --api-key "$RENDER_API_KEY"
```

Exit codes:
- `0`: Settings match `render.yaml` (no drift detected).
- `1`: Drift detected (one or more settings differ; differences are reported).
- `2`: Configuration or API error.

The script is strictly read-only and issues only `GET` requests to `https://api.render.com/v1/services`.

### Manual check via Render API

If checking manually without the script:

1. Retrieve service settings:
   ```bash
   curl -fsS -H "Authorization: Bearer $RENDER_API_KEY" \
     https://api.render.com/v1/services/<SERVICE_ID> | jq '.serviceDetails'
   ```
   Confirm that:
   - `dockerCommand` matches `/wayfared -addr 0.0.0.0:8080 -schedule=0 -history-first`
   - `plan` is `free`
   - `healthCheckPath` is `/healthz`
   - `region` is `frankfurt`

2. Retrieve environment variables:
   ```bash
   curl -fsS -H "Authorization: Bearer $RENDER_API_KEY" \
     https://api.render.com/v1/services/<SERVICE_ID>/env-vars | jq '.items'
   ```
   Confirm that `WAYFARE_LOG_LEVEL` is set to `info`.

---

## Related

- [cold-start-reliability.md](cold-start-reliability.md) — observed free-tier
    wake-up behavior and current retry boundary
- [embedded-history.md](embedded-history.md) — the deployed instance serves history embedded at build time; freshness depends on redeploys, not the scheduler
- [upstream-failures.md](upstream-failures.md) — what each upstream failure shape means
- [run-store.md](run-store.md) — the chain, and what verification proves
- [ADR 008](adr/008-why-measurements-stay-on-main.md) — why the committed chain
    is on `main` rather than a data branch, and what would reopen that
- [verify-store.md](verify-store.md) — running `-verify-store`, exit codes, and what broken chains look like
- [snapshot-format.md](snapshot-format.md) — recorded upstream bytes
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
