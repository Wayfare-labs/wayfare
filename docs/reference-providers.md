# Reference providers

Wayfare scores every route against a reference mid-market rate. The scored
benchmark is chosen from **two primary providers**; a **third provider is
consulted only to diagnose a disagreement** between them. None of the three
values is ever averaged, blended or synthesised. The decision that forbids
blending is [ADR 001: reference mids are never averaged](adr-reference-mids.md).

## The three roles

| Role | Provider | Moved into the scored mid? |
|:---|:---|:---|
| Primary | `exchangerate-api` (`refrate/exchangerate.go`) | Yes, when it wins the documented band rule |
| Secondary cross-check | `currency-api` (`refrate/currencyapi.go`) | Yes, on `DISAGREE` when its mid is more conservative |
| Diagnostic | `floatrates` (`refrate/floatrates.go`) | **No. Never.** |

The first two are the benchmark. The diagnostic is evidence about the
benchmark: when the primaries disagree, it reports which of them is farther
from an independent observation. That is what lets a `DISAGREE` say *which*
side looks wrong instead of only that one of them does.

## Why `floatrates` is the diagnostic provider

`floatrates` publishes a free, keyless daily JSON feed per base currency:

```
https://www.floatrates.com/daily/{base}.json
```

Each document is keyed by lowercase quote code, and each entry carries a
string `rate`, an RFC 1123 GMT publication timestamp, and an `inverseRate` this
project does not use. The feed refreshes twice a day (12 AM/PM GMT), which is
enough for a source consulted only on a disagreement: it is not asked on
`AGREE`, `STALE`, `MALFUNCTION` or `SINGLE`.

It was selected because:

- it answers all three corridors this project measures — **NGN, GHS and KES** —
  from the same USD document;
- it needs no API key or account;
- it is operationally independent of the two scored feeds: FloatRates describes
  its data as gathered from central banks and international financial
  institutions and passed through its own aggregation, rather than reselling
  either scored provider;
- its endpoint, response shape and publication stamp are stable enough to parse
  without guessing.

Like the scored providers, it publishes an official/interbank-style rate, not a
parallel or street rate. It is a diagnostic, not ground truth.

## Coverage verified live

Checked on **2026-10-19** against the USD feed. These are the figures the
endpoints actually returned for the three corridors this project measures:

| Provider | USD/NGN | USD/GHS | USD/KES | As of |
|:---|:---|:---|:---|:---|
| `exchangerate-api` | 1331.61657 | 11.653982 | 129.78817 | 2026-10-19T00:02:32Z |
| `currency-api` | 1333.14758612 | 11.68024189 | 129.99445625 | 2026-10-19 |
| `floatrates` | 1331.60997067 | 11.64151782 | 129.86270328 | 2026-10-19T07:55:04Z |

Reproduce the diagnostic's coverage with:

```bash
curl -fsSL https://www.floatrates.com/daily/usd.json |
  jq '{date: .ngn.date, ngn: .ngn.rate, ghs: .ghs.rate, kes: .kes.rate}'
```

`fxratesapi` was also checked and returned all three pairs on 2026-10-19, but
its documentation describes an API key even though the keyless call happened to
answer. A diagnostic source must not depend on an undocumented access mode, so
it was not selected.

## The diagnostic contract

A diagnostic is produced only when all of the following hold:

1. the two primaries answered and `reconcile` classified them `DISAGREE`;
2. a diagnostic provider is configured; and
3. the diagnostic provider answers with a usable, non-zero mid.

The attribution rule is:

- compare each primary's mid with the diagnostic mid using the same relative
  measure the disagreement band uses, `|a - b| / min(a, b)`;
- **`outlier` is the primary with the larger distance**;
- if the two distances are exactly equal, no side is named and `reason` says
  the primaries are equidistant;
- if the diagnostic provider fails, `reason` records that failure and no
  outlier is named.

The scored mid is untouched through every one of those branches. The
diagnostic has no path into `Rate.Mid`, `Rate.Source`, `Rate.AsOf`,
`Rate.SecondaryMid`, `Rate.DivergencePct`, the agreement band or any verdict.

### Wire shape

`reference_diagnostic` is an optional object on `GET /api/corridor`. It is
absent when no diagnostic ran, including on an older stored record.

```json
{
  "reference_agreement": "DISAGREE",
  "reference_mid": "1365",
  "reference_source": "secondary",
  "reference_secondary_mid": "1300",
  "reference_secondary_source": "primary",
  "reference_diagnostic": {
    "source": "floatrates",
    "mid": "1350",
    "as_of": "2026-10-19T07:55:04Z",
    "outlier": "primary",
    "outlier_distance_pct": "3.8462",
    "other_distance_pct": "1.1111"
  }
}
```

On a provider failure or a tie the object still names the diagnostic source and
carries `reason`, with `outlier` absent.

### Storage

Recorded runs preserve the diagnostic in the `reference` block as
`diagnostic_source`, `diagnostic_mid`, `diagnostic_as_of`,
`diagnostic_outlier`, `diagnostic_outlier_distance_pct`,
`diagnostic_other_distance_pct` and, when present, `diagnostic_reason`. The
fields are `omitempty` and were added after every existing field, so records
written before the diagnostic existed still encode — and verify — byte for
byte as they did before.

## Related

- [`refrate/cross.go`](../refrate/cross.go) — the band rule and `Diagnostic`
- [`refrate/floatrates.go`](../refrate/floatrates.go) — the diagnostic provider
- [`route/wire.go`](../route/wire.go) — `reference_diagnostic` on the wire
- [ADR 001](adr-reference-mids.md) — never average provider mids
- [`docs/api.md`](api.md) — the corridor response contract
