import { type Page } from '@playwright/test';

/**
 * Chat composer input — AssistantUI renders ComposerPrimitive.Input as a
 * <textarea> with aria-label="Message input" (ChatScreen.tsx:631).
 */
export const chatInput = (page: Page) =>
  page.locator('textarea[aria-label="Message input"]');

/**
 * Send button — ComposerPrimitive.Send rendered with aria-label="Send message"
 * (ChatScreen.tsx:698). Only visible when not streaming.
 */
export const sendButton = (page: Page) =>
  page.locator('button[aria-label="Send message"]').first();

/**
 * Agent picker button — rendered in the <header> as a button whose text includes
 * the agent name followed by an em-dash (e.g. "Mia — Omnipus Guide").
 * Scoped to the banner landmark to avoid matching sidebar items.
 *
 * Ground truth: header structure confirmed via Playwright MCP live inspection.
 * The button carries the full "Name — Tagline" text — match via em-dash presence.
 * The <header> has implicit ARIA role "banner" — use getByRole to locate it.
 */
export const agentPicker = (page: Page) =>
  page.getByRole('banner').locator('button').filter({ hasText: '—' }).first();

/**
 * Assistant messages — AssistantUI renders each message inside
 * MessagePrimitive.Root which produces a <div> with data-message-role set by
 * the primitive. The role for assistant messages is "assistant".
 */
export const assistantMessages = (page: Page) =>
  page.locator('[data-message-role="assistant"]');

/**
 * Nav link helper — sidebar must be open before calling this.
 * Returns the anchor inside the nav for a given href.
 * The sidebar renders nav[aria-label="Main navigation"] ONLY while open.
 *
 * HashRouter: TanStack Router generates href="/#/<path>" links.
 * Call with the full hash-prefixed href, e.g. navLink(page, '/#/agents').
 */
export const navLink = (page: Page, href: string) =>
  page.locator(`nav[aria-label="Main navigation"] a[href="${href}"]`);

/**
 * Agent cards on the roster page — AgentCard renders a <button> with
 * aria-label="View agent {name}" (AgentCard.tsx:29).
 * Ground truth: "View agent Mia — Omnipus Guide" (em-dash, not regular dash).
 */
export const agentCards = (page: Page) =>
  page.locator('button[aria-label^="View agent "]');

/**
 * New-chat button — rendered in the header banner with accessible name "New Chat".
 * Ground truth confirmed via Playwright MCP: button "New Chat" (not title="New chat").
 */
export const newChatButton = (page: Page) =>
  page.getByRole('banner').getByRole('button', { name: 'New Chat' });
