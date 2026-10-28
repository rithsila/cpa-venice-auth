# Decision Records

Short ADRs. Each records what was chosen, why, and what it costs. Status values: **Proposed**, **Accepted**, **Rejected**, **Superseded**.

---

## ADR-001 — Write the plugin in Go

**Status:** Accepted
**Date:** 2026-10-28

### Context
CPA is written in Go. Its plugin examples exist in Go, C, and Rust. The ABI is C, so all three are viable.

### Decision
Go, with cgo and `-buildmode=c-shared`.

### Why
- The reference examples are Go. Copying their structure removes ABI guesswork.
- The `sdk/pluginapi` types are Go structs. Using them means no hand-written JSON schema drift.
- one toolchain, one way to build for every platform.

### Consequences
- cgo must be enabled on every build machine.
- Every platform needs its own build. No cross-compiling without a C toolchain.
- Binary size is larger than C. Irrelevant here.

### Alternatives considered
- **Rust.** Stronger safety, smaller binaries, worse fit with the `pluginapi` types. Would require re-declaring every struct and keeping them in sync.
- **C.** Fastest to load, slowest to maintain. Rejected.

---

## ADR-002 — Build a plugin at all, instead of config only

**Status:** Proposed
**Date:** 2026-10-28

### Context
Venice is OpenAI-compatible. `upstream.openai-compatibility` already provides a working integration: base URL, keys, weights, model aliases. No code required. The PRD asks whether Stage 1 deserves to exist.

### Decision
Build the plugin, but only for what config cannot do:
1. x402 wallet auth — requires a fresh signature per request.
2. Balance and quota in the management panel.
3. Live model catalog from `/models` without editing YAML.
4. A login card that opens the right Venice page.

### Why
Config covers the API-key case well. A plugin that only re-implements config is dead weight. The x402 path is the one real capability gap, and it cannot be expressed as a static header.

### Consequences
- Two supported paths. The README must say which to use when.
- Plugin maintenance follows CPA's ABI.
- If x402 is dropped, this ADR should be reverted and Stage 1 deleted.

### Alternatives considered
- **Config only.** Rejected because wallet mode is impossible.
- **Plugin for everything.** Rejected because config is simpler and already tested upstream.

---

## ADR-003 — x402 wallet as the headline feature

**Status:** Proposed
**Date:** 2026-10-28

### Context
Three ways to reach Venice: paid API key, x402 wallet, free web session. Only one can lead the README and the release story.

### Decision
Lead with x402.

### Why
- It is the only capability no other CPA provider offers.
- It removes account creation entirely — a wallet and USDC are enough.
- It is fully documented by Venice, with a published header format and error table.
- The web session is unproven. The API key is already handled by config.

### Consequences
- The project inherits crypto UX: wallets, USDC, gas, top-ups.
- Support load includes "why is my balance zero".
- If Venice changes the SIWE format, the headline breaks. The signer must be isolated in `internal/x402` so a fix is one file.

### Alternatives considered
- **API key path.** Already free of code; nothing to distinguish.
- **Web session.** Unknown, possibly blocked. See `STAGE3-SPIKE.md`.

---

## ADR-004 — Distribute via GitHub releases, not the plugin store

**Status:** Proposed
**Date:** 2026-10-28

### Context
CPA supports a plugin store with registries and artifacts. It also loads local `.dylib` paths.

### Decision
Ship versioned binaries on GitHub releases with SHA-256 checksums. Add a store manifest later if the project earns users.

### Why
- Store listing adds a manifest format and a review surface for zero early users.
- Local path loading is one line of config and works today.
- Checksums give integrity without infrastructure.

### Consequences
- Users must download the right platform binary manually.
- No in-panel discovery.
- Adding the store later costs one manifest file. Cheap to defer.

---

## ADR-005 — No fake OAuth for Venice

**Status:** Accepted
**Date:** 2026-10-28

### Context
CPA's other providers use OAuth flows: Claude Code, Codex, Gemini. Venice issues API keys in a web settings page and offers no OAuth authorization server.

### Decision
The login flow is explicit: open `https://venice.ai/settings/api`, create a key, paste it. For wallet mode, set an environment variable and create an auth file.

### Why
- Inventing a redirect flow that does not exist produces a broken button.
- A paste flow is honest and takes 30 seconds.
- The panel can still show status, balance, and last refresh.

### Consequences
- The login experience is worse than the OAuth providers. That is Venice's constraint, not ours.
- Documentation must carry the weight. The README link goes straight to the key page.

### Alternatives considered
- **Browser automation to scrape a key.** Fragile, ToS-relevant, and breaks on any UI change. Rejected.
- **Admin API key generation.** Requires an ADMIN key the user must create anyway. No gain.

---

## ADR-006 — Pin CPA v8.0.45 for v1

**Status:** Accepted
**Date:** 2026-10-28

### Context
The plugin ABI is versioned but still young. Method names and struct fields can move between minor releases.

### Decision
Build and test against `v8.0.45`. Document the pin. Re-verify on each CPA minor release.

### Why
- A moving target makes "works on my machine" the default state.
- One known-good version makes bug reports meaningful.

### Consequences
- Users on a newer CPA may hit ABI drift. The README says which version is verified.
- A re-verify step belongs in the release checklist.
