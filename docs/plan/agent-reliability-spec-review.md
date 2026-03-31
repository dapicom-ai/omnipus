# Adversarial Review: Agent Loop Reliability — OpenClaw Parity

**Spec reviewed**: docs/plan/agent-reliability-spec.md
**Review date**: 2026-03-30
**Verdict**: REVISE

## Executive Summary

The spec identifies real, impactful bugs in the agent loop (silent response loss, limited retry scope, no turn timeout) and proposes reasonable fixes. However, it contains a critical inconsistency with the existing codebase's multi-key fallback mechanism, several incomplete error-handling specifications, and missing concurrency/idempotency analysis that will cause implementation confusion or new bugs. 9 findings across 8 lenses, with 0 critical but 5 major issues requiring revision before implementation.

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| MAJOR | 5 |
| MINOR | 3 |
| OBSERVATION | 3 |
| **Total** | **11** |

---

## Findings

### MAJOR Findings

#### [MAJ-001] Multi-key rotation proposal conflicts with existing fallback chain mechanism

- **Lens**: Incorrectness
- **Affected section**: User Story 4, FR-005, Symbols table entry for `ModelConfig.APIKeys`
- **Description**: The spec proposes adding "rotation index tracking" to `ModelConfig.APIKeys` and implementing key rotation inside the retry loop. But the codebase already implements multi-key rotation via `expandMultiKeyModels()` in `config.go:1396`, which expands multiple API keys into separate `FallbackCandidate` entries processed by `FallbackChain.Execute()` in `fallback.go:102`. The existing mechanism uses the `CooldownTracker` (with per-provider cooldown, billing disable, and 24h failure window) to manage key rotation at the fallback chain level — not inside the retry loop. The spec's approach would create a parallel, conflicting rotation mechanism.
- **Impact**: If implemented as specified, there would be two competing key-rotation systems: the fallback chain (which already works) and the new retry-loop rotation. They would interfere with each other's cooldown state, causing unpredictable behavior — e.g., the retry loop rotates to key 2, but the fallback chain has key 2 in cooldown and skips it.
- **Recommendation**: Rewrite User Story 4 to leverage and enhance the existing `FallbackChain` + `CooldownTracker` + `expandMultiKeyModels` mechanism instead of proposing a new rotation index. The real gap is that the *retry loop inside `runTurn`* (loop.go:2056) does not use the fallback chain — it calls `callLLM` directly. The fix should be to route retryable errors through the fallback chain, not to add a separate rotation mechanism. Update the Symbols table to reference `FallbackChain.Execute`, `CooldownTracker`, and `expandMultiKeyModels` instead of "rotation index tracking."

---

#### [MAJ-002] Turn timeout interaction with tool execution is underspecified

