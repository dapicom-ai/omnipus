# Adversarial Review: Joined Session Store — Multi-Agent Sessions (v2)

**Spec reviewed**: docs/specs/joined-session-store-spec.md
**Review date**: 2026-04-12
**Verdict**: REVISE

## Executive Summary

The v2 spec addresses many gaps from the prior review (summarization strategy, concurrency model, system agent blocking, schema migration) and is substantially improved. However, two critical findings remain: the `UnifiedStore` constructor takes a per-agent `agentID` that is baked into session creation, and the spec does not specify how a single shared store handles this; and the `HandoffTool` in the current codebase has no access to the session store, no context transfer logic, and no meta.json update capability, making the spec's "Modifies" label severely misleading. Six major findings cover unspecified injection paths, missing API surface changes, and a flock claim that does not match reality.

| Severity | Count |
|----------|-------|
| CRITICAL | 2 |
| MAJOR | 6 |
| MINOR | 4 |
| OBSERVATION | 3 |
| **Total** | **15** |

---

## Findings

### CRITICAL Findings

#### [CRIT-001] UnifiedStore.agentID is per-agent — shared store constructor is unspecified

- **Lens**: Incorrectness
- **Affected section**: Concurrency Model (CRIT-002), Existing Codebase Context table
- **Description**: The spec states "A single `*UnifiedStore` instance is created for the shared sessions directory at `$OMNIPUS_HOME/sessions/`." However, `NewUnifiedStore(baseDir, agentID string)` takes a fixed `agentID` parameter that is used in `NewSession()` to set `meta.AgentID` unconditionally (line 158 of `pkg/session/unified.go`). If a single shared store is created, which `agentID` is passed to the constructor? Currently each agent gets its own `UnifiedStore` via `initSessionStore(dir, agentID)` in `pkg/agent/instance.go:365`. The spec's `SessionMeta v2` schema adds `AgentIDs` and `ActiveAgentID` but does not specify how `NewSession()` is modified, what `agentID` to pass to the shared store constructor, or how `AppendTranscript` knows which agent is writing.
- **Impact**: An implementer will either (a) pass an empty `agentID` to the shared store, breaking all existing code that reads `meta.AgentID`, or (b) pass the default agent ID, causing all sessions to incorrectly show the default agent as the creator regardless of which agent actually created them.
- **Recommendation**: Specify that `NewUnifiedStore` for the shared store is constructed with an empty `agentID` (or remove the field entirely from the shared store variant). Then specify that `NewSession()` must accept the creating agent's ID as a parameter: `NewSession(sessionType UnifiedSessionType, channel string, creatingAgentID string) (*UnifiedMeta, error)`. The method sets both `AgentID` and `AgentIDs: [creatingAgentID]` and `ActiveAgentID: creatingAgentID`. Similarly, `AppendTranscript` callers must set `TranscriptEntry.AgentID` before calling (not derived from the store).

---

#### [CRIT-002] HandoffTool gap is a rewrite, not a modification — dependencies and access paths unspecified

- **Lens**: Incompleteness
- **Affected section**: Handoff Failure Handling (MAJ-004), Existing Codebase Context table
- **Description**: The current `HandoffTool.Execute` in `pkg/tools/handoff.go:72-112` does three things: validate agent exists, call `setActive(sessionKey, agentID)`, and notify frontend. It does NOT: update `meta.json` with `ActiveAgentID` or `AgentIDs`, read transcript for context transfer, perform token-budget-aware message selection, attempt summarization, enforce the 10-second timeout, check for system agent, or implement idempotency. The tool has no access to any `*UnifiedStore` instance — its dependencies are `getRegistry`, `setActive` (a callback), and `onHandoff` (a callback). The spec says "Modifies" in the Existing Codebase Context table, but every capability described in the Summarization Strategy and Handoff Failure Handling sections requires new dependencies injected into the tool.
- **Impact**: An implementer will not understand the scope of the change, will miss injecting the session store and model config into the handoff tool, and will likely implement context transfer incorrectly or not at all.
- **Recommendation**: (1) Add a "Modified HandoffTool Dependencies" section specifying the new constructor: `NewHandoffTool` must receive the shared `*UnifiedStore`, a function to resolve target agent context window size, and access to the target agent's LLM provider for summarization calls. Provide the new signature. (2) Provide pseudocode for the full `Execute` replacement covering: idempotency check, system agent block, pre-validation, transcript read, token-budget context selection, tiered summarization, atomic meta update, WebSocket notification, and the 10-second overall timeout. (3) Specify how the tool gets the target agent's context window — from `AgentConfig`, the model registry, or the provider.

