# Spec: Joined Session Store — Multi-Agent Sessions

## Context

Currently each agent has its own isolated session store at `~/.omnipus/agents/{id}/sessions/`. When a handoff occurs (Mia → Ray), Ray starts a new session with no knowledge of the prior conversation. The user expects ONE continuous conversation where specialists participate.

**Goal:** Sessions belong to the USER, not the agent. One session can involve multiple agents. Each message is tagged with which agent sent it. On handoff, the target agent receives the last N messages + a summary of older context.

## Design Decisions (from requirements interview)

| Decision | Choice |
|---|---|
| History on handoff | Last N messages (20) + summary of older context |
| Storage location | User-level: `~/.omnipus/sessions/{id}/` (shared) |
| UI display | Single flat list with agent participation badges |
| Migration | New sessions only — old per-agent sessions stay as-is |

---

## User Stories

### US-1: Shared Session Creation (P0)

When a user starts a new chat (via webchat or external channel), a session is created in the shared sessions directory. The session metadata tracks which agents have participated.

**Why P0:** Foundation — everything else depends on this.

**Independent Test:** Create a new session via webchat → verify `~/.omnipus/sessions/{id}/meta.json` exists with `agent_ids: ["mia"]`.

**Acceptance Scenarios:**
1. **Given** no active session, **When** user sends first message, **Then** session is created at `~/.omnipus/sessions/{id}/` with meta containing `agent_ids: [current_agent]`.
2. **Given** a shared session exists, **When** handoff switches to Ray, **Then** `agent_ids` in meta is updated to `["mia", "ray"]`.
3. **Given** a shared session, **When** a message is recorded, **Then** the transcript entry includes `agent_id` field.

### US-2: Multi-Agent Transcript (P0)

Each message in the transcript JSONL is tagged with the `agent_id` that generated it. The UI renders messages with the agent's name/icon/color.

**Why P0:** Without agent tagging, multi-agent conversations are confusing.

**Independent Test:** Handoff from Mia to Ray → verify transcript.jsonl entries have `agent_id: "mia"` and `agent_id: "ray"` respectively.

**Acceptance Scenarios:**
1. **Given** Mia is active, **When** she sends a response, **Then** the transcript entry has `agent_id: "mia"`.
2. **Given** handoff to Ray, **When** Ray responds, **Then** the transcript entry has `agent_id: "ray"`.
3. **Given** return to Mia, **When** Mia responds, **Then** the entry has `agent_id: "mia"`.

### US-3: Context Transfer on Handoff (P0)

When a handoff occurs, the target agent receives the last 20 messages from the session plus a summary of older context. The agent's system prompt is switched but the conversation continues.

**Why P0:** This is the core value of joined sessions.

**Independent Test:** Have a 30-message conversation with Mia → handoff to Ray → Ray's context window contains last 20 messages + summary.

**Acceptance Scenarios:**
1. **Given** a session with 30 messages, **When** handoff to Ray, **Then** Ray receives messages 11-30 in full + a summary of messages 1-10.
2. **Given** a session with 5 messages, **When** handoff, **Then** target agent receives all 5 messages (no summary needed).
3. **Given** handoff, **When** target agent processes the context, **Then** the system prompt switches to the target agent's SOUL but conversation history is preserved.

### US-4: Session Panel — Single List with Agent Badges (P1)

The session panel shows a flat list of all sessions. Each session displays badges for all agents that participated. No more per-agent grouping.

**Why P1:** UX improvement but not blocking for backend functionality.

**Independent Test:** Open session panel → sessions show agent participation badges → clicking opens the full multi-agent conversation.

**Acceptance Scenarios:**
1. **Given** sessions exist, **When** session panel opens, **Then** all sessions are in a single flat list sorted by last activity.
2. **Given** a session with Mia and Ray, **When** rendered in the list, **Then** both agents' icons/colors are shown as badges.
3. **Given** user clicks a session, **Then** all messages render with the correct agent's name/icon per message.

### US-5: Backward Compatibility (P1)

Old per-agent sessions continue to work. New sessions use the shared model. No migration required.

**Why P1:** Must not break existing data.

