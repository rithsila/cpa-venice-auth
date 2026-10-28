#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

VENICE_API_KEY="${VENICE_API_KEY:-}"
VENICE_BASE_URL="${VENICE_BASE_URL:-https://api.venice.ai/api/v1}"

if [ -z "$VENICE_API_KEY" ]; then
  echo "Notice: VENICE_API_KEY not set. Running mock-backed smoke test..."
  go test -v -run TestExecuteNonStreamingSuccess "$ROOT_DIR/internal/executor"
  echo "Smoke test (mock): PASS"
  exit 0
fi

echo "Running live smoke test against $VENICE_BASE_URL..."
HTTP_STATUS=$(curl -s -o /tmp/smoke-models.json -w "%{http_code}" \
  "$VENICE_BASE_URL/models" \
  -H "Authorization: Bearer $VENICE_API_KEY")

if [ "$HTTP_STATUS" != "200" ]; then
  echo "Error: models request returned status $HTTP_STATUS" >&2
  cat /tmp/smoke-models.json >&2
  exit 1
fi

echo "Live models call: PASS"

HTTP_STATUS=$(curl -s -o /tmp/smoke-chat.json -w "%{http_code}" \
  "$VENICE_BASE_URL/chat/completions" \
  -H "Authorization: Bearer $VENICE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"venice-uncensored-1-2","messages":[{"role":"user","content":"say ok"}],"max_tokens":5}')

if [ "$HTTP_STATUS" != "200" ]; then
  echo "Error: chat completions returned status $HTTP_STATUS" >&2
  cat /tmp/smoke-chat.json >&2
  exit 1
fi

echo "Live chat completion: PASS"
echo "Live smoke test: ALL PASS"
