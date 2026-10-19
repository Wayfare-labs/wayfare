#!/usr/bin/env bash
# Post-deploy smoke check for a running wayfared instance.
#
# Usage: smoke-check.sh [base-url]   (default http://127.0.0.1:8080)
#
# Proves the deployed instance *answers correctly*, which the build job and
# the -verify-store run do not: it exercises the HTTP surface the way a client
# would and asserts on the wire shape. The default image entrypoint runs with
# -history-first, so /api/corridor serves recorded history and never touches
# the network — the check stays offline by construction and never measures
# live. Nothing is asserted beyond what the served document itself states:
# freshness is read from `live` and `stale`, never assumed.
#
# The same assertions exist as a Go test (server/smoke_test.go) so a change
# that breaks them fails `go test` too, not just CI.
set -euo pipefail

BASE="${1:-http://127.0.0.1:8080}"
die() { echo "smoke check FAILED: $*" >&2; exit 1; }

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"

# ---------------------------------------------------------------- health ----
echo "== GET $BASE/healthz"
HEALTH="$(curl -fsS --max-time 10 "$BASE/healthz")" || die "/healthz did not answer 200"
[ "$(printf '%s' "$HEALTH" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("status",""))')" = "ok" ] \
  || die "/healthz did not report status ok: $HEALTH"

# -------------------------------------------------------------- UI page -----
# Substring tests rather than `printf | grep -q`: under pipefail, grep -q's
# early exit after a match SIGPIPEs the writer and turns a match into a
# failure.
echo "== GET $BASE/"
UI="$(curl -fsS --max-time 10 "$BASE/")" || die "/ did not answer 200"
[[ "$UI" == *"Corridor integrity monitor"* ]] \
  || die "the UI at / is not the corridor monitor page"
[[ "$UI" == *"Wayfare is non-custodial"* ]] \
  || die "the UI at / is missing the provenance footer"

# ------------------------------------------------------- corridor request ---
echo "== GET $BASE/api/corridor?to=NGNC"
CORRIDOR="$(curl -fsS --max-time 120 "$BASE/api/corridor?to=NGNC")" \
  || die "/api/corridor did not answer 200"

assert() { printf '%s' "$CORRIDOR" | python3 -c "$1" || die "$2"; }

assert '
import json,sys
d=json.load(sys.stdin)
assert "live" in d, "live field absent: freshness must never be guessed by the client"
assert d["finding"], "finding empty"
assert d["reference_mid"], "reference_mid empty"
assert d["reference_source"], "reference_source empty"
assert isinstance(d["rungs"], list) and d["rungs"], "rungs empty"
for r in d["rungs"]:
    assert "send_amount" in r and "priced" in r, "rung missing send_amount/priced"
    if r["priced"]:
        assert r.get("quote"), "priced rung missing its quote"
        assert "loss_pct" in r["quote"], "priced rung quote missing loss_pct"
assert "recommended" in d, "recommended field absent: clients must not have to guess"
if d["recommended"] is not None:
    assert d.get("recommended_size"), "recommendation without a size"
' "the served corridor document does not carry the wire shape clients rely on"

# History-first deployments answer from the chain: whatever freshness is
# served, the document must state which it was.
printf '%s' "$CORRIDOR" | python3 -c '
import json,sys
d=json.load(sys.stdin)
if d["live"] is False:
    assert d.get("stale"), "a non-live reading must carry its stale envelope"
' || die "freshness labelling is wrong (live:false without a stale envelope)"

# ---------------------------------------------------------------- assets ----
echo "== GET $BASE/api/assets"
ASSETS="$(curl -fsS --max-time 10 "$BASE/api/assets")" || die "/api/assets did not answer 200"
[ "$(printf '%s' "$ASSETS" | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["assets"]) > 0)')" = "True" ] \
  || die "/api/assets returned no assets"

echo "smoke check passed"
