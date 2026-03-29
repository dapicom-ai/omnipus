# Feature Specification: Wave 4 — WhatsApp Channel & Browser Automation

**Created**: 2026-03-29
**Status**: Draft
**Input**: BRD FUNC-19 through FUNC-24, Appendix B §B.3.2–B.3.3, Appendix E §E.3

---

## Existing Codebase Context

> GitNexus index is not available. Context gathered via manual codebase exploration.

### Symbols Involved

| Symbol | Role | Context |
|--------|------|---------|
| `channels.Channel` | extends | Interface all channels implement: `Name()`, `Start()`, `Stop()`, `Send()`, `IsRunning()`, `IsAllowed()`, `IsAllowedSender()`, `ReasoningChannelID()`. WhatsApp native already implements this. Browser tools will NOT be a channel. |
| `channels.BaseChannel` | calls | Embed struct providing common allow-list, HandleMessage, typing/placeholder/reaction hooks. WhatsApp native already embeds this. |
| `channels.RegisterFactory` | calls | Registration function called from `init()` in each channel subpackage. WhatsApp native already registered as `"whatsapp_native"`. |
| `channels.Manager` | modifies | Manages channel lifecycle, outbound routing, rate limiting. No modification needed — existing registration/dispatch works. |
| `config.WhatsAppConfig` | extends | Existing config struct with `Enabled`, `BridgeURL`, `UseNative`, `SessionStorePath`, `AllowFrom`, `ReasoningChannelID`. Needs new fields for group config. |
| `config.ChannelsConfig` | modifies | Will not change — WhatsApp already present. No browser channel needed. |
| `tools.ToolRegistry` | calls | Browser tools register here via `Register()`. |
| `tools.Tool` | extends | Interface browser tools implement: `Name()`, `Description()`, `Parameters()`, `Execute()`. |
| `bus.MessageBus` | calls | WhatsApp publishes inbound messages here. Browser tools do not use MessageBus. |
| `WhatsAppNativeChannel` | modifies | Existing implementation needs: group message support improvements, media stub hooks, QR-over-WebUI event publishing, session re-pairing. |

### Impact Assessment

| Symbol Modified | Risk Level | d=1 Dependents | d=2 Dependents |
|----------------|------------|----------------|----------------|
| `WhatsAppNativeChannel` | LOW | `channels.Manager` (dispatch only) | Gateway (startup) |
| `config.WhatsAppConfig` | LOW | `WhatsAppNativeChannel`, `WhatsAppChannel` (bridge) | Config loader |
| `tools.ToolRegistry` | LOW | `toolloop`, `SubagentManager` | Agent loop |

### Cluster Placement

This feature spans two clusters:
- **Channels cluster** — WhatsApp native channel (FUNC-23, FUNC-24)
- **Tools cluster** — Browser automation tools (FUNC-19, FUNC-20, FUNC-21, FUNC-22)

No architectural coupling between the two — they are independent features that happen to ship in the same wave.

---

## User Stories & Acceptance Criteria

### User Story 1 — WhatsApp Native Channel Connection (Priority: P0)

An end user wants to connect their WhatsApp account to Omnipus so that they can interact with their AI agent through WhatsApp messages. Currently, PicoClaw requires an external WebSocket bridge (`ws://localhost:3001`) which is fragile, adds a runtime dependency, and frequently drops connection. A compiled-in native channel eliminates this external dependency, improving reliability and simplifying setup.

**Why this priority**: WhatsApp is the #1 messaging platform globally (2B+ users) and the most requested channel. Without native WhatsApp, Omnipus cannot credibly compete with OpenClaw. The bridge-based approach violates the single-binary, zero-dependency constraint.

**Independent Test**: Start Omnipus with `channels.whatsapp.use_native: true`, verify the WhatsApp native channel initializes, creates its SQLite session store, and enters QR-pairing mode on first run.

**Acceptance Scenarios**:

1. **Given** a fresh Omnipus install with `channels.whatsapp.enabled: true` and `use_native: true`, **When** the gateway starts, **Then** the WhatsApp native channel initializes, creates `~/.omnipus/channels/whatsapp/store.db`, and displays a QR code for pairing.
2. **Given** a previously paired WhatsApp session with valid credentials in `store.db`, **When** the gateway starts, **Then** the channel reconnects automatically without displaying a QR code.
3. **Given** a connected WhatsApp session, **When** a text message is received from an allowed sender, **Then** the message is published to the MessageBus with correct `channel: "whatsapp_native"`, `senderID`, `chatID`, and `content`.
4. **Given** a connected WhatsApp session, **When** the agent produces a response routed to WhatsApp, **Then** the message is sent to the correct WhatsApp chat via `whatsmeow.Client.SendMessage`.
5. **Given** a connected WhatsApp session, **When** the network connection drops, **Then** the channel attempts reconnection with exponential backoff (5s initial, 5m max, 2x multiplier).
6. **Given** `channels.whatsapp.allow_from` is configured with specific phone numbers, **When** a message arrives from an unlisted sender, **Then** the message is silently dropped (not published to MessageBus).

---

### User Story 2 — WhatsApp QR Pairing Flow (Priority: P0)

An operator wants to pair their WhatsApp account with Omnipus by scanning a QR code, so that the session is established and persists across restarts. The pairing must work in CLI (terminal), TUI, and WebUI contexts. Session expiry must be handled gracefully with automatic re-pairing prompts.

**Why this priority**: QR pairing is the mandatory first-use flow — without it, the WhatsApp channel cannot function at all. It is a hard dependency for US-1.

**Independent Test**: Start Omnipus without an existing WhatsApp session, verify QR code appears in the terminal, scan it with a phone, verify session is persisted and survives a restart.

**Acceptance Scenarios**:

1. **Given** no existing WhatsApp session, **When** the WhatsApp channel starts, **Then** a QR code is generated and displayed in the terminal using `qrterminal`.
2. **Given** a QR code is displayed, **When** the user scans it with their WhatsApp mobile app (Linked Devices), **Then** the session credentials are persisted to `store.db` and the channel transitions to connected state.
3. **Given** a QR code is displayed, **When** the QR code expires before scanning (typically 20s), **Then** a new QR code is generated and displayed automatically.
4. **Given** a paired session, **When** the WhatsApp server invalidates the session (e.g., user unlinks from phone), **Then** a `LoggedOut` event is received, the channel logs the event, and a new QR code is generated for re-pairing.
5. **Given** the gateway is running in headless mode (no TTY), **When** QR pairing is needed, **Then** the QR code data is emitted as a structured log event (`whatsapp.qr_code`) and published via WebSocket event so the WebUI can render it.
6. **Given** a valid paired session, **When** the gateway restarts, **Then** the channel reads credentials from `store.db` and connects without QR pairing.

---

### User Story 3 — WhatsApp Group Messages (Priority: P0)

An end user wants their Omnipus agent to participate in WhatsApp group chats, so that the agent can assist in group conversations. Group messages require different handling — the agent should respond only when triggered (mentioned or prefix-matched), and outbound messages must target the group JID.

**Why this priority**: Group messaging is a core WhatsApp use case. Without it, the channel is limited to 1:1 conversations, which significantly reduces utility.

**Independent Test**: Send a message in a WhatsApp group where the agent is a member, verify the message is received with `peer_kind: "group"` metadata and the agent can respond to the group.

**Acceptance Scenarios**:

1. **Given** the agent is a member of a WhatsApp group, **When** a message is sent in the group by an allowed sender, **Then** the inbound message includes `peer.Kind: "group"` and `peer.ID` set to the group JID.
2. **Given** `group_trigger.mention_only: true` in WhatsApp config, **When** a group message does NOT mention the agent, **Then** the message is not forwarded to the agent.
3. **Given** `group_trigger.prefixes: ["/ask"]` in WhatsApp config, **When** a group message starts with "/ask", **Then** the prefix is stripped and the remaining content is forwarded to the agent.
4. **Given** the agent responds to a group message, **When** the response is sent, **Then** it is delivered to the group chat (group JID), not the individual sender.

