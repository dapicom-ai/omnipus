<div align="center">
<img src="IMG_0432.svg" alt="Omnipus" width="400">

<h1>Omnipus</h1>

<h3>Elite Simplicity. Sovereign Control.</h3>

<p>Enterprise-hardened AI agent runtime. Single Go binary, kernel-level sandboxing, 16+ channels, runs on $10 hardware.</p>

<p>
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat&logo=react&logoColor=white" alt="React">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="License">
  <br>
  <a href="https://omnipus.ai"><img src="https://img.shields.io/badge/Website-omnipus.ai-D4AF37?style=flat&logo=google-chrome&logoColor=white" alt="Website"></a>
</p>

</div>

---

## What is Omnipus?

Omnipus is a sovereign AI agent runtime — a single Go binary with kernel-level sandboxing, multi-channel communication, a polished React web UI, and the ability to run on minimal hardware. It delivers enterprise-grade security, multi-agent orchestration, and a complete task management system out of the box.

### Features

**Agent Runtime**
- Multi-agent architecture with system, core, and custom agents
- Agent creation with avatar customization, model selection, and temperature tuning
- Task delegation from Command Center with real-time status tracking
- Sub-agent spawning (sync and async) with status tracking
- Session management with day-partitioned JSONL transcripts
- JSONL memory store with configurable retention (default 90 days)
- Heartbeat / proactive agent scheduling (HEARTBEAT.md)
- Cron / scheduled tasks with full lifecycle management

**Multi-Provider LLM Support**
- 15 providers: OpenAI, Anthropic, OpenRouter, Google Gemini, Groq, DeepSeek, Mistral, Azure OpenAI, Zhipu, Moonshot, NVIDIA, MiniMax, Qwen, Ollama, Cerebras
- Smart model routing with fallback chains and multi-key rotation
- Vision/multimodal input support
- Streaming responses with real-time token delivery

**Communication Channels**
- 16 compiled-in Go channels: Web Chat, Telegram, Discord, Slack, WhatsApp, Feishu/Lark, DingTalk, WeCom, Weixin, LINE, QQ, OneBot, IRC, Matrix, MaixCam
- Bridge protocol for non-Go channels (JSON over stdin/stdout)
- Per-channel configuration with test connection, allow-from restrictions, and proxy support

**Tools (20 built-in)**
- File operations: `read_file`, `write_file`, `edit_file`, `append_file`, `list_dir`
- Shell: `exec` with background sessions, PTY support, poll/read/write/kill
- Web: `web_fetch`, `web_search` (DuckDuckGo)
- Tasks: `task_create`, `task_update`, `task_delete`, `task_list`
- Agents: `agent_list`, `subagent` (sync), `spawn` (async)
- Skills: `find_skills` (ClawHub search), `install_skill`
- Communication: `message`, `send_file`
- Scheduling: `cron`
- Browser automation: 7 chromedp tools (navigate, click, type, screenshot, get_text, wait, evaluate)

**Security**
- Kernel-level sandboxing (Landlock filesystem, seccomp syscall filtering) with graceful fallback
- Policy engine: deny-by-default, per-agent tool allow/deny lists
- RBAC with admin/user roles and per-user token authentication
- Credential encryption at rest (AES-256-GCM, Argon2id KDF)
- SSRF protection for all outbound HTTP (private IP, cloud metadata blocking)
- Prompt injection defense (configurable Low/Medium/High detection)
- Rate limiting (per-agent LLM calls/hour, tool calls/minute, global daily cost cap)
- Structured audit logging with automatic API key/PII redaction
- Exec approval system with interactive Allow/Deny/Always Allow
- Per-binary execution control with glob pattern allowlists
- Security diagnostics (`omnipus doctor`) with risk scoring
- Skill trust verification (SHA-256 hash against ClawHub manifest)

**Skill Ecosystem**
- ClawHub registry integration (search, install, verify)
- SKILL.md format for custom skills
- MCP protocol support (Model Context Protocol) with stdio/SSE/WebSocket transports
- Skill auto-discovery from installed directories

**Web UI — "The Sovereign Deep"**
- Dark-first design with Deep Space Black, Liquid Silver, Forge Gold palette
- Chat-first interface with AssistantUI integration
- Real-time streaming with WebSocket delivery
- Tool call visualization with collapsible detail
- Session panel with search, rename, delete
- Command Center: gateway status, agent summary, task board with list/board views, activity feed
- Agent management: create/view/configure with avatar customization
- Skills & Tools: 4-tab manager (Installed Skills, MCP Servers, Channels, Built-in Tools)
- Settings: 9-tab configuration hub (Providers, Security, Gateway, Data, Routing, Profile, Devices, Policy Approvals, About)
- Guided onboarding wizard (4 steps: Welcome → Provider → Admin → Done)
- Responsive design (desktop, tablet, mobile)
- Branded 404 page ("This page drifted into the deep.")

**Operations**
- Single binary deployment with embedded web UI (go:embed)
- Atomic config writes (temp file + rename)
- Hot-reload for non-security configuration
- Graceful shutdown with partial response preservation
- Backup and restore support
- Device pairing with admin approval workflow

## Architecture

```
                    +------------------+
                    |   Web UI (SPA)   |   React 19 + Vite 6 + shadcn/ui
                    +--------+---------+
                             |
                    +--------+---------+
                    |     Gateway      |   HTTP / WebSocket / SSE
                    +--------+---------+
                             |
              +--------------+--------------+
              |              |              |
     +--------+--+  +-------+---+  +-------+---+
     | Agent Loop |  | Policy    |  | Audit     |
     | + Tools    |  | Engine    |  | Logger    |
     +--------+---+  +-----------+  +-----------+
              |
     +--------+---+
     |  Channels  |   16 compiled-in Go channels
     +------------+   + bridge protocol for external
```

## Quick Start

```bash
# Build
CGO_ENABLED=0 go build -o omnipus ./cmd/omnipus/

# Run (opens on localhost:3000)
./omnipus gateway

# First launch opens the onboarding wizard:
# 1. Select provider (OpenRouter, OpenAI, Anthropic, ...)
# 2. Enter API key → Connect & Load Models → Select model
# 3. Create admin account
# 4. Start chatting
```

## Tech Stack

**Backend:** Go 1.26+, `golang.org/x/sys/unix` (Landlock, seccomp), `chromedp` (browser), `whatsmeow` (WhatsApp), `discordgo`, `telebot`, `slack-go`, `modernc.org/sqlite` (pure Go SQLite for WhatsApp sessions)

**Frontend:** TypeScript, React 19, Vite 6, shadcn/ui (Radix + Tailwind CSS v4), Zustand, TanStack Query + Router, AssistantUI, Phosphor Icons, Framer Motion

**Storage:** File-based (JSON/JSONL). No PostgreSQL, no Redis. Data directory: `~/.omnipus/`

**Brand:** "The Sovereign Deep" — Deep Space Black (`#0A0A0B`), Liquid Silver (`#E2E8F0`), Forge Gold (`#D4AF37`). Dark-first. Octopus mascot ("Master Tasker").

## Specification

| Document | Contents |
|---|---|
| [Main BRD](docs/BRD/Omnipus%20BRD.md) | 30 security + 36 functional requirements |
| [Appendix A](docs/BRD/Omnipus%20Windows%20BRD%20appendic.md) | Windows kernel security |
| [Appendix B](docs/BRD/Omnipus_BRD_AppendixB_Feature_Parity.md) | Feature parity requirements |
| [Appendix C](docs/BRD/Omnipus_BRD_AppendixC_UI_Spec.md) | Full UI/UX spec |
| [Appendix D](docs/BRD/Omnipus_BRD_AppendixD_System_Agent.md) | System agent (35 tools, 3 core agents) |
| [Appendix E](docs/BRD/Omnipus_BRD_AppendixE_DataModel.md) | File-based data model |

## Domain

[omnipus.ai](https://omnipus.ai)

## License

MIT

## Contributing

We welcome contributions! Review the BRD documents, check the [project board](https://github.com/users/Dapicom/projects/1) for open issues, and submit PRs.
