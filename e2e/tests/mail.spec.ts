import { expect, test } from '@playwright/test';
import { alice, expectInbox, fromIP, seedPassword, signIn } from '../helpers';

test.use(fromIP('203.0.113.70'));

test.describe('folder list', () => {
  test('shows the special folders first, with unread counts', async ({ page }) => {
    await signIn(page, alice, seedPassword());
    await expectInbox(page);
    const nav = page.getByRole('navigation', { name: 'Folders' });
    // Each folder's own button; the "Actions for …" menu buttons are not folders.
    await expect(nav.getByRole('button', { name: /^(?!Actions for)/ })).toHaveText([/^Inbox/, 'Drafts', 'Sent', 'Archive', 'Junk', 'Trash', '+ New folder']);
    // alice has four unread messages from the seed.
    await expect(nav.getByRole('button', { name: /^Inbox/ }).getByLabel('4 unread')).toBeVisible();
  });

  test('opening a folder changes the address and survives a reload', async ({ page }) => {
    await signIn(page, alice, seedPassword());
    await expectInbox(page);
    await page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: 'Sent', exact: true }).click();
    await expect(page.getByRole('heading', { name: 'Sent' })).toBeVisible();
    await expect(page).toHaveURL(/\/folder\/Sent$/);
    await expect(page.getByText('No conversations here.')).toBeVisible();
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Sent' })).toBeVisible();
    await page.goBack();
    await expectInbox(page);
  });
});

test.describe('conversation list', () => {
  test('groups a thread, newest first, with decoded subjects and snippets', async ({ page }) => {
    await signIn(page, alice, seedPassword());
    await expectInbox(page);
    const rows = page.getByRole('region', { name: 'Conversations' }).getByRole('listitem');
    await expect(rows).toHaveCount(3);

    const fence = rows.nth(0);
    await expect(fence).toContainText('Fence repairs');
    await expect(fence.getByLabel('2 messages')).toBeVisible();
    await expect(fence).toContainText('please bring the blue folder');
    await expect(fence.getByRole('img', { name: 'Unread' })).toBeVisible();

    // Encoded-word subject, quoted-printable HTML-and-text body.
    await expect(rows.nth(1)).toContainText('This week: café planters and winter pruning');
    await expect(rows.nth(1)).toContainText('Café planters are back');
    await expect(rows.nth(2)).toContainText('Welcome to Postbox');
  });

  test('loads the next page on scrolling, without repeats', async ({ page }) => {
    await signIn(page, 'paging@example.test', seedPassword());
    await expectInbox(page);
    const rows = page.getByRole('region', { name: 'Conversations' }).getByRole('listitem');
    await expect(rows.first()).toContainText('Paging 60');
    await rows.nth(49).scrollIntoViewIfNeeded();
    await expect(rows).toHaveCount(60);
    await expect(rows.last()).toContainText('Paging 01');
    const subjects = await rows.allInnerTexts();
    expect(new Set(subjects).size).toBe(60);
  });
});
