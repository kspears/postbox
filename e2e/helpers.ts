import { expect, type APIRequestContext, type Page } from '@playwright/test';

export const alice = 'alice@example.test';
export const bob = 'bob@example.test';

export function seedPassword(): string {
  const password = process.env.SEED_PASSWORD;
  if (!password) throw new Error('SEED_PASSWORD is not set; run the tests through deploy/ci/test.sh');
  return password;
}

// Postbox trusts X-Test-Client-IP in the test stack. Each test that signs in picks its own
// address from 203.0.113.0/24, so one test's failed attempts never throttle another's.
export function fromIP(ip: string): { extraHTTPHeaders: Record<string, string> } {
  return { extraHTTPHeaders: { 'X-Test-Client-IP': ip } };
}

export async function signIn(page: Page, email: string, password: string, keep = false): Promise<void> {
  await page.goto('/');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  if (keep) await page.getByLabel('Keep me signed in').check();
  await page.getByRole('button', { name: 'Sign in' }).click();
}

export async function expectInbox(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible();
}

export async function expectSignInScreen(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
}

// A failed sign-in straight through the API, for tests that need many of them quickly. The
// browser would send Origin itself; a bare API request has to say it.
export async function failSignIn(request: APIRequestContext, baseURL: string, email: string): Promise<void> {
  const response = await request.post('/api/session', {
    headers: { Origin: baseURL },
    data: { email, password: 'not-the-password', keep: false },
  });
  expect(response.status(), `failed sign-in for ${email}`).toBe(401);
}

export async function advanceClock(request: APIRequestContext, baseURL: string, seconds: number): Promise<void> {
  const response = await request.post('/api/test/clock', { headers: { Origin: baseURL }, data: { seconds } });
  expect(response.status()).toBe(204);
}
