# Research: Wayfare loss versus published remittance prices

**Status: completed.** **Finding: measurable context exists, but it does not
change the maintained verdict thresholds.**

This comparison answers whether the corridor verdict bands remain reasonably
anchored against the published cost of an equivalent USD remittance.

## Dataset and edition

The comparison uses the World Bank Remittance Prices Worldwide complete
dataset, edition covering 2011 through 2025 Q3. The catalog records the data
file as last updated 2026-09-23 and the quarterly report for 2025 Q3 as last
updated 2026-04-17.

- Dataset: https://datacatalog.worldbank.org/search/dataset/0037898/Remittance-Prices-Worldwide
- Complete workbook: https://datacatalogfiles.worldbank.org/ddh-published/0037898/DR0095523/rpw_dataset_2011_2025_q3.xlsx
- 2025 Q3 report: https://www.remittanceprices.worldbank.org/sites/default/files/2026-04/RPW_main_report_and_annex_Q325.pdf
- RPW data page, last update: https://remittanceprices.worldbank.org/

The observations below are from period `2025_3Q`, source USA, destinations
Nigeria (`NGA`), Ghana (`GHA`) and Kenya (`KEN`), and the workbook's exact
send amounts of USD 200 and USD 500. Only rows marked `transparent=Yes` are
included. The figures are the full total-cost percentage from the
dataset, including fee and exchange-rate margin where the published service
reports them.

## Comparable-cost table

| Corridor | 2025 Q3 RPW, $200 median (range) | 2025 Q3 RPW, $500 median (range) | Wayfare on-chain, $500 | Speed / service basis |
|:---|---:|---:|---:|:---|
| USD → NGN | 2.47% (-0.49% to 9.87%), 33 observations | 1.36% (-0.49% to 5.74%), 31 observations | 81.84% loss for NGNC; 0.00% loss for NGNT | RPW prices retail bank, agent, card and internet services; on-chain figures price token delivery, not bank/cash-out redemption |
| USD → GHS | 4.35% (-0.13% to 11.87%), 31 observations | 3.27% (-0.13% to 8.27%), 30 observations | 95.50% loss for GHSC | RPW includes cash, bank-account and card/payment variants; GHSC is a derivative route through NGNC |
| USD → KES | 4.48% (-0.29% to 11.26%), 40 observations | 3.41% (-1.49% to 7.66%), 39 observations | No route | RPW includes bank, mobile-wallet, card and cash variants; Wayfare observed no DEX market from USDC to KESC |

Negative RPW values in the workbook are published observations in the
transparent-service rows, not additions made by this research. They indicate
promotional or negative-margin results and should not be interpreted as
unusually cheap "loss" because the median and range are both reported.

The $500 on-chain observations are the 2026-08-08 NGNC/GHSC ladder and the
2026-08-08 KESC ladder in
[`docs/corridor-measurements.md`](corridor-measurements.md). The NGNT result is
the separate 2026-08-27 Cowrie ladder in the same document.

## What differs

- **Service and settlement:** RPW is a retail remittance-price survey.
  Wayfare's DEX loss is the loss on delivering a Stellar fiat-labelled token,
  not necessarily cash or mobile-money settlement.
- **KYC and identity:** RPW prices include services that may require account
  registration or KYC. Wayfare's on-chain pathfinding is public and
  account-independent, so it does not measure an identity burden or redemption
  obligation.
- **Cash-out:** RPW's pickup, bank-account and mobile-wallet methods differ
  from each other and from token redemption. A Wayfare result does not include
  fees or FX charged by an off-ledger issuer or cash-out agent.
- **Speed:** RPW records same-day, next-day, several-day and agent collection
  methods. Wayfare measures a DEX quote at a point in time, not settlement
  speed.
- **Coverage:** The RPW median is an empirical distribution of retail options,
  not a universal least-cost or guaranteed quote. The Wayfare ladder is a
  point-in-time DEX observation.

## Verdict-band assessment

The `GOOD <= 3%` anchor remains reasonably well calibrated for retail
remittance comparison:

- USD → NGN has a $500 RPW median of 1.36%, and several published low-cost
  options fall within 3%.
- USD → GHS has a $500 RPW median of 3.27%, only slightly above the GOOD
  boundary.
- USD → KES has a $500 RPW median of 3.41%, again close to the boundary and
  with a broad range.

This does not prove that 3% is a universal remittance cost. It is a
conservative-looking quality band for the $500 observations, while the $200
medians are 2.47%, 4.35% and 4.48% respectively. The on-chain NGNC, GHSC and
KESC observations are far outside the published retail range, and no DEX
comparison can erase the different product and cash-out assumptions.

**Recommendation:** retain the existing GOOD/FAIR/POOR/UNUSABLE bands; do not
change them solely from this RPW comparison. If bands are later tightened or
relocated, that decision must be a maintainer-owned change and must distinguish
token delivery from actual bank/cash-out settlement.
