# Adversarial Review: Sprint H — Subagent Collapsed-Block UI

**Spec reviewed**: `docs/plan/sprint-h-subagent-block-spec.md`
**Review date**: 2026-04-20
**Verdict**: BLOCK

## Executive Summary

Sprint H has a head-on collision with an existing acceptance decision that is encoded in both a passing Go test and an ADR-style comment: `pkg/tools/spawn_grandchild_test.go::TestSubagentCanSpawnGrandchild` asserts — citing "Plan 3 §1" and "temporal-puzzling-melody.md §4 Axis-1" — that subagents MUST be allowed to spawn grandchildren with unlimited depth and only budget-level caps. Sprint H's FR-H-005 / US-3 requires the exact opposite: `spawn` absent from the sub-turn registry so grandchildren are impossible. That is a direct contradiction of a previously ratified product decision and cannot ship as-is without someone explicitly reversing Plan 3. Beyond that, the spec papers over the most technically loaded plumbing change (how `parent_call_id` gets onto tool-call WS frames in a live sub-turn) with the phrase "pass an `eventForwarder`" without acknowledging that `ToolExecStartPayload`/`EndPayload` currently carry no turn identity, so the WebSocket forwarder has no way to distinguish parent-turn tool calls from sub-turn tool calls without a new payload field that the spec never adds.

| Severity | Count |
|----------|-------|
| CRITICAL | 4 |
| MAJOR | 9 |
| MINOR | 6 |
| OBSERVATION | 4 |
| **Total** | **23** |

---

## Findings

### CRITICAL Findings

#### [CRIT-001] Directly contradicts ratified Plan 3 §1 decision that grandchildren are allowed

- **Lens**: Incorrectness (COR-01, COR-05)
- **Affected section**: Hard Constraints bullet 3 ("Forbids grandchildren at the tool-registry level"); US-3; FR-H-005; BDD Scenarios 8 & 9; TDD test 2 (`TestSubTurnRegistry_OmitsSpawn`)
- **Description**: `pkg/tools/spawn_grandchild_test.go` is a live passing contract test that explicitly encodes the opposite decision: its docblock cites "Plan 3 §1 acceptance decision — subagents are allowed to spawn grandchildren (unlimited depth; budget-only caps apply)" and "temporal-puzzling-melody.md §4 Axis-1". The test asserts the tool schema has no `depth`/`max_depth` fields and that no depth rejection ever occurs. Sprint H's registry-filter approach does not literally trigger "depth limit" error text (so the existing test's string assertions may still pass by accident), but the *behavior* it mandates — that a subagent cannot see `spawn` at all — is in head-on product-decision conflict with the ratified "unlimited depth, budget-only caps" rule. Shipping Sprint H without first reversing Plan 3 §1 means either (a) the prior acceptance decision is silently overruled, or (b) both behaviors coexist in the codebase and the tests will eventually collide when someone tightens the grandchild assertions.
- **Impact**: You ship a feature that contradicts a documented product decision. Any later effort to actually honor Plan 3 ("unlimited depth, budget-only caps" — e.g., team tools, evaluator-optimizer loops, plan-then-execute patterns) will have to unwind FR-H-005. Worse: the existing test passes today only because its assertions are narrowly string-based; a reader who sees it as "the depth contract" gets a false signal about system behavior. A later refactor could break one or the other without warning.
- **Recommendation**: Do not merge Sprint H until one of the following is explicit: (1) Plan 3 §1 is formally reversed in a new ADR/decision record and `spawn_grandchild_test.go` is rewritten or deleted in the same sprint with a cross-reference; or (2) Sprint H's grandchild-forbidden requirement is reframed as a **policy opt-in** (default: allow grandchildren per Plan 3; config flag: `agents.subturn.forbid_grandchildren`) with the UI component tolerating both modes. Option (1) is cleaner. Either way, the spec must cite the reversal and the two tests must be reconciled in a single PR.

---

#### [CRIT-002] No mechanism specified for threading `parent_call_id` onto live tool-call WS frames

