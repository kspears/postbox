import { expect, test } from '@playwright/test';
import { expectInbox, fromIP, seedPassword, signIn, writeMessage } from '../../helpers';
import { senderState } from './state';

// Criterion 24, first half: send, then stop. deploy/ci/test.sh restarts Postbox at once, inside
// the undo window, and after.spec.ts checks the message stayed in Drafts.
test.use(fromIP('203.0.113.51'));

test('send just before a restart', async ({ page }) => {
  await signIn(page, 'writer@example.test', seedPassword());
  await expectInbox(page);
  const composer = await writeMessage(page, 'bob@example.test', 'Interrupted by a restart', 'Must not arrive.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toContainText('Sending…');
  await page.context().storageState({ path: senderState });
});
