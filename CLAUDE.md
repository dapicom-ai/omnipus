# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Omnipus is an agentic core built on Omnipus's foundation, shipping as three product variants:

1. **Omnipus Open Source** (primary, ships first) — Single Go binary with embedded web UI, similar to Omnipus/OpenClaw. Community-facing, builds adoption.
2. **Omnipus Desktop** (ships second) — Free polished Electron desktop app. Premium UX, auto-updates, native menus.
3. **Omnipus Cloud/SaaS** (ships third) — Scalable hosted version with team features and managed infrastructure.

All variants share a common Go agentic core with kernel-level sandboxing, RBAC, audit logging, credential management, compiled-in Go channels, and a shared `@omnipus/ui` React component library.

**Domain:** omnipus.ai

## Status

Pre-implementation. The `docs/BRD/` directory contains the complete specification:

- `Omnipus BRD.md` — Main BRD: 27 security + 18 functional requirements, 3 delivery phases
- `Omnipus Windows BRD appendic.md` — Appendix A: Windows kernel security (Job Objects, Restricted Tokens, DACL)
- `Omnipus_BRD_AppendixB_Feature_Parity.md` — Appendix B: 38 feature parity requirements (ClawHub, browser, WhatsApp, channels)
- `Omnipus_BRD_AppendixC_UI_Spec.md` — Appendix C: Full UI/UX spec (React 19, Vite 6, shadcn/ui, Phosphor Icons)
- `Omnipus_BRD_AppendixD_System_Agent.md` — Appendix D: System agent with 35 system tools, 3 core agents
- `Omnipus_BRD_AppendixE_DataModel.md` — Appendix E: File-based data model (JSON/JSONL), directory structure, entity schemas
- `OpenClaw_vs_Omnipus_Comparison.md` — Competitive analysis informing UI/UX decisions

Always read the relevant BRD documents before implementing a feature. The specs are the source of truth.

## Hard Constraints

These are non-negotiable and apply to every decision:

1. **Single Go binary (agentic core)** — all backend features compile into one binary. No new runtime dependencies. Desktop wraps this in Electron. Open source embeds web UI via go:embed.
2. **Pure Go** — no CGo, no external C libraries, no shelling out for security-critical paths. Use `golang.org/x/sys/unix` for kernel interfaces.
3. **Minimal footprint** — total RAM overhead for all security features must stay under 10MB beyond baseline.
4. **Graceful degradation** — features requiring Linux 5.13+ (Landlock, seccomp) must fall back to application-level enforcement on older kernels, non-Linux platforms, and Android/Termux.
5. **Ecosystem compatibility** — follows Omnipus/OpenClaw conventions (SKILL.md, HEARTBEAT.md, SOUL.md, AGENTS.md, JSON config patterns) for skill ecosystem and community compatibility. Omnipus has its own config format but adopts the same concepts.
6. **Deny-by-default for security, opt-in for features** — security policies default to most restrictive; functional features default to disabled.

## Tech Stack

**Backend:** Go (targeting Go 1.21+ for `slog`). Key packages: `golang.org/x/sys/unix` (Landlock, seccomp), `chromedp` (browser automation), `whatsmeow` (WhatsApp), `discordgo` (Discord), `telebot` (Telegram), `slack-go` (Slack), `go-nostr` (Nostr), `modernc.org/sqlite` (pure Go SQLite for whatsmeow — no CGo). Go channels are compiled into the single binary. Non-Go channels (Signal/Java, Teams/Node.js) and community channels use the bridge protocol as external processes.

**Frontend:** TypeScript, React 19, Vite 6, shadcn/ui (Radix + Tailwind CSS v4), AssistantUI (chat), Phosphor Icons (`@phosphor-icons/react`), Zustand (UI state), TanStack Query (server state), TanStack Router, Framer Motion. Shared `@omnipus/ui` component library across three variants: web (go:embed in binary for open source, ships first), Electron desktop (ships second), npm package (for SaaS/embedded, ships third).

