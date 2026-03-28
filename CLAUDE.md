# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Omnipus is an agentic core built on PicoClaw's foundation, shipping as three product variants:

1. **Omnipus Open Source** (primary, ships first) — Single Go binary with embedded web UI, similar to PicoClaw/OpenClaw. Community-facing, builds adoption.
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
- `OpenClaw_vs_PicoClaw_Comparison.md` — Competitive analysis informing UI/UX decisions

Always read the relevant BRD documents before implementing a feature. The specs are the source of truth.

## Hard Constraints

These are non-negotiable and apply to every decision:

1. **Single Go binary (agentic core)** — all backend features compile into one binary. No new runtime dependencies. Desktop wraps this in Electron. Open source embeds web UI via go:embed.
2. **Pure Go** — no CGo, no external C libraries, no shelling out for security-critical paths. Use `golang.org/x/sys/unix` for kernel interfaces.
3. **Minimal footprint** — total RAM overhead for all security features must stay under 10MB beyond baseline.
4. **Graceful degradation** — features requiring Linux 5.13+ (Landlock, seccomp) must fall back to application-level enforcement on older kernels, non-Linux platforms, and Android/Termux.
5. **Ecosystem compatibility** — follows PicoClaw/OpenClaw conventions (SKILL.md, HEARTBEAT.md, SOUL.md, AGENTS.md, JSON config patterns) for skill ecosystem and community compatibility. Omnipus has its own config format but adopts the same concepts.
6. **Deny-by-default for security, opt-in for features** — security policies default to most restrictive; functional features default to disabled.

## Tech Stack

**Backend:** Go (targeting Go 1.21+ for `slog`). Key packages: `golang.org/x/sys/unix` (Landlock, seccomp), `chromedp` (browser automation), `whatsmeow` (WhatsApp), `discordgo` (Discord), `telebot` (Telegram), `slack-go` (Slack), `go-nostr` (Nostr), `modernc.org/sqlite` (pure Go SQLite for whatsmeow — no CGo). Go channels are compiled into the single binary. Non-Go channels (Signal/Java, Teams/Node.js) and community channels use the bridge protocol as external processes.

**Frontend:** TypeScript, React 19, Vite 6, shadcn/ui (Radix + Tailwind CSS v4), AssistantUI (chat), Phosphor Icons (`@phosphor-icons/react`), Zustand (UI state), TanStack Query (server state), TanStack Router, Framer Motion. Shared `@omnipus/ui` component library across three variants: web (go:embed in binary for open source, ships first), Electron desktop (ships second), npm package (for SaaS/embedded, ships third).

**Storage:** File-based only (JSON/JSONL) for all Omnipus data. No PostgreSQL or Redis. Exception: WhatsApp session uses SQLite via whatsmeow with `modernc.org/sqlite` (pure Go, no CGo). SQLite is isolated to WhatsApp session storage only — never used for Omnipus's own data. Data directory: `~/.omnipus/`. Atomic writes (temp file + rename). Credentials in `credentials.json` (AES-256-GCM encrypted, Argon2id KDF), never in `config.json`. **Sessions:** Day-partitioned JSONL transcripts (`sessions/<id>/<YYYY-MM-DD>.jsonl`) with configurable retention (default 90 days). Two-layer context compression: tool result pruning (in-memory) + conversation compaction (persistent, with memory flush). **Concurrency:** Per-entity files for high-contention data (tasks, pins), single-writer goroutine for shared files (config, credentials), advisory `flock`/`LockFileEx` as defense-in-depth.

## Architecture Patterns

**Platform abstraction for sandboxing:** `SandboxBackend` interface with Linux (Landlock+seccomp), Windows (Job Objects+Restricted Tokens+DACL), and Fallback (app-level) backends. Policy engine and audit logging are cross-platform; only enforcement backend varies.

**Channel provider model:** Hybrid in-process/bridge architecture inheriting PicoClaw's design. Go channels are compiled into the binary and communicate via an internal `MessageBus` (zero IPC overhead, single process). Non-Go channels (Signal/Java, Teams/Node.js) and community channels run as external processes using a bridge protocol (JSON over stdin/stdout). All channels implement the same `ChannelProvider` Go interface — compiled-in channels implement it directly, external channels implement it via `BridgeAdapter`. Community channels are built with the Omnipus Channel SDK, installed locally at user's own risk.

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