- **Lens**: Incompleteness
- **Affected section**: User Story 1, FR-001, FR-011
- **Description**: The spec says the timeout "wraps the entire turn" (Ambiguity Warning #1) but does not specify what happens to in-progress tool executions when the timeout fires. `runTurn` at loop.go:1660 launches tool calls that may have side effects (file writes, shell commands, web requests). Cancelling the context mid-tool-execution could leave files half-written, shell commands running as orphans, or HTTP connections dangling. The spec only addresses context compaction and retry — not cleanup of tool side effects.
- **Impact**: A turn timeout during a file edit could leave a file in a corrupt state. A timeout during a shell command could leave a process running (the background kill spec in US6 only covers explicitly-backgrounded processes, not foreground tools that become orphans after context cancellation).
- **Recommendation**: Add acceptance scenarios for: (a) turn timeout fires while a file edit tool is executing — what happens to the file? (b) turn timeout fires while a foreground shell command is running — is the command killed? (c) turn timeout fires while a web fetch is in progress — is the HTTP request cancelled? Specify whether tool cleanup is synchronous (block on cleanup before retry) or best-effort.

---

#### [MAJ-003] Retry loop expansion scope is unclear — retry loop vs. fallback chain

- **Lens**: Ambiguity
- **Affected section**: User Story 2, FR-002, FR-010, Symbols table "LLM retry loop (loop.go:2056-2111)"
- **Description**: The spec says "System MUST retry LLM calls on retryable errors (429, 5xx, auth with rotation) up to `max_retries` (default 5)." But the existing code has *two* retry/fallback layers: (1) the inline retry loop in `runTurn` (loop.go:2056, currently `maxRetries=2`), and (2) the `FallbackChain.Execute` used by the provider layer. The spec does not clarify which layer should handle which errors. Currently, the inline loop handles timeout and context overflow; the fallback chain handles everything else via `ClassifyError`. Adding 429/5xx/auth retry to the inline loop would duplicate what the fallback chain already does.
- **Impact**: An implementer could reasonably add retry logic in the wrong layer, causing double-retry behavior (retry in the loop, then fallback chain retries again) or retry count multiplication (5 retries x N fallback candidates).
- **Recommendation**: Explicitly state the division of responsibility: the inline `runTurn` retry loop handles *turn-level* concerns (timeout with compaction, context overflow with compaction), while provider-level transient errors (429, 5xx, auth) should be handled by the `FallbackChain`. If the intent is to add retries *within* the inline loop for these errors, specify that the fallback chain should NOT also retry them, and explain why the inline loop is the better location.

---

#### [MAJ-004] Post-turn response loss fix is described at symptom level, not root cause

- **Lens**: Incompleteness
- **Affected section**: User Story 3, FR-004, post-turn steering code (loop.go:445-525)
- **Description**: The spec identifies "5 code paths" where responses are silently discarded but does not enumerate them. Reading the actual code at loop.go:445-525, I count 4 `return` statements that skip `publishResponseIfNeeded`: (1) line 458 after `buildContinuationTarget` fails, (2) line 486 after `Continue()` returns error in the first steering loop, (3) line 489 after `Continue()` returns empty in the first loop, (4) line 513 after `Continue()` returns error in the drain loop. The spec says to "add calls before every early return" but does not specify which variable holds the response at each point — at line 458, it's `finalResponse`; at line 486/489, it could be the original `response` or a `continued` value from a prior iteration. Getting this wrong means publishing an empty string or stale response.
- **Impact**: An implementer may add `publishResponseIfNeeded` calls at the wrong points or with the wrong variable, causing either duplicate publishes or publishing empty content. The existing `publishResponseIfNeeded` already skips empty strings (line 630-632), but `HasSentInRound()` could cause the second publish to be suppressed, losing the real response.
- **Recommendation**: Enumerate each specific `return` path by line number/context, specify exactly which response variable should be published at each, and address the `HasSentInRound()` idempotency check — should it be reset between the original response and the guard publish? Consider whether a `defer` pattern (defer the publish at the top of the goroutine) is a more robust fix than patching each return.

---

#### [MAJ-005] Backoff strategy has no jitter — thundering herd under shared rate limits

- **Lens**: Incompleteness
- **Affected section**: FR-009, Ambiguity Warning #4
- **Description**: FR-009 specifies "exponential backoff (base: 2s, factor: 2x, max: 30s)" but does not include jitter. When multiple agent instances (or multiple users on a SaaS deployment) hit the same provider's rate limit simultaneously, deterministic exponential backoff causes all instances to retry at the exact same times (2s, 4s, 8s...), creating repeated synchronized spikes — the "thundering herd" problem.
- **Impact**: Under shared rate limits (common with OpenAI, Anthropic), synchronized retries will repeatedly trigger 429s, causing all agents to exhaust their retry budget simultaneously instead of spreading load.
- **Recommendation**: Add jitter to FR-009: "System MUST use exponential backoff with full jitter between retries (base: 2s, factor: 2x, max: 30s, jitter: uniform random [0, calculated_delay])." Reference the existing `CooldownTracker` which already uses exponential backoff with `math.Pow` — ensure the new retry backoff is consistent with or delegates to that mechanism.

---

### MINOR Findings

#### [MIN-001] Default `max_retries: 5` is aggressive for billing-adjacent scenarios

- **Lens**: Overcomplexity
- **Affected section**: FR-002, Config section "max_retries (int, default 5)"
- **Description**: Five retries with exponential backoff (2+4+8+16+30 = 60s total wait) is aggressive. For a user sitting at a chat interface, 60 seconds of silent waiting before an error message is poor UX. The existing code uses `maxRetries=2` which is 10s total — a much better user experience. Furthermore, the `CooldownTracker` already provides cross-request resilience, so per-request retries don't need to be this aggressive.
- **Recommendation**: Consider defaulting to `max_retries: 3` (2+4+8 = 14s total) and document the tradeoff. Alternatively, specify that the UI should show a "Retrying..." indicator during backoff (currently not in the spec).

---

#### [MIN-002] "65% context window" threshold is a magic number without justification

- **Lens**: Ambiguity
- **Affected section**: User Story 1 Scenario 1, FR-003
- **Description**: The threshold "65% of the window" for triggering auto-compaction appears without justification. Why 65% and not 50% or 80%? The existing `SummarizeTokenPercent` config field (line 422) already controls when summarization kicks in. Is the 65% figure related to `SummarizeTokenPercent`? Should they share a config value?
- **Recommendation**: Either justify the 65% threshold with data/reasoning, reference its relationship to `SummarizeTokenPercent`, or make it configurable with a documented default.

---

#### [MIN-003] Shutdown timeout default of 30s may conflict with turn timeout default of 300s

- **Lens**: Inconsistency
- **Affected section**: FR-008 vs FR-001
- **Description**: The shutdown timeout defaults to 30s (FR-008, US7), but the turn timeout defaults to 300s (FR-001, US1). If the turn is 200s into a 300s timeout when SIGTERM arrives, the 30s shutdown window will expire before the turn completes, causing a force-cancel. The spec says "waits up to `shutdown_timeout_seconds` (default: 30) for the turn to complete" but a turn can legitimately run for 5 minutes.
- **Recommendation**: Either increase the default `shutdown_timeout_seconds` to match `timeout_seconds` (or derive it: `shutdown_timeout = min(timeout_seconds, 60)`), or document that shutdown may force-cancel long-running turns and that operators should tune `shutdown_timeout_seconds` accordingly.

---

### Observations

#### [OBS-001] Existing `expandMultiKeyModels` already solves most of User Story 4

- **Lens**: Overcomplexity
- **Affected section**: User Story 4
- **Description**: The codebase already expands multiple API keys into separate fallback candidates with per-key cooldown tracking (`expandMultiKeyModels` in config.go:1396, `CooldownTracker` in cooldown.go). The real gap is narrower than the spec suggests — it's that the inline retry loop in `runTurn` doesn't delegate to the fallback chain for transient errors.
- **Suggestion**: Shrink User Story 4 to focus on the actual gap: ensuring the retry loop delegates to the fallback chain (or at minimum, the fallback chain is invoked for auth/rate-limit errors). The acceptance scenarios about "rotate to the second key" and "cooldown" are already implemented by the fallback chain.

---

#### [OBS-002] No UI feedback during retry backoff

- **Lens**: Inoperability
- **Affected section**: User Story 2
- **Suggestion**: With up to 60s of retries (5 retries with exponential backoff), the user has no indication that the agent is retrying vs. hung. Consider specifying that a "Retrying..." or "Rate limited, waiting..." message is published to the chat after the first retry, similar to how context overflow already publishes "Compressing history and retrying..." (loop.go:2138).

---

#### [OBS-003] Spec does not reference existing event system for observability

- **Lens**: Inoperability
- **Affected section**: Behavioral Contract, all user stories
- **Suggestion**: The codebase already has `al.emitEvent(EventKindLLMRetry, ...)` (loop.go:2087) for retry events. The spec should specify which events are emitted for the new behaviors (turn timeout, empty response retry, key rotation, background kill, shutdown wait). This is low-effort — the infrastructure exists — but without explicit event requirements, implementers may skip it, degrading observability.

---

## Structural Integrity

| Check | Result | Notes |
|-------|--------|-------|
| Every goal/objective has acceptance criteria | PASS | All 7 user stories have acceptance scenarios |
| Cross-references are consistent | FAIL | FR-005 references "rotation" but codebase uses fallback chain; Symbols table references "rotation index tracking" which doesn't exist |
| Scope boundaries are explicit | PASS | Non-behaviors section clearly defines what's out of scope |
| Success criteria are measurable | PASS | SC-001 through SC-007 are all observable/testable |
| Error/failure scenarios addressed | PASS | Explicit non-behaviors and error flows are well-specified |
| Dependencies between requirements identified | FAIL | FR-005 (key rotation) depends on how FR-002 (retry expansion) is implemented, but no dependency is stated; FR-003 (compaction on timeout) depends on FR-001 (timeout) but no ordering is specified |

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Scenarios |
|----------|----------------|-------------------|
| Concurrency | No test for concurrent turn timeout + message arrival — what happens if a new message arrives while a timed-out turn is compacting and retrying? | US1, US3 |
| Idempotency | No test for duplicate `publishResponseIfNeeded` calls — if the guard publish and the normal publish both fire, is the response sent twice? | US3 |
| State persistence | No test for turn timeout during session save — if the timeout fires while `session.Save()` is in progress, is the session corrupted? | US1, US7 |
| Integration | No test for fallback chain + retry loop interaction — does the fallback chain's own retry interfere with the new retry loop? | US2, US4 |
| Resource exhaustion | No test for many background processes hitting timeout simultaneously — does mass SIGTERM/SIGKILL cause file descriptor exhaustion? | US6 |

### Dataset Gaps

| Dataset | Missing Boundary Type | Recommendation |
|---------|----------------------|----------------|
| Timeout values | Zero, negative, very large (MAX_INT) | Test `timeout_seconds: -1`, `timeout_seconds: 2147483647` — what happens? |
| Retry counts | Zero retries | Test `max_retries: 0` — should this disable retries entirely? The spec doesn't say. |
| API key lists | Empty list, single key, very large list (100 keys) | Test degenerate key configurations |
| Empty response | Response with whitespace-only content | Is `"   "` considered empty? The spec says "no content, no tool calls" but whitespace-only is ambiguous |

---

## STRIDE Threat Summary

| Component | S | T | R | I | D | E | Notes |
|-----------|---|---|---|---|---|---|-------|
| API Key Rotation | ok | ok | risk | risk | ok | ok | No audit trail for key rotation events (SEC-03); failed key attempts may log the key or provider details in error messages (SEC-06) |
| Turn Timeout | ok | ok | ok | ok | risk | ok | A malicious tool could exploit the retry-after-compaction to get a second execution within one user turn (DoS via amplification) |
| Background Process Kill | ok | ok | risk | ok | ok | ok | No audit log for process kills; no verification that SIGKILL succeeded |
| Retry Backoff | ok | ok | ok | ok | risk | ok | Deterministic backoff without jitter enables timing-based DoS amplification |
| Shutdown | ok | risk | ok | ok | ok | ok | Force-cancel after timeout could leave session data in inconsistent state if atomic write is interrupted |

---

## Unasked Questions

1. **What happens to the `HasSentInRound()` flag when `publishResponseIfNeeded` is called as a guard before return?** If a tool already sent a message via `MessageTool`, the guard publish will be suppressed. Is this the correct behavior? The spec assumes publishing always works.

2. **Should `max_retries` be per-turn or per-LLM-call?** A single turn may make multiple LLM calls (initial + continuations). Does the retry budget reset between LLM calls within the same turn?

3. **What is the interaction between `maxRetries=2` (current) for timeout/context errors and the new `max_retries=5` for all retryable errors?** Are they the same counter? Separate counters? Does a context overflow retry consume one of the 5 retries, or is it additional?

4. **What happens when `forceCompression` fails?** FR-003 says "auto-compact context when turn timeout fires" but doesn't specify what happens if compaction itself fails (e.g., summary LLM call times out during compaction).

5. **How does the turn timeout interact with sub-turns?** The codebase has `SubTurnConfig` with `ConcurrencyTimeoutSec`. Does the turn-level timeout override, nest within, or run independently of sub-turn timeouts?

6. **Should the exponential backoff respect the `Retry-After` header from 429 responses?** Many LLM providers include this header. Ignoring it and using a fixed backoff may cause retries to fail if the provider specifies a longer cooldown.

7. **What is the behavior when `timeout_seconds` is set but the LLM provider has its own `RequestTimeout` (config.go line ~1429)?** Which timeout wins? Can the turn timeout fire before or after the provider's request timeout?

---

## Verdict Rationale

The spec identifies genuine, high-impact bugs in the agent loop and proposes reasonable solutions at the user-story level. However, the implementation direction for multi-key rotation (MAJ-001) directly conflicts with the existing `FallbackChain` + `CooldownTracker` + `expandMultiKeyModels` architecture, which would lead to a parallel competing mechanism. The ambiguity about which retry layer handles which errors (MAJ-003) compounds this — an implementer cannot proceed without a clear decision on retry-loop-vs-fallback-chain responsibility. The post-turn response loss fix (MAJ-004) needs concrete enumeration of the return paths and the `defer` vs. per-return-guard design decision. The missing jitter in the backoff strategy (MAJ-005) will cause production issues under shared rate limits.

No critical findings block implementation entirely, but the 5 major findings require revision to avoid building the wrong thing or introducing new bugs.

### Recommended Next Actions

- [ ] Rewrite User Story 4 and FR-005 to leverage existing `FallbackChain`/`CooldownTracker`/`expandMultiKeyModels` instead of proposing rotation index tracking (MAJ-001)
- [ ] Clarify retry-loop vs. fallback-chain responsibility for each error class (MAJ-003)
- [ ] Add tool-cleanup acceptance scenarios for turn timeout (MAJ-002)
- [ ] Enumerate specific return paths and specify `defer` vs. per-return guard for response loss fix (MAJ-004)
- [ ] Add jitter to the backoff specification in FR-009 (MAJ-005)
- [ ] Address the 7 unasked questions, particularly #2 (retry scope), #3 (counter interaction), and #5 (sub-turn timeout interaction)
