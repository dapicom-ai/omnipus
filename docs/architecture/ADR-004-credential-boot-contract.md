# ADR-004 — Credential Boot Contract

**Status:** Accepted
**Date:** 2026-04-11
**Deciders:** backend-lead, security-lead

---

## Context

The previous configuration format (v0) stored plaintext secrets (API keys, bot tokens, channel passwords) directly in `config.json`. The `.security.yml` approach used in PicoClaw has been removed. All secrets must now flow through the encrypted credential store (`credentials.json`, AES-256-GCM, Argon2id KDF).

Without a formal contract, callers were using `LoadConfig` (store=nil), which silently dropped plaintext secrets during v0→v1 migration and logged inject failures at `Warn` level, allowing the gateway to start with broken channels.

---

## Decision

### Boot Order Contract

Every production caller must follow this exact sequence, implemented in the shared `bootCredentials` helper (`pkg/gateway/gateway.go`):

```
NewStore → Unlock → LoadConfigWithStore → InjectFromConfig →
ResolveBundle → cfg.RegisterSensitiveValues(plaintexts) →
NewManager(cfg, bundle, bus) → Start
```

1. **`credentials.NewStore(path)`** — construct the store (does not read disk, safe before any I/O)
2. **`credentials.Unlock(store)`** — decrypt the store using the master key (see Provisioning below). If this fails, boot aborts with a fatal error.
3. **`config.LoadConfigWithStore(path, store)`** — load config, migrating v0 configs by moving plaintext secrets into the store and returning a v1 config with `*Ref` fields. If the v0 config contains plaintext secrets and the store is nil or locked, the load fails with an actionable error.
4. **`credentials.InjectFromConfig(cfg, store)`** — resolve each provider `APIKeyRef` from the store and inject into the process environment so LLM SDK clients can read them. Any error is fatal at boot; reject the reload on hot-reload.
5. **`credentials.ResolveBundle(cfg, store)`** — resolve all channel `*Ref` fields into a `SecretBundle`. Channels receive secrets via the bundle — no `os.Setenv` for channel credentials. `ErrNotFound` for an **enabled** channel is fatal. `ErrNotFound` for a **disabled** channel is logged at Info and skipped.
6. **`cfg.RegisterSensitiveValues(plaintexts)`** — register all resolved plaintext values with the config's sensitive-data replacer so they are scrubbed from LLM output and audit logs. Semantics are "replace not append" — every call must supply the complete current set so that rotated or removed secrets are evicted.
7. **`channels.NewManager(cfg, bundle, bus, mediaStore)`** — channel constructors receive secrets via the `SecretBundle` parameter. If construction fails for an enabled channel, boot aborts.
8. **`manager.Start()`** — begin receiving messages.

### Canonical Shared Helper

`bootCredentials(homePath, configPath string)` in `pkg/gateway/gateway.go` is the single implementation of the above sequence. Both `gateway.Run` and `pkg/gateway/boot_order_test.go` call this helper so that a refactor of one cannot silently drift from the other.

REST-initiated config writes (`safeUpdateConfigJSON`) also rewire sensitive-data scrubbing via `restAPI.refreshConfigAndRewireServices`, which runs steps 3–6 on the new config and atomically swaps it via `agentLoop.SwapConfig`.

### Master-Key Provisioning

The credential store is unlocked using the first available source (in priority order):

1. **`OMNIPUS_MASTER_KEY`** — 64-character hex-encoded 256-bit key set in the environment. Recommended for CI/CD and container deployments.
2. **`OMNIPUS_KEY_FILE`** — path to a file containing the hex key, mode 0600. Recommended for server deployments where env injection is impractical.
3. **Interactive TTY prompt** — passphrase entered at the terminal. Only available when a TTY is attached (not suitable for headless/daemon mode).

To generate a key file:

```bash
openssl rand -hex 32 > /path/to/omnipus.key
chmod 600 /path/to/omnipus.key
export OMNIPUS_KEY_FILE=/path/to/omnipus.key
```

Headless deployments **must** set `OMNIPUS_MASTER_KEY` or `OMNIPUS_KEY_FILE`. If neither is set and no TTY is present, `credentials.Unlock` returns an error and boot aborts.

### Failure Semantics

