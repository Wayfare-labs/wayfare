# Chain Head Signatures: Design Note

**Status:** implemented per issue #717

---

## What the Signature Attests

Each chain head published by the measure workflow is signed using keyless CI
signing (Sigstore OIDC through GitHub's built-in attestation). The signature
attests exactly one statement:

> **"This chain head was produced by the Wayfare measure workflow running in
> GitHub Actions at the stated time, using the code at the stated commit,
> and the resulting record was appended to the corridor's hash chain."**

The signature covers:
- The corridor identifier (e.g., `USDC-NGNC`)
- The sequence number of the chain head
- The record's timestamp (`recorded_at`)
- The record's hash (`hash`)
- The Git commit SHA that produced the measurement
- The GitHub Actions run ID and workflow name

---

## What the Signature Does NOT Attest

The signature does **not** attest:

1. **That the figures are correct.** The measurement engine may have bugs, the
   upstream data may be stale or wrong, or the classification logic may
   misclassify. The signature only attests that *this code at this commit*
   produced *this record*.

2. **That the upstream data is authentic.** The signature does not cover the
   Horizon responses, reference rate provider responses, or anchor TOML
   documents. Those are recorded in separate snapshots with their own
   content-addressed hashes.

3. **That the corridor classification is appropriate.** A corridor classified
   as `DIRECT` may in fact depend on another fiat token if the asset registry
   is incomplete. The signature attests the *classification the engine
   produced*, not the ground truth.

4. **That the verdict thresholds are correct.** The thresholds (3%/8%/20%)
   are maintainer-owned judgements (ADR 002). A signature on a `POOR` verdict
   does not mean 8-20% loss is the right boundary — it means the engine at
   that commit applied those boundaries.

5. **That the record will not be superseded.** A later measurement of the same
   corridor will produce a new chain head with a new signature. The old
   signature remains valid for the old record; it does not attest anything
   about future measurements.

---

## Why This Boundary Matters

The chain head signature is a **transparency log entry**, not a correctness
certificate. It makes a later rewrite of the repository's `data/` directory
detectable without trusting the repository at read time. A reader who
verifies the signature chain can detect:

- A record that was deleted from the chain (gap in sequence numbers)
- A record that was modified (hash mismatch)
- A record that was inserted out of order (prev_hash mismatch)
- A record that claims to be from a commit that doesn't exist in the repo's
  history

What the reader **cannot** detect from the signature alone is whether the
measurement itself was faithful to the market at that moment. That requires
replaying the snapshot — which the signature does not replace.

---

## Verification Procedure

A third party verifying a chain head:

1. **Fetch the chain head** from `/api/chain-heads` (or from the `data/`
   directory in the repository).
2. **Verify the Sigstore signature** using `cosign verify-blob` with the
   GitHub Actions OIDC issuer.
3. **Verify the hash chain** by running `wayfared -verify-store -data ./data`
   on a clone of the repository at the commit the signature references.
4. **Optionally replay the snapshot** for any record whose figures matter to
   the verifier. The snapshot hash is stored in the record's `Snapshot` field.

The signature proves the record *was published by the CI system*. The hash
chain proves the record *was not modified after publication*. The snapshot
proves the record *can be recomputed from the same upstream bytes*.

---

## Implementation Notes

- Signing uses `cosign sign-blob --yes` with GitHub's OIDC token
  (`ACTIONS_ID_TOKEN_REQUEST_TOKEN` / `ACTIONS_ID_TOKEN_REQUEST_URL`).
- The signature and certificate are stored as GitHub Actions artifacts and
  attached to the workflow run.
- The signed payload is a JSON object matching the `chainHeadJSON` structure
  plus the Git commit SHA and workflow run ID.
- Verification uses `cosign verify-blob --certificate-identity-regexp
  '^https://github.com/Wayfare-labs/wayfare/.github/workflows/measure\.yml@.*'
  --certificate-oidc-issuer https://token.actions.githubusercontent.com`.

---

## Related

- [ADR 002](adr/002-verdict-thresholds-are-maintainer-owned.md) — verdict
  thresholds are maintainer-owned
- [docs/snapshot-format.md](snapshot-format.md) — snapshot content hashing
- [docs/run-store.md](run-store.md) — hash chain structure
- [.github/workflows/measure.yml](../.github/workflows/measure.yml) — the
  workflow that produces and signs chain heads