**Storage:** File-based only (JSON/JSONL) for all Omnipus data. No PostgreSQL or Redis. Exception: WhatsApp session uses SQLite via whatsmeow with `modernc.org/sqlite` (pure Go, no CGo). SQLite is isolated to WhatsApp session storage only — never used for Omnipus's own data. Data directory: `~/.omnipus/`. Atomic writes (temp file + rename). Credentials in `credentials.json` (AES-256-GCM encrypted, Argon2id KDF), never in `config.json`. **Sessions:** Day-partitioned JSONL transcripts (`sessions/<id>/<YYYY-MM-DD>.jsonl`) with configurable retention (default 90 days). Two-layer context compression: tool result pruning (in-memory) + conversation compaction (persistent, with memory flush). **Concurrency:** Per-entity files for high-contention data (tasks, pins), single-writer goroutine for shared files (config, credentials), advisory `flock`/`LockFileEx` as defense-in-depth.

**Credential provisioning:** All secrets are stored in `credentials.json` (AES-256-GCM, Argon2id KDF). See [ADR-004](docs/architecture/ADR-004-credential-boot-contract.md) for the full boot contract.

**Unlock modes** (tried in priority order):

1. `OMNIPUS_MASTER_KEY` — 64-char hex-encoded 256-bit key in the environment. Use for CI/CD pipelines and container deployments where secrets are injected via env.
2. `OMNIPUS_KEY_FILE` — path to a file (mode 0600) containing the hex key. Use for long-running server deployments where mounting a key file is more practical than env injection.
3. **Default key file** — if `$OMNIPUS_HOME/master.key` exists (mode 0600), it is loaded automatically. This is how auto-generated keys survive across reboots without any env configuration.
4. **Auto-generate on fresh install** — if no key is configured and no `credentials.json` exists yet, the gateway mints a fresh 256-bit key, writes it to `$OMNIPUS_HOME/master.key` with 0600, and logs a prominent backup warning to stderr. This closes the headless first-run chicken-and-egg: a new user on a cloud VPS can start the gateway with zero configuration and still end up with a working encrypted credential store. Auto-generate **never** fires when an existing `credentials.json` is present — that would strand the encrypted data.
5. **Interactive TTY prompt** — passphrase entered at the terminal. Only works when a TTY is attached; never use for headless/daemon mode.

**Critical — back up the master key file.** Whether you provide it via `OMNIPUS_KEY_FILE`, or it was auto-generated to `$OMNIPUS_HOME/master.key` on first boot, losing it makes every credential in `credentials.json` (API keys, channel tokens, etc.) permanently inaccessible. The auto-generate path prints a multi-line warning to stderr on first boot — watch for it in systemd journal / Docker logs.

**Generating a key file manually** (for operators who prefer explicit provisioning over auto-generate):

```bash
openssl rand -hex 32 > /var/lib/omnipus/master.key
chmod 600 /var/lib/omnipus/master.key
export OMNIPUS_KEY_FILE=/var/lib/omnipus/master.key
```

**Key rotation:** Generate a new key, then re-encrypt using `omnipus credentials rotate` (checks `--old-key-file` and `--new-key-file`). The rotate command decrypts with the old key and re-encrypts every credential with the new key atomically. Update `OMNIPUS_KEY_FILE` to point at the new key (or replace `$OMNIPUS_HOME/master.key`) before restarting the gateway. There is no zero-downtime rotation path in the current CLI — a brief restart is required.

**Boot order:** `NewStore → Unlock → LoadConfigWithStore → InjectFromConfig → ResolveBundle → RegisterSensitiveValues → NewManager → Start` — any failure aborts boot. Channel secrets are passed directly as a `credentials.SecretBundle` to channel constructors; they do not require environment injection.

## Architecture Patterns

**Platform abstraction for sandboxing:** `SandboxBackend` interface with Linux (Landlock+seccomp), Windows (Job Objects+Restricted Tokens+DACL), and Fallback (app-level) backends. Policy engine and audit logging are cross-platform; only enforcement backend varies.