---

### MAJOR Findings

#### [MAJ-001] Token estimation for context transfer is unspecified

- **Lens**: Ambiguity
- **Affected section**: Summarization Strategy (CRIT-001), FR-004, FR-011
- **Description**: The pseudocode says `budget = target_agent.context_window * 0.50` and `system_prompt_tokens = estimate(target_agent.SOUL + tool_schemas)` but does not specify: (a) where `target_agent.context_window` comes from — is it a config field, a model registry lookup, or hardcoded per model? The codebase has `isOverContextBudget` in `pkg/agent/context_budget.go` which accepts `contextWindow int`, but no model-to-context-window mapping is documented. (b) What `estimate()` function is used — `estimateMessageTokens` (2.5 chars/token) and `estimateToolDefsTokens` exist in `pkg/agent/context_budget.go` but are not referenced. (c) How transcript entries map to token counts — the `TranscriptEntry.Tokens` field may be 0 for old entries.
- **Impact**: Two implementers would produce different token budgets for the same handoff.
- **Recommendation**: Specify: (1) Context window resolved from the model configuration (add the lookup path). (2) Token estimation reuses `estimateMessageTokens` from `pkg/agent/context_budget.go`. (3) If `TranscriptEntry.Tokens > 0`, use the stored value; otherwise re-estimate from content. (4) Tool schema tokens estimated via `estimateToolDefsTokens`.

---

#### [MAJ-002] flock on meta.json is claimed as existing but does not exist

- **Lens**: Incorrectness
- **Affected section**: Concurrency Model (CRIT-002) — "advisory `flock` is acquired on `meta.json` before writes (matching the existing pattern in `pkg/fileutil/flock.go`)"
- **Description**: A search of `pkg/session/` reveals zero flock usage. The `pkg/fileutil/flock_unix.go` and `flock_windows.go` files exist with the primitives, but `writeUnifiedMeta` in `pkg/session/unified.go:470` uses only `fileutil.WriteFileAtomic` (temp file + rename) and acquires no file lock. The phrase "matching the existing pattern" implies this is already implemented.
- **Impact**: An implementer might skip adding flock thinking it already exists, or add it without understanding the lock ordering relative to the in-process mutex.
- **Recommendation**: Change the statement to: "NEW: acquire advisory flock on `meta.json` before writes using `fileutil.Flock`/`fileutil.Unflock` from `pkg/fileutil/flock_unix.go`." Specify lock ordering: always acquire the in-process `sync.Mutex` first, then flock. Specify the exact function to modify (`writeUnifiedMeta`) and the handoff tool's meta update path.

---

#### [MAJ-003] ListAllSessions shared store injection unspecified

- **Lens**: Incompleteness
- **Affected section**: ListAllSessions Algorithm (MAJ-002), Existing Codebase Context table
- **Description**: The spec's algorithm says "Read all sessions from `$OMNIPUS_HOME/sessions/` (shared store)" as step 1, then iterate per-agent stores for legacy sessions. But `ListAllSessions` in `pkg/agent/loop.go:1762` discovers stores via `al.GetRegistry().ListAgentIDs()` and `al.GetAgentStore(id)`. A shared store has no agent ID and is not in the registry. The spec says `agent.AgentLoop.sharedSessionStore` is a "New field" but does not specify: (a) how it is initialized and injected, (b) whether `GetAgentStore` still returns per-agent stores or is replaced, (c) the type compatibility — the shared store returns `*UnifiedMeta` but per-agent `PartitionStore.ListSessions()` returns `*SessionMeta`.
- **Impact**: The implementer must design the injection path, handle the type mismatch between `UnifiedMeta` and `SessionMeta`, and decide the initialization order — all with no spec guidance.
- **Recommendation**: Add a "Modified AgentLoop" section specifying: (1) New field `sharedStore *session.UnifiedStore` on `AgentLoop`, set during `NewAgentLoop`. (2) `ListAllSessions` reads from `sharedStore` first, then iterates per-agent stores for legacy-only sessions. (3) Legacy `*SessionMeta` values are wrapped in `*UnifiedMeta` for the return type (or specify a common interface).

