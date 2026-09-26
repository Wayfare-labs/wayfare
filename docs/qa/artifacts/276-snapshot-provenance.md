# Snapshot provenance audit — Issue #276

**Target:** repository snapshots under `testdata/snapshots/` and `checks/testdata/snapshots/`
**Audit date:** 2026-09-26T00:00:00Z
**Method:** load each snapshot directory through `snapshot.Load`, which verifies every recorded response body against its manifest hash and refuses the snapshot if a body was edited or a request is missing.

## Scope and claim

This audit tests the exact claim in the backlog entry: that every recorded snapshot verifies on load and records how it was taken.

The repository currently contains 11 snapshot directories, not 8:

- 4 under `testdata/snapshots/`
- 7 under `checks/testdata/snapshots/`

## Procedure

The following script reproduces the same validation used here:

```bash
cd /workspaces/wayfare
cat > /tmp/check_snapshots.go <<'EOF'
package main

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "time"

  "github.com/Wayfare-labs/wayfare/snapshot"
)

func main() {
  roots := []string{"testdata/snapshots", "checks/testdata/snapshots"}
  dirs := []string{}
  for _, r := range roots {
    entries, err := os.ReadDir(r)
    if err != nil {
      panic(err)
    }
    for _, e := range entries {
      if e.IsDir() {
        dirs = append(dirs, filepath.Join(r, e.Name()))
      }
    }
  }
  sort.Strings(dirs)
  fmt.Printf("snapshot_dirs=%d\n", len(dirs))
  for _, d := range dirs {
    m, err := snapshot.Load(d)
    if err != nil {
      fmt.Printf("FAIL %s :: %v\n", filepath.Base(d), err)
      continue
    }
    fmt.Printf("OK %s :: recorded=%s corridor=%s->%s git=%s interactions=%d\n",
      filepath.Base(d),
      m.RecordedAt.Format(time.RFC3339),
      m.Corridor.Send.Code,
      m.Corridor.Receive.Code,
      m.GitRevision,
      len(m.Interactions),
    )
  }
}
EOF

go run /tmp/check_snapshots.go
```

## Observed results

### Summary

The loader reports 10 passing snapshot directories and 1 failing snapshot directory.

