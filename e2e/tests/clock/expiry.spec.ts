import { expect, test, type BrowserContext } from '@playwright/test';
import { advanceClock, alice, expectInbox, expectSignInScreen, fromIP, seedPassword, signIn } from '../../helpers';

// Criterion 7: session lifetimes. These move Postbox's clock forward for good, so this project
// runs after every other.
test.use(fromIP('203.0.113.60'));

const day = 24 * 60 * 60;

async function sessionCookie(context: BrowserContext) {
  const cookie = (await context.cookies()).find((c) => c.name.endsWith('postbox_session'));
  expect(cookie, 'session cookie').toBeDefined();
  return cookie!;
}

test('a password session without "Keep me signed in" ends with the browser and after 24 hours', async ({ page, baseURL }) => {
  await signIn(page, alice, seedPassword());
  await expectInbox(page);
  expect((await sessionCookie(page.context())).expires, 'a browser-session cookie has no expiry').toBe(-1);

  await advanceClock(page.request, baseURL!, day - 60);
  await page.reload();
  await expectInbox(page);

  await advanceClock(page.request, baseURL!, 60);
  await page.reload();
  await expectSignInScreen(page);
});

// The passkey case lasts the same 30 days and is added with passkeys (criteria 3-6).
test('a "Keep me signed in" session lasts 30 days', async ({ page, baseURL }) => {
  await signIn(page, alice, seedPassword(), true);
  await expectInbox(page);
  const expires = (await sessionCookie(page.context())).expires;
  expect(expires * 1000 - Date.now()).toBeGreaterThan(29 * day * 1000);

  await advanceClock(page.request, baseURL!, 30 * day - 60);
  await page.reload();
  await expectInbox(page);

  await advanceClock(page.request, baseURL!, 60);
  await page.reload();
  await expectSignInScreen(page);
});
