# Spec: Joined Session Store — Multi-Agent Sessions (v2)

## Context

Currently each agent has its own isolated session store at `~/.omnipus/agents/{id}/sessions/`. When a handoff occurs (Mia → Ray), Ray starts a new session with no knowledge of the prior conversation. The user expects ONE continuous conversation where specialists participate.

**Goal:** Sessions belong to the USER, not the agent. One session can involve multiple agents. Each message is tagged with which agent sent it. On handoff, the target agent receives recent messages (token-budget-aware) + a summary of older context.

## Terminology

- **Session**: persistent storage unit — directory containing `meta.json` and `transcript.jsonl`. Spans the entire multi-agent conversation.
- **Conversation**: the LLM context window for a single agent turn. Each handoff starts a new conversation within the same session.
- **Handoff**: switching the active agent on a session. The session persists; only the active agent changes.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| History on handoff | Token-budget-aware (50% of target context window) | Fixed message count risks overflow or underuse |
| Summarization strategy | Tiered: LLM summary (5s timeout) → fallback to truncation | Balance quality vs reliability |
| Storage location | User-level: `$OMNIPUS_HOME/sessions/{id}/` | Shared across agents, clean separation |
| Concurrency model | Single shared `*UnifiedStore` instance + flock on meta.json | Prevents race conditions on concurrent writes |
| UI display | Single flat list with agent participation badges | User sees one conversation, not per-agent silos |
| Migration | New sessions only — old per-agent sessions read-only | Zero migration risk |
| System agent handoff | BLOCKED — handoff to/from `omnipus-system` rejected | System agent has incompatible tool model |
| Old session continuation | Frozen — old sessions are read-only, new messages start a new shared session | Clean cut, no mixed-state |
| Task sessions | Stay per-agent (task execution is agent-scoped) | Tasks are independent executions, not conversations |

---

## User Stories

### US-1: Shared Session Creation (P0)

When a user starts a new chat (via webchat or external channel), a session is created in the shared sessions directory. The session metadata tracks which agents have participated.

**Why P0:** Foundation — everything else depends on this.

**Independent Test:** Create a new session via webchat → verify `$OMNIPUS_HOME/sessions/{id}/meta.json` exists with `agent_ids: ["mia"]`.

**Acceptance Scenarios:**
1. **Given** no active session, **When** user sends first message, **Then** session is created at `$OMNIPUS_HOME/sessions/{id}/` with meta containing `agent_ids: [current_agent]` and `active_agent_id: current_agent`.
2. **Given** a shared session exists, **When** handoff switches to Ray, **Then** `agent_ids` in meta is updated to `["mia", "ray"]` and `active_agent_id` is `"ray"`.
3. **Given** a shared session, **When** a message is recorded, **Then** the transcript entry includes `agent_id` field (never empty).
4. **Given** two concurrent writes to the same session meta, **When** both complete, **Then** both changes are persisted and no data is lost (CRIT-002).

### US-2: Multi-Agent Transcript (P0)

Each message in the transcript JSONL is tagged with the `agent_id` that generated it. The UI renders messages with the agent's name/icon/color.

**Why P0:** Without agent tagging, multi-agent conversations are confusing.

**Independent Test:** Handoff from Mia to Ray → verify transcript.jsonl entries have `agent_id: "mia"` and `agent_id: "ray"` respectively.

**Acceptance Scenarios:**
1. **Given** Mia is active, **When** she sends a response, **Then** the transcript entry has `agent_id: "mia"`.
2. **Given** handoff to Ray, **When** Ray responds, **Then** the transcript entry has `agent_id: "ray"`.
3. **Given** return to Mia, **When** Mia responds, **Then** the entry has `agent_id: "mia"`.
4. **Given** a legacy transcript entry with no `agent_id`, **When** rendered in UI, **Then** `agent_id` is inferred from the session's original `AgentID` field.

### US-3: Context Transfer on Handoff (P0)

When a handoff occurs, the target agent receives as many recent messages as fit within 50% of its context window, plus a summary of older messages. The handoff completes within 10 seconds regardless of summarization outcome.

**Why P0:** This is the core value of joined sessions.

**Independent Test:** Have a 30-message conversation with Mia → handoff to Ray → Ray's context window contains recent messages + summary.

