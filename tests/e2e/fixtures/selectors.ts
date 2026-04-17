import { type Page } from '@playwright/test';

/**
 * Chat composer input — AssistantUI renders ComposerPrimitive.Input as a
 * <textarea> with aria-label="Message input" (ChatScreen.tsx:631).
 * We target it by role=textbox scoped to the last match to avoid picking up
 * other text inputs on the page.
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
 * Agent picker dropdown trigger — the DropdownMenuTrigger Button in SessionBar
 * (SessionBar.tsx:90-116). It has no fixed aria-label but carries
 * aria-haspopup="menu" from Radix DropdownMenuTrigger.
 * Scoped to the session bar area (first occurrence in the header).
 */
export const agentPicker = (page: Page) =>
  page.locator('button[aria-haspopup="menu"]').first();

/**
 * Assistant messages — AssistantUI renders each message inside
 * MessagePrimitive.Root which produces a <div> with data-message-role set by
 * the primitive. The role for assistant messages is "assistant".
 */
export const assistantMessages = (page: Page) =>
  page.locator('[data-message-role="assistant"]');

/**
 * Nav link helper — returns the anchor inside the main navigation sidebar
 * for a given href (e.g. "/" | "/agents" | "/command-center" etc.).
 */
export const navLink = (page: Page, href: string) =>
  page.locator(`nav[aria-label="Main navigation"] a[href="${href}"]`);

/**
 * Agent cards on the roster page — AgentCard renders a <button> with
 * aria-label="View agent {name}" (AgentCard.tsx:29).
 */
export const agentCards = (page: Page) =>
  page.locator('button[aria-label^="View agent"]');

/**
 * New-chat button — rendered in SessionBar with title="New chat" (SessionBar.tsx:157-162).
 * Two variants exist (mobile icon-only + desktop text+icon); we target the
 * desktop one which also carries a visible label.
 */
export const newChatButton = (page: Page) =>
  page.locator('button[title="New chat"]').first();
