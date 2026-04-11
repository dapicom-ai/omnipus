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

Every production caller must follow this exact sequence:

```
NewStore → Unlock → LoadConfigWithStore → InjectFromConfig → InjectChannelsFromConfig → NewManager → Start
```

1. **`credentials.NewStore(path)`** — construct the store (does not read disk, safe before any I/O)
2. **`credentials.Unlock(store)`** — decrypt the store using the master key (see Provisioning below). If this fails, boot aborts with a fatal error.
3. **`config.LoadConfigWithStore(path, store)`** — load config, migrating v0 configs by moving plaintext secrets into the store and returning a v1 config with `*Ref` fields. If the v0 config contains plaintext secrets and the store is nil or locked, the load fails with an actionable error.
4. **`credentials.InjectFromConfig(cfg, store)`** — resolve each provider `APIKeyRef` from the store and inject into the process environment. Any error is fatal at boot; reject the reload on hot-reload.
5. **`credentials.InjectChannelsFromConfig(cfg, store)`** — resolve all channel/web-tool `*Ref` fields into the process environment. `ErrNotFound` for an enabled channel is fatal. `ErrNotFound` for a disabled channel or unconfigured tool is logged at Info and skipped.
6. **`channels.NewManager(cfg, bus, mediaStore)`** — channel constructors read their credentials via `os.Getenv(cfg.X.TokenRef)`. If construction fails for an enabled channel, boot aborts.
7. **`manager.Start()`** — begin receiving messages.

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

| Scenario | Boot behavior | Hot-reload behavior |
|----------|--------------|---------------------|
| `Unlock` fails | Fatal — abort boot | Reject reload, keep old config |
| `InjectFromConfig` returns any error | Fatal — abort boot | Reject reload, keep old config |
| `InjectChannelsFromConfig` returns `ErrStoreLocked` | Fatal | Reject reload |
| `InjectChannelsFromConfig` returns `ErrNotFound` for enabled channel | Fatal | Reject reload |
| `InjectChannelsFromConfig` returns `ErrNotFound` for disabled channel/tool | Info log, continue | Info log, continue |
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

## Consequences

- Gateway always starts with a fully resolved environment or not at all.
- Operators get a clear error message when OMNIPUS_MASTER_KEY is missing.
- v0 → v1 migration is automatic and preserves all secrets.
- Hot-reload failures are non-destructive: the old config continues serving.
- See also: `pkg/credentials/inject.go`, `pkg/config/config_old.go` (`MigrateWithStore`), `pkg/gateway/gateway.go` (`Run`, `executeReload`).
