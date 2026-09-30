# Discussion questions: what contributors actually ask

GitHub Discussions is enabled on this repository, and it is **empty**. Four
places in the tree send contributors there to "ask the rest" — this README, the
[contributor FAQ](contributor-faq.md), [CONTRIBUTING.md](../CONTRIBUTING.md) and
the [Discussions → Q&A](https://github.com/Wayfare-labs/wayfare/discussions/categories/q-a)
category itself — and none of them says what a question looks like here.

This document is the seed. It is deliberately **not** a list of questions someone
imagined contributors would ask. Every question below was asked by a named person
in a public issue, and every answer was checked against the code as it stood on
the date at the top. A question with no answer yet is here marked as such.

**Checked against the code at commit `74c1f17`, 2026-09-30.**

---

## Where the evidence comes from, and how much there is

The corpus is small, and saying so is more useful than padding it out.

Of the issues in this repository, **five were filed by someone other than the
maintainer**, by two people, on 2026-09-23 and 2026-09-24:

| # | Asked by | Date | Subject |
|:---|:---|:---|:---|
| [#474](https://github.com/Wayfare-labs/wayfare/issues/474) | `Hotmopo` | 2026-09-23 | the corridor selector never loads |
| [#475](https://github.com/Wayfare-labs/wayfare/issues/475) | `Hotmopo` | 2026-09-23 | every error shows the same panel |
| [#476](https://github.com/Wayfare-labs/wayfare/issues/476) | `Hotmopo` | 2026-09-23 | the page scrolls sideways on phones |
| [#477](https://github.com/Wayfare-labs/wayfare/issues/477) | `Hotmopo` | 2026-09-23 | a Cloudflare script in a self-contained page |
| [#481](https://github.com/Wayfare-labs/wayfare/issues/481) | `goodness-cpu` | 2026-09-24 | the Horizon call count is wrong twice |

**A finding about the evidence itself, because it changes what this document can
be.** The comment threads on these issues are overwhelmingly not questions. They
are Stellar Wave Program bounty applications — *"Hi maintainer, can i resolve
this. Kindly assign"*, *"pls assign me"* — posted by people looking for work, not
for answers. Of the comments read across #176, #233, #336 and #327, one was a
substantive contributor-facing note; the rest were assignment requests and
programme boilerplate.

That is worth stating plainly, because it is the reason Discussions is empty
rather than a backlog of unanswered questions. The questions that exist are the
ones somebody attached to a defect report because there was nowhere else to put
them. If you want a question answered, filing it as an issue has worked; asking
it in Discussions has no audience yet, because there is nothing in there.

---

## The questions, with their answers checked

Each answer below quotes the code it rests on, so you can check it rather than
trust this document. Line numbers are at `74c1f17`.

### 1. "How many Horizon calls does one ladder actually make?"

Asked in [#481](https://github.com/Wayfare-labs/wayfare/issues/481), 2026-09-24.
**Still open.** Two places in the tree state a count and neither matches the
other, let alone the measurement:

`server/api.go:52-53` says:

> // Timeout bounds a single corridor measurement. A full ladder is a
> // dozen round trips to Horizon, so this is generous by HTTP standards.

`docs/deployment.md:201-203` says:

> One sweep is roughly three dozen Horizon calls per corridor — twelve sizes,
> each with pathfinding plus a slippage probe — across three corridors, so about
> 110 requests every six hours.

The reporter measured **28** calls for a priced corridor, not 12: twelve
`/paths/strict-send` requests for the ladder, plus a two-call slippage probe
(`dex/dex.go:298` `MeasureSlippage` prices the requested amount *and* a small
probe) on each rung above the probe threshold of 10 send units
(`route/route.go:334`). Across the three corridors they counted 68, not 110,
because a `NO-MARKET` corridor short-circuits before the probe.

Both mechanisms described are real; the arithmetic in both places is not. This
is a documentation defect with a measurement attached, and the measurement is
reproducible with a counting proxy.

### 2. "Why does a self-contained page carry a Cloudflare challenge script?"

Asked in [#477](https://github.com/Wayfare-labs/wayfare/issues/477), 2026-09-23.
**Still open.** `server/index.html` still ends with a script block that injects a
hidden iframe and loads `/cdn-cgi/challenge-platform/…`. That path only exists
when the page is served behind Cloudflare; on `go run ./cmd/wayfared` or the
container image it 404s and the browser refuses to execute it.

It slips past `TestUIIsServed`, which asserts the page references no external
asset, because that test looks for `src="http`, `href="http` and `cdn.` — and
this is a root-relative `src` in single quotes. A self-contained UI that fetches
a third party it does not control, failing on every load, is a real defect. It
is also the sort of thing that arrives by accident: a fetched page captured into
the embed rather than written.

### 3. "Why does the page scroll sideways at every phone width?"

Asked in [#476](https://github.com/Wayfare-labs/wayfare/issues/476), 2026-09-23.
**Not established either way.** This repository already has a dated measurement
of it: [docs/qa/artifacts/270-mobile.md](qa/artifacts/270-mobile.md), run
`2026-09-23T17:35:50Z`, which records the document overflowing its viewport by
43 px at 320, 20 px at 390 and 13 px at 412, clean at 768, and identifies the
cause as a single label in the *Verdict thresholds* legend with no clipping
ancestor.

Whether it still reproduces is **not established**. The legend's markup has
changed since — it is a `<dl>` with `<dt>`/`<dd>` cells now, not `<span>`s — and
`server/index.html:436` gives `.legend-item` a `min-width: 0` while
`server/index.html:438` gives the `<dd>` label none, so the structure that
allowed the overflow is still there. That is a reading of the CSS, not a
measurement, and this document does not present it as one.

Re-running the harness to settle it did not complete. With Chromium installed,
`node run-mobile.mjs` against a real `-history-first` server timed out on its
`.scroll` locator for all four device profiles, so it produced no current
figure. The harness has drifted from the UI and fixing that is
[#270](https://github.com/Wayfare-labs/wayfare/issues/270)'s job, not this
document's.

**If you want to settle this one, that is a genuinely useful contribution** and
nobody has done it.

### 4. "Why does every error look the same if the API publishes a code?"

Asked in [#475](https://github.com/Wayfare-labs/wayfare/issues/475), 2026-09-23.
**Partly addressed since.** The API does publish a stable machine-readable code
on every error — `server/api.go:624-629` `writeError` writes both `error` and
`code` — and the UI now carries it: `server/index.html:986` attaches `data.code`
to the thrown `Error`.

But the code is read for exactly one purpose, `server/index.html:992`, where
`invalid_sizes` marks the sizes field `aria-invalid`. The message the reader
actually sees is still assembled from the English alone,
`server/index.html:993`:

> showErrorBanner(`Could not measure: ${esc(e.message)}`);

So the reporter's central observation still stands: the *rendered* error text
carries no code, and a reader cannot distinguish seven causes without reading
prose. The plumbing to fix it is now in place, which is most of the work.

### 5. "Why does the corridor selector never load?"

Asked in [#474](https://github.com/Wayfare-labs/wayfare/issues/474), 2026-09-23.
**Fixed, and this is the one that worked.** The issue reported `loadAssets()`
defined at `server/index.html:796` and never called, leaving the selector stuck
on "Loading corridors…" and `/api/assets` never requested.

It is now called — `server/index.html:1829` and `server/index.html:1856` — and
the issue is closed. Recorded here because it is the answer to a question worth
keeping: the report was accurate, it was acted on, and the fix is in the tree.

---

## What is not in here, and why

- **No question about the reference rate, the verdict thresholds or the
  integrity states.** Those are documented and there is no evidence anyone has
  asked them. Adding invented entries would make this document look thorough and
  make it worthless.
- **No question from the backlog.** The backlog records gaps someone found; it
  is not a record of questions anyone asked.
- **Nothing about a discussion that does not exist.** Discussions holds no
  threads, so this document links only to the category. It does not link to a
  thread, because there is no thread to link to.

## Where a question of your own goes

If your question is not here, [Discussions → Q&A](https://github.com/Wayfare-labs/wayfare/discussions/categories/q-a)
is still where the repository points you, and a defect or scoped piece of work
belongs in [Issues](https://github.com/Wayfare-labs/wayfare/issues).

If a question here turns out to be already answered, or the answer changes when
the code moves, a PR that corrects this document is welcome — the test suite
fails when the code it quotes changes, so a correction is easy to make and easy
to check.

If a question gets asked twice, it belongs in the
[contributor FAQ](contributor-faq.md) instead. That is the existing rule
([CONTRIBUTING.md](../CONTRIBUTING.md#before-opening-a-pull-request)), and this
document is not a substitute for it: the FAQ answers 21 questions that are
settled, and this one holds the ones that are not.

## Related documents

- [docs/contributor-faq.md](contributor-faq.md) — the 21 settled questions
- [docs/qa/README.md](qa/README.md) — the browser QA harness, and the recorded
  results these answers are checked against
- [docs/backlog.md](backlog.md) — backlog entry #274, this issue
- [CONTRIBUTING.md](../CONTRIBUTING.md) — what the project will and will not
  accept
- [docs/glossary.md](glossary.md) — verdicts, integrity states, agreement bands,
  and what *not determined* means