---

### User Story 4 — CDP Browser Control: Managed Mode (Priority: P0)

An agent wants to launch and control a Chromium browser instance to perform web automation tasks (research, form filling, data extraction), so that it can interact with web pages programmatically. Managed mode means Omnipus launches and manages its own dedicated Chromium instance, isolated from the user's personal browser.

**Why this priority**: Browser automation is the #2 most requested feature after WhatsApp. It enables agents to perform web research, interact with web applications, and extract structured data — fundamental capabilities for a general-purpose AI agent.

**Independent Test**: Configure `tools.browser.enabled: true`, invoke `browser.navigate("https://example.com")`, verify a Chromium instance launches with its own user data directory and the page loads successfully.

**Acceptance Scenarios**:

1. **Given** `tools.browser.enabled: true` and Chromium is installed on the system, **When** the first browser tool is invoked, **Then** a Chromium instance launches with a dedicated user data directory under `~/.omnipus/browser/profiles/default/`, headless by default.
2. **Given** `tools.browser.headless: false`, **When** a browser tool is invoked, **Then** Chromium launches in headed (visible) mode.
3. **Given** no Chromium binary is found on the system, **When** a browser tool is invoked, **Then** the tool returns a clear error: "Chromium not found. Install Chromium or configure tools.browser.cdp_url for remote mode."
4. **Given** a managed Chromium instance is running, **When** all browser sessions are closed or the gateway shuts down, **Then** the Chromium process is terminated gracefully.
5. **Given** a managed Chromium instance, **When** `browser.navigate(url)` is called, **Then** the page loads and the tool returns structured result `{ "url": "<final_url>", "title": "<page_title>", "status": <http_status> }`.
6. **Given** the SSRF protection module (SEC-24) is active, **When** `browser.navigate` targets a private IP range (10.x, 172.16-31.x, 192.168.x, 169.254.x), **Then** the navigation is blocked and the tool returns an SSRF error.

---

### User Story 5 — Browser Action Primitives (Priority: P0)

An agent wants a set of fine-grained browser actions (click, type, screenshot, get_text, wait, evaluate) so that it can interact with web page elements and extract information. Each action returns structured results the agent can reason about.

**Why this priority**: Navigation alone is insufficient — agents need to interact with page elements. These primitives form the vocabulary for all browser-based tasks.

**Independent Test**: Navigate to a known page, invoke `browser.click`, `browser.type`, `browser.get_text`, and `browser.screenshot` in sequence, verify each returns the expected structured result.

**Acceptance Scenarios**:

1. **Given** a page is loaded, **When** `browser.click(selector)` is called with a valid CSS selector, **Then** the element is clicked and the tool returns `{ "success": true, "selector": "<selector>" }`.
2. **Given** a page is loaded, **When** `browser.click(selector)` is called with a selector matching no elements, **Then** the tool returns an error: `"element not found: <selector>"` within the configured timeout.
3. **Given** a page with an input field, **When** `browser.type(selector, text)` is called, **Then** the text is typed into the element and the tool returns `{ "success": true }`.
4. **Given** a page is loaded, **When** `browser.screenshot()` is called, **Then** a PNG screenshot is captured, saved to a temp file, and the tool returns `{ "path": "<filepath>", "width": <w>, "height": <h> }`.
5. **Given** a page is loaded, **When** `browser.get_text(selector)` is called, **Then** the inner text of the matched element is returned as `{ "text": "<content>" }`.
6. **Given** a page is loaded, **When** `browser.wait(selector)` is called, **Then** the tool blocks until the element is present in the DOM (up to the page timeout), returning `{ "found": true }` or an error on timeout.
7. **Given** a page is loaded and `tools.browser.evaluate` is allowed by policy, **When** `browser.evaluate(js)` is called, **Then** the JavaScript is executed in the page context and the result is returned as `{ "result": <value> }`.
8. **Given** `tools.deny` includes `"browser.evaluate"`, **When** `browser.evaluate(js)` is called, **Then** the tool invocation is denied with a policy violation error (SEC-04/SEC-06).

---

### User Story 6 — Remote CDP Mode (Priority: P1)

An operator wants to connect Omnipus to an external Chromium instance running on a different host (e.g., Docker container, Browserless cloud service, Lightpanda), so that browser automation works on resource-constrained devices (Raspberry Pi, RISC-V) where local Chromium is impractical (100-300MB RAM).

**Why this priority**: Essential for the low-resource hardware target audience. Without remote CDP, browser automation is unusable on the primary differentiating hardware tier.

**Independent Test**: Configure `tools.browser.cdp_url: "ws://localhost:9222"`, start an external Chromium with `--remote-debugging-port=9222`, invoke `browser.navigate`, verify it works through the remote connection.

**Acceptance Scenarios**:

1. **Given** `tools.browser.cdp_url` is configured with a valid WebSocket URL, **When** the first browser tool is invoked, **Then** Omnipus connects to the external Chromium via CDP instead of launching a local instance.
2. **Given** `tools.browser.cdp_url` is configured, **When** the remote Chromium is unreachable, **Then** the tool returns an error: `"cannot connect to remote browser at <url>: <reason>"`.
3. **Given** both `tools.browser.cdp_url` and local Chromium are available, **When** a browser tool is invoked, **Then** remote CDP takes precedence (remote mode overrides managed mode).
4. **Given** a remote CDP connection, **When** the remote browser disconnects mid-session, **Then** subsequent tool calls return a connection error and the next invocation attempts to reconnect.

---

### User Story 7 — Browser Resource Limits (Priority: P1)

An operator wants to configure resource limits on browser sessions to prevent runaway browser processes from consuming excessive system resources (memory, CPU, open tabs), especially on constrained hardware.

**Why this priority**: Without limits, a single agent browsing loop could exhaust system memory or open hundreds of tabs. Resource limits are a safety net that prevents the browser from degrading the entire system.

**Independent Test**: Configure `tools.browser.max_tabs: 3`, attempt to open 4 tabs, verify the 4th is rejected. Configure `tools.browser.page_timeout: 5s`, navigate to a slow page, verify timeout fires.

**Acceptance Scenarios**:

1. **Given** `tools.browser.page_timeout: 10s` (default 30s), **When** a page load exceeds 10 seconds, **Then** the navigation is aborted and the tool returns a timeout error with the partial page state.
2. **Given** `tools.browser.max_tabs: 5` (default), **When** a 6th tab is requested, **Then** the tool returns an error: `"maximum concurrent tabs (5) reached. Close a tab first."`.
3. **Given** `tools.browser.max_memory_mb: 512`, **When** browser memory usage exceeds 512MB, **Then** a warning is logged and the oldest inactive tab is closed to reclaim memory.
4. **Given** browser resource limits are configured, **When** `omnipus doctor` runs, **Then** it reports the configured browser limits and warns if they seem too permissive for the available system memory.

---

## Behavioral Contract

Primary flows:
- When WhatsApp `use_native: true` and `enabled: true`, the system starts the native channel using whatsmeow (not the bridge).
- When no WhatsApp session exists, the system displays a QR code and waits for pairing.
- When a paired session exists, the system auto-reconnects on startup without QR.
- When an inbound WhatsApp message arrives from an allowed sender, the system publishes it to MessageBus.
- When `browser.navigate(url)` is called with a valid URL, the system loads the page and returns structured metadata.
- When `browser.click(selector)` targets a valid element, the system clicks it and returns success.
- When `browser.screenshot()` is called, the system captures a PNG and returns the file path.
- When `tools.browser.cdp_url` is set, the system uses remote CDP instead of launching local Chromium.

Error flows:
- When Chromium is not installed and no `cdp_url` is configured, the system returns a descriptive error on any browser tool invocation.
- When the WhatsApp connection drops, the system reconnects with exponential backoff (5s–5m).
- When a CSS selector matches no elements, browser tools return an "element not found" error.
- When a page load exceeds the configured timeout, the system aborts and returns a timeout error.
- When the remote CDP endpoint is unreachable, the system returns a connection error.
- When a WhatsApp session is invalidated (logged out from phone), the system enters re-pairing mode.

