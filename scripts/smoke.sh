#!/usr/bin/env bash
# Phase 1 smoke test for bluei-edge.
# Requires: go, curl, sqlite3 (optional for DB inspection)

set -euo pipefail

BINARY="${BINARY:-./bin/bluei-edge}"
CONFIG="${CONFIG:-./configs/edge.example.yaml}"
API="http://127.0.0.1:8080"
DB_PATH="./var/bluei-edge/edge.db"
PID_FILE="/tmp/bluei-edge-smoke.pid"

cleanup() {
  if [ -f "$PID_FILE" ]; then
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
    rm -f "$PID_FILE"
  fi
}
trap cleanup EXIT

pass() { echo "  PASS: $*"; }
fail() { echo "  FAIL: $*"; exit 1; }

echo "=== bluei-edge Phase 1 smoke test ==="

# --- Build ---
echo
echo "-- Build"
mkdir -p bin
go build -o "$BINARY" ./cmd/bluei-edge/
pass "binary built at $BINARY"

# --- check-config ---
echo
echo "-- check-config"
"$BINARY" check-config --config "$CONFIG"
pass "check-config passed"

# --- migrate ---
echo
echo "-- migrate"
"$BINARY" migrate --config "$CONFIG"
pass "migrate passed"

# Optional: confirm tables exist
if command -v sqlite3 &>/dev/null; then
  for tbl in events current_device_status current_tank_environment current_camera_status tank_profiles open_alerts control_commands sync_batches sync_batch_events runtime_kv; do
    count=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='$tbl';")
    [ "$count" = "1" ] || fail "table $tbl not found in DB"
  done
  pass "all 10 required tables present"
fi

# --- run (background) ---
echo
echo "-- run (background)"
"$BINARY" run --config "$CONFIG" &
echo $! > "$PID_FILE"
sleep 2   # brief wait for startup

# --- healthz ---
echo
echo "-- GET /healthz"
resp=$(curl -sf "$API/healthz") || fail "/healthz returned non-200"
echo "$resp" | grep -q '"status":"alive"' || fail "/healthz missing status:alive"
pass "/healthz returned alive"

# --- readyz ---
echo
echo "-- GET /readyz"
resp=$(curl -sf "$API/readyz") || fail "/readyz returned non-200"
echo "$resp" | grep -qE '"status":"(ready|degraded)"' || fail "/readyz unexpected status"
pass "/readyz OK"

# --- /v1/status ---
echo
echo "-- GET /v1/status"
resp=$(curl -sf "$API/v1/status") || fail "/v1/status returned non-200"
echo "$resp" | grep -q '"edge_id"' || fail "/v1/status missing edge_id"
pass "/v1/status OK"

# --- /v1/sync/status ---
echo
echo "-- GET /v1/sync/status"
resp=$(curl -sf "$API/v1/sync/status") || fail "/v1/sync/status returned non-200"
echo "$resp" | grep -q '"endpoint_configured"' || fail "/v1/sync/status missing field"
pass "/v1/sync/status OK"

# --- /v1/operations/status ---
echo
echo "-- GET /v1/operations/status"
resp=$(curl -sf "$API/v1/operations/status") || fail "/v1/operations/status returned non-200"
echo "$resp" | grep -q '"overall"' || fail "/v1/operations/status missing overall"
echo "$resp" | grep -q '"cameras"' || fail "/v1/operations/status missing cameras"
echo "$resp" | grep -q '"sync"' || fail "/v1/operations/status missing sync"
pass "/v1/operations/status OK"

# --- /v1/readings/latest ---
echo
echo "-- GET /v1/readings/latest?tank_id=tank_01"
resp=$(curl -sf "$API/v1/readings/latest?tank_id=tank_01") || fail "/v1/readings/latest returned non-200"
echo "$resp" | grep -q '"metric":"dissolved_oxygen"' || fail "/v1/readings/latest missing dissolved_oxygen"
pass "/v1/readings/latest OK"

# --- /v1/tanks ---
echo
echo "-- GET /v1/tanks"
resp=$(curl -sf "$API/v1/tanks") || fail "/v1/tanks returned non-200"
echo "$resp" | grep -q '"tank_id":"tank_01"' || fail "/v1/tanks missing tank_01"
pass "/v1/tanks OK"

# --- /v1/tanks/{tank_id}/profile ---
echo
echo "-- GET /v1/tanks/tank_01/profile"
resp=$(curl -sf "$API/v1/tanks/tank_01/profile") || fail "/v1/tanks/tank_01/profile returned non-200"
echo "$resp" | grep -q '"species":"atlantic_salmon"' || fail "/v1/tanks/tank_01/profile missing species"
pass "/v1/tanks/tank_01/profile OK"

# --- /v1/tanks/{tank_id}/state ---
echo
echo "-- GET /v1/tanks/tank_01/state"
resp=$(curl -sf "$API/v1/tanks/tank_01/state") || fail "/v1/tanks/tank_01/state returned non-200"
echo "$resp" | grep -q '"tank_id":"tank_01"' || fail "/v1/tanks/tank_01/state missing tank_id"
echo "$resp" | grep -q '"environment"' || fail "/v1/tanks/tank_01/state missing environment"
echo "$resp" | grep -q '"cameras"' || fail "/v1/tanks/tank_01/state missing cameras"
pass "/v1/tanks/tank_01/state OK"

# --- /v1/devices ---
echo
echo "-- GET /v1/devices"
resp=$(curl -sf "$API/v1/devices") || fail "/v1/devices returned non-200"
pass "/v1/devices OK"

# --- /v1/alerts/open ---
echo
echo "-- GET /v1/alerts/open"
resp=$(curl -sf "$API/v1/alerts/open") || fail "/v1/alerts/open returned non-200"
pass "/v1/alerts/open OK"