---

#### [MAJ-004] WebSocket session creation path unspecified

- **Lens**: Incompleteness
- **Affected section**: Call Sites That Must Set AgentID, US-1 Acceptance Scenario 1
- **Description**: US-1 says "When user sends first message, session is created at `$OMNIPUS_HOME/sessions/{id}/`." Currently, the WebSocket handler creates sessions via the agent's per-agent store (`agent.Sessions.NewSession()`). The spec does not specify: (a) how the WebSocket handler obtains the shared store reference, (b) whether session creation delegates through the agent or goes directly to the shared store, (c) how the initial `agent_ids` and `active_agent_id` are set during creation (see CRIT-001).
- **Impact**: The WebSocket handler is the primary session creation path. Without specifying the interaction, the most critical flow is left to guesswork.
- **Recommendation**: Add a "Session Creation Flow" section: WebSocket handler calls `sharedStore.NewSession(type, channel, activeAgentID)` where `activeAgentID` is resolved from the current routing. Specify that the handler obtains `sharedStore` from the `AgentLoop` or a new dependency injection path.

---

#### [MAJ-005] MetaPatch does not include multi-agent fields

- **Lens**: Incompleteness
- **Affected section**: Schema Migration (MAJ-001), Handoff Failure Handling (MAJ-004)
- **Description**: The handoff sequence requires updating `ActiveAgentID` and appending to `AgentIDs` in `meta.json`. The current `MetaPatch` struct (`pkg/session/unified.go:32-36`) only has `Title`, `Status`, and `TaskID`. The spec defines the new schema fields but does not specify the API surface for updating them — through `MetaPatch` extension, a new dedicated method, or direct read-modify-write.
- **Impact**: Each approach has different concurrency implications. Extending `MetaPatch` overloads a simple struct; direct manipulation bypasses the existing update pattern.
- **Recommendation**: Specify a new method `SwitchAgent(sessionID, newAgentID string) error` on `UnifiedStore` that: acquires mutex + flock, reads meta, sets `ActiveAgentID`, appends to `AgentIDs` if not present, updates `UpdatedAt`, writes atomically. This is cleaner than extending `MetaPatch` for a specialized operation.

---

#### [MAJ-006] Duplicate handoff idempotency check has no data access path

- **Lens**: Ambiguity
- **Affected section**: US-3 Acceptance Scenario 7, Handoff Failure Handling — Idempotency
- **Description**: The spec says "The handoff tool checks if `ActiveAgentID == target_agent_id` before proceeding." But `HandoffTool` currently has no access to `ActiveAgentID` — it has `getRegistry`, `setActive` (write-only callback), and `onHandoff`. Reading the current active agent requires access to the session store or a new getter callback. The spec does not specify which.
- **Impact**: The idempotency check cannot be implemented without a new dependency or access path.
- **Recommendation**: Specify that the idempotency check is part of the `SwitchAgent` method (MAJ-005): the method reads `ActiveAgentID` under the lock and returns a sentinel error (e.g., `ErrAlreadyActive`) if it already matches the target. `HandoffTool` treats this as success.

---

### MINOR Findings

#### [MIN-001] Spec SessionMeta v2 omits existing fields from the struct

- **Lens**: Inconsistency
- **Affected section**: Schema Migration (MAJ-001) — SessionMeta v2 code block
- **Description**: The spec's `SessionMeta v2` struct omits `Model`, `Provider`, `ProjectID`, `TaskID`, `Partitions`, and `LastCompactionSummary` fields that exist in the current struct (`pkg/session/daypartition.go:68-83`). This could mislead an implementer into thinking these fields are removed.
- **Recommendation**: Either show the complete struct with all fields (existing + new), or add a note: "Fields not shown are unchanged from the current definition."

---

#### [MIN-002] Summary length enforcement unspecified

