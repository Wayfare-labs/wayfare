"""Shared constants and Horizon helpers for the Soroban liquidity research.

Read-only. Nothing here signs or submits anything.
"""
import json
import time
import urllib.parse
import urllib.request
from decimal import Decimal

from sac import sac

HORIZON = "https://horizon.stellar.org"

# Issuers as asset/known.go records them (each read from the issuer's own
# stellar.toml, per SEP-1). Asset code alone never identifies an asset.
USDC_ISSUER = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
LINKIO_ISSUER = "GASBV6W7GGED66MXEVC7YZHTWWYMSVYEY35USF2HJZBLABLYIFQGXZY6"
COWRIE_ISSUER = "GAWODAROMJ33V5YDFY3NPYTHVYQG7MJXVJ2ND3AOGIHYRWINES6ACCPD"

# The corridors with recorded history under data/.
FIAT = {
    "NGNC": LINKIO_ISSUER,
    "NGNT": COWRIE_ISSUER,
    "GHSC": LINKIO_ISSUER,
    "KESC": LINKIO_ISSUER,
}

# Protocol entry points. Soroswap's come from its own published
# public/mainnet.contracts.json (soroswap/core and soroswap/aggregator); the
# Phoenix and Aquarius ones are read out of the Soroswap aggregator's on-chain
# adapter storage at capture time, so they are not typed in here.
SOROSWAP_FACTORY = "CA4HEQTL2WPEUYKYKCDOHCDNIV4QHNJ7EL4J4NQ6VADP7SYHVRYZ7AW2"
SOROSWAP_ROUTER = "CAG5LRYQ5JVEUI5TEID72EYOVX44TTUJT5BQR2J6J77FH65PCCFAJDDH"
SOROSWAP_AGGREGATOR = "CAYP3UWLJM7ZPTUKL6R6BFGTRWLZ46LRKOXTERI2K6BIJAWGYY62TXTO"
# Published wasm hash of the aggregator (same file); checked against the
# deployed instance so a moved address is noticed rather than trusted.
SOROSWAP_AGGREGATOR_WASM = "5e0bff5a29e2473b5289fe96aeb59b48ac335e5e87c46632b53be1f662447d2c"

AQUARIUS_API = "https://amm-api.aqua.network/api/external/v1"

STROOP = Decimal(10) ** 7  # every SAC and classic asset here has 7 decimals

XLM_SAC = sac("XLM", "")
USDC_SAC = sac("USDC", USDC_ISSUER)
FIAT_SAC = {code: sac(code, iss) for code, iss in FIAT.items()}
SAC_LABEL = {XLM_SAC: "XLM", USDC_SAC: "USDC", **{v: k for k, v in FIAT_SAC.items()}}


def units(raw):
    """Contract integer amount -> Decimal units, as a string."""
    return str((Decimal(int(raw)) / STROOP).normalize())


def utcnow():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())


def http_json(url, data=None, headers=None):
    h = {"User-Agent": "research-script", "Accept": "application/json"}
    h.update(headers or {})
    req = urllib.request.Request(url, data, h)
    with urllib.request.urlopen(req, timeout=60) as r:
        return json.load(r)


def horizon_pages(path, params, cap=100):
    """Follows Horizon's `next` links; returns all records (up to cap pages)."""
    url = HORIZON + path + "?" + urllib.parse.urlencode(params)
    out = []
    for _ in range(cap):
        d = http_json(url)
        recs = d["_embedded"]["records"]
        out += recs
        if len(recs) < int(params.get("limit", 10)):
            return out, True
        url = d["_links"]["next"]["href"]
    return out, False