Boundary conditions:
- When the max tab limit is reached, new tab requests are rejected with a clear error.
- When browser memory exceeds the configured limit, the oldest inactive tab is closed.
- When `browser.evaluate` is policy-denied, the invocation is blocked with a policy violation.
- When SSRF protection blocks a browser navigation URL, the tool returns an SSRF error.
- When WhatsApp `allow_from` is empty, all senders are allowed (open access).

---

## Edge Cases

- What happens when the WhatsApp SQLite database is corrupted? Expected: The channel logs an error on startup, renames the corrupted DB to `store.db.corrupt.<timestamp>`, creates a fresh DB, and enters QR-pairing mode.
- What happens when two Omnipus instances try to use the same WhatsApp session? Expected: The second instance fails to acquire the SQLite lock and logs an error. WhatsApp only allows one connection per linked device — the second connection would disconnect the first.
- What happens when `browser.type` is called on a non-input element (e.g., a `<div>`)? Expected: chromedp attempts to focus and type; if the element is not focusable, the tool returns an error.
- What happens when `browser.screenshot` is called before any page is loaded? Expected: Returns a screenshot of the blank page (`about:blank`).
- What happens when the browser process crashes mid-operation? Expected: The next tool call detects the dead process, logs the crash, and launches a new instance (managed mode) or returns a connection error (remote mode).
- What happens when WhatsApp rate-limits outbound messages? Expected: The per-channel rate limiter (SEC-26) prevents hitting WhatsApp's limits. If rate-limited by WhatsApp servers, the send returns `ErrTemporary` and the channel retries with backoff.
- What happens when a very large page (>10MB DOM) is loaded? Expected: `browser.get_text` on the body returns truncated text (configurable max, default 100KB). `browser.screenshot` works normally.
- What happens when `browser.evaluate` returns a non-serializable value (e.g., DOM node, function)? Expected: chromedp serializes what it can; non-serializable values return as `null` with a warning.
- What happens when WhatsApp sends a message type the agent cannot handle (e.g., sticker, poll, reaction)? Expected: The channel logs the unsupported message type at debug level and ignores it (does not publish to MessageBus). No error.
- What happens when Chromium's user data directory (`~/.omnipus/browser/profiles/`) is on a read-only filesystem? Expected: Browser launch fails with a clear error about the read-only directory.

---

## Explicit Non-Behaviors

- The system must not bundle Chromium with the Omnipus binary because it would add 100-300MB, violating the lightweight single-binary constraint. Chromium is a user-installed optional dependency.
- The system must not use CGo for SQLite because it violates the "Pure Go, no CGo" hard constraint. Only `modernc.org/sqlite` is permitted.
- The system must not store WhatsApp session credentials anywhere except `store.db` because duplicating session data creates consistency risks and potential security exposure.
- The system must not implement WhatsApp Business API mode because it requires Meta approval and is out of scope for Wave 4. Personal account via whatsmeow only.
- The system must not implement WhatsApp media handling (images, audio, documents) because that is FUNC-25, scoped to a future wave.
- The system must not implement a browser canvas/A2UI rendering surface because that is FUNC-28, scoped to a future wave.
- The system must not allow `browser.evaluate` by default because arbitrary JavaScript execution is a security risk. It must be explicitly allowed via policy (SEC-04/SEC-06).
- The system must not persist browser session state (cookies, localStorage) across gateway restarts by default because it creates a security liability. Opt-in via `tools.browser.persist_session: true`.
- The system must not navigate to `file://` URLs because local file access via browser bypasses Landlock filesystem restrictions. Only `http://` and `https://` schemes are permitted.
- The system must not auto-reconnect to WhatsApp indefinitely without backoff because it could hammer WhatsApp servers and get the account banned. Exponential backoff with 5-minute ceiling is mandatory.

---

## Integration Boundaries

### WhatsApp Web (whatsmeow)

- **Data in**: Inbound text messages (conversation, extended text), message metadata (sender JID, chat JID, push name, message ID, group/direct indicator)
- **Data out**: Outbound text messages (`waE2E.Message` with `Conversation` field)
- **Contract**: WhatsApp Web multi-device protocol via `whatsmeow` Go library. WebSocket connection to WhatsApp servers. Binary protobuf messages. Session stored in SQLite.
- **On failure**: Network disconnect triggers exponential backoff reconnection. Session invalidation triggers QR re-pairing. WhatsApp server errors return `ErrTemporary` for retry. Rate limiting from WhatsApp servers logged and respected.
- **Development**: Real WhatsApp account required for integration testing. Unit tests mock the `whatsmeow.Client` interface. No simulated twin available — WhatsApp's protocol is proprietary and cannot be meaningfully simulated.

### Chrome DevTools Protocol (chromedp)

- **Data in**: CDP commands (navigate, evaluate, screenshot, DOM queries) sent as JSON-RPC over WebSocket
- **Data out**: CDP responses (page metadata, DOM content, screenshot bytes, JS evaluation results)
- **Contract**: Chrome DevTools Protocol over WebSocket. Managed mode: Omnipus launches Chromium with `--remote-debugging-port` and connects. Remote mode: Omnipus connects to user-provided `ws://` URL.
- **On failure**: Browser crash detected via WebSocket close; next tool call launches new instance (managed) or returns error (remote). Page load timeout returns partial state. Element not found returns error after wait timeout.
- **Development**: Real Chromium for integration tests. Unit tests mock the chromedp allocator context. `chromedp/chromedp` provides a `testAllocator` for test harness.

### MessageBus (internal)

- **Data in**: `bus.InboundMessage` from WhatsApp channel
- **Data out**: `bus.OutboundMessage` to WhatsApp channel
- **Contract**: In-process Go channels. Zero IPC overhead. Published via `bus.PublishInbound()`, dispatched by `channels.Manager`.
- **On failure**: Cannot fail — in-process channels. Blocked publish would indicate a bug in the consumer goroutine.
- **Development**: Real MessageBus in all tests — it's lightweight in-process infrastructure.

### Filesystem (session storage)

- **Data in**: WhatsApp session reads from `~/.omnipus/channels/whatsapp/store.db`. Browser profile reads from `~/.omnipus/browser/profiles/<name>/`.
- **Data out**: WhatsApp session writes to `store.db`. Browser screenshots to temp directory. Browser profile writes (cookies, cache) to profile directory.
- **Contract**: SQLite for WhatsApp (via `modernc.org/sqlite`). Standard filesystem for browser profiles and screenshots.
- **On failure**: Read-only filesystem returns clear error. Corrupted SQLite DB is renamed and recreated. Disk full returns error on write operations.
- **Development**: Temp directories for test isolation. `t.TempDir()` for all file-based tests.

### Security Subsystem (SEC-04, SEC-06, SEC-24, SEC-26)

- **Data in**: Tool invocation requests from agent loop
- **Data out**: Allow/deny decisions
- **Contract**: Browser tools are subject to tool allow/deny (SEC-04), per-method control (SEC-06), SSRF protection (SEC-24), and rate limiting (SEC-26). WhatsApp outbound is subject to per-channel rate limiting.
- **On failure**: Policy denial returns structured error with explanation (SEC-17). SSRF block returns specific error. Rate limit returns `retry_after_seconds`.
- **Development**: Unit tests use the FallbackBackend (app-level policy). Integration tests verify SSRF blocking and rate limiting.

---

## BDD Scenarios

### Feature: WhatsApp Native Channel

#### Scenario: Fresh install QR pairing

**Traces to**: User Story 2, Acceptance Scenario 1
**Category**: Happy Path

- **Given** Omnipus is configured with `channels.whatsapp.enabled: true` and `use_native: true`
- **And** no WhatsApp session exists (`store.db` does not exist)
- **When** the gateway starts and the WhatsApp channel initializes
- **Then** a QR code is displayed in the terminal
- **And** the QR code contains valid WhatsApp pairing data
- **And** the channel logs "Scan this QR code with WhatsApp (Linked Devices)"

