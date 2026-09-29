# Dependency policy

Why this module runs on exactly two direct dependencies, and how that
position is defended.

**Status: enforced in CI.** The test `TestDirectDependenciesAreExactlyTheDeclaredSet`
(and its neighbours in `deps_test.go`) parse `go.mod` and `go.sum` on every
build and fail when the surface below moves. This document explains the
position those tests defend; the tests are what make it hold.

---

## The position

The module has exactly two direct dependencies:

| Module | Pinned version | Why it exists |
|:---|:---|:---|
| `github.com/shopspring/decimal` | `v1.4.0` | All money. Binary floating point cannot represent decimal fractions exactly, and rounding drift in a tool whose entire purpose is measuring small differences is a correctness bug, not a style issue. |
| `github.com/BurntSushi/toml` | `v1.6.0` | Asset identity. Issuers publish `stellar.toml` per SEP-1, and every measurement depends on parsing that document correctly. A hand-rolled TOML parser would put identity behind unreviewed code. |

Everything else is the Go standard library. Neither direct dependency has
transitive requirements of its own, so `go.sum` carries exactly four lines —
both modules at their pinned version, in both hash forms — and the supply
chain this project asks people to trust is two modules, version-pinned.

## Why the position is what it is

**Measurement discipline has a supply-chain shape.** A tool whose claims are
reproducible measurements should not have a dependency graph its reviewers
cannot hold in their heads. Two dependencies with no transitive closure means
every byte of third-party code in the binary is accounted for on one page.

**Fewer dependencies is fewer incident classes.** A transitive tree has to be
watched for CVEs, licence changes, maintainer abandonment, and hijacked
releases. Two pinned modules watched by hand is a smaller, more honest
burden than tooling that watches a hundred.

**The bar for a third dependency is replacement, not addition.** The
question is never "is this library good" but "which of the two does it
replace, and why is that no longer defensible". That keeps the surface from
growing by accretion.

## What counts as the surface

The surface is `go.mod`'s `require` block plus `go.sum`'s module set,
together:

- The **direct set** must be exactly `DirectDependencies` in `deps.go` —
  same modules, same order, same pinned versions. The declaration in
  `deps.go` is the code-readable statement of this document; the two are
  kept in step by the tests, and a PR that moves one must move the other.
- The **sum set** must be exactly the pinned direct modules. Any module
  version in `go.sum` outside that set — however it arrived — fails the
  build until it is either removed or argued into the policy.

## How to add a dependency (i.e. how to change this policy)

1. Argue it in an issue first: what it does, which standard-library or
   existing-dependency path it replaces, its own transitive closure, its
   licence, and its maintenance status. The non-goals register
   ([docs/non-goals.md](non-goals.md)) is the spirit of the argument.
2. Update `DirectDependencies` and `DirectDependenciesPinned` in `deps.go`
   and the table above in the same PR.
3. The guard tests will fail until both move together — that is them
   working as intended, not a test to delete.

## What is deliberately not done

- **No vendoring.** `go.sum` already pins content hashes; a vendor tree
  would double the review surface without adding a guarantee.
- **No tool-based pinning beyond go.mod.** `go.mod` + `go.sum` is Go's
  lockfile. Adding a second pinning format creates two places to disagree.
- **No automated dependency updates.** Dependabot-style automation would
  move the pin without a human arguing the supply-chain decision each
  move represents. Bumps are small and manual on purpose.

---

## Related

- [CONTRIBUTING.md](../CONTRIBUTING.md) — the one-line statement of the set
- `deps.go` — the declaration the tests check against
- [why-stellar-native.md](why-stellar-native.md) — what the code uses, and why
- [non-goals.md](non-goals.md) — the register of what this project refuses
