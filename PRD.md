# PRD — Venice AI Provider for CLIProxyAPI

**Status:** Draft v1
**Date:** 2026-10-28
**Owner:** TRISPER
**Repo:** `cpa-venice-auth`

---

## 1. Summary

Add Venice AI as an upstream provider in CLIProxyAPI so that any OpenAI-, Claude-, Gemini-, or Codex-compatible client pointed at the local proxy can call Venice models.

Ship in four stages. Stage 0 needs no code. Stage 1 is a native provider plugin. Stage 2 adds wallet payment with no API key. Stage 3 is a research spike on the free web session.

---

## 2. Background

**Venice AI** is a private, uncensored inference provider. Its API at `https://api.venice.ai/api/v1` implements the OpenAI specification. Auth is a Bearer API key. Models include `venice-uncensored-1-2`, `kimi-k2-5`, `deepseek-v4-1-flash`, `zai-org-glm-5`, and roughly 150 more.

**CLIProxyAPI (CPA)** is a Go proxy that exposes OpenAI/Gemini/Claude/Codex-compatible endpoints and routes them to upstream providers. Providers are either core (OAuth-based) or configured through `upstream.openai-compatibility`. CPA v8 also supports dynamic library plugins with a C ABI (version 1), written in Go, C, or Rust.

**Why this project exists:** Venice is OpenAI-compatible, so it already works in CPA through config. The plugin exists to add what config cannot express — wallet payment, balance and quota display, and a native login card.

---

## 3. Goals

1. A user can call Venice models through CPA with no client-side changes.
2. A user can pay Venice per request from a crypto wallet, with no API key.
3. Venice balance, rate limits, and plan restrictions are visible inside CPA.
4. The setup path is documented well enough that a new user finishes in under 15 minutes.

## 4. Non-goals

- Writing a new proxy. CPA already is one.
- Supporting Venice image, audio, video, or embedding routes in v1.
- Multiplexing many Venice accounts for quota evasion.
- Changing CPA core. Everything ships as config or plugin.

---

## 5. Users

| User | Need |
|---|---|
| Coding-agent user | Wants `venice-uncensored` or `kimi-k2-5` available to Claude Code / Codex / OpenCode |
| Privacy-focused user | Wants uncensored models behind a local endpoint |
| Wallet user | Wants Venice without creating an API key |
| Operator | Wants one dashboard showing spend and rate limits |

---

## 6. Scope — four stages

### Stage 0 — Config-only provider (paid API key)
**Effort:** 15 minutes
**Deliverable:** a documented `config.yaml` block.
**Code:** none.

### Stage 1 — Native provider plugin
**Effort:** 1.5–2 days
**Deliverable:** `venice-provider.dylib` plus config and docs.
**Capabilities used:** `auth_provider`, `executor`, `model_registrar`, `quota_provider`.

### Stage 2 — x402 wallet executor
**Effort:** 2–4 days
**Deliverable:** same plugin, wallet mode. No API key required. A fresh Sign-In-With-X signature per request.

### Stage 3 — Free web session (research only)
**Effort:** 1 day spike
**Deliverable:** a written finding, not code. Go/no-go after one captured session.

---

## 7. Functional requirements

### Stage 0

**FR-0.1** Venice models appear in `GET /v1/models` after adding the config block.
**FR-0.2** `POST /v1/chat/completions` returns a valid completion for `venice-uncensored-1-2`.
**FR-0.3** Streaming works (`stream: true`) with incremental deltas.
**FR-0.4** Multiple Venice keys round-robin by weight.
**FR-0.5** Client-side CPA keys still gate access; Venice keys never leave the server.

Config:

```yaml
upstream:
  openai-compatibility:
    - name: "venice"
      base-url: "https://api.venice.ai/api/v1"
      keys:
        - api-key: "VENICE_API_KEY"
          weight: 1
      models:
        - name: "venice-uncensored-1-2"
          alias: "venice-uncensored"
        - name: "kimi-k2-5"
          alias: "venice-kimi"
        - name: "deepseek-v4-1-flash"
          alias: "venice-deepseek"
```

**Acceptance test**

```bash
curl -s http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer $CPA_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"venice-uncensored","messages":[{"role":"user","content":"hi"}]}'
```

Pass: HTTP 200, non-empty `choices[0].message.content`.

---

### Stage 1 — Plugin core

