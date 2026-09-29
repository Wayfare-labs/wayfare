# Rolling back

How to return a deployment to an earlier image, and how to prove the chain it
serves still verifies afterwards.

**Status: implemented and tested.** Every command below exists in the current
tree, and the guarantee the procedure relies on — a newer chain loads under an
older build when the newer records were written in that older build's schema —
is asserted by `TestRollbackOlderBuildLoadsWithNewerRecords` in
`runstore/rollback_test.go` (issue #318).

---

## When you would do this

- A deploy broke something the tree does not fix forward cleanly.
- You want to compare behaviour between two images before deciding.
- An image was built from a commit whose `data/` held older embedded history
  (the normal case on the hosted deployment — see
  [embedded-history.md](embedded-history.md): freshness advances by redeploy,
  so an older image simply serves an older window of the chain).

Rolling back the image never means rolling back the measurements. Records are
facts about when the monitor looked and what it saw; a rollback changes which
code serves them, not which of them happened.

---

## The three situations, and what each means for the chain

### 1. Same schema, older history

The common case: the older image was built from a commit whose records are the
same **schema version** as the chain on disk. The measure workflow commits
records continuously, so "older image" usually means "older data window", not
older code.

The chain on disk — including records the older image has never seen — loads
and verifies unchanged. `runstore.Open` verifies every chain it is given
regardless of which build opened it; the chain is self-describing bytes, not
state carried in the process.

```bash
# roll back, then prove it
wayfared -verify-store -data ./data
```

Exit code 0 and an `ok` line per corridor: done. The only cost of the rollback
is that `/api/corridor/trend` now serves the window the older image embedded
rather than the newest records — visible in `stale.age_human`, never hidden.

### 2. Older schema, newer records

The measure workflow advanced the schema (say the tree moved to version 4 and
records on disk are now version 4), and you roll back to a version-3 image.

**This refuses at startup, on purpose.** The version-mismatch rule in
[run-store.md](run-store.md#the-version-mismatch-rule): a record version the
build does not recognise is an error, never a best-effort parse. An old binary
that guessed at a new schema would relabel or silently drop fields — and the
one field it guesses wrong may be the one that makes a published figure
reproducible.

What the operator sees is exactly the mismatch, named:

```
runstore: USDC-NGNC line 1 has record version 4, this build understands 3;
refusing to guess at a schema it does not know
```

and the process exits non-zero rather than serving a chain it half-remembers.
**The correct path forward is a data-side rollback, not a code-side guess:**
restore the `data/` directory from before the schema advance (the records are
committed to the repository, so the git history *is* the backup), or fix
forward.

The reverse direction is safe and tested: a **newer** build loads an **older**
chain, because every migration so far added its fields with `omitempty` after
every earlier field, so a version-1 or version-2 record encodes byte-for-byte
as it did when it was written. `TestRollbackNewerBuildLoadsWithOlderRecords`
pins this in the rollback direction: mixed chains — old records followed by
records written by the current build — verify end to end.

### 3. Volume attached, rollback with history on disk

A deployment with a writable `WAYFARE_DATA_DIR` keeps records across image
changes. Procedure:

```bash
# 0. Before rolling back: snapshot the chain as it stands
fly ssh sftp get /data/USDC-NGNC.ndjson ./backup/USDC-NGNC.ndjson   # per corridor
# (or wherever the volume lives; the point is a copy off the box)

# 1. Roll the image back to the previous tag
fly deploy --image registry.fly.io/wayfare:previous

# 2. Verify the chain the rolled-back image now serves
fly ssh console -C "/wayfared -verify-store -data /data"

# 3. Confirm the served data is what you expect — provenance, not vibes:
curl -s https://<your-instance>/healthz | jq '.data'
curl -s "https://<your-instance>/api/corridor?to=NGNC" | jq '{live, stale}'
```

The store refuses to open on a broken chain (`Open` verifies before serving),
so step 2 failing loudly *is* the safety net: a rollback that would serve
corrupt history never serves at all.

On the embedded-history deployment there is no volume to preserve — the
rolled-back image serves whatever `data/` was committed when it was built. See
[embedded-history.md](embedded-history.md) for how that window is chosen, and
ADR 007 for why the committed chain is a bounded window whose rotation
re-seals the surviving records.

---

## How to verify the chain after rolling back

One command, whichever situation you are in:

```bash
wayfared -verify-store -data ./data
```

What it does: walks every corridor's chain in the store the rolled-back image
would open, recomputes every record's hash, checks every `prev_hash` link, and
exits 0 on success, 1 on any failure. It never measures, never starts the
server, and never writes.

Full output modes, failure shapes and exit codes:
[verify-store.md](verify-store.md). The same check runs at every startup
(`Open` verifies) and in CI on every build (`embed_test.go`), so a rollback
that could not verify its history is caught before the first request is
served, not after.

---

## What verification proves here (and what it does not)

Verification proves the history the rolled-back image serves is the one that
was written — no record edited, no record dropped from the middle, links
intact. It does not prove the measurements were correct, and it does not
compare the window this image serves against the window the previous image
served: on the hosted deployment, an older image serving an older window is
working as designed, and the age is published in `stale.age_human` and
`/healthz` rather than being a fault to hunt.

One asymmetry worth knowing when comparing two images' answers: rotation
(ADR 007) re-seals the surviving window's hash fields, so the *tip hash*
published by `/api/chain-heads` is not comparable across images built at
different times. The measured fields — the loss, the verdict, the timestamp —
are identical; only the commitment moves when the window head moves. Pin
readings by `recorded_at` plus measured fields, not by tip hash, across a
rollback boundary.

---

## The tests behind this document

`runstore/rollback_test.go` (added for issue #318) drives the file store the
way a rollback does:

- **Older build, newer records:** a chain written by the current build loads
  and verifies through a fresh `Open` — the "rollback" image's load path —
  with every record it never wrote intact. This is situation 1.
- **Newer build, older records:** a mixed chain of version-1-shaped and
  current-version records verifies end to end, and the old records keep their
  legacy hashes. This is the safe direction of situation 2, and the same
  guarantee the version-mismatch rule refuses to extend to unknown versions.
- **Fresh store, empty dir:** the rolled-back image on a fresh volume starts
  an empty chain cleanly, and the first append after rollback continues the
  new chain from genesis.

## Related

- [verify-store.md](verify-store.md) — the verification command in full
- [run-store.md](run-store.md) — the chain, the preimage rule, version migrations
- [embedded-history.md](embedded-history.md) — why an older image serves an older window
- [deployment.md](deployment.md) — the deployment this procedure operates on
- [adr/007-why-the-committed-chain-is-a-rolling-window.md](adr/007-why-the-committed-chain-is-a-rolling-window.md) — the window and its re-sealing
