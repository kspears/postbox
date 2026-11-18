import { test } from '@playwright/test';
import { alice, expectInbox, fromIP, seedPassword, signIn } from '../../helpers';
import { changerState, restartState } from './state';

// First halves. deploy/ci/test.sh then changes changer's password, restarts Postbox, and runs
// after.spec.ts.
test.use(fromIP('203.0.113.50'));

test('sign in before the restart', async ({ page }) => {
  await signIn(page, alice, seedPassword());
  await expectInbox(page);
  await page.context().storageState({ path: restartState });
});

test('sign in before the password changes', async ({ page }) => {
  await signIn(page, 'changer@example.test', seedPassword());
  await expectInbox(page);
  await page.context().storageState({ path: changerState });
});
