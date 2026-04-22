/**
 * sprint-k-setup.ts — Shared gateway lifecycle helpers for Sprint K E2E tests
 *
 * Exports startGateway() / stopGateway() used by test.beforeAll / test.afterAll.
 *
 * Design:
 *   - Starts the Sprint K binary (OMNIPUS_BINARY env or /tmp/omnipus-sprint-k)
 *   - Creates a throwaway OMNIPUS_HOME with mkdtempSync (unique per call)
 *   - Waits for /health to return 200 before resolving
 *   - Onboards a first admin via POST /api/v1/onboarding/complete
 *   - Returns the admin credentials so callers can auth-seed their own sessions
 *
 * Traces to: CLAUDE.md "SPA Embed Pipeline" and E2E "Known blockers and workarounds"
 */

import { spawn, type ChildProcess } from 'child_process';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { fileURLToPath } from 'url';

// Default binary used when OMNIPUS_BINARY is not set.
export const DEFAULT_OMNIPUS_BINARY = '/tmp/omnipus-sprint-k';

export interface GatewayHandle {
  process: ChildProcess;
  homeDir: string;
  port: number;
  baseURL: string;
  adminUsername: string;
  adminPassword: string;
}

export interface StartGatewayOptions {
  port: number;
  binary?: string;
  adminUsername?: string;
  adminPassword?: string;
  /** Extra gateway args (e.g. '--sandbox=off') */
  extraArgs?: string[];
}

/**
 * Verify the embedded SPA contains Sprint K user-management strings.
 * Fails with a descriptive error if the sync pipeline was skipped.
 * Traces to: CLAUDE.md "SPA Embed Pipeline" — verification step.
 */
export function assertSprintKEmbedPresent(): void {
  const spaAssetsDir = path.join(
    path.dirname(fileURLToPath(import.meta.url)),
    '../../pkg/gateway/spa/assets',
  );
  let jsFiles: string[];
  try {
    jsFiles = fs.readdirSync(spaAssetsDir).filter((f) => f.endsWith('.js'));
  } catch {
    throw new Error(
      `BLOCKED: pkg/gateway/spa/assets/ not found.\n` +
        'Run the SPA sync pipeline:\n' +
        '  npm run build\n' +
        '  rm -rf pkg/gateway/spa/assets\n' +
        '  cp -r dist/spa/* pkg/gateway/spa/\n' +
        '  CGO_ENABLED=0 go build -o /tmp/omnipus-sprint-k ./cmd/omnipus/',
    );
  }
  const hasUserManagement = jsFiles.some((f) => {
    try {
      const content = fs.readFileSync(path.join(spaAssetsDir, f), 'utf-8');
      // "Add user" is the button label in UsersSection.tsx — proves Sprint K UI is embedded.
      return content.includes('Add user');
    } catch {
      return false;
    }
  });
  if (!hasUserManagement) {
    throw new Error(
      'BLOCKED: Sprint K frontend not embedded in pkg/gateway/spa/.\n' +
        'Run the sync pipeline:\n' +
        '  npm run build && rm -rf pkg/gateway/spa/assets && cp -r dist/spa/* pkg/gateway/spa/\n' +
        '  CGO_ENABLED=0 go build -o /tmp/omnipus-sprint-k ./cmd/omnipus/',
    );
  }
}

/**
 * Start a Sprint K gateway instance on the given port.
 * Returns a GatewayHandle with process info + onboarded admin credentials.
 *
 * Usage:
 *   let handle: GatewayHandle;
 *   test.beforeAll(async () => { handle = await startGateway({ port: 5551 }); });
 *   test.afterAll(async () => { await stopGateway(handle); });
 */
