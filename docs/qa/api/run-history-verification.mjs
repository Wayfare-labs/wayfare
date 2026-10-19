// Verify the deployed instance against the repository it claims to be (#259).
//
// Reads the committed chain (data/*.ndjson), verifies it locally with the same
// Go verifier the binary uses, then fetches the deployed instance's served
// history and compares the two field by field. The comparison is deliberately
// limited to what the public API exposes and to the fields the hash chain
// seals; where the wire omits a stored field, that is recorded, not guessed.
//
// Usage:
//   node docs/qa/api/run-history-verification.mjs [--base=URL] [--data=DIR]
//
// Writes docs/qa/api/results/history-verification.json.

import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { nowIso, parseArgs, request, resolveBase, repoRoot, resultsDir, writeJson } from "./lib.mjs";

// eq treats "", null and undefined as the same "absent" value: the stored
// format writes "" where the wire omits the key, and a client cannot tell the
// two apart on the wire.
function eq(committed, served) {
  const norm = (v) => (v === undefined || v === null || v === "" ? "" : String(v));
  return norm(committed) === norm(served);
}

function cmpField(label, committed, served, diffs) {
  if (!eq(committed, served)) {
    diffs.push({ field: label, committed: committed ?? null, served: served ?? null });
  }
}

// compareRecordAgainstTrend compares one committed hash-sealed record against
// its served trend run, field by field. Only fields the trend wire carries.
function compareRecordAgainstTrend(rec, run, diffs) {
  cmpField("recorded_at", rec.recorded_at, run.recorded_at, diffs);
  cmpField("integrity", rec.integrity, run.integrity, diffs);
  cmpField("depends_on", (rec.depends_on || []).join(","), (run.depends_on || []).join(","), diffs);
  cmpField("reference.mid", rec.reference?.mid, run.reference?.mid, diffs);
  cmpField("reference.source", rec.reference?.source, run.reference?.source, diffs);
  cmpField("reference.as_of", rec.reference?.as_of, run.reference?.as_of, diffs);
  cmpField("reference.secondary_mid", rec.reference?.secondary_mid, run.reference?.secondary_mid, diffs);
  cmpField("reference.secondary_source", rec.reference?.secondary_source, run.reference?.secondary_source, diffs);
  cmpField("reference.divergence_pct", rec.reference?.divergence_pct, run.reference?.divergence_pct, diffs);
  cmpField("reference.scored_against", rec.reference?.scored_against, run.reference?.scored_against, diffs);
  cmpField("floor_loss_pct", rec.floor_loss_pct, run.floor_loss_pct, diffs);
  cmpField("floor_size", rec.floor_size, run.floor_size, diffs);
  cmpField("worst_loss_pct", rec.worst_loss_pct, run.worst_loss_pct, diffs);
  cmpField("worst_size", rec.worst_size, run.worst_size, diffs);
  cmpField("recommended_size", rec.recommended_size, run.recommended_size, diffs);
  cmpField("finding", rec.finding, run.finding, diffs);
  cmpField("rungs.length", (rec.rungs || []).length, (run.rungs || []).length, diffs);
  const n = Math.min((rec.rungs || []).length, (run.rungs || []).length);
  for (let i = 0; i < n; i++) {
    const a = rec.rungs[i];
    const b = run.rungs[i];
    cmpField(`rungs[${i}].send_amount`, a.send_amount, b.send_amount, diffs);
    cmpField(`rungs[${i}].priced`, a.priced, b.priced, diffs);
    cmpField(`rungs[${i}].loss_pct`, a.loss_pct, b.loss_pct, diffs);
    cmpField(`rungs[${i}].verdict`, a.verdict, b.verdict, diffs);
  }
}

function loadCommitted(dataDir) {
  const out = [];
  for (const f of readdirSync(dataDir)) {
    if (!f.endsWith(".ndjson")) continue;
    const key = f.slice(0, -".ndjson".length);
    const records = readFileSync(join(dataDir, f), "utf8")
      .split("\n")
      .map((l) => l.trim())
      .filter(Boolean)
      .map((l) => JSON.parse(l));
    out.push({ key, file: f, records });
  }
  return out.sort((a, b) => a.key.localeCompare(b.key));
}