**Channel provider model:** Hybrid in-process/bridge architecture inheriting Omnipus's design. Go channels are compiled into the binary and communicate via an internal `MessageBus` (zero IPC overhead, single process). Non-Go channels (Signal/Java, Teams/Node.js) and community channels run as external processes using a bridge protocol (JSON over stdin/stdout). All channels implement the same `ChannelProvider` Go interface — compiled-in channels implement it directly, external channels implement it via `BridgeAdapter`. Community channels are built with the Omnipus Channel SDK, installed locally at user's own risk.

**Agent types:** System (`omnipus-system`, hardcoded, always on, 35 exclusive `system.*` tools), Core (hardcoded prompts compiled into binary, user can toggle/configure), Custom (user-defined with SOUL.md + AGENTS.md).

**Brand:** "The Sovereign Deep" — dark-first design. Colors: Deep Space Black (`#0A0A0B`), Liquid Silver (`#E2E8F0`), Forge Gold (`#D4AF37`). Fonts: Outfit (headlines), Inter (body), JetBrains Mono (code). Octopus mascot ("Master Tasker"). See `docs/brand/brand-guidelines.md`.

**UI design rules:** Chat-first, dark-first. Sidebar defaults to overlay drawer but can be pinned for persistent navigation. No separate canvas (rich content renders inline, expands to fullscreen). No emoji in stored data or UI chrome (emoji-to-Phosphor-icon translator in chat output only). Tool calls visible by default with collapsible detail.

## Spec-Driven Workflow

Use this sequence when implementing features:

1. Read the relevant BRD section(s) before writing any code
2. Use `/plan-spec` to generate implementation specs with TDD/BDD scenarios
3. Use `/grill-spec` to stress-test specs for gaps before implementation
4. Use `/taskify` to decompose into structured task graphs
5. Implement in Plan Mode first, then switch to normal mode
6. Use `/grill-code` to verify spec compliance after implementation

## Subagent Workflow

The lead (you) orchestrates all work by spawning specialized subagents via the Agent tool. There are no agent teams — you spawn subagents directly, give them focused tasks, and review their output.

### Implementing subagents (spawn via Agent tool with `subagent_type`)
- `frontend-lead` (sonnet) — React/TypeScript UI work. Scope: `src/`, `packages/ui/`
- `backend-lead` (sonnet) — Go backend work. Scope: `pkg/`, `cmd/`, `internal/` (except security packages)
- `security-lead` (opus) — Security implementation. Scope: `pkg/security/`, `pkg/sandbox/`, `pkg/audit/`, `pkg/policy/`
- `qa-lead` (sonnet) — Tests only. Scope: `*_test.go`, `*.test.ts`, `*.test.tsx`

### Review subagents (spawn via Agent tool with `subagent_type`)
- `architect` (opus) — cross-cutting design review, ADRs
- pr-review-toolkit agents (6 total, always run all after implementation work)

### How to use subagents

1. **Decompose the work** — break the task into focused units scoped to one subagent each
2. **Spawn subagents with clear, complete prompts** — include the spec reference, the exact files to modify, and what "done" looks like. Each subagent starts fresh with no prior context.
3. **Run subagents in parallel** when their work is independent (e.g., frontend + backend for the same feature)
4. **Review every subagent's output** — check their functional proof, verify their claims, run the review pipeline
5. **Run QA after implementation** — spawn qa-lead to write tests against the code the other subagents just wrote

### Which subagents to spawn per task type
- **Frontend-only work:** frontend-lead → qa-lead
- **Backend-only work:** backend-lead → qa-lead
- **Security work:** security-lead + backend-lead → qa-lead
- **Full-stack features:** frontend-lead + backend-lead (parallel) → qa-lead
- **Design questions:** architect

### Review pipeline (run after implementation subagents complete)

**Step 1 — Project-specific reviews (in parallel):**
- Go files changed → `architect` for cross-cutting concerns
- Frontend files changed → `architect` for integration coherence
- Security files changed → `architect` for threat model review

