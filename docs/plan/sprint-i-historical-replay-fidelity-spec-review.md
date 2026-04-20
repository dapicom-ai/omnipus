# Adversarial Review: Sprint I — Historical Chat Replay Fidelity

**Spec reviewed**: `docs/plan/sprint-i-historical-replay-fidelity-spec.md`
**Review date**: 2026-04-20
**Verdict**: REVISE

## Executive Summary

Sprint I is architecturally sound and addresses a real, observable product regression (the smoking gun at `pkg/gateway/websocket.go:662-727` is precisely as described). However, the spec makes two factual errors about the store layer that change the shape of the implementation: it claims `ReadTranscript` "already reads across partitions" (false — `UnifiedStore.ReadTranscript` reads a single `transcript.jsonl` file; `PartitionStore.ReadMessages` is the day-partitioned one) and that the backend can read `ToolCall.Parameters` and `ToolCall.Result` on disk "in full" (true, but with a quiet catch — the result may be so large that emitting it as one WS frame violates the 5MB inbound message limit, and this is unexamined). The spec also inherits Sprint H's underspecified `parent_call_id` semantics without resolving them (see CRIT-H findings): "synthesize `subagent_start`/`_end`" depends on a `span_id` invariant that Sprint H never fixes. Further, the backward-compat claim for legacy sessions is stronger than what the current code can actually do — legacy `spawn` results are opaque text blobs, not structured tool-call data, and the spec's "pre-Sprint-H spawn renders as a flat ToolCallBadge" requires inventing a tool-call entry for data that was never structured that way.

| Severity | Count |
|----------|-------|
| CRITICAL | 2 |
| MAJOR | 10 |
| MINOR | 7 |
| OBSERVATION | 4 |
| **Total** | **23** |

---

## Findings

### CRITICAL Findings

#### [CRIT-001] Factually wrong claim about `ReadTranscript` and multi-partition ordering