- **Lens**: Incompleteness (INC-01, INC-03, INC-05); Infeasibility (FEA-04)
- **Affected section**: Hard Constraints bullet 2; Existing Codebase Context → Symbols Involved table row "pkg/tools/subagent.go::SubTurnSpawner — modifies — Implementations of `SpawnSubTurn` must receive + respect an `eventForwarder` that tags outbound frames with `parent_call_id`"; FR-H-004; Sprint Execution Plan H1 step 3
- **Description**: The spec hand-waves the most technically loaded change. Today `pkg/gateway/websocket.go::eventForwarder` consumes `agent.ToolExecStartPayload` / `ToolExecEndPayload` (defined in `pkg/agent/events.go`) — neither payload currently carries a turn ID, a span ID, or a `parent_call_id`. The forwarder disambiguates events only by `ChatID`, but sub-turns in `pkg/agent/subturn.go` inherit the **same** `parentTS.chatID` (`opts.ChatID = parentTS.chatID` at line 390). So the forwarder cannot distinguish a parent-turn tool call from a sub-turn tool call without a new payload field — which FR-H-004 requires but the spec never adds. "Pass an eventForwarder that tags outbound frames" is a verbal hand-wave: there is no eventForwarder parameter on `tools.SubTurnSpawner.SpawnSubTurn(ctx, cfg)`, and adding one breaks the interface.
- **Impact**: Either (a) H1 lands a `ParentCallID string` field on `ToolExecStartPayload`/`EndPayload` that the spec did not authorize, requiring every producer in `pkg/agent/loop.go` (which I count 3+ call sites emitting these events) to set it, OR (b) the forwarder mis-tags: every tool call during a sub-turn still streams to the browser with no `parent_call_id`, the span never fills up, and the SubagentBlock stays forever stuck at "0 steps". The spec as written does not tell H1 which path to take, and an agent following it literally will wire up `subagent_start`/`_end` frames correctly but leave the per-step frames un-tagged.
- **Recommendation**: Add an explicit FR: "`agent.ToolExecStartPayload` and `ToolExecEndPayload` MUST include a `ParentCallID string` field. All emit sites (`pkg/agent/loop.go` lines ~3685, ~3815, ~4074) MUST populate it from `turnState.parentSpawnCallID`, which is set when a sub-turn is spawned from the spawn tool." Also specify that the sub-turn's `turnState` must track its originating parent spawn `call_id` (a new field, e.g., `parentSpawnCallID string`), wired at subturn spawn time inside `spawnSubTurn` in `pkg/agent/subturn.go`. Add a TDD unit test: `TestSubTurn_ToolCallEvents_CarryParentSpawnCallID`.

---

#### [CRIT-003] Span lifecycle is "anchored to spawn tool call" in prose but the spawn tool returns before the sub-turn completes in Async mode

- **Lens**: Incorrectness (COR-05); Incompleteness (INC-03)
- **Affected section**: Behavioral Contract ("When the parent agent calls spawn … the chat immediately shows a SubagentBlock … When the subagent finishes, the header updates to terminal status"); FR-H-002/FR-H-003; BDD Scenario 3
- **Description**: `pkg/tools/spawn.go::execute` runs the sub-turn inside `go func() { … t.spawner.SpawnSubTurn(ctx, SubTurnConfig{… Async: true}) … }()` and returns an immediate `AsyncResult("Spawned subagent for task: …")` synchronously. The parent turn's ToolExecEnd fires as soon as the spawn *call* returns, not when the sub-turn completes. FR-H-003 says `subagent_end` is emitted "at the end of every spawn tool execution" — but spawn tool execution ends in milliseconds while the sub-turn itself runs for seconds or minutes. The spec never states that `subagent_end` is tied to sub-turn completion (via `EventKindSubTurnEnd`), not to spawn-tool return.
- **Impact**: Implemented literally, the SubagentBlock flips to terminal state immediately after spawn kicks off. Users see "success · 4ms" before any nested tool calls have even started. Every scenario that depends on "live step count updates" (US-4, BDD Scenarios 2 & 3) fails.
- **Recommendation**: Add a Hard Constraint: "`subagent_start` MUST be emitted when the parent's `ToolExecStartPayload` for `tool=\"spawn\"` fires; `subagent_end` MUST be emitted when `EventKindSubTurnEnd` fires for the spawned child turn — NOT when the spawn tool's `ExecuteAsync` returns." Specify the correlation key: the parent spawn tool call's `ID` maps to the child sub-turn's `parentSpawnCallID`; the forwarder matches by that correlation. Add a BDD scenario: "Given the spawn tool has returned its AsyncResult immediately but the sub-turn is still running, When 30s elapse, Then the SubagentBlock header is still in running state and step count is live-updating."

---

#### [CRIT-004] "Backward-compat additive field" is factually wrong — the `ToolCall` struct has no `CallID` field, only `ID`

