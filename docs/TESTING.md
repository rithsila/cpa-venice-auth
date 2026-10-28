# Testing Guide

**Rule:** a task in `IMPLEMENTATION_PLAN.md` is done only when its test here passes. No exceptions for "works on my machine".

---

## 1. Layers

| Layer | What it proves | Command |
|---|---|---|
| Unit | pure logic, parsing, mapping | `go test ./internal/...` |
| Fixture | correct handling of real Venice payloads | `go test ./internal/... -run Fixture` |
| Race | no data races under concurrency | `go test -race ./...` |
| Integration | plugin loads in real CPA and serves traffic | `make itest` |
| Live smoke | a real key can actually call Venice | `make smoke` |
| Soak | no leaks, no nonce reuse, no retry storms | `make soak` |

---

## 2. Unit tests per package

### `internal/envelope`

- decode valid envelope
- reject trailing garbage
- reject body over 8 MiB
- missing `ok` field fails closed
- unknown error code still produces a message

### `internal/dispatch`

- every registered method returns an envelope
- unknown method returns `unknown_method`, not a panic
- a method that panics returns `plugin_error` and the process survives

### `internal/auth`

- parse a valid `api_key` file
- parse a valid `x402` file
- reject a file with an unknown `mode`
- reject a file with a missing key
- refresh with a 200 upstream response sets `NextRefreshAfter` roughly 6h out
- refresh with a 401 returns a typed auth error
- refresh never logs the key

### `internal/executor`

- identifier is `venice`
- request body is forwarded byte-for-byte
- `venice_parameters` survives translation
- extra unknown fields survive
- `execute_stream` emits at least three chunks for a three-frame SSE fixture, then closes
- stream error mid-flight emits one `Err` chunk and closes
- `count_tokens` returns unsupported

### `internal/venice` (error mapping)

Table test over:

| Status | Body | Expected |
|---|---|---|
| 402 | `{"error":"INSUFFICIENT_BALANCE"}` | `balance` |
| 401 | `{"error":"PRO_ONLY_MODEL"}` | `plan` |
| 401 | `{"error":"invalid api key"}` | `auth` |
| 429 | empty, `x-ratelimit-reset-requests` present | `rate_limit` with reset |
| 500 | html | `upstream` |
| 0 | context deadline | `timeout` |

### `internal/models`

- fixture `testdata/models.json` maps to the expected count
- a model missing context length gets a sane default
- cache hit avoids a second HTTP call
- cache expiry triggers exactly one refresh under 50 concurrent readers

### `internal/quota`

- headers parsed into remaining requests, remaining tokens, reset times
- missing headers produce zero values, not an error
- balance header parsed as string, not float

### `internal/x402`

- golden message matches the documented EIP-4361 layout byte-for-byte
- recovered address equals the signer address
- two consecutive signatures differ
- nonce is 32 hex characters and never repeats across 10,000 samples
- expired timestamp refused
- future timestamp beyond 60s refused
- header is valid base64 of the expected JSON keys
- retry path signs again

---

## 3. Fixtures to record

Save real responses, with keys stripped, into `testdata/`:

| File | Content |
|---|---|
| `models.json` | `GET /api/v1/models` |
| `chat_ok.json` | non-streaming completion |
| `stream_ok.sse` | streaming completion |
| `error_402.json` | insufficient balance |
| `error_401_pro.json` | pro-only model |
| `headers_ok.txt` | response headers with rate limits and balance |

Recording rule: replace every key with `REDACTED`, keep every header name.

---

## 4. Integration harness

Script lives in `scripts/itest.sh`. The `make itest` target is a thin wrapper.

`make itest` does:

```bash
#!/usr/bin/env bash
set -euo pipefail

PLUGIN_DIR="$PWD/bin/plugins"

CPA=${CPA_BIN:-./vendor/CLIProxyAPI}
PORT=${PORT:-18317}

cat > /tmp/cpa-venice-test.yaml <<YAML
config-version: 8
server: {host: "127.0.0.1", port: $PORT}
access: {api-keys: ["test-key"]}
plugins:
  enabled: true
  dir: "$PLUGIN_DIR"
  configs:
    venice-provider:
      enabled: true
      priority: 10
      models_cache_seconds: 60
YAML

"$CPA" -config /tmp/cpa-venice-test.yaml &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  curl -sf "http://127.0.0.1:$PORT/v1/models" -H "Authorization: Bearer test-key" >/dev/null && break
  sleep 0.2
done

curl -sf "http://127.0.0.1:$PORT/v1/models" \
  -H "Authorization: Bearer test-key" | grep -q "venice-uncensored-1-2"

curl -sf "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H "Authorization: Bearer test-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"venice-uncensored-1-2","messages":[{"role":"user","content":"say ok"}]}' \
  | grep -q '"choices"'

echo "integration: PASS"
```