export async function startGateway(opts: StartGatewayOptions): Promise<GatewayHandle> {
  const binary = opts.binary ?? process.env.OMNIPUS_BINARY ?? DEFAULT_OMNIPUS_BINARY;
  const adminUsername = opts.adminUsername ?? 'admin';
  const adminPassword = opts.adminPassword ?? 'admin1234';

  // Verify binary exists.
  if (!fs.existsSync(binary)) {
    throw new Error(
      `BLOCKED: Gateway binary not found at ${binary}.\n` +
        'Build it with:\n' +
        '  CGO_ENABLED=0 go build -o /tmp/omnipus-sprint-k ./cmd/omnipus/',
    );
  }

  // Verify Sprint K SPA is embedded.
  assertSprintKEmbedPresent();

  // Create a throwaway OMNIPUS_HOME — unique per startGateway() call.
  const homeDir = fs.mkdtempSync(path.join(os.tmpdir(), `omnipus-k-${opts.port}-`));

  // Pre-write config.json with the port.
  fs.writeFileSync(
    path.join(homeDir, 'config.json'),
    JSON.stringify({ version: 1, gateway: { port: opts.port } }, null, 2),
    { mode: 0o600 },
  );

  const args = ['gateway', '--allow-empty', ...(opts.extraArgs ?? ['--sandbox=off'])];

  const proc = await new Promise<ChildProcess>((resolve, reject) => {
    const child = spawn(binary, args, {
      env: {
        ...process.env,
        OMNIPUS_HOME: homeDir,
        OMNIPUS_BEARER_TOKEN: '',
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    });

    const timeout = setTimeout(() => {
      child.kill();
      reject(new Error(`Gateway did not start within 30s on port ${opts.port}`));
    }, 30_000);

    let output = '';
    const onData = (chunk: Buffer) => {
      output += chunk.toString();
      // Match any "listening on :<PORT>" pattern the gateway logs.
      if (
        output.includes(`localhost:${opts.port}`) ||
        output.includes(`0.0.0.0:${opts.port}`) ||
        output.includes(`:${opts.port}`)
      ) {
        clearTimeout(timeout);
        resolve(child);
      }
    };
    child.stdout?.on('data', onData);
    child.stderr?.on('data', onData);

    child.on('exit', (code) => {
      clearTimeout(timeout);
      if (code !== null && code !== 0) {
        reject(new Error(`Gateway exited with code ${code}.\nOutput:\n${output}`));
      }
    });
    child.on('error', (err) => {
      clearTimeout(timeout);
      reject(err);
    });
  });

  const baseURL = `http://localhost:${opts.port}`;

  // Wait for /health to return 200.
  await waitForHealth(baseURL);

  // Onboard the first admin.
  await onboardAdmin(baseURL, adminUsername, adminPassword);

  return { process: proc, homeDir, port: opts.port, baseURL, adminUsername, adminPassword };
}

/**
 * Gracefully stop a gateway started by startGateway().
 * Always attempts cleanup of the temp OMNIPUS_HOME.
 */
export async function stopGateway(handle: GatewayHandle | null): Promise<void> {
  if (!handle) return;
  if (handle.process && !handle.process.killed) {
    handle.process.kill('SIGTERM');
  }
  if (handle.homeDir && fs.existsSync(handle.homeDir)) {
    try {
      fs.rmSync(handle.homeDir, { recursive: true, force: true });
    } catch {
      // Best-effort cleanup; workspace dirs created by the gateway may be non-empty.
    }
  }
}

/**
 * Poll /health until it returns 200 or the timeout expires.
 */
export async function waitForHealth(baseURL: string, timeoutMs = 15_000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${baseURL}/health`);
      if (res.ok) return;
    } catch {
      // Not ready yet — keep polling.
    }
    await new Promise((r) => setTimeout(r, 200));
  }
  throw new Error(`${baseURL}/health did not return 200 within ${timeoutMs}ms`);
}

/**
 * Seed an admin via POST /api/v1/onboarding/complete.
 * Idempotent: 409 is treated as success.
 * Traces to: pkg/gateway/rest_onboarding.go — endpoint is CSRF-exempt.
 */
export async function onboardAdmin(
  baseURL: string,
  username: string,
  password: string,
): Promise<void> {
  const res = await fetch(`${baseURL}/api/v1/onboarding/complete`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: {
        id: 'openrouter',
        api_key: process.env.OPENROUTER_API_KEY ?? 'sk-test-placeholder',
        model: 'openai/gpt-4o',
      },
      admin: { username, password },
    }),
  });

  if (!res.ok && res.status !== 409) {
    const body = await res.text();
    throw new Error(`onboardAdmin: onboarding failed ${res.status}: ${body}`);
  }
}

/**
 * Log in and return the bearer token.
 * Traces to: pkg/gateway/rest_auth.go HandleLogin.
 */
export async function loginAPI(
  baseURL: string,
  username: string,
  password: string,
): Promise<{ token: string; role: string }> {
  const res = await fetch(`${baseURL}/api/v1/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`loginAPI: login failed ${res.status}: ${body}`);
  }
  const data = (await res.json()) as { token: string; role: string };
  return data;
}
