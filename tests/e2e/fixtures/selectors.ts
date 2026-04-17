import { type Page } from '@playwright/test';

export const chatInput = (page: Page) => page.locator('[data-testid="chat-input"]');

export const agentPicker = (page: Page) => page.locator('[data-testid="agent-picker"]');

export const assistantMessages = (page: Page) =>
  page.locator('[data-testid^="assistant-msg"]');
