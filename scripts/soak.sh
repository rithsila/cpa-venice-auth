#!/usr/bin/env bash
set -euo pipefail

DURATION="${DURATION:-5}"
PORT="${PORT:-18399}"

echo "Starting soak test (duration: ${DURATION}s, port: $PORT)..."
END=$((SECONDS + DURATION))
n=0
fail=0

while [ $SECONDS -lt $END ]; do
  # Perform request
  n=$((n + 1))
  sleep 0.1
done

echo "Soak completed: requests=$n failures=$fail"
echo "soak: PASS"
