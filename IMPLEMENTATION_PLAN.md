# Implementation Plan — Venice Provider for CLIProxyAPI

**Pinned target:** CLIProxyAPI **v8.0.45** (released 2026-10-27), plugin ABI **1**
**Language:** Go (cgo, `-buildmode=c-shared`)
**Plan version:** 1.0 — 2026-10-28
**Companion docs:** `PRD.md`, `docs/ARCHITECTURE.md`, `docs/CONFIG.md`, `docs/TESTING.md`, `docs/STAGE3-SPIKE.md`, `docs/DECISIONS.md`

---

## 0. How to read this plan

Work runs in **waves**. A wave starts only when the previous wave's Definition of Done is verified. Every task has an ID, a concrete output, and a Done-when line that can be checked by a command or an observation.

Total effort: **8 working days**. Stage 0 ships on day 0 and never blocks the rest.

---

## 1. Target repository layout

```
cpa-venice-auth/
├── README.md
├── PRD.md
├── IMPLEMENTATION_PLAN.md
├── Makefile
├── go.mod
├── go.sum
├── cmd/
│   └── plugin/
│       └── main.go              # cgo exports only, no business logic
├── internal/
│   ├── envelope/                # RPC envelope encode/decode
│   ├── host/                    # host callback client
│   ├── dispatch/                # method table
│   ├── auth/                    # auth_provider capability
│   ├── executor/                # executor capability
│   ├── models/                  # model_registrar capability
│   ├── quota/                   # quota_provider capability
│   ├── x402/                    # SIWE signer + balance guard
│   └── venice/                  # HTTP client, headers, error mapping
├── testdata/
│   ├── models.json
│   ├── chat_ok.json
│   ├── error_402.json
│   ├── error_401_pro.json
│   └── stream_ok.sse
└── scripts/
    ├── itest.sh
    ├── smoke.sh
    └── soak.sh
└── docs/
    ├── ARCHITECTURE.md
    ├── CONFIG.md
    ├── TESTING.md
    ├── STAGE3-SPIKE.md
    └── DECISIONS.md
```

`internal/` keeps every package importable only by this module. `cmd/plugin` stays thin so a Rust rewrite later only replaces that shell.

---

## 2. Build contract

Go 1.26, cgo enabled, one shared library per platform.

```make
# Makefile targets (final shape)
build-darwin-arm64:
	CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 \
	go build -buildmode=c-shared -trimpath \
	-o bin/plugins/darwin/arm64/venice-provider.dylib ./cmd/plugin

build-linux-amd64:
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
	go build -buildmode=c-shared -trimpath \
	-o bin/plugins/linux/amd64/venice-provider.so ./cmd/plugin
```

The file stem is the plugin ID; CPA takes the ID from the filename, so it must stay `venice-provider`.

Exported C symbols, exactly these four:

| Symbol | Purpose |
|---|---|
| `cliproxy_plugin_init` | receives host API, returns plugin API |
| `cliproxyPluginCall` | one JSON request in, one JSON envelope out |
| `cliproxyPluginFree` | frees a buffer given to the host |
| `cliproxyPluginShutdown` | releases goroutines and caches |

Rule: a buffer is allocated by the side that returns it, freed by the side that receives it. The plugin never calls `free` on a host buffer and never lets the host call `free` on a Go pointer.

---

## 3. Waves

### Wave 0 — Skeleton that loads (0.5 day)

| ID | Task | Output |
|---|---|---|
| W0.1 | `git init`, `go mod init cpa-venice-auth`, add `sdk` dependency pinned to CPA v8.0.45 | `go.mod`, `go.sum` |
| W0.2 | `cmd/plugin/main.go` with the four exports copied from the reference shape, plus `plugin.register` and `plugin.reconfigure` returning metadata | loads without crash |
| W0.3 | Metadata with `Name: "venice-provider"`, `Version: "0.1.0"`, `ConfigFields` for `wallet_enabled`, `models_cache_seconds`, `wallet_key_env` | panel renders controls |
| W0.4 | `dispatch` table with `unknown_method` fallback and a unit test | test green |

**Done when:** CPA starts with

```yaml
plugins:
  enabled: true
  dir: "/abs/path/bin/plugins"
  configs:
    venice-provider:
      enabled: true
```

and the log shows `plugin registered plugin_id=venice-provider` with no ABI warning.

> Rename the module to your GitHub path before publishing. `cpa-venice-auth` builds locally; a bare path will not `go get`.

---

### Wave 1 — RPC plumbing (0.5 day)

