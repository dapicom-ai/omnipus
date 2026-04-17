import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test.fixme(
  '(a) Ray→Max→Jim chain: transcript shows all three agent labels',
  async ({ page }) => {
    // Agent handoff is driven via LLM tool calls. The chat transcript does not
    // surface agent labels per-message in the DOM (no data-testid="messages-list"
    // and no per-message agent attribution element exists in AssistantMessage).
    // A working real-LLM test would be flaky without a deterministic handoff.
    // See tests/e2e/SPA-GAPS.md — "Agent handoff transcript labels not surfaced in DOM".
  },
);

test.fixme(
  '(b) collapsed subagent display: spawn output renders as collapsed block, expandable',
  async ({ page }) => {
    // The SPA does not render a data-testid="subagent-collapsed" block.
    // Subagent output arrives as assistant message text — there is no dedicated
    // collapsed/expandable subagent UI component.
    // See tests/e2e/SPA-GAPS.md — "Subagent collapsed block UI not implemented".
  },
);

test.fixme(
  '(c) 6th-handoff refusal: chain of 5 handoffs triggers policy error on 6th',
  async ({ page, request }) => {
    // Driving 5 real LLM handoffs in a single test is too slow and unreliable for CI.
    // The policy error is surfaced via RateLimitIndicator in the chat composer area
    // but there is no stable way to drive 5 consecutive handoffs deterministically.
    // See tests/e2e/SPA-GAPS.md — "Handoff depth policy test requires deterministic LLM".
  },
);
