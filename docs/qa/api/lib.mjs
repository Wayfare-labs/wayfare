// Shared helpers for the Wayfare HTTP API QA scripts.
//
// These scripts drive the real service over HTTP and record what it returns;
// nothing here mocks the product. They are dependency-free (Node 18+ global
// fetch) and write their results under docs/qa/api/results/ so the recorded
// set travels with the procedure that produced it.

import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

// Directory of this file: docs/qa/api/
export const scriptDir = dirname(fileURLToPath(import.meta.url));
// Repository root: docs/qa/api -> docs/qa -> docs -> <root>
export const repoRoot = fileURLToPath(new URL("../../../", import.meta.url));
export const resultsDir = join(scriptDir, "results");

export const DEFAULT_BASE = "https://wayfare-cdb9.onrender.com";

// parseArgs turns `--key=value`, `--flag` and bare positionals into an object.
export function parseArgs(args) {
  const out = { _: [] };
  for (const a of args) {
    const m = /^--([^=]+)(?:=(.*))?$/.exec(a);
    if (m) out[m[1]] = m[2] === undefined ? true : m[2];
    else out._.push(a);
  }
  return out;
}

export function resolveBase(args) {
  return String(args.base || process.env.WAYFARE_BASE || DEFAULT_BASE).replace(/\/+$/, "");
}

export function nowIso() {
  return new Date().toISOString();
}

const HEADER_KEYS = [
  "content-type",
  "allow",
  "access-control-allow-origin",
  "access-control-allow-methods",
  "cache-control",
];

// request performs one HTTP request and records status, a small header subset,
// wall time and the raw body. A transport failure is recorded, not thrown:
// a failed request is an observation.
export async function request(method, url, { timeoutMs = 30000, body = undefined } = {}) {
  const startedMs = Date.now();
  const startedAt = new Date(startedMs).toISOString();
  const ac = new AbortController();
  const timer = setTimeout(() => ac.abort(), timeoutMs);
  const rec = { method, url, started_at: startedAt };
  try {
    const res = await fetch(url, {
      method,
      redirect: "manual",
      signal: ac.signal,
      ...(body === undefined ? {} : { body }),
    });
    const text = await res.text();
    rec.status = res.status;
    rec.elapsed_ms = Date.now() - startedMs;
    rec.headers = {};
    for (const k of HEADER_KEYS) {
      const v = res.headers.get(k);
      if (v !== null) rec.headers[k] = v;
    }
    rec.body = text;
    try {
      rec.json = JSON.parse(text);
    } catch {
      rec.json = null;
    }
  } catch (err) {
    rec.status = null;
    rec.elapsed_ms = Date.now() - startedMs;
    rec.error = err && err.name === "AbortError"
      ? `timeout after ${timeoutMs}ms`
      : String((err && err.message) || err);
  } finally {
    clearTimeout(timer);
  }
  return rec;
}

// compare performs the expected-vs-observed check for one case and returns the
// list of failures (empty means it matched). `code` is compared against the
// JSON `code` field, which is the machine-readable contract clients switch on.
export function compare(rec, expected) {
  const failures = [];
  if (expected.status !== undefined && rec.status !== expected.status) {
    failures.push(`expected HTTP ${expected.status}, got ${rec.status}`);
  }
  if (expected.code !== undefined) {
    const got = rec.json && rec.json.code;
    if (got !== expected.code) failures.push(`expected code ${JSON.stringify(expected.code)}, got ${JSON.stringify(got)}`);
  }
  return failures;
}

export function writeJson(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, JSON.stringify(value, null, 2) + "\n");
}

// truncate large bodies for terminal output only; the result file keeps them.
export function head(s, n = 200) {
  s = String(s == null ? "" : s).replace(/\s+/g, " ").trim();
  return s.length > n ? s.slice(0, n) + "…" : s;
}
