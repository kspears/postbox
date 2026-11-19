import { expect, test, type Page } from '@playwright/test';
import { addAuthenticator, addPasskey, expectInbox, expectSignInScreen, fromIP, seedPassword, setMailPassword, signIn } from '../helpers';

// Milestone 9: passkeys, with Chromium's virtual authenticator as the passkey provider.
test.use(fromIP('203.0.113.150'));

async function signOut(page: Page): Promise<void> {
  await page.getByRole('button', { name: /^Account:/ }).click();
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
  await expectSignInScreen(page);
}

async function closeSettings(page: Page): Promise<void> {
  await page.getByRole('dialog', { name: 'Settings' }).getByRole('button', { name: 'Close' }).click();
}

test('criterion 3: a passkey added in Settings signs in without a password', async ({ page }) => {
  await addAuthenticator(page);
  await signIn(page, 'keyholder@example.test', seedPassword());
  await expectInbox(page);
  await addPasskey(page);
  await closeSettings(page);
  await signOut(page);

  await page.getByRole('button', { name: 'Sign in with a passkey' }).click();
  await expectInbox(page);
  await expect(page.getByRole('button', { name: 'Account: keyholder@example.test' })).toBeVisible();
  // deploy/ci/test.sh then searches postbox.db for the password, with this session open.
});

// As 1Password and Bitwarden do: the password is locked with POSTBOX_PASSKEY_KEY instead.
// tests/plain/passkey.spec.ts covers a Postbox without that key.
test('a passkey without PRF signs in through the server key', async ({ page }) => {
  await addAuthenticator(page, false);
  await signIn(page, 'plainkey@example.test', seedPassword());
  await expectInbox(page);
  await addPasskey(page);
  await closeSettings(page);
  await signOut(page);

  await page.getByRole('button', { name: 'Sign in with a passkey' }).click();
  await expectInbox(page);
});

test('removing the last passkey turns "Require passkey" off', async ({ page }) => {
  await addAuthenticator(page);
  await signIn(page, 'remover@example.test', seedPassword());
  await expectInbox(page);
  const settings = await addPasskey(page);
  const require = settings.getByRole('switch', { name: 'Require passkey' });
  await expect(settings.getByText('This protects Postbox only. Your password still works in other mail apps.')).toBeVisible();
  await require.check();
  await expect(require).toBeChecked();
  await settings.getByRole('button', { name: /^Remove the passkey added / }).click();
  await expect(require).not.toBeChecked();
  await expect(require).toBeDisabled();
  await closeSettings(page);
  await signOut(page);
  await signIn(page, 'remover@example.test', seedPassword());
  await expectInbox(page);
});

test('criterion 6: a passkey asks once for a password changed on the mail server', async ({ page }) => {
  const account = 'rotator@example.test';
  await addAuthenticator(page);
  await signIn(page, account, seedPassword());
  await expectInbox(page);
  const settings = await addPasskey(page);
  await settings.getByRole('switch', { name: 'Require passkey' }).check();
  await closeSettings(page);
  await signOut(page);

  const changed = `rotated-${Date.now()}`;
  await setMailPassword(account, changed);
  const passkey = page.getByRole('button', { name: 'Sign in with a passkey' });
  await passkey.click();
  await expect(page.getByRole('status')).toHaveText('Your password has changed. Enter your new password.');
  await expect(page.getByLabel('Email')).toHaveCount(0);

  await page.getByLabel('Password').fill('not-it');
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('Wrong email or password.');
  await page.getByLabel('Password').fill(changed);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expectInbox(page);

  await signOut(page);
  await passkey.click();
  await expectInbox(page);
});
