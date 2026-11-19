import { expect, test } from '@playwright/test';
import { expectInbox, fromIP, seedPassword, signIn, writeMessage } from '../../helpers';

// Criterion 20, against postbox-plain: its dovecot marks no special-use folders and the account
// has "Sent Items". The copy must go there, and no folder named "Sent" may appear.
test.use(fromIP('203.0.113.130'));

test('criterion 20: the Sent copy goes to "Sent Items"', async ({ page }) => {
  await signIn(page, 'writer@example.test', seedPassword());
  await expectInbox(page);
  const subject = `Plain server ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'Sent through a server without special-use folders.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });

  const nav = page.getByRole('navigation', { name: 'Folders' });
  await page.reload();
  await expect(nav.getByRole('button', { name: /^Sent Items/ })).toBeVisible();
  await expect(nav.getByRole('button', { name: 'Sent', exact: true })).toHaveCount(0);
  await nav.getByRole('button', { name: /^Sent Items/ }).click();
  await expect(page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject })).toHaveCount(1);
});
