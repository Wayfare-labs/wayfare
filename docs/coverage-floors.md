# Coverage floors for maintainer-owned packages

Four packages publish numbers under the project's name: `route` grades the
ladder, `dex` prices the paths, `refrate` chooses the benchmark, and `sep38`
handles anchor quotes. An untested branch in any of them does not break a
feature quietly — it can move a figure that a document or an API consumer
repeats as a measurement. That is why their coverage is enforced rather than
reported.

Issue #666 asked for the enforcement; issue #314's coverage reporting was the
precondition.

## The policy

`coverage-floors.txt` at the repository root is the policy:

```
route    89
dex      90
refrate  90
sep38    76
```

Each number is the minimum **statement-weighted** coverage for that package,
the same figure `go test -cover` prints. The script reads the profile directly
rather than averaging `go tool cover -func` output, because a per-function
average weights a ten-statement helper the same as a two-hundred-statement
ladder.

`scripts/check-coverage-floors.sh` is the mechanism. It is run by the
`coverage floors` job in `.github/workflows/ci.yml`, and locally by:

```bash
make coverage-floors
```

It exits non-zero when a package falls below its floor, and also when a package
named in the policy file is absent from the profile — a profile that measured
nothing must not pass.

## Why the floors sit below the measured values

A floor is a ratchet, not a target. Each is committed slightly below the
coverage measured at the time, so moving a statement from a covered block into
an uncovered one during an unrelated refactor does not fail the build for a
reason that has nothing to do with the change under review. The cost is that a
small regression can be absorbed; the alternative — a floor at the exact
current value — makes ordinary maintenance noisy and teaches people to lower
floors on sight.

## Ratchet procedure

Raise a floor when coverage has risen **and the rise is deliberate** — new
tests for a previously untested path, not incidental coverage from unrelated
work.

1. Run `make coverage-floors` and read the `actual` column.
2. Choose a new floor slightly below the measured value (usually one whole
   percentage point, the same margin the initial floors use).
3. Edit `coverage-floors.txt`.
4. Run `make coverage-floors` again and confirm the new floor passes.
5. Raise the floor in the **same pull request** that adds the tests, and say in
   the pull request why the new floor is the right one. A ratchet applied
   separately from the coverage that justifies it is a number nobody can
   review.

Lowering a floor is allowed only when the coverage loss is intentional and
explained — for example, deleting dead code that was covered only by tests for
a behaviour that no longer exists. Say so in the pull request. A lowering
without an explanation is the failure mode this file exists to prevent.

## What this does not claim

Coverage measures statement execution, not assertion quality. A package can be
100% covered and still have its refusals untested: the statement runs and the
test checks nothing. That gap is what
[the mutation catalogue](qa/artifacts/667-mutation-catalogue.md) measures for
the pricing path specifically.
