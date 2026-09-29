# Verifying the committed chain independently — Issue #277

**Claim under test:** `wayfared -verify-store -data ./data` is the project's
strongest evidence claim; this artifact records an outside run of it and
whether the behaviour matches `docs/verify-store.md`.
**Tested by:** Buffy (autonomous agent), deliberately *outside* the project's
own test suite — the only project code executed is the documented binary
itself, plus the project's own `runstore` package in two explicitly-labelled
forge/inspect helpers.
**Environment:** macOS, local tree at commit `7b53469` (same commit cloned
fresh from upstream for the independent run — see §4).
**Timestamps:** main run 2026-09-27T21:59:36Z; failure-mode sweep
21:59Z–22:15Z; deployed chain-heads fetched 2026-09-27T22:21:23Z.
**Endpoint:** https://wayfare-cdb9.onrender.com/api/chain-heads

## What the issue asks

`wayfared -verify-store -data ./data` is the project's strongest evidence
claim; have someone outside the project run it and write down what they saw.
Constraints: report what was actually observed (contradictions are findings),
do not fix anything here, keep the artifact usable by someone who did not run
the test, and record timestamps and endpoints with every figure.

## Procedure (repeatable by anyone)

```bash
git clone https://github.com/Wayfare-labs/wayfare
cd wayfare
go run ./cmd/wayfared -verify-store -data ./data; echo "exit=$?"
```

Requires only the repo and Go 1.22+. No network is needed for the verification
itself; network was used only for the fresh clone, the toolchain download and
the deployed cross-check below.

## 1. The main run, as documented

```
$ go run ./cmd/wayfared -verify-store -data ./data
ok   USDC-GHSC: 1 records, latest 2026-08-22T12:10:05Z
ok   USDC-KESC: 1 records, latest 2026-08-22T12:10:09Z
ok   USDC-NGNC: 1 records, latest 2026-08-22T12:09:59Z
exit=0
```

(on stderr, before the verdict: two INFO log lines — `binding the port
assigned by the platform port=0` and `run store open dir=./data corridors=3`.
The doc's examples show stdout only; the verdict is stdout-only and therefore
script-friendly, which is worth knowing rather than surprising.)

Two checks the doc implies but the run itself does not show:

- **Nothing was written.** File mtimes of every file under `data/` were
  snapshotted before and after; identical. Confirms "does not write anything".
- **The ok lines are true.** Each `data/*.ndjson` file holds exactly one line,
  `seq: 1`, `prev_hash` equal to the genesis value
  (`sha256:000…0`), `version: 1` — and the printed `latest` timestamps match
  the files byte for byte. The chains are currently single-record chains (the
  measure workflow's push failure, #63, means the committed window is one
  record per corridor), so "1 records" is the store honestly describing the
  committed data, not a bug.

## 2. An outside verifier recomputing the hashes by hand

The chain's whole point is that verification is reproducible without trusting
the writer, so the hashes were recomputed in Python, outside all project code.

**First attempt — following the documentation literally — failed on all three
records.** `docs/run-store.md` and the `Record` doc comment say the preimage
is the record JSON *"with Hash omitted"*. Removing the `hash` field entirely
and hashing the remainder does not reproduce any stored hash.

**Root cause, established with an inspect helper** (a five-line Go program
calling the exported `runstore.Record.Preimage()`): the implementation keeps
the `hash` **field** in the preimage with its **value emptied** — the preimage
ends `…,"prev_hash":"sha256:000…0","hash":""}` — and renders with
`SetEscapeHTML(false)`.

**Second attempt, with that rule, from Python only:** parse the stored line,
set `hash` to `""`, re-serialise compact with field order preserved and raw
UTF-8, append `\n`, SHA-256. **All three records match their stored hashes**:

| Corridor | Stored hash | Recomputed | |
|:---|:---|:---|:---|
| USDC-NGNC | `sha256:424b33fcf120…44f` | `sha256:424b33fcf120…44f` | MATCH |
| USDC-GHSC | `sha256:974c3fac0854…d55` | `sha256:974c3fac0854…d55` | MATCH |
| USDC-KESC | `sha256:15ed4ac7d484…f55` | `sha256:15ed4ac7d484…f55` | MATCH |