**Acceptance Scenarios:**
1. **Given** a session with messages totaling 20K tokens, **When** handoff to Ray (32K context), **Then** Ray receives messages fitting within 16K tokens (50%) + summary of older messages.
2. **Given** a session with 5 short messages (< 50% budget), **When** handoff, **Then** target agent receives all 5 messages (no summary needed).
3. **Given** handoff, **When** target agent processes the context, **Then** the system prompt switches to the target agent's SOUL but conversation history is preserved.
4. **Given** handoff, **When** LLM summarization times out after 5s, **Then** handoff proceeds with recent messages only + system note "[Earlier context summarization timed out — showing recent messages only]".
5. **Given** handoff, **When** LLM summarization call fails, **Then** same fallback as timeout — handoff still completes.
6. **Given** handoff to an agent whose provider is unreachable, **When** handoff executes, **Then** the session remains with the current agent and an error is displayed to the user.
7. **Given** a duplicate handoff request (double-click), **When** both fire, **Then** only one handoff is processed (idempotent).

### US-4: Session Panel — Single List with Agent Badges (P1)

The session panel shows a flat list of all sessions (shared + legacy). Each session displays badges for all agents that participated.

**Why P1:** UX improvement but not blocking for backend functionality.

**Acceptance Scenarios:**
1. **Given** sessions exist, **When** session panel opens, **Then** all sessions (shared + legacy) are in a single flat list sorted by last activity.
2. **Given** a session with Mia and Ray, **When** rendered in the list, **Then** both agents' icons/colors are shown as badges.
3. **Given** user clicks a session, **Then** all messages render with the correct agent's name/icon per message.
4. **Given** a session with only one agent, **When** rendered, **Then** agent badge is shown (no special handling needed).

### US-5: Backward Compatibility (P1)

Old per-agent sessions continue to work read-only. New messages always create shared sessions.

**Why P1:** Must not break existing data.

