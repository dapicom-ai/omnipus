<div align="center">
<img src="IMG_0432.svg" alt="Omnipus" width="400">

<h1>Omnipus</h1>

<h3>Multi-agent orchestration — sovereign, sandboxed, single binary.</h3>

<p>An opinionated agent runtime with five named coworkers, hand-off between them, kernel-level sandboxing, and 17 chat channels. One <code>go build</code>, no database, runs on a $10 VPS.</p>

<p>
  <img src="https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat&logo=react&logoColor=white" alt="React">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <a href="https://omnipus.ai"><img src="https://img.shields.io/badge/Website-omnipus.ai-D4AF37?style=flat&logo=google-chrome&logoColor=white" alt="Website"></a>
</p>

<img src="docs/marketing/screenshots/04-agents-roster.png" alt="Omnipus agent roster" width="900">

</div>

---

## Why Omnipus

Most agent frameworks give you orchestration **or** a security story. Omnipus ships both, in a single Go binary, without pulling in Postgres, Redis, or a Python runtime.

- **Five named agents out of the box** — not one general-purpose chatbot, but a team with defined roles and delegation rules.
- **Real hand-off, not fake role-play** — agents pass control through a transcript the next agent can actually read.
- **Deny-by-default security** — Linux Landlock sandbox, SSRF guard, three-tier tool policy (allow / ask / deny), encrypted credential store.
- **Runs anywhere** — single static binary, embedded SPA, auto-generates its own encryption key on first boot. Works on a laptop, a $10 VPS, or a Raspberry Pi.

---

## The five core agents

<img src="docs/marketing/screenshots/04-agents-roster.png" alt="Agents roster" width="900">

| Agent | Role | What they do |
|---|---|---|
| **Mia** | Coach & Guide | Default agent. Onboards you to the platform, explains features, answers setup questions. |
| **Jim** | General Purpose | Warm, fast, reliable. Research, writing, analysis, coordination with other agents. |
| **Ava** | Agent Builder | Interviews you about what you need, then creates a custom agent with tools, persona, and prompt. |
| **Ray** | Researcher | Deep research with citations. Web search, web fetch, synthesis — then hands visual/automation work to Max. |
| **Max** | Automator | Browser automation, plan-then-execute, multi-step orchestration with approval gates. |

Identity (name, description, color, icon, prompt) is **locked** on core agents — users can change their model and tool policy, but can't silently replace Mia with a knock-off. Custom agents are unlimited.

---

## Live demos

All screenshots below are real conversations captured against the running binary.

### Max screenshots a page, inline

Ask Max to screenshot a URL. He chains `browser.navigate` → `browser.screenshot`, the image streams back into the chat through the media pipeline, and renders inline.

<img src="docs/marketing/screenshots/13-max-screenshot-demo.png" alt="Max screenshots example.com" width="900">

### Ray researches with sources

Ray fans out web searches, synthesises, and always prints the source URLs. His prompt is tuned to refuse to bluff — if the evidence isn't there, he says so.

<img src="docs/marketing/screenshots/14-ray-research-demo.png" alt="Ray research with citations" width="900">

### Ava builds a custom agent live

Tell Ava what you need. She writes the persona, picks the tools, calls `system.agent.create`, and shows you a summary card for the new agent. It shows up in the roster immediately.

<img src="docs/marketing/screenshots/15-ava-build-agent.png" alt="Ava builds Penny the pricing analyst" width="900">

### Hand-off across agents

Ask Ray to research then hand off to Max for a screenshot. Ray researches, calls the `handoff` tool with a short brief, the session's active agent switches to Max, and Max finishes the job in the same transcript — no context loss, no copy-paste.

<img src="docs/marketing/screenshots/16-handoff-ray-to-max.png" alt="Ray hands off to Max" width="900">

---

## What's under the hood

### Multi-agent orchestration (the differentiator)

