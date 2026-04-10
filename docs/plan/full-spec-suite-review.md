# Adversarial Review: Full Spec Suite + shadcn/ui Mandate Check

**Specs reviewed**: All 9 specs in `docs/plan/` plus BRD Appendix C (UI Spec), CLAUDE.md
**Review date**: 2026-04-01
**Verdict**: REVISE

## Executive Summary

The shadcn/ui question is settled: shadcn/ui is mandatory per CLAUDE.md tech stack, BRD Appendix C Table C.2, and Wave 0 FR-004. It is a foundational technology decision, not optional. The full spec suite contains 4 MAJOR findings and 8 MINOR findings across cross-spec consistency, completeness, and feasibility concerns. No CRITICAL findings — the individual specs are well-structured. The main risks are inter-spec coordination gaps and several unresolved ambiguities that could cause implementation conflicts.

| Severity | Count |
|----------|-------|
| CRITICAL | 0 |
| MAJOR | 4 |
| MINOR | 8 |
| OBSERVATION | 5 |
| **Total** | **17** |

---

## Part 1: Is shadcn/ui Mandatory?

**Answer: Yes, unambiguously mandatory.**

Evidence chain:

1. **CLAUDE.md (Tech Stack section)**: `"shadcn/ui (Radix + Tailwind CSS v4)"` is listed as the frontend component library. CLAUDE.md is the source of truth for all implementation decisions.

2. **BRD Appendix C, Table C.2**: `Components | shadcn/ui (Radix + Tailwind)` — declared as the component layer in the technology stack table.

3. **BRD Appendix C, Section C.2.2** (Component Architecture): Four of six top-level components are annotated `(shadcn/ui)`: CommandCenter, AgentsView, SkillsBrowser, SettingsPanel.

4. **Wave 0 spec, FR-004**: `"System MUST theme all shadcn/ui components to use brand colors by default, including but not limited to Button, Card, Input, Dialog, and Tooltip."` — This is a MUST requirement with RFC 2119 language.

5. **Wave 0 spec, User Story 2 (P0)**: shadcn/ui component theming is a P0 story — the highest priority.

6. **Existing codebase**: 22 shadcn/ui components already installed in `src/components/ui/` (button, card, input, dialog, select, tabs, avatar, progress, accordion, dropdown-menu, separator, textarea, toast-container, badge, switch, sheet, label, slider, table).

7. **Wave 0 spec, Ambiguity #8**: Explicitly states `"YES — install AssistantUI and shadcn from the beginning."` Both must be present in Wave 0 `package.json`.

**Conclusion**: shadcn/ui is not merely recommended — it is a mandatory, P0, already-implemented technology decision. Replacing it would require a BRD amendment, CLAUDE.md update, rework of Wave 0, and removal of 22 existing components. There is no ambiguity here.

---

## Part 2: Cross-Spec Findings

### MAJOR Findings

#### [MAJ-001] Wave 5a backend scope ambiguity remains partially unresolved

- **Lens**: Incompleteness
- **Affected section**: Wave 5a, Ambiguity #1 and Assumption #3
- **Description**: Wave 5a Ambiguity #1 asks "should Wave 5a include implementing backend REST endpoints?" and the Clarifications section says "Resolved — Wave 5a implements both frontend + backend together." However, the Assumptions section still says "REST API endpoints (currently 501 stubs) will be implemented either as part of Wave 5a or a prerequisite wave." These two statements contradict each other. Additionally, the spec's functional requirements, BDD scenarios, and TDD plan are entirely frontend-focused — there are zero backend FRs, zero backend BDD scenarios, and zero Go tests in the TDD plan.
- **Impact**: If the backend-lead reads the spec's FR list and TDD plan, they will conclude their work has no specification. If the frontend-lead reads the clarification, they will assume backend work is included but find no backend spec to implement against. This will cause either duplicated effort or a gap.
- **Recommendation**: Either (a) add a separate "Backend Functional Requirements" section to Wave 5a with FRs for each REST endpoint and WebSocket handler, or (b) create a Wave 5a-backend spec that the backend-lead works from while frontend-lead works from the existing spec. The traceability matrix must cover both.

---

#### [MAJ-002] Agent Task Management spec conflicts with Wave 5a Command Center task board

