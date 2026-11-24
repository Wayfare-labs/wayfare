# USDC on-chain loss compared with published remittance prices

**Status:** completed.
**Finding:** The 3% GOOD band is an order of magnitude or more below the
current measured LINK.IO losses for NGNC and GHSC. The 20% POOR/UNUSABLE
boundary is also below those losses. KES has no comparable on-chain quote.

## Scope

This comparison uses published World Bank Remittance Prices Worldwide (RPW)
data for United States → Nigeria, United States → Ghana, and United States →
Kenya, and Wayfare's own 2026-08-08 LINK.IO on-chain measurements.

The RPW edition is the complete **2011–2025 Q3** dataset, last updated
2026-09-23. The RPW landing page records the data update as 2025-08-18 and
links the Q3 2025 report and annex. These are the latest values available
from the World Bank catalog at research time.

## Comparable amounts

RPW reports provider-level total cost for $200 and $500 send amounts. Those
map to Wayfare's $250 and $500 rungs without implying that timing or delivery
method is identical.

The table below uses the cross-provider median of valid RPW `total cost %`
values in 2025 Q3. Wayfare values are the 2026-08-08 committed measurements.

| Corridor | RPW at $200 | On-chain loss at $250 | RPW at $500 | On-chain loss at $500 |
|:---|---:|---:|---:|---:|
| USD → NGN | 2.47% median (33 providers) | 70.86% (NGNC) | 1.36% median (31 providers) | 81.87% |
| USD → GHS | 4.35% median (31 providers) | 92.34% (GHSC, derivative) | 3.27% median (30 providers) | 95.50% |
| USD → KES | 4.48% median (40 providers) | no route | 3.41% median (39 providers) | no route |

The RPW values include negative provider-level observations in their median
calculation. This matters less than the gap: every on-chain NGN/GHS loss is
far above the published median. No RPW provider-level row was used to replace
the cross-provider median.

## What differs

- **Speed:** RPW records reported payment speed; an RPW row is not an
  executable Stellar quote. Wayfare's Horizon path is a one-shot ledger
  quote, not settlement to a bank or cash location.
- **KYC:** RPW provider prices generally assume a customer completing that
  provider's identity and compliance process. Wayfare is read-only and does
  not perform KYC or hold funds.
- **Cash-out:** RPW providers deliver NGN, GHS or KES through their published
  receiving methods. Wayfare measures delivery of NGNC/GHSC/KESC on-chain
  tokens; NGNC and GHSC may require a separate off-chain redemption that
  has not been priced here.
- **Issuer/dependency:** GHSC is derivative. Its measured route traverses
  NGNC, so the GHSC loss is not an independent USDC-to-GHS market cost.
- **KES:** KESC has no executable Horizon route. The correct on-chain result
  is no market, not a loss percentage.
- **Timing:** RPW is 2025 Q3 and Wayfare's measurements are from 2026-08-08.
  This is a benchmark comparison, not a same-day provider quote.

## Verdict-band assessment

The GOOD ≤ 3% band remains broadly well anchored as a "competitive small
remittance" threshold: the RPW $500 medians are 1.36%, 3.27% and 3.41%, with
the Nigeria median and Ghana/Kenya medians close to or above the $200/GHS and
KES figures. The exact $500 KES median is slightly above 3%, but the band's
round-number convention is close to the competitive-median scale.

The FAIR ≤ 8%, POOR ≤ 20%, and UNUSABLE > 20% bands are not directly tested
as provider-price bands because RPW's published corridor distributions are
lower than 20%. They remain useful as Wayfare's loss-severity conventions:
both measured NGN and GHS markets cross 20% rapidly, then remain far above it.

For these corridors, the evidence does **not** support changing or removing
the 3% GOOD boundary. It does support state that the on-chain data are outside
the normal small-remittance cost range and, for KES, that no comparable
on-chain route exists.

## Sources

- [World Bank RPW landing page](https://remittanceprices.worldbank.org/)
- [World Bank RPW complete 2011–2025 Q3 dataset](https://datacatalogfiles.worldbank.org/ddh-published/0037898/DR0095523/rpw_dataset_2011_2025_q3.xlsx)
- [World Bank Q3 2025 report and annex](https://remittanceprices.worldbank.org/sites/default/files/2026-04/RPW_main_report_and_annex_Q325.pdf)
- [World Bank RPW catalog record](https://datacatalog.worldbank.org/search/dataset/0037898/Remittance-Prices-Worldwide)
- [Wayfare corridor measurements](corridor-measurements.md)
