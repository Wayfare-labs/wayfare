# Cold-start reliability

This note records what is known about the public Render instance waking from
the free plan's idle sleep. It is an operational observation, not a claim that
the service has automatic retry or readiness orchestration.

## Observation

On 2026-08-28, the sweep reported that the first request to the sleeping free
instance failed during connection setup and the second request succeeded in
0.6 seconds. Source: the sweep report supplied with this issue; the result was
not independently reproduced while writing this note.

That is a negative finding about the first request, not a measured wake-up
duration. The 0.6-second value describes the successful second request only.

## What the repository supports

The following points were checked against the repository on 2026-08-28:

- [`render.yaml`](../render.yaml) deploys the service on Render's free plan,
  uses `/healthz` as its health check, and starts it with
  `-schedule=0 -history-first`.
- [`server/index.html`](../server/index.html) includes a CSS-only loading state
  before the initial API response. The page fetches
  `/api/corridor?to=...&live=1`; a failed fetch is shown as an error and is not
  retried by the page.
- [`server/api.go`](../server/api.go) has no connection retry around the live
  measurement. It can return the most recent stored reading after a reachable
  request's measurement fails, but that fallback cannot help when the instance
  has not accepted the connection.
- `/healthz` returns HTTP 200 with `{"status":"ok"}` once the process is
  serving. It does not prove that a live corridor measurement will succeed.
- [`README.md`](../README.md) says the free instance sleeps after fifteen
  minutes without traffic and advises the reader to retry once. That advice is
  manual operational guidance, not an implemented retry policy.

## Practical interpretation

The first connection can fail before Wayfare's handler can return a labelled
stale reading or an error body. A reader should retry the live URL once after
that connection failure. Once the process is reachable, the existing loading
state gives the reader context while the initial request is pending.

The observation does not establish a guaranteed wake-up time, a success rate,
or that every first request fails. No such measurement was made here.

## Scope boundary

Adding automatic retry, a readiness endpoint that models upstream availability,
or changing the Render plan would be future work. None is described as built
here. This note does not change verdict thresholds, integrity semantics, check
composition, or the run-record layout.

## Decision: Free-instance keep-warm policy

**Decision:** We do **not** run an artificial keep-alive ping (such as a cron job
or scheduled monitor ping) to keep the Render free-tier instance awake.

**Reasons:**

1. **Terms of service and fair use:** Render's free tier policies explicitly
   forbid artificial keep-alive pingers designed to defeat the 15-minute idle
   sleep. Defeating the sleep mechanism abuses free-tier infrastructure.
2. **Quota exhaustion risk:** The free plan provides 750 free instance hours per
   calendar month shared across all services on the account. An instance kept
   awake 24/7 exhausts the monthly quota in ~31 days, leaving zero buffer and
   risking a full account service suspension near month-end.
3. **Observability integrity:** An artificial pinger masks cold-start latency and
   connection failures from measurement tools, preventing accurate recording of
   real-world user experience.
4. **Architectural mitigation:** The UI provides an embedded CSS-only cold-start
   loading indicator, and history-first caching ensures that once awake, responses
   are served in sub-20ms. Guidance in `README.md` and this document advises
   users to retry once after sleep.

## Related

- [`deployment.md`](deployment.md) — deployment choices and failure fallback
- [`upstream-failures.md`](upstream-failures.md) — upstream failure contracts
- [Issue #53](https://github.com/Wayfare-labs/wayfare/issues/53) — loading state