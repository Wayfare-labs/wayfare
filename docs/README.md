# Docs index

151 documents in `docs/`, grouped by primary audience. Every file is listed
exactly once; the grouping reflects who is most likely to need it first.

---

## Reader — understanding what Wayfare is and how it works

| Document | What it covers |
|:---|:---|
| [about.md](about.md) | What Wayfare is and what problem it solves |
| [how-wayfare-works.md](how-wayfare-works.md) | End-to-end explanation for someone new to exchange-rate monitoring |
| [why-wayfare.md](why-wayfare.md) | Why this project exists and what makes it different |
| [why-stellar-native.md](why-stellar-native.md) | Why the measurement layer is Stellar-native |
| [non-goals.md](non-goals.md) | What Wayfare refuses to build and why |
| [glossary.md](glossary.md) | Every state term a reader meets in a response |
| [state-vocabulary.md](state-vocabulary.md) | State vocabulary for the interface layer |
| [ui-states.md](ui-states.md) | Interactive states — focus, hover, active, disabled |
| [first-15-minutes.md](first-15-minutes.md) | Orienting guide for a first read-through |
| [discussion-questions.md](discussion-questions.md) | Questions contributors actually asked, answered against the code |
| [backlog.md](backlog.md) | Contributor backlog — what is open and why |
| [good-first-issue-audit.md](good-first-issue-audit.md) | Audit of issues labelled good-first-issue |

---

## Consumer — using the HTTP API and SDK

| Document | What it covers |
|:---|:---|
| [api.md](api.md) | Full HTTP API reference |
| [api-consumer.md](api-consumer.md) | How to read the API correctly, with worked examples |
| [api-compatibility-policy.md](api-compatibility-policy.md) | What the project promises about breaking changes |
| [errors.md](errors.md) | Error response shapes and machine-readable codes |
| [freshness.md](freshness.md) | When responses are live vs stale and what each means |
| [ladder-sizes.md](ladder-sizes.md) | The send-size ladder and what each rung measures |
| [metrics.md](metrics.md) | Metrics methodology — what each figure is and how it is derived |
| [corridor-measurements.md](corridor-measurements.md) | The LINK.IO African-fiat asset set: the live corridors |
| [cost-decomposition.md](cost-decomposition.md) | How total transfer cost is broken into components |
| [reference-providers.md](reference-providers.md) | Which reference-rate providers are used and why |
| [adr-reference-mids.md](adr-reference-mids.md) | ADR: why reference mids are never averaged |
| [sep38-fee-identity.md](sep38-fee-identity.md) | SEP-38 fee-denomination identity and what it implies |
| [snapshot-format.md](snapshot-format.md) | On-disk snapshot format |
| [run-store.md](run-store.md) | The run store: how measurements are persisted |
| [verify-store.md](verify-store.md) | How to verify a stored run-chain |
| [embedded-history.md](embedded-history.md) | The embedded history bundle shipped with the binary |
| [upstream-failures.md](upstream-failures.md) | Upstream failure shapes and what each one means |
| [live-measurement-failures.md](live-measurement-failures.md) | When a live measurement fails locally |

---

## Contributor — writing code and docs for Wayfare

| Document | What it covers |
|:---|:---|
| [contributor-faq.md](contributor-faq.md) | Questions a new contributor actually asks |
| [development-loop.md](development-loop.md) | The local development loop |
| [adding-a-corridor.md](adding-a-corridor.md) | Step-by-step guide to adding a new corridor |
| [adding-a-check.md](adding-a-check.md) | Step-by-step guide to adding a new check |
| [adding-a-metric.md](adding-a-metric.md) | Step-by-step guide to adding a new metric |
| [checks.md](checks.md) | The check contract — what every check must satisfy |
| [correction-records.md](correction-records.md) | How to fix a wrong published figure in an append-only chain |
| [wrong-published-figure.md](wrong-published-figure.md) | Responding to a wrong published figure |
| [offline-testing.md](offline-testing.md) | The offline-testing requirement and how to satisfy it |
| [snapshot-record-replay.md](snapshot-record-replay.md) | Snapshot record-and-replay workflow |
| [snapshot-workflow.md](snapshot-workflow.md) | Snapshot workflow overview |
| [dependency-policy.md](dependency-policy.md) | What dependencies the project will and won't take |
| [maintainer-owned-areas.md](maintainer-owned-areas.md) | Which areas have named owners and why |
| [auto-merge.md](auto-merge.md) | The auto-merge gate and what it checks |
| [workflow-permissions.md](workflow-permissions.md) | Workflow permissions and supply-chain path |
| [chart-palette.md](chart-palette.md) | The chart colour palette and usage rules |
| [spacing-and-radius.md](spacing-and-radius.md) | Spacing and border-radius scale |
| [troubleshooting.md](troubleshooting.md) | Common problems and how to fix them |
| [adr/README.md](adr/README.md) | Index of Architecture Decision Records |
| [adr/001-why-reference-mids-are-never-averaged.md](adr/001-why-reference-mids-are-never-averaged.md) | ADR 001: reference mids are never averaged |
| [adr/002-why-checks-never-move-the-headline.md](adr/002-why-checks-never-move-the-headline.md) | ADR 002: checks never move the headline figure |
| [adr/003-why-layers-3-and-4-have-no-packages.md](adr/003-why-layers-3-and-4-have-no-packages.md) | ADR 003: layers 3 and 4 have no packages |
| [adr/004-why-a-monitor-and-not-a-router.md](adr/004-why-a-monitor-and-not-a-router.md) | ADR 004: a monitor, not a router |
| [adr/005-why-the-scheduler-does-not-depend-on-the-server.md](adr/005-why-the-scheduler-does-not-depend-on-the-server.md) | ADR 005: the scheduler does not depend on the server |
| [adr/006-why-money-crosses-the-wire-as-decimal-strings.md](adr/006-why-money-crosses-the-wire-as-decimal-strings.md) | ADR 006: money crosses the wire as decimal strings |
| [adr/007-why-the-committed-chain-is-a-rolling-window.md](adr/007-why-the-committed-chain-is-a-rolling-window.md) | ADR 007: the committed chain is a rolling window |
| [adr/008-why-measurements-stay-on-main.md](adr/008-why-measurements-stay-on-main.md) | ADR 008: measurements stay on main |
| [adr/009-why-the-health-score-stays-off-the-wire.md](adr/009-why-the-health-score-stays-off-the-wire.md) | ADR 009: the health score stays off the wire |
| [adr/010-why-pathfinder-defers-horizon-replacement.md](adr/010-why-pathfinder-defers-horizon-replacement.md) | ADR 010: Horizon stays behind a Pathfinder seam until a trigger appears |

---

## Operator — deploying and running Wayfare

| Document | What it covers |
|:---|:---|
| [deployment.md](deployment.md) | Production deployment: Render, environment variables, health checks |
| [load-capacity.md](load-capacity.md) | Deployed configuration capacity and load benchmarks |
| [rollback.md](rollback.md) | How to roll back a bad deployment |
| [cold-start-reliability.md](cold-start-reliability.md) | Cold-start reliability: what happens before the first sweep |
| [regime-classification.md](regime-classification.md) | Regime classification — how corridors are labelled LIVE/STALE/etc. |
| [baseline-of-normal.md](baseline-of-normal.md) | What a corridor "baseline of normal" would require |
| [measurement-inference-boundary.md](measurement-inference-boundary.md) | The boundary between measurement and inference in the UI |
| [horizon-rpc-migration.md](horizon-rpc-migration.md) | Plan: the Horizon → Stellar RPC migration |
| [design-webhook-subscriptions.md](design-webhook-subscriptions.md) | Design: webhook subscriptions for corridor transitions |

---

## Researcher — corridor research, spikes, and investigations

