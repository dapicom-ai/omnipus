# ADR-003: Wave 3 + Wave 4 Cross-Cutting Architecture Review

**Status:** Proposed
**Date:** 2026-03-29
**Deciders:** architect, backend-lead, security-lead, qa-lead

## Context

Wave 3 (Skill Ecosystem & ClawHub) and Wave 4 (WhatsApp Channel & Browser Automation) introduce new integration surfaces that cross package boundaries. This ADR reviews five cross-cutting concerns:

1. Skill trust → policy engine integration (`pkg/policy/` + `pkg/skills/`)
2. Browser SSRF → security package integration (`pkg/tools/browser/` + `pkg/security/`)
3. Channel typing → `TypingCapable` interface usage across channels
4. Auto-discovery → tool registry composition (`pkg/skills/discovery.go`, `pkg/skills/mcp_bridge.go`)
5. Prompt injection guard design (`pkg/security/promptguard.go`)

BRD requirements in scope: SEC-04, SEC-07, SEC-09, SEC-24, SEC-25, FUNC-12c, FUNC-13, FUNC-19–24.

## Findings

### Severity Definitions
- **blocker** — Violates hard constraint or BRD requirement. Must resolve before proceeding.
- **warning** — Architectural risk. Should address, can defer with documented rationale.
- **note** — Observation or suggestion. Non-blocking.

---

### B-1: Skill Trust Policy Defined But Never Enforced (SEC-09 non-compliance)

**Severity:** blocker
**Component:** `pkg/policy/policy.go`, `pkg/skills/clawhub_registry.go`, `pkg/skills/installer.go`

`SkillTrustPolicy` is defined in `pkg/policy/policy.go:111-121` with three levels (`block_unverified`, `warn_unverified`, `allow_all`) and `EffectiveSkillTrust()` returns the configured value. However, **no caller ever reads this policy**.

- `ClawHubRegistry.DownloadAndInstall()` (`clawhub_registry.go:273-285`) performs hash verification only when the registry provides a hash (`meta.ExpectedHash != ""`). If the registry returns no hash, verification is silently skipped — regardless of the trust policy.
- `SkillInstaller.InstallFromGitHub()` (`installer.go:107`) performs zero hash verification. GitHub-sourced skills bypass SEC-09 entirely.
- The Wave 3 test file (`wave3_hash_trust_test.go:82-101`) confirms this with `t.Skip("TODO: skill trust policy not yet implemented")`.

The trust policy configuration exists in the policy engine, but the install pipeline has no reference to `SecurityConfig` or `EffectiveSkillTrust()`. There is no mechanism to pass the policy decision into the install flow.

**Recommendation:** The `ClawHubRegistry.DownloadAndInstall()` method (and the `SkillInstaller`) need a trust policy parameter or a policy evaluator dependency. The proposed integration contract:

```go
// Option A: Pass policy as parameter to DownloadAndInstall
type InstallOptions struct {
    TrustPolicy  policy.SkillTrustPolicy
    AuditLogger  *audit.Logger  // SEC-15: log trust decisions
}

// In DownloadAndInstall, after hash check:
if meta == nil || meta.ExpectedHash == "" {
    switch opts.TrustPolicy {
    case policy.SkillTrustBlockUnverified:
        return nil, fmt.Errorf("skill %q has no hash in registry manifest; blocked by trust policy", slug)
    case policy.SkillTrustWarnUnverified:
        slog.Warn("skill has no registry hash — installing unverified", "slug", slug)
        result.Verified = false
    case policy.SkillTrustAllowAll:
        // no-op
    }
}
```

The CLI command layer (`cmd/omnipus/internal/skills/command.go`) must load the security config and pass the effective trust policy to the installer. Similarly, any agent-callable `install_skill` tool must inject the policy.

**BRD Ref:** SEC-09, FUNC-12c

---

### B-2: Auto-Discovery Produces Tools But No Composition Point Exists

**Severity:** blocker
**Component:** `pkg/skills/discovery.go`, `pkg/skills/mcp_bridge.go`

Two discovery sources exist:
- `DiscoverAllTools(loader)` scans SKILL.md `allowed-tools` frontmatter → returns `[]DiscoveredTool`
- `MCPBridge.DiscoverMCPTools()` queries MCP servers → returns `[]DiscoveredTool`

Both files correctly note that "callers must run results through the policy engine before making tools available to agents (SEC-04, SEC-07)." However, **there is no composition function** that:

