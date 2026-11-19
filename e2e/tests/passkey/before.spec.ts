import { expect, test } from '@playwright/test';
import { addAuthenticator, addPasskey, expectInbox, expectSignInScreen, fromIP, seedPassword, signIn } from '../../helpers';

// Criterion 5, first half. deploy/ci/test.sh then runs `postbox passkeys clear` for the account
// and after.spec.ts signs in with the password.
test.use(fromIP('203.0.113.160'));

const account = 'guarded@example.test';

test('with "Require passkey" on, password sign-in is refused, and other password sessions end', async ({ page, browser }) => {
  const other = await browser.newContext(fromIP('203.0.113.161'));
  const elsewhere = await other.newPage();
  await signIn(elsewhere, account, seedPassword());
  await expectInbox(elsewhere);

  await addAuthenticator(page);
  await signIn(page, account, seedPassword());
  await expectInbox(page);
  const settings = await addPasskey(page);
  await settings.getByRole('switch', { name: 'Require passkey' }).check();
  await expect(settings.getByRole('switch', { name: 'Require passkey' })).toBeChecked();

  // The session that turned it on stays; the other password session is over.
  await elsewhere.reload();
  await expectSignInScreen(elsewhere);
  await page.reload();
  await expectInbox(page);

  await signIn(elsewhere, account, seedPassword());
  await expect(elsewhere.getByRole('alert')).toHaveText('This account requires a passkey.');
  await signIn(elsewhere, account, 'not-the-password');
  await expect(elsewhere.getByRole('alert')).toHaveText('Wrong email or password.');
  await other.close();
});
