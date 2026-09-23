#!/usr/bin/env bash
#
# Start or stop the three server instances the browser QA harness drives.
#
#   docs/qa/browser/servers.sh start    # build ./cmd/wayfared and listen on 8099/8098/8097
#   docs/qa/browser/servers.sh stop     # stop them
#   docs/qa/browser/servers.sh status   # report which ports answer
#
# Three instances, because the causes the UI must display come from the server's
# own behaviour, not from a mock:
#
#   8099  normal          -history-first over the embedded history. Serves the UI
#                         and answers the deterministic 400s (unknown asset,
#                         malformed size, unknown query parameter) with no
#                         network access at all.
#   8098  upstream-down   empty store, Horizon pointed at a closed port, so a
#                         live measurement fails and the server returns a real
#                         502 measurement_failed.
#   8097  upstream-slow   empty store, Horizon black-holed, -timeout=2s, so a
#                         live measurement exceeds the deadline and the server
#                         returns a real 504 upstream_timeout.
#
# RUNDIR defaults to /tmp/wayfare-qa and holds the binary, logs and pid file.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
RUN_DIR="${WAYFARE_QA_RUNDIR:-/tmp/wayfare-qa}"
BIN="$RUN_DIR/wayfared"
PIDFILE="$RUN_DIR/pids"

start() {
  mkdir -p "$RUN_DIR/empty"
  echo "building $BIN"
  (cd "$ROOT" && go build -o "$BIN" ./cmd/wayfared)

  : >"$PIDFILE"
  setsid "$BIN" -schedule=0 -history-first -addr=127.0.0.1:8099 -log-level=warn \
    >"$RUN_DIR/normal.log" 2>&1 &
  echo $! >>"$PIDFILE"
  setsid "$BIN" -schedule=0 -addr=127.0.0.1:8098 -data "$RUN_DIR/empty" \
    -horizon http://127.0.0.1:9 -log-level=warn >"$RUN_DIR/down.log" 2>&1 &
  echo $! >>"$PIDFILE"
  setsid "$BIN" -schedule=0 -addr=127.0.0.1:8097 -data "$RUN_DIR/empty" \
    -timeout=2s -horizon http://10.255.255.1:9 -log-level=warn >"$RUN_DIR/slow.log" 2>&1 &
  echo $! >>"$PIDFILE"

  for port in 8099 8098 8097; do
    for _ in $(seq 1 50); do
      if curl -sf -m 2 "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then break; fi
      sleep 0.2
    done
  done

  status
}

stop() {
  if [ -f "$PIDFILE" ]; then
    while read -r pid; do
      [ -n "$pid" ] && kill "$pid" 2>/dev/null || true
    done <"$PIDFILE"
    rm -f "$PIDFILE"
  fi
  echo "stopped"
}

status() {
  for port in 8099 8098 8097; do
    if curl -sf -m 2 "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
      echo "  $port up"
    else
      echo "  $port DOWN"
    fi
  done
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  status) status ;;
  *) echo "usage: $0 {start|stop|status}" >&2; exit 2 ;;
esac
