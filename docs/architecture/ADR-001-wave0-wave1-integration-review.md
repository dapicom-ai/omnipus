# ADR-001: Wave 0 + Wave 1 Cross-Cutting Architecture Review

**Status:** Proposed
**Date:** 2026-03-29
**Deciders:** architect, team-lead

## Context

Wave 0 (frontend shell) and Wave 1 (backend core) are functionally complete. Before connecting them and before Wave 2 (security layer), we need to verify:

1. Data model alignment with Appendix E spec
2. MessageBus / channel policy readiness for Wave 2 security
3. Credential store SEC-23a-e compliance
4. SSE streaming protocol compatibility
5. Frontend/backend integration gaps

This review examines all new and modified code in `pkg/datamodel/`, `pkg/credentials/`, `pkg/session/`, `pkg/gateway/sse.go`, `pkg/gateway/shutdown.go`, `pkg/config/config.go`, `pkg/fileutil/`, `pkg/bus/`, and `src/`.

## Architecture Review: Wave 0 + Wave 1 Integration

### Summary

The backend data model, credential store, session system, and SSE endpoint are well-implemented and closely follow BRD specs. The frontend shell is structurally sound but contains only empty-state placeholders with no API integration. There are **3 blockers**, **5 warnings**, and **4 notes** requiring attention before frontend-backend connection and Wave 2 security work.

### Findings