A subtlety worth recording, because it will bite the next outside verifier:
the stored **line bytes** and the **preimage bytes** differ. `Append` writes
lines with `json.Marshal` (HTML escaping on), so NGNC/GHSC lines contain
`- Cumulative \u003e` — literally, `->` path arrows stored as `\u003e` —
while the preimage renders them unescaped. Hashing the stored line minus the
hash value matches only for KESC, whose record contains no escapable
character. The verdict `ok` is genuine (the verifier recomputes from the
parsed, unescaped form), but see Finding D1.

## 3. Documented failure modes, exercised on copies

Every case below was run on a copy under `/tmp`; the committed `data/` was
never modified. Exit codes captured per run; the `notadir` case was re-run
with explicit capture after a shell-sequencing artefact made its first exit
code ambiguous (observed both times: `FAIL` on stdout, exit 1).

| # | Scenario | Observed | Doc says | Verdict |
|:---|:---|:---|:---|:---|
| 1 | Record edited in place, hash untouched (`floor_loss_pct` 27.15→20.15) | `FAIL … record seq 1 has hash 424b33fcf120… but its contents hash to aaaed64a830b…; it was modified after it was written`, exit 1 | same wording, exit 1 | ✅ |
| 2 | Two-record chain, record 1 edited in place | `FAIL … record seq 1 … modified after it was written`, exit 1 | same, exit 1 | ✅ |
| 3 | Record 2 appended with `prev_hash` = `sha256:fff…f` (genuine broken link) | `FAIL … record seq 2 expects prev_hash ffffffffffff… but the previous record hashes to 424b33fcf120…; the chain is broken at position 1`, exit 1 | same wording ("chain is broken at position N"), exit 1 | ✅ |
| 4 | Malformed JSON line | `FAIL runstore: USDC-NGNC line 1: unexpected end of JSON input`, exit 1 | same shape, exit 1 | ✅ |
| 5 | Record with `version: 9` | `FAIL runstore: USDC-NGNC line 1 has record version 9, this build understands 3; refusing to guess at a schema it does not know`, exit 1 | same, exit 1 | ✅ |
| 6 | Empty chain file | `no corridor history to verify`, exit 0 | "nothing to verify" semantics | ✅ |
| 7 | Empty directory | `no corridor history to verify`, exit 0 | same, exit 0 | ✅ |
| 8 | Stray non-`.ndjson` file in the directory | ignored; three `ok` lines, exit 0 | (undocumented) | ✅ sensible |
| 9 | Leading blank lines in a chain file | ignored; `ok`, exit 0 | (undocumented) | ✅ sensible |
| 10 | `-data` pointing at a regular file | `FAIL runstore: creating /tmp/v277/notadir: mkdir …: not a directory`, exit 1 | "FAIL runstore: reading …", exit 1 | ✅ (wording differs: `creating`, not `reading`) |
| 11 | **Missing directory** | **`no corridor history to verify`, exit 0 — and the missing directory is created** | `FAIL runstore: reading ./data: <system error>`, exit 1 | ❌ **D2** |
| 12 | **No `-data` flag at all** | **silently verifies the embedded history** (same three `ok` lines), exit 0 | exit code 2: "no data directory configured" | ❌ **D3** |
| 13 | Whole-chain rewrite with honest re-sealing | verifies `ok`, exit 0 | documented limitation — "someone with write access can rewrite the whole chain from any point" | ✅ (demonstrated live: the forge helper re-appended records via the store's own `Append`, which re-links and re-seals anything it is given, and the result verifies) |

## 4. The independent run: fresh clone, pinned toolchain, copied data

To satisfy "someone outside the project" as strictly as the environment
allows:

1. `git clone --depth 1 https://github.com/Wayfare-labs/wayfare.git` → commit
   `7b53469fb5194437a3f0d1b9f2b0ab7f39eff435` (identical to the local tree).
2. Only the three chain files copied into the clone's `data/`; `diff -r`
   confirms the directories are byte-identical.
3. The CI-pinned toolchain (`GO_VERSION: "1.22"` in `ci.yml`) downloaded fresh
   via `golang.org/dl`: `go1.22.12`.

```
$ cd wayfare-clone && ~/go/bin/go1.22.12 run ./cmd/wayfared -verify-store -data ./data
ok   USDC-GHSC: 1 records, latest 2026-08-22T12:10:05Z
ok   USDC-KESC: 1 records, latest 2026-08-22T12:10:09Z
ok   USDC-NGNC: 1 records, latest 2026-08-22T12:09:59Z
exit=0
```

Identical output, identical exit code, on a toolchain the project's CI pins,
in a directory the project never wrote.

## 5. Cross-check against the deployed instance

`GET https://wayfare-cdb9.onrender.com/api/chain-heads` at
2026-09-27T22:21:23Z returned, for all three corridors, `seq: 1` and hashes
**identical to the local committed files**:

| Corridor | Deployed hash = local hash |
|:---|:---|
| USDC-GHSC | `sha256:974c3fac0854…` — match |
| USDC-KESC | `sha256:15ed4ac7d484…` — match |
| USDC-NGNC | `sha256:424b33fcf120…` — match |

The deployment serves the same chain tips the repository commits, and those
tips verify.

## 6. The CI claim

`docs/verify-store.md` states "CI runs this check on every push
(`.github/workflows/ci.yml`)". Observed in the workflows: `ci.yml:152` runs
`docker run --rm wayfare:ci -verify-store -data /tmp/empty` — a container
smoke check over an **empty** store, which exercises `no corridor history to
verify` and nothing else. The check over **real committed data** runs in
`measure.yml:69` (the measure workflow, not every push). The claim is true in
spirit (the binary is exercised on every push) and imprecise in letter — see
D4. The doc's companion claim, that every new build verifies chains at
startup via `Open`, is code-true (`OpenFS` verifies every chain it loads).

## Findings (reported, not fixed)

### D1 — The documented preimage recipe does not reproduce the hashes ⚠️

`docs/run-store.md` and the `Record` doc comment define the preimage as the
record JSON *"with Hash omitted"*, `SetEscapeHTML(false)`, trailing newline.
Two parts of that recipe do not match what was hashed:

1. **The hash field is not omitted — it is emptied.** The preimage keeps
   `"hash":""` (verified against the exported `Preimage()` output; the field
   is part of the hashed bytes).
2. **The stored line bytes are not the preimage bytes whenever a record
   contains `<`, `>` or `&`.** `Append` writes lines with plain
   `json.Marshal` (HTML escaping **on** → `\u003e`), while the preimage is
   rendered with escaping **off**. A non-Go verifier that hashes the stored
   line minus the hash value gets a different hash for USDC-NGNC and
   USDC-GHSC today (both contain `->` in `path` fields); USDC-KESC matches
   only because it contains no escapable character.

The project's own verifier is self-consistent, so `ok` is genuine and no
stored hash is wrong. But the README's provenance story — "anyone can clone
the repository and verify the hash chains" — currently requires knowing two
undocumented facts (emptied-not-omitted, and unescape-before-hash). A
stranger following the documented recipe gets three mismatches and no way to
tell a broken chain from a broken recipe.

**Reproduction:**

```bash
python3 - <<'EOF'
import json, hashlib, re
line = open('data/USDC-NGNC.ndjson').read().rstrip('\n')
# documented recipe: hash field omitted
pre = re.sub(r',"hash":"sha256:[0-9a-f]{64}"\}$', '}', line) + '\n'
print("documented recipe:", hashlib.sha256(pre.encode()).hexdigest())
print("stored           :", json.loads(line)['hash'])
EOF
```

Observe the two hashes differ (and note the `\u003e` bytes in the raw line).
The implementation-true recipe — empty the hash *value*, keep the field,
render unescaped — reproduces the stored hash. Fixing the documentation (or
the write path, so line bytes equal preimage bytes) is a small, separate
change; either is fine, but it should be one or the other.

### D2 — A missing `-data` directory exits 0 and is created ❌

**Doc:** "If the data directory is missing… `FAIL runstore: reading ./data:
<system error>` … the exit code is **1**."
**Observed (2026-09-27, build at `7b53469`):** with `-data /tmp/v277/missing`
(where the directory did not exist):

```
no corridor history to verify
exit=0
```

and the directory `/tmp/v277/missing` **now exists** — `runstore.Open` calls
`os.MkdirAll` (`runstore/file.go:47`) before reading. A verifier who
misspells the directory gets a green exit code and an empty directory created
as a side effect; the doc's failure mode is unreachable for the missing-case.

**Reproduction:** `go run ./cmd/wayfared -verify-store -data /tmp/does-not-exist-277; echo $?` →
`no corridor history to verify`, exit 0, directory created.

### D3 — `-verify-store` without `-data` verifies the embedded history; exit 2 is unreachable ❌

**Doc:** exit-code table: `2 | -verify-store was requested but no data
directory is configured`.
**Observed:** `go run ./cmd/wayfared -verify-store` (no `-data`) prints the
same three `ok` lines as the documented run and exits 0 — `openStore("")`
falls back to the history embedded in the binary (`wayfare.History`). The
exit-2 path exists (`store == nil → return 2`) but no flag combination
reaches it: an empty `-data` serves embedded history, and even an embedded
failure yields a `Nop` store that lists zero corridors → exit 0.

This fallback is arguably the *right* behaviour for a self-contained image —
but it is undocumented, and it means `-verify-store` can print `ok` for
embedded history when the operator believed they were verifying a data
directory. Worth either documenting or gating (e.g. require `-data` under
`-verify-store`).

**Reproduction:** `go run ./cmd/wayfared -verify-store; echo $?` → same `ok`
lines as with `-data ./data`, exit 0.

### D4 — The doc's "CI runs this check on every push" is imprecise ⚠️

`ci.yml` runs `-verify-store` only inside the container job, against
`/tmp/empty` — it proves the image starts, and exercises only the empty-store
path. The verification of real committed data runs in `measure.yml:69`, which
is not "every push". One sentence in `docs/verify-store.md` should split the
two claims. (Related cosmetic drift in the same doc: the version-refusal
example says "understands 2"; this build understands 3 — and the tamper
example shows a single `runstore:` prefix where the binary prints the prefix
twice, `FAIL runstore: USDC-NGNC: runstore: record seq 1 …`.)

## Method-level conclusion

**The claim holds.** The committed chains verify: in the working tree, in a
fresh clone of upstream at the same commit, on the CI-pinned Go 1.22.12
toolchain, with hashes independently reproduced outside the project's code —
and the deployed instance serves exactly the chain tips the repository
commits. Every documented tamper-detection behaviour works as written, with
the documented wording, on copies.

**But the strongest evidence claim has two sharp edges an outside verifier
will hit:** the documented preimage recipe produces mismatches (D1), and the
command's exit code does not distinguish "verified" from "verified nothing
because the path was wrong" (D2) or "verified the embedded copy, not your
data" (D3). None of these break the chain's guarantees; all three sit exactly
on the path a first-time independent verifier walks, which is what this issue
asked someone to walk.

## Honesty notes

- The committed chains are single-record chains; every "chain" observation
  above (link checks, position numbering) was exercised on forged copies under
  `/tmp` because the committed data has only one link per corridor. The forge
  helper used the project's own `runstore` package for honest sealing — except
  case 3 of §3, where record 2 was appended as raw bytes precisely so the
  link would be genuinely broken.
- Case 13 (§3) demonstrated the documented whole-chain-rewrite limitation
  inadvertently first: the forge helper's `Append` calls re-linked and
  re-sealed what it was given, producing a consistent chain. That is the
  store doing exactly what its documentation says it cannot detect.
- The first observation of the `notadir` exit code appeared as 0 due to a
  shell-sequencing artefact in the capture command; it was re-run with
  explicit redirection and captured exit 1 both times. The artifact records
  the re-run.
- `go1.22.12` was downloaded fresh from `golang.org/dl` for the clone run;
  the main runs used the local Go 1.26.7. Both agreed.
- The clone at `~/Desktop/wayfare-clone` and all `/tmp/v277` fixtures are
  disposable; nothing under the repository's `data/` was modified at any
  point.