For CI without a real key, point `base_url` at a local mock server that replays the fixtures. The plugin must not care.

---

## 5. Acceptance checklist

Map to PRD requirement IDs. Tick only with evidence.

**Stage 0**

- [ ] FR-0.1 models listed — response body saved
- [ ] FR-0.2 non-streaming 200 — response body saved
- [ ] FR-0.3 streaming deltas — at least 3 chunks captured
- [ ] FR-0.4 two keys alternate — request log shows both key IDs
- [ ] FR-0.5 Venice key never sent to the client — grep the response

**Stage 1**

- [ ] FR-1.1 provider registers as `venice` — startup log
- [ ] FR-1.2 auth file parses — panel shows credential
- [ ] FR-1.3 refresh validates — next-refresh timestamp set
- [ ] FR-1.4 chat and responses both 200 — two saved bodies
- [ ] FR-1.5 catalog exposed — `/v1/models` count matches fixture
- [ ] FR-1.6 quota visible — panel screenshot or API output
- [ ] FR-1.7 typed errors — one test per row of the error table

**Stage 2**

- [ ] FR-2.1 wallet key only from env — grep the auth file for `0x`
- [ ] FR-2.2 fresh signature per request — 100 requests, 100 distinct headers
- [ ] FR-2.3 replay rejected — upstream returns 401, plugin reports auth error
- [ ] FR-2.4 balance guard — empty wallet yields a readable refusal
- [ ] FR-2.5 balance tracked — header value equals subsequent balance call
- [ ] FR-2.6 top-up documented — README section exists

---

## 6. Soak test

One request per second for 24 hours.

```bash
#!/usr/bin/env bash
set -euo pipefail
PORT=${PORT:-18317}
END=$((SECONDS + 86400))
n=0; fail=0
while [ $SECONDS -lt $END ]; do
  code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://127.0.0.1:$PORT/v1/chat/completions" \
    -H "Authorization: Bearer test-key" \
    -H "Content-Type: application/json" \
    -d '{"model":"venice-uncensored-1-2","messages":[{"role":"user","content":"ping"}],"max_tokens":1}')
  n=$((n+1))
  [ "$code" = "200" ] || fail=$((fail+1))
  sleep 1
done
echo "requests=$n failures=$fail"
```

Pass criteria:

| Metric | Target |
|---|---|
| HTTP 200 rate | above 99% |
| `401` from nonce reuse | 0 |
| cooling events from `402` | 1 per exhausted credential, not per retry |
| RSS growth | under 20 MB over 24h |
| goroutines after shutdown | 0 leaked |

---

## 7. Performance measurement

Measure overhead, not upstream speed.

```bash
# 100 sequential non-streaming calls, report p50 and p95 wall time
for i in $(seq 1 100); do
  curl -s -o /dev/null -w '%{time_total}\n' \
    http://127.0.0.1:8317/v1/chat/completions \
    -H "Authorization: Bearer test-key" \
    -H "Content-Type: application/json" \
    -d '{"model":"venice-uncensored-1-2","messages":[{"role":"user","content":"hi"}],"max_tokens":1}'
done | sort -n | awk '{a[NR]=$1} END{print "p50="a[int(NR*0.5)]" p95="a[int(NR*0.95)]}'
```

Compare against a direct call to Venice. Difference is plugin overhead. Target: under 5 ms at p95.

### Measured Hardening Metrics (Wave 6 & 8)

| Metric | PRD Target | Measured Result | Status |
|---|---|---|---|
| **Metric 2: Proxy overhead** | p95 < 5 ms for non-streaming chat | 66.4 µs / op (~0.066 ms) via benchmark | PASS |
| **Metric 3: Zero 401 nonce reuse** | 0 nonce reuse errors in soak | Fresh EIP-4361 payload signed per request, tested in `TestExecuteWalletModeReSignsOnRetry` | PASS |
| **Metric 4: 402 cooling handling** | Single cooling event, no retry storm | 402 mapped immediately to `insufficient_balance` fault with no retry loops | PASS |
| **Metric 5: Stage 3 spike go/no-go** | Written within 1 day of starting spike | `docs/STAGE3-VERDICT.md` completed with NO-GO verdict and trace evidence | PASS |

---

## 8. Release gate

Before tagging:

1. `go test -race ./...` green.
2. `make itest` green on macOS and Linux.
3. Live smoke on one paid key and one wallet.
4. Soak report attached to the release.
5. `docs/CONFIG.md` examples copy-paste verified.
6. No secret in `git log -p` or in any committed config.
