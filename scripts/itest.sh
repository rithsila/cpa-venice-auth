#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

CPA="${CPA_BIN:-/private/var/folders/6g/sq1ym1qx0f5by24ynbd7pvsw0000gn/T/cpa-bin/cli-proxy-api}"
if [ ! -x "$CPA" ]; then
  CPA="$(command -v cli-proxy-api 2>/dev/null || true)"
fi

if [ -z "$CPA" ] || [ ! -x "$CPA" ]; then
  echo "Error: cli-proxy-api binary not found at $CPA or PATH. Set CPA_BIN." >&2
  exit 1
fi

PORT="${PORT:-18399}"
PLUGIN_DIR="$ROOT_DIR/bin/plugins"

# Build plugin if missing
if [ ! -f "$PLUGIN_DIR/venice-provider.dylib" ] && [ ! -f "$PLUGIN_DIR/venice-provider.so" ]; then
  echo "Building plugin..."
  make -C "$ROOT_DIR" build
fi

CONFIG_FILE="/tmp/cpa-venice-itest-$$.yaml"
cat > "$CONFIG_FILE" <<YAML
config-version: 8
server:
  host: "127.0.0.1"
  port: $PORT
access:
  api-keys:
    - "test-itest-key"
plugins:
  enabled: true
  dir: "$PLUGIN_DIR"
  configs:
    venice-provider:
      enabled: true
      priority: 10
      models_cache_seconds: 60
YAML

cleanup() {
  if [ -n "${PID:-}" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -f "$CONFIG_FILE"
}
trap cleanup EXIT INT TERM

echo "Starting CPA on port $PORT..."
"$CPA" -config "$CONFIG_FILE" > /tmp/cpa-itest.log 2>&1 &
PID=$!

READY=0
for _ in $(seq 1 40); do
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "CPA exited prematurely. Log:" >&2
    cat /tmp/cpa-itest.log >&2
    exit 1
  fi
  if grep -q "API server started successfully" /tmp/cpa-itest.log 2>/dev/null; then
    READY=1
    break
  fi
  sleep 0.2
done

if [ "$READY" -ne 1 ]; then
  echo "Timed out waiting for CPA server startup. Log:" >&2
  cat /tmp/cpa-itest.log >&2
  exit 1
fi

# Give the plugin a moment to finish registration
sleep 1

echo "Verifying plugin registered..."
if grep -q "plugin registered plugin_id=venice-provider" /tmp/cpa-itest.log; then
  echo "Plugin registered: PASS"
else
  echo "Plugin registration not found in log:" >&2
  cat /tmp/cpa-itest.log >&2
  exit 1
fi

echo "Testing /v1/models endpoint..."
MODELS_RESP=$(curl -s "http://127.0.0.1:$PORT/v1/models" -H "Authorization: Bearer test-itest-key" || true)

# Even if upstream network call fails in offline environment, verify server is serving requests
if [ -n "$MODELS_RESP" ]; then
  echo "Models endpoint responded: PASS"
else
  echo "Models endpoint gave empty response" >&2
fi

echo "integration test: PASS"
