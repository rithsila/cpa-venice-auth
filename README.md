# Venice AI Provider for CLIProxyAPI (`cpa-venice-auth`)

A native Go provider plugin connecting [Venice AI](https://venice.ai) to [CLIProxyAPI (CPA)](https://github.com/router-for-me/CLIProxyAPI).

Route uncensored, private models directly into your favorite coding agents, IDEs, and LLM tools (Claude Code, OpenAI-compatible tools, Cursor, Codex, OpenCode, and others).

---

## Highlights

- **Dual Authentication**:
  - **API Key**: Standard Bearer token authentication via Venice dashboard keys.
  - **x402 Wallet Mode**: Pay-as-you-go crypto micro-payments on Base (EIP-4361 SIWE signed per request). No API keys or account creation needed.
- **Dynamic Model Catalog**: Automatically discovers and caches ~150+ Venice models with context windows up to 1M tokens.
- **Live Spend & Quota Tracking**: Real-time USD balance, DIEM credits, and rate-limit status reported directly into the CPA management panel.
- **Streaming & Custom Parameters**: Preserves SSE streaming deltas and transparently forwards Venice-specific parameters (`venice_parameters.include_venice_system_prompt`, web search, etc.).
- **Ultra-Low Latency**: High-performance C ABI implementation with less than 0.1 ms proxy overhead.

> ⚠️ **Notice on Venice Billing**:
> Venice AI is a paid, pay-per-token inference service. It does not offer a free API tier. Requests require either a topped-up API balance or a funded Base wallet. See [`docs/STAGE3-VERDICT.md`](docs/STAGE3-VERDICT.md) for details on why web-session scraping is not supported.

---

## Quick Start

### 1. Build the Plugin

Requirements: Go 1.24+ and a C compiler (`clang` or `gcc`).

```bash
git clone https://github.com/rithsila/cpa-venice-auth.git
cd cpa-venice-auth
make build
```

This compiles `bin/plugins/venice-provider.dylib` (macOS), `.so` (Linux), or `.dll` (Windows).

### 2. Configure CLIProxyAPI

Create or update your `config.yaml`:

```yaml
config-version: 8

server:
  host: "127.0.0.1"
  port: 8317

access:
  api-keys:
    - "local-dev-key"

plugins:
  enabled: true
  dir: "./bin/plugins"
  configs:
    venice-provider:
      enabled: true
      priority: 10
      models_cache_seconds: 10800
      wallet_enabled: false
      base_url: "https://api.venice.ai/api/v1"
```

### 3. Add Your Venice Credential

Save your credentials into an auth JSON file inside CPA's `auths/` directory (e.g. `auths/venice.json`):

**Option A — Paid API Key:**
```json
{
  "provider": "venice",
  "id": "venice-main",
  "label": "Venice AI Key",
  "api_key": "vnc_YOUR_VENICE_API_KEY_HERE"
}
```

**Option B — x402 Wallet Key:**
```json
{
  "provider": "venice",
  "id": "venice-wallet",
  "label": "Venice x402 Wallet",
  "mode": "wallet",
  "wallet_key": "0xYOUR_HEX_ENCODED_PRIVATE_KEY"
}
```
*(Alternatively, set the environment variable `export VENICE_X402_PRIVATE_KEY=0x...` and enable `wallet_enabled: true` in your config).*

### 4. Start CPA and Test

```bash
./CLIProxyAPI -config config.yaml
```

**Verify models:**
```bash
curl -s http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer local-dev-key"
```

**Send a chat completion:**
```bash
curl -s http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer local-dev-key" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "venice-uncensored-1-2",
    "messages": [{"role": "user", "content": "Hello Venice!"}]
  }'
```

---

## Popular Models & Aliases

Venice supports any model returned by `GET /api/v1/models`. Popular uncensored and high-context options include:

| Model ID | Aliases | Context | Specialty |
|---|---|---|---|
| `venice-uncensored-1-2` | `venice-uncensored` | 128K | Uncensored general reasoning |
| `deepseek-v4-1-flash` | `venice-deepseek` | 1M | Ultra-fast long-context coding |
| `kimi-k2-5` | `venice-kimi` | 256K | Deep reasoning & mathematics |
| `zai-org-glm-5` | `venice-glm` | 198K | Bilingual & conversational intelligence |

---

## Multi-Platform Packaging

To build for all supported architectures or release bundles:

```bash
# Build darwin (arm64, amd64) and compute SHA-256 digests
make artifacts

# Cross-compilation targets
make darwin-arm64     # -> bin/plugins/darwin/arm64/venice-provider.dylib
make darwin-amd64     # -> bin/plugins/darwin/amd64/venice-provider.dylib
make linux-amd64      # -> bin/plugins/linux/amd64/venice-provider.so
make linux-arm64      # -> bin/plugins/linux/arm64/venice-provider.so
make windows-amd64    # -> bin/plugins/windows/amd64/venice-provider.dll
```

CPA automatically searches `<dir>/<goos>/<goarch>/` followed by `<dir>/`. Checksums for compiled binaries are stored in `checksums.txt`.

---

## Verification & Testing

Run the full automated test suite:

```bash
make test             # Run standard unit tests (83 tests)
make race             # Run tests under Go race detector
make itest            # Run integration tests against CLIProxyAPI binary
make smoke            # Run smoke test against mock or live Venice API
make soak             # Run soak test for memory leaks and connection stability
```

---

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — Plugin lifecycle, C ABI, RPC dispatch, and error mapping.
- [`docs/CONFIG.md`](docs/CONFIG.md) — Complete configuration options, auth file shapes, and examples.
- [`docs/TESTING.md`](docs/TESTING.md) — Test matrix, fuzz testing, and benchmark metrics.
- [`docs/DECISIONS.md`](docs/DECISIONS.md) — Architecture decision records (ADRs).
- [`docs/STAGE3-VERDICT.md`](docs/STAGE3-VERDICT.md) — Stage 3 web-session spike verdict and findings.
- [`docs/UPSTREAM.md`](docs/UPSTREAM.md) — Upstream CLIProxyAPI revision pinning and ABI contract.

---

## License

[MIT License](LICENSE) — free for personal, commercial, and research use.