function verifyChainLocally(dataDir) {
  try {
    const stdout = execFileSync("go", ["run", "./cmd/wayfared", "-verify-store", "-data", dataDir], {
      cwd: repoRoot,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "pipe"],
      timeout: 120000,
    });
    return { ok: true, command: `go run ./cmd/wayfared -verify-store -data ${dataDir}`, stdout: stdout.trim() };
  } catch (err) {
    return {
      ok: false,
      command: `go run ./cmd/wayfared -verify-store -data ${dataDir}`,
      stdout: String(err.stdout || "").trim(),
      stderr: String(err.stderr || err.message || err).trim(),
      exit_code: err.status ?? null,
    };
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const base = resolveBase(args);
  const dataDir = args.data ? String(args.data) : join(repoRoot, "data");
  const startedAt = nowIso();

  console.log(`Wayfare served-history verification (#259)`);
  console.log(`Base:    ${base}`);
  console.log(`Data:    ${dataDir}`);
  console.log(`Started: ${startedAt}\n`);

  const chain = verifyChainLocally(dataDir);
  console.log(`Local chain verification: ${chain.ok ? "ok" : "FAILED"} (${chain.command})`);
  console.log(chain.ok ? chain.stdout : `${chain.stdout}\n${chain.stderr}`);

  const corridors = loadCommitted(dataDir);
  const results = [];

  for (const c of corridors) {
    const [send, recv] = c.key.split("-");
    const trendUrl = `${base}/api/corridor/trend?from=${send}&to=${recv}&limit=500`;
    const trendRec = await request("GET", trendUrl, { timeoutMs: 30000 });
    const servedRuns = (trendRec.json && trendRec.json.runs) || [];

    const perRecord = [];
    const diffs = [];
    for (const rec of c.records) {
      const run = servedRuns.find((r) => r.seq === rec.seq);
      if (!run) {
        diffs.push({ field: `seq ${rec.seq}`, committed: "present", served: "missing" });
        perRecord.push({ seq: rec.seq, served: false });
        continue;
      }
      const rdiffs = [];
      compareRecordAgainstTrend(rec, run, rdiffs);
      // Compare per-record so a mismatch names the seq that carried it.
      for (const d of rdiffs) diffs.push({ seq: rec.seq, ...d });
      perRecord.push({ seq: rec.seq, served: true, mismatches: rdiffs.length });
    }

    const latest = c.records[c.records.length - 1];
    const corridorUrl = `${base}/api/corridor?from=${send}&to=${recv}`;
    const corridorRec = await request("GET", corridorUrl, { timeoutMs: 30000 });
    const served = corridorRec.json || {};
    const liveDiffs = [];
    cmpField("integrity", latest.integrity, served.integrity, liveDiffs);
    cmpField("floor_loss_pct", latest.floor_loss_pct, served.floor_loss_pct, liveDiffs);
    cmpField("floor_size", latest.floor_size, served.floor_size, liveDiffs);
    cmpField("worst_loss_pct", latest.worst_loss_pct, served.worst_loss_pct, liveDiffs);
    cmpField("worst_size", latest.worst_size, served.worst_size, liveDiffs);
    cmpField("live", false, served.live, liveDiffs);
    cmpField("measured_at", latest.recorded_at, served.measured_at, liveDiffs);
    cmpField("stale.recorded_at", latest.recorded_at, served.stale?.recorded_at, liveDiffs);
    cmpField("rungs.length", (latest.rungs || []).length, (served.rungs || []).length, liveDiffs);

    const match = diffs.length === 0 && liveDiffs.length === 0;
    results.push({
      corridor: c.key,
      committed_file: c.file,
      committed_records: c.records.length,
      served_records: servedRuns.length,
      trend_http_status: trendRec.status,
      corridor_http_status: corridorRec.status,
      match,
      trend_field_mismatches: diffs,
      history_first_field_mismatches: liveDiffs,
      per_record: perRecord,
      served_trend_body: trendRec.body,
      served_corridor_body: corridorRec.body,
    });

    const tag = match ? "MATCH" : "MISMATCH";
    console.log(`\n${tag}  ${c.key}  committed=${c.records.length} served=${servedRuns.length}`);
    for (const d of diffs) console.log(`  trend  seq ${d.seq}: ${d.field} committed=${JSON.stringify(d.committed)} served=${JSON.stringify(d.served)}`);
    for (const d of liveDiffs) console.log(`  corridor: ${d.field} committed=${JSON.stringify(d.committed)} served=${JSON.stringify(d.served)}`);
  }

  const out = {
    issue: "#259",
    base,
    data_dir: dataDir,
    started_at: startedAt,
    finished_at: nowIso(),
    chain_verification: chain,
    corridors_all_match: results.every((r) => r.match),
    corridors: results,
  };
  const path = join(resultsDir, "history-verification.json");
  writeJson(path, out);

  console.log(`\nCommitted chain verified locally: ${chain.ok ? "yes" : "no"}`);
  console.log(`All corridors match the committed chain: ${out.corridors_all_match ? "yes" : "no"}`);
  console.log(`Recorded: ${path}`);
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
