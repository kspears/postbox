import { expect, test } from '@playwright/test';
import { expectInbox, expectSignInScreen, fromIP, subjectsIn } from '../../helpers';
import { changerState, restartState, senderState } from './state';

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

test.describe('after a restart during an undo window', () => {
  test.use({ storageState: senderState });

  test('criterion 24: the message stayed in Drafts, undelivered, and the page says so', async ({ page }) => {
    await page.goto('/');
    await expectInbox(page);
    await expect(page.getByRole('alert').filter({ hasText: 'A message was not sent and is in Drafts.' })).toBeVisible();
    // Long past the window: nothing may arrive late either.
    await page.waitForTimeout(11_000);
    expect(await subjectsIn('bob@example.test', 'INBOX')).not.toContain('Interrupted by a restart');
    expect(await subjectsIn('writer@example.test', 'Drafts')).toContain('Interrupted by a restart');
  });
});
