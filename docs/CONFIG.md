# Configuration Reference

Two independent setups live here:

- **Stage 0** — Venice through `upstream.openai-compatibility`. No plugin, no code.
- **Stage 1+** — the Venice provider plugin, loaded from `plugins`.

You can run both at once, but you do not need to. Pick one.

---

## 1. Stage 0 — config-only Venice

Fastest path. Works with a Venice API key from `https://venice.ai/settings/api`.

```yaml
config-version: 8

server:
  host: "127.0.0.1"
  port: 8317

# Keys your local clients present to CPA. Not Venice keys.
access:
  api-keys:
    - "local-dev-key"

upstream:
  openai-compatibility:
    - name: "venice"
      base-url: "https://api.venice.ai/api/v1"
      keys:
        - api-key: "YOUR_VENICE_KEY"
          weight: 1
      models:
        - name: "venice-uncensored-1-2"
          alias: "venice-uncensored"
          display-name: "Venice Uncensored 1.2"
          max-context-length: 131072
        - name: "kimi-k2-5"
          alias: "venice-kimi"
          max-context-length: 262144
        - name: "deepseek-v4-1-flash"
          alias: "venice-deepseek"
          max-context-length: 1048576
        - name: "zai-org-glm-5"
          alias: "venice-glm"
          max-context-length: 202752
```

Run:

```bash
./CLIProxyAPI -config config.yaml
```

Verify:

```bash
curl -s http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer local-dev-key"
```

### Multiple Venice keys

Repeat entries under `keys`. CPA round-robins by weight.

```yaml
      keys:
        - api-key: "KEY_A"
          weight: 3
        - api-key: "KEY_B"
          weight: 1
```

### How a client uses it

Point any OpenAI-compatible client at `http://127.0.0.1:8317/v1` with `local-dev-key` as the API key, and select the alias (`venice-uncensored`, `venice-kimi`, …).

---

## 2. Plugin configuration

```yaml
plugins:
  enabled: true
  # CPA scans <dir>/<goos>/<goarch>/ first, then <dir>/.
  # The file stem is the plugin ID and must match the config key.
  dir: "/absolute/path/bin/plugins"
  configs:
    venice-provider:
      enabled: true
      priority: 10
      wallet_enabled: false
      models_cache_seconds: 10800
      request_timeout_seconds: 300
      base_url: "https://api.venice.ai/api/v1"
      wallet_key_env: "VENICE_X402_PRIVATE_KEY"
      chain: "base"
      model_allowlist: []
      debug: false
```

### Keys

| Key | Type | Default | Meaning |
|---|---|---|---|
| `enabled` | bool | `false` | Load and serve this plugin |
| `priority` | int | `10` | Lower wins when two plugins claim a provider |
| `wallet_enabled` | bool | `false` | Allow x402 credentials |
| `models_cache_seconds` | int | `10800` | Catalog cache lifetime |
| `request_timeout_seconds` | int | `300` | Per-attempt upstream timeout |
| `base_url` | string | `https://api.venice.ai/api/v1` | Override only for testing |
| `wallet_key_env` | string | `VENICE_X402_PRIVATE_KEY` | Env var holding the wallet private key |
| `chain` | string | `base` | `base` or `solana` |
| `model_allowlist` | list | `[]` | Empty means all models from `/models` |
| `debug` | bool | `false` | Verbose logs, still redacted |

`debug` never prints secrets. A debug build that leaks keys is a bug, not a setting.

---

## 3. Auth file format

Place in the CPA auth directory (`oauth.auth-dir`, default `~/.cli-proxy-api`).

**Filename:** `venice-main.json`

```json
{
  "type": "venice",
  "mode": "api_key",
  "api_key": "YOUR_VENICE_KEY",
  "created_at": "2026-10-28T10:00:00Z"
}
```

For wallet mode:

```json
{
  "type": "venice",
  "mode": "x402",
  "wallet_key_env": "VENICE_X402_PRIVATE_KEY",
  "created_at": "2026-10-28T10:00:00Z"
}
```

The plugin converts this into the CPA `Auth` record. `StorageJSON` is base64 of the file contents. `Provider` is always `venice`.

Rules:

1. File permissions `0600`. The plugin warns at startup if a Venice auth file is world-readable.
2. Wallet private keys never go in this file. Only the environment variable name.
3. Deleting the file removes the credential on next reload.

---

## 4. Environment variables

| Variable | Used by | Purpose |
|---|---|---|
| `VENICE_API_KEY` | Stage 0 | Convenience for shell tests, not read by CPA |
| `VENICE_X402_PRIVATE_KEY` | Plugin | Wallet key for x402 signing |
| `VENICE_X402_TOPUP_USD` | Manual CLI | Default top-up amount for the helper script |

CPA itself reads `GITHUB_TOKEN` for its own updates. Do not reuse that variable.

---

## 5. Complete x402 example

```yaml
config-version: 8

server:
  host: "127.0.0.1"
  port: 8317

access:
  api-keys:
    - "local-dev-key"

oauth:
  auth-dir: "~/.cli-proxy-api"

plugins:
  enabled: true
  dir: "/Users/you/cpa-venice-auth/bin/plugins"
  configs:
    venice-provider:
      enabled: true
      priority: 10
      wallet_enabled: true
      wallet_key_env: "VENICE_X402_PRIVATE_KEY"
      chain: "base"
      models_cache_seconds: 10800
```

Then:

```bash
export VENICE_X402_PRIVATE_KEY=0x...
mkdir -p ~/.cli-proxy-api
cat > ~/.cli-proxy-api/venice-wallet.json <<'JSON'
{"type":"venice","mode":"x402","wallet_key_env":"VENICE_X402_PRIVATE_KEY"}
JSON
chmod 600 ~/.cli-proxy-api/venice-wallet.json
```

First call performs a balance check, then signs and sends. A wallet with no USDC returns a readable `insufficient balance` message, not a crash.

---

## 6. Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `model not found` | alias not in `models` list | add the alias or call the raw model name |
| `401` on every call | wrong key, or Venice key pasted as CPA key | check `access.api-keys` vs the Venice key |
| `402 INSUFFICIENT_BALANCE` | Venice account has no credit | top up, or switch to x402 |
| `401 PRO_ONLY_MODEL` | model requires Pro/Plus/Max | pick another model or upgrade |
| Plugin not listed | `plugins.enabled: false`, or wrong absolute path | fix path, restart, check startup log |
| Plugin loads but no models | provider name mismatch | executor identifier must be `venice` |
| `401` only on x402 | reused signature or clock drift | re-sign per request; check system clock |
| Slow first request | catalog fetch | expected once per cache window |