- **Lens**: Ambiguity
- **Affected section**: Summarization Strategy — Constraints: "Maximum summary length: 500 tokens"
- **Description**: The spec sets a 500-token max for summaries but does not specify whether this is enforced via `max_tokens` on the LLM call, post-generation truncation, or prompt instruction.
- **Recommendation**: Specify: "Set `max_tokens: 500` on the summarization LLM call. If the response exceeds 500 tokens, truncate to the last complete sentence within the limit."

---

#### [MIN-003] Channel field semantics on handoff unclear

- **Lens**: Ambiguity
- **Affected section**: Schema Migration (MAJ-001)
- **Description**: `SessionMeta` has a `Channel` field. The spec does not address whether the channel changes on handoff. Since sessions are user-scoped, the channel should remain the originating channel, but this is unstated.
- **Recommendation**: Add: "The `Channel` field reflects the originating channel and does not change on handoff."

---

#### [MIN-004] Call site line numbers will drift

- **Lens**: Infeasibility
- **Affected section**: Call Sites That Must Set AgentID table
- **Description**: The table references approximate line numbers (`~223`, `~180`, `~3850`, `~2570`, `~670`) that will become stale as the codebase evolves.
- **Recommendation**: Remove the "Line" column. The function names are sufficient for locating call sites.

---

### Observations

#### [OBS-001] PartitionStore vs UnifiedStore type mismatch in ListAllSessions

- **Lens**: Incompleteness
- **Affected section**: ListAllSessions Algorithm (MAJ-002)
- **Suggestion**: `PartitionStore.ListSessions()` returns `[]*SessionMeta` while `UnifiedStore.ListSessions()` returns `[]*UnifiedMeta`. The merge algorithm must handle this type difference. Consider defining a common interface or wrapping `SessionMeta` in `UnifiedMeta` during the merge.

---

#### [OBS-002] Day-partitioned transcripts not addressed in shared store

- **Lens**: Incompleteness
- **Affected section**: US-2, Schema Migration
- **Suggestion**: The existing `PartitionStore` uses day-partitioned JSONL files (`sessions/<id>/<YYYY-MM-DD>.jsonl`). The shared `UnifiedStore` uses a single `transcript.jsonl`. For long-running multi-agent sessions, clarify whether day partitioning is preserved or consolidated. If single file: what is the growth bound?

---

#### [OBS-003] ReturnToDefaultTool interaction with shared session model

- **Lens**: Incompleteness
- **Affected section**: Existing Codebase Context
- **Suggestion**: `pkg/tools/handoff.go` also defines `ReturnToDefaultTool` which clears the agent override by calling `setActive(sessionKey, "")`. Should this also update `ActiveAgentID` in `meta.json`? Should it be treated as a handoff for context transfer purposes? The spec should address this tool.

---

## Structural Integrity

| Check | Result | Notes |
|-------|--------|-------|
| Every goal/objective has acceptance criteria | PASS | All 5 user stories have acceptance scenarios |
| Cross-references are consistent | FAIL | Existing Codebase Context says "Modifies" for HandoffTool but the gap is a rewrite (CRIT-002). Schema v2 omits fields present in codebase (MIN-001) |
| Scope boundaries are explicit | PASS | "Out of Scope" section is clear and comprehensive |
| Success criteria are measurable | PASS | SC-001 through SC-006 have concrete thresholds (SC-005 could be tighter per the prior review) |
| Error/failure scenarios addressed | PARTIAL | Handoff failures well-covered. Session creation failures and concurrent write failures during shared store access are not covered |
| Dependencies between requirements identified | FAIL | FR-010 (single UnifiedStore) is prerequisite for FR-003, FR-004, FR-005, FR-014 but dependency ordering is not stated |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Concurrency | No test for two agents writing to the same session's transcript simultaneously | US-1 AC4, Concurrency Model |
| Multi-process | No test for flock coordination between desktop + CLI processes | Concurrency Model |
| Token budget edge cases | No test for context window = 0, very small windows, or models without known context window | US-3 AC1, FR-004 |
| Summarization failure modes | No test for LLM returning malformed summary, summary > 500 tokens, or partial response before timeout | US-3 AC4, AC5 |
| Legacy session enumeration at scale | No performance test with many per-agent sessions across many agents | ListAllSessions O(agents x sessions) |
| Mid-stream handoff | No test for the mid-stream behavior (response completes before switch) | Mid-Stream Handoff Behavior |
| ReturnToDefaultTool | No test for return-to-default updating meta.json | OBS-003 |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| Context transfer | Empty session (0 messages) | Test handoff on brand-new session with no messages |
| Context transfer | Single message exactly at 50% budget | Test token count exactly at the budget boundary |
| Context transfer | All messages fit within budget (no summary needed) | Verify summarization is skipped entirely |
| Agent IDs | 100+ agents in one session | Test AgentIDs deduplication and serialization at scale |
| Legacy migration | meta.json with AgentID but no AgentIDs/ActiveAgentID | Test PostLoad backfill produces correct values |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| Shared session store | ok | risk | ok | ok | risk | ok | T: flock claimed but not implemented (MAJ-002), multi-process corruption possible. D: single mutex serializes all agents' writes |
| Handoff tool | ok | ok | risk | ok | ok | risk | R: no audit log entry specified for handoff events. E: no permission check — any agent can handoff to any other agent |
| Context transfer / summarization | ok | ok | ok | risk | risk | ok | I: conversation history sent to LLM for summary may contain sensitive data. D: 5s timeout but no rate limit on handoff attempts |
| ListAllSessions merge | ok | ok | ok | ok | risk | ok | D: O(agents x sessions) with no pagination — large deployments may timeout |
| WebSocket agent_switched frame | risk | ok | ok | ok | ok | ok | S: no spec that the frame includes auth verification — could a client inject a spoofed switch event? |