### Corridor research

| Document | What it covers |
|:---|:---|
| [corridor-research-template.md](corridor-research-template.md) | Standardised template for a corridor research write-up |
| [corridor-research-inr.md](corridor-research-inr.md) | Research: USDC → INR (Indian Rupee) |
| [mxn-corridor-research.md](mxn-corridor-research.md) | Research: USDC → MXN (Mexican Peso) — UNVERIFIABLE/UNUSABLE |
| [php-corridor-research.md](php-corridor-research.md) | Research: USDC → PHP (Philippine Peso) — NO-MARKET |
| [eurc-corridor-research.md](eurc-corridor-research.md) | Research: USDC → EUR via Circle's EURC |
| [cop-corridor-research.md](cop-corridor-research.md) | Research: USDC → COP (Colombian Peso) |
| [research-BRL.md](research-BRL.md) | Research: USDC → BRL (Brazilian Real) — NO-MARKET |
| [research-ZAR.md](research-ZAR.md) | Research: USDC → ZAR (South African Rand) — NO-MARKET |
| [research-GHS.md](research-GHS.md) | Research: USDC → GHS (Ghanaian Cedi) — UNUSABLE |
| [research-tzs.md](research-tzs.md) | Research: USDC → TZS (Tanzanian Shilling) — NO-MARKET |
| [research-egp.md](research-egp.md) | Research: USDC → EGP (Egyptian Pound) — NO-MARKET |
| [corridor-research-ugx.md](corridor-research-ugx.md) | Research: USDC → UGX (Ugandan Shilling) |
| [corridor-research-xof-xaf.md](corridor-research-xof-xaf.md) | Research: USDC → XOF / XAF (CFA Franc) |
| [reverse-corridor-research.md](reverse-corridor-research.md) | Reverse corridors: the exit leg as a first-class measurement |

### Market and issuer research

| Document | What it covers |
|:---|:---|
| [sep38-african-fiat-survey.md](sep38-african-fiat-survey.md) | Do any Stellar anchors publish SEP-38 for African fiat? |
| [fair-value-ngn.md](fair-value-ngn.md) | What "fair value" means for the NGN reference rate |
| [ngnc-structural-floor.md](ngnc-structural-floor.md) | Case study: the USDC → NGNC structural floor |
| [parallel-rate-research.md](parallel-rate-research.md) | Research: a usable parallel/street-rate data source for NGN |
| [native-xlm-routing.md](native-xlm-routing.md) | Native XLM routing: does it change execution quality? |
| [liquidity-venues.md](liquidity-venues.md) | Liquidity venues: book-based metrics vs route figures |
| [market-structure.md](market-structure.md) | Market structure on Stellar |
| [offchain-remittance-price-research.md](offchain-remittance-price-research.md) | USDC on-chain loss compared with published remittance prices |
| [moneygram-cashout-research.md](moneygram-cashout-research.md) | MoneyGram Stellar USDC cash-out observability |
| [stellar-fiat-tokens-twosided-book-research.md](stellar-fiat-tokens-twosided-book-research.md) | Which Stellar fiat tokens have a two-sided book at all? |
| [strict-receive-asymmetry-research.md](strict-receive-asymmetry-research.md) | Does strict-receive price a corridor differently from strict-send? |
| [orderbook-differential-crosscheck-research.md](orderbook-differential-crosscheck-research.md) | An independent cross-check of Horizon's order book |
| [account-level-concentration-research.md](account-level-concentration-research.md) | Can account-level concentration be measured at all? |
| [ecosystem-mapping.md](ecosystem-mapping.md) | Mapping the Stellar ecosystem projects Wayfare could inform |
| [research-measurement-noise.md](research-measurement-noise.md) | How much does a rung move between back-to-back measurements? |
| [research-quoted-vs-realised.md](research-quoted-vs-realised.md) | Research: quoted versus realised path payments |

### Spikes