- **Core + custom agents** — 5 named core agents ship in the binary; unlimited custom agents created through Ava or the UI.
- **Hand-off** — atomic control transfer with shared transcript and budget split.
- **Sub-agents** — spawn synchronous `subagent` or background `spawn` tool calls; cloned tool registry, budget controls, status polling.
- **Task delegation** — `task_create` / `task_update` / `task_list` wired to the heartbeat service for background execution.
- **Hook system** — observers, interceptors, approvals around every tool call.
- **Joined session store** — multi-agent conversations share a single day-partitioned JSONL transcript.

### Security posture

<img src="docs/marketing/screenshots/06-max-tools-permissions.png" alt="Per-agent tool policy" width="900">

- **Landlock sandbox** on Linux 5.13+ (pure Go via `golang.org/x/sys/unix`), app-level fallback elsewhere.
- **Three-tier tool policy per agent** — `allow` / `ask` / `deny` with glob patterns and interactive approval over the WebSocket.
- **SSRF guard** on every outbound fetch / navigate — private IP ranges, cloud metadata endpoints, link-local addresses blocked.
- **Encrypted credential store** — AES-256-GCM with Argon2id KDF; master key auto-generated on first boot, rotation via CLI.
- **Prompt-injection guard**, per-channel rate limits, per-binary exec allowlists.
- **Audit log** — structured JSONL with automatic secret/PII redaction.

### Built-in tools (27 loaded by default)

Files (`read_file`, `write_file`, `edit_file`, `append_file`, `list_dir`), shell (`exec` with PTY + approval), web (`web_search`, `web_fetch`), tasks (`task_create` / `update` / `delete` / `list`), agents (`agent_list`, `subagent`, `spawn`, `spawn_status`, `handoff`, `return_to_default`), browser (`navigate`, `click`, `type`, `screenshot`, `get_text`, `wait`), skills (`find_skills`, `install_skill`), comms (`message`, `send_file`), scheduling (`cron`), and more. Additional tools register from MCP servers at runtime.

### Connectivity

<img src="docs/marketing/screenshots/10-settings.png" alt="Provider matrix" width="900">

**20+ LLM providers** compiled in — OpenRouter, Anthropic, OpenAI, Google Gemini, DeepSeek, Qwen, Moonshot, Groq, Cerebras, Mistral, MiniMax, Ollama, vLLM, Azure, GitHub Copilot, Volcengine, ModelScope, NVIDIA, Avian, LongCat, Shengsuanyun, Vivgrid, Zhipu. Fallback chains, multi-key rotation, streaming, vision.

**17 chat channels** — Web Chat, Telegram, Discord, Slack, Teams, Matrix, WhatsApp, Line, QQ, WeChat, WeCom, Weixin, IRC, Feishu, DingTalk, Google Chat, OneBot, MaixCAM. All compiled in; no external services needed.

### Operator surfaces

| | |
|---|---|
| <img src="docs/marketing/screenshots/08-command-center.png" alt="Command Center" width="420"> | **Command Center** — gateway status, agent summary, task board, activity feed, rate-limit events, approval queue. |
| <img src="docs/marketing/screenshots/07-ava-profile.png" alt="Agent profile" width="420"> | **Agent profile** — model, temperature, per-tool policy, session history, activity timeline. Identity fields read-only on core agents. |
| <img src="docs/marketing/screenshots/12-agent-picker-menu.png" alt="Agent picker" width="420"> | **Agent picker** — switch who you're talking to in one click; sessions stay with the session, not the agent. |
| <img src="docs/marketing/screenshots/01-login.png" alt="Login" width="420"> | **Dark-first UI** — "The Sovereign Deep" design system: Deep Space Black, Liquid Silver, Forge Gold accents. Chat-first, no separate canvas. |

---

## Quick start

```bash
# 1. Clone and build
git clone https://github.com/dapicom-ai/omnipus.git
cd omnipus
CGO_ENABLED=0 go build -o omnipus ./cmd/omnipus/

# 2. Run the gateway (binds 0.0.0.0:3000 by default)
./omnipus gateway

# 3. Open http://localhost:3000 and follow the onboarding wizard:
#    Welcome → Provider → API Key → Model → Admin Account → Done
```

First boot auto-generates an encryption key at `~/.omnipus/master.key` (mode `0600`). **Back it up** — losing it makes the credential store unrecoverable. For headless deployments, pre-provision via `OMNIPUS_KEY_FILE` or `OMNIPUS_MASTER_KEY`.