**Acceptance Scenarios:**
1. **Given** existing per-agent sessions, **When** app starts, **Then** old sessions are still accessible and functional (read-only).
2. **Given** old sessions, **When** listed in session panel, **Then** they appear alongside new shared sessions, deduplicated by ID.
3. **Given** an old session, **When** user opens it, **Then** messages render correctly (agent_id inferred from session's `AgentID`).
4. **Given** an old session, **When** user sends a new message, **Then** a NEW shared session is created (old session stays frozen).

---

## Concurrency Model (CRIT-002)

A **single `*UnifiedStore` instance** is created for the shared sessions directory at `$OMNIPUS_HOME/sessions/`. This instance is injected into ALL agents at initialization. The existing `sync.Mutex` on `UnifiedStore` serializes all in-process access to meta.json and transcript.jsonl.

As defense-in-depth for multi-process scenarios (desktop + CLI), advisory `flock` is acquired on `meta.json` before writes (matching the existing pattern in `pkg/fileutil/flock.go`).

The handoff tool and message recording both go through the same `UnifiedStore` instance — no cross-instance race is possible.

---

## Summarization Strategy (CRIT-001)

### Token-Budget-Aware Context Transfer

Replace the fixed "last 20 messages" with:

```
budget = target_agent.context_window * 0.50
system_prompt_tokens = estimate(target_agent.SOUL + tool_schemas)
available = budget - system_prompt_tokens

Transfer recent messages (newest first) until available tokens exhausted.
If older messages remain → attempt summarization.
```

### Tiered Summarization

1. **Attempt LLM summary** — call the target agent's model with prompt: "Summarize this conversation context in under 500 tokens: {older_messages}". Timeout: 5 seconds.
2. **On timeout or error** — fall back to: include a system note "[Earlier context (N messages) not shown — summarization unavailable]" and proceed with recent messages only.
3. **No summary needed** — if all messages fit within budget, skip summarization entirely.

### Constraints
- Maximum summary length: 500 tokens
- Summarization timeout: 5 seconds (hard)
- Handoff total timeout: 10 seconds (including summarization)
- Summarization model: target agent's configured model (same provider)

---

## Schema Migration (MAJ-001)

### SessionMeta v2

```go
type SessionMeta struct {
    ID             string        `json:"id"`
    AgentID        string        `json:"agent_id"`               // legacy: first/primary agent
    AgentIDs       []string      `json:"agent_ids,omitempty"`    // v2: all participating agents
    ActiveAgentID  string        `json:"active_agent_id,omitempty"` // v2: currently active
    Title          string        `json:"title"`
    Status         SessionStatus `json:"status"`
    CreatedAt      time.Time     `json:"created_at"`
    UpdatedAt      time.Time     `json:"updated_at"`
    Channel        string        `json:"channel"`
    Stats          SessionStats  `json:"stats"`
}
```

### Backward-Compatible Deserialization

Custom `UnmarshalJSON` or `PostLoad()` method:
```go
func (m *SessionMeta) PostLoad() {
    // Backfill AgentIDs from legacy AgentID
    if len(m.AgentIDs) == 0 && m.AgentID != "" {
        m.AgentIDs = []string{m.AgentID}
    }
    // Backfill ActiveAgentID
    if m.ActiveAgentID == "" && m.AgentID != "" {
        m.ActiveAgentID = m.AgentID
    }
}
```

`AgentID` remains serialized for backward compat. `AgentIDs` is `omitempty` so old sessions without it deserialize cleanly with nil (backfilled by `PostLoad`).

### TranscriptEntry — No omitempty

```go
type TranscriptEntry struct {
    // ... existing fields ...
    AgentID string `json:"agent_id"` // NO omitempty — always present
}
```

### Call Sites That Must Set AgentID

| File | Function | Line |
|---|---|---|
| `pkg/session/unified.go` | `AppendTranscript` | ~223 |
| `pkg/session/daypartition.go` | `AppendMessage` | ~180 |
| `pkg/agent/loop.go` | `recordAssistantMessage` | ~3850 |
| `pkg/agent/loop.go` | `recordUserMessage` | ~2570 |
| `pkg/gateway/websocket.go` | WebSocket replay | ~670 |

---

## ListAllSessions Algorithm (MAJ-002)

```
1. Read all sessions from $OMNIPUS_HOME/sessions/ (shared store)
2. Build set of known IDs from step 1
3. For each registered agent, scan $OMNIPUS_HOME/agents/{id}/sessions/
   - Skip sessions whose ID is already in the shared set (dedup)
   - Add remaining (legacy) sessions to the list
4. Sort merged list by UpdatedAt descending
5. Return
```

This ensures:
- Shared sessions take precedence (newer model)
- Legacy sessions appear but are not duplicated
- Performance: shared store read is O(1), legacy scan is O(agents × sessions)

---

## Handoff Failure Handling (MAJ-004)

### Pre-validation

Before switching `ActiveAgentID`:
1. Verify target agent exists in registry
2. Verify target agent's provider is configured (model exists in config)
3. If validation fails → return error to user, session stays with current agent

### Rollback

The handoff sequence is:
1. Validate target agent ✓
2. Read transcript for context transfer ✓
3. Attempt summarization (with timeout/fallback) ✓
4. Update `ActiveAgentID` in meta.json (atomic write)
5. Send `agent_switched` WebSocket frame

If step 4 fails (disk error) → session stays with current agent, error returned.
If step 5 fails (WebSocket disconnected) → meta is updated but UI doesn't reflect. Next page load will show correct agent.

### Idempotency

The handoff tool checks if `ActiveAgentID == target_agent_id` before proceeding. If already handed off to the target, return success without re-executing.

---

## System Agent Handoff (MAJ-005)

**Decision: BLOCKED.** Handoff to/from `omnipus-system` is rejected.

The system agent has 35 exclusive tools and a fundamentally different operating model. Its tool-heavy conversation history is meaningless to chat agents, and chat context is irrelevant to the system agent.

Implementation: `HandoffTool.Execute` checks `if agentID == "omnipus-system" { return error }`.

Note: The system agent was removed in #45 (Core Agent Roster v1). This constraint is a safety net in case it's re-added.

---

## Mid-Stream Handoff Behavior

If a handoff is triggered while the current agent is still generating a response:
1. The current response completes (not interrupted)
2. The "done" frame is sent
3. The `agent_switched` frame follows
4. Next user message goes to the new agent

Rationale: interrupting mid-stream loses partial work and creates confusing UX. Let the current turn finish, then switch.

---

## Context Compaction Interaction

The existing two-layer context compression (tool result pruning + conversation compaction with `LastCompactionSummary`) operates per-agent. On handoff:
- The compaction state is NOT carried over (it's agent-specific)
- The target agent starts with a fresh compaction state
- The transferred messages are the post-compaction view from the source agent

---

## Existing Codebase Context

### Symbols Involved

| Symbol | Role | Context |
|--------|------|---------|
| `session.UnifiedStore` | Modifies | Single shared instance for `$OMNIPUS_HOME/sessions/` |
| `session.SessionMeta` | Modifies | Add `AgentIDs`, `ActiveAgentID` with `PostLoad()` backfill |
| `session.TranscriptEntry` | Modifies | Add `AgentID string` (no omitempty) |
| `agent.AgentInstance.Sessions` | Modifies | All agents share one `*UnifiedStore` instance |
| `agent.AgentLoop.sharedSessionStore` | New field | The single shared store instance |
| `agent.AgentLoop.ListAllSessions` | Modifies | Merge shared + per-agent stores, dedup by ID |
| `gateway.websocket.handleChatMessage` | Modifies | Creates session in shared dir |
| `gateway.rest.HandleSessions` | Modifies | Reads from shared store + legacy merge |
| `tools.HandoffTool.Execute` | Modifies | Switches agent on existing session, context transfer |

### Impact Assessment

| Symbol Modified | Risk Level | Direct Dependents | Notes |
|----------------|------------|-------------------|-------|
| `UnifiedStore` | HIGH | All session read/write paths | Single instance mitigates concurrency risk |
| `SessionMeta` | MEDIUM | Session listing, session panel | `PostLoad()` handles migration |
| `TranscriptEntry` | LOW | Transcript recording, replay | Additive field |
| `AgentInstance.Sessions` | HIGH | WebSocket handler, task execution | Must inject shared store |

---

## Functional Requirements

| ID | Requirement |
|---|---|
| FR-001 | System MUST create new sessions in `$OMNIPUS_HOME/sessions/{id}/` |
| FR-002 | System MUST tag each transcript entry with the generating agent's ID (never empty) |
| FR-003 | System MUST track all participating agent IDs in session metadata |
| FR-004 | System MUST transfer messages fitting within 50% of target agent's context window on handoff |
| FR-005 | System MUST switch ActiveAgentID in session meta on handoff |
| FR-006 | System MUST preserve existing per-agent sessions (read-only accessible) |
| FR-007 | System MUST display agent participation badges in session list |
| FR-008 | System MUST render per-message agent identity in chat view |
| FR-009 | System MUST complete handoff within 10 seconds regardless of summarization outcome |
| FR-010 | System MUST use a single UnifiedStore instance for the shared session directory |
| FR-011 | Context transfer MUST NOT exceed 50% of the target agent's context window budget |
| FR-012 | System MUST validate target agent availability before updating ActiveAgentID |
| FR-013 | System MUST reject handoff requests targeting the system agent |
| FR-014 | System MUST deduplicate sessions when merging shared and legacy stores (by session ID) |

## Success Criteria

| ID | Criterion |
|---|---|
| SC-001 | Handoff from Mia to Ray preserves conversation continuity — Ray's context contains the recent messages from the session and can reference them |
| SC-002 | Session panel shows a single flat list with agent badges for all participating agents |
| SC-003 | Old per-agent sessions remain accessible and visible after upgrade |
| SC-004 | No data loss during the transition — zero sessions go missing |
| SC-005 | New session creation completes in <50ms wall-clock time at p99 on a single-core system with SSD storage, measured from API call entry to meta.json write completion |
| SC-006 | Handoff completes in <10 seconds including summarization (or fallback) |

---

## Out of Scope

- Migrating old sessions to new format (old sessions stay frozen in per-agent dirs)
- Real-time collaborative editing (two agents responding simultaneously)
- Session merging (combining two separate sessions into one)
- Cross-device session sync
- Task session sharing (task sessions remain per-agent)
- Handoff to/from the system agent
- Session title regeneration after handoff
