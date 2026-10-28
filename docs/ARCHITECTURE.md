# Architecture — Venice Provider Plugin

**Target:** CLIProxyAPI v8.0.45, plugin ABI 1
**Status:** Design of record for v1

---

## 1. What the plugin is

A shared library (`.dylib`, `.so`, `.dll`) that CPA loads at startup. CPA calls JSON-RPC-shaped methods on it over a C ABI. The plugin advertises four capabilities:

| Capability | Purpose |
|---|---|
| `auth_provider` | Parse, refresh, and validate Venice credentials |
| `executor` | Send inference requests to `api.venice.ai` and return the bytes |
| `model_registrar` | Publish the Venice model catalog to CPA |
| `quota_provider` | Report balance and rate-limit budget |

```
              ┌──────────────────────────────┐
Client ──────►│  CLIProxyAPI core            │
(OpenAI /     │  router, translator, logs    │
 Claude /     │                              │
 Gemini)      │        ┌──────────────────┐  │
              │        │ venice-provider  │  │
              │        │ plugin (.dylib)  │  │
              │        └────────┬─────────┘  │
              └─────────────────┼────────────┘
                                │ HTTPS via host transport
                                ▼
                     https://api.venice.ai/api/v1
```

---

## 2. Lifecycle

1. CPA dlopen's the library and calls `cliproxy_plugin_init(host_api, plugin_api)`.
2. The plugin stores the host function pointers and fills in its own call/free/shutdown pointers. It returns `0`.
3. CPA calls `plugin.register`. The plugin returns metadata plus the capability flags it supports.
4. On first request, CPA calls `executor.identifier`, then `model.register`, then optionally `quota.identifier`.
5. Configuration changes trigger `plugin.reconfigure` with the same registration payload shape.
6. On shutdown, CPA calls `cliproxy_plugin_shutdown`. The plugin cancels in-flight work and releases caches.

A plugin that panics inside a call must be isolated by the plugin's own `recover`. Returning an error envelope is correct; crashing the host is not.

---

## 3. RPC method table

Verified against CPA v8.0.45 source. Direction is host → plugin unless marked otherwise.

### Core

| Method | Request | Response |
|---|---|---|
| `plugin.register` | empty or config | metadata + capabilities |
| `plugin.reconfigure` | config | metadata + capabilities |

### Auth

| Method | Purpose |
|---|---|
| `auth.identifier` | returns `venice` |
| `auth.parse` | turns a stored auth file into an Auth record |
| `auth.login.start` | begins a login flow, returns URL and state |
| `auth.login.poll` | reports pending/success/failure |
| `auth.refresh` | validates the credential, returns `NextRefreshAfter` |

### Executor

| Method | Purpose |
|---|---|
| `executor.identifier` | returns `venice` |
| `executor.execute` | non-streaming upstream call |
| `executor.execute_stream` | streaming upstream call |
| `executor.count_tokens` | token counting, unsupported in v1 |
| `executor.http_request` | generic passthrough |

### Models and quota

| Method | Purpose |
|---|---|
| `model.register` | publish catalog |
| `model.for_auth` | models visible to one credential |
| `quota.identifier` | returns `venice` |
| `quota.fetch` | balance and limits |
| `quota.remaining` | remaining budget |
| `quota.reset` | reset window |

### Management

| Method | Purpose |
|---|---|
| `management.register` | register panel routes |
| `management.handle` | serve a panel request |

---

## 4. Routing

CPA lowercases the provider name. The chain is:

```
auth file Provider: "venice"
        │
        ▼
executor.identifier() == "venice"
        │
        ▼
requests for models registered by this plugin
```

Two rules follow:

- The auth file's `Provider` must be exactly `venice`.
- The executor must return `venice` from `executor.identifier`, or the model provider override must be set. A mismatch means no route.

---

## 5. Auth storage

The host persists the plugin's bytes, so the plugin owns the format.

```json
{
  "type": "venice",
  "mode": "api_key",
  "api_key": "vn-...",
  "wallet_key_env": "",
  "created_at": "2026-10-28T10:00:00Z",
  "last_balance_usd": "12.40"
}
```

