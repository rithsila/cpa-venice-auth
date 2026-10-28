# cpa-venice-auth

Venice AI as a provider for [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI): paid API keys, x402 wallet payments, and a live model catalog behind one local endpoint.

> **Venice has no free API tier.** Their docs say it plainly. Every request is billed per token. Credits come from USD or crypto top-up, staked DIEM, or a plan bundle. A free account can call the API, but it is charged the same rates and returns `402 INSUFFICIENT_BALANCE` at zero balance. Any guide promising a free Venice API is wrong.

---

## Status

| Stage | What it is | State |
|---|---|---|
| 0 | Config-only provider, paid API key | Ready now — no code |
| 1 | Native plugin: auth, executor, catalog, quota | Planned |
| 2 | x402 wallet, no API key | Planned |
| 3 | Free web session | Research, unproven |

Verified against **CLIProxyAPI v8.0.45**, plugin ABI 1.

---

## Quick start — Stage 0, no plugin

**You need:** a Venice API key from <https://venice.ai/settings/api> and a CPA binary.

**1. Create `config.yaml`:**

```yaml
config-version: 8

server:
  host: "127.0.0.1"
  port: 8317

access:
  api-keys:
    - "local-dev-key"

upstream:
  openai-compatibility:
    - name: "venice"
      base-url: "https://api.venice.ai/api/v1"
      keys:
        - api-key: "YOUR_VENICE_KEY"
      models:
        - name: "venice-uncensored-1-2"
          alias: "venice-uncensored"
        - name: "kimi-k2-5"
          alias: "venice-kimi"
        - name: "deepseek-v4-1-flash"
          alias: "venice-deepseek"
```

**2. Start CPA:**

```bash
./CLIProxyAPI -config config.yaml
```

**3. Check the catalog:**

```bash
curl -s http://127.0.0.1:8317/v1/models \
  -H "Authorization: Bearer local-dev-key"
```

**4. Send a message:**

```bash
curl -s http://127.0.0.1:8317/v1/chat/completions \
  -H "Authorization: Bearer local-dev-key" \
  -H "Content-Type: application/json" \
  -d '{"model":"venice-uncensored","messages":[{"role":"user","content":"hello"}]}'
```

**5. Point a client at it.** Any OpenAI-compatible tool works:

```bash
export OPENAI_BASE_URL=http://127.0.0.1:8317/v1
export OPENAI_API_KEY=local-dev-key
```

That is the whole Stage 0 setup. Under 15 minutes.

---

## Plugin — Stage 1 and 2

The plugin adds what config cannot express:

- x402 wallet auth, with a fresh signature per request
- balance and rate limits in the management panel
- live catalog from `GET /api/v1/models`
- typed error handling for `402`, `401 PRO_ONLY_MODEL`, and `429`

Build:

```bash
make build          # -> bin/plugins/venice-provider.dylib
make artifacts      # -> builds darwin-arm64, darwin-amd64 and generates checksums.txt
make itest          # -> run integration tests against pinned CPA binary
make smoke          # -> run smoke tests (mocked or live if VENICE_API_KEY is set)
make soak           # -> run soak testing
```

Cross-compilation targets:
- `make darwin-arm64` -> `bin/plugins/darwin/arm64/venice-provider.dylib`
- `make darwin-amd64` -> `bin/plugins/darwin/amd64/venice-provider.dylib`
- `make linux-amd64` -> `bin/plugins/linux/amd64/venice-provider.so` (requires `x86_64-linux-gnu-gcc` or zig)
- `make linux-arm64` -> `bin/plugins/linux/arm64/venice-provider.so` (requires `aarch64-linux-gnu-gcc` or zig)
- `make windows-amd64` -> `bin/plugins/windows/amd64/venice-provider.dll` (requires `x86_64-w64-mingw32-gcc` or zig)

Or by hand on macOS:

```bash
mkdir -p bin/plugins
CGO_ENABLED=1 go build -buildmode=c-shared -trimpath \
  -o bin/plugins/venice-provider.dylib ./cmd/plugin
```

On Linux write `.so`, on Windows `.dll`. The file stem **is** the plugin ID, so it must be `venice-provider`.

Load:

```yaml
plugins:
  enabled: true
  dir: "/absolute/path/bin/plugins"
  configs:
    venice-provider:
      enabled: true
      wallet_enabled: false
```

Full key reference: [`docs/CONFIG.md`](docs/CONFIG.md).

---

## Models

Any model in the Venice catalog works through the raw ID. These aliases exist for shorter names:

| Alias | Venice model | Context |
|---|---|---|
| `venice-uncensored` | `venice-uncensored-1-2` | 128K |
| `venice-kimi` | `kimi-k2-5` | 256K |
| `venice-deepseek` | `deepseek-v4-1-flash` | 1M |
| `venice-glm` | `zai-org-glm-5` | 198K |

Edit the `models` list to add your own. Context lengths come from the [Venice model catalog](https://docs.venice.ai/overview/models).

---

## What it does not do

- Image, audio, video, and embedding routes. Chat and responses only in v1.
- Automatic money movement. Top-ups are manual.
- Cloudflare bypass of any kind.
- Free Venice usage. There is no free tier; see [`docs/STAGE3-VERDICT.md`](docs/STAGE3-VERDICT.md) for the spike verdict (NO-GO).

---

## Docs

| File | Contents |
|---|---|
| [`PRD.md`](PRD.md) | Product scope, requirements, success metrics |
| [`IMPLEMENTATION_PLAN.md`](IMPLEMENTATION_PLAN.md) | Waves, tasks, done criteria, timeline |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | Plugin lifecycle, RPC methods, routing, error map |
| [`docs/CONFIG.md`](docs/CONFIG.md) | Every setting, auth files, examples, troubleshooting |
| [`docs/TESTING.md`](docs/TESTING.md) | Test layers, fixtures, acceptance checklist, soak |
| [`docs/UPSTREAM.md`](docs/UPSTREAM.md) | Pinned CLIProxyAPI revision, verified host behaviour, refresh procedure |
| [`docs/STAGE3-SPIKE.md`](docs/STAGE3-SPIKE.md) | Web-session capture protocol and kill criteria |
| [`docs/STAGE3-VERDICT.md`](docs/STAGE3-VERDICT.md) | Stage 3 web session spike verdict (NO-GO) |
| [`docs/DECISIONS.md`](docs/DECISIONS.md) | Why Go, why a plugin, why x402 leads |

---

## Compatibility

| Platform | Artifact |
|---|---|
| macOS arm64 / amd64 | `bin/plugins/darwin/<arch>/venice-provider.dylib` |
| Linux amd64 / arm64 | `bin/plugins/linux/<arch>/venice-provider.so` |
| Windows amd64 | `bin/plugins/windows/amd64/venice-provider.dll` |

Cross-built files go under `<dir>/<goos>/<goarch>/`, which is the first path CPA scans. A single-platform local build can sit flat in `<dir>/`. Either way the stem is `venice-provider`.

CPA version is pinned in `IMPLEMENTATION_PLAN.md`. A different major ABI means rebuild and re-verify.

---

## License

MIT License. See [`LICENSE`](LICENSE) for terms.