- **Lens**: Incorrectness (COR-05); Inconsistency (CON-04)
- **Affected section**: FR-H-001 (`Add a ParentCallID string` json:"parent_call_id,omitempty" `field to pkg/session/daypartition.go::ToolCall`); BDD Scenario 1 (`parent_call_id:"call-42"`); Test Datasets row D2 (`ParentCallID: "call-42"`)
- **Description**: The spec treats `parent_call_id` as a pointer to another `ToolCall`'s `call_id`, but `pkg/session/daypartition.go::ToolCall` (lines 140-147) has only an `ID` field — there is no `CallID` field on persisted `ToolCall` entries. Meanwhile, the WS frame layer uses `call_id` (pkg/gateway/websocket.go::wsServerFrame line 57 has `CallID`, `src/lib/ws.ts::WsToolCallStartFrame.call_id` exists). Sprint H never reconciles these: is `ToolCall.ParentCallID` the persisted parent's `ToolCall.ID`? The parent's runtime WS `call_id`? These are derived from the same value in `pkg/agent/loop.go::ToolCallID: tc.ID` (lines 3621, 3661, 3688) but the spec never declares the invariant. A sibling field named `parent_call_id` on a struct whose own ID field is just `id` invites future bugs.
- **Impact**: Implementation ambiguity. An engineer may populate `ParentCallID` with the sub-turn's own ID, a provider-assigned call ID, a ULID generated at spawn time — all of which currently exist in the codebase at different layers. Persisted data becomes impossible to correlate on replay (Sprint I depends on this correlation).
- **Recommendation**: Add to Hard Constraints: "`ToolCall.ParentCallID` stores the parent `ToolCall.ID` value of the spawn that initiated the sub-turn. The WS frame's `call_id` (wire), the agent-loop event's `ToolCallID`, and the persisted `ToolCall.ID` are all the same string — this is an existing invariant. ParentCallID on a ToolCall references this invariant chain." Add unit test: `TestToolCall_ParentCallID_MatchesParentToolCallID` (round-trip: spawn tool's `tc.ID` equals nested tool call's `ParentCallID`). Also rename the field to `ParentToolCallID` if ambiguity persists.

---

### MAJOR Findings

#### [MAJ-001] "1-level enforcement at the registry" is an architectural inversion — registry-construction is not a sub-turn concern today

- **Lens**: Incompleteness (INC-03); Infeasibility (FEA-04)
- **Affected section**: Hard Constraints bullet 3; FR-H-005; Sprint Execution Plan H1 step 2 ("Construct subagent's tool registry by copying the parent registry minus `spawn`")
- **Description**: `pkg/agent/subturn.go` clones the parent agent's tool registry at line 381-383 (`agent.Tools = baseAgent.Tools.Clone()`) but the spawn tool is registered on that registry by `pkg/agent/loop.go`-level wiring (see `NewSpawnTool(manager)` registration site). There is no existing "sub-turn registry" construction code to surgically add the filter to; the clone inherits whatever the parent has. Adding a `Tools.CloneWithout("spawn")` method is a non-trivial registry API change not called out in the spec. Worse, tools are often registered dynamically (the team tool, handoff tool, subagent tool) — the spec doesn't enumerate which other tools must also be filtered or preserved.
- **Impact**: H1's scope is underestimated. The engineer will either (a) add `Clone()` variants (API change) or (b) mutate the cloned registry after-the-fact (fragile). Either way, tests cross-cutting agent/tools package boundaries need updating.
- **Recommendation**: Add FR-H-005a: "`tools.ToolRegistry` MUST expose a `CloneExcept(names ...string) *ToolRegistry` method. `pkg/agent/subturn.go::spawnSubTurn` MUST call `baseAgent.Tools.CloneExcept(\"spawn\")` when constructing the child's registry." Specify that `handoff` and `team` tools are explicitly preserved (if applicable). Add unit test: `TestToolRegistry_CloneExcept_PreservesOthers`.

---

#### [MAJ-002] Ordering of text vs tool calls inside a subagent span is underspecified and conflicts with live reducer semantics

- **Lens**: Ambiguity (AMB-04); Incompleteness (INC-03)
- **Affected section**: Behavioral Contract ("each text chunk as a plain text line, in arrival order"); BDD Scenario 5 (interleaved text and tool calls); TDD test 10
- **Description**: The live reducer (`src/store/chat.ts`) captures `textAtToolCallStart` to preserve interleaving between tokens and tool calls for the parent assistant message. For a subagent span, the spec declares that text and tool calls render in "arrival order", but never says where the text comes from. Subagents run via `runTurn` which emits token frames via the wsStreamer — those get attached to the nearest assistant message via `updateLastAssistantMessage`, which operates on the **last** assistant message in the store. During a sub-turn, the last assistant message is the parent's, not the sub-turn's. There is no existing frame type that says "this token belongs to a subagent span". The spec never adds one, but Scenario 5 requires it.
- **Impact**: Either (a) subagent text tokens are silently routed into the parent message (visible leakage), (b) subagent text is dropped entirely (the MVP fallback — acceptable only if stated), or (c) the spec needs a new `subagent_token` frame it didn't add. The current Explicit Non-Behaviors says "The system must not stream the subagent's system prompt or raw provider messages … Only user-visible artifacts (tool calls with params + results, assistant text, final result) flow." — so "assistant text" IS in scope, but the wire and store mechanisms to preserve it inside a span are missing.
- **Recommendation**: Either (a) add `subagent_token` frame + reducer case to route sub-turn assistant text into the span, with FR+TDD; OR (b) restrict Scenario 5 to "tool calls only" and declare in Explicit Non-Behaviors that subagent intermediate assistant text is not streamed to the frontend (deferred). Resolve in the Ambiguity Warnings section.

---

#### [MAJ-003] "Live step count" requires a frame type that is underspecified

