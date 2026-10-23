#!/usr/bin/env bash
# Offline test for scripts/render-drift-check.sh.
set -euo pipefail

DIR="$(mktemp -d)"
trap 'rm -rf "$DIR"' EXIT

cat > "$DIR/matching_service.json" <<'EOF'
{
  "id": "srv-test123456",
  "name": "wayfare",
  "type": "web_service",
  "serviceDetails": {
    "plan": "free",
    "region": "frankfurt",
    "dockerfilePath": "./Dockerfile",
    "healthCheckPath": "/healthz",
    "dockerCommand": "/wayfared -addr 0.0.0.0:8080 -schedule=0 -history-first"
  }
}
EOF

cat > "$DIR/matching_env.json" <<'EOF'
{
  "items": [
    {
      "envVar": {
        "key": "WAYFARE_LOG_LEVEL",
        "value": "info"
      }
    }
  ]
}
EOF

cat > "$DIR/drifted_service.json" <<'EOF'
{
  "id": "srv-test123456",
  "name": "wayfare",
  "type": "web_service",
  "serviceDetails": {
    "plan": "starter",
    "region": "frankfurt",
    "dockerfilePath": "./Dockerfile",
    "healthCheckPath": "/healthz",
    "dockerCommand": "/wayfared -addr 0.0.0.0:8080"
  }
}
EOF

cat > "$DIR/drifted_env.json" <<'EOF'
{
  "items": [
    {
      "envVar": {
        "key": "WAYFARE_LOG_LEVEL",
        "value": "debug"
      }
    }
  ]
}
EOF

echo "== Test 1: Matching live settings exits 0"
MATCH_OUT="$(./scripts/render-drift-check.sh \
  --render-yaml ./render.yaml \
  --mock-service "$DIR/matching_service.json" \
  --mock-env "$DIR/matching_env.json")"
[[ "$MATCH_OUT" == *"OK: live service settings match render.yaml (no drift detected)"* ]] || {
  echo "Expected match output, got: $MATCH_OUT" >&2
  exit 1
}

echo "== Test 2: Drifted settings exits 1 and reports differences"
set +e
DRIFT_OUT="$(./scripts/render-drift-check.sh \
  --render-yaml ./render.yaml \
  --mock-service "$DIR/drifted_service.json" \
  --mock-env "$DIR/drifted_env.json" 2>&1)"
STATUS=$?
set -e
if [[ $STATUS -ne 1 ]]; then
  echo "Expected exit code 1 for drift, got $STATUS" >&2
  exit 1
fi
[[ "$DRIFT_OUT" == *"DRIFT DETECTED"* ]] || {
  echo "Expected 'DRIFT DETECTED', got: $DRIFT_OUT" >&2
  exit 1
}
[[ "$DRIFT_OUT" == *"dockerCommand"* ]] || {
  echo "Expected dockerCommand diff, got: $DRIFT_OUT" >&2
  exit 1
}
[[ "$DRIFT_OUT" == *"plan"* ]] || {
  echo "Expected plan diff, got: $DRIFT_OUT" >&2
  exit 1
}
[[ "$DRIFT_OUT" == *"envVar WAYFARE_LOG_LEVEL"* ]] || {
  echo "Expected envVar diff, got: $DRIFT_OUT" >&2
  exit 1
}

echo "== Test 3: Missing render.yaml or API key exits 2"
set +e
./scripts/render-drift-check.sh --render-yaml "$DIR/nonexistent.yaml" >/dev/null 2>&1
STATUS=$?
set -e
if [[ $STATUS -ne 2 ]]; then
  echo "Expected exit code 2 for missing file, got $STATUS" >&2
  exit 1
fi

echo "all render-drift-check tests passed"
