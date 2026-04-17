# SPA Gaps — Playwright Test Requirements

This file tracks features referenced by E2E tests that are not yet implemented in the SPA
(`src/`). Each item maps to one or more `test.fixme` calls in the spec files.

---

## Missing Features

- [ ] **Dev-mode banner** (`auth.spec.ts (c)`)
  When `gateway.dev_mode_bypass = true` the SPA does not render a persistent red banner.
  AppShell only shows a `connectionError` banner — not a dev-mode warning.

- [ ] **Version-drift toast + `/api/v1/version` polling** (`version-drift.spec.ts`)
  The SPA does not poll `/api/v1/version` on window focus and does not display a
  "New version available" toast when the build hash changes.

- [ ] **Core-agent locked-field indicator** (`agents.spec.ts (d)`)
  AgentProfile hides the Identity accordion for locked agents (`canEdit` guard at
  `AgentProfile.tsx:353`) — the name input is never rendered, so there is nothing to
  assert as readOnly. The "read-only" badge exists but is insufficient for the test intent.

- [ ] **"Agent removed" banner on deleted-agent session** (`agents.spec.ts (g)`)
  ChatScreen does not check `agent_removed` in the session response and does not render
  a `data-testid="agent-removed-banner"` or disable the composer for ghost sessions.

- [ ] **Deleted-agent branded 404** (`agents.spec.ts (e)`)
  Navigating to `/agents/:nonexistent-slug` fetches the agent and renders a generic error
  state — no "Back to Agents" link with that exact text, no branded 404 component.

- [ ] **Offline send queue** (`chat.spec.ts (f)`)
  The chat store (`useChatStore`) does not implement a message queue for offline mode.
  Messages sent while `context.setOffline(true)` are dropped rather than queued.

- [ ] **Approval modal with stable testid** (`command-center.spec.ts (b)`)
  `ExecApprovalBlock` renders inside the chat composer area but does not use
  `data-testid="approval-modal"`. There is no stable selector to drive the approve flow.

- [ ] **Subagent collapsed block UI** (`handoff.spec.ts (b)`)
  No `data-testid="subagent-collapsed"` component. Subagent output arrives as plain
  assistant message text with no distinct collapsible UI.

- [ ] **Agent handoff transcript labels in DOM** (`handoff.spec.ts (a)`)
  AssistantMessage does not annotate each message with the handoff-chain agent's name
  in a way discoverable without `data-testid="messages-list"`.

- [ ] **Handoff depth policy test** (`handoff.spec.ts (c)`)
  Driving 5 real LLM handoffs deterministically in CI is impractical without a mock
  tool that auto-triggers handoffs on a signal.

- [ ] **Skill hash-mismatch error UI** (`skills.spec.ts (b)`)
  SkillBrowser does not expose a file input on the `/skills` route itself. The
  hash-mismatch error dialog is not reachable via a file input on the main page.

- [ ] **Download test via mock media tool** (`media.spec.ts (b)`)
  A deterministic file-download test requires a mock gateway tool that returns a
  non-image media frame. Real LLM instruction is non-deterministic.

---

## Missing `data-testid` attributes (nice-to-have for test stability)

- [ ] Chat composer textarea → `data-testid="chat-input"` (currently uses `aria-label="Message input"`)
- [ ] Send button → `data-testid="chat-send"` (currently uses `aria-label="Send message"`)
- [ ] Stop generation button → `data-testid="stop-btn"` (currently uses `aria-label="Stop generation"`)
- [ ] Assistant messages → `data-testid="assistant-message"` (currently uses `[data-message-role="assistant"]`)
- [ ] User messages → `data-testid="user-message"` (currently uses `[data-message-role="user"]`)
- [ ] Settings tabs → `data-testid="settings-tab-{providers|security|about}"` (currently uses `button[role="tab"]` with text match)
- [ ] Provider "Connected" badge → `data-testid="connected-badge"` (currently uses text match)
- [ ] Login error display → `data-testid="login-error"` (currently uses inline style match)
- [ ] Onboarding error display → `data-testid="onboarding-error"` (currently uses inline style match)
- [ ] Agent card → `data-testid="agent-card-{slug}"` (currently uses `button[aria-label^="View agent"]`)
- [ ] Messages list wrapper → `data-testid="messages-list"`
- [ ] Approval modal → `data-testid="approval-modal"`
- [ ] Agent-removed banner → `data-testid="agent-removed-banner"`
- [ ] Subagent collapsed block → `data-testid="subagent-collapsed"`
- [ ] Dev-mode banner → `data-testid="dev-mode-banner"`
- [ ] Version toast → `data-testid="version-toast"`
- [ ] Always-allow toggle → `data-testid="always-allow-toggle"`
- [ ] Build version display → `data-testid="build-version"`
- [ ] Build commit display → `data-testid="build-commit"`

---

## Routing Gaps

- [ ] `/about` is NOT a separate SPA route — it maps to `/settings?tab=about`.
  `accessibility.spec.ts` uses `/settings?tab=about` (correct) but the original spec
  used `/about` which would 404.

- [ ] `/chat` is NOT a separate route — the chat screen is at `/` (root).
  Tests must use `page.goto('/')` for the chat screen, not `/chat`.
