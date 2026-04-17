import { expect } from '@playwright/test';
import { test } from './fixtures/console-errors';
import { expectA11yClean } from './fixtures/a11y';
import { agentCards } from './fixtures/selectors';

// Global storageState provides pre-authenticated session (see playwright.config.ts + global-setup.ts).

test.beforeEach(async ({ page }) => {
  // HashRouter: routes live in the fragment, not the pathname.
  await page.goto('/#/agents');
});

test('(a) roster loads with 5 core agents (Mia/Jim/Ava/Ray/Max) plus any custom', async ({
  page,
}) => {
  await expect(page).toHaveURL(/agents/, { timeout: 10_000 });

  // Verify each core agent name appears in the page body
  for (const name of ['Mia', 'Jim', 'Ava', 'Ray', 'Max']) {
    await expect(page.locator('body')).toContainText(new RegExp(name, 'i'), { timeout: 15_000 });
  }

  // AgentCard renders button[aria-label="View agent {name}"] (AgentCard.tsx:29)
  await expect(agentCards(page).first()).toBeVisible({ timeout: 10_000 });
  expect(await agentCards(page).count()).toBeGreaterThanOrEqual(5);

  await expectA11yClean(page);
});

test('(b) profile accordion expands all available sections', async ({ page }) => {
  // Click the first agent card to navigate to its profile
  const firstCard = agentCards(page).first();
  await expect(firstCard).toBeVisible({ timeout: 10_000 });
  await firstCard.click();

  await expect(page).toHaveURL(/\/agents\//, { timeout: 10_000 });

  // Accordion is a Radix Accordion — items produce [data-state="closed"|"open"] (AgentProfile.tsx:347)
  // Each AccordionTrigger is a button. Find all accordion triggers and click them.
  const accordionTriggers = page.locator('[data-radix-accordion-trigger]');
  const triggerCount = await accordionTriggers.count();

  if (triggerCount > 0) {
    for (let i = 0; i < triggerCount; i++) {
      await accordionTriggers.nth(i).click();
    }
    // At least one item should be open
    const openItems = page.locator('[data-state="open"]');
    await expect(openItems.first()).toBeVisible({ timeout: 10_000 });
  } else {
    // Fallback: Radix accordion triggers without the data attribute — use role
    const triggers = page.locator('[role="button"][aria-expanded]');
    const count = await triggers.count();
    if (count > 0) {
      await triggers.first().click();
      await expect(page.locator('[data-state="open"]').first()).toBeVisible({ timeout: 5_000 });
    }
  }
});

test('(c) "New Agent" button on roster opens the create-agent modal', async ({ page }) => {
  // Button text is "New Agent" (agents.index.tsx:29)
  const createBtn = page.getByRole('button', { name: 'New Agent' });
  await expect(createBtn).toBeVisible({ timeout: 10_000 });
  await createBtn.click();

  // CreateAgentModal renders a Radix Dialog — [role="dialog"]
  const modal = page.locator('[role="dialog"]');
  await expect(modal).toBeVisible({ timeout: 10_000 });
});

test.fixme(
  '(d) locked fields render read-only on core agents',
  async ({ page }) => {
    // AgentProfile renders a "read-only" badge for locked agents but does NOT render
    // data-testid="agent-name-input" or make the identity inputs accessible without
    // navigating into the accordion. The identity accordion section is hidden for
    // locked (core) agents (canEdit check in AgentProfile.tsx:353).
    // No stable selector exists to assert readOnly/disabled on the name field for
    // locked agents. See tests/e2e/SPA-GAPS.md — "Core-agent locked-field indicator".
  },
);

test.fixme(
  '(e) deleted agent URL returns branded 404 with "Back to Agents" link',
  async ({ page }) => {
    // Navigating to /agents/nonexistent-slug renders a generic error state or the
    // router's 404 page — neither includes a "Back to Agents" link with that exact text.
    // See tests/e2e/SPA-GAPS.md — "Deleted-agent branded 404 not implemented".
  },
);

test('(f) name collision on Create Agent surfaces server 409 error in UI', async ({ page }) => {
  // Open the create-agent modal
  const createBtn = page.getByRole('button', { name: 'New Agent' });
  await expect(createBtn).toBeVisible({ timeout: 10_000 });
  await createBtn.click();

  const modal = page.locator('[role="dialog"]');
  await expect(modal).toBeVisible({ timeout: 10_000 });

  // Find the name input within the modal
  // pressSequentially() required — fill() doesn't fire React onChange on controlled inputs
  const nameInput = modal.locator('input').first();
  await expect(nameInput).toBeVisible({ timeout: 10_000 });
  await nameInput.pressSequentially('Mia');

  // Intercept the POST to return 409
  await page.route('**/api/v1/agents**', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 409,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'agent name already exists' }),
      });
    } else {
      await route.continue();
    }
  });

  // Submit — look for a Create/Save button in the modal
  const submitBtn = modal.getByRole('button', { name: /create|save/i }).first();
  await expect(submitBtn).toBeVisible({ timeout: 5_000 });
  await submitBtn.click();

  // Error appears as a toast (ToastContainer in AppShell — no role="alert").
  // The api.ts request() helper throws new Error(`${status}: ${body}`) so the message is
  // "409: {\"error\": \"agent name already exists\"}". Match on the 409 status prefix.
  const errorToast = page.locator('text=409').first();
  await expect(errorToast).toBeVisible({ timeout: 10_000 });
});

test.fixme(
  '(g) session with deleted agent shows read-only transcript and "Agent removed" banner',
  async ({ page }) => {
    // The SPA does not render an "Agent removed" banner on sessions where agent_removed=true.
    // The chat screen does not handle this state — there is no data-testid="agent-removed-banner".
    // See tests/e2e/SPA-GAPS.md — "Agent-removed banner not implemented".
  },
);

test.afterAll(async ({ request }) => {
  // Clean up any PennyTest agents created by test (c) across all runs
  const resp = await request.get('/api/v1/agents');
  if (!resp.ok()) return;
  const data = (await resp.json()) as { id: string; name: string }[];
  for (const agent of data) {
    if (/^PennyTest/i.test(agent.name)) {
      await request.delete(`/api/v1/agents/${agent.id}`);
    }
  }
});