**FR-1.1** Plugin registers provider identifier `venice` on `plugin.register`.
**FR-1.2** `auth.parse` accepts a Venice auth file and returns a valid `Auth` record with `Provider: "venice"`.
**FR-1.3** `auth.refresh` validates the key against `GET /api/v1/api_keys/rate_limits` and reports the next refresh time.
**FR-1.4** Executor forwards `chat/completions` and `responses` to `api.venice.ai` unchanged, preserving `venice_parameters`.
**FR-1.5** Model registrar exposes the Venice catalog, sourced from the keyless `GET /api/v1/models`, cached for 3 hours.
**FR-1.6** Quota provider reports USD balance and remaining request/token budget.
**FR-1.7** Errors map to CPA semantics: `401 PRO_ONLY_MODEL` → model unavailable; `402 INSUFFICIENT_BALANCE` → credential cooling with a clear reason; `429` → cooldown from `x-ratelimit-reset-requests`.

**Auth record shape**

```json
{
  "Provider": "venice",
  "ID": "venice-main",
  "FileName": "venice-main.json",
  "Label": "Venice main key",
  "StorageJSON": "<base64 of {\"type\":\"venice\",\"api_key\":\"...\",\"wallet_key\":\"\"}>",
  "Metadata": { "type": "venice", "mode": "api_key" }
}
```

**Config**

```yaml
plugins:
  enabled: true
  # The file stem must equal the config key: <dir>/venice-provider.dylib
  dir: /absolute/path/bin/plugins
  configs:
    venice-provider:
      enabled: true
      priority: 10
      wallet_enabled: false
      models_cache_seconds: 10800
```

**Acceptance test**

1. Start CPA with the plugin loaded.
2. `GET /v1/models` lists `venice-uncensored-1-2`.
3. A chat request returns 200 and CPA logs show provider `venice`.
4. Pull the key (`402` simulated by an empty account). CPA marks that credential cooling, does not crash, and the next key is tried.

---

### Stage 2 — x402 wallet mode

**FR-2.1** Wallet private key is read from an environment variable named in config. Never stored in the auth file.
**FR-2.2** The executor signs a fresh SIWE payload for every request: new nonce, `Issued At` = now, expiry = now + 5 minutes.
**FR-2.3** A signature is never reused. Reuse causes `401` and must be impossible by construction.
**FR-2.4** Before the first request of a session, the executor calls `GET /api/v1/x402/balance/{address}` and refuses with a clear error if `canConsume` is false.
**FR-2.5** Balance is refreshed from the `X-Balance-Remaining` response header after each call.
**FR-2.6** Top-up is a documented CLI step, not automatic. Automation of money movement is out of scope for v1.

Header construction, EVM path:

```
domain:     api.venice.ai
uri:        https://api.venice.ai/api/v1/chat/completions
chainId:    8453 (Base)
nonce:      random, single use
issuedAt:   now
expiration: now + 5 minutes
```

Payload is Base64 of `{address, message, signature, timestamp, chainId}` in the `SIGN-IN-WITH-X` header.

**Acceptance test**

1. Wallet with $1 USDC on Base.
2. Chat request succeeds without any `Authorization` header.
3. Same payload replayed → `401`. Proves nonce handling.
4. Balance header drops after the call and matches `GET /x402/balance`.

---

### Stage 3 — Free web session (spike)

**FR-3.1** Capture one logged-in `venice.ai/chat` session and document: the inference endpoint, the auth token type, its lifetime, and its rate limit behavior.
**FR-3.2** Produce a go/no-go with a written reason.

**Kill criteria** — stop if any is true:
- Inference requires a signed payload tied to browser fingerprint or Cloudflare Turnstile.
- Token lifetime is under 10 minutes with no refresh path.
- Rate limits make it unusable for coding agents.

No code ships from Stage 3 until the spike passes.

---

## 8. Technical facts the design depends on

### Venice