| Snapshot | Status | Recorded at | Corridor | Git revision | Interactions | Notes |
|:---|:---|:---|:---|:---|---:|:---|
| `testdata/snapshots/usdc-ghsc-20260821T222915Z` | PASS | 2026-08-21T22:29:15Z | USDC → GHSC | `ee3d2b3` | 13 | Full route fixture |
| `testdata/snapshots/usdc-kesc-20260821T222949Z` | PASS | 2026-08-21T22:29:49Z | USDC → KESC | `ee3d2b3` | 13 | Full route fixture |
| `testdata/snapshots/usdc-ngnc-20260821T223040Z` | PASS | 2026-08-21T22:30:40Z | USDC → NGNC | `b36a3af` | 13 | Full route fixture |
| `testdata/snapshots/strictsend-malformed-20260829T000000Z` | PASS | 2026-08-29T00:00:00Z | USDC → NGNC | absent | 7 | Negative-path malformed payload fixture |
| `checks/testdata/snapshots/usdc-ngnc-strictsend-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | USDC → NGNC | absent | 2 | Strict-send fixture |
| `checks/testdata/snapshots/usdc-ngnc-strictsend-curve-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | USDC → NGNC | absent | 6 | Curve fixture |
| `checks/testdata/snapshots/usdc-ngnc-strictsend-empty-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | USDC → NGNC | absent | 1 | Empty result fixture |
| `checks/testdata/snapshots/xlm-ngnc-orderbook-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | XLM → NGNC | absent | 1 | Standard order-book fixture |
| `checks/testdata/snapshots/xlm-ngnc-orderbook-empty-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | XLM → NGNC | absent | 1 | Empty order-book fixture |
| `checks/testdata/snapshots/xlm-ngnc-orderbook-onesided-20260823T000000Z` | PASS | 2026-08-23T00:00:00Z | XLM → NGNC | absent | 1 | One-sided book fixture |
| `checks/testdata/snapshots/xlm-ngnc-orderbook-deep-20260823T000000Z` | FAIL | 2026-08-23T00:00:00Z | XLM → NGNC | absent | 1 | Hash mismatch: file edited after capture |

### Actual verification output

This is the fresh result from the loader:

```text
snapshot_dirs=11
OK usdc-ngnc-strictsend-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=USDC->NGNC git= interactions=2
OK usdc-ngnc-strictsend-curve-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=USDC->NGNC git= interactions=6
OK usdc-ngnc-strictsend-empty-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=USDC->NGNC git= interactions=1
OK xlm-ngnc-orderbook-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=XLM->NGNC git= interactions=1
FAIL xlm-ngnc-orderbook-deep-20260823T000000Z :: snapshot: body responses/001-order-book.json does not match its recorded hash (manifest sha256:407747..., file sha256:09287b...); the fixture has been edited since it was captured
OK xlm-ngnc-orderbook-empty-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=XLM->NGNC git= interactions=1
OK xlm-ngnc-orderbook-onesided-20260823T000000Z :: recorded=2026-08-23T00:00:00Z corridor=XLM->NGNC git= interactions=1
OK strictsend-malformed-20260829T000000Z :: recorded=2026-08-29T00:00:00Z corridor=USDC->NGNC git= interactions=7
OK usdc-ghsc-20260821T222915Z :: recorded=2026-08-21T22:29:15Z corridor=USDC->GHSC git=ee3d2b3 interactions=13
OK usdc-kesc-20260821T222949Z :: recorded=2026-08-21T22:29:49Z corridor=USDC->KESC git=ee3d2b3 interactions=13
OK usdc-ngnc-20260821T223040Z :: recorded=2026-08-21T22:30:40Z corridor=USDC->NGNC git=b36a3af interactions=13
```

## What is actually recorded

The manifest format records:

- `recorded_at`
- `corridor.send` and `corridor.receive`
- `sources.horizon.base_url`
- `sources.reference.provider` and `base_url` where present
- `sizes` where applicable
- the per-interaction request URL, method, status, body file and `body_sha256`
- `git_revision` when the recorder was run against a clean tree

This is the provenance data the loader is designed to keep. It is present for the main corridor snapshots and absent for the order-book fixtures under `checks/testdata/snapshots/`.

## Findings

### F1 — One snapshot fails hash verification and therefore does not load

**Observed:** `checks/testdata/snapshots/xlm-ngnc-orderbook-deep-20260823T000000Z` fails:

> `snapshot: body responses/001-order-book.json does not match its recorded hash ... the fixture has been edited since it was captured`

**Why this matters:** the snapshot loader is intentionally strict; it refuses a modified fixture instead of silently trusting it. This is the correct safety behavior, but the repository currently contains one fixture that fails the guarantee.

**Reproduction:** run the script above. The failure is deterministic and appears before any analysis step.

**Separate issue to file:** file a reproducibility bug for the edited order-book fixture and include the failing directory and the exact hash mismatch as the reproduction steps.

### F2 — The repository has a provenance gap: not every snapshot records a git revision

**Observed:** only 3 of 11 snapshot directories carry a `git_revision`; the remaining 8 do not.

This means some fixtures are replayable, but they do not record the exact committed source tree that produced them. This is a provenance gap even when the snapshot itself still verifies.

**Importance:** the docs say the `git_revision` field is the only link between a committed fixture and the code that produced it. Those fields are missing in the checks fixtures.

**Separate issue to file:** file a provenance-recording defect for the `checks/testdata/snapshots/*` fixtures and require `git_revision` for each committed capture.

### F3 — The backlog count is inaccurate

**Observed:** the backlog issue says there are eight snapshot directories across the two fixture trees; the repo currently contains 11.

This mismatch is descriptive, not a loader failure. The relevant fact is the repository’s actual state, and the audit must reflect that actual set.

## Conclusion

The audit’s central claim is only partly true:

- most snapshots verify successfully
- one snapshot does not verify and must be treated as a broken fixture
- the provenance record is incomplete for several snapshots because `git_revision` is absent

The repository therefore does not currently satisfy the “every recorded snapshot verifies on load and records how it was taken” claim as written. The failing fixture is the concrete breakage; the missing git revisions are the provenance gap.