1. Merges `DiscoverAllTools` + `DiscoverMCPTools` results
2. Deduplicates by tool name (what happens when a skill and an MCP server both declare `web_search`?)
3. Applies `policy.Evaluator.EvaluateTool()` to filter the merged set
4. Registers approved tools into `tools.ToolRegistry`
5. Handles runtime re-discovery (skill install/remove, MCP server connect/disconnect)

Without this composition layer, the discovery pipeline is disconnected from the agent loop. Tools are discovered but never registered.

**Recommendation:** Create a `pkg/skills/compositor.go` (or similar) that orchestrates the full pipeline:

```go
// ToolCompositor merges discovered tools from all sources and applies policy.
type ToolCompositor struct {
    loader    *SkillsLoader
    mcpBridge *MCPBridge
    evaluator *policy.Evaluator
    registry  *tools.ToolRegistry
}

// ComposeForAgent discovers all tools, applies per-agent policy, and returns
// the approved tool names. Does NOT register — the caller decides what to do.
func (c *ToolCompositor) ComposeForAgent(agentID string) (approved, denied []DiscoveredTool)
```

The agent loop startup should call this compositor. Runtime re-discovery (on skill install/remove) should trigger re-composition. The deduplication strategy must be defined: skill-declared tools vs MCP tools — which wins? Recommendation: MCP tools take precedence (they represent live servers) unless the skill tool is explicitly in the agent's `tools.allow` list.

**BRD Ref:** FUNC-13, SEC-04, SEC-07

---

### W-1: Browser Tools Acquire a New Tab Per Operation — No Session Continuity

**Severity:** warning
**Component:** `pkg/tools/browser/tools.go`

Every browser tool (`click`, `type`, `get_text`, `wait`, `evaluate`) calls `t.mgr.AcquireTab()` which creates a **new** chromedp context. This means:
- `browser.navigate` opens tab 1, navigates to a page, then the tab is closed by `defer cancel()`
- `browser.click` opens tab 2 (a blank page) — cannot interact with the page from step 1
- The agent cannot perform multi-step browser workflows (navigate → click → get_text)

This is a fundamental usability issue. The Wave 4 spec (US-5) describes browser tools as a coherent interaction set, not isolated operations.

**Recommendation:** Introduce a session concept — a persistent tab context that outlives individual tool calls. Options:
- **Option A (simple):** `BrowserManager` maintains a "current tab" context that persists until explicitly closed or timed out. Tools operate on this current context.
- **Option B (multi-session):** `AcquireTab` returns a session ID. Tools accept an optional `session_id` parameter. The manager holds sessions in a map with idle timeout eviction.

Option A is simpler and matches the spec's single-session model. Option B is future-proofing that may not be needed now. Recommend Option A for Wave 4.

**BRD Ref:** FUNC-19, FUNC-20 (browser tool usability)

---

### W-2: ClawHub HTTP Client Bypasses SSRF Protection

**Severity:** warning
**Component:** `pkg/skills/clawhub_registry.go:78-86`

`ClawHubRegistry` creates its own `http.Client` with a default `http.Transport` (`clawhub_registry.go:78-86`). This client does **not** use `security.SSRFChecker.SafeTransport()`. While ClawHub's `baseURL` defaults to `https://clawhub.ai` (a public endpoint), the `baseURL` is configurable by the operator.

If an operator sets `baseURL` to an internal IP (e.g., for a self-hosted ClawHub mirror), the registry client would happily connect to private networks without SSRF checks. More critically, the download URL is constructed from the registry's response — a compromised or malicious registry could redirect downloads to internal endpoints.

The same concern applies to `SkillInstaller` (`installer.go:44-55`) which creates an HTTP client via `utils.CreateHTTPClient()` — also not SSRF-protected.

**Recommendation:** Both `ClawHubRegistry` and `SkillInstaller` should accept an `*http.Client` from the caller (dependency injection), allowing the top-level wiring to provide an SSRF-safe client. The browser tools already demonstrate this pattern correctly via `BrowserManager.ValidateURL()`.

```go
// At wiring time:
ssrfChecker := security.NewSSRFChecker(cfg.Security.SSRF.AllowInternal)
safeClient := ssrfChecker.SafeClient()

// Pass to ClawHub:
registry := skills.NewClawHubRegistry(cfg, skills.WithHTTPClient(safeClient))
```

**BRD Ref:** SEC-24 (SSRF protection applies to all outbound HTTP)

---

### W-3: PromptGuard Strictness Not Configurable via Policy

**Severity:** warning
**Component:** `pkg/security/promptguard.go`, `pkg/policy/policy.go`

