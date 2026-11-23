"""Issue #682: compare Wayfare's best route (Horizon strict-send) with Soroban
router quotes captured at the same moment.

For each corridor:

  1. `ladder -to T -json -checks=false` (Horizon, the published method)
  2. Soroban quotes for every rung that ladder priced:
       - Soroswap router  router_get_amounts_out, paths USDC->T and USDC->XLM->T
       - Aquarius router  the protocol's own find-path service (multi-hop)
       - Phoenix          only if a Phoenix pool holds T (none did on 2026-11-23)
  3. `ladder` again, so every Soroban quote is bracketed by two Horizon
     ladders and the drift between them is visible.

    python3 -I -B capture_quotes.py LADDER_BINARY OUT_DIR NGNC NGNT

Read-only. No transaction is built for submission; simulateTransaction and
the find-path endpoint only quote.
"""
import json
import os
import subprocess
import sys
import urllib.error
from decimal import Decimal

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import sorobanrpc as rpc  # noqa: E402
from common import (AQUARIUS_API, FIAT_SAC, SOROSWAP_ROUTER, STROOP,  # noqa: E402
                    USDC_SAC, XLM_SAC, http_json, units, utcnow)


def ladder(binary, code, path):
    started = utcnow()
    out = subprocess.run([binary, "-to", code, "-json", "-checks=false"],
                         capture_output=True, text=True, check=True).stdout
    with open(path, "w") as f:
        f.write(out)
    doc = json.loads(out)
    return started, doc


def soroswap_quote(amount, token):
    out = []
    for label, path in (("USDC->%s", [USDC_SAC, token]), ("USDC->XLM->%s", [USDC_SAC, XLM_SAC, token])):
        v, raw, ledger = rpc.simulate(SOROSWAP_ROUTER, "router_get_amounts_out", rpc.I128(amount), path)
        rec = {"path": label % rpc_label(token), "ledger": ledger}
        if v is None:
            rec["error"] = (raw.get("error") or "")[:300]
        else:
            rec["receive_raw"] = str(v[-1])
            rec["receive"] = units(v[-1])
        out.append(rec)
    return out


def aquarius_quote(amount, token):
    body = json.dumps({"token_in_address": USDC_SAC, "token_out_address": token,
                       "amount": str(amount)}).encode()
    rec = {"endpoint": AQUARIUS_API + "/find-path/"}
    try:
        d = http_json(AQUARIUS_API + "/find-path/", body, {"Content-Type": "application/json"})
    except urllib.error.HTTPError as e:
        rec["error"] = "HTTP %d: %s" % (e.code, e.read()[:300].decode(errors="replace"))
        return rec
    if not d.get("success"):
        rec["error"] = json.dumps(d)[:300]
        return rec
    rec.update({"receive_raw": str(d["amount"]), "receive": units(d["amount"]),
                "pools": d.get("pools"), "tokens": [t.split(":")[0] for t in d.get("tokens", [])]})
    return rec


def rpc_label(token):
    return {v: k for k, v in FIAT_SAC.items()}.get(token, token)


def main(binary, out_dir, codes):
    os.makedirs(out_dir, exist_ok=True)
    for code in codes:
        token = FIAT_SAC[code]
        stamp = utcnow().replace("-", "").replace(":", "")
        before_at, before = ladder(binary, code, os.path.join(out_dir, "%s-%s-horizon-before.json" % (stamp, code)))
        rungs = []
        sor_started = utcnow()
        for r in before["rungs"]:
            size = Decimal(r["send_amount"])
            amount = int(size * STROOP)
            rungs.append({
                "send_amount": r["send_amount"],
                "quoted_at": utcnow(),
                "soroswap": soroswap_quote(amount, token),
                "aquarius": aquarius_quote(amount, token),
            })
        sor_finished = utcnow()
        after_at, _ = ladder(binary, code, os.path.join(out_dir, "%s-%s-horizon-after.json" % (stamp, code)))
        doc = {
            "issue": 682,
            "corridor": "USDC->%s" % code,
            "token_contract": token,
            "horizon_before_started_at": before_at,
            "soroban_started_at": sor_started,
            "soroban_finished_at": sor_finished,
            "horizon_after_started_at": after_at,
            "soroban_rpc": rpc.RPC,
            "phoenix": "no Phoenix pool holds this token (capture_liquidity.py); not quoted",
            "rungs": rungs,
        }
        with open(os.path.join(out_dir, "%s-%s-soroban.json" % (stamp, code)), "w") as f:
            json.dump(doc, f, indent=1)
            f.write("\n")
        print("captured", code, before_at, "->", after_at)


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2], sys.argv[3:])
