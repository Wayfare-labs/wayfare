#!/bin/bash
export PATH=/workspace/chats/884c0999674a53118a8a4b3d/scripts/tmp/go/bin:$PATH
cd /workspace/chats/884c0999674a53118a8a4b3d/scripts/tmp/wf
BIN=/workspace/chats/884c0999674a53118a8a4b3d/scripts/tmp/wf-noise/ladder
go build -o "$BIN" ./cmd/ladder || exit 1
out=/workspace/chats/884c0999674a53118a8a4b3d/scripts/tmp/wf-noise
for i in $(seq 1 31); do
  ts=$(date -u +%Y%m%dT%H%M%SZ)
  for to in NGNC NGNT; do
    "$BIN" -to $to -json -checks=false > "$out/${ts}-${to}.json" 2>>"$out/err.log"
    echo "$(date -u +%H:%M:%S) run $i $to exit=$?" >> "$out/progress.log"
  done
  [ "$i" -lt 31 ] && sleep 105
done
echo "DONE $(date -u)" >> "$out/progress.log"
