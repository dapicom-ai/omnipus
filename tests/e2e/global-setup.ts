import { chromium } from '@playwright/test';
import path from 'path';
import { fileURLToPath } from 'url';
import { loginAs } from './fixtures/login';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const AUTH_FILE = path.join(__dirname, 'fixtures/.auth/admin.json');

async function globalSetup(): Promise<void> {
  const browser = await chromium.launch();
  const context = await browser.newContext({
    baseURL: process.env.OMNIPUS_URL || 'http://localhost:6060',
  });
  const page = await context.newPage();

  await loginAs(page, 'admin', 'admin123');

  await context.storageState({ path: AUTH_FILE });
  await browser.close();
}

export default globalSetup;