| # | Concern | Severity | Component | Finding | BRD Ref | Recommendation |
|---|---------|----------|-----------|---------|---------|----------------|
| 1 | SSE endpoint has no authentication | **blocker** | `pkg/gateway/sse.go` | POST `/api/v1/chat` accepts requests from any origin with `Access-Control-Allow-Origin: *` and no token check. While SEC-20 (gateway auth) is a Wave 2 deliverable, the current CORS wildcard combined with no auth means any browser tab on localhost can inject messages into the agent loop. This is exploitable before Wave 2 ships. | SEC-20, SEC-07 | Add a placeholder auth middleware now (check `Authorization: Bearer <token>` header) with a startup-generated token printed to console. The wildcard CORS header must be replaced with configurable allowed origins. This is a pre-Wave-2 hardening step, not the full SEC-20 implementation. |
| 2 | Dual session backends create integration ambiguity | **blocker** | `pkg/session/` | Two session backends exist: `SessionManager` (legacy PicoClaw in-memory + flat JSON files) and `PartitionStore` (new Appendix E day-partitioned JSONL). Both satisfy `SessionStore` interface. The gateway currently uses `SessionManager` via the agent loop, while `PartitionStore` + `daypartition.go` are new but not wired into the gateway startup path. The agent loop does not know about `PartitionStore`. | Appendix E §E.5 | Decide which backend the gateway uses and wire it in. The spec requires day-partitioned JSONL with `meta.json` — that is `PartitionStore`. `SessionManager` should be deprecated or kept only as a fallback for legacy PicoClaw migrations. The `SessionStore` interface is the right abstraction; the wiring just needs to happen. |
| 3 | No REST API for frontend data access | **blocker** | `pkg/gateway/` | The only HTTP endpoint is SSE at `/api/v1/chat`. The frontend will need REST endpoints for: session list/history, agent list/status, task CRUD, project CRUD, settings read/write, credential management (list/add/delete, never expose values). Currently there is no REST router, no JSON API handlers, and no CORS middleware. The frontend shell has empty-state screens for all these but no fetch calls. | FUNC-10a, Appendix E | Define the REST API surface before connecting frontend and backend. Recommended: `/api/v1/sessions`, `/api/v1/agents`, `/api/v1/tasks`, `/api/v1/projects`, `/api/v1/settings`, `/api/v1/credentials` (names only). Use the existing `dynamicServeMux` in channels package for registration. |
| 4 | PartitionStore mutex serializes all sessions | **warning** | `pkg/session/daypartition.go` | `PartitionStore` uses a single `sync.Mutex` for all sessions of an agent. When two concurrent webchat sessions append messages, they block each other even though they write to different directories. This contradicts the per-entity concurrency model in Appendix E §E.2. | Appendix E §E.2 | Use a per-session lock (e.g., `sync.Map` of `*sync.Mutex` keyed by session ID), or accept the current design with a documented note that it becomes a bottleneck only under high concurrency (unlikely for single-user open-source variant). |
| 5 | Credential store reads bypass flock | **warning** | `pkg/credentials/store.go` | `loadFileInternal()` calls `os.ReadFile()` directly without acquiring an advisory lock. While the BRD spec says reads are allowed concurrent with writes (single-writer goroutine pattern), the `Store` uses a `sync.RWMutex` internally but no `flock`. If an external process (e.g., `omnipus doctor`) reads `credentials.json` while the gateway writes, it could see a partial temp file before rename. | Appendix E §E.2, SEC-23e | The atomic write pattern (temp+rename) already prevents partial reads of the target file. However, `saveFileNoLock` should call `fileutil.WithFlock` for defense-in-depth consistency with the stated storage philosophy. Currently only `fileutil.WithFlock` exists but is never called from credential store code. |
| 6 | `RotateWithPassphrase` generates salt then `Rotate` generates a different salt | **warning** | `pkg/credentials/store.go:277-287` | `RotateWithPassphrase` generates a new salt, derives a key, then calls `Rotate(newKey)` which generates *another* new salt and overwrites the first. The salt stored on disk will not match the salt used to derive the key. On next unlock with the same passphrase, `Argon2id` will produce a different key because the persisted salt differs. | SEC-23c | Fix `Rotate` to accept and persist the caller's salt, or have `RotateWithPassphrase` set the salt on the store file directly instead of delegating to `Rotate` which generates its own. This is a **data-loss bug** — after rotation, the new passphrase will fail to unlock the store. |
| 7 | Config has no `security.policy` section placeholder | **warning** | `pkg/config/config.go` | The BRD specifies `security.policy` in config.json (SEC-11) and `security.default_policy` (SEC-07). The Config struct has `ChannelPolicies` and `OmnipusStorageConfig` but no `SecurityPolicy` struct. Wave 2 will need to add tool allow/deny lists, RBAC roles, filesystem restrictions, and rate limit config. | SEC-07, SEC-11, SEC-12 | Add an empty `SecurityConfig` struct with `json:"security,omitempty"` now so that: (a) Wave 2 agents know where to put their code, (b) config files created during Wave 1 testing already have the correct top-level key, (c) the `MergeChannelPoliciesIntoBindings` routing can later be unified under `security.policy`. |
| 8 | SSE handler silently drops tokens on slow clients | **warning** | `pkg/gateway/sse.go:167-169` | `sseStreamer.Update` drops tokens with a `default` case when the 128-buffer channel is full. No error, no log, no notification to the client. The client will see a response with missing chunks and no indication of data loss. | FUNC-36 (graceful degradation) | At minimum, log a warning on token drop. Better: send an SSE `event: error` with `{"type":"backpressure"}` so the frontend can indicate incomplete output. Alternatively, increase the buffer or block with a context-aware timeout. |
| 9 | Frontend uses `createRouter` without hash mode | **note** | `src/main.tsx` | Comment says "Hash routing — required for go:embed" but `createRouter()` is called without `{ history: createHashHistory() }`. TanStack Router defaults to browser history (pushState). This will break under go:embed because the Go static file server cannot handle client-side routes — refreshing `/settings` will return 404. | CLAUDE.md (go:embed variant) | Import `createHashHistory` from `@tanstack/react-router` and pass it to `createRouter`. Without this, the open-source variant (go:embed) cannot serve the SPA correctly. |
| 10 | `cmd/omnipus/main.go` still shows PicoClaw banner | **note** | `cmd/omnipus/main.go:57-66` | The ASCII art banner still says "PICOCLAW" in box characters despite the codebase rename. The command name is correctly `omnipus`. | Brand | Replace the PicoClaw ASCII banner with Omnipus branding. Low priority but visible to every user on every launch. |
| 11 | No go:embed directive for frontend assets | **note** | `cmd/omnipus/` | The open-source variant requires `go:embed` to serve the built frontend from the binary. There is currently no `//go:embed` directive anywhere in `cmd/`. The frontend build output (Vite `dist/`) is not referenced by any Go code. | CLAUDE.md (single binary) | This is expected for Wave 0/1 — the frontend and backend run separately during development. However, an integration task should be tracked for connecting them: add `//go:embed dist/*` in a `cmd/omnipus/internal/web/` package that serves the static assets. |
| 12 | JSONLBackend uses `log` instead of `slog` | **note** | `pkg/session/jsonl_backend.go` | All error logging uses `log.Printf` instead of the project-standard `log/slog`. The rest of Wave 1 code consistently uses `slog`. | CLAUDE.md (Go 1.21+ slog) | Replace `log.Printf` calls with `slog.Error` or `slog.Warn` with structured fields. |

