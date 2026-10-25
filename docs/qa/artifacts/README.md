# QA artifacts, and how they were produced

QA artifacts are dated, named records of a review or measurement that a reader
can check against the code. They are written up whether they pass, fail, or are
ambiguous, and the status is the claim — not the tone.

A new artifact gets a numeric prefix taken from its run id, the same convention
used everywhere else in `docs/qa/`. The file is placed in `docs/qa/artifacts/`
and named after what it inspected, not after the reviewer.

## What belongs here

- browser QA runs (colour, contrast, accessibility, screen-reader, cross-browser)
- responsive / breakpoint QA — screenshot sets at 320, 375, 768, 1024 and 1440 px,
  recorded per corridor state
- smoke tests against a deployed instance
- reproducibility checks — can an independent run reproduce a claimed figure?
- documentation-vs-code audits — what the docs say versus what the code does
- offline / network isolation checks

## What does not

- a single figure pulled from a live run with no timestamp and no source

That belongs in `docs/corridor-measurements.md` or a corridor research doc, with
the timestamp and the raw output. An artifact can *cite* that doc; it is not a
substitute for it.

## Naming

`<run-id>-<one-line-subject>.md`

Examples in this tree: `272-color-schemes.md`, `259-served-history-vs-committed.md`,
`260-api-consumer-qa.md`.

## Checklist for a new artifact

- [ ] Title states what was inspected and on what date.
- [ ] The commit or revision checked is named.
- [ ] Every tool, browser, or environment that produced a result is named enough
      that a reader could repeat it.
- [ ] A failure is recorded as a failure, not as a question.
- [ ] A contradiction with the docs is recorded as a finding, not silently fixed
      in the text.
- [ ] Anything that depends on a live network record carries the timestamp and the
      source.
- [ ] For breakpoint QA: each screenshot is named by breakpoint width and corridor
      state, and the viewport and browser used are recorded.

## Acceptance criteria

- [x] The directory and this note exist.
- [x] A new artifact added here is named and dated the same way as the existing
      ones.
- [x] The breakpoint set (320, 375, 768, 1024, 1440) is listed as in scope for
      responsive QA artifacts.