---

#### Scenario: Auto-reconnect with existing session

**Traces to**: User Story 2, Acceptance Scenario 6
**Category**: Happy Path

- **Given** a valid WhatsApp session exists in `store.db`
- **When** the gateway starts
- **Then** the WhatsApp channel connects without displaying a QR code
- **And** the channel logs "WhatsApp native channel connected"

---

#### Scenario: QR code refresh on expiry

**Traces to**: User Story 2, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** a QR code is displayed and the user has not scanned it
- **When** the QR code expires (WhatsApp server sends new code event)
- **Then** a new QR code is displayed automatically
- **And** the old QR code is no longer valid

---

#### Scenario: Session invalidation and re-pairing

**Traces to**: User Story 2, Acceptance Scenario 4
**Category**: Error Path

- **Given** a previously paired WhatsApp session
- **When** the WhatsApp server sends a `LoggedOut` event (user unlinked device)
- **Then** the channel logs "WhatsApp session invalidated, re-pairing required"
- **And** a new QR code is generated for re-pairing

---

#### Scenario: Headless QR pairing via WebSocket event

**Traces to**: User Story 2, Acceptance Scenario 5
**Category**: Alternate Path

- **Given** the gateway is running without a TTY (headless mode)
- **When** QR pairing is needed
- **Then** the QR code data is emitted as a structured log event with key `whatsapp.qr_code`
- **And** the QR data is published via the gateway WebSocket as event type `channel.whatsapp.qr`
- **And** no terminal QR rendering is attempted

---

#### Scenario: Receive text message from allowed sender

**Traces to**: User Story 1, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a connected WhatsApp session
- **And** `allow_from` includes the sender's phone number
- **When** a text message arrives from that sender
- **Then** the message is published to MessageBus as `bus.InboundMessage`
- **And** `msg.Channel` equals `"whatsapp_native"`
- **And** `msg.Sender.Platform` equals `"whatsapp"`
- **And** `msg.Sender.CanonicalID` equals `"whatsapp:<sender_jid>"`

---

#### Scenario: Drop message from unlisted sender

**Traces to**: User Story 1, Acceptance Scenario 6
**Category**: Alternate Path

- **Given** a connected WhatsApp session with `allow_from: ["1234567890"]`
- **When** a text message arrives from phone number `9999999999`
- **Then** the message is NOT published to MessageBus
- **And** no error is logged (silent drop)

---

#### Scenario: Send response to WhatsApp chat

**Traces to**: User Story 1, Acceptance Scenario 4
**Category**: Happy Path

- **Given** a connected WhatsApp session
- **When** the agent sends a response routed to WhatsApp with `chatID: "1234567890@s.whatsapp.net"`
- **Then** `whatsmeow.Client.SendMessage` is called with the correct JID
- **And** the message content is wrapped in a `waE2E.Message` protobuf

---

#### Scenario: Send fails when not paired

**Traces to**: User Story 1, Acceptance Scenario 4 (error variant)
**Category**: Error Path

- **Given** a WhatsApp channel that is started but QR pairing is not yet complete
- **When** a send is attempted
- **Then** the send returns `ErrTemporary` with message "whatsapp not yet paired (QR login pending)"

---

#### Scenario: Network disconnect and reconnection

**Traces to**: User Story 1, Acceptance Scenario 5
**Category**: Error Path

- **Given** a connected WhatsApp session
- **When** the network connection drops (Disconnected event received)
- **Then** the channel attempts reconnection with 5s initial backoff
- **And** backoff doubles on each failure up to 5m maximum
- **And** reconnection succeeds when the network is restored

---

#### Scenario: Group message with mention trigger

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Happy Path

- **Given** the agent is in a WhatsApp group
- **And** `group_trigger.mention_only: true`
- **When** a group message mentions the agent
- **Then** the message is forwarded to the agent with `peer.Kind: "group"`

---

#### Scenario: Group message without trigger ignored

**Traces to**: User Story 3, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** `group_trigger.mention_only: true`
- **When** a group message does NOT mention the agent
- **Then** the message is not forwarded to the agent

---

#### Scenario: Group response targets group JID

**Traces to**: User Story 3, Acceptance Scenario 4
**Category**: Happy Path

- **Given** the agent is responding to a group message
- **When** the response is sent
- **Then** the message is delivered to the group JID (e.g., `12345678@g.us`)
- **And** NOT to the individual sender's JID

---

#### Scenario: Corrupted SQLite database recovery

**Traces to**: Edge Cases
**Category**: Edge Case

- **Given** the WhatsApp `store.db` file is corrupted (not valid SQLite)
- **When** the WhatsApp channel starts
- **Then** the channel logs an error about database corruption
- **And** renames the corrupted file to `store.db.corrupt.<timestamp>`
- **And** creates a fresh database
- **And** enters QR pairing mode

---

### Feature: Browser Automation — Managed Mode

#### Scenario: Launch managed Chromium on first tool call

**Traces to**: User Story 4, Acceptance Scenario 1
**Category**: Happy Path

- **Given** `tools.browser.enabled: true`
- **And** Chromium is installed and on PATH
- **When** `browser.navigate("https://example.com")` is called for the first time
- **Then** a headless Chromium instance launches with user data dir `~/.omnipus/browser/profiles/default/`
- **And** the page loads and the tool returns `{ "url": "https://example.com/", "title": "Example Domain", "status": 200 }`

---

#### Scenario: Launch headed Chromium

**Traces to**: User Story 4, Acceptance Scenario 2
**Category**: Alternate Path

- **Given** `tools.browser.headless: false`
- **When** a browser tool is invoked
- **Then** Chromium launches in visible (headed) mode

---

#### Scenario: Chromium not found

**Traces to**: User Story 4, Acceptance Scenario 3
**Category**: Error Path

- **Given** no Chromium binary is found on the system
- **And** no `tools.browser.cdp_url` is configured
- **When** a browser tool is invoked
- **Then** the tool returns error: `"Chromium not found. Install Chromium or configure tools.browser.cdp_url for remote mode."`

---

#### Scenario: SSRF protection blocks private IP navigation

**Traces to**: User Story 4, Acceptance Scenario 6
**Category**: Error Path

- **Given** SSRF protection is active (SEC-24)
- **When** `browser.navigate("http://169.254.169.254/latest/meta-data/")` is called
- **Then** the navigation is blocked before the request is sent
- **And** the tool returns error containing "SSRF" and the blocked URL

---

#### Scenario: Graceful browser shutdown

**Traces to**: User Story 4, Acceptance Scenario 4
**Category**: Happy Path

- **Given** a managed Chromium instance is running
- **When** the gateway shuts down (SIGTERM)
- **Then** the Chromium process receives a graceful termination signal
- **And** the Chromium process exits within 5 seconds
- **And** no orphaned Chromium processes remain

---

### Feature: Browser Action Primitives

#### Scenario: Click element by CSS selector

**Traces to**: User Story 5, Acceptance Scenario 1
**Category**: Happy Path

- **Given** a page is loaded with a button `<button id="submit">Submit</button>`
- **When** `browser.click("#submit")` is called
- **Then** the button is clicked
- **And** the tool returns `{ "success": true, "selector": "#submit" }`

---

#### Scenario: Click non-existent element

**Traces to**: User Story 5, Acceptance Scenario 2
**Category**: Error Path

- **Given** a page is loaded
- **When** `browser.click("#nonexistent")` is called
- **Then** the tool waits up to the page timeout
- **And** returns error: `"element not found: #nonexistent"`

---

#### Scenario: Type text into input field

**Traces to**: User Story 5, Acceptance Scenario 3
**Category**: Happy Path

- **Given** a page with `<input id="search" type="text">`
- **When** `browser.type("#search", "hello world")` is called
- **Then** "hello world" is typed into the input
- **And** the tool returns `{ "success": true }`

---

#### Scenario: Take screenshot

**Traces to**: User Story 5, Acceptance Scenario 4
**Category**: Happy Path

