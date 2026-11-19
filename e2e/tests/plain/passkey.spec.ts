import { expect, test } from '@playwright/test';
import { addAuthenticator, expectInbox, fromIP, openSettings, seedPassword, signIn } from '../../helpers';

// Criterion 3, against postbox-plain, which has no POSTBOX_PASSKEY_KEY: a passkey without PRF
// cannot lock the password, so it is refused and nothing is listed.
test.use(fromIP('203.0.113.131'));

test('criterion 3: without a server key, a passkey without PRF is refused', async ({ page }) => {
  await addAuthenticator(page, false);
  await signIn(page, 'plainkey@example.test', seedPassword());
  await expectInbox(page);
  const settings = await openSettings(page);
  await settings.getByRole('button', { name: 'Add a passkey' }).click();
  await expect(settings.getByRole('alert')).toHaveText("This passkey can't be used with Postbox. Try a different passkey provider.");
  await expect(settings.getByRole('listitem').filter({ hasText: /^Added / })).toHaveCount(0);
  await expect(settings.getByRole('switch', { name: 'Require passkey' })).toBeDisabled();
});
