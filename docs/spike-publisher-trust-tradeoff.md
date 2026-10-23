# Spike: publisher-trust tradeoff

**Status:** inconclusive research finding; no implementation proposed

**Checked:** 2026-09-24

**Question:** Can Wayfare make published figures less dependent on trusting the
publisher, while preserving the current recorded-evidence workflow?

## Finding

The current repository supports recording structured snapshot records and
replaying them, but it does not currently support a publisher-independent
attestation of those records. The result of this spike is therefore
**inconclusive for reducing publisher trust with the current data model**. The
repository can make a publisher's evidence inspectable and replayable; it
cannot currently prove that the publisher supplied the complete, untampered,
or economically representative input set before publication.

**Source:** `snapshot/record.go`, `snapshot/replay.go`,
`docs/snapshot-record-replay.md`; **checked:** 2026-09-24.

## What exists today

Snapshot records contain the recorded observation data and metadata needed by
the replay path. Replay consumes those records rather than requiring a live
upstream during the replay operation. This supports an important form of
reproducibility: a reader with the same recorded bytes and compatible code can
rerun the calculation and compare the result.

**Source:** `snapshot/record.go`, `snapshot/replay.go`,
`docs/snapshot-format.md`, `docs/offline-testing.md`; **checked:** 2026-09-24.

Run-store interfaces and filesystem storage persist run artifacts, but the
stored run layout is a storage and retrieval contract. It is not an
attestation protocol: the current implementation does not add a signed
publisher statement, a trusted timestamp, a public-key identity, or a
publisher-independent commitment that a reader can verify.

**Source:** `runstore/runstore.go`, `runstore/file.go`,
`runstore/fsstore.go`, `docs/run-store.md`; **checked:** 2026-09-24.

A repository-wide check found no current implementation of a hash or digest
commitment, digital signature, public-key verification, Merkle commitment, or
attestation record for this purpose. This is a statement about the checked
repository state, not a claim that such mechanisms could not be added in a
future version.

**Source:** repository search across `*.go` and `*.md` for `sha256`, `sha512`,
`digest`, `checksum`, `signature`, `public key`, `attest`, `merkle`, and
`canonical`; **checked:** 2026-09-24.

## Trust tradeoff

Recorded bytes reduce one kind of trust: a reader does not have to accept the
publisher's arithmetic on faith when the bytes and replay procedure are
available. They do not remove the need to trust the publisher's collection
and publication boundary. In particular, replay cannot recover observations
that were omitted, selectively sampled, altered before recording, or replaced
after publication unless an independently verifiable commitment exists.

**Source:** `snapshot/replay.go`, `runstore/runstore.go`,
`docs/snapshot-record-replay.md`; **checked:** 2026-09-24. The omitted,
altered, and replaced cases are the security implication of the absence of a
current commitment or attestation mechanism, not behavior claimed to be
detected by the repository.

The practical conclusion is a tradeoff, not a verdict that the current
workflow is invalid: replayability is useful evidence, while provenance and
completeness remain publisher-trust assumptions. This spike found no basis for
claiming that the existing system already provides stronger guarantees.

**Source:** the current snapshot and run-store implementations cited above;
**checked:** 2026-09-24.

## Future design space

Reducing the remaining trust would require a separately specified provenance
and attestation design. Possible ingredients include a canonical commitment to
the exact recorded bytes, an authenticated publisher identity, an independently
verifiable timestamp or publication log, and explicit rules for completeness,
ordering, and key rotation. These are design options only; none is implemented
by this spike or should be described as a current capability.

**Source:** design implication of the current interfaces in
`snapshot/record.go` and `runstore/runstore.go`; **checked:** 2026-09-24.

Choosing any of those options would affect more than documentation. It could
change the run-record layout and integrity semantics, and might affect how
checks compose or how verdicts are interpreted. Per the issue boundary, this
spike does not choose thresholds, alter verdict semantics, modify integrity
rules, or implement a new record format.

**Source:** `checks/checks.go`, `checks/runner.go`, `runstore/runstore.go`,
`docs/checks.md`; **checked:** 2026-09-24.

## Outcome

**Negative/inconclusive result:** the current repository provides replayable
recorded evidence but no implemented mechanism that makes the publisher's
input selection, completeness, or publication history independently
verifiable. A future V6/E4 proposal needs a separate design and review before
the project can claim a reduction in publisher trust.

**Source:** all sources cited in this document; **checked:** 2026-09-24.