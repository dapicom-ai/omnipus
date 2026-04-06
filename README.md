<div align="center">
<img src="IMG_0432.svg" alt="Omnipus" width="400">

<h1>Omnipus</h1>

<h3>Elite Simplicity. Sovereign Control.</h3>

<p>Enterprise-hardened AI agent runtime. Single Go binary, kernel-level sandboxing, 20+ channels, runs on $10 hardware.</p>

<p>
  <img src="https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat&logo=react&logoColor=white" alt="React">
  <img src="https://img.shields.io/badge/license-TBD-yellow" alt="License">
  <br>
  <a href="https://omnipus.ai"><img src="https://img.shields.io/badge/Website-omnipus.ai-D4AF37?style=flat&logo=google-chrome&logoColor=white" alt="Website"></a>
</p>

</div>

---

> **Status: Active Development.** Inspired by the open-source agent ecosystem — [OpenClaw](https://github.com/openclaw/openclaw), [NemoClaw](https://github.com/nemoclaw/nemoclaw), and [PicoClaw](https://github.com/sipeed/picoclaw) — Omnipus is a ground-up reimplementation with enterprise security, a polished UI, and the "Sovereign Deep" design system. Star and watch to follow progress.

## What is Omnipus?

Omnipus is a sovereign AI agent runtime — a single Go binary that ships with kernel-level sandboxing, 20+ communication channels, a polished React UI, and runs on $10 hardware. Originally inspired by the open-source agent ecosystem (OpenClaw, NemoClaw, PicoClaw), Omnipus has been substantially rewritten to deliver enterprise-grade security, reliability, and developer experience.

### Key Differentiators

- **vs OpenClaw**: 10x lighter (34MB vs 180MB+), single Go binary vs Node.js, runs on $10 hardware, same feature depth
- **vs NemoClaw**: 100x lighter (no Docker, no K3s, no 8GB RAM requirement), comparable security model
- **vs PicoClaw**: Real security (kernel sandboxing, encrypted credentials, audit logs), polished web UI, rewritten agent loop with streaming, tool call visualization, and response reliability

### Features

- 20+ channels: Telegram, Discord, Slack, WhatsApp, WeChat, DingTalk, LINE, Matrix, IRC, QQ, and more
- MCP protocol support (Model Context Protocol)
- Smart model routing with FallbackChain and multi-key rotation
- Vision/multimodal input
- Heartbeat / proactive agent (HEARTBEAT.md)
- Cron / scheduled tasks
- Sub-agent spawning with status tracking
- JSONL memory store with day-partitioned sessions
- Hardware I/O (I2C/SPI) for IoT
- Kernel-level sandboxing (Landlock filesystem, seccomp syscall filtering)
- Policy engine (deny-by-default, per-agent tool allow/deny)
- Structured audit logging with redaction and explainable decisions
- Credential encryption at rest (AES-256-GCM, Argon2id KDF)
- Rate limiting (per-agent, per-channel, global cost cap)
- SSRF protection for all outbound HTTP
- Browser automation (chromedp)
- ClawHub skill ecosystem compatibility (13K+ skills)
- Polished React 19 web UI ("The Sovereign Deep" design system)
- Real-time tool call visualization with WebSocket streaming
- Interactive exec approval with "Always Allow" persistence
- System agent for conversational configuration
- Graceful shutdown with partial response preservation

## Architecture

```
                    +------------------+
                    |   Web UI (SPA)   |   React 19 "Sovereign Deep"
                    +--------+---------+
                             |
                    +--------+---------+
                    |     Gateway      |   HTTP/WebSocket server
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
     |  Channels  |   Telegram, Discord, WhatsApp, Slack, ...
     +------------+   (compiled-in Go + bridge for non-Go)
```

## Tech Stack

**Backend:** Go 1.25+, `golang.org/x/sys/unix` (Landlock, seccomp), `chromedp` (browser), `whatsmeow` (WhatsApp), `discordgo`, `telebot`, `slack-go`, `modernc.org/sqlite` (pure Go SQLite)

**Frontend:** TypeScript, React 19, Vite 6, shadcn/ui, Zustand, TanStack Query, Phosphor Icons, Framer Motion

**Storage:** File-based (JSON/JSONL). No PostgreSQL, no Redis. Data directory: `~/.omnipus/`

**Brand:** "The Sovereign Deep" — Deep Space Black, Liquid Silver, Forge Gold. Dark-first. Octopus mascot.

## Specification

| Document | Contents |
|---|---|
| [Main BRD](docs/BRD/Omnipus%20BRD.md) | 30 security + 36 functional requirements |
| [Appendix A](docs/BRD/Omnipus%20Windows%20BRD%20appendic.md) | Windows kernel security |
| [Appendix B](docs/BRD/Omnipus_BRD_AppendixB_Feature_Parity.md) | Feature parity (ClawHub, browser, WhatsApp) |
| [Appendix C](docs/BRD/Omnipus_BRD_AppendixC_UI_Spec.md) | Full UI/UX spec |
| [Appendix D](docs/BRD/Omnipus_BRD_AppendixD_System_Agent.md) | System agent (35 tools, 3 core agents) |
| [Appendix E](docs/BRD/Omnipus_BRD_AppendixE_DataModel.md) | File-based data model |
| [Wave 0 Spec](docs/plan/wave0-brand-design-spec.md) | Brand & design foundation |
| [Wave 1 Spec](docs/plan/wave1-core-foundation-spec.md) | Core foundation |
| [Wave 2 Spec](docs/plan/wave2-security-layer-spec.md) | Security layer |

## Domain

[omnipus.ai](https://omnipus.ai)

## Credits

Inspired by the open-source AI agent ecosystem:
- [OpenClaw](https://github.com/openclaw/openclaw) — agent loop architecture and streaming patterns
- [NemoClaw](https://github.com/nemoclaw/nemoclaw) — security model and container isolation concepts
- [PicoClaw](https://github.com/sipeed/picoclaw) by [Sipeed](https://sipeed.com) — original lightweight Go runtime and channel integrations

Omnipus has been substantially rewritten from these foundations with a new agent loop, streaming pipeline, security layer, and UI.

## License

MIT

## Contributing

We welcome contributions! Review the BRD documents and wave specs, then open issues for gaps, questions, or suggestions.
