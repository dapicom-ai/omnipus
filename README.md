# Omnipus

**Enterprise-hardened AI agent runtime.** Single Go binary, kernel-level sandboxing, 20+ channels, runs on $10 hardware.

> **Status: Pre-release.** The specification is complete. Implementation has not started. Star and watch to follow progress.

## What is Omnipus?

Omnipus is an agentic core built on [PicoClaw](https://github.com/sipeed/picoclaw)'s foundation. It adds kernel-level sandboxing (Landlock, seccomp), RBAC, audit logging, credential management, a polished React UI, browser automation, and expanded channel/skill coverage — while preserving PicoClaw's identity: single binary, zero dependencies, sub-second startup, minimal RAM.

### Key Differentiators

- **vs PicoClaw**: Real security (kernel sandboxing, encrypted credentials, audit logs), polished web UI, native WhatsApp, browser automation, ClawHub skill ecosystem
- **vs OpenClaw**: 10x lighter (20MB vs 180MB+), single Go binary vs Node.js, runs on $10 hardware, same feature depth
- **vs NemoClaw**: 100x lighter (no Docker, no K3s, no 8GB RAM requirement), comparable security model

## Architecture

```
                    +------------------+
                    |   Web UI (SPA)   |   React 19, embedded via go:embed
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
     +------------+   (compiled-in Go + bridge protocol for non-Go)
```

**Agentic Core:** Single Go binary. All Go channels compiled in. Security subsystem (Landlock, seccomp, RBAC, credential encryption). File-based data model (JSON/JSONL).

**Channels:** Hybrid model — Go channels in-process via internal MessageBus, non-Go channels (Signal/Java, Teams/Node.js) and community channels via bridge protocol (JSON over stdin/stdout).

**UI:** `@omnipus/ui` React component library. Supports web (go:embed), Electron desktop, and hosted deployment.

## Specification

The complete specification lives in `docs/BRD/`:

| Document | Contents |
|---|---|
| [Main BRD](docs/BRD/Omnipus%20BRD.md) | 30 security + 36 functional requirements, 3 delivery phases |
| [Appendix A](docs/BRD/Omnipus%20Windows%20BRD%20appendic.md) | Windows kernel security (Job Objects, Restricted Tokens, DACL) |
| [Appendix B](docs/BRD/Omnipus_BRD_AppendixB_Feature_Parity.md) | Feature parity requirements (ClawHub, browser, WhatsApp, channels) |
| [Appendix C](docs/BRD/Omnipus_BRD_AppendixC_UI_Spec.md) | Full UI/UX spec (React 19, Vite 6, shadcn/ui, Phosphor Icons) |
| [Appendix D](docs/BRD/Omnipus_BRD_AppendixD_System_Agent.md) | System agent with 35 system tools, 3 core agents |
| [Appendix E](docs/BRD/Omnipus_BRD_AppendixE_DataModel.md) | File-based data model (JSON/JSONL), directory structure, entity schemas |
| [Competitive Analysis](docs/BRD/OpenClaw_vs_PicoClaw_Comparison.md) | OpenClaw vs PicoClaw feature and UX comparison |

## Tech Stack

**Backend:** Go 1.21+, `golang.org/x/sys/unix` (Landlock, seccomp), `chromedp` (browser), `whatsmeow` (WhatsApp), `discordgo`, `telebot`, `modernc.org/sqlite` (pure Go SQLite)

**Frontend:** TypeScript, React 19, Vite 6, shadcn/ui, Zustand, TanStack Query, Phosphor Icons

**Storage:** File-based (JSON/JSONL). No PostgreSQL, no Redis. Data directory: `~/.omnipus/`

## Domain

[omnipus.ai](https://omnipus.ai)

## License

TBD

## Contributing

We welcome contributions! The project is in the specification phase. The best way to contribute right now is to review the BRD documents and open issues for gaps, questions, or suggestions.
