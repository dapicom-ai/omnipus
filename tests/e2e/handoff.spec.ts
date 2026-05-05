import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

// ARCHITECTURE NOTE: The sprint-h-subagent-block-spec.md (TDD row 20) calls for using a
// "scenario-provider path" for determinism. The Go-level scenario provider (pkg/agent/testutil)
// is only injectable into the gateway via the test_harness build tag — it is NOT available as
// an HTTP endpoint when running a live Playwright-targeted gateway. These tests therefore use
// a real LLM (requires OPENROUTER_API_KEY_CI) and prompts that strongly suggest spawning.
// Traces to: sprint-h-subagent-block-spec.md line 380 (TDD row 20, BDD Scenarios 1, 4)

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test.skip(
  '(a) Ray→Max→Jim chain: transcript shows all three agent labels',
  // blocked on #111: AssistantMessage does not annotate messages with per-agent attribution
  // in the DOM. No data-testid="messages-list" and no per-message agent label element.
  // A deterministic handoff also requires a mock tool trigger, not a real LLM call.
  // See tests/e2e/SPA-GAPS.md — "Agent handoff transcript labels not surfaced in DOM".
  // ALLOW-LISTED: { issue: "https://github.com/dapicom-ai/omnipus/issues/111", until: "2026-07-01" }
  async ({ page }) => {},
);

// BDD Scenario 1 (sprint-h-subagent-block-spec.md line 207):
//   Given the chat view is mounted on a live session
//   And the assistant issues a spawn tool call with label="audit go files"
//   When the backend fires EventKindSubTurnSpawn
//   Then [data-testid="subagent-collapsed"] appears
//   And clicking it reveals [data-testid="subagent-expanded"]
//   And the expanded region contains ≥1 [data-testid="tool-call-badge"] (FR-H-008)
//
// BDD Scenario 4 (line 241):
//   Given a collapsed SubagentBlock with 2 nested tool calls
//   When the user clicks the collapsed header
//   Then [data-testid="subagent-expanded"] is rendered
//   And the expanded region contains tool-call-badge elements
//
// Traces to: sprint-h-subagent-block-spec.md TDD row 20, SC-H-001
test(
  '(b) collapsed subagent display: spawn output renders as collapsed block, expandable',
  async ({ page }) => {
    // T0.1: OPENROUTER_API_KEY_CI soft-skip removed. The key is required in CI;
    // its absence is a CI configuration failure, not a per-test skip condition.
    // If OPENROUTER_API_KEY_CI is unset, the LLM call below will fail and the
    // test will fail honestly — which is the correct behavior.

    const input = chatInput(page);
    await expect(input).toBeVisible({ timeout: 15_000 });

    // Deterministic prompt: explicit tool name, exact arguments, no prose allowed.
    // temperature=0 + seed=42 are plumbed into OpenRouter requests for determinism.
    await input.fill(
      [
        'Call the `spawn` tool exactly once, right now, with these arguments:',
        '  label: "handoff-b test"',
        '  task: "You are the subagent. Call the `shell` tool ONCE with cmd=\\"echo hello\\". Then reply with the single word \\"done\\". Do not use any other tool."',
        'Do not reply in prose. Do not call any other tool. Call spawn now.',
      ].join('\n'),
    );
    await input.press('Enter');

    // Wait up to 30s for a subagent-collapsed block to appear.
    // Structural assertion: if no spawn occurred the test fails honestly.
    const collapsedBlock = page.locator('[data-testid="subagent-collapsed"]');
    await expect(collapsedBlock).toBeVisible({ timeout: 30_000 });

    // Assert: at least one collapsed block is present with correct structure.
    const blockCount = await collapsedBlock.count();
    expect(blockCount).toBeGreaterThanOrEqual(1, 'at least one SubagentBlock must be rendered');

    // BDD Scenario 4: click the collapsed header → expanded region appears.
    await collapsedBlock.first().click();

    const expandedBlock = page.locator('[data-testid="subagent-expanded"]');
    await expect(expandedBlock).toBeVisible({ timeout: 10_000 });

    // Assert: expanded block has at least one tool-call-badge (the subagent called shell).
    // Structural assertion: checks [data-testid="tool-call-badge"] presence.
    const toolCallBadges = expandedBlock.locator('[data-testid="tool-call-badge"]');
    await expect(toolCallBadges.first()).toBeVisible({ timeout: 10_000 });

    // a11y baseline check on subagent elements (BDD Scenario 11, FR-H-008).
    // Traces to: sprint-h-subagent-block-spec.md line 316 (Scenario 11)
    await expectA11yClean(page, {
      include: ['[data-testid^="subagent-"]'],
    });
  },
);

// (c) 6th-handoff refusal — DELETED.
// The concept no longer exists in Omnipus. Per owner decision (2026-04-20, documented
// in Sprint H / Plan 3 §1 reversal), handoffs are 1-level only. There is no "chain"
// or "depth limit" to refuse — the second-handoff refusal invariant is tested
// deterministically at the Go tool layer:
//   - pkg/gateway/handoff_summary_test.go :: TestHandoff_RejectsSecondHandoffInSession
//   - pkg/tools/handoff_test.go :: TestHandoffTool_RejectsSecondHandoff
// A Playwright placeholder for a deleted concept is dead code; removed.