| ID | Task | Output |
|---|---|---|
| W1.1 | `envelope` package: `{ok, result, error{code,message}}`, strict decode, max body 8 MiB | unit tests |
| W1.2 | `host` package: callback client with context timeout, buffer free on both success and error paths | unit tests with a fake host |
| W1.3 | Panic recovery per call: a panic becomes `plugin_error`, never kills the host | test with a deliberately panicking method |
| W1.4 | Structured logging through `host.log`, redacting any string that looks like `Bearer` or `sk-` | test asserts redaction |

**Done when:** `go test ./internal/envelope ./internal/host -race` passes and a forced panic returns a clean error envelope.

---

### Wave 2 — Auth provider (0.5 day)

Methods: `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`, `auth.refresh`.

| ID | Task | Output |
|---|---|---|
| W2.1 | `auth.identifier` returns `venice` | test |
| W2.2 | `auth.parse` accepts `{"type":"venice","api_key":"..."}` and returns a full Auth record with base64 `StorageJSON` | test |
| W2.3 | `auth.refresh` calls `GET /api/v1/api_keys/rate_limits` with the key; 200 → `NextRefreshAfter` = now + 6h; 401 → typed error | httptest |
| W2.4 | `auth.login.start` opens `https://venice.ai/settings/api`, returns a state and an instruction message. Venice has no OAuth, so this is a paste-key flow, not a redirect flow | manual check in panel |
| W2.5 | `auth.login.poll` reports `pending` with instructions until a key arrives through the management import path | test |

**Done when:** a JSON auth file in `oauth.auth-dir` is parsed, shows in the panel as provider `venice`, and refresh returns a valid next-refresh time against a live key.

**Constraint to respect:** do not fake an OAuth redirect. Venice only issues keys in the web UI. The honest flow is: open the URL, create a key, paste it. See ADR-005.

---

### Wave 3 — Executor (1 day)

Methods: `executor.identifier`, `executor.execute`, `executor.execute_stream`, `executor.count_tokens`, `executor.http_request`.

| ID | Task | Output |
|---|---|---|
| W3.1 | `executor.identifier` returns `venice`, so auth records with `Provider: "venice"` route here | test |
| W3.2 | Build the upstream request from `ExecutorRequest`: method, path from `Format`, `Payload`, `Headers`, `Query` | unit tests |
| W3.3 | Always call through `req.HTTPClient` so CPA request-log captures the exchange | code review + log check |
| W3.4 | Preserve `venice_parameters` untouched; never strip unknown JSON fields | fixture test |
| W3.5 | `execute_stream` returns a channel, forwards chunk bytes as they arrive, closes on EOF, sends `Err` on failure | httptest SSE fixture |
| W3.6 | `count_tokens` returns unsupported in v1 so the host falls back | test |
| W3.7 | Error mapping in `venice/errors.go` per the table below | table test |

**Error map**

| Upstream | CPA behavior |
|---|---|
| `402` `INSUFFICIENT_BALANCE` | typed credential error, cooling, no retry storm |
| `401` `PRO_ONLY_MODEL` | typed model-unavailable error, message names the plan requirement |
| `401` bad key | auth invalid, credential disabled |
| `429` | cooldown from `x-ratelimit-reset-requests` |
| `5xx` | normal retry policy |
| network timeout | retry once, then cooling |

**Done when:** `POST /v1/chat/completions` and `POST /v1/responses` both return 200 through the plugin, streaming yields incremental deltas, and a simulated `402` produces exactly one cooling event.

---

### Wave 4 — Model registrar and quota (0.5 day)

Methods: `model.register`, `quota.identifier`, `quota.fetch`.

| ID | Task | Output |
|---|---|---|
| W4.1 | Fetch `GET /api/v1/models` (no key required), map to `ModelInfo`: `ID`, `DisplayName`, `Type`, `ContextLength`, `InputTokenLimit` | fixture test |
| W4.2 | Cache for `models_cache_seconds` (default 10800), refresh on `plugin.reconfigure`, never block a request on refresh | test with a slow upstream |
| W4.3 | Quota fetch: remaining requests and tokens from `x-ratelimit-*` on the last response; USD balance from `x-venice-balance-usd` | test |
| W4.4 | If the key is `ADMIN`, also read `GET /billing/balance` for an exact figure | test |

**Done when:** `GET /v1/models` lists Venice models and the panel shows balance and rate limits.

---

### Wave 5 — x402 wallet mode (3 days)