- **Lens**: Ambiguity (AMB-02, AMB-07); Incompleteness
- **Affected section**: FR-H-010 ("The system MAY support `subagent_step` marker frames … but this is optional — step counter updates via the existing `tool_call_start` stream suffice"); US-4; BDD Scenario 2
- **Description**: FR-H-010 says step-count updates rely on `tool_call_start` frames carrying `parent_call_id`. But BDD Scenario 2 says the header shows "1 step" when a tool_call_start + tool_call_result arrive. What counts as a "step"? One tool_call_start → one step, incrementing immediately on start? Or one completed tool_call_result → one step? Or one tool_call_start (started steps = N) and another when the result arrives (completed steps = M)? The spec doesn't say. US-4 BDD Scenario 1 says "makes 5 nested tool calls … step count updates at least twice" — so 5 calls = 5 steps, but whether they count on start or end is unspecified.
- **Impact**: Implementer guesses. If they increment on start, the counter reaches final value before any tool has finished, giving misleading "5 steps" while the spinner still shows running. If they increment on end, the counter lags reality. Either is defensible but the spec must pick.
- **Recommendation**: Specify in Behavioral Contract: "Step count increments by 1 on every `tool_call_start` frame with matching `parent_call_id`. It does NOT decrement or re-count on result. A step is 'one tool invocation regardless of outcome'." Update BDD Scenario 2 to match.

---

#### [MAJ-004] `pendingResults` channel is closed on parent finish — spec's "The system must not block the parent agent's turn on subagent completion for streaming purposes" contradicts existing result-delivery semantics

- **Lens**: Incorrectness (COR-05); Inconsistency (CON-06)
- **Affected section**: Explicit Non-Behaviors ("The system must not block the parent agent's turn on subagent completion for streaming purposes. The spawn call is awaited per the existing `ExecuteAsync` contract, but the span frames stream live so users see progress.")
- **Description**: `spawn.go::execute` explicitly uses `go func()` + `Async: true` + callback. The parent turn does NOT await the sub-turn — it gets an `AsyncResult` immediately, and the sub-turn result is delivered to `pendingResults` channel (see `deliverSubTurnResult` in `pkg/agent/subturn.go`). If the parent turn's LLM decides "I'm done" before the sub-turn delivers, the result becomes orphan. Sprint H's Behavioral Contract promises a terminal `subagent_end` in every case — but if the parent turn has finished, the agent-loop may or may not emit EventKindSubTurnEnd (it does, but through `deliverSubTurnResult`'s orphan-path too). The spec never addresses: what if the sub-turn orphans? What `subagent_end` status does the user see? What if the gateway WS connection has moved on?
- **Impact**: Orphaned sub-turns (which happen today in production) produce SubagentBlocks stuck in `running` state forever. Edge Cases mentions "Network disconnect during a live subagent run" but not "parent turn finishes first". The spec's "streaming" guarantees are conditional on the connection staying alive and the parent not having moved on.
- **Recommendation**: Add Edge Case: "Parent turn finishes before sub-turn. The SubagentBlock MUST resolve to a terminal state when one of: (a) `EventKindSubTurnEnd` fires for the child turn, (b) `EventKindSubTurnOrphan` fires, or (c) the gateway sees the parent turn's done frame and no SubTurnEnd within a grace period (recommended: 5s); in case (c) the block resolves to `unknown` or `interrupted` status with an explanatory tooltip." Add TDD test.

---

#### [MAJ-005] "Current session falls back to ToolCallBadge (Scenario 12)" is untestable as specified

- **Lens**: Infeasibility (FEA-03, FEA-04)
- **Affected section**: BDD Scenario 12; TDD plan (no unit test traces to Scenario 12)
- **Description**: Scenario 12 asserts replay behavior under **Sprint H's code alone** (i.e., BEFORE Sprint I changes the replay pipeline). But Sprint H only touches the live streaming path and the persisted schema. The current replay pipeline (websocket.go:662-727) emits all tool calls as flattened markdown text in a single `replay_message`; no `tool_call_start` frames are emitted at all. So when a legacy session without `parent_call_id` is replayed under Sprint H, the assertion "each spawn tool call renders as a regular ToolCallBadge" is already false in main — ToolCallBadges don't render during replay at all (this is the exact problem Sprint I fixes). The scenario is a no-regression claim but under Sprint H alone, the feature is broken.
- **Impact**: Scenario 12 can't be written as a Playwright test that passes against Sprint H's branch without Sprint I also merged. TDD plan row referencing this scenario is missing. If someone runs E2E against Sprint H only, Scenario 12 silently misrepresents the current state.
- **Recommendation**: Either (a) delete Scenario 12 from Sprint H (belongs to Sprint I); or (b) rephrase to: "Given Sprint H is the only Sprint merged, When a historical session replay runs (pre-Sprint-I pipeline), Then no SubagentBlock is rendered AND the existing markdown-text replay behavior is unchanged." Add a TDD test that traces to it.

