import { expect, test, type BrowserContext } from '@playwright/test';
import {
  addAuthenticator,
  addPasskey,
  advanceClock,
  alice,
  expectInbox,
  expectSignInScreen,
  fromIP,
  openSettings,
  seedPassword,
  signIn,
} from '../../helpers';

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

test('a passkey session lasts 30 days', async ({ page, baseURL }) => {
  await addAuthenticator(page);
  await signIn(page, 'lasting@example.test', seedPassword());
  await expectInbox(page);
  await addPasskey(page);
  await page.context().clearCookies();
  await page.goto('/');
  await page.getByRole('button', { name: 'Sign in with a passkey' }).click();
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

test('ten minutes after sign-in, changing passkeys asks for the password again', async ({ page, baseURL }) => {
  await addAuthenticator(page);
  await signIn(page, 'confirmer@example.test', seedPassword());
  await expectInbox(page);
  await advanceClock(page.request, baseURL!, 11 * 60);

  const settings = await openSettings(page);
  await settings.getByRole('button', { name: 'Add a passkey' }).click();
  const prompt = settings.getByRole('form', { name: 'Enter your password to change passkeys.' });
  await expect(prompt).toBeVisible();
  await prompt.getByLabel('Password').fill('not-it');
  await prompt.getByRole('button', { name: 'Continue' }).click();
  await expect(prompt.getByRole('alert')).toHaveText('Wrong password.');

  await prompt.getByLabel('Password').fill(seedPassword());
  await prompt.getByRole('button', { name: 'Continue' }).click();
  await expect(prompt).toBeHidden();
  // The waiting change goes ahead; the virtual authenticator may want the second prompt.
  const listed = settings.getByRole('listitem').filter({ hasText: /^Added / });
  const finish = settings.getByRole('button', { name: 'Finish' });
  await expect(listed.or(finish)).toBeVisible();
  if (await finish.isVisible()) await finish.click();
  await expect(listed).toHaveCount(1);
});