- **Given** a page is loaded at `https://example.com`
- **When** `browser.screenshot()` is called
- **Then** a PNG file is saved to a temp directory
- **And** the tool returns `{ "path": "/tmp/omnipus-browser-<hash>.png", "width": 1280, "height": 720 }`

---

#### Scenario: Get text content

**Traces to**: User Story 5, Acceptance Scenario 5
**Category**: Happy Path

- **Given** a page with `<h1 id="title">Welcome</h1>`
- **When** `browser.get_text("#title")` is called
- **Then** the tool returns `{ "text": "Welcome" }`

---

#### Scenario: Wait for element

**Traces to**: User Story 5, Acceptance Scenario 6
**Category**: Happy Path

- **Given** a page that dynamically loads a `<div id="loaded">` after 2 seconds
- **When** `browser.wait("#loaded")` is called
- **Then** the tool blocks until the element appears
- **And** returns `{ "found": true }`

---

#### Scenario: Wait for element timeout

**Traces to**: User Story 5, Acceptance Scenario 6 (timeout variant)
**Category**: Error Path

- **Given** a page where `#never-appears` is never added to the DOM
- **When** `browser.wait("#never-appears")` is called
- **Then** the tool waits until the page timeout
- **And** returns error: `"timeout waiting for element: #never-appears"`

---

#### Scenario: Evaluate JavaScript

**Traces to**: User Story 5, Acceptance Scenario 7
**Category**: Happy Path

- **Given** a page is loaded
- **And** `browser.evaluate` is allowed by policy
- **When** `browser.evaluate("document.title")` is called
- **Then** the tool returns `{ "result": "Example Domain" }`

---

#### Scenario: Evaluate JavaScript denied by policy

**Traces to**: User Story 5, Acceptance Scenario 8
**Category**: Error Path

- **Given** `tools.deny` includes `"browser.evaluate"`
- **When** `browser.evaluate("document.title")` is called
- **Then** the tool returns a policy violation error
- **And** the denial is audit-logged with the matching policy rule

---

#### Scenario Outline: Browser navigate to various URL schemes