- **Lens**: Inconsistency
- **Affected section**: `agent-task-management-spec.md` Context section vs. Wave 5a User Story 13
- **Description**: The Agent Task Management spec states: "This spec replaces the GTD task board with an agent work queue — a delegation system." But Wave 5a User Story 13 implements the Command Center with a GTD kanban board (5 columns: Inbox, Next, Active, Waiting, Done) and drag-and-drop. If the task management spec replaces the GTD board, then Wave 5a is implementing a UI that will be torn out. The two specs define incompatible task models.
- **Impact**: Engineering effort wasted building a GTD board in Wave 5a that gets replaced by the agent work queue. Additionally, the task JSON schema likely differs between the two specs.
- **Recommendation**: Resolve the sequencing. Either (a) Wave 5a implements the agent work queue UI from the start (skip GTD board), or (b) the agent task management spec explicitly states it extends rather than replaces the GTD board, mapping its statuses (queued/running/completed/failed) to GTD columns (Inbox/Active/Done). Document the decision.

---

#### [MAJ-003] No cross-spec WebSocket protocol contract document

- **Lens**: Incompleteness
- **Affected section**: Wave 5a Integration Boundaries, Wave 5b System Agent, Agent Task Management
- **Description**: Wave 5a defines WebSocket JSON frame types inline (token, done, error, tool_call_start, tool_call_result, exec_approval_request, cancel, message, exec_approval_response, auth). Wave 5b adds system agent interactions. Agent Task Management adds task status events. Agent Reliability adds timeout and retry events. There is no single canonical protocol reference. Each spec defines fragments of the protocol.
- **Impact**: Two engineers implementing different specs could define conflicting `type` field values, inconsistent JSON shapes, or miss event types the other spec expects. When the system agent streams a response about task status, which frame types does it use? Not specified.
- **Recommendation**: Create a `docs/protocol/websocket-protocol.md` document that is the single source of truth for all WebSocket frame types, their JSON schemas, and which component produces/consumes each. All specs reference this document instead of defining inline.

---

#### [MAJ-004] Agent Reliability spec's retry/timeout changes have unspecified UI implications