Rules:

- `StorageJSON` is base64 of the JSON above.
- `wallet_key_env` stores the **name** of an environment variable, never the private key.
- `last_balance_usd` is a cache for the panel, never used for billing decisions.
- Nothing secret is written to logs. Redaction is applied at the log call site, not after.

Mode is one of:

| Mode | Auth header | Requires |
|---|---|---|
| `api_key` | `Authorization: Bearer <key>` | Inference key |
| `x402` | `SIGN-IN-WITH-X: <payload>` | Wallet key in env, USDC balance |

---

## 6. Request flow

### Non-streaming chat

```
client → CPA router → translator → executor.execute
                                   │
                                   ├─ pick auth (provider venice)
                                   ├─ load StorageJSON
                                   ├─ build upstream headers
                                   ├─ host HTTPClient.Do
                                   ├─ read status + body + x-venice-* headers
                                   └─ map errors, update balance cache
                       → translator → client
```

### Streaming

`executor.execute_stream` returns a channel. The plugin writes each SSE frame as it arrives from upstream, then closes the channel. It must not buffer the whole response. On upstream error mid-stream, it sends one chunk with `Err` set and closes.

### x402 signing

Signing happens per attempt, inside the request path:

```
nonce = 16 random bytes, hex
message = EIP-4361 with domain api.venice.ai, chainId 8453, issuedAt now,
          expiration now+5m
signature = secp256k1.sign(message, wallet_key)
header = base64({address, message, signature, timestamp, chainId})
```

The header value is a local variable. It is never stored on the credential, never cached, and never reused across retries.

---

## 7. Error mapping

| Upstream | Detected by | Plugin returns | CPA behavior |
|---|---|---|---|
| Insufficient balance | `402` + `INSUFFICIENT_BALANCE` | credential error, reason `balance` | cooling, no retry storm |
| Pro-only model | `401` + `PRO_ONLY_MODEL` | model error, reason `plan` | clear message to client |
| Invalid key | `401` generic | auth error | credential disabled |
| Rate limited | `429` | rate error with reset time | cooldown until reset |
| Upstream fault | `5xx` | transient error | retry policy |
| Timeout | context deadline | transient error | one retry, then cooling |

The plugin never converts a `402` into a generic `500`. That is the difference between one cooling event and an infinite loop.

---

## 8. Venice API surface used in v1

| Route | Auth | Used for |
|---|---|---|
| `POST /chat/completions` | Bearer or x402 | inference |
| `POST /responses` | Bearer or x402 | inference |
| `GET /models` | none | catalog |
| `GET /api_keys/rate_limits` | either key type | refresh + quota |
| `GET /billing/balance` | ADMIN | exact balance |
| `GET /x402/balance/{address}` | x402 | pre-flight balance |
| `POST /x402/top-up` | payment header | documented manual step, not automated |

Headers read: `x-ratelimit-remaining-requests`, `x-ratelimit-remaining-tokens`, `x-ratelimit-reset-requests`, `x-ratelimit-reset-tokens`, `x-venice-balance-usd`, `x-venice-balance-diem`, `X-Balance-Remaining`, `CF-RAY`.

---

## 9. Concurrency and memory

- Every RPC call may arrive on its own goroutine. All shared state sits behind one `sync.RWMutex`.
- No lock is held across an HTTP call. Snapshot the config, release, then call.
- Streaming channels are buffered to 16 chunks and closed by the producer.
- `C.CBytes` allocations are freed exactly once, on the error path and the success path.
- Shutdown cancels the root context and waits up to 5 seconds for producers to close.

---

## 10. Security rules

1. Keys are read from the auth store or an environment variable. Never from a request body.
2. Logs redact `Authorization`, `SIGN-IN-WITH-X`, `PAYMENT-SIGNATURE`, and anything matching `sk-` or a 64-hex private key.
3. The plugin never sends credentials to any host other than `api.venice.ai`.
4. Wallet private keys are never written to disk by this plugin.
5. The management panel shows a masked key, last four characters only.