**Acceptance Scenarios:**
1. **Given** existing per-agent sessions, **When** app starts, **Then** old sessions are still accessible and functional.
2. **Given** old sessions, **When** listed in session panel, **Then** they appear alongside new shared sessions.
3. **Given** an old session, **When** user opens it, **Then** messages render correctly (agent_id inferred from session's AgentID).

---

## Existing Codebase Context

### Symbols Involved

| Symbol | Role | Context |
|--------|------|---------|
| `session.UnifiedStore` | Modifies | Per-agent store → shared store |
| `session.SessionMeta` | Modifies | Add `AgentIDs []string`, `ActiveAgentID string` |
| `session.TranscriptEntry` | Modifies | Add `AgentID string` field |
| `agent.AgentInstance.Sessions` | Modifies | Points to shared store instead of per-agent |
| `agent.AgentLoop.GetAgentStore` | Modifies | Becomes `GetSessionStore` (agent-agnostic) |
| `agent.AgentLoop.ResolveSessionStore` | Modifies | Simplified — one shared store |
| `agent.AgentLoop.ListAllSessions` | Modifies | Reads from shared dir instead of aggregating |
| `gateway.websocket.handleChatMessage` | Modifies | Creates session in shared dir |
| `gateway.rest.HandleSessions` | Modifies | Reads from shared dir |
| `tools.HandoffTool.Execute` | Modifies | Switches agent on existing session |

### Impact Assessment

| Symbol Modified | Risk Level | Direct Dependents |
|----------------|------------|-------------------|
| `UnifiedStore` | HIGH | All session read/write paths |
| `SessionMeta` | MEDIUM | Session listing, session panel |
| `TranscriptEntry` | LOW | Transcript recording, replay |
| `GetAgentStore` | HIGH | WebSocket handler, task execution |

---

## Implementation Steps

### Step 1: Shared Session Directory

- Create `~/.omnipus/sessions/` on boot (alongside existing `agents/`, `workspace/`, etc.)
- `datamodel.Init()` creates this directory

### Step 2: Update SessionMeta

```go
type SessionMeta struct {
    ID             string
    AgentIDs       []string      // all agents that participated
    ActiveAgentID  string        // currently active agent
    Title          string
    Status         SessionStatus
    CreatedAt      time.Time
    UpdatedAt      time.Time
    Channel        string
    Stats          SessionStats
}
```

Keep `AgentID string` as a computed property (first agent) for backward compat.

### Step 3: Tag TranscriptEntry with AgentID

```go
type TranscriptEntry struct {
    // ... existing fields ...
    AgentID string `json:"agent_id,omitempty"` // which agent generated this message
}
```

### Step 4: Shared UnifiedStore

Instead of per-agent stores, create ONE shared store:
- Root: `~/.omnipus/sessions/`
- All sessions under this root
- Agent-specific context (SOUL, tools) comes from the agent registry, not the store

### Step 5: Context Transfer on Handoff

When handoff tool fires:
1. Read last 20 transcript entries from the session
2. If there are older entries, generate a summary (via LLM call or simple truncation)
3. Switch `ActiveAgentID` in session meta
4. The agent loop's context builder picks up the session's transcript for the new agent

### Step 6: Session Panel Update

Frontend changes:
- Fetch sessions from single `/api/v1/sessions` endpoint (already aggregates)
- Remove per-agent grouping (AccordionItem per agent)
- Show flat list with agent participation badges per session
- Each message in chat view shows agent icon/name

### Step 7: Backward Compatibility

- On boot: check if `~/.omnipus/sessions/` exists
- Old sessions in `~/.omnipus/agents/{id}/sessions/` are read-only accessible
- `ListAllSessions` merges both shared and per-agent sessions
- New sessions always go to shared dir

---

## Functional Requirements

| ID | Requirement |
|---|---|
| FR-001 | System MUST create new sessions in `~/.omnipus/sessions/{id}/` |
| FR-002 | System MUST tag each transcript entry with the generating agent's ID |
| FR-003 | System MUST track all participating agent IDs in session metadata |
| FR-004 | System MUST transfer last 20 messages + summary on handoff |
| FR-005 | System MUST switch ActiveAgentID in session meta on handoff |
| FR-006 | System MUST preserve existing per-agent sessions (read-only) |
| FR-007 | System SHOULD display agent participation badges in session list |
| FR-008 | System SHOULD render per-message agent identity in chat view |

## Success Criteria

| ID | Criterion |
|---|---|
| SC-001 | Handoff from Mia to Ray preserves conversation continuity — Ray sees prior context |
| SC-002 | Session panel shows a single flat list with agent badges |
| SC-003 | Old per-agent sessions remain accessible after upgrade |
| SC-004 | No data loss during the transition — zero sessions go missing |
| SC-005 | New session creation takes <50ms (no performance regression) |

---

## Out of Scope

- Migrating old sessions to new format
- Real-time collaborative editing (two agents responding simultaneously)
- Session merging (combining two separate sessions)
- Cross-device session sync
