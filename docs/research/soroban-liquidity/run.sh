#!/usr/bin/env bash
# Reproduces the 2026-11-23 captures for #681 and #682 into a new dated
# directory. Read-only against mainnet: Soroban RPC simulations, ledger-entry
# reads, Horizon GETs and Aquarius's find-path quote. Nothing is signed.
#
#   docs/research/soroban-liquidity/run.sh            # writes ./YYYY-MM-DD/
#
# Python 3 standard library only; Go to build cmd/ladder.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
day=$(date -u +%Y-%m-%d)
out="$here/$day"
mkdir -p "$out"
bin=$(mktemp -d)/ladder
(cd "$root" && go build -o "$bin" ./cmd/ladder)

python3 -I -B "$here/capture_liquidity.py" "$out/liquidity.json"
python3 -I -B "$here/capture_quotes.py" "$bin" "$out" NGNC NGNT
python3 -I -B "$here/capture_liquidity.py" "$out/liquidity-after.json"
python3 -I -B "$here/probe_split.py" "$bin" "$out/split-probe-NGNC.json" NGNC
python3 -I -B "$here/analyze.py" "$out"
(cd "$out" && sha256sum -- *.json > SHA256SUMS)