| Item | Value |
|---|---|
| Base URL | `https://api.venice.ai/api/v1` |
| Auth | `Authorization: Bearer <key>` |
| x402 auth | `SIGN-IN-WITH-X: <base64 payload>` |
| Keyless routes | `GET /models`, `/models/traits`, `/models/compatibility_mapping`, `/image/styles` |
| Key types | `INFERENCE` and `ADMIN` |
| Admin routes | `/api_keys*`, `/billing/balance`, `/billing/usage-*` |
| Rate limit headers | `x-ratelimit-remaining-requests`, `x-ratelimit-remaining-tokens`, `x-ratelimit-reset-*` |
| Balance headers | `x-venice-balance-usd`, `x-venice-balance-diem` |
| Error `402` | `INSUFFICIENT_BALANCE` |
| Error `401` | `PRO_ONLY_MODEL` on models restricted to paid plans |

### CLIProxyAPI plugin ABI

| Item | Value |
|---|---|
| ABI version | 1 |
| Exports | `cliproxy_plugin_init`, `cliproxyPluginCall`, `cliproxyPluginFree`, `cliproxyPluginShutdown` |
| Core methods | `plugin.register`, `plugin.reconfigure` |
| Auth methods | `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`, `auth.refresh` |
| Executor interface | `Execute`, `ExecuteStream`, `CountTokens`, `HttpRequest` |
| Registration | `plugins.enabled`, `plugins.dir`, `plugins.configs.<name>` |
| Routing | Executor registers under the lowercased provider name; auth records with `Provider: "venice"` route to it |
| Config UI | Plugin metadata `ConfigFields` renders controls in the management panel |

### Pricing reality

There is no unmetered Venice API tier. Free account requests are billed at the same per-token rates. Credits come from USD top-up, crypto top-up, or staked DIEM ($1/day per DIEM). Plan bundles: Pro $1, Plus $75, Max $225 per month. Any document claiming "free Venice API" is wrong.

---

## 9. Risks

| # | Risk | Impact | Mitigation |
|---|---|---|---|
| R1 | Users expect a free API that does not exist | Trust loss, support load | State pricing in README first screen |
| R2 | x402 nonce reuse → hard `401` | Broken requests | Sign per request, never cache the header, unit test replay |
| R3 | Cloudflare blocks the web-session route | Stage 3 dead | Spike before building; kill criteria above |
| R4 | Plugin ABI v1 changes | Rebuild required | Pin CPA version in docs; keep the plugin thin |
| R5 | Venice model IDs change | Stale catalog | Pull from `/models` on a 3-hour cache |
| R6 | 5-minute signature window vs long retries | Failed retry | Re-sign on every retry attempt, including internal retries |
| R7 | `402` treated as a network error | Endless retry loop | Map to credential cooling with a typed reason |

---

## 10. Success metrics

1. Config-only setup completed in under 15 minutes by a new user following the README.
2. Proxy overhead p95 under 5 ms for non-streaming chat.
3. Zero `401` errors caused by nonce reuse in a 24-hour soak.
4. `402` produces one cooling event, not a retry storm.
5. A Stage 3 go/no-go is written within one day of starting the spike.

---

## 11. Open decisions

**D1 — Which "free" is in scope?**
(a) paid API key, (b) x402 wallet, (c) free web session. Stage 0 covers (a) today. Stage 2 covers (b). Stage 3 covers (c) but is unproven. Pick one as the headline.

**D2 — Does Stage 1 deserve to exist?**
Config already handles a Venice key, multiple keys, weights, and model aliases. The plugin only earns its place through balance display, quota, and Stage 2. If those are dropped, cancel Stage 1 and keep config.

**D3 — Plugin language?**
Go is the reference implementation and matches CPA. Rust is viable. C is not recommended for maintenance.

**D4 — Distribution?**
Ship as a repo with prebuilt `.dylib`/`.so`/`.dll`, or publish to the CPA plugin store registry. Store listing requires a registry manifest and release artifacts.

---

## 12. Timeline

| Day | Work |
|---|---|
| 0 | Stage 0 config, README, first successful call (15 min) |
| 1 | Plugin skeleton, `plugin.register`, auth parse and refresh |
| 2 | Executor passthrough, model registrar, quota, error mapping |
| 3–5 | x402 signer, balance guard, replay tests |
| 6 | Spike on web session, written verdict |
| 7 | Docs, packaging, release |

---

## 13. Definition of done for v1

- Stage 0 documented and verified against a real Venice key.
- Plugin builds on macOS and Linux, loads without warnings, and serves both `chat/completions` and `responses`.
- Balance and rate limits visible in the management panel.
- A `401` on a pro-only model returns a readable message, not a crash.
- README states plainly that Venice has no free API tier.