- **Lens**: Incompleteness
- **Affected section**: `agent-reliability-spec.md` User Story 1, Acceptance Scenario 6
- **Description**: The agent reliability spec states: "Given a turn timeout fires mid-stream (tokens already streamed to user), Then the partial streamed content is preserved, and the timeout message is appended." Wave 5a's cancel/interrupt spec (User Story 17) handles user-initiated cancel, but there is no specification for system-initiated interruptions (timeouts, context overflow compaction). The UI has no defined behavior for receiving a `timeout` or `compaction_triggered` event from the backend.
- **Impact**: When the backend times out a turn and appends a message, the UI will either not recognize the event type (unknown_type → ignored per test dataset #6), or the partial message will render incorrectly because the "done" frame was never sent.
- **Recommendation**: Add WebSocket event types for system-initiated interruptions to Wave 5a: `{"type":"timeout","partial_content":"...","message":"Turn timed out after retry."}` and `{"type":"compaction","summary":"..."}`. Define UI rendering for both (similar to cancel's "(interrupted)" label). Add these to the protocol document from MAJ-003.

---

### MINOR Findings

#### [MIN-001] Tablet breakpoint inconsistency across specs

- **Lens**: Inconsistency
- **Affected section**: Wave 5a FR-031 vs. BRD Appendix C Section C.5
- **Description**: Wave 5a FR-031 defines breakpoints as "desktop (>1024px), tablet (640-1024px), phone (<640px)." BRD Appendix C Section C.5 defines "Desktop (>1024px), Tablet (768-1024px), Phone (<768px)." The tablet and phone breakpoints differ by 128px.
- **Recommendation**: Align to one set of breakpoints. The BRD's 768px phone breakpoint is the industry standard. Update Wave 5a FR-031 to match.

---

#### [MIN-002] Wave 0 does not specify which shadcn/ui components to install

- **Lens**: Ambiguity
- **Affected section**: Wave 0 User Story 2 and FR-004
- **Description**: Wave 0 says "theme all shadcn/ui components" and lists "Button, Card, Input, Dialog, and Tooltip" but uses "including but not limited to." It does not define the complete initial set. The existing codebase has 22 components, but Wave 0's spec predates some of them.
- **Recommendation**: Add an explicit list of shadcn/ui components to install in Wave 0 (or document that components are added as-needed by subsequent waves). The current 22 components in `src/components/ui/` should be the baseline.

---

#### [MIN-003] Wave 3 skill trust verification has no UI spec

- **Lens**: Incompleteness
- **Affected section**: Wave 3 Skill Ecosystem, Wave 5a User Story 14
- **Description**: Wave 3 specifies hash verification and trust levels for skills. Wave 5a's Skills Browser (User Story 14) shows "verification status" in skill cards but does not define what verification statuses exist, what icons/colors they use, or how unverified skills are distinguished from verified ones.
- **Recommendation**: Define the verification status values (e.g., verified, unverified, untrusted) and their visual representation in the Skills Browser.

---

#### [MIN-004] Wave 4 WhatsApp QR code has no Web UI component spec

- **Lens**: Incompleteness
- **Affected section**: Wave 4, Acceptance Scenario 1 vs. Wave 5a
- **Description**: Wave 4 says the WhatsApp channel "displays a QR code for pairing." Wave 5a implements the Channels tab in Skills & Tools but does not specify a QR code display component. Where does the QR code render? In the channel configuration? In a modal? In chat via the system agent?
- **Recommendation**: Add a BDD scenario in Wave 5a (or a follow-up spec) for the WhatsApp QR code rendering path.

---

#### [MIN-005] Wave 5b system agent session is not specified in Wave 5a session panel

- **Lens**: Incompleteness
- **Affected section**: Wave 5a User Story 15 (Session Panel) vs. Wave 5b User Story 1
- **Description**: Wave 5a's session panel shows agents in accordion format with their sessions. Wave 5a's Explicit Non-Behaviors says "The system must not render the system agent in the agent selector dropdown for chat." But the session panel is different from the agent selector — it is not specified whether the system agent (Omnipus) appears in the session panel's accordion. Wave 5b assumes a separate "Omnipus session" that the user can navigate to.
- **Recommendation**: Clarify in Wave 5a whether the system agent appears in the session hierarchy panel (it should, since the user needs to navigate to it), while remaining excluded from the chat agent selector dropdown.

---

#### [MIN-006] No spec for the "Pin" feature referenced throughout BRD Appendix C

- **Lens**: Incompleteness
- **Affected section**: BRD Appendix C, Sections C.6.1.6, C.6.1.7
- **Description**: Every rich component in chat has a `[Pin]` action button. The BRD says "Pin saves the response as a persistent artifact accessible from a pinned items list." No spec in `docs/plan/` covers the pin feature — not its data model (though Appendix E mentions `pins/`), not its UI (the pinned items list), and not its lifecycle.
- **Recommendation**: Create a spec for the pin feature or explicitly defer it with a note in Wave 5a's "Explicit Non-Behaviors" section.

---

#### [MIN-007] Wave 5a evaluation scenarios test backend behaviors the spec does not cover

- **Lens**: Inconsistency
- **Affected section**: Wave 5a Evaluation Scenarios, "Rapid message sending"
- **Description**: The "Rapid message sending" evaluation scenario expects "All 5 user messages appear in order. Responses stream without interleaving or corruption." This tests a backend queuing behavior (handling multiple concurrent requests) that is not specified in any FR, BDD scenario, or TDD test. If the backend does not queue, responses will interleave.
- **Recommendation**: Either add a BDD scenario and FR for message queuing/ordering, or remove this evaluation scenario.

---

#### [MIN-008] Light mode not specified anywhere

- **Lens**: Incompleteness
- **Affected section**: BRD Appendix C, Section C.2.0
- **Description**: The BRD states "Light mode is a secondary consideration — invert primary/secondary while maintaining accent and semantic colors." Settings Section C.6.5.1 lists "theme (light/dark/system)" as a preference. But no spec defines the light mode color palette, nor does any Wave include light mode implementation. There are no light mode tokens, no light mode screenshots, and no light mode BDD scenarios.
- **Recommendation**: Either explicitly defer light mode ("Wave N — not in scope for MVP") or define the light mode palette. The current state is ambiguous — it is listed as a setting but has no implementation spec.

---

### Observations

#### [OBS-001] All nine specs use plan-spec format consistently

- **Lens**: N/A
- **Suggestion**: The structural consistency across specs is strong. All have user stories, acceptance scenarios, BDD scenarios, TDD plans, FRs, and traceability matrices. This is good engineering practice.

---

#### [OBS-002] Agent task management spec is the newest and least reviewed

- **Lens**: N/A
- **Affected section**: `agent-task-management-spec.md`
- **Suggestion**: This spec's status is "Draft (pending grill)" — it has not been through `/grill-spec` yet. It should be grilled before implementation, especially given MAJ-002's conflict with Wave 5a.

---

#### [OBS-003] Wave ordering may benefit from a dependency diagram

- **Lens**: Overcomplexity
- **Affected section**: All specs
- **Suggestion**: With 9 specs and cross-cutting dependencies (e.g., Wave 5a depends on Wave 0 theme + Wave 1 backend + Wave 4 channel UI), a visual dependency graph would help the team understand implementation order and identify parallel workstreams.

---

#### [OBS-004] Phosphor Icons override of shadcn/ui default Lucide icons needs explicit documentation

- **Lens**: Ambiguity
- **Affected section**: BRD Appendix C, Section C.3.1
- **Suggestion**: shadcn/ui ships with Lucide icons by default. The BRD mandates Phosphor Icons. Wave 0 should explicitly document how to prevent Lucide from being installed when adding new shadcn components (since `npx shadcn-ui@latest add` may bring in Lucide dependencies). The current codebase likely has both icon libraries in `package.json`.

---

#### [OBS-005] AssistantUI integration with custom tool components may have compatibility constraints

- **Lens**: Infeasibility
- **Affected section**: Wave 5a, Ambiguity #5
- **Suggestion**: AssistantUI has its own tool call rendering system. Wave 5a specifies a custom `toolComponents` registry with 14 custom components. The spec assumes these can be plugged into AssistantUI's rendering pipeline, but AssistantUI's extension points for custom tool renderers should be verified against the current AssistantUI version before implementation begins.

---

## Structural Integrity

All 9 specs follow the plan-spec format. Structural integrity checks are per-spec concerns and have been addressed in individual grill-spec reviews. This cross-spec review focuses on inter-spec consistency, which is where the findings above emerge.

---

## Test Coverage Assessment

### Missing Test Categories

| Category | Gap Description | Affected Specs |
|----------|----------------|---------------|
| Cross-spec integration | No tests verify that Wave 5a's UI correctly handles events defined in Agent Reliability (timeouts) or Agent Task Management (task status) | Wave 5a, Agent Reliability, Agent Task Management |
| WebSocket protocol conformance | No contract tests ensure frontend and backend agree on frame schemas | Wave 5a, all backend specs |
| Multi-wave regression | No tests verify that Wave 5a's implementation doesn't break Wave 0's theme or existing shadcn components | Wave 0, Wave 5a |

---

## STRIDE Threat Summary

Not applicable — this is a cross-spec consistency review, not a single-feature security analysis. Individual spec grill reviews cover STRIDE per feature.

---

## Unasked Questions

1. **What is the task data model?** Wave 5a assumes GTD tasks. Agent Task Management assumes an agent work queue. What JSON schema does `~/.omnipus/tasks/<id>.json` actually use? The two specs imply different schemas.

2. **When does the `@omnipus/ui` package get created?** CLAUDE.md and the BRD reference it, but all specs implement directly in `src/`. At what point does the code move into a shared package? No spec covers this.

3. **What happens when Wave 5a's frontend receives an event type defined by a spec not yet implemented?** For example, if Agent Reliability ships after Wave 5a, the frontend will receive `timeout` events it has never seen. The spec says "unknown types are ignored" (test dataset #6), but timeouts should NOT be silently ignored.

4. **Is there a maximum number of shadcn/ui components that can be installed before bundle size becomes a concern?** Currently 22 components. Each spec adds more UI. shadcn/ui is tree-shakeable, but Radix primitives add weight.

5. **Who maintains the WebSocket protocol contract?** As specs add event types, someone needs to own the protocol documentation. Currently, it is spread across 4+ specs.

---

## Verdict Rationale

The spec suite is well-structured and internally consistent within individual specs. The problems are at the seams between specs: conflicting task models (MAJ-002), missing protocol contract (MAJ-003), unspecified UI for backend events (MAJ-004), and a Wave 5a backend scope ambiguity (MAJ-001). These are coordination issues, not fundamental design flaws.

The shadcn/ui question has a clear, unambiguous answer: it is mandatory, P0, and already implemented. No action needed.

### Recommended Next Actions

- [ ] Resolve the task model conflict between Wave 5a and Agent Task Management (MAJ-002) — decide whether to build GTD board or agent work queue
- [ ] Create `docs/protocol/websocket-protocol.md` as the single protocol contract (MAJ-003)
- [ ] Add system-initiated interruption events to Wave 5a (MAJ-004)
- [ ] Clarify Wave 5a backend scope: add backend FRs or create a separate spec (MAJ-001)
- [ ] Align breakpoints to BRD values (MIN-001)
- [ ] Run `/grill-spec` on `agent-task-management-spec.md` before implementation (OBS-002)
- [ ] Verify AssistantUI custom tool component extension points (OBS-005)

---

Verdict: **REVISE**

Address the 4 MAJOR findings above, then re-run:
  `/grill-spec docs/plan/full-spec-suite-review.md`

For individual spec revisions, run:
  `/plan-spec --revise <spec-path> <this-review-path>`