---

## Unasked Questions

1. **How is the shared `*UnifiedStore` injected into `AgentLoop` and `AgentInstance` objects?** The current initialization creates per-agent stores in `initSessionStore`. What is the new initialization sequence?

2. **What happens to the `UnifiedStore.agentID` field in the shared store?** Is it removed, set to empty string, or set to a sentinel? This affects `migrateLegacy`, `NewSession`, and all code reading `us.agentID`.

3. **How does the REST API's `HandleAgentSessions` endpoint work with shared sessions?** Does it filter by `AgentIDs` to find sessions the agent participated in, or only return legacy per-agent sessions?

4. **What is the maximum number of agents in a single session?** Is there a practical upper bound on the `AgentIDs` slice?

5. **How does session deletion work for shared sessions?** Who is authorized to delete — any participating agent, only the creator, or only via the system agent?

6. **What happens when an agent is deleted from the registry but its ID still appears in `AgentIDs`?** The session panel will try to render badges for a non-existent agent.

7. **How does `LastCompactionSummary` interact with the shared session model?** It is per-agent state on the session. With multiple agents, do we need per-agent compaction summaries?

8. **Should handoff events be logged as transcript entries?** The spec mentions an `agent_switched` WebSocket frame but not whether the handoff appears in `transcript.jsonl` (e.g., as `{"type": "system", "content": "Handoff to Ray"}`). This affects audit trail and session replay.

---

## Verdict Rationale

The v2 spec is a significant improvement — the summarization strategy, concurrency model, system agent blocking, and schema migration are now addressed at design level. However, the two critical findings (CRIT-001: shared store constructor incompatibility; CRIT-002: handoff tool dependency gap) mean the spec describes the target state well but under-specifies the transition from current code to target state, which is exactly where implementation bugs will occur. The six major findings reinforce this pattern: token estimation algorithm, flock integration, store injection, WebSocket path, MetaPatch gap, and idempotency access path are all left to the implementer.

The spec should be revised to close the critical and major gaps before task decomposition.

### Recommended Next Actions

- [ ] Specify shared `UnifiedStore` constructor changes and how `agentID` is handled (CRIT-001)
- [ ] Provide complete handoff tool rewrite specification with new dependencies and pseudocode (CRIT-002)
- [ ] Specify token estimation chain for context transfer (MAJ-001)
- [ ] Correct flock claim from "existing" to "new" and specify integration points (MAJ-002)
- [ ] Specify `AgentLoop.sharedStore` injection and `ListAllSessions` rewrite (MAJ-003)
- [ ] Specify WebSocket session creation path through shared store (MAJ-004)
- [ ] Add `SwitchAgent` method specification for `UnifiedStore` (MAJ-005, MAJ-006)
- [ ] Address all 8 unasked questions and encode decisions into the spec
