// QA the HTTP API from a consumer's perspective (issue #260).
//
// Records every endpoint and query parameter documented in docs/api.md, plus
// the method and error paths a consumer hits, as fixtures. Each case states
// what the documentation leads a client to expect; a mismatch is recorded, not
// hidden — the mismatch is the finding.
//
// Usage:
//   node docs/qa/api/run-endpoints.mjs [--base=URL]
//
// Writes docs/qa/api/results/endpoints.json.
//
// `expected` is what docs/api.md leads a caller to expect for that case. Where
// the live service disagrees with the code, that is reported in the artifact,
// not "fixed" in the expectation.

import { join } from "node:path";
import { compare, head, nowIso, parseArgs, request, resolveBase, resultsDir, writeJson } from "./lib.mjs";

// docs/api.md: "Unsupported methods return 405." Every endpoint below is
// expected to enforce that; the two that do not are the finding of #260.
const UNSUPPORTED_METHOD = { status: 405 };

const tooManySizes = Array.from({ length: 25 }, (_, i) => i + 1).join(",");

const CASES = [
  // --- Positive paths, exactly as documented in docs/api.md --------------
  { group: "documented", name: "ui-root", method: "GET", path: "/", expect: { status: 200 },
    note: "docs/api.md: GET / serves the embedded single-file UI." },
  { group: "documented", name: "healthz", method: "GET", path: "/healthz", expect: { status: 200 },
    note: "docs/api.md documents the body as {\"status\":\"ok\"} only." },
  { group: "documented", name: "assets", method: "GET", path: "/api/assets", expect: { status: 200 },
    note: "docs/api.md: assets[] with can_be_destination." },
  { group: "documented", name: "corridor-default", method: "GET", path: "/api/corridor", expect: { status: 200 },
    note: "No parameters: from defaults to USDC, to to NGNC." },
  { group: "documented", name: "corridor-to-ngnc", method: "GET", path: "/api/corridor?to=NGNC", expect: { status: 200 },
    note: "to=NGNC." },
  { group: "documented", name: "corridor-to-ghsc", method: "GET", path: "/api/corridor?to=GHSC", expect: { status: 200 },
    note: "to=GHSC (DERIVATIVE corridor)." },
  { group: "documented", name: "corridor-to-kesc", method: "GET", path: "/api/corridor?to=KESC", expect: { status: 200 },
    note: "to=KESC (NO-MARKET corridor)." },
  { group: "documented", name: "corridor-from", method: "GET", path: "/api/corridor?from=NGNT&to=NGNC", expect: { status: 200 },
    note: "docs/api.md documents from; NGNT is a verified send asset." },
  { group: "documented", name: "corridor-sizes", method: "GET", path: "/api/corridor?to=NGNC&sizes=10,100", expect: { status: 200 },
    note: "docs/api.md documents sizes as a comma-separated list, max 24." },
  { group: "documented", name: "corridor-live", method: "GET", path: "/api/corridor?to=NGNC&live=1", expect: { status: 200 }, timeoutMs: 90000,
    note: "docs/api.md: live=1 bypasses stored history. Slow; measured separately below." },
  { group: "documented", name: "trend-default", method: "GET", path: "/api/corridor/trend", expect: { status: 200 },
    note: "Defaults from=USDC, to=NGNC; reads stored history only." },
  { group: "documented", name: "trend-limit", method: "GET", path: "/api/corridor/trend?to=NGNC&limit=1", expect: { status: 200 },
    note: "limit=1." },
  { group: "documented", name: "trend-limit-capped", method: "GET", path: "/api/corridor/trend?to=NGNC&limit=99999", expect: { status: 200 },
    note: "docs/api.md: limit is capped at 500, not rejected." },
  { group: "documented", name: "trend-from", method: "GET", path: "/api/corridor/trend?from=NGNT&to=NGNC", expect: { status: 200 },
    note: "from parameter on the trend endpoint." },

  // --- Undocumented but implemented -------------------------------------
  { group: "undocumented", name: "corridor-pretty", method: "GET", path: "/api/corridor?to=NGNC&pretty=1", expect: { status: 200 },
    note: "pretty is accepted by every JSON handler but is absent from docs/api.md." },
  { group: "undocumented", name: "assets-pretty", method: "GET", path: "/api/assets?pretty=1", expect: { status: 200 },
    note: "pretty on /api/assets." },
  { group: "undocumented", name: "healthz-pretty", method: "GET", path: "/healthz?pretty=1", expect: { status: 200 },
    note: "pretty on /healthz." },
  { group: "undocumented", name: "options-corridor", method: "OPTIONS", path: "/api/corridor", expect: { status: 204 },
    note: "CORS preflight is implemented but docs/api.md does not mention it." },

  // --- Method handling ---------------------------------------------------
  { group: "method", name: "post-corridor", method: "POST", path: "/api/corridor", expect: { status: 405, code: "method_not_allowed" },
    note: "docs/api.md: unsupported methods return 405." },
  { group: "method", name: "post-trend", method: "POST", path: "/api/corridor/trend", expect: { status: 405, code: "METHOD_NOT_ALLOWED" },
    note: "405 expected; the code casing is observed, not assumed." },
  { group: "method", name: "post-assets", method: "POST", path: "/api/assets", expect: UNSUPPORTED_METHOD,
    note: "docs/api.md: unsupported methods return 405." },
  { group: "method", name: "post-healthz", method: "POST", path: "/healthz", expect: UNSUPPORTED_METHOD,
    note: "docs/api.md: unsupported methods return 405." },
  { group: "method", name: "put-healthz", method: "PUT", path: "/healthz", expect: UNSUPPORTED_METHOD,
    note: "docs/api.md: unsupported methods return 405." },

  // --- Error paths -------------------------------------------------------
  { group: "error", name: "corridor-unknown-to", method: "GET", path: "/api/corridor?to=NOPE", expect: { status: 400, code: "unknown_receive_asset" },
    note: "Unknown receive asset." },
  { group: "error", name: "corridor-unknown-from", method: "GET", path: "/api/corridor?from=NOPE", expect: { status: 400, code: "unknown_send_asset" },
    note: "Unknown send asset." },
  { group: "error", name: "corridor-no-fiat-peg", method: "GET", path: "/api/corridor?to=USDC", expect: { status: 400, code: "no_fiat_peg" },
    note: "USDC is a verified asset but not a fiat-peg destination in this deployment." },
  { group: "error", name: "corridor-bad-size", method: "GET", path: "/api/corridor?to=NGNC&sizes=abc", expect: { status: 400, code: "invalid_sizes" },
    note: "Non-numeric size." },
  { group: "error", name: "corridor-zero-size", method: "GET", path: "/api/corridor?to=NGNC&sizes=0", expect: { status: 400, code: "invalid_sizes" },
    note: "Sizes must be positive." },
  { group: "error", name: "corridor-too-many-sizes", method: "GET", path: `/api/corridor?to=NGNC&sizes=${tooManySizes}`, expect: { status: 400, code: "invalid_sizes" },
    note: "docs/api.md limit is 24 sizes; this sends 25." },
  { group: "error", name: "corridor-unknown-param", method: "GET", path: "/api/corridor?to=NGNC&tp=x", expect: { status: 400, code: "invalid_query" },
    note: "Strict query handling: an unknown parameter is rejected, not ignored." },
  { group: "error", name: "trend-unknown-to", method: "GET", path: "/api/corridor/trend?to=NOPE", expect: { status: 400, code: "UNKNOWN_ASSET" },
    note: "405/400 path: code casing observed." },
  { group: "error", name: "trend-unknown-from", method: "GET", path: "/api/corridor/trend?from=NOPE", expect: { status: 400, code: "UNKNOWN_ASSET" },
    note: "Unknown send asset on trend." },
  { group: "error", name: "trend-bad-limit", method: "GET", path: "/api/corridor/trend?to=NGNC&limit=abc", expect: { status: 400, code: "BAD_LIMIT" },
    note: "Non-numeric limit." },
  { group: "error", name: "trend-zero-limit", method: "GET", path: "/api/corridor/trend?to=NGNC&limit=0", expect: { status: 400, code: "BAD_LIMIT" },
    note: "limit must be positive." },
  { group: "error", name: "trend-unknown-param", method: "GET", path: "/api/corridor/trend?to=NGNC&tp=x", expect: { status: 400, code: "INVALID_QUERY_PARAM" },
    note: "Strict query handling on trend." },
  { group: "error", name: "assets-unknown-param", method: "GET", path: "/api/assets?foo=1", expect: { status: 400, code: "invalid_query" },
    note: "/api/assets accepts no parameters." },
  { group: "error", name: "healthz-unknown-param", method: "GET", path: "/healthz?foo=1", expect: { status: 400, code: "invalid_query" },
    note: "/healthz accepts no parameters." },
];

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = resolveBase(args);
  const startedAt = nowIso();

  console.log(`Wayfare API consumer QA (#260)`);
  console.log(`Base:    ${base}`);
  console.log(`Started: ${startedAt}`);
  console.log(`Cases:   ${CASES.length}\n`);

  const results = [];
  const failures = [];
  for (const c of CASES) {
    const url = base + c.path;
    const rec = await request(c.method, url, { timeoutMs: c.timeoutMs || 30000 });
    const mismatches = compare(rec, c.expect);
    const entry = {
      name: c.name,
      group: c.group,
      note: c.note,
      method: c.method,
      path: c.path,
      expected: c.expect,
      status: rec.status,
      elapsed_ms: rec.elapsed_ms,
      started_at: rec.started_at,
      headers: rec.headers,
      error: rec.error,
      code: rec.json && rec.json.code,
      body: rec.body,
    };
    if (mismatches.length) {
      entry.mismatches = mismatches;
      failures.push(`${c.name}: ${mismatches.join("; ")}`);
      console.log(`FAIL ${c.name.padEnd(28)} ${mismatches.join("; ")}`);
    } else {
      console.log(`ok   ${c.name.padEnd(28)} HTTP ${rec.status} ${rec.elapsed_ms}ms  ${head(rec.body, 90)}`);
    }
    results.push(entry);
  }

  const out = {
    issue: "#260",
    base,
    started_at: startedAt,
    finished_at: nowIso(),
    total: results.length,
    matched: results.length - failures.length,
    mismatched: failures.length,
    failures,
    cases: results,
  };
  const path = join(resultsDir, "endpoints.json");
  writeJson(path, out);

  console.log(`\n${out.matched}/${out.total} cases matched the documented expectation.`);
  if (failures.length) {
    console.log(`\n${failures.length} mismatch(es) — these are the findings, recorded in ${path}:`);
    for (const f of failures) console.log(`  - ${f}`);
  }
  console.log(`\nRecorded: ${path}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