# --- POST /v1/feedings ---
echo
echo "-- POST /v1/feedings"
resp=$(curl -sf -X POST "$API/v1/feedings" \
  -H "Content-Type: application/json" \
  -d '{"tank_id":"tank_01","feeder_id":"feeder_01","feed_amount_g":123.5,"feed_type":"pellet","recorded_by":"smoke-test"}') || fail "/v1/feedings returned non-2xx"
echo "$resp" | grep -q '"ok":true' || fail "/v1/feedings missing ok:true"
pass "/v1/feedings OK"

# --- GET /v1/feedings/recent ---
echo
echo "-- GET /v1/feedings/recent"
resp=$(curl -sf "$API/v1/feedings/recent?limit=5") || fail "/v1/feedings/recent returned non-200"
echo "$resp" | grep -q '"feedings"' || fail "/v1/feedings/recent missing feedings"
pass "/v1/feedings/recent OK"

# --- GET /v1/feedings/today ---
echo
echo "-- GET /v1/feedings/today"
resp=$(curl -sf "$API/v1/feedings/today") || fail "/v1/feedings/today returned non-200"
echo "$resp" | grep -q '"total_feed_g"' || fail "/v1/feedings/today missing total_feed_g"
pass "/v1/feedings/today OK"

# --- POST /v1/gateway/readings ---
echo
echo "-- POST /v1/gateway/readings"
resp=$(curl -sf -X POST "$API/v1/gateway/readings" \
  -H "Content-Type: application/json" \
  -d '{"gateway_id":"lattepanda_01","adapter_id":"lattepanda-smoke","readings":[{"sensor_id":"scale_01","device_id":"feeder_01","metric":"feed_weight","value":7820,"unit":"g","quality":"ok","observed_at":"2026-05-05T06:00:00Z","location":{"tank_id":"tank_01"},"raw":{"source":"smoke-test"}}]}') || fail "/v1/gateway/readings returned non-2xx"
echo "$resp" | grep -q '"accepted":1' || fail "/v1/gateway/readings missing accepted:1"
pass "/v1/gateway/readings OK"

# --- POST /v1/gateway/device-health ---
echo
echo "-- POST /v1/gateway/device-health"
resp=$(curl -sf -X POST "$API/v1/gateway/device-health" \
  -H "Content-Type: application/json" \
  -d '{"gateway_id":"lattepanda_01","adapter_id":"lattepanda-smoke","devices":[{"device_id":"lattepanda_01","device_type":"local_gateway","tank_id":"tank_01","status":"online","quality":"ok","last_seen_at":"2026-05-05T06:00:00Z","details":{"source":"smoke-test"}}]}') || fail "/v1/gateway/device-health returned non-2xx"
echo "$resp" | grep -q '"accepted":1' || fail "/v1/gateway/device-health missing accepted:1"
pass "/v1/gateway/device-health OK"

# --- ESP32 controller polling flow ---
echo
echo "-- ESP32 controller polling flow"
esp32_idempotency_key="smoke-esp32-$(date +%s%N)"
resp=$(curl -sf -X POST "$API/v1/control/commands" \
  -H "Content-Type: application/json" \
  -d "{\"idempotency_key\":\"$esp32_idempotency_key\",\"requested_by\":{\"type\":\"operator\",\"id\":\"tester\"},\"target\":{\"device_id\":\"feeder_esp32_01\"},\"command\":{\"type\":\"feed.start\",\"params\":{\"amount_g\":10}},\"expires_in_sec\":60}") || fail "ESP32 command submit returned non-2xx"
echo "$resp" | grep -q '"status":"accepted"' || fail "ESP32 command not accepted"
cmd_id=$(printf '%s' "$resp" | sed -n 's/.*"command_id":"\([^"]*\)".*/\1/p')
[ -n "$cmd_id" ] || fail "ESP32 command_id missing"
resp=$(curl -sf "$API/v1/controllers/esp32_feeder_controller_01/commands/next") || fail "controller next returned non-200"
echo "$resp" | grep -q "$cmd_id" || fail "controller next missing command_id"
resp=$(curl -sf -X POST "$API/v1/controllers/esp32_feeder_controller_01/commands/$cmd_id/ack" \
  -H "Content-Type: application/json" \
  -d '{"ack":{"firmware":"smoke","accepted":true}}') || fail "controller ack returned non-2xx"
echo "$resp" | grep -q '"status":"acknowledged"' || fail "controller ack not acknowledged"
resp=$(curl -sf -X POST "$API/v1/controllers/esp32_feeder_controller_01/commands/$cmd_id/result" \
  -H "Content-Type: application/json" \
  -d '{"result":"succeeded","details":{"dispensed_g":10}}') || fail "controller result returned non-2xx"
echo "$resp" | grep -q '"ok":true' || fail "controller result missing ok:true"
pass "ESP32 controller polling flow OK"

# --- health subcommand ---
echo
echo "-- health subcommand"
"$BINARY" health --url "$API/healthz"
pass "health subcommand OK"

# --- POST /v1/control/commands (unknown device should 422) ---
echo
echo "-- POST /v1/control/commands (unknown device → 422)"
http_code=$(curl -s -o /dev/null -w "%{http_code}" \
  -X POST "$API/v1/control/commands" \
  -H "Content-Type: application/json" \
  -d '{"idempotency_key":"smoke-test-001","requested_by":{"type":"operator","id":"tester"},"target":{"device_id":"nonexistent_device"},"command":{"type":"feed.start"},"expires_in_sec":60}')
[ "$http_code" = "422" ] || fail "expected 422 for unknown device, got $http_code"
pass "unknown device correctly rejected with 422"

echo
echo "=== SMOKE TEST PASSED ==="
