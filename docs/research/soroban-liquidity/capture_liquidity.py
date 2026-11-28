"""Issue #681: how much fiat-token liquidity sits in Soroban AMMs that
Horizon strict-send pathfinding cannot route through?

Enumerates every pool of three Soroban AMMs from on-chain state (not from
the protocols' web APIs), keeps those holding a measured fiat token, records
reserves, and records the classic liquidity Horizon *can* route through for
the same tokens: classic AMM pools and open offers selling the token.

    python3 -I -B capture_liquidity.py OUT.json

Read-only: simulateTransaction and getLedgerEntries never write the ledger.
"""
import json
import os
import sys

# -I (isolated mode) leaves the script's own directory off sys.path.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import sorobanrpc as rpc  # noqa: E402
from common import (FIAT, FIAT_SAC, SAC_LABEL, SOROSWAP_AGGREGATOR,
                    SOROSWAP_AGGREGATOR_WASM, SOROSWAP_FACTORY, USDC_SAC,
                    XLM_SAC, horizon_pages, http_json, HORIZON, units, utcnow)
from decimal import Decimal

FIAT_IDS = set(FIAT_SAC.values())


_names = {}


def label(addr):
    """Known corridor assets by code; anything else by the token's own name()
    (for a Stellar Asset Contract, CODE:ISSUER), falling back to the address."""
    if addr in SAC_LABEL:
        return SAC_LABEL[addr]
    if addr not in _names:
        name, _, _ = rpc.simulate(addr, "name")
        _names[addr] = name.split(":")[0] if isinstance(name, str) and name else addr
    return _names[addr]


def pool_record(protocol, address, tokens, reserves, **extra):
    rec = {
        "protocol": protocol,
        "pool": address,
        "tokens": [label(t) for t in tokens],
        "token_contracts": tokens,
        "reserves_raw": [str(r) for r in reserves],
        "reserves": [units(r) for r in reserves],
    }
    rec.update(extra)
    return rec


def batched_entries(keys, size=200):
    out, ledger = [], None
    for i in range(0, len(keys), size):
        res = rpc.get_entries(keys[i:i + size])
        ledger = res.get("latestLedger", ledger)
        out += res.get("entries") or []
    return out, ledger


def soroswap():
    n, _, ledger = rpc.simulate(SOROSWAP_FACTORY, "all_pairs_length")
    keys = [rpc.contract_data_key(SOROSWAP_FACTORY,
                                  rpc.scval([rpc.Sym("PairAddressesNIndexed"), rpc.U32(i)]))
            for i in range(n)]
    entries, _ = batched_entries(keys)
    pairs = [rpc.decode_contract_data_entry(e["xdr"])[2] for e in entries]
    insts, ledger = batched_entries([rpc.instance_key(p) for p in pairs])
    pools = []
    for e in insts:
        addr, _, inst = rpc.decode_contract_data_entry(e["xdr"])
        st = dict((k, v) for k, v in inst["storage"] if isinstance(k, int))
        tokens = [st[0], st[1]]
        if FIAT_IDS & set(tokens):
            pools.append(pool_record("soroswap", addr, tokens, [st[2], st[3]]))
    return {"pairs_enumerated": n, "pair_addresses_read": len(pairs),
            "latest_ledger": ledger, "pools": pools}


def aggregator_adapters():
    inst, res = rpc.instance_storage(SOROSWAP_AGGREGATOR)
    adapters = {}
    for k, v in inst["storage"]:
        if isinstance(k, list) and k and k[0] == "Adapter":
            d = dict(v)
            adapters[d["protocol_id"]] = d["router"]
    return {"wasm_hash": inst["executable"],
            "wasm_hash_matches_published": inst["executable"] == SOROSWAP_AGGREGATOR_WASM,
            "adapters": adapters, "latest_ledger": res.get("latestLedger")}


