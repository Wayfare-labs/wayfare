## What this changes

<!-- One or two sentences. What did you add or fix, and why does it change
     what a reader believes about a corridor? -->

Closes #
Roadmap entry: Backlog #

## Confirmations

Tick each box. **An unticked box is not a rejection** — it routes the PR to a
human instead of merging automatically, which is often the right outcome.

If a line does not apply to your change, tick it and say why underneath.

- [ ] **Unknown is reported as unknown.** When data is unavailable this
      returns UNABLE-TO-DETERMINE — not zero, not a default, not an estimate.
      An anchor that does not publish something is different from one that
      publishes something wrong, and the output says which.
- [ ] **Every figure came from a live source or a recorded snapshot.** Nothing
      is guessed, interpolated, or averaged from other figures.
- [ ] **Tests run from `testdata/snapshots`, with no live network.** Verified
      with `make offline-test`.
- [ ] **There is a negative test.** A case where the code must fail or return
      undeterminable — not only the happy path. A test that cannot fail proves
      nothing.
- [ ] **`decimal.Decimal` for all money and rates.** No `float64` anywhere a
      price, amount or percentage is handled.
- [ ] **No new third-party dependencies.**
- [ ] **No maintainer-owned area modified without flagging.** Nothing in `dex/`
      pricing arithmetic, verdict thresholds, integrity taxonomy/semantics,
      SEP-38 fee handling, check composition rules (`route.WithFindings`),
      run-record layout, or corridor health score. (If the work turns out to
      require changing any of these, stop and flag it — that is a different
      issue with a different review bar).
- [ ] **Assert nothing the repository does not support.** Every claim is checked
      against the code at time of writing; every measurement carries its source
      and date checked; future capabilities are marked future; a negative or
      inconclusive finding is reported as a valid result.
- [ ] **`make fmt vet test race lint offline-test` is clean.**

## How you verified it

<!-- The exact command, and what it printed. If you measured something live,
     include the raw figures and the timestamp. -->

```
```

---

<details>
<summary>Why this template exists</summary>

This project's value is arithmetic correctness about money. A plausible-looking
PR that passes CI can still quietly change a published number, and the reader
of a published figure has no way to tell.

So the first review pass sits with you. The auto-merge gate lands changes it
can verify mechanically and hands everything else to a maintainer — the boxes
above are what it reads. Nothing here is ceremony: each line corresponds to a
failure this repository has actually had, or to an invariant in
[CONTRIBUTING.md](CONTRIBUTING.md).

</details>
