// Record the cold-start behaviour properly (#258) and verify /healthz during
// a cold start (#262).
//
// The Render free plan sleeps an otherwise-idle instance after fifteen minutes
// without traffic. This script makes the instance idle for a configurable
// window, then issues a fixed sequence of requests and records each one's
// status, wall time and transport outcome. Repeating it across cycles produces
// a measured distribution rather than the single anecdote the backlog started
// from.
//
// Usage:
//   node docs/qa/api/run-cold-start.mjs [--base=URL] [--idle=900] [--cycles=2]
//
// Writes docs/qa/api/results/cold-start.json after each cycle, so an
// interrupted run still records what it observed.
//
// Scope limit, stated up front: sleeping is the platform's behaviour, not the
// script's. A cycle in which the first request answers instantly is recorded as
// "no cold start observed" — it does not prove the instance slept, and it is
// not treated as a failure.

import { join } from "node:path";
import { head, nowIso, parseArgs, request, resolveBase, resultsDir, writeJson } from "./lib.mjs";

// The wake observation timeline, in order. The first entry is the one that
// either wakes the instance or fails at the edge; /healthz is first because
// issue #262 is specifically about what the Render health check sees while the
// instance is waking.
const TIMELINE = [
  { name: "healthz-1", method: "GET", path: "/healthz" },
  { name: "healthz-2", method: "GET", path: "/healthz" },
  { name: "ui-root", method: "GET", path: "/" },
  { name: "healthz-3", method: "GET", path: "/healthz" },
  { name: "corridor-ngnc", method: "GET", path: "/api/corridor?to=NGNC" },
  { name: "healthz-4", method: "GET", path: "/healthz" },
];

const REQUEST_TIMEOUT_MS = 120000;

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function idleWait(seconds) {
  if (seconds <= 0) return;
  const until = Date.now() + seconds * 1000;
  console.log(`  idling for ${seconds}s (sending no traffic)…`);
  // Print a marker every 60s so a long run is visibly alive without touching
  // the network.
  while (Date.now() < until) {
    await sleep(Math.min(60000, until - Date.now()));
    const left = Math.max(0, Math.round((until - Date.now()) / 1000));
    if (left > 0) console.log(`    ${left}s of idle left`);
  }
}

async function runCycle(base, index) {
  const cycle = {
    cycle: index,
    idle_wait_seconds: null,
    first_request_at: null,
    requests: [],
  };
  for (const step of TIMELINE) {
    const rec = await request(step.method, base + step.path, { timeoutMs: REQUEST_TIMEOUT_MS });
    cycle.requests.push({
      name: step.name,
      method: step.method,
      path: step.path,
      status: rec.status,
      elapsed_ms: rec.elapsed_ms,
      started_at: rec.started_at,
      error: rec.error,
      body: rec.body,
    });
    if (cycle.first_request_at === null) cycle.first_request_at = rec.started_at;
  }
  const successful = cycle.requests.filter((r) => r.status === 200);
  cycle.first_request_failed = cycle.requests[0].status !== 200;
  cycle.first_request_ms = cycle.requests[0].elapsed_ms;
  cycle.first_success_ms = successful.length ? successful[0].elapsed_ms : null;
  cycle.healthz = cycle.requests.filter((r) => r.path === "/healthz");
  return cycle;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = resolveBase(args);
  const idle = args.idle === undefined ? 900 : Number(args.idle);
  const cycles = args.cycles === undefined ? 2 : Number(args.cycles);
  const startedAt = nowIso();

  console.log(`Wayfare cold-start measurement (#258) and /healthz cold start (#262)`);
  console.log(`Base:   ${base}`);
  console.log(`Idle:   ${idle}s per cycle (Render free plan sleeps after 15 minutes)`);
  console.log(`Cycles: ${cycles}`);
  console.log(`Started: ${startedAt}\n`);

  const out = {
    issue: "#258 and #262",
    base,
    idle_seconds: idle,
    cycles_requested: cycles,
    started_at: startedAt,
    note: "A request that fails at the connection stage is recorded with status null and an error string. A cycle whose first request answers instantly is recorded as observed-but-warm, not as a pass.",
    cycles: [],
  };
  const path = join(resultsDir, "cold-start.json");

  for (let i = 1; i <= cycles; i++) {
    console.log(`\n=== Cycle ${i}/${cycles} ===`);
    await idleWait(idle);
    const cycle = await runCycle(base, i);
    cycle.idle_wait_seconds = idle;
    out.cycles.push(cycle);
    out.finished_at = nowIso();
    writeJson(path, out);

    for (const r of cycle.requests) {
      const status = r.status === null ? `FAILED (${r.error})` : `HTTP ${r.status}`;
      console.log(`  ${r.name.padEnd(14)} ${status.padEnd(28)} ${r.elapsed_ms}ms  ${head(r.body, 80)}`);
    }
  }

  const cold = out.cycles.filter((c) => c.first_request_failed || c.first_request_ms > 2000);
  console.log(`\nCycles observed: ${out.cycles.length}`);
  console.log(`Cycles whose first request failed or took >2s: ${cold.length}`);
  console.log(`Recorded: ${path}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