---

#### [MAJ-006] "100+ steps" threshold is a magic number with no rationale and no test dataset

- **Lens**: Ambiguity (AMB-03, AMB-07)
- **Affected section**: Edge Cases — "Subagent emits a stream of >100 steps"
- **Description**: Where does 100 come from? Is it the same threshold as existing UI rules? Should it be configurable? What happens at exactly 100? At 101? At 99 + 1 in-flight? The spec says "100+" but doesn't define the display format ("100+", "100 steps", ">100"). No test dataset covers this boundary.
- **Impact**: Three different implementers ship three different thresholds. Chances are zero users actually hit >100 steps in 2026; burning design thought on this is scope creep, but the spec bakes it in without acknowledging.
- **Recommendation**: Either (a) delete this edge case (YAGNI — no sub-turn realistically emits 100+ tool calls in the current architecture, which caps at `maxIterations = 10` per sub-turn); or (b) pick the exact threshold, pick the display string, add a test dataset row D7, and specify the config key for tuning.

---

#### [MAJ-007] Concurrency/race conditions across sibling spans are not addressed

- **Lens**: Incompleteness (INC-05); Incorrectness (COR-06)
- **Affected section**: Edge Cases — "Two sibling spawn calls in the same assistant message"; BDD Scenario 13
- **Description**: Two spawn calls issued back-to-back from the parent LLM can run concurrently (sub-turns are not serialized; `concurrencySem` allows up to `maxConcurrentSubTurns` = 5 in parallel). Their nested tool-call WS frames therefore interleave on the wire. The spec says each is grouped by span_id — but the reducer in `src/store/chat.ts` currently has `toolCalls: Record<string, ToolCall>` keyed by call_id. There is no mention of race conditions like: (a) subagent_start for s1 arrives before subagent_start for s2 but a tool_call_start for s2 arrives in between (reducer must handle out-of-order delivery). (b) Two tool_call_start frames with the SAME call_id arrive if a provider retries — which span claims it? (c) A tool_call_start with parent_call_id="c1" arrives BEFORE subagent_start for c1 (possible if forwarder interleaves).
- **Impact**: Race conditions manifest as tool calls attached to the wrong span, orphan tool calls with no span, or duplicated tool calls across spans. Intermittent UI glitches that pass unit tests but fail in production.
- **Recommendation**: Add FR: "The reducer MUST tolerate out-of-order arrival of `subagent_start` vs `tool_call_*` frames. When a `tool_call_start` with `parent_call_id=P` arrives before a `subagent_start` with `parent_call_id=P`, the tool call is buffered and attached once the start frame arrives (or dropped if no start arrives within 10s and attached to the fallback 'flat' rendering)." Add TDD tests: `ChatStore_ToolCallBeforeSubagentStart_Buffered`, `ChatStore_DuplicateCallID_DeduplicatesPerSpan`.

---

#### [MAJ-008] Span ID generation ambiguity warning is left unresolved

