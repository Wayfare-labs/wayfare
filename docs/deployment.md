# Deployment

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
no bill, no credentials.

**A hosted instance** (the rest of this document) — serves the API and UI at a
public URL, and measures on its own schedule.

They are not alternatives so much as different products. Pick on what you need:

| | Scheduled CI | Hosted |
|:---|:---|:---|
| Measures continuously | yes | yes |
| History is publicly auditable | **yes — it is in the repo** | only via the running instance |
| Live `/api/corridor` and UI | no | yes |
| Cost | none | a few dollars a month |
| Credentials to manage | none | a Fly account |

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

Nothing to configure. The workflow needs `contents: write`, which it declares,
and runs on the repository's default token.

To measure on demand:

```bash
gh workflow run measure --repo <owner>/<repo>
```

The chain is verified **before** anything is committed. A chain that does not
verify must never reach the repository: appending to a broken one buries the
break under later valid records, and `runstore.Open` refuses to load it
afterwards.

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
| `-addr` | | `:8080` | Listen address |
| `-data` | `WAYFARE_DATA_DIR` | none | Run store directory; empty disables history |
| `-schedule` | | `6h` | Measurement interval; `0` disables the scheduler |
| `-serve` | | `true` | Serve HTTP; `false` runs the scheduler alone |
| `-timeout` | | `90s` | Per-corridor measurement timeout |
| `-verify-store` | | | Walk every chain and exit |
| `-log-level` | `WAYFARE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

No secrets. Nothing to rotate.

**A store that fails to open is fatal.** On a deployment with a volume
attached, that means the volume did not mount, and running anyway would record
nothing while the health check stayed green.

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
and one asserting a missing one is an error rather than a plausible number.

---

## Related

- [upstream-failures.md](upstream-failures.md) — what each upstream failure shape means
- [run-store.md](run-store.md) — the chain, and what verification proves
- [snapshot-format.md](snapshot-format.md) — recorded upstream bytes
- [CONTRIBUTING.md](../CONTRIBUTING.md) — project invariants
