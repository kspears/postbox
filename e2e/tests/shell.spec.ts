import { expect, test } from '@playwright/test';

test('the health check answers ok', async ({ request }) => {
  const response = await request.get('/healthz');
  expect(response.status()).toBe(200);
  expect(await response.text()).toBe('ok');
});

test('the app loads in a secure context', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Postbox' })).toBeVisible();
  // Passkeys and Secure cookies depend on this.
  expect(await page.evaluate(() => window.isSecureContext)).toBe(true);
});

test('an app route survives a reload', async ({ page }) => {
  await page.goto('/folder/INBOX');
  await expect(page.getByRole('heading', { name: 'Postbox' })).toBeVisible();
});
