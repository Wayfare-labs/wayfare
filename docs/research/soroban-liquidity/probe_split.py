"""Issue #682, follow-up: does splitting a rung between Horizon's route and a
Soroban route beat Horizon alone?

The analysis bound f(S) + g(S) cannot rule it out at 50 USDC, so this
measures actual splits: for each rung S and Soroban share x, Horizon prices
S - x (`ladder -sizes`) and the Soroban routers price x, in one window.

    python3 -I -B probe_split.py LADDER_BINARY OUT.json NGNC

Read-only, as the other captures.
"""
import json
import os
import subprocess
import sys
from decimal import Decimal

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from capture_quotes import aquarius_quote, soroswap_quote  # noqa: E402
from common import FIAT_SAC, STROOP, utcnow  # noqa: E402

RUNGS = [Decimal(25), Decimal(50), Decimal(100)]
SHARES = [Decimal(1), Decimal(2), Decimal(5), Decimal(10)]


def main(binary, out, code):
    token = FIAT_SAC[code]
    horizon_sizes = sorted({s - x for s in RUNGS for x in SHARES} | set(RUNGS))
    started = utcnow()
    raw = subprocess.run([binary, "-to", code, "-json", "-checks=false",
                          "-sizes", ",".join(str(s) for s in horizon_sizes)],
                         capture_output=True, text=True, check=True).stdout
    horizon = json.loads(raw)
    h = {Decimal(r["send_amount"]): Decimal(r["quote"]["receive_amount"])
         for r in horizon["rungs"] if r.get("quote")}
    soroban = {}
    for x in SHARES:
        amount = int(x * STROOP)
        quotes = soroswap_quote(amount, token) + [aquarius_quote(amount, token)]
        best = max((Decimal(q["receive"]) for q in quotes if "receive" in q), default=None)
        soroban[str(x)] = {"quotes": quotes, "best": str(best) if best is not None else None}
    finished = utcnow()
    rows = []
    for s in RUNGS:
        alone = h[s]
        for x in SHARES:
            sb = soroban[str(x)]["best"]
            if sb is None or (s - x) not in h:
                continue
            total = h[s - x] + Decimal(sb)
            rows.append({"rung": str(s), "soroban_share": str(x), "horizon_part": str(h[s - x]),
                         "soroban_part": sb, "split_total": str(total), "horizon_alone": str(alone),
                         "split_minus_alone_pct": str(((total - alone) / alone * 100).quantize(Decimal("0.01")))})
    doc = {"issue": 682, "corridor": "USDC->" + code, "started_at": started, "finished_at": finished,
           "reference_mid": horizon.get("reference_mid"), "horizon_measured_at": horizon.get("measured_at"),
           "horizon": horizon, "soroban": soroban, "splits": rows}
    with open(out, "w") as f:
        json.dump(doc, f, indent=1)
        f.write("\n")
    for r in rows:
        print(r["rung"], r["soroban_share"], r["split_total"], r["horizon_alone"], r["split_minus_alone_pct"])


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], sys.argv[3])