| Scenario | Boot behavior | Hot-reload / REST-write behavior |
|----------|--------------|----------------------------------|
| `Unlock` fails | Fatal — abort boot | Reject reload, keep old config |
| `LoadConfigWithStore` fails | Fatal — abort boot | Reject reload, keep old config |
| `InjectFromConfig` returns any error | Fatal — abort boot | Reject reload, keep old config |
| `ResolveBundle` returns `ErrNotFound` for **enabled** channel | Fatal — abort boot | Reject reload, keep old config |
| `ResolveBundle` returns `ErrNotFound` for **disabled** channel | Info log, continue | Info log, continue |
| `RegisterSensitiveValues` (never errors) | — | — |
| `channels.NewManager` fails (enabled channel) | Fatal | Reject reload |

### Legacy v0 Migration

When `LoadConfigWithStore` encounters a v0 config file:

1. Each non-empty plaintext secret field is written to the credential store under a canonical ref name (e.g. `TELEGRAM_TOKEN`, `DISCORD_TOKEN`).
2. The corresponding `*Ref` field in the output config is set to that ref name.
3. A v1 config is written back to disk, replacing the v0 file. A backup of the original is kept at `config.json.bak`.
4. If the store is nil or locked during migration and the v0 config contains secrets, `LoadConfigWithStore` returns an actionable error: the operator must set `OMNIPUS_MASTER_KEY` and retry.

### Canonical Loader

`config.LoadConfigWithStore` is the **only** sanctioned loader for production callers. `config.LoadConfig` (store=nil) is reserved for CLI sub-commands that do not perform migration (e.g., `omnipus auth`) and for unit tests that work exclusively with v1 configs containing no plaintext secrets.

---

## Failure Modes and Recovery

### Boot-time failures (fatal)

Any failure in the boot sequence (Unlock, LoadConfigWithStore, InjectFromConfig,
ResolveBundle, NewManager) aborts the process with a descriptive error. The
operator must fix the root cause and restart.

### Reload-time failures (rollback + degraded)

When `POST /reload` or the hot-reload watcher triggers `executeReload`, any
failure in `InjectFromConfig`, `ResolveBundle`, `handleConfigReload`, or
`restartServices` is caught:

1. `executeReload` snapshots the pre-reload `services` state via
   `snapshotServices` (captures bundle, ChannelManager, CronService,
   HeartbeatService, MediaStore, DeviceService).
2. On failure, `markDegraded` calls `restoreServices` to restore the snapshot
   and sets:
   - `services.reloadDegraded = true`
   - `services.reloadError = <wrapped error>`
3. `/health` returns HTTP 503 with:
   ```json
   {"status": "degraded", "reason": "config reload failed: <error>"}
   ```
4. Load balancers (k8s readiness probes, nginx, envoy) remove the pod from
   rotation automatically.
5. Old channels + services continue serving requests from the snapshotted
   state — no traffic interruption.

**Recovery:** fix `config.json`, POST `/reload` again. On success,
`clearDegraded` is called and `/health` returns 200.

**Partial rollback scope:** only the fields listed in `servicesSnapshot` are
restored (bundle, ChannelManager, CronService, HeartbeatService, MediaStore,
DeviceService). Cron task state, agent loop state, audit log, and other
side-effects that `stopAndCleanupServices` triggers are NOT rolled back.
Operators should prefer a full process restart for reloads that touch
cron/heartbeat/MCP state.

---

## Consequences

- Gateway always starts with a fully resolved environment or not at all.
- Operators get a clear error message when OMNIPUS_MASTER_KEY is missing.
- v0 → v1 migration is automatic and preserves all secrets.
- Hot-reload and REST config writes re-arm sensitive-data scrubbing so newly-added credentials are immediately scrubbed from LLM output and audit logs.
- Hot-reload failures roll back the in-memory service graph and set the process degraded. The old config continues serving while the operator fixes the new config.
- See also: `pkg/credentials/inject.go`, `pkg/config/config_old.go` (`MigrateWithStore`), `pkg/gateway/gateway.go` (`bootCredentials`, `Run`, `executeReload`, `snapshotServices`, `restoreServices`), `pkg/gateway/rest.go` (`refreshConfigAndRewireServices`, `safeUpdateConfigJSON`).