**Traces to**: User Story 4, Acceptance Scenario 5; Explicit Non-Behaviors (file:// URLs)
**Category**: Edge Case

- **Given** a managed Chromium instance is running
- **When** `browser.navigate("<url>")` is called
- **Then** the result is `<outcome>`

**Examples**:

| url | outcome |
|-----|---------|
| `https://example.com` | Success with page metadata |
| `http://example.com` | Success with page metadata |
| `file:///etc/passwd` | Error: "file:// URLs are not permitted" |
| `javascript:alert(1)` | Error: "javascript: URLs are not permitted" |
| `data:text/html,<h1>Hi</h1>` | Error: "data: URLs are not permitted" |
| `chrome://settings` | Error: "chrome: URLs are not permitted" |

---

### Feature: Browser Remote CDP Mode

#### Scenario: Connect via remote CDP URL

**Traces to**: User Story 6, Acceptance Scenario 1
**Category**: Happy Path

- **Given** `tools.browser.cdp_url: "ws://localhost:9222"`
- **And** an external Chromium is running with remote debugging on port 9222
- **When** `browser.navigate("https://example.com")` is called
- **Then** the tool connects via the CDP URL (no local Chromium launched)
- **And** the page loads and returns structured metadata

---

#### Scenario: Remote CDP endpoint unreachable

**Traces to**: User Story 6, Acceptance Scenario 2
**Category**: Error Path

- **Given** `tools.browser.cdp_url: "ws://localhost:9222"`
- **And** no Chromium is running on port 9222
- **When** a browser tool is invoked
- **Then** the tool returns error: `"cannot connect to remote browser at ws://localhost:9222: connection refused"`

---

#### Scenario: Remote CDP takes precedence over local

**Traces to**: User Story 6, Acceptance Scenario 3
**Category**: Alternate Path

- **Given** both `tools.browser.cdp_url: "ws://remote:9222"` and local Chromium are available
- **When** a browser tool is invoked
- **Then** the tool uses the remote CDP connection
- **And** no local Chromium is launched

---

### Feature: Browser Resource Limits

#### Scenario: Page load timeout

**Traces to**: User Story 7, Acceptance Scenario 1
**Category**: Error Path

- **Given** `tools.browser.page_timeout: "5s"`
- **When** `browser.navigate` targets a page that takes 30 seconds to load
- **Then** the navigation is aborted after 5 seconds
- **And** the tool returns a timeout error

---

#### Scenario: Maximum tabs exceeded

**Traces to**: User Story 7, Acceptance Scenario 2
**Category**: Error Path

- **Given** `tools.browser.max_tabs: 3`
- **And** 3 tabs are already open
- **When** a 4th `browser.navigate` with `new_tab: true` is called
- **Then** the tool returns error: `"maximum concurrent tabs (3) reached. Close a tab first."`

---

#### Scenario Outline: Tab limit enforcement

**Traces to**: User Story 7, Acceptance Scenario 2
**Category**: Edge Case

- **Given** `tools.browser.max_tabs` is `<limit>`
- **And** `<open>` tabs are currently open
- **When** a new tab is requested
- **Then** the result is `<outcome>`

**Examples**:

| limit | open | outcome |
|-------|------|---------|
| 5 | 0 | Tab opens successfully |
| 5 | 4 | Tab opens successfully |
| 5 | 5 | Error: max tabs reached |
| 1 | 1 | Error: max tabs reached |
| 0 | 100 | Tab opens (0 = unlimited) |

---

## Test-Driven Development Plan

### Test Hierarchy

| Level | Scope | Purpose |
|-------|-------|---------|
| Unit | Individual functions/methods: JID parsing, URL scheme validation, config parsing, SSRF URL checks, tab counting, timeout handling | Validates logic in isolation without external dependencies |
| Integration | WhatsApp channel lifecycle (mocked whatsmeow), browser tool with real chromedp against test HTML server | Validates components work together with controlled external dependencies |
| E2E | Full WhatsApp message flow (requires real WhatsApp account), full browser automation flow with real Chromium | Validates complete feature from user perspective |

### Test Implementation Order

| Order | Test Name | Level | Traces to BDD Scenario | Description |
|-------|-----------|-------|------------------------|-------------|
| 1 | `TestParseJID_Valid` | Unit | Scenario: Send response to WhatsApp chat | Verify JID parsing for phone numbers and full JID strings |
| 2 | `TestParseJID_Invalid` | Unit | Scenario: Send response to WhatsApp chat | Verify error on empty, malformed JID inputs |
| 3 | `TestURLSchemeValidation` | Unit | Scenario Outline: Browser navigate URL schemes | Verify only http/https allowed, file/javascript/data/chrome blocked |
| 4 | `TestSSRFURLCheck` | Unit | Scenario: SSRF protection blocks private IP | Verify private IPs and metadata endpoints are blocked |
| 5 | `TestTabCounter_Limits` | Unit | Scenario Outline: Tab limit enforcement | Verify tab count tracking and limit enforcement |
| 6 | `TestPageTimeoutConfig` | Unit | Scenario: Page load timeout | Verify timeout parsing and default values |
| 7 | `TestWhatsAppConfigParsing` | Unit | Scenario: Fresh install QR pairing | Verify WhatsApp config fields parse correctly including new group_trigger |
| 8 | `TestBrowserConfigParsing` | Unit | Scenario: Launch managed Chromium | Verify browser config fields parse correctly with defaults |
| 9 | `TestGroupTriggerWhatsApp` | Unit | Scenario: Group message with mention trigger | Verify BaseChannel.ShouldRespondInGroup for WhatsApp group messages |
| 10 | `TestCorruptedDBRecovery` | Unit | Scenario: Corrupted SQLite database recovery | Verify corrupt DB detection, rename, and fresh DB creation |
| 11 | `TestBrowserToolRegistration` | Integration | Scenario: Launch managed Chromium | Verify all 7 browser tools register in ToolRegistry |
| 12 | `TestWhatsAppChannelStart_NoSession` | Integration | Scenario: Fresh install QR pairing | Verify channel enters QR mode when no store.db exists (mocked client) |
| 13 | `TestWhatsAppChannelStart_ExistingSession` | Integration | Scenario: Auto-reconnect with existing session | Verify channel auto-connects with existing session (mocked client) |
| 14 | `TestWhatsAppInboundMessage` | Integration | Scenario: Receive text message from allowed sender | Verify message event → MessageBus publish with correct fields |
| 15 | `TestWhatsAppSendMessage` | Integration | Scenario: Send response to WhatsApp chat | Verify outbound message → whatsmeow SendMessage with correct JID |
| 16 | `TestWhatsAppSendNotPaired` | Integration | Scenario: Send fails when not paired | Verify send returns ErrTemporary when not paired |
| 17 | `TestWhatsAppAllowListDrop` | Integration | Scenario: Drop message from unlisted sender | Verify unlisted sender messages are silently dropped |
| 18 | `TestWhatsAppReconnectBackoff` | Integration | Scenario: Network disconnect and reconnection | Verify exponential backoff timing (5s, 10s, 20s...) |
| 19 | `TestBrowserNavigate_ManagedMode` | Integration | Scenario: Launch managed Chromium | Verify chromedp launches Chromium and navigates (real Chromium) |
| 20 | `TestBrowserNavigate_RemoteCDP` | Integration | Scenario: Connect via remote CDP URL | Verify connection to external Chromium via CDP URL |
| 21 | `TestBrowserClick` | Integration | Scenario: Click element by CSS selector | Verify click on test HTML page element |
| 22 | `TestBrowserType` | Integration | Scenario: Type text into input field | Verify typing into input on test HTML page |
| 23 | `TestBrowserScreenshot` | Integration | Scenario: Take screenshot | Verify PNG file created with correct dimensions |
| 24 | `TestBrowserGetText` | Integration | Scenario: Get text content | Verify text extraction from test HTML page |
| 25 | `TestBrowserWait_Found` | Integration | Scenario: Wait for element | Verify wait returns when element appears |
| 26 | `TestBrowserWait_Timeout` | Integration | Scenario: Wait for element timeout | Verify timeout error when element never appears |
| 27 | `TestBrowserEvaluate` | Integration | Scenario: Evaluate JavaScript | Verify JS evaluation returns result |
| 28 | `TestBrowserEvaluate_PolicyDenied` | Integration | Scenario: Evaluate JavaScript denied by policy | Verify policy denial blocks evaluation |
| 29 | `TestBrowserSSRFBlock` | Integration | Scenario: SSRF protection blocks private IP | Verify navigate to private IP blocked by SSRF check |
| 30 | `TestBrowserMaxTabs` | Integration | Scenario: Maximum tabs exceeded | Verify tab limit enforcement |
| 31 | `TestBrowserChromiumNotFound` | Integration | Scenario: Chromium not found | Verify clear error when no Chromium available |
| 32 | `TestBrowserRemoteCDPUnreachable` | Integration | Scenario: Remote CDP endpoint unreachable | Verify connection error message |
| 33 | `TestBrowserGracefulShutdown` | Integration | Scenario: Graceful browser shutdown | Verify Chromium process terminated on gateway shutdown |
| 34 | `TestWhatsAppE2E_MessageRoundTrip` | E2E | Scenario: Receive + Send | Full message receive → agent → send roundtrip (requires real account) |
| 35 | `TestBrowserE2E_NavigateClickScreenshot` | E2E | Scenario: Multiple primitives | Navigate → click → screenshot flow with real Chromium |

### Test Datasets

#### Dataset: JID Parsing

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `"1234567890"` | Happy path | `JID{User: "1234567890", Server: "s.whatsapp.net"}` | Scenario: Send response | Plain phone number |
| 2 | `"1234567890@s.whatsapp.net"` | Happy path | `JID{User: "1234567890", Server: "s.whatsapp.net"}` | Scenario: Send response | Full direct JID |
| 3 | `"12345678@g.us"` | Happy path | `JID{User: "12345678", Server: "g.us"}` | Scenario: Group response | Group JID |
| 4 | `""` | Empty | Error: "empty chat id" | Scenario: Send response | Empty string |
| 5 | `"   "` | Whitespace | Error: "empty chat id" | Scenario: Send response | Whitespace only |
| 6 | `"@s.whatsapp.net"` | Boundary | Error (no user part) | Scenario: Send response | Missing user |
| 7 | `"+1-234-567-890"` | Edge case | Parsed (whatsmeow handles) | Scenario: Send response | Formatted phone number |

#### Dataset: URL Scheme Validation

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `"https://example.com"` | Happy path | Allowed | Scenario Outline: URL schemes | Standard HTTPS |
| 2 | `"http://example.com"` | Happy path | Allowed | Scenario Outline: URL schemes | HTTP |
| 3 | `"file:///etc/passwd"` | Security | Blocked | Scenario Outline: URL schemes | Local file access |
| 4 | `"javascript:alert(1)"` | Security | Blocked | Scenario Outline: URL schemes | XSS vector |
| 5 | `"data:text/html,<h1>Hi</h1>"` | Security | Blocked | Scenario Outline: URL schemes | Data URI |
| 6 | `"chrome://settings"` | Security | Blocked | Scenario Outline: URL schemes | Browser internals |
| 7 | `"HTTP://EXAMPLE.COM"` | Edge case | Allowed | Scenario Outline: URL schemes | Case-insensitive scheme |
| 8 | `""` | Empty | Error | Scenario Outline: URL schemes | Empty URL |
| 9 | `"ftp://files.example.com"` | Edge case | Blocked | Scenario Outline: URL schemes | Non-web protocol |
| 10 | `"https://example.com:8080/path?q=1"` | Happy path | Allowed | Scenario Outline: URL schemes | URL with port and query |

#### Dataset: SSRF URL Validation (Browser)

| # | Input | Boundary Type | Expected Output | Traces to | Notes |
|---|-------|---------------|-----------------|-----------|-------|
| 1 | `"http://10.0.0.1"` | Security | Blocked (10.x) | Scenario: SSRF block | Private class A |
| 2 | `"http://172.16.0.1"` | Security | Blocked (172.16-31.x) | Scenario: SSRF block | Private class B |
| 3 | `"http://192.168.1.1"` | Security | Blocked (192.168.x) | Scenario: SSRF block | Private class C |
| 4 | `"http://169.254.169.254"` | Security | Blocked (link-local/metadata) | Scenario: SSRF block | AWS metadata |
| 5 | `"http://127.0.0.1"` | Security | Blocked (loopback) | Scenario: SSRF block | Loopback |
| 6 | `"https://example.com"` | Happy path | Allowed | Scenario: SSRF block | Public URL |
| 7 | `"http://[::1]"` | Security | Blocked (IPv6 loopback) | Scenario: SSRF block | IPv6 loopback |
| 8 | `"http://0x7f000001"` | Security | Blocked (hex loopback) | Scenario: SSRF block | Hex-encoded 127.0.0.1 |

#### Dataset: Tab Limit Enforcement

| # | Input (limit, open_tabs) | Boundary Type | Expected Output | Traces to | Notes |
|---|--------------------------|---------------|-----------------|-----------|-------|
| 1 | (5, 0) | Min | Allow | Scenario Outline: Tab limit | Zero tabs open |
| 2 | (5, 4) | Below limit | Allow | Scenario Outline: Tab limit | One below limit |
| 3 | (5, 5) | At limit | Deny | Scenario Outline: Tab limit | Exactly at limit |
| 4 | (5, 6) | Above limit | Deny | Scenario Outline: Tab limit | Should not happen but test defensively |
| 5 | (1, 0) | Min limit | Allow | Scenario Outline: Tab limit | Minimum useful limit |
| 6 | (1, 1) | At min limit | Deny | Scenario Outline: Tab limit | Single tab limit hit |
| 7 | (0, 100) | Unlimited | Allow | Scenario Outline: Tab limit | Zero means unlimited |

### Regression Test Requirements

> No regression impact — new capability. Both WhatsApp native and browser tools are entirely new code paths.

Integration seams protected by:
- `TestManagerDispatch` (existing) — verifies Manager dispatches to registered channels. WhatsApp native uses the same registration pattern.
- `TestToolRegistryRegister` (existing) — verifies tool registration. Browser tools use the same pattern.
- `TestBaseChannelAllowList` (existing) — verifies allow-list filtering. WhatsApp native reuses BaseChannel.
- `TestSSRFCheck` (existing in `pkg/security/`) — verifies SSRF IP blocking. Browser reuses the same checker.

---

## Functional Requirements

- **FR-001**: System MUST implement WhatsApp channel as a compiled-in Go channel using `whatsmeow`, communicating via the internal MessageBus (zero IPC overhead).
- **FR-002**: System MUST use `modernc.org/sqlite` (pure Go) for WhatsApp session storage. CGo is prohibited.
- **FR-003**: System MUST generate a QR code for WhatsApp pairing on first connection, displayed in CLI terminal via `qrterminal`.
- **FR-004**: System MUST persist WhatsApp session credentials in `store.db` and auto-reconnect on gateway restart.
- **FR-005**: System MUST handle WhatsApp session invalidation by entering QR re-pairing mode.
- **FR-006**: System MUST support WhatsApp group messages with configurable trigger behavior (mention-only, prefix-based, or all messages).
- **FR-007**: System MUST reconnect to WhatsApp on network disconnect with exponential backoff (5s initial, 5m max, 2x multiplier).
- **FR-008**: System MUST emit QR code data as a WebSocket event (`channel.whatsapp.qr`) for headless/WebUI pairing.
- **FR-009**: System MUST implement browser automation using `chromedp` with managed mode (launch local Chromium).
- **FR-010**: System MUST provide 7 browser action tools: `browser.navigate`, `browser.click`, `browser.type`, `browser.screenshot`, `browser.get_text`, `browser.wait`, `browser.evaluate`.
- **FR-011**: System MUST return structured JSON results from all browser tools (not raw strings).
- **FR-012**: System MUST support remote CDP mode via configurable `tools.browser.cdp_url`.
- **FR-013**: System MUST enforce browser resource limits: page timeout (default 30s), max tabs (default 5), max memory per profile.
- **FR-014**: System MUST block browser navigation to non-HTTP(S) URL schemes (`file://`, `javascript:`, `data:`, `chrome://`).
- **FR-015**: System MUST apply SSRF protection (SEC-24) to browser navigation URLs.
- **FR-016**: System MUST subject `browser.evaluate` to tool allow/deny policy (SEC-04/SEC-06). It SHOULD be denied by default when `security.default_policy: "deny"`.
- **FR-017**: System MUST gracefully terminate managed Chromium processes on gateway shutdown.
- **FR-018**: System MUST launch Chromium with a dedicated user data directory isolated from the user's personal browser.
- **FR-019**: System SHOULD recover from corrupted WhatsApp SQLite databases by renaming the corrupt file and creating a fresh database.
- **FR-020**: System SHOULD warn via `omnipus doctor` when browser limits are too permissive for available system memory.
- **FR-021**: System MUST NOT bundle Chromium with the binary. Chromium is an optional user-installed dependency.
- **FR-022**: System MUST prioritize remote CDP when `tools.browser.cdp_url` is configured (over local managed mode).
- **FR-023**: System MUST set `MaxOpenConns(1)` and `MaxIdleConns(1)` on the WhatsApp SQLite connection to prevent write contention.
- **FR-024**: System MUST audit-log all browser tool invocations and WhatsApp connection state changes via SEC-15.

---

## Success Criteria

- **SC-001**: WhatsApp native channel auto-reconnects successfully after network interruption in >=99% of cases during a 72-hour soak test.
- **SC-002**: Browser `navigate + screenshot` completes in <2 seconds on localhost pages (managed mode, p95).
- **SC-003**: All 7 browser tool primitives return structured JSON results parseable by `encoding/json.Unmarshal`.
- **SC-004**: WhatsApp QR pairing completes within 60 seconds of QR code display (user scanning time excluded from measurement).
- **SC-005**: WhatsApp session persists across 10 consecutive gateway restarts without requiring re-pairing.
- **SC-006**: Browser SSRF protection blocks 100% of private IP navigation attempts (all ranges in test dataset).
- **SC-007**: Browser tab limit enforcement correctly blocks new tabs when at limit and allows when below limit in 100% of test cases.
- **SC-008**: Managed Chromium process is fully terminated (no orphan processes) within 5 seconds of gateway shutdown.
- **SC-009**: WhatsApp SQLite database uses zero CGo calls (verified by `go build` without CGo, no `import "C"` in dependency tree).
- **SC-010**: Browser tools register in ToolRegistry and appear in agent tool lists when `tools.browser.enabled: true`.

---

## Traceability Matrix

| Requirement | User Story | BDD Scenario(s) | Test Name(s) |
|-------------|-----------|--------------------------|-------------------------|
| FR-001 | US-1 | Receive text message; Send response | `TestWhatsAppInboundMessage`, `TestWhatsAppSendMessage` |
| FR-002 | US-1 | Fresh install QR pairing | `TestWhatsAppChannelStart_NoSession` |
| FR-003 | US-2 | Fresh install QR pairing | `TestWhatsAppChannelStart_NoSession` |
| FR-004 | US-2 | Auto-reconnect with existing session | `TestWhatsAppChannelStart_ExistingSession` |
| FR-005 | US-2 | Session invalidation and re-pairing | `TestWhatsAppChannelStart_ExistingSession` (variant) |
| FR-006 | US-3 | Group message with mention trigger; Group message without trigger ignored | `TestGroupTriggerWhatsApp` |
| FR-007 | US-1 | Network disconnect and reconnection | `TestWhatsAppReconnectBackoff` |
| FR-008 | US-2 | Headless QR pairing via WebSocket event | `TestWhatsAppChannelStart_NoSession` (headless variant) |
| FR-009 | US-4 | Launch managed Chromium | `TestBrowserNavigate_ManagedMode` |
| FR-010 | US-5 | Click element; Type text; Take screenshot; Get text; Wait for element; Evaluate JavaScript | `TestBrowserClick`, `TestBrowserType`, `TestBrowserScreenshot`, `TestBrowserGetText`, `TestBrowserWait_Found`, `TestBrowserEvaluate` |
| FR-011 | US-5 | All primitive scenarios | All `TestBrowser*` integration tests |
| FR-012 | US-6 | Connect via remote CDP URL | `TestBrowserNavigate_RemoteCDP` |
| FR-013 | US-7 | Page load timeout; Maximum tabs exceeded | `TestBrowserMaxTabs`, `TestPageTimeoutConfig` |
| FR-014 | US-4 | Scenario Outline: URL schemes | `TestURLSchemeValidation` |
| FR-015 | US-4 | SSRF protection blocks private IP | `TestBrowserSSRFBlock`, `TestSSRFURLCheck` |
| FR-016 | US-5 | Evaluate JavaScript denied by policy | `TestBrowserEvaluate_PolicyDenied` |
| FR-017 | US-4 | Graceful browser shutdown | `TestBrowserGracefulShutdown` |
| FR-018 | US-4 | Launch managed Chromium | `TestBrowserNavigate_ManagedMode` |
| FR-019 | US-2 | Corrupted SQLite database recovery | `TestCorruptedDBRecovery` |
| FR-020 | US-7 | (doctor integration, not directly tested in wave 4 scope) | — |
| FR-021 | US-4 | Chromium not found | `TestBrowserChromiumNotFound` |
| FR-022 | US-6 | Remote CDP takes precedence over local | `TestBrowserNavigate_RemoteCDP` |
| FR-023 | US-1 | Fresh install QR pairing | `TestWhatsAppChannelStart_NoSession` |
| FR-024 | US-4, US-1 | All scenarios (audit logging) | Covered by existing audit log integration tests |

---

## Ambiguity Warnings

| # | What's Ambiguous | Likely Agent Assumption | Question to Resolve |
|---|------------------|------------------------|---------------------|
| 1 | WhatsApp group mention detection — how does the agent detect it's been "mentioned" in a group? WhatsApp doesn't have @mentions like Telegram. | Agent uses push name or phone number substring match in message text. | Should mention detection be based on: (a) the message containing the bot's phone number, (b) a quoted reply to the bot's message, (c) the message containing a configurable trigger word, or (d) WhatsApp's native @mention feature (available in groups)? **Resolved: Use WhatsApp's native @mention JID matching (whatsmeow provides `evt.Info.MentionedJIDs`) plus configurable prefix triggers. Document this.** |
| 2 | Browser profile persistence — should cookies/localStorage persist across tool invocations within a session? Across sessions? Across restarts? | Agent assumes cookies persist within a session but not across restarts. | Decision: Cookies persist within a gateway lifecycle (session). Cleared on restart unless `tools.browser.persist_session: true`. **Accepted as default behavior.** |
| 3 | Browser `new_tab` behavior — how does the agent open a new tab vs reuse existing? Is there an explicit tab management API? | Agent assumes `browser.navigate` reuses the current tab. No explicit tab API. | Decision: `browser.navigate` reuses current tab by default. Add optional `new_tab: true` parameter to open in new tab. Add `browser.close_tab()` for explicit tab management. Defer `browser.list_tabs` to future wave. **Accepted.** |
| 4 | WhatsApp message types beyond text — what happens with images, voice, stickers, polls, reactions received before FUNC-25? | Agent ignores non-text messages silently. | Decision: Log unsupported message types at debug level. Do not publish to MessageBus. No error. **Accepted as specified in Edge Cases.** |
| 5 | Browser `max_memory_mb` enforcement mechanism — chromedp doesn't expose per-profile memory metrics. | Agent skips memory-based enforcement, only enforces tab limits and timeouts. | Decision: Memory enforcement is best-effort. Use Chrome's `--max-old-space-size` flag at launch for V8 heap, plus periodic check of OS-level process RSS. Log warning when threshold exceeded, close oldest inactive tab. Do not hard-kill the browser. **Accepted.** |
| 6 | WhatsApp `store.db` location — the BRD says `~/.omnipus/channels/whatsapp/session.db` but existing code uses `workspace/whatsapp/store.db`. | Agent uses whatever the existing code uses. | Decision: Use the config-driven path (`session_store_path`) with default `~/.omnipus/channels/whatsapp/store.db` (matching BRD). Existing code's default of `workspace/whatsapp/store.db` is a PicoClaw-ism that should be updated. **Accepted — use BRD path.** |

---

## Evaluation Scenarios (Holdout)

> **Note**: These scenarios are for post-implementation evaluation only.
> They must NOT be visible to the implementing agent during development.
> Do not reference these in the TDD plan or traceability matrix.

### Scenario: WhatsApp round-trip latency under load
- **Setup**: Connected WhatsApp session. 10 messages queued in rapid succession from a real phone.
- **Action**: Send 10 text messages to the agent in <5 seconds.
- **Expected outcome**: All 10 messages are received and responses sent within 30 seconds total. No messages dropped. No duplicate responses.
- **Category**: Happy Path

### Scenario: WhatsApp session survival across 24-hour period
- **Setup**: Paired WhatsApp session, gateway running continuously.
- **Action**: Leave gateway running for 24 hours with intermittent messages (every 2 hours).
- **Expected outcome**: All messages throughout the 24-hour period are received and responded to without re-pairing. Session remains valid.
- **Category**: Happy Path

### Scenario: Browser automation of a dynamic SPA
- **Setup**: Browser tools enabled, Chromium installed.
- **Action**: Navigate to a JavaScript-heavy single-page app (e.g., React app), wait for dynamic content to load, extract text from a dynamically-rendered element.
- **Expected outcome**: `browser.wait` correctly waits for the SPA to render. `browser.get_text` returns the dynamically-rendered content, not the initial HTML skeleton.
- **Category**: Happy Path

### Scenario: WhatsApp during network flap
- **Setup**: Connected WhatsApp session.
- **Action**: Simulate network disconnect (firewall rule) for 30 seconds, then restore.
- **Expected outcome**: Channel detects disconnect, logs reconnection attempts with increasing backoff, reconnects when network restored, and processes any queued inbound messages.
- **Category**: Error

### Scenario: Browser tool with invalid CSS selector syntax
- **Setup**: Page loaded in browser.
- **Action**: Call `browser.click("###invalid[[[")` with syntactically invalid CSS.
- **Expected outcome**: Tool returns a clear error about invalid selector syntax, not a generic timeout or crash.
- **Category**: Error

### Scenario: Concurrent browser and WhatsApp usage
- **Setup**: Both WhatsApp and browser tools enabled and active.
- **Action**: While a browser automation sequence is running (navigate + wait + screenshot), receive a WhatsApp message that requires a response.
- **Expected outcome**: WhatsApp message is processed independently. Browser automation completes uninterrupted. No resource contention or deadlock.
- **Category**: Edge Case

### Scenario: Gateway restart during active WhatsApp QR pairing
- **Setup**: WhatsApp channel is in QR pairing mode (QR displayed, not yet scanned).
- **Action**: Send SIGTERM to gateway.
- **Expected outcome**: Gateway shuts down gracefully within the configured timeout. No orphaned goroutines. No corrupted `store.db`. On restart, a new QR code is generated.
- **Category**: Edge Case

---

## Assumptions

- Chromium is not bundled with Omnipus. Users install Chromium separately via their system package manager, or use remote CDP mode.
- WhatsApp Web multi-device protocol remains accessible via `whatsmeow`. If Meta blocks reverse-engineered clients, WhatsApp integration is lost until the library adapts.
- `whatsmeow` library is pinned to a known stable version. Library updates require explicit version bumps and re-testing.
- `modernc.org/sqlite` adds ~10-15MB to the binary size. This is accepted because it avoids CGo.
- Browser action primitives do not handle iframes or shadow DOM in Wave 4. These are future enhancements.
- WhatsApp media handling (images, audio, documents) is explicitly out of scope (FUNC-25, future wave).
- The build tag `whatsapp_native` is retained for optional compilation. Users who don't need WhatsApp can build without it to save binary size.
- Remote CDP mode trusts the remote endpoint. Authentication to the CDP endpoint is the operator's responsibility (e.g., SSH tunnel, VPN).
- `browser.evaluate` returns JSON-serializable values only. DOM nodes, functions, and other non-serializable values return as `null`.

## Clarifications

### 2026-03-29

- Q: Should the WhatsApp native channel replace the bridge-based channel entirely? -> A: No. Both coexist. `use_native: true` selects native; `use_native: false` (default for backward compat) uses the bridge. The bridge remains for users with existing setups.
- Q: Should browser tools be registered as a single tool with sub-commands or as separate tools? -> A: Separate tools (`browser.navigate`, `browser.click`, etc.), each registered independently in the ToolRegistry. This allows per-method policy control (SEC-06) — operators can allow `browser.navigate` but deny `browser.evaluate`.
- Q: Should the browser persist across multiple agent sessions or be launched per-session? -> A: Browser persists across sessions within a gateway lifecycle. It launches on first use and shuts down with the gateway. This avoids the overhead of launching Chromium for every tool call.
- Q: Where are browser screenshots saved? -> A: Temp directory (`os.TempDir()`), with filenames like `omnipus-browser-<random>.png`. Cleaned up on gateway shutdown. Not persisted to workspace unless the agent explicitly copies them.
- Q: Is `browser.evaluate` on the default allow or deny list? -> A: It's a separate tool subject to policy. In `security.default_policy: "deny"` mode, it requires explicit allowance like any other tool. In the default `"allow"` mode, it's available unless explicitly denied. Operators are warned by `omnipus doctor` if evaluate is allowed without explicit policy review.