`PromptGuard` accepts a `Strictness` at construction (`NewPromptGuard(s)`), but `SecurityConfig` has no field for prompt guard strictness. The BRD (SEC-25) says "configurable strictness levels." The UI spec (Appendix C) says "Strictness level selector (Low / Medium / High)."

There is no configuration path from `config.json` → `SecurityConfig` → `PromptGuard`. The guard must be wired to a config key (e.g., `security.prompt_guard.strictness`) so operators can configure it without code changes.

**Recommendation:** Add to `SecurityConfig`:

```go
type PromptGuardPolicy struct {
    Strictness string `json:"strictness,omitempty"` // "low", "medium", "high"
}
```

And validate in `validateConfig()`. The default should be `"medium"` per the spec.

**BRD Ref:** SEC-25, Appendix C §Security Settings

---

### W-4: Browser `NavigateTool` Does Not SSRF-Check Post-Redirect Final URL

**Severity:** warning
**Component:** `pkg/tools/browser/tools.go:55-68`

`NavigateTool.Execute()` validates the initial URL via `ValidateURL()` (which uses SSRF checker). However, after `chromedp.Navigate(rawURL)`, it reads the final URL via `chromedp.Location(&finalURL)` but **does not validate the final URL**. If the initial URL was a public redirect service (e.g., `https://example.com/redirect?to=http://169.254.169.254/latest/meta-data/`), the browser would follow the redirect to the cloud metadata endpoint.

The `SSRFChecker.SafeTransport()` handles redirects at the HTTP transport level, but chromedp uses its own Chromium networking stack — it does not go through Go's `http.Transport`. The SSRF check in `ValidateURL` only validates the URL string before passing it to Chrome; Chrome's internal redirect handling is uncontrolled.

**Recommendation:** After navigation completes, validate the final URL:

```go
// After chromedp.Navigate + chromedp.Location
if finalURL != rawURL {
    if err := t.mgr.ValidateURL(ctx, finalURL); err != nil {
        // Navigation succeeded but landed on a blocked URL via redirect
        // Close the tab and return error
        return tools.ErrorResult(fmt.Sprintf("browser.navigate: redirect landed on blocked URL: %s", err))
    }
}
```

This is defense-in-depth. The primary SSRF protection is the pre-navigation check, but server-side redirects within Chrome's networking are not covered by Go's `SafeTransport()`.

**BRD Ref:** SEC-24 (DNS rebinding protection extends to redirect-based attacks)

---

### N-1: TypingCapable Pattern Is Well-Implemented Across Channels

**Severity:** note
**Component:** `pkg/channels/interfaces.go`, all channel implementations