Rotate the key any time:

```bash
./omnipus credentials rotate --old-key-file old.key --new-key-file new.key
```

---

## Architecture

```
                    ┌────────────────────┐
                    │   Web UI (SPA)     │   React 19 · Vite 6 · shadcn/ui
                    │   embedded via     │
                    │   go:embed         │
                    └─────────┬──────────┘
                              │ HTTP · WebSocket · SSE
                    ┌─────────┴──────────┐
                    │      Gateway       │   auth, rate limits, CORS
                    └─────────┬──────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
   ┌─────┴──────┐      ┌──────┴──────┐      ┌──────┴──────┐
   │ Agent Loop │      │ Policy      │      │ Audit       │
   │ + Hooks    │◄────►│ Engine      │      │ Logger      │
   │ + Tools    │      │ allow/ask/  │      │ JSONL +     │
   │ + Handoff  │      │ deny        │      │ redaction   │
   └─────┬──────┘      └─────────────┘      └─────────────┘
         │
   ┌─────┴──────┐      ┌─────────────┐      ┌─────────────┐
   │  Channels  │      │  Sandbox    │      │ Credentials │
   │ 17 compiled│      │ Landlock +  │      │ AES-256-GCM │
   │ in Go      │      │ seccomp +   │      │ Argon2id KDF│
   └────────────┘      │ SSRF guard  │      └─────────────┘
                       └─────────────┘
```

Single binary. File-based storage (`~/.omnipus/` — JSON + JSONL, atomic writes). No Postgres. No Redis. WhatsApp uses pure-Go SQLite (`modernc.org/sqlite`) in its own session namespace.

---

## Tech stack

**Backend:** Go 1.21+ · `chromedp` (browser) · `whatsmeow` (WhatsApp) · `discordgo` · `telebot` · `slack-go` · `go-nostr` · `modernc.org/sqlite` · `golang.org/x/sys/unix` (Landlock, seccomp)

**Frontend:** TypeScript · React 19 · Vite 6 · shadcn/ui (Radix + Tailwind CSS v4) · AssistantUI · Phosphor Icons · Zustand · TanStack Query / Router · Framer Motion

**Storage:** File-based JSON / JSONL. Day-partitioned session transcripts with configurable retention (default 90 days) and two-layer context compression.

---

## Status

Pre-1.0. Three shipping variants:

1. **Omnipus Open Source** (primary, ships first) — single Go binary, embedded web UI, community focus. This repo.
2. **Omnipus Desktop** — Electron wrapper with native menus and auto-update.
3. **Omnipus Cloud / SaaS** — hosted variant with team features and managed infrastructure.

All three share the same Go core and the `@omnipus/ui` React components.

Active development on [`feature/next-wave`](https://github.com/dapicom-ai/omnipus/tree/feature/next-wave) · tracked in [PR #69](https://github.com/dapicom-ai/omnipus/pull/69).

---

## Specification

The full design is written down, not vibes:

| Document | Scope |
|---|---|
| [Main BRD](docs/BRD/Omnipus%20BRD.md) | 30 security + 36 functional requirements, delivery phases |
| [Appendix A](docs/BRD/Omnipus%20Windows%20BRD%20appendic.md) | Windows kernel security (Job Objects, Restricted Tokens, DACL) |
| [Appendix B](docs/BRD/Omnipus_BRD_AppendixB_Feature_Parity.md) | Feature parity requirements vs. the Claw ecosystem |
| [Appendix C](docs/BRD/Omnipus_BRD_AppendixC_UI_Spec.md) | Full UI / UX spec |
| [Appendix D](docs/BRD/Omnipus_BRD_AppendixD_System_Agent.md) | System agent and system tools |
| [Appendix E](docs/BRD/Omnipus_BRD_AppendixE_DataModel.md) | File-based data model and directory structure |

---

## Contributing

Issues, PRs, discussions — all welcome. Start with the BRD, then check the [project board](https://github.com/users/Dapicom/projects/1) for open work.

## License

MIT · [omnipus.ai](https://omnipus.ai)