- **Lens**: Incorrectness (COR-05); Incompleteness
- **Affected section**: Relevant Execution Flows → "Multi-day session with day-partitioned JSONL | `ReadTranscript` already reads across partitions and returns a flat entry list — replay consumes that list as-is. No multi-day concern."; BDD Scenario 13; TDD test 12; Existing Codebase Context → Symbols Involved ("`pkg/session/daypartition.go::TranscriptEntry`, `ToolCall`")
- **Description**: The spec conflates two different stores. `pkg/session/daypartition.go::PartitionStore::ReadMessages` reads from day-partitioned JSONL files (partitions listed in meta.json). But the WS handler at `websocket.go:655` uses `store.ReadTranscript(attachID)` — which is defined on `UnifiedStore` (`pkg/session/unified.go:335`) and reads from a SINGLE file `transcript.jsonl`. There is no partition-aware method called `ReadTranscript`. So Scenario 13 ("Multi-day session whose entries span two JSONL partitions → entries render in timestamp order across partition boundaries") can only be tested against `PartitionStore`, but Sprint I's implementation target is the `UnifiedStore` path used by `handleAttachSession`. TDD test 12 is ambiguous about which store it exercises.
- **Impact**: Sprint I ships passing tests against `PartitionStore` while the actual gateway replay path (through `UnifiedStore.ReadTranscript`) has a single-file assumption. Multi-day sessions against `UnifiedStore` are untested. If `UnifiedStore` is also day-partitioned at some point (or already is — there's a `Partitions` field on `SessionMeta`), the replay could silently truncate to the first partition. Worse, the ambiguity causes Sprint I's engineer to either (a) refactor `handleAttachSession` to use `PartitionStore` (major unintended scope) or (b) ship a multi-partition-capable replay against a single-file store, which is nonsensical.
- **Recommendation**: Replace the claim with: "Sprint I's replay target is `handleAttachSession` which calls `UnifiedStore.ReadTranscript(sessionID)` — this currently reads a single `transcript.jsonl` file. If multi-partition support is required for replay, it must be added separately to `UnifiedStore` or the call must be migrated to `PartitionStore.ReadMessages`. Scope decision needed before implementation." If multi-partition is OUT of scope for Sprint I, delete BDD Scenario 13 and TDD test 12 OR scope them to `PartitionStore` unit-only, with an explicit note that `UnifiedStore` replay through the gateway does not yet honor partitions.

---

#### [CRIT-002] Legacy spawn replay "falls back to flat ToolCallBadge" is not achievable with today's persisted data shape

- **Lens**: Infeasibility (FEA-04); Incorrectness (COR-05)
- **Affected section**: BDD Scenarios 6 and 7 ("legacy spawn falls back to flat badge"); TDD test 7 (`TestReplay_LegacySpawn_NoSpanFrames`); Test Dataset D5
- **Description**: Pre-Sprint-H sessions were persisted by the old code path. For those sessions, the `spawn` tool's result was delivered via the `SubagentManager` or `SubTurnSpawner` path that ultimately called `AppendTranscript` with a `TranscriptEntry` whose `.Content` field contained the subagent's final text. Whether such a legacy session contains a `ToolCall` entry at all for the spawn invocation — let alone one populated with `Parameters` (the original spawn args) and `Result` (a "tool result" shape) — is not established. The `SpawnTool.execute` path returns `AsyncResult(...)` whose string becomes the spawn tool call's `Result.value`, and the async sub-turn result is delivered later via the `pendingResults` channel. Whether this is persisted as a proper `ToolCall` with `Status: success`, `Parameters: {task,label,agent_id}`, and `Result: {...}` — or as just a text blob — is not verified by the spec. Test Dataset D5 specifies "legacy-shape assistant with spawn call but no parent-linked children" but doesn't describe whether the spawn ToolCall's Result is an opaque string blob or a structured result map.
- **Impact**: If legacy spawn entries persisted only the text-blob result (no structured `ToolCall`), Sprint I cannot reconstruct a ToolCallBadge from them — there's no structured params to show. "Falls back to flat ToolCallBadge" becomes "the badge shows spawn with empty params and the raw result as a map{value: <blob>}" — which may be worse for the user than the current markdown-joined representation (lists the tool name at least). The "no user-visible data loss relative to today" guarantee in Hard Constraints is therefore unverifiable until someone inspects a real legacy session file.
- **Recommendation**: Before implementation, inspect 3-5 real legacy session JSONL files (from CI artifacts or dev env) and document in the spec: (a) the exact shape of a persisted spawn `ToolCall` — what Parameters keys, what Result shape; (b) whether the sub-turn's final text is inside `ToolCall.Result` or in the enclosing `TranscriptEntry.Content` or both. Add a test dataset row with a literal JSONL line from a real legacy session. If the legacy data is truly opaque-text-blob, weaken Scenario 6 to: "the legacy spawn entry renders as a ToolCallBadge whose Result pane shows the raw stored string; no SubagentBlock is attempted." This is honest about the fallback fidelity.

---

### MAJOR Findings

#### [MAJ-001] `subagent_start`/`_end` synthesis in replay depends on Sprint H invariants that aren't established yet

- **Lens**: Inconsistency (CON-02); Incompleteness (INC-03)
- **Affected section**: Depends on Sprint H (stubbed schema note); BDD Scenario 5; TDD test 6; Behavioral Contract ("A subagent spawn entry emits `subagent_start` before its nested frames and `subagent_end` after them")
- **Description**: Sprint I says it can build "against a stubbed schema" while Sprint H defines the schema. But the critical invariant Sprint I needs — `ToolCall.ParentCallID == parent spawn's ToolCall.ID` — is NOT established in Sprint H's Hard Constraints (see Sprint H review, CRIT-H-004). Without this invariant fixed in Sprint H, Sprint I's "detect spawn entries by looking up subsequent entries whose `ParentCallID` matches the spawn's `call_id`" (I1 step 3) is under-specified: which field of the spawn's ToolCall is the "call_id"? Today the ToolCall struct has `.ID`, not `.CallID`. The two sprints reference a shared schema that isn't consistent between them.
- **Impact**: If Sprint H lands with `ToolCall.ID` = provider-assigned call_id and Sprint I assumes `ToolCall.CallID`, the replay's span reconstruction silently finds zero matches and emits no `subagent_*` frames. Tests that trace to Scenario 5 pass because they construct fixtures with matching strings, but real data shows zero spans.
- **Recommendation**: Sprint I's "Depends on Sprint H" block must cite the specific invariants it relies on: "Depends on Sprint H landing: (a) `ToolCall.ParentCallID` field; (b) invariant that `ParentCallID` holds the parent's `ToolCall.ID`; (c) persistence of nested tool calls inside the parent's `TranscriptEntry.ToolCalls` slice." If Sprint H doesn't land those specific items, Sprint I needs a preamble fixing them.

---

#### [MAJ-002] Unbounded WS frame size for large tool results is acknowledged in edge cases but not mitigated

- **Lens**: Insecurity (SEC-09); Inoperability (OPS-06)
- **Affected section**: Edge Cases — "Transcript with a tool call whose `Result` is extremely large (e.g., 1 MB)"; BDD Scenario 11
- **Description**: The backend's `handleMessage` enforces a 5MB inbound message cap (websocket.go:470), but there is no equivalent cap on outbound frames. A session with a single 10MB tool result (e.g., a `fs.read` on a large file, a web scrape, a base64-encoded binary) produces a single `tool_call_result` frame exceeding the WS frame size. Most WebSocket clients (browsers, proxies, CDNs) have per-frame limits around 1-16MB depending on configuration. The spec's edge case says "Frame is emitted as-is. The SPA's existing rendering already handles truncation" — but the frame never reaches the SPA if the WS layer drops it. And BDD Scenario 11 specifies 500KB which is well below typical limits, not boundary-adjacent.
- **Impact**: Replay of a session with a large tool result either (a) drops the frame silently and leaves the replay hanging, (b) closes the WS connection with frame-too-large, (c) transmits OK but the SPA runs out of memory trying to JSON.parse. This is a regression: the current markdown-joined replay truncates everything to one text blob, so it never hit this limit. Sprint I introduces a new failure mode.
- **Recommendation**: Add FR-I-011: "The backend MUST truncate or chunk `ToolCall.Result` in the replayed `tool_call_result` frame when the JSON-encoded frame would exceed 1MB. Truncation replaces the result payload with `{\"_truncated\": true, \"original_size_bytes\": N, \"preview\": \"<first 10KB>\"}` and logs a warning." Add BDD + TDD + dataset boundary at exactly 1MB + 1 byte.

---

#### [MAJ-003] `tool_call_start` + `tool_call_result` semantics in replay are identical to live — but the SPA's reducer currently assumes live-only

- **Lens**: Incorrectness (COR-05); Inconsistency (CON-06)
- **Affected section**: FR-I-009 ("The SPA reducer MUST treat replay tool-call frames identically to live tool-call frames"); I2 step 1 ("Primary goal: no changes needed to the reducer")
- **Description**: The reducer's `tool_call_start` case (chat.ts:451-459) has a specific handler: it creates an assistant placeholder if one doesn't exist, then captures `textAtToolCallStart` — a snapshot of the last assistant message's current content. During replay, `replay_message` arrives as a complete finished message (status: "done"). Then `tool_call_start` arrives. The reducer snapshots the *entire* content of the completed replay_message — not the progressive streaming state. The interleaving captured by `textAtToolCallStart` during live streaming (designed to interleave live-streaming tokens with tool calls) becomes meaningless during replay. FR-I-009 says this "identically" works; it doesn't — the snapshot has different semantics.
- **Impact**: Rendering may show all tool calls appearing AFTER all the message text (not interleaved) during replay, violating US-4. The "one reducer path" Hard Constraint reads as a goal but requires reducer changes the spec explicitly declares won't be needed.
- **Recommendation**: Revise I2 step 1 to: "The reducer needs minor adjustments for replay fidelity. Specifically, `textAtToolCallStart` must be captured as 'the assistant message content at the time the tool call event was ORIGINALLY emitted' — not at reducer time. During replay, this means embedding a text-position hint inside the `tool_call_start` frame itself (e.g., `text_offset: 42` — offset into the replay_message content)." Add FR-I-012 and TDD test for "interleaved text+tool-call in replay".

---

#### [MAJ-004] User-message-mid-replay behavior is explicitly undecided

- **Lens**: Ambiguity (AMB-04); Incompleteness (INC-03)
- **Affected section**: US-5 AC2 ("**Default:** queue the user message until `done` arrives, then send. Needs user confirmation of the desired UX. Alternative: cancel replay and jump to live. — Owner pick."); Ambiguity Warnings row 3
- **Description**: This is a shipping behavioral decision left for "owner pick" inside the spec itself. A spec that reaches implementation with an unresolved behavior question means the implementer picks, and the first bug report becomes a product argument. Given a 500-entry session (replay takes 1-2 seconds per the spec's own performance target), the user genuinely might type mid-replay.
- **Impact**: Implementer picks one. User files a bug. Cycle.
- **Recommendation**: Resolve before implementation. Recommended default: "The input is disabled during replay (grey-out + loading indicator); the send button is enabled only after `done` arrives. This is simple, predictable, and avoids the 'did my message get sent?' question." Add a BDD scenario asserting the input is disabled during replay.

---

#### [MAJ-005] Orphan `parent_call_id` is logged and rendered flat — but the linkage semantics may be wrong

- **Lens**: Incorrectness (COR-06); Incompleteness
- **Affected section**: BDD Scenario 8; Edge Cases — "Entry with `ParentCallID` referring to a spawn that does not exist in the transcript"; FR-I-007
- **Description**: An orphan `parent_call_id` can arise from (a) transcript corruption, (b) the spawn entry being in a different partition that wasn't loaded, (c) a legitimate in-progress spawn where the `spawn` tool call is still pending but nested calls have landed, (d) a persistence bug. The spec only handles it as "flat badge + warn log" — but the warning is one-way. There's no mechanism to detect the orphan → auto-heal when the spawn arrives out-of-order. Replay loads ordered entries, so ordering is deterministic post-load — but if replay runs while the session is still being written (gateway restart mid-session?), the spawn entry may still be being appended.
- **Impact**: A legitimate late-arriving spawn renders as an orphan. User sees their session with a "hanging" tool call that should have been grouped. Unlikely but possible.
- **Recommendation**: Specify: "Orphan detection is strictly post-load: after all entries are loaded from disk, any ToolCall with a `ParentCallID` whose target is not in the loaded entries is an orphan. Orphans are rendered as flat ToolCallBadges. This is deterministic relative to what was on disk at read time." Document that if the session is actively being written, the replay may see a partial state — add this to Edge Cases.

---

#### [MAJ-006] "Preserve existing agent_id forwarding" is mentioned but the current code has edge cases

- **Lens**: Incorrectness (COR-05)
- **Affected section**: I1 step 4 ("Preserve existing agent_id forwarding into `replay_message` and into the synthetic tool-call frames")
- **Description**: Synthetic tool-call frames aren't currently emitted, so "into the synthetic tool-call frames" is new behavior. The `wsServerFrame.AgentID` field exists but current `tool_call_start`/`tool_call_result` don't set it. Adding it changes the frame shape and may shift rendering if the SPA reducer uses `frame.agent_id` for routing. The spec doesn't declare whether tool-call frames SHOULD carry `agent_id` (arguably yes, for attribution); if they should, the live forwarder (eventForwarder at websocket.go:951) also needs updating for parity with the replay — otherwise replay and live diverge in which frames carry agent_id.
- **Impact**: Replay frames carry `agent_id` but live frames don't (or vice versa). Snapshot parity tests between live and replay fail because the same logical sequence has different frame shapes.
- **Recommendation**: Specify that both replay AND live `tool_call_start`/`result` frames MUST carry `agent_id` after Sprint I. Add a parallel change to `eventForwarder` so live-side emission is updated in the same PR. Add TDD: `TestReplayAndLive_AgentID_Parity`.

---

#### [MAJ-007] `done` frame emission vs parallel event subscriber — potential race

- **Lens**: Incorrectness (COR-06); Incompleteness (INC-05)
- **Affected section**: Behavioral Contract ("After all entries, exactly one `done` frame closes the replay"); FR-I-006; `handleAttachSession` flow
- **Description**: `handleAttachSession` currently registers the connection as the event forwarder target AFTER the replay `done` is sent (websocket.go:734-745). Sprint I preserves this order. But if live events for the session arrive WHILE replay is still emitting, they're dropped (not registered yet). If the session is `StatusActive` and the agent is mid-turn when the user attaches, the user sees replay of pre-attach state, then `done`, then live events — with a gap where nothing was shown. BDD Scenario 9 says "After replay … new assistant response streams in" — but doesn't cover the case where live events were already in flight during replay.
- **Impact**: A user attaching to an in-progress session sees a visible pause between replayed history and resumed live streaming, potentially missing tokens or tool calls emitted during the replay window.
- **Recommendation**: Either (a) register for live events BEFORE replay starts, buffer events during replay, flush buffer after `done` — or (b) document that attach-during-active-turn has a small event-loss window, with a follow-up ticket. Add Edge Case + BDD scenario.

---

#### [MAJ-008] `ToolCall.ID` uniqueness is not guaranteed at write time — dedup is deferred

- **Lens**: Infeasibility (FEA-05); Incorrectness (COR-06)
- **Affected section**: Ambiguity Warnings row 4 ("Should the backend deduplicate duplicate `call_id`s in the transcript (possible if persistence glitched)? — Emit all occurrences as-is and let the SPA reducer tolerate duplicates. No dedup at the replay layer. — Confirm — dedup belongs to the writer, not the reader.")
- **Description**: The SPA reducer keys `toolCalls` by `call_id` — a duplicate call_id overwrites the previous entry silently (see chat.ts:158-171). If a session on disk has two ToolCalls with the same ID (possible if a resume/crash path re-wrote an entry, or a provider reused call_ids), replay emits two `tool_call_start` frames with the same call_id and the reducer drops one. Sprint I says "reducer tolerates duplicates" but it doesn't — it overwrites. The spec's ambiguity-warning resolution is wrong about current reducer behavior.
- **Impact**: Session with duplicate call_ids has one of the duplicates silently lost from the UI.
- **Recommendation**: Reframe the ambiguity resolution: "Replay MUST deduplicate `ToolCall.ID` within a transcript: if two entries have the same ID, only the latest is emitted. This is defense-in-depth against writer bugs. Separately, the SPA reducer SHOULD handle duplicates by using a monotonically-increasing index rather than call_id as the key — deferred to a future reducer refactor." Add TDD: `TestReplay_DuplicateCallID_EmitsLatestOnly`.

---

#### [MAJ-009] "Visual parity" success criterion (SC-I-004) is aspirational without a concrete diffing method

- **Lens**: Infeasibility (FEA-04)
- **Affected section**: SC-I-004 ("The DOM produced by reopening a session matches the DOM the same session produced live (ignoring streaming cursors + timestamps), verified via a snapshot test or side-by-side comparison"); I3 step 3 ("Visual parity check: run the same prompt twice — once and capture live DOM, close + reopen and capture replay DOM — assert structural equivalence (a DOM diffing helper, or a simplified text-based comparison).")
- **Description**: "DOM matches" is a big promise. React may render slightly different DOM for the same logical state (memoization, keys, transient layouts). Streaming cursors, timestamps, and render-order variations all produce DOM-level differences that are not semantic. "A DOM diffing helper, or a simplified text-based comparison" handwaves what is actually a very sticky test problem.
- **Impact**: Either (a) the test is too strict and flakes, requiring constant maintenance; (b) it's too lenient and catches nothing. Most "visual parity" tests end up being the latter.
- **Recommendation**: Replace SC-I-004 with narrower, testable checks: "(i) Both live and replay produce the same number of `[data-testid=tool-call-badge]` elements. (ii) For each badge, the `tool` attribute text matches. (iii) For each badge, the status icon kind matches. (iv) Message order (user/assistant sequence) matches." This is inspectable and does not depend on DOM-level diffing.

---

#### [MAJ-010] No observability for the high-risk code path

- **Lens**: Inoperability (OPS-02, OPS-04)
- **Affected section**: The "CRITICAL path: every session reopen goes through `attachSession`" note in Impact Assessment
- **Description**: Impact Assessment calls this a CRITICAL path — a replay bug breaks every session reopen. But no FR requires structured logging on replay. When a user reports "my old session looks wrong", there's no log to grep for. Sprint I should require a `slog.Debug` entry per replay emission batch (e.g., entry count, tool call count, span count) — or at minimum one summary line at start and end.
- **Recommendation**: Add FR-I-013: "The backend MUST emit one `slog.Info` entry at the start of a replay (session_id, entry_count_loaded, tool_call_count_loaded, span_count_detected) and one at the end (frames_emitted, duration_ms)."

---

### MINOR Findings

#### [MIN-001] `replay_message` vs `token` is a wire-contract subtlety that should be documented

- **Lens**: Ambiguity (AMB-02)
- **Affected section**: FR-I-008 ("The replay MUST NOT emit `token` frames. Partial/streaming semantics are live-only.")
- **Description**: This is a sound design decision but the rationale is in the "live-only" part, not the "MUST NOT" part. Future engineers adding a "retype the session for demo" feature will want to re-emit tokens. Add the reasoning.
- **Recommendation**: Expand to: "The replay MUST NOT emit `token` frames. Rationale: `token` frames imply streaming-in-progress semantics (isStreaming=true, streamCursor=true) that are false for completed messages; the reducer would mis-handle them by keeping the cursor visible forever."

---

#### [MIN-002] Text-based comparison tools for cross-partition ordering are not specified

- **Lens**: Infeasibility (FEA-03)
- **Affected section**: TDD test 12 (`TestReplay_MultiPartition_Order`)
- **Description**: How is "timestamp order across partition boundaries" asserted? Sort by timestamp, then assert positions? Or assert the frame emission order matches entry sort order? Deterministic ordering in day-partition JSONL requires entries within a partition to also be timestamp-ordered — which is true for normal appends but could skew if the clock jumps.
- **Recommendation**: Specify the assertion: "For a two-partition fixture (day N, day N+1), `TestReplay_MultiPartition_Order` asserts that every emitted frame's corresponding entry timestamp is >= the previous frame's entry timestamp."

---

#### [MIN-003] No test covers entries with zero `agent_id` under multi-agent sessions

- **Lens**: Incompleteness
- **Affected section**: TDD plan; FR-I-002
- **Description**: `TranscriptEntry.AgentID` is the entry's emitting agent. Entries with `AgentID=""` exist (system messages, legacy pre-multi-agent data). FR-I-002 says "preserving `role` and `agent_id`" — but doesn't specify behavior when agent_id is empty (do we omit the field from the frame? emit "" literally?). Scenario 12 (system role) has no agent_id but is emitted.
- **Recommendation**: Add dataset D8: "`system, content="agent switched", agent_id=""`. Expected: `replay_message{role:"system", content:"agent switched"}` — `agent_id` field omitted from JSON output." Add matching TDD assertion.

---

#### [MIN-004] Holdout I-E6 "500 tool calls, interactive within 2 seconds" is a loose performance SLO

- **Lens**: Ambiguity (AMB-03); Infeasibility (FEA-02)
- **Affected section**: Holdout Evaluation Scenarios I-E6
- **Description**: "Interactive within 2 seconds" is defined from what baseline? User-click-to-first-paint? First-token received from WS? Input-field-enabled? Also, 500 tool calls generates 500 * 2 = 1000 WS frames plus `replay_message` frames — at typical Go-to-WS throughput (~5K-20K frames/s), this is sub-second, but DOM rendering of 500 ToolCallBadges can take 200ms+ on the SPA side (React reconciliation). The 2s target is unrealistic or is missing caveats.
- **Recommendation**: Specify: "Time from user clicking the session to the chat input becoming enabled (post-`done`-frame): ≤3s for a 500-entry session on a reference laptop (M-series Mac, Chrome stable). The full DOM being painted may take longer due to virtualization." Or, if 2s is non-negotiable, specify the virtualization strategy.

---

#### [MIN-005] Compaction entries: FR-I-010 says "skip" but the existing behavior is "emit if Content != empty"

- **Lens**: Incorrectness (COR-05)
- **Affected section**: FR-I-010; Edge Cases — "Compaction entries (`Type=EntryTypeCompaction`). Today the replay skips them silently (because `Content == ""`). Keep that behavior."
- **Description**: "Skips because Content is empty" is different from "skips because Type is compaction". A compaction entry CAN have non-empty `Summary` (see daypartition.go:118). If such an entry also has Content populated (which the schema permits), the current replay emits it. Sprint I hardens this by skipping by type — which is a behavior change disguised as a non-change. Legacy sessions with compaction+content entries may visually lose content.
- **Recommendation**: Verify against real data whether compaction entries ever have Content != "". If yes, either (a) emit them (current behavior) or (b) skip them (spec's FR-I-010) — but pick knowingly. Add a TDD dataset row for "compaction with non-empty Content".

---

#### [MIN-006] Concurrent attach is dismissed but the scenario has a race

- **Lens**: Incompleteness
- **Affected section**: Edge Cases — "Concurrent attach. Two browsers attached to the same session each receive their own independent replay. Existing behavior — unchanged."
- **Description**: If both browsers are in the replay flow AND a tool call is persisted mid-replay (live session), browser A sees the entry (disk was read after the append) while browser B does not (read before). Results in desynced views. "Unchanged" is technically true but glosses over the race.
- **Recommendation**: Acknowledge: "If the session is actively being written when a browser attaches, that browser's replay is a snapshot taken at read time. Subsequent live events cover the gap, but there is a small window where the replay view lags the persisted state — acceptable for MVP."

---

#### [MIN-007] New regression tests duplicate TDD test intent

- **Lens**: Overcomplexity (CPX-06)
- **Affected section**: Regression section — "TestReplay_LegacySession_UnchangedUI_Snapshot" and "TestChatScreen_LiveAndReplay_VisualParity"
- **Description**: The first overlaps TDD test 15 (`ChatScreen_Replay_RendersToolCallBadge`); the second overlaps TDD test 17 (E2E fidelity). Both are broader versions of existing tests. Having multiple "the same thing, viewed differently" tests increases maintenance cost for questionable coverage gain.
- **Recommendation**: Either drop the regression tests or scope them specifically: e.g., `TestReplay_LegacySession_UnchangedUI_Snapshot` becomes "snapshot a specific fixture file's rendered DOM; any structural change triggers a diff review."

---

### Observations

#### [OBS-001] Replay could be a REST endpoint instead of WS-streamed

- **Lens**: Overcomplexity (CPX-07)
- **Affected section**: Relevant Execution Flows → REST row
- **Suggestion**: The REST endpoint `/api/v1/sessions/{id}/messages` returns raw `TranscriptEntry[]`. The ChatThread prefetch already uses this. Instead of streaming replay through WS (which has frame-size caveats, ordering races, and backpressure concerns), the SPA could use REST for "full history" and WS only for live streaming post-attach. This would eliminate Sprint I's backend work in favor of frontend-only logic to render `TranscriptEntry[]` directly. The cost: two code paths for "the same data" (REST for history, WS for live). The benefit: dramatically simpler backend; existing REST is already tested. Consider as a v2 option.

---

#### [OBS-002] The spec's smoking-gun code snippet is accurate and useful

- **Lens**: Incorrectness (COR-05) — positive confirmation
- **Affected section**: Context section code block
- **Suggestion**: The line reference `websocket.go:662-727` is correct (verified). Keep this style of "evidence block" — it makes reviews much faster.

---

#### [OBS-003] I1 extraction to `pkg/gateway/replay.go` is good Sprint hygiene

- **Lens**: N/A — structural suggestion
- **Affected section**: I1 step 1 ("Extract the replay logic out of `attachSession` into a testable helper")
- **Suggestion**: Good call. Make the extracted helper take a `func(wsServerFrame) error` rather than the `wsConn` directly, so unit tests can drive it with a slice-backed sink. Add to the spec.

---

#### [OBS-004] No mention of privacy redaction at replay time

- **Lens**: Insecurity (SEC-05)
- **Affected section**: n/a — missing
- **Suggestion**: Live tool-call params can contain secrets (API tokens in headers, credentials in args). Current live flow emits them unredacted (the frontend just displays them). Replay reads the same data from disk and emits the same frames. This is existing behavior, not a regression introduced by Sprint I — but Sprint I is the right moment to note: "Replay inherits the live flow's treatment of secrets in tool params/results; any redaction must happen at the writer (pre-persist) or at render time (frontend), not at replay." Consider for a later SEC sprint.

---

## Structural Integrity

### Variant A: Plan-Spec Format

| Check | Result | Notes |
|-------|--------|-------|
| Every user story has acceptance scenarios | PASS | US-1..US-5 covered. |
| Every acceptance scenario has BDD scenarios | PASS | Mapping is clean. |
| Every BDD scenario has `Traces to:` reference | PASS | All 14 traced. |
| Every BDD scenario has a test in TDD plan | PASS | Each scenario has at least one test. |
| Every FR appears in traceability matrix | PASS | FR-I-001..FR-I-010. |
| Every BDD scenario in traceability matrix | PASS | All 14 in matrix. |
| Test datasets cover boundaries/edges/errors | PARTIAL | Missing large-result-boundary (MAJ-002), duplicate-call_id (MAJ-008), non-empty compaction (MIN-005). |
| Regression impact addressed | PARTIAL | New regression tests duplicate TDD (MIN-007); no mention of impact on `message-history.test.tsx` diff granularity. |
| Success criteria are measurable | PARTIAL | SC-I-004 aspirational (MAJ-009); SC-I-006 a11y baseline undefined. |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Frame size limits | No test for tool results > 1MB | MAJ-002 edge |
| Duplicate call_id | No test for ToolCall.ID duplicated in one transcript | MAJ-008 |
| Interleaved text+tool-call in replay | TDD tests 14-16 cover structure but not text-interleaving fidelity | MAJ-003 |
| Attach-during-active-turn | No test for "live events in flight during replay" | MAJ-007 |
| Realistic legacy-shape fixture | All legacy-shape datasets are synthesized; need a real JSONL sample | CRIT-002 |
| Frame agent_id parity | No test asserting both live and replay tool-call frames carry agent_id | MAJ-006 |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| D-new | Tool result at exactly 1 MB + 1 byte | Add row for MAJ-002 |
| D-new | Two ToolCalls with same ID | Add row for MAJ-008 |
| D-new | Compaction entry with non-empty Content AND Summary | Add row for MIN-005 |
| D-new | AssistantEntry with agent_id="" | Add row for MIN-003 |
| D-new | Real legacy JSONL line copy-paste | Add for CRIT-002 verification |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| `attachSession` replay loop | ok | ok | ok | risk | risk | ok | I: large tool results containing secrets stream unredacted to browser (OBS-004). D: unbounded frame size breaks WS transport (MAJ-002). |
| `ReadTranscript`→JSONL read | ok | ok | ok | ok | risk | ok | D: malformed JSONL lines are skipped silently but the caller doesn't know how many were skipped — a corrupted session could show a partial transcript without warning. |
| Synthetic `subagent_start/_end` replay emission | ok | ok | ok | ok | ok | risk | E: if someone injects a forged transcript line with ParentCallID referencing a spawn they don't own, the span is reconstructed (irrelevant for single-tenant local deployment, but worth noting for multi-tenant SaaS variant). |

Legend: risk = identified threat not mitigated in spec; ok = adequately addressed or not applicable.

---

## Unasked Questions

1. Does `UnifiedStore.ReadTranscript` replay actually need multi-partition support, or is Sprint I's scope only single-file sessions? (See CRIT-001.)
2. What does a real legacy (pre-Sprint-H) spawn `ToolCall` entry look like on disk? Is `Parameters` populated? Is `Result` structured or opaque text? (See CRIT-002.)
3. Is `ToolCall.ParentCallID` === parent's `ToolCall.ID`? What field carries the provider's original call_id?
4. When a user attaches mid-active-turn, what's the correct behavior for live events that arrive during the replay window?
5. Can a `tool_call_result` frame exceed WS per-frame limits? What's the mitigation?
6. Should `tool_call_start` / `tool_call_result` frames carry `agent_id` in both live and replay?
7. What's the dedup strategy for duplicate `ToolCall.ID` within one transcript?
8. Is mid-replay message send blocked, queued, or replay-cancelling?
9. How is "visual parity" actually measured without being flaky?
10. Is there a configurable per-session replay frame-count ceiling (to prevent abuse of a session with a million tool calls)?

---

## Verdict Rationale

REVISE. Two findings rise to critical: CRIT-001 (factual error about the store API that changes implementation scope) and CRIT-002 (unverified legacy-data shape assumption). Both are fixable with a brief code inspection before implementation starts — CRIT-001 by choosing which store Sprint I targets, CRIT-002 by inspecting a real legacy session file. Neither should block the sprint once addressed.

The MAJOR findings are mostly specification omissions: wire semantics unresolved (MAJ-003), behavioral decisions deferred to implementer (MAJ-004), performance/size edge cases dismissed (MAJ-002), and race conditions handwaved (MAJ-007). These need explicit FRs before handing off to I1/I2/I3.

The core premise — rewriting `attachSession` to emit per-tool-call frames instead of a markdown blob — is architecturally correct and addresses a real, observable regression. The extraction to `pkg/gateway/replay.go` is good hygiene. The sprint's shape is defensible once the specification gaps are filled.

### Recommended Next Actions

- [ ] Resolve CRIT-001 by inspecting `handleAttachSession` store use and scoping multi-partition support (OR deferring it).
- [ ] Resolve CRIT-002 by inspecting 3-5 real legacy session JSONL files and documenting the observed shape.
- [ ] Resolve MAJ-001 by aligning with Sprint H's resolution of ParentCallID ↔ ToolCall.ID invariant.
- [ ] Add FRs for frame-size cap (MAJ-002), user-message-mid-replay (MAJ-004), attach-during-active-turn (MAJ-007), duplicate call_id dedup (MAJ-008), agent_id on tool-call frames (MAJ-006), replay telemetry (MAJ-010).
- [ ] Replace SC-I-004 with narrower, testable criteria (MAJ-009).
- [ ] Fill in test dataset boundaries per the "Dataset Gaps" table.
- [ ] Re-run `/grill-spec` after revisions.
