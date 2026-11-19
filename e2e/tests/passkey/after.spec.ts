import { test } from '@playwright/test';
import { expectInbox, fromIP, seedPassword, signIn } from '../../helpers';

// Criterion 5, second half: after `postbox passkeys clear`, the password works again.
test.use(fromIP('203.0.113.162'));

test('after passkeys clear, password sign-in works', async ({ page }) => {
  await signIn(page, 'guarded@example.test', seedPassword());
  await expectInbox(page);
});
