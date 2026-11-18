import { expect, test } from '@playwright/test';
import { expectInbox, expectSignInScreen, fromIP } from '../../helpers';
import { changerState, restartState } from './state';

test.use(fromIP('203.0.113.50'));

test.describe('after a restart', () => {
  test.use({ storageState: restartState });

  test('criterion 7: still signed in after a restart', async ({ page }) => {
    await page.goto('/');
    await expectInbox(page);
    await expect(page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').first()).toBeVisible();
  });
});

test.describe('after the password changed elsewhere', () => {
  test.use({ storageState: changerState });

  test('the next action signs out with "Your password has changed. Sign in again."', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('status')).toHaveText('Your password has changed. Sign in again.');
    await page.reload();
    await expectSignInScreen(page);
  });
});