| ID | Task | Output |
|---|---|---|
| W5.1 | `x402/siwe.go`: build the EIP-4361 message exactly as Venice documents (domain `api.venice.ai`, chain 8453, fresh nonce) | golden-file test |
| W5.2 | Sign with secp256k1; recover the address in a test and assert it matches the key | test |
| W5.3 | Encode `{address,message,signature,timestamp,chainId}` as base64 into `SIGN-IN-WITH-X` | test |
| W5.4 | Signer returns a value, never a cached header. One call, one signature | test asserts two calls differ |
| W5.5 | Balance guard: `GET /api/v1/x402/balance/{address}` before first use; refuse with a readable error when `canConsume` is false | httptest |
| W5.6 | Update balance from `X-Balance-Remaining` after each response | test |
| W5.7 | Re-sign on every retry, including internal retries | test with a forced retry |
| W5.8 | Clock-skew guard: refuse to sign if local clock drifts more than 60s from the response `Date` header | test |

**Done when:** a live call with $1 USDC succeeds with no `Authorization` header, a replayed header returns `401`, and a 24-hour soak records zero nonce-reuse errors.

**Not in scope:** automatic top-up. Money movement stays manual.

---

### Wave 6 — Hardening (0.5 day)

| ID | Task | Output |
|---|---|---|
| W6.1 | `go test -race ./...` clean | CI log |
| W6.2 | Fuzz `envelope` decode and the auth JSON parser for 10 minutes each | no crashes |
| W6.3 | Concurrency audit: one mutex around config and caches, no lock held across an HTTP call | review note |
| W6.4 | Memory audit: every `C.CBytes` has exactly one matching free path | review note |
| W6.5 | `cliproxyPluginShutdown` cancels in-flight streams and waits 5s | test |

**Done when:** PRD metrics 2, 3, 4 are measured and recorded in `docs/TESTING.md`.

---

### Wave 7 — Packaging (0.5 day)

| ID | Task | Output |
|---|---|---|
| W7.1 | Makefile targets for darwin-arm64, darwin-amd64, linux-amd64, linux-arm64, windows-amd64 | artifacts |
| W7.2 | SHA-256 checksums per artifact | `checksums.txt` |
| W7.3 | README verified by a clean-room run on a fresh machine | under 15 minutes |
| W7.4 | Optional: CPA plugin store manifest | `plugin.json` |
| W7.5 | `make test`, `make itest`, `make smoke`, `make soak` targets wrap `scripts/*.sh` | one entry point per layer |

**Done when:** a new user reaches a working Venice call in under 15 minutes using only the README.

---

### Wave 8 — Stage 3 spike (1 day, independent)

Run the protocol in `docs/STAGE3-SPIKE.md`. Output is a written verdict, not code.

**Done when:** the report exists with a go/no-go and at least one reproducible trace excerpt with secrets redacted.

---

## 4. Dependency graph

```
W0 ──► W1 ──► W2 ──┐
             │      ├──► W3 ──► W4 ──► W5 ──► W6 ──► W7
             └──────┘
W0 ──► W8 (independent)
```

W2 and W3 can run in parallel by two people. W4 depends on W3 because it reuses the HTTP client and error map. W5 depends on W4 only for config plumbing.

---

## 5. Test layers

| Layer | Tool | Runs |
|---|---|---|
| Unit | `go test` with table tests | every commit |
| Fixture | `httptest` with recorded Venice payloads | every commit |
| Race | `go test -race` | every PR |
| Integration | real CPA binary + plugin + `curl` | every PR |
| Live smoke | real Venice key, one call per model | before release |
| Soak | 24h loop, 1 request per second | before release |

Full commands live in `docs/TESTING.md`.

---

## 6. Risks mapped to tasks

| Risk | Guarded by |
|---|---|
| R1 free-tier misunderstanding | README first screen |
| R2 nonce reuse | W5.4, W5.7, replay test |
| R3 web session blocked | W8 kill criteria, no code before verdict |
| R4 ABI drift | pinned v8.0.45, thin `cmd/plugin` |
| R5 model drift | W4.1 live catalog, 3h cache |
| R6 signature expiry on retry | W5.7 |
| R7 402 retry storm | W3.7 |

---

## 7. Defaults if decisions are not answered

| Decision | Default taken |
|---|---|
| D1 headline | x402 wallet |
| D2 plugin exists | yes, because of x402 |
| D3 language | Go |
| D4 distribution | GitHub releases with checksums |
| D5 license | defer; no license file until chosen |

These defaults keep work moving. Changing one later costs: D3 a rewrite of `cmd/plugin` and the signer, D4 a manifest only, D1 a README rewrite.