**Step 2 — PR-review-toolkit (run ALL 6 in parallel, always):**
1. `pr-review-toolkit:code-reviewer` — CLAUDE.md compliance, bugs, quality
2. `pr-review-toolkit:code-simplifier` — simplify for clarity and maintainability
3. `pr-review-toolkit:comment-analyzer` — verify comment accuracy
4. `pr-review-toolkit:pr-test-analyzer` — test coverage quality
5. `pr-review-toolkit:silent-failure-hunter` — find silent failures, bad error handling
6. `pr-review-toolkit:type-design-analyzer` — type/interface design quality

**Step 3 — Resolve findings:**
- Fix issues found by reviews (spawn the relevant implementing subagent to fix)
- Re-run failed reviews after fixes
- Only create PR when all reviews pass

## Build & E2E Testing

### SPA Embed Pipeline

The Go binary embeds the frontend SPA from `pkg/gateway/spa/` via `go:embed`. This directory is **not** the Vite build output — `npm run build` outputs to `dist/spa/`. You must sync them before building the binary:

```bash
npm run build                    # builds to dist/spa/
rm -rf pkg/gateway/spa/assets    # remove stale assets
cp -r dist/spa/* pkg/gateway/spa/  # sync to embed dir
CGO_ENABLED=0 go build -o /tmp/omnipus ./cmd/omnipus/  # rebuild binary
```

**If you skip the sync, the binary will serve a stale SPA that does not include your frontend changes.** Verify with: `grep -c "YOUR_NEW_STRING" pkg/gateway/spa/assets/index-*.js` — must be >0.

### E2E Testing with the Embedded SPA

Always test against the embedded SPA (the Go binary), not the Vite dev server. The Vite dev server proxies `/api` to `localhost:18790` which may not match the gateway port.

**Start the gateway:**

```bash
export OMNIPUS_HOME=/tmp/omnipus-e2e-test
rm -rf "$OMNIPUS_HOME" && mkdir -p "$OMNIPUS_HOME"
OMNIPUS_BEARER_TOKEN="" ./omnipus gateway --allow-empty &
```

**Known blockers and workarounds:**

1. **Port conflict with other apps** — Port 3000 is the default. If another app (e.g., Next.js) is running on 3000, the gateway silently fails to bind. Check with `lsof -i :3000 | grep LISTEN`. Fix: set a different port in `$OMNIPUS_HOME/config.json` under `gateway.port` (e.g., 5000) before starting.

2. **Onboarding requires auth bypass** — The onboarding flow calls API endpoints that require bearer auth before an admin account exists. Set `"dev_mode_bypass": true` in `config.json` under `gateway` after the first run creates the config, then restart the gateway. This is a pre-existing bug (the onboarding endpoints should use `withOptionalAuth`).

3. **Model must support tool use** — Omnipus sends tools with every LLM request. If the selected model doesn't support tool use (e.g., `google/gemma-2-9b-it` on OpenRouter), the LLM call returns a 404 with "No endpoints found that support tool use." Use a tool-capable model like `z-ai/glm-5-turbo`, `google/gemini-2.5-flash`, or `anthropic/claude-3.5-haiku`.

4. **Gateway logs are in `$OMNIPUS_HOME/logs/`** — `gateway.log` for runtime logs, `gateway_panic.log` for startup errors. Always check `gateway_panic.log` if the gateway exits silently.

### E2E Test Checklist

After frontend+backend changes, verify these flows on the embedded SPA:

1. **Onboarding** — Welcome → Provider → API Key → Model Select → Admin Account → Complete
2. **Chat** — Send message → receive LLM response → multi-turn context retained → token/cost updates
3. **Agents** — List (system + custom) → Create Agent (with Tools & Permissions) → Agent Profile (accordion, tools panel)
4. **System Agent** — Profile shows read-only sections only (no Identity, no Tools & Permissions)
5. **Settings** — Provider shows Connected, all tabs load
6. **Command Center** — Gateway status, task board
7. **Skills & Tools** — 4 tabs, empty states
8. **Sidebar** — All nav items, active highlighting
9. **Console errors** — Zero JS errors (WebSocket reconnect warnings are acceptable)