| Document | What it covers |
|:---|:---|
| [spike-90-day-history.md](spike-90-day-history.md) | What can be learned from 90 days of history |
| [spike-adversarial-review.md](spike-adversarial-review.md) | How would someone game a Wayfare verdict? |
| [spike-alerting-semantics.md](spike-alerting-semantics.md) | Alerting semantics for corridor deterioration |
| [spike-api-consumers.md](spike-api-consumers.md) | Who would consume this API, and what shape do they need? |
| [spike-composite-undetermined.md](spike-composite-undetermined.md) | How a composite score would handle undetermined components |
| [spike-confidence-expression.md](spike-confidence-expression.md) | Survey: how other market-quality tools express confidence |
| [spike-corridor-attestation.md](spike-corridor-attestation.md) | What a corridor attestation would actually assert |
| [spike-cost-of-being-wrong.md](spike-cost-of-being-wrong.md) | The cost of being wrong, per figure published |
| [spike-expected-slippage-book-shape.md](spike-expected-slippage-book-shape.md) | Expected slippage from book shape alone |
| [spike-fx-corridor-seasonality.md](spike-fx-corridor-seasonality.md) | Seasonality in FX corridors under exchange controls |
| [spike-model-vs-deterministic.md](spike-model-vs-deterministic.md) | Would a model add anything over the deterministic measurements? |
| [spike-multi-region-measurement.md](spike-multi-region-measurement.md) | Multi-region measurement |
| [spike-non-stellar-corridor.md](spike-non-stellar-corridor.md) | Could Wayfare measure a non-Stellar corridor? |
| [spike-publishable-prediction-shape.md](spike-publishable-prediction-shape.md) | What would make a prediction publishable? |
| [spike-publisher-trust-tradeoff.md](spike-publisher-trust-tradeoff.md) | Publisher-trust tradeoff |
| [spike-realtime-vs-scheduled-economics.md](spike-realtime-vs-scheduled-economics.md) | Real-time versus scheduled measurement economics |
| [spike-route-failure-definition.md](spike-route-failure-definition.md) | What a route failure actually is, observationally |
| [spike-sdk-surface.md](spike-sdk-surface.md) | What a Wayfare SDK would need to expose |
| [spike-second-maintainer-verification.md](spike-second-maintainer-verification.md) | How would a second maintainer verify a published claim? |
| [spike-single-number-cost.md](spike-single-number-cost.md) | What a single number would destroy |
| [spike-soroban-cost-of-verifying-corridor-claim.md](spike-soroban-cost-of-verifying-corridor-claim.md) | Soroban cost of verifying a corridor claim |
| [spike-trend-minimum-observations.md](spike-trend-minimum-observations.md) | Minimum observations for a defensible trend claim |
| [spike-v2-longitudinal-measurements.md](spike-v2-longitudinal-measurements.md) | Which V2 measurements are worth storing longitudinally? |
| [spike-wrong-prediction-failure-modes.md](spike-wrong-prediction-failure-modes.md) | The failure modes of publishing a wrong prediction |

### QA artifacts

