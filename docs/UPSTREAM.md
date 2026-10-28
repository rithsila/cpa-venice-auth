# Upstream provenance

The plugin is built against a pinned CLIProxyAPI revision. This file records
exactly which one, what was copied from it, and how to move to a newer one.

## Pinned revision

| Field | Value |
|---|---|
| Repository | `https://github.com/router-for-me/CLIProxyAPI` |
| Tag | `v8.0.45` |
| Commit | `ad8bdd3f20bef44f0684b8a6096a216b2c76040d` |
| Module path | `github.com/router-for-me/CLIProxyAPI/v8` |
| Plugin ABI version | `1` |

The clone lives at `third_party/CLIProxyAPI` and is gitignored. It is reference
material, not a build input.

```bash
git clone --depth 1 --branch v8.0.45 \
  https://github.com/router-for-me/CLIProxyAPI.git third_party/CLIProxyAPI
```

## What came from upstream

Copied by hand, then adapted:

- The cgo preamble in `cmd/plugin/main.go` — `cliproxy_buffer`,
  `cliproxy_host_api`, `cliproxy_plugin_api`, and the four exported symbols.
  Shape taken from `examples/plugin/simple/go/main.go`.
- RPC method name strings: `plugin.register`, `plugin.reconfigure`,
  `auth.identifier`, `auth.parse`, `auth.login.start`, `auth.login.poll`,
  `auth.refresh`, `executor.*`, `quota.*`, `model.*`. Taken from
  `sdk/pluginabi/types.go`.
- Response struct shapes: `pluginapi.Metadata`, `pluginapi.ConfigField`,
  `pluginapi.Plugin`, auth and executor request/response types. Taken from
  `sdk/pluginapi/types.go`.

Nothing is imported. `go.mod` has no `require` line. Every type the host
exchanges with the plugin is re-declared in `internal/` so the build cannot
break when upstream reshuffles its module layout.

Why: at v8.0.45 the example modules still declare
`module .../CLIProxyAPI/v7` with a `replace` to `../../../..`, while the root
module is `/v8`. Depending on that path is a build trap. Re-declaring the wire
types is cheaper than tracking it.

## Host behaviour verified at this revision

Facts checked by reading the pinned source and by loading the built plugin into
the v8.0.45 release binary.

| Behaviour | Where |
|---|---|
| Discovery scans `<dir>/<goos>/<goarch>/` then `<dir>/` | `internal/pluginhost/platform.go` `candidateDirs` |
| Plugin ID is the filename stem | `internal/pluginhost/platform.go` `pluginIDFromPath` |
| Extension is `.dylib` on darwin, `.so` on linux, `.dll` on windows | `internal/pluginhost/platform.go` `pluginExtension` |
| Registration is rejected unless `Name`, `Version`, `Author`, and `GitHubRepository` are all non-empty | `internal/pluginhost/host.go` `validPlugin` |
| Rejection is logged as `returned invalid metadata or no capabilities`, which is misleading: it also fires for missing metadata | `internal/pluginhost/host.go:1139` |
| Metadata JSON uses Go field names (`Name`, `Version`, `Author`, `GitHubRepository`, `Logo`, `ConfigFields`) | `sdk/pluginapi/types.go` |
| Capability keys are lowercase snake case (`auth_provider`, `executor`, ...) | `internal/pluginhost/rpc_schema.go` |
| Every reply is an envelope: `{"ok":true,"result":{...}}` or `{"ok":false,"error":{"code":...,"message":...}}` | `sdk/pluginabi` envelope, `internal/pluginhost/rpc_client.go` `callPlugin` |
| A missing `schema_version` is read as `1`; a higher-than-supported version fails registration | `internal/pluginhost/rpc_client.go` `registerRPCPlugin` |
| `host.log` payload is `{"level","message","fields"}` | `internal/pluginhost/host_callbacks.go` |
| `plugins.configs.<id>` must use the same id as the filename stem | `internal/pluginhost/config.go` |

## Refresh procedure

Do this in one sitting. Half-migrated ABI code fails in confusing ways.

1. Pick the new tag. Read its `sdk/pluginabi/types.go` and
   `sdk/pluginapi/types.go` diffs against the pinned commit.
2. Re-clone at the new tag:

   ```bash
   rm -rf third_party/CLIProxyAPI
   git clone --depth 1 --branch <new-tag> \
     https://github.com/router-for-me/CLIProxyAPI.git third_party/CLIProxyAPI
   ```

3. Diff the cgo preamble against `examples/plugin/simple/go/main.go`. If the
   struct layout changed, update `cmd/plugin/main.go` first — nothing else can
   be trusted until the ABI matches.
4. Diff method names in `sdk/pluginabi/types.go` against the strings in
   `internal/plugin/plugin.go`. Rename in one commit.
5. Diff `rpcRegistration`, `rpcCapabilities`, and `pluginapi.Metadata` against
   `internal/plugin/registration.go`. A new required metadata field shows up as
   a false `invalid metadata` warning at load time.
6. Rebuild, run the load test, and confirm the log line:

   ```
   pluginhost: plugin registered plugin_id=venice-provider plugin_name=venice-provider
   ```

7. Update the table above with the new tag and commit, then commit the doc
   change with the code change.

## Failure signature to watch for

| Log line | Meaning |
|---|---|
| `plugin loaded ... path=...` only, no `plugin registered` | Registration call failed or returned invalid metadata |
| `returned invalid metadata or no capabilities` | One of `Name`/`Version`/`Author`/`GitHubRepository` is empty, or every capability flag is false |
| `plugin.register failed: ...` | Envelope decode error, wrong result shape, or unsupported `schema_version` |
| `plugin.load failed` | The shared library did not export the four C symbols, or the ABI struct layout is wrong |

## Related

- [`ARCHITECTURE.md`](ARCHITECTURE.md) — lifecycle, RPC table, routing
- [`DECISIONS.md`](DECISIONS.md) — why Go, why a plugin, why no module import
