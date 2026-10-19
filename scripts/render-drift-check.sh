#!/usr/bin/env bash
# Compare render.yaml against the live deployed service settings on Render.
#
# Usage:
#   render-drift-check.sh [options]
#
# Options:
#   --render-yaml PATH      Path to render.yaml (default: ./render.yaml)
#   --service-id ID         Render Service ID (or via RENDER_SERVICE_ID env var)
#   --api-key KEY           Render API Key (or via RENDER_API_KEY env var)
#   --mock-service FILE     Path to mock service JSON (for offline testing)
#   --mock-env FILE         Path to mock env-vars JSON (for offline testing)
#   -h, --help              Show this help message and exit
#
# Exit codes:
#   0 - No drift detected: live service settings match render.yaml
#   1 - Drift detected: one or more settings differ
#   2 - Configuration / execution error
#
# The check is strictly read-only: it issues only GET requests to the
# Render REST API v1.
set -euo pipefail

die_usage() { echo "Error: $*" >&2; echo "Run with --help for usage." >&2; exit 2; }
die_error() { echo "Error: $*" >&2; exit 2; }

RENDER_YAML="./render.yaml"
SERVICE_ID="${RENDER_SERVICE_ID:-}"
API_KEY="${RENDER_API_KEY:-}"
MOCK_SERVICE=""
MOCK_ENV=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --render-yaml)
      RENDER_YAML="$2"
      shift 2
      ;;
    --service-id)
      SERVICE_ID="$2"
      shift 2
      ;;
    --api-key)
      API_KEY="$2"
      shift 2
      ;;
    --mock-service)
      MOCK_SERVICE="$2"
      shift 2
      ;;
    --mock-env)
      MOCK_ENV="$2"
      shift 2
      ;;
    -h|--help)
      sed -n '2,17p' "$0" | sed 's/^# //' | sed 's/^#//'
      exit 0
      ;;
    *)
      die_usage "unknown option: $1"
      ;;
  esac
done

command -v python3 >/dev/null 2>&1 || die_error "python3 is required"

[[ -r "$RENDER_YAML" ]] || die_error "render.yaml not readable at '$RENDER_YAML'"

SERVICE_JSON=""
ENV_JSON=""

if [[ -n "$MOCK_SERVICE" ]]; then
  [[ -r "$MOCK_SERVICE" ]] || die_error "mock service file not readable: '$MOCK_SERVICE'"
  SERVICE_JSON="$(cat "$MOCK_SERVICE")"
  if [[ -n "$MOCK_ENV" ]]; then
    [[ -r "$MOCK_ENV" ]] || die_error "mock env file not readable: '$MOCK_ENV'"
    ENV_JSON="$(cat "$MOCK_ENV")"
  else
    ENV_JSON="[]"
  fi
else
  command -v curl >/dev/null 2>&1 || die_error "curl is required for live checks"
  [[ -n "$API_KEY" ]] || die_error "Render API key required: set RENDER_API_KEY or pass --api-key"

  AUTH_HEADER="Authorization: Bearer $API_KEY"
  ACCEPT_HEADER="Accept: application/json"

  # If SERVICE_ID is not provided, look it up by service name
  if [[ -z "$SERVICE_ID" ]]; then
    SERVICE_NAME="$(python3 -c '
import re, sys
with open(sys.argv[1]) as f:
    text = f.read()
m = re.search(r"name:\s*([^\s#]+)", text)
if m:
    print(m.group(1))
' "$RENDER_YAML")"
    [[ -n "$SERVICE_NAME" ]] || die_error "could not determine service name from $RENDER_YAML"

    echo "== Looking up service ID for '$SERVICE_NAME'..." >&2
    SERVICES_LIST="$(curl -fsS --max-time 15 \
      -H "$AUTH_HEADER" -H "$ACCEPT_HEADER" \
      "https://api.render.com/v1/services?limit=50")" || die_error "failed to fetch services list from Render API"

    SERVICE_ID="$(python3 -c '
import json, sys
data = json.loads(sys.argv[1])
name = sys.argv[2]
items = data if isinstance(data, list) else data.get("items", [])
for entry in items:
    svc = entry.get("service", entry)
    if svc.get("name") == name:
        print(svc.get("id", ""))
        sys.exit(0)
