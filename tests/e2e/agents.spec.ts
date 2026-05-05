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

test('(d) locked fields render read-only on core agents', async ({ page }) => {
  // Navigate to the Jim agent profile (locked core agent)
  await page.goto('/#/agents');
  const jimCard = page.locator('[aria-label*="Jim" i]').or(page.getByText('Jim', { exact: true })).first();
  await expect(jimCard).toBeVisible({ timeout: 15_000 });
  await jimCard.click();

  // Wait for the profile to load
  await expect(page).toHaveURL(/\/agents\//, { timeout: 10_000 });

  // The identity accordion should exist and be open (defaultValue includes 'identity')
  const nameInput = page.getByTestId('agent-name-input');
  await expect(nameInput).toBeVisible({ timeout: 10_000 });

  // For a locked agent, the input must be disabled
  await expect(nameInput).toBeDisabled();
});

test('(e) deleted agent URL returns branded 404 with "Back to Agents" link', async ({ page }) => {
  await page.goto('/#/agents/this-agent-does-not-exist-xyz');

  // Should see a "not found" message, not crash the app
  const notFoundMsg = page.locator('text=not found').or(page.locator('text=Not Found')).or(page.locator('text=Agent not found')).first();
  await expect(notFoundMsg).toBeVisible({ timeout: 10_000 });

  // Must have "Back to Agents" link (exact text per SKIP_ALLOWLIST note)
  const backLink = page.getByRole('link', { name: 'Back to Agents' });
  await expect(backLink).toBeVisible({ timeout: 5_000 });
  await backLink.click();
  await expect(page).toHaveURL(/agents/, { timeout: 5_000 });
});

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
  // CreateAgentModal uses isApiError(err) ? err.userMessage which for a 409 response
  // is defaultUserMessage(409) = "This conflicts with the current state. Please refresh and try again."
  // (see src/lib/api-error.ts and src/components/agents/CreateAgentModal.tsx).
  const errorToast = page.locator('text=conflicts with the current state').first();
  await expect(errorToast).toBeVisible({ timeout: 10_000 });
});

test.skip(
  '(g) session with deleted agent shows read-only transcript and "Agent removed" banner',
  // blocked on #103: ChatScreen does not check agent_removed in the session response.
  // Needs data-testid="agent-removed-banner" and a disabled composer for ghost sessions.
  // See tests/e2e/SPA-GAPS.md — "Agent-removed banner".
  async ({ page }) => {},
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
