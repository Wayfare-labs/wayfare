"""Derives the per-rung comparison (#682) and the per-corridor reserve table
(#681) from the committed captures, and writes analysis.json beside them.

    python3 -I -B analyze.py 2026-11-23

Decimal throughout; nothing here is float.
"""
import glob
import json
import os
import sys
from decimal import Decimal

PCT = Decimal(100)


def load(p):
    with open(p) as f:
        return json.load(f)


def rungs_by_size(ladder):
    out = {}
    for r in ladder["rungs"]:
        q = r.get("quote") or {}
        out[r["send_amount"]] = {
            "receive": q.get("receive_amount"),
            "path": q.get("description"),
            "loss_pct": q.get("loss_pct"),
            "verdict": q.get("verdict"),
            "priced": r.get("priced"),
        }
    return out


def best_soroban(rung):
    cands = []
    for s in rung["soroswap"]:
        if "receive" in s:
            cands.append((Decimal(s["receive"]), "soroswap " + s["path"]))
    a = rung["aquarius"]
    if "receive" in a:
        cands.append((Decimal(a["receive"]), "aquarius " + "->".join(a.get("tokens") or [])))
    return max(cands) if cands else (None, None)


def pct(a, b):
    return ((a - b) / b * PCT).quantize(Decimal("0.01"))


def loss(receive, send, mid):
    """Loss against the reference mid as docs/glossary.md defines it, floored
    at zero. Recomputed here only so it can be checked against the ladder's
    own loss_pct before being applied to a figure the ladder never priced."""
    expected = Decimal(send) * Decimal(mid)
    return max(Decimal(0), (1 - Decimal(receive) / expected) * PCT).quantize(Decimal("0.01"))


def grade(loss_pct):
    for limit, name in ((3, "GOOD"), (8, "FAIR"), (20, "POOR")):
        if loss_pct <= limit:
            return name
    return "UNUSABLE"


def soroban_reserves(liq, code):
    tot, rows = Decimal(0), []
    for proto, d in liq["soroban"].items():
        for p in d["pools"]:
            if code in p["tokens"]:
                amt = Decimal(p["reserves"][p["tokens"].index(code)])
                other = [t for t in p["tokens"] if t != code][0]
                rows.append({"protocol": proto, "pool": p["pool"], "counter": other,
                             "fiat_reserve": str(amt),
                             "counter_reserve": p["reserves"][p["tokens"].index(other)]})
                tot += amt
    return tot, rows