def aquarius(router):
    count, _, _ = rpc.simulate(router, "get_tokens_sets_count")
    pools, ledger, step = [], None, 20
    seen = 0
    for start in range(0, count, step):
        v, raw, ledger = rpc.simulate(router, "get_pools_for_tokens_range",
                                      rpc.U128(start), rpc.U128(min(start + step, count)))
        if v is None:
            raise RuntimeError("aquarius range %d: %s" % (start, raw.get("error")))
        for tokens, pmap in v:
            seen += len(pmap or [])
            if not FIAT_IDS & set(tokens):
                continue
            for _, addr in pmap or []:
                res, raw, _ = rpc.simulate(addr, "get_reserves")
                ptoks, _, _ = rpc.simulate(addr, "get_tokens")
                fee, _, _ = rpc.simulate(addr, "get_fee_fraction")
                pools.append(pool_record("aquarius", addr, ptoks or tokens, res or [],
                                         fee_bps=fee))
    return {"router": router, "token_sets_enumerated": count, "pools_enumerated": seen,
            "latest_ledger": ledger, "pools": pools}


def phoenix(multihop):
    inst, _ = rpc.instance_storage(multihop)
    factory = dict((k[0] if isinstance(k, list) else k, v) for k, v in inst["storage"])["FactoryKey"]
    addrs, _, ledger = rpc.simulate(factory, "query_pools")
    pools = []
    for a in addrs:
        info, _, _ = rpc.simulate(a, "query_pool_info")
        d = dict(info)
        pa, pb = dict(d["asset_a"]), dict(d["asset_b"])
        tokens = [pa["address"], pb["address"]]
        if FIAT_IDS & set(tokens):
            pools.append(pool_record("phoenix", a, tokens, [pa["amount"], pb["amount"]]))
    return {"multihop": multihop, "factory": factory, "pools_enumerated": len(addrs),
            "latest_ledger": ledger, "pools": pools}


def classic(code, issuer):
    asset = "%s:%s" % (code, issuer)
    lps, complete_lp = horizon_pages("/liquidity_pools", {"reserves": asset, "limit": 200})
    pools = []
    for p in lps:
        res = {r["asset"]: r["amount"] for r in p["reserves"]}
        pools.append({"id": p["id"], "reserves": res,
                      "fiat_reserve": res.get(asset, "0"),
                      "counter": [a.split(":")[0] if a != "native" else "XLM"
                                  for a in res if a != asset]})
    offers, complete_off = horizon_pages("/offers", {"selling": asset, "limit": 200})
    by_counter = {}
    total = Decimal(0)
    for o in offers:
        b = o["buying"]
        name = "XLM" if b["asset_type"] == "native" else b["asset_code"]
        by_counter[name] = by_counter.get(name, Decimal(0)) + Decimal(o["amount"])
        total += Decimal(o["amount"])
    return {
        "pools": pools,
        "pool_fiat_reserve_total": str(sum((Decimal(p["fiat_reserve"]) for p in pools), Decimal(0))),
        "pools_complete": complete_lp,
        "offers_selling_count": len(offers),
        "offers_selling_amount_total": str(total),
        "offers_selling_by_counter": {k: str(v) for k, v in sorted(by_counter.items())},
        "offers_complete": complete_off,
    }


def sac_deployment():
    """Whether each corridor token's Stellar Asset Contract exists on chain.
    An undeployed SAC means no Soroban contract can hold the token at all:
    its Soroban liquidity is zero by construction, not by observation."""
    out = {}
    for code, addr in FIAT_SAC.items():
        res = rpc.get_entries([rpc.instance_key(addr)])
        out[code] = {"contract": addr, "deployed": bool(res.get("entries")),
                     "latest_ledger": res.get("latestLedger")}
    return out


def main(out):
    started = utcnow()
    root = http_json(HORIZON + "/")
    sacs = sac_deployment()
    agg = aggregator_adapters()
    sw = soroswap()
    aq = aquarius(agg["adapters"][2])
    ph = phoenix(agg["adapters"][1])
    cl = {code: classic(code, iss) for code, iss in FIAT.items()}
    doc = {
        "issue": 681,
        "started_at": started,
        "finished_at": utcnow(),
        "sources": {"soroban_rpc": rpc.RPC, "horizon": HORIZON},
        "horizon_ledger_at_start": root.get("history_latest_ledger"),
        "contracts": {"XLM": XLM_SAC, "USDC": USDC_SAC, **FIAT_SAC},
        "sac_deployment": sacs,
        "soroswap_aggregator": agg,
        "soroban": {"soroswap": sw, "aquarius": aq, "phoenix": ph},
        "classic": cl,
    }
    with open(out, "w") as f:
        json.dump(rpc.jsonable(doc), f, indent=1, sort_keys=True)
        f.write("\n")


if __name__ == "__main__":
    main(sys.argv[1])