- **Lens**: Ambiguity (AMB-01); Inconsistency (CON-01)
- **Affected section**: Ambiguity Warnings ("How is span_id generated?")
- **Description**: The Ambiguity Warnings section ends with "Confirm ULID; alternative is UUIDv4 or tool-call-id-based derivation." — but this is a spec sprint, not an ambiguity-shipping sprint. Leaving it unresolved means H1 picks one, H2 assumes another, E2E test fixtures use a third. If it's ULID, which ULID library? If it's tool-call-id-based ("s" + spawn's `call_id`?), what's the exact format?
- **Impact**: H2 reducer may string-match "s\\d+" to detect spans and miss ULID-formatted ones. Cross-process stability of span IDs matters for test fixtures.
- **Recommendation**: Resolve before merge. Recommended: `span_id = "span_" + tc.ID` where `tc` is the spawn tool call — deterministic, derivable from persisted data, makes replay-time span reconstruction trivial in Sprint I.

---

#### [MAJ-009] Interaction with `handoff` tool (the existing 1-level precedent) is not examined

- **Lens**: Incompleteness; Inconsistency (CON-06)
- **Affected section**: Available Reference Patterns ("pkg/tools/handoff.go (Sprint G, 1-level enforcement via isHandoffActive callback)"); Behavioral Contract
- **Description**: Handoff tool switches the active agent; spawn tool creates a sub-turn. What if inside a subagent, the LLM calls `handoff`? Today handoff swaps the active agent on the session — but the subagent runs on an ephemeral session (see `ephemeralStore` in subturn.go line 352). Does handoff work at all in a subagent? Does it try to switch the parent's agent? The spec removes `spawn` from the sub-turn registry but says nothing about `handoff`. If handoff is also registered, a subagent could effectively handoff the parent session — almost certainly not intended.
- **Impact**: Subagent-initiated agent swaps surprise the user. Parent session's active agent silently changes mid-flight.
- **Recommendation**: Add Hard Constraint: "The sub-turn's registry MUST exclude both `spawn` and `handoff` tools (only `subagent` and `return_to_default` and standard tools remain). FR-H-005 extends to the handoff tool." Add TDD test: `TestSubTurnRegistry_OmitsHandoff`.

---

### MINOR Findings

#### [MIN-001] Traceability matrix has FR-H-001 tracing to no user story ("— (schema)")

- **Lens**: Inconsistency (CON-02)
- **Affected section**: Traceability Matrix row 1
- **Description**: The matrix uses the sentinel "— (schema)" to opt-out of user-story traceability for FR-H-001. This is a common spec-gaming pattern: low-level requirements that don't map to user stories often indicate missing rationale. FR-H-001 (adding `ParentCallID` to ToolCall) exists *for* US-1/US-2 (so the UI can reconstruct spans from disk) — it DOES have a user-story trace, just not a direct one.
- **Recommendation**: Change to "US-1, US-2, US-3 (enabling)" with a note clarifying that FR-H-001 is a schema enabler. Don't allow "—" in the user-story column.

---

#### [MIN-002] Success Criterion SC-H-006 requires real-LLM run in Playwright — non-deterministic

- **Lens**: Infeasibility (FEA-03)
- **Affected section**: SC-H-006 ("The collapsed header step counter increments at least once during a real-LLM spawn run that makes ≥2 nested tool calls (observed in the Playwright run)")
- **Description**: Real-LLM runs are flaky: different providers may decide to use 1 tool call, or no tool calls, or 10. SC-H-006 requires observable increment — but under OpenRouter CI the LLM may return without calling a tool at all. Sprint-H's scenario provider path (used elsewhere in the spec) would make this deterministic but SC-H-006 explicitly says "real-LLM".
- **Recommendation**: Split: SC-H-006a (deterministic — scenario provider, step counter verifiably increments) as a required gate; SC-H-006b (real-LLM, best-effort, not blocking) as a smoke check.

---

#### [MIN-003] Naming inconsistency: "subagent" vs "sub-turn" vs "spawn" used interchangeably

- **Lens**: Inconsistency (CON-01)
- **Affected section**: Throughout
- **Description**: "Subagent span", "spawn call", "sub-turn", "nested tool call" are conflated. The codebase distinguishes them (a `SpawnTool` creates a `SubTurn` via `SubagentManager`'s async path; `SubagentTool` creates one synchronously). The UI concept is "SubagentBlock" but the wire frame is `subagent_start` and the backend concept is a sub-turn. Which framing is canonical?
- **Recommendation**: Add a one-line glossary: "span" = "A UI grouping tied to a single `spawn` tool invocation and its sub-turn." Use "sub-turn" for agent-loop concepts, "span" for UI/wire concepts, "spawn" for the tool.

---

#### [MIN-004] "Collapsed header task label" truncation rule deferred to Ambiguity Warning

- **Lens**: Ambiguity (AMB-03, AMB-07)
- **Affected section**: Ambiguity Warnings ("is `SpawnTool.Parameters.label` used if set, else `task` truncated to 60 chars?")
- **Description**: 60 chars is a magic number; no test dataset covers boundary values. If label is absent and task is 59 / 60 / 61 chars, what's the output? Is the truncation at word boundary, graceme, char, byte? For RTL / CJK, byte truncation can corrupt.
- **Recommendation**: Resolve: "label if present, else task.slice(0, 60) + (task.length > 60 ? '…' : '')". Add test-dataset entries for exactly 60 chars, 61 chars, emoji boundary.

---

#### [MIN-005] a11y tests rely on axe without specifying rules or severity

- **Lens**: Ambiguity (AMB-02)
- **Affected section**: BDD Scenario 10; SC-H-005 ("axe baseline on pages containing a SubagentBlock shows zero new violations")
- **Description**: "Zero new violations" is relative to a baseline that's never defined. If the current baseline has violations, does Sprint H's addition need to not add more of those same rule violations, or no new rule categories? axe can be configured with WCAG 2.0 vs 2.1 vs 2.2, AA vs AAA.
- **Recommendation**: Specify "axe runs at WCAG 2.1 AA; zero new violations means zero violations of any rule introduced by elements under `[data-testid^=subagent-]`. Baseline: current `main` axe output."

---

#### [MIN-006] No observability requirement — how do we debug a broken span in production?

- **Lens**: Inoperability (OPS-02, OPS-04)
- **Affected section**: Entire spec
- **Description**: No FR requires logging span lifecycle on the backend. When a user reports "my SubagentBlock stuck at 'running'", an on-call has no structured logs to grep for (`slog.Debug` or `slog.Info` with span_id). No metric tracks "orphan subagent spans per day".
- **Recommendation**: Add FR-H-011: "The backend MUST emit structured `slog.Debug` entries on `subagent_start` and `subagent_end`, keyed by `span_id`, `parent_call_id`, and `agent_id`. On orphan (child turn finishes after parent), emit `slog.Warn`." Add operational metric: `omnipus_subagent_span_duration_ms` histogram with status label.

---

### Observations

#### [OBS-001] The span schema could live entirely in UI state

- **Lens**: Overcomplexity (CPX-04, CPX-09)
- **Affected section**: FR-H-001; Hard Constraints ("Forward-only schema")
- **Suggestion**: Because FR-H-004 already requires `parent_call_id` on live wire frames, the SPA can reconstruct spans purely from the streamed frame sequence without persisting `parent_call_id` on disk. FR-H-001 is there to support Sprint I's replay, but the persisted data could equivalently be inferred at replay time by observing that a `spawn` tool call's `Result` contains the child's sub-turn output (current flattened format). Adding a schema field is a one-way door; consider whether the replay-side reconstruction is feasible instead. If not, keep FR-H-001 — but document the alternative that was rejected.

---

#### [OBS-002] Visual grammar reuse is asserted but not prototyped

- **Lens**: Incorrectness (COR-05)
- **Affected section**: Available Reference Patterns row 1
- **Suggestion**: `ToolCallBadge` is a relatively simple component. `SubagentBlock` adds step counting, live spinner, nested child rendering, interleaved text — 3-4x the complexity. "Reuses the shell structure" is optimistic. Consider attaching a mockup or shadcn-style component outline to remove ambiguity for H2.

---

#### [OBS-003] Sprint duration is not stated

- **Lens**: Inoperability
- **Affected section**: Status line
- **Suggestion**: "Status: Draft · 2026-04-20" has no target-complete date. For a sprint named "Sprint H", naming an expected duration (3 days? 5 days?) and a merge-by date helps coordination with Sprint I's dependency.

---

#### [OBS-004] The cancellation path for a subagent's own tool calls is assumed but not tested

- **Lens**: Incompleteness
- **Affected section**: Edge Cases — "Subagent is cancelled mid-run"
- **Suggestion**: What triggers sub-turn cancellation? User cancel of the parent turn cascades? Explicit sub-turn cancel? The ephemeral agent context is an INDEPENDENT context (subturn.go line 337: `context.WithTimeout(context.Background(), timeout)`). Parent cancellation doesn't propagate. So "user cancels the turn" may leave the sub-turn running. Add TDD to verify the cascade (or document that it intentionally doesn't cascade for `Critical: true`).

---

## Structural Integrity

### Variant A: Plan-Spec Format

| Check | Result | Notes |
|-------|--------|-------|
| Every user story has acceptance scenarios | PASS | US-1..US-5 all covered. |
| Every acceptance scenario has BDD scenarios | PASS | AS → BDD mapping is clean. |
| Every BDD scenario has `Traces to:` reference | PASS | All 13 scenarios traced. |
| Every BDD scenario has a test in TDD plan | PARTIAL | Scenario 12 has no TDD test row that traces to it (MAJ-005). |
| Every FR appears in traceability matrix | PASS | FR-H-001..FR-H-010 all present. |
| Every BDD scenario in traceability matrix | PARTIAL | Scenario 7 (cancellation), 12 (legacy fallback), 13 (siblings) are traced via "Edge Cases" text, not scenario number — inconsistent with matrix format. |
| Test datasets cover boundaries/edges/errors | PARTIAL | No dataset for >100 steps boundary (MAJ-006), no label truncation boundary (MIN-004), no orphan-span or race dataset (MAJ-007). |
| Regression impact addressed | PARTIAL | Lists 4 existing tests but doesn't cover `spawn_grandchild_test.go` which is in direct conflict (CRIT-001). |
| Success criteria are measurable | PARTIAL | SC-H-006 depends on real-LLM non-determinism (MIN-002); SC-H-005 baseline ambiguous (MIN-005). |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Concurrency / out-of-order frames | No test for `tool_call_start` arriving before `subagent_start`, or sibling spans interleaving on the wire | Scenario 13 + edge |
| Parent-child correlation identity | No test asserting `ToolCall.ParentCallID == parent spawn's ToolCall.ID` | FR-H-001 impl detail |
| Event payload enrichment | No test asserting `ToolExecStartPayload.ParentCallID` is set when sub-turn tool calls | FR-H-004 (CRIT-002) |
| Orphan sub-turn handling | No test for "parent finishes before sub-turn" lifecycle | MAJ-004 |
| Registry filter generality | `handoff` tool filter is not tested (only `spawn`) | MAJ-009 |
| Cross-sprint integration | No integration test for Sprint H + I together (legacy + new behavior in one session) | Scenario 7 (I) |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| D-new (step count boundary) | exactly-100, >100 threshold | Add: `{steps: 99, display: "99 steps"}`, `{steps: 100, display: "100+ steps"}` |
| D-new (label truncation) | exactly 60 chars, 61 chars, emoji | Add three rows for AMB-03 resolution |
| D-new (sibling overlap) | two spans with interleaved tool calls | Add for MAJ-007 |
| D-new (orphan) | sub-turn result arriving after parent done | Add for MAJ-004 |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| `subagent_start/end` WS frames | ok | ok | ok | ok | risk | ok | D: no rate-limit on span frame emission rate — a malicious LLM scenario could emit millions of nested tool calls; no bound on span's step count. |
| `ToolCall.ParentCallID` persistence | ok | risk | ok | ok | ok | ok | T: no integrity check — a hand-edited JSONL file can forge a ParentCallID to attach a tool call to any span. Acceptable for local-only file-based store but should be documented. |
| Sub-turn registry filter | ok | ok | ok | ok | ok | risk | E: if the filter is bypassed (e.g., via a code path that constructs a sub-turn registry without going through `subturn.go`), a subagent gains `spawn` and the grandchild invariant breaks. Add a central enforcement point or an assertion. |

Legend: risk = identified threat not mitigated in spec; ok = adequately addressed or not applicable.

---

## Unasked Questions

1. Has Plan 3 §1 ("subagent grandchildren allowed, unlimited depth, budget-only caps") been formally reversed? If not, this entire sprint is contrary to a ratified decision.
2. Where is `parent_call_id` injected into live `ToolExecStartPayload` events? If it goes on the payload, every emit site in `pkg/agent/loop.go` must be updated — is that in scope?
3. When the spawn tool returns its `AsyncResult` synchronously in milliseconds but the sub-turn runs for minutes, does `subagent_end` tie to tool return or sub-turn completion? (The spec reads as the former; semantics require the latter.)
4. Is `ToolCall.ParentCallID` the parent's `ToolCall.ID`, the wire-layer `call_id`, or a new span ID? Currently these are the same string in-memory; the invariant must be documented.
5. Does the sub-turn's tool registry filter apply only to `spawn`, or also to `handoff`? What about team tools if they exist?
6. What happens when the parent turn finishes before the sub-turn? Does `subagent_end` still emit? If yes, does it reach the browser (connection may be idle)?
7. What exactly counts as a "step" — one `tool_call_start`, or one `tool_call_result`?
8. If sibling spawn calls produce interleaved WS frames, how does the reducer handle out-of-order `subagent_start` relative to the first `tool_call_start` carrying its `parent_call_id`?
9. Does a subagent's intermediate assistant text (tokens between tool calls) stream to the frontend? If yes, what's the frame type?
10. Span ID generation format: ULID, UUIDv4, or derived from `spawn` call's ID? Pick one before merge.

---

## Verdict Rationale

BLOCK. Two issues rise to ship-stopping severity. CRIT-001 (contradiction with Plan 3 §1, encoded in passing tests) cannot be safely merged without a ratified decision reversal; absent that, Sprint H breaks an explicit product contract. CRIT-002 and CRIT-003 together render the live-subagent-visibility user story (US-1, the entire reason for the sprint) mechanically unachievable as specified — there is no path from spawn-tool invocation to `parent_call_id`-tagged WS frames reaching the browser, and `subagent_end` is tied to the wrong event. CRIT-004 adds persisted-data ambiguity that compounds across Sprint I. The MAJOR findings collectively indicate the spec was not tested against actual in-tree symbol behavior (registry cloning API, event payload shape, async sub-turn lifecycle, ephemeral session semantics).

The sprint's UI layer (H2) is defensible on its own — given a correctly-tagged frame stream, the SubagentBlock / reducer changes are straightforward. But the wire contract feeding it is underspecified in ways that will cause H1 to ship something that doesn't produce the frames H2 needs.

### Recommended Next Actions

- [ ] Resolve CRIT-001 by filing an ADR reversing Plan 3 §1 OR reframing FR-H-005 as an opt-in policy. Reconcile `spawn_grandchild_test.go`.
- [ ] Resolve CRIT-002 by adding FR: `ToolExecStartPayload.ParentCallID` field + emit-site updates + sub-turn `parentSpawnCallID` field in `turnState`.
- [ ] Resolve CRIT-003 by rewriting FR-H-002/003 to bind `subagent_start`/`end` to `EventKindSubTurnSpawn`/`EventKindSubTurnEnd`, not to spawn-tool `Execute` return.
- [ ] Resolve CRIT-004 by specifying the `ParentCallID` ↔ `ToolCall.ID` invariant in Hard Constraints.
- [ ] Add FRs for `handoff` tool exclusion (MAJ-009), out-of-order frame tolerance (MAJ-007), orphan-span terminal state (MAJ-004), and span-lifecycle logging (MIN-006).
- [ ] Specify span_id format, step-counting rule, and task-label truncation in Ambiguity Warnings before H1 starts.
- [ ] Split SC-H-006 into deterministic + smoke variants.
- [ ] Re-run `/grill-spec` after revisions.
