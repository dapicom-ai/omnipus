import { test as base, expect } from '@playwright/test';

const CONSOLE_ERROR_ALLOWLIST: RegExp[] = [
  /WebSocket.*reconnect/i,
  /hydration/i,
  /HMR/i,
  /manifest\.json.*404/i,
];

export const test = base.extend<{ consoleErrors: string[] }>({
  consoleErrors: async ({ page }, use) => {
    const errors: string[] = [];
    page.on('console', (msg) => {
      if (msg.type() !== 'error') return;
      const text = msg.text();
      if (CONSOLE_ERROR_ALLOWLIST.some((re) => re.test(text))) return;
      errors.push(text);
    });
    await use(errors);
    expect(errors, 'unexpected console errors').toEqual([]);
  },
});

export { expect } from '@playwright/test';
