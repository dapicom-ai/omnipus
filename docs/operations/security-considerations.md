# Security Considerations — Operator Guide

## Two-port origin isolation

The gateway runs two listeners so that the SPA and agent-served content occupy different browser origins. An `<iframe>` loading `https://preview.omnipus.example.com` cannot read cookies, `localStorage`, or in-memory state from `https://omnipus.example.com` because the browser enforces the same-origin policy across hostnames. Without this separation, an agent that writes and immediately serves a malicious HTML file would inherit the SPA's origin and could make authenticated requests to the admin API.

See [Threat Model in chat-served-iframe-preview-spec.md](../specs/chat-served-iframe-preview-spec.md#threat-model) for the full threat enumeration. T-01 through T-10 cover the iframe-preview attack surface in detail, including token leakage (T-02), cross-origin escalation (T-03), content injection into the SPA (T-04), and exfiltration via embedded resources (T-08).

---

## Bearer-token contract for `/preview/`

Preview URLs contain a time-limited bearer token embedded in the path. Anyone who has the URL can load the content until the token expires — the gateway does not require a logged-in session to serve preview responses. Both static-served directories (formerly `serve_workspace`) and dev-server processes (formerly `run_in_workspace`) are now reachable through the unified `/preview/<agent>/<token>/` route.

Operators who want tighter access control have two levers:

1. **Shorten the token lifetime** — lower `tools.web_serve.max_duration_seconds` from the default `86400` (24 hours) to a value appropriate for the deployment, for example `3600` (1 hour). Tokens issued after the change use the new duration; existing tokens are not revoked.

2. **Treat preview URLs as secrets** — avoid sharing a preview URL outside the trusted user who triggered the agent turn that generated it. The URL itself is the credential for that preview.

There is no per-token revocation endpoint in the current release. To invalidate all outstanding tokens, restart the gateway (tokens are stored in memory and are not persisted).

---

## Kernel-enforced bind-port allow-list

On Linux kernels with Landlock ABI v4+ (kernel 6.7 and later), the gateway and every child process it spawns are restricted to binding TCP ports inside `cfg.Sandbox.DevServerPortRange` (default `[18000, 18999]`) plus the gateway's own listener ports. Any `bind(2)` to a port outside that allow-list returns `EACCES` from the kernel — including `bind(0.0.0.0:5173)` from a shell-spawned dev server.

This means an agent calling `exec npx vite --host 0.0.0.0 --port 5173` will fail at the bind syscall, regardless of the agent's tool policy. The only legal way for an agent to expose a website is through the `web_serve` tool, which auto-picks a port from the allow-listed range and routes traffic through `/preview/<agent>/<token>/` with the bearer-token contract above.

**Graceful degradation:** on kernels with Landlock ABI < 4 (no `NET_BIND_TCP`), this enforcement is silently inactive. Operators on such kernels still get tool-layer port-range validation (the `web_serve` tool refuses out-of-range ports), but a shell-spawned process can technically bind anywhere. Plan to upgrade to kernel 6.7+ for the full enforcement story.

Connect-side rules (`NET_CONNECT_TCP`) are similarly enforced for the same range plus the gateway's listener ports, so the reverse proxy can still reach back into agent dev servers without leaking unrestricted outbound TCP.

---

## `gateway.public_url` is required for strict embedding control

The CSP `frame-ancestors` directive controls which origins are permitted to embed the SPA in an `<iframe>`. The gateway derives this value from `gateway.public_url`.

When `gateway.host` is `0.0.0.0` and `gateway.public_url` is unset, the gateway cannot determine the canonical origin and falls back to `frame-ancestors '*'`, which allows any site to embed the SPA. This is acceptable in a trusted local network but is not recommended for internet-facing deployments.

To lock down embedding, set `public_url` in `~/.omnipus/config.json`:

```json
{
  "gateway": {
    "public_url": "https://omnipus.example.com",
    "preview_origin": "https://preview.omnipus.example.com"
  }
}
```

The gateway will then emit `frame-ancestors https://omnipus.example.com` and allow only the declared preview origin to embed preview iframes.

---

## Master key backup

The credential store (`~/.omnipus/credentials.json`) is encrypted with a 256-bit key. Losing that key makes every stored secret — API keys, channel tokens, webhook credentials — permanently inaccessible.

Key provisioning priority, rotation procedure, and the auto-generate first-boot behavior are documented in [ADR-004](../architecture/ADR-004-credential-boot-contract.md#master-key-provisioning). Follow the key rotation steps there before decommissioning a server or moving the data directory.

---

## Run by trusted users only

The gateway executes tool calls on behalf of the active agent and the user directing it. A user with chat access can instruct agents to read files, run shell commands (subject to tool policy), and make outbound HTTP requests. This is by design — the product is an agentic runtime.

Operators should extend chat access only to users they trust with shell-level capabilities on the host. T-06 in the [Threat Model](../specs/chat-served-iframe-preview-spec.md#threat-model) covers the trusted-prompt boundary and what happens when an agent receives instructions from untrusted content (for example, an HTML file fetched from the web).

---

## `tools.exec.allow_remote` removed

The `allow_remote` field on `ExecConfig` was a legacy GHSA-pv8c-p6jf-3fpp channel block that prevented the exec tool from running when a message arrived via a remote channel (Telegram, Discord, Slack, etc.). This field has been removed. Exec access is now governed entirely by per-agent `ToolPolicyCfg` (allow/ask/deny) and `sandbox_profile`. Agents that must not use exec on remote channels must have `exec: deny` in their tool policy. A boot-time WARN is emitted listing any agents with remote channels enabled and a non-deny exec policy.
