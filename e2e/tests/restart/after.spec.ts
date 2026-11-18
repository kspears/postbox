import { test } from '@playwright/test';
import { expectInbox, fromIP } from '../../helpers';
import { restartState } from './state';

// Criterion 7, second half: the same browser is still signed in after Postbox restarted.
test.use({ ...fromIP('203.0.113.50'), storageState: restartState });

test('criterion 7: still signed in after a restart', async ({ page }) => {
  await page.goto('/');
  await expectInbox(page);
});