| Document | What it covers |
|:---|:---|
| [qa/README.md](qa/README.md) | Browser QA overview and test matrix |
| [qa/api/README.md](qa/api/README.md) | HTTP API QA — consumer-perspective test suite |
| [qa/artifacts/README.md](qa/artifacts/README.md) | QA artifacts: what they are and how they were produced |
| [qa/artifacts/48-session-audit.md](qa/artifacts/48-session-audit.md) | QA artifact #48: session audit |
| [qa/artifacts/257-production-smoke-test.md](qa/artifacts/257-production-smoke-test.md) | QA artifact #257: production smoke test |
| [qa/artifacts/258-cold-start-distribution.md](qa/artifacts/258-cold-start-distribution.md) | QA artifact #258: cold-start distribution |
| [qa/artifacts/259-served-history-vs-committed.md](qa/artifacts/259-served-history-vs-committed.md) | QA artifact #259: served history vs committed |
| [qa/artifacts/260-api-consumer-qa.md](qa/artifacts/260-api-consumer-qa.md) | QA artifact #260: API consumer QA |
| [qa/artifacts/261-live-ladder-timeout.md](qa/artifacts/261-live-ladder-timeout.md) | QA artifact #261: live ladder timeout |
| [qa/artifacts/262-healthz-cold-start.md](qa/artifacts/262-healthz-cold-start.md) | QA artifact #262: /healthz cold-start |
| [qa/artifacts/263-ui-state-matrix.md](qa/artifacts/263-ui-state-matrix.md) | QA artifact #263: UI state matrix |
| [qa/artifacts/264-nomarket-corridor.md](qa/artifacts/264-nomarket-corridor.md) | QA artifact #264: NO-MARKET corridor |
| [qa/artifacts/265-derivative-corridor.md](qa/artifacts/265-derivative-corridor.md) | QA artifact #265: derivative corridor |
| [qa/artifacts/266-error-paths.md](qa/artifacts/266-error-paths.md) | QA artifact #266: error paths |
| [qa/artifacts/268-metric-rendering.md](qa/artifacts/268-metric-rendering.md) | QA artifact #268: metric rendering |
| [qa/artifacts/269-cross-browser.md](qa/artifacts/269-cross-browser.md) | QA artifact #269: cross-browser |
| [qa/artifacts/270-mobile.md](qa/artifacts/270-mobile.md) | QA artifact #270: mobile |
| [qa/artifacts/272-color-schemes.md](qa/artifacts/272-color-schemes.md) | QA artifact #272: colour schemes |
| [qa/artifacts/273-accessibility-audit.md](qa/artifacts/273-accessibility-audit.md) | QA artifact #273: accessibility audit |
| [qa/artifacts/274-screen-reader.md](qa/artifacts/274-screen-reader.md) | QA artifact #274: screen reader |
| [qa/artifacts/275-reproduce-corridor-measurements.md](qa/artifacts/275-reproduce-corridor-measurements.md) | QA artifact #275: reproduce corridor measurements |
| [qa/artifacts/276-snapshot-provenance.md](qa/artifacts/276-snapshot-provenance.md) | QA artifact #276: snapshot provenance |
| [qa/artifacts/277-verify-committed-chain.md](qa/artifacts/277-verify-committed-chain.md) | QA artifact #277: verify committed chain |
| [qa/artifacts/278-property-tests.md](qa/artifacts/278-property-tests.md) | QA artifact #278: property tests |
| [qa/artifacts/286-token-contrast.md](qa/artifacts/286-token-contrast.md) | QA artifact #286: token contrast |
| [qa/artifacts/293-error-preservation.md](qa/artifacts/293-error-preservation.md) | QA artifact #293: error preservation |
| [qa/artifacts/304-state-legibility.md](qa/artifacts/304-state-legibility.md) | QA artifact #304: state legibility |
| [qa/artifacts/closed-issue-audit.md](qa/artifacts/closed-issue-audit.md) | Closed issue audit — 2026-11-23 |
| [qa/artifacts/751-scheduled-wcag-audit.md](qa/artifacts/751-scheduled-wcag-audit.md) | QA artifact #751: scheduled WCAG audit |

### Schemas

| Document | What it covers |
|:---|:---|
| [schemas/runstore-v1.json](schemas/runstore-v1.json) | Run-store JSON schema v1 |
| [schemas/runstore-v2.json](schemas/runstore-v2.json) | Run-store JSON schema v2 |
| [schemas/runstore-v3.json](schemas/runstore-v3.json) | Run-store JSON schema v3 |
| [schemas/runstore-v4.json](schemas/runstore-v4.json) | Run-store JSON schema v4 |
| [schemas/runstore-v5.json](schemas/runstore-v5.json) | Run-store JSON schema v5 |
| [schemas/runstore-v6.json](schemas/runstore-v6.json) | Run-store JSON schema v6 |