### Severity Definitions

- **blocker** -- Violates hard constraint or BRD requirement. Must resolve before proceeding to frontend-backend connection.
- **warning** -- Architectural risk. Should address, can defer with documented rationale.
- **note** -- Observation or suggestion. Non-blocking.

### Integration Risks

1. **Frontend-backend contract undefined.** The frontend has screens for Chat, Command Center, Agents, Skills, and Settings, all showing empty states. The backend has SSE streaming for chat. Everything in between (REST API, WebSocket for real-time updates, session listing, agent management) is undefined. The recommended path is to define a REST API spec (`/api/v1/*`) before connecting them — this becomes the integration contract.

2. **Session backend ambiguity blocks Chat integration.** The Chat screen needs to create sessions, stream tokens, and display history. The backend has two session backends with different storage formats. Until one is wired as primary, the frontend cannot know what session shape to expect.

3. **Wave 2 security needs insertion points.** The MessageBus currently has no middleware/interceptor layer. SEC-06 (per-method tool control) and SEC-15 (audit logging) require intercepting tool invocations. The recommended approach: add a `ToolInterceptor` interface to the agent loop that Wave 2 fills in, rather than retrofitting the MessageBus. The `ChannelPolicies` config pattern is good — it cleanly merges into `Bindings` — but needs to extend to tool policies.

4. **Credential rotation bug (Finding #6) is a data-loss risk.** If anyone tests `omnipus credentials rotate` before this is fixed, their credential store becomes unreadable.

### Positive Findings

- **Credential store (pkg/credentials/)** is excellent. Clean separation of concerns: `store.go` (CRUD + crypto), `keymgr.go` (three-mode provisioning), `inject.go` (config-to-env bridge). Matches SEC-23a-e precisely. The `_ref` pattern in config correctly decouples secrets from config.
- **Day-partitioned session storage (pkg/session/daypartition.go)** matches Appendix E §E.5 schema exactly: `SessionMeta`, `TranscriptEntry`, `ToolCall`, `Attachment` types all correspond to the spec's JSON structures. ULID-based session IDs, UTC day partitioning, compaction entry support — all correct.
- **Atomic writes (pkg/fileutil/)** are solid: temp+rename with fsync, directory sync for durability, proper cleanup on failure. The `AppendJSONL` function correctly uses `O_APPEND` for POSIX atomicity. `WithFlock` exists with graceful degradation on Windows.
- **Graceful shutdown (pkg/gateway/shutdown.go)** implements the 5-step sequence per FUNC-36 with proper timeout handling and service ordering.
- **Data model init (pkg/datamodel/init.go)** creates the exact directory tree from Appendix E §E.3 with correct permissions (0o700) and idempotent behavior.
- **Config extensibility** — the `OmnipusChannelPolicy` and `MergeChannelPoliciesIntoBindings` pattern is clean and provides a good template for how Wave 2 security policies should integrate.

### Verdict

**REVISE** — Fix the 3 blockers (SSE auth, session backend wiring, REST API surface) and the credential rotation bug (Finding #6, data-loss risk) before connecting frontend to backend. The remaining warnings and notes can be addressed incrementally.

## Consequences

### Positive
- Clear integration contract prevents frontend/backend drift
- Early SSE auth hardening reduces attack surface before Wave 2
- Single session backend eliminates confusion for all subsequent waves

### Negative
- REST API definition is additional work before frontend integration
- Deprecating SessionManager requires migration path for existing PicoClaw data

### Neutral
- Wave 2 security work has clean insertion points via the existing config pattern
- The three-variant deployment model is not yet tested but the architecture supports it

## Affected Components

- Backend: `pkg/gateway/` (REST API, SSE auth), `pkg/session/` (backend selection), `pkg/credentials/store.go` (rotation bug), `pkg/config/config.go` (security placeholder)
- Frontend: `src/main.tsx` (hash routing), all route screens (API integration)
- Variants: Open source (go:embed not wired yet — expected at this stage)