sys.exit(1)
' "$SERVICES_LIST" "$SERVICE_NAME" 2>/dev/null)" || die_error "service with name '$SERVICE_NAME' not found in Render account"
    echo "Found service ID: $SERVICE_ID" >&2
  fi

  echo "== Fetching live settings for service $SERVICE_ID..." >&2
  SERVICE_JSON="$(curl -fsS --max-time 15 \
    -H "$AUTH_HEADER" -H "$ACCEPT_HEADER" \
    "https://api.render.com/v1/services/$SERVICE_ID")" || die_error "failed to fetch service details from Render API"

  echo "== Fetching environment variables for service $SERVICE_ID..." >&2
  ENV_JSON="$(curl -fsS --max-time 15 \
    -H "$AUTH_HEADER" -H "$ACCEPT_HEADER" \
    "https://api.render.com/v1/services/$SERVICE_ID/env-vars")" || die_error "failed to fetch service env-vars from Render API"
fi

# Compare render.yaml with live service settings using python3
python3 -c '
import json, re, sys

yaml_path = sys.argv[1]
svc_raw = sys.argv[2]
env_raw = sys.argv[3]

with open(yaml_path) as f:
    yaml_text = f.read()

# Parse render.yaml expected values
expected = {}
for line in yaml_text.splitlines():
    line = line.split("#")[0].strip()
    if not line:
        continue
    m = re.match(r"^([a-zA-Z0-9_-]+):\s*(.*)$", line)
    if m:
        expected[m.group(1)] = m.group(2).strip()

expected_envs = {}
env_matches = re.findall(r"-\s*key:\s*([^\s#]+)\s+value:\s*([^\s#]+)", yaml_text)
for k, v in env_matches:
    expected_envs[k] = v

svc = json.loads(svc_raw)
if "service" in svc:
    svc = svc["service"]

details = svc.get("serviceDetails", {})

live_plan = details.get("plan") or svc.get("plan", "")
live_region = details.get("region") or svc.get("region", "")
live_cmd = details.get("dockerCommand") or svc.get("dockerCommand", "")
live_health = details.get("healthCheckPath") or svc.get("healthCheckPath", "")
live_dockerfile = details.get("dockerfilePath") or svc.get("dockerfilePath", "")

env_data = json.loads(env_raw) if env_raw.strip() else []
if isinstance(env_data, dict) and "items" in env_data:
    items = env_data["items"]
elif isinstance(env_data, list):
    items = env_data
else:
    items = []

live_envs = {}
for item in items:
    ev = item.get("envVar", item)
    if isinstance(ev, dict) and "key" in ev:
        live_envs[ev["key"]] = ev.get("value", "")

diffs = []

def check_field(field_name, exp_val, act_val):
    if exp_val and exp_val != act_val:
        act_display = act_val if act_val else "<empty>"
        diffs.append(f"  - {field_name}:\n      expected: {exp_val}\n      actual:   {act_display}")

check_field("dockerCommand (start command flags)", expected.get("dockerCommand"), live_cmd)
check_field("plan", expected.get("plan"), live_plan)
check_field("region", expected.get("region"), live_region)
check_field("healthCheckPath", expected.get("healthCheckPath"), live_health)
if expected.get("dockerfilePath"):
    check_field("dockerfilePath", expected.get("dockerfilePath"), live_dockerfile)

for k, exp_v in expected_envs.items():
    act_v = live_envs.get(k)
    if act_v != exp_v:
        act_display = act_v if act_v else "<missing>"
        diffs.append(f"  - envVar {k}:\n      expected: {exp_v}\n      actual:   {act_display}")

svc_name = svc.get("name", "unknown")
svc_id = svc.get("id", "unknown")
print(f"Comparing {yaml_path} with live service {svc_name} ({svc_id}):")
if not diffs:
    print("OK: live service settings match render.yaml (no drift detected)")
    print(f"  - plan:            {live_plan}")
    print(f"  - region:          {live_region}")
    print(f"  - dockerCommand:   {live_cmd}")
    print(f"  - healthCheckPath: {live_health}")
    for k, v in expected_envs.items():
        print(f"  - envVar {k}: {live_envs.get(k)}")
    sys.exit(0)
else:
    print(f"DRIFT DETECTED: {len(diffs)} setting(s) differ from {yaml_path}:")
    for d in diffs:
        print(d)
    sys.exit(1)
' "$RENDER_YAML" "$SERVICE_JSON" "$ENV_JSON"