The `TypingCapable` interface is cleanly designed:
- Defined in `interfaces.go:13` with an idempotent `stop` function contract
- `BaseChannel.HandleMessage` (`base.go:284`) auto-detects via type assertion and triggers typing
- `PlaceholderRecorder` manages the stop lifecycle
- Implemented by: WhatsApp (real composing presence with ticker refresh), Slack (graceful no-op — correct since Slack API doesn't expose bot typing), Discord, Telegram, LINE, Matrix, QQ, WeChat, Pico, IRC

The WhatsApp implementation (`whatsapp_native.go:560-610`) is particularly well done — it refreshes the composing presence every 10s with a 5-minute max duration, and sends a `ChatPresencePaused` on stop.

The test (`wave4_typing_registry_test.go`) correctly verifies that `BaseChannel` itself does NOT implement `TypingCapable`, ensuring the type assertion path works correctly.

No issues found. This is a good model for future optional capability interfaces.

**BRD Ref:** FUNC-24 (WhatsApp typing indicator), Appendix B

---

### N-2: PromptGuard Escape Strategy May Be Fragile

**Severity:** note
**Component:** `pkg/security/promptguard.go:102-133`

The `escapeInjectionPhrases` method inserts a zero-width non-joiner (U+200C) after the first character of matched phrases. This approach:
- **Works** for LLMs that tokenize based on the raw string (the ZWNJ disrupts token boundaries)
- **May not work** for LLMs with aggressive Unicode normalization or byte-pair encoding that collapses ZWNJ
- **Is not foolish-proof** against obfuscation (mixed case works, but Unicode homoglyphs, base64 encoding, or multi-message spanning can bypass)

The BRD (SEC-25) positions this as a defense-in-depth measure, not a guarantee. The three-tier strictness model (tag → escape → replace) is a reasonable layered approach. The `[UNTRUSTED_CONTENT]` tagging at all levels is the primary defense (gives the LLM context), with pattern escaping as secondary hardening.

**Recommendation (non-blocking):** Consider augmenting Medium strictness with:
1. Normalize Unicode confusables before pattern matching (e.g., fullwidth characters)
2. Strip zero-width characters from input before escaping (attackers can pre-insert ZWNJ to prevent your ZWNJ from being effective)

These are incremental improvements — the current implementation satisfies the BRD's "configurable strictness levels" requirement.

**BRD Ref:** SEC-25

---

### N-3: MCP Bridge Has No Server Authentication or Trust Boundary

**Severity:** note
**Component:** `pkg/skills/mcp_bridge.go`

`MCPBridge.DiscoverMCPTools()` trusts all tools from all connected MCP servers equally. The `Source` field is set to `"mcp:<serverName>"` but there is no trust differentiation between:
- Local MCP servers (trusted, running on the same machine)
- Remote MCP servers (potentially untrusted, network-connected)

The tool name from an MCP server could shadow a builtin tool (e.g., an MCP server declares `system.exec` which collides with the system agent's exclusive tool namespace).

**Recommendation (non-blocking):** When composing tools (B-2 recommendation), enforce that:
1. MCP-discovered tools cannot use the `system.*` namespace (reserved for `omnipus-system` per Appendix D)
2. MCP tools from remote servers require explicit operator opt-in (trust-on-first-use or pre-configured allowlist)

This can be deferred to Wave 5 if MCP remote servers are not yet supported.

**BRD Ref:** Appendix D (system tools are exclusive to omnipus-system), SEC-04

---

## Integration Risks

### Risk 1: No End-to-End Skill Install → Discovery → Policy → Agent Loop Path

The biggest architectural gap across Wave 3+4 is that no integration path exists from skill installation to agent tool availability. The pieces exist:
- Install: `ClawHubRegistry.DownloadAndInstall` + `SkillInstaller`
- Discovery: `DiscoverAllTools` + `MCPBridge.DiscoverMCPTools`
- Policy: `Evaluator.EvaluateTool`
- Registration: `tools.ToolRegistry.Register`

But they are not wired together. An installed skill's tools are discoverable but never registered. A registered tool is not policy-checked at discovery time. This is the highest-priority integration work for Wave 3.

### Risk 2: Browser and Skill HTTP Clients Have Different SSRF Postures

The browser manager receives an `*security.SSRFChecker` at construction and validates every URL. The skill installer and ClawHub registry create their own HTTP clients without SSRF checks. This inconsistency means the SSRF protection boundary has gaps — an attacker who can influence the ClawHub `baseURL` or a redirect target during skill download can reach internal networks.

### Risk 3: PromptGuard Not Integrated Into Any Data Path

`PromptGuard` exists as a standalone utility in `pkg/security/` but is not called anywhere in the codebase. For SEC-25 to be satisfied, it must be wired into:
- Tool result processing (web_fetch, file_read, browser.get_text return untrusted content)
- Skill output processing (SKILL.md from unverified sources)
- Channel inbound messages (content from external platforms)

The integration point is the agent loop's tool result handler — but this is a Wave 5 concern (agent loop implementation). Flag for tracking.

## Verdict

**REVISE** — Two blockers (B-1: trust policy never enforced, B-2: no tool composition layer) must be resolved before Wave 3 can be considered complete. Four warnings (W-1 through W-4) should be addressed in the current wave or documented as known limitations. Notes are non-blocking improvements.

### Priority Order

1. **B-2** (tool compositor) — Foundational. Without this, auto-discovery is inert code. Enables B-1 integration.
2. **B-1** (trust policy enforcement) — Wire `EffectiveSkillTrust()` into install pipeline via the compositor or directly.
3. **W-1** (browser session continuity) — Without this, browser tools are unusable for multi-step workflows.
4. **W-2** (SSRF on skill HTTP clients) — Security hardening with clear attack path.
5. **W-3** (PromptGuard config) — Config plumbing, lower risk.
6. **W-4** (post-redirect SSRF check) — Defense-in-depth, medium effort.

## Affected Components

- **Backend:** `pkg/skills/` (compositor, installer policy integration), `pkg/tools/browser/` (session model, post-redirect check), `pkg/security/promptguard.go` (config integration), `pkg/policy/policy.go` (PromptGuard config type)
- **Frontend:** None directly — but PromptGuard strictness config (W-3) will need a UI selector in Wave 5a
- **Variants:** All findings apply to all three deployment variants (Open Source, Desktop, SaaS)
