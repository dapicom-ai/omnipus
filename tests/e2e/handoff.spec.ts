import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { chatInput, agentPicker, assistantMessages } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test.skip(
  '(a) Ray→Max→Jim chain: transcript shows all three agent labels',
  // blocked on #111: AssistantMessage does not annotate messages with per-agent attribution
  // in the DOM. No data-testid="messages-list" and no per-message agent label element.
  // A deterministic handoff also requires a mock tool trigger, not a real LLM call.
  // See tests/e2e/SPA-GAPS.md — "Agent handoff transcript labels not surfaced in DOM".
  async ({ page }) => {},
);

test.skip(
  '(b) collapsed subagent display: spawn output renders as collapsed block, expandable',
  // blocked on #112: No data-testid="subagent-collapsed" block exists in the SPA.
  // Subagent output arrives as plain assistant message text — no dedicated
  // collapsed/expandable subagent UI component has been implemented.
  // See tests/e2e/SPA-GAPS.md — "Subagent collapsed block UI not implemented".
  async ({ page }) => {},
);

test.skip(
  '(c) 6th-handoff refusal: chain of 5 handoffs triggers policy error on 6th',
  // blocked on #113: Driving 5 real LLM handoffs deterministically in CI is impractical
  // without a mock tool that auto-triggers handoffs on a signal. The policy error is
  // surfaced via RateLimitIndicator but there is no stable deterministic driver.
  // See tests/e2e/SPA-GAPS.md — "Handoff depth policy test requires deterministic LLM".
  async ({ page, request }) => {},
);
