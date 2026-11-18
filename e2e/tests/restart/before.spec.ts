import { test } from '@playwright/test';
import { alice, expectInbox, fromIP, seedPassword, signIn } from '../../helpers';
import { restartState } from './state';

// Criterion 7, first half: sign in and keep the browser's state. deploy/ci/test.sh then
// restarts Postbox and runs after.spec.ts.
test.use(fromIP('203.0.113.50'));

test('sign in before the restart', async ({ page }) => {
  await signIn(page, alice, seedPassword());
  await expectInbox(page);
  await page.context().storageState({ path: restartState });
});