def main(day):
    liq = load(os.path.join(day, "liquidity.json"))
    out = {"reserves": {}, "comparison": {}}

    for code in ("NGNC", "NGNT", "GHSC", "KESC"):
        s_tot, rows = soroban_reserves(liq, code)
        c = liq["classic"][code]
        c_pool, c_off = Decimal(c["pool_fiat_reserve_total"]), Decimal(c["offers_selling_amount_total"])
        classic = c_pool + c_off
        direct_usdc_classic = sum((Decimal(p["fiat_reserve"]) for p in c["pools"] if p["counter"] == ["USDC"]),
                                  Decimal(0)) + Decimal(c["offers_selling_by_counter"].get("USDC", "0"))
        direct_xlm_classic = sum((Decimal(p["fiat_reserve"]) for p in c["pools"] if p["counter"] == ["XLM"]),
                                 Decimal(0)) + Decimal(c["offers_selling_by_counter"].get("XLM", "0"))
        s_usdc = sum((Decimal(r["fiat_reserve"]) for r in rows if r["counter"] == "USDC"), Decimal(0))
        s_xlm = sum((Decimal(r["fiat_reserve"]) for r in rows if r["counter"] == "XLM"), Decimal(0))
        total = s_tot + classic
        out["reserves"][code] = {
            "soroban_pools": rows,
            "soroban_fiat_total": str(s_tot),
            "classic_pool_fiat_total": str(c_pool),
            "classic_offers_selling_fiat_total": str(c_off),
            "soroban_share_of_all_pct": str((s_tot / total * PCT).quantize(Decimal("0.01"))) if total else None,
            "usdc_pair": {"soroban": str(s_usdc), "classic": str(direct_usdc_classic)},
            "xlm_pair": {"soroban": str(s_xlm), "classic": str(direct_xlm_classic)},
        }

    for path in sorted(glob.glob(os.path.join(day, "*-soroban.json"))):
        s = load(path)
        code = s["corridor"].split("->")[1]
        stem = path[: -len("-soroban.json")]
        before = load(stem + "-horizon-before.json")
        after = load(stem + "-horizon-after.json")
        hb, ha = rungs_by_size(before), rungs_by_size(after)
        cap = Decimal(out["reserves"][code]["soroban_fiat_total"])
        rows = []
        for r in s["rungs"]:
            size = r["send_amount"]
            sb, via = best_soroban(r)
            b, a = hb.get(size, {}), ha.get(size, {})
            row = {"send": size,
                   "horizon_before": b.get("receive"), "horizon_after": a.get("receive"),
                   "horizon_path": b.get("path"), "horizon_verdict": b.get("verdict"),
                   "horizon_loss_pct": b.get("loss_pct"),
                   "soroban_best": str(sb) if sb is not None else None, "soroban_via": via}
            mid = before.get("reference_mid")
            if b.get("receive") and mid:
                recomputed = loss(b["receive"], size, mid)
                row["horizon_loss_recomputed"] = str(recomputed)
                row["loss_recomputation_matches_ladder"] = recomputed == Decimal(b["loss_pct"]).quantize(Decimal("0.01"))
                # f(S-x) + g(x) <= f(S) + g(S) for increasing f and g, so no
                # split of S across Horizon's route and a Soroban route can
                # deliver more than the two full-size quotes added together.
                bound = Decimal(b["receive"]) + (sb if sb is not None else Decimal(0))
                row["split_upper_bound_receive"] = str(bound)
                row["split_upper_bound_loss_pct"] = str(loss(bound, size, mid))
                row["split_upper_bound_grade"] = grade(loss(bound, size, mid))
            if b.get("receive") and a.get("receive"):
                row["horizon_drift_pct"] = str(pct(Decimal(a["receive"]), Decimal(b["receive"])))
            if sb is not None and b.get("receive"):
                hb_amt = Decimal(b["receive"])
                row["soroban_vs_horizon_pct"] = str(pct(sb, hb_amt))
                row["soroban_beats_both_horizon"] = bool(
                    sb > hb_amt and (not a.get("receive") or sb > Decimal(a["receive"])))
            if b.get("receive"):
                row["horizon_exceeds_all_soroban_reserves"] = Decimal(b["receive"]) > cap
            rows.append(row)
        out["comparison"][code] = {
            "reference_mid": before.get("reference_mid"),
            "reference_source": before.get("reference_source"),
            "horizon_before_measured_at": before.get("measured_at"),
            "horizon_after_measured_at": after.get("measured_at"),
            "soroban_window": [s["soroban_started_at"], s["soroban_finished_at"]],
            "soroban_fiat_reserves_total": str(cap),
            "rungs": rows,
        }

    with open(os.path.join(day, "analysis.json"), "w") as f:
        json.dump(out, f, indent=1)
        f.write("\n")
    return out


if __name__ == "__main__":
    o = main(sys.argv[1])
    for code, r in o["reserves"].items():
        print(code, {k: v for k, v in r.items() if k != "soroban_pools"})
    for code, c in o["comparison"].items():
        print("==", code, c["reference_mid"], c["soroban_window"], "cap", c["soroban_fiat_reserves_total"])
        for row in c["rungs"]:
            print(" | ".join(str(row.get(k)) for k in ("send", "horizon_before", "horizon_after", "horizon_drift_pct",
                                                       "soroban_best", "soroban_via", "soroban_vs_horizon_pct",
                                                       "horizon_path", "horizon_verdict", "horizon_loss_pct",
                                                       "loss_recomputation_matches_ladder",
                                                       "split_upper_bound_loss_pct", "split_upper_bound_grade")))
