import { expect, test, type Page } from '@playwright/test';
import { deliver, expectInbox, fileBySubject, fromIP, seedPassword, signIn, subjectsIn } from '../helpers';

// Milestone 8: search, and keyboard shortcuts.
test.use(fromIP('203.0.113.150'));

async function search(page: Page, text: string) {
  const box = page.getByRole('searchbox', { name: 'Search mail' });
  await box.fill(text);
  await box.press('Enter');
  return page.getByRole('region', { name: 'Search results' });
}

/** Delivers a message to account and waits for the server to have filed it in the Inbox. */
async function arrive(account: string, subject: string, body: string): Promise<void> {
  await deliver(account, subject, body);
  await expect.poll(() => subjectsIn(account, 'INBOX'), { timeout: 10_000 }).toContain(subject);
}

test('criterion 14: a body word is found in other folders from Inbox, and in Trash only from Trash', async ({ page }) => {
  const account = 'searcher@example.test';
  const stamp = Date.now();
  const word = `quokka${stamp}`;
  const archived = `Filed away ${stamp}`;
  const trashed = `Thrown away ${stamp}`;
  await arrive(account, archived, `The ${word} sat in the archive.`);
  await arrive(account, trashed, `The ${word} sat in the trash.`);
  await fileBySubject(account, archived, 'Archive');
  await fileBySubject(account, trashed, 'Trash');

  await signIn(page, account, seedPassword());
  await expectInbox(page);
  let results = await search(page, word);
  await expect(results.getByRole('listitem')).toHaveCount(1);
  const hit = results.getByRole('listitem').filter({ hasText: archived });
  await expect(hit).toContainText('Archive');
  await expect(results).not.toContainText(trashed);

  // Opening a result shows its conversation.
  await hit.getByRole('link').click();
  await expect(page.getByRole('region', { name: 'Reading pane' })).toContainText(archived);

  // From inside Trash, only Trash.
  await page.getByRole('button', { name: 'Close search' }).click();
  await page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: /^Trash/ }).click();
  results = await search(page, word);
  await expect(results.getByRole('listitem')).toHaveCount(1);
  await expect(results.getByRole('listitem')).toContainText(trashed);

  // A search that finds nothing says so.
  results = await search(page, 'nothing-matches-this');
  await expect(results).toContainText('No messages match.');
});

test('criterion 27: each shortcut does its job, and none fires while typing', async ({ page }) => {
  const account = 'keys@example.test';
  const stamp = Date.now();
  const older = `Older ${stamp}`;
  const newer = `Newer ${stamp}`;
  const third = `Third ${stamp}`;
  for (const subject of [older, newer, third]) await arrive(account, subject, `About ${subject}.`);

  await signIn(page, account, seedPassword());
  await expectInbox(page);
  const list = page.getByRole('region', { name: 'Conversations' });
  const reading = page.getByRole('region', { name: 'Reading pane' });
  await expect(list.getByRole('listitem').filter({ hasText: third })).toBeVisible();

  // j opens the first conversation, then the next; k goes back.
  await page.keyboard.press('j');
  await expect(reading).toContainText(third);
  await page.keyboard.press('j');
  await expect(reading).toContainText(newer);
  await page.keyboard.press('k');
  await expect(reading).toContainText(third);

  // r replies to the open conversation.
  await page.keyboard.press('r');
  const reply = page.getByRole('region', { name: `Re: ${third}` });
  await expect(reply).toBeVisible();
  // Typing in the composer is only typing.
  await reply.getByLabel('Message').pressSequentially('c j e #');
  await expect(page.getByRole('region', { name: 'New message' })).toHaveCount(0);
  await expect(reply).toBeVisible();
  await reply.getByRole('button', { name: 'Discard' }).click();
  await expect(reply).toBeHidden();

  // e archives the open conversation; # deletes the next one opened.
  await page.keyboard.press('e');
  await expect(page.getByRole('status')).toContainText('Conversation archived.');
  await expect(list.getByRole('listitem').filter({ hasText: third })).toHaveCount(0);
  await page.keyboard.press('j');
  await expect(reading).toContainText(newer);
  await page.keyboard.press('#');
  await expect(page.getByRole('status')).toContainText('Conversation moved to Trash.');
  await expect(list.getByRole('listitem').filter({ hasText: newer })).toHaveCount(0);
  await expect.poll(() => subjectsIn(account, 'Archive')).toContain(third);
  await expect.poll(() => subjectsIn(account, 'Trash')).toContain(newer);

  // With a row ticked by a click, e acts on it, though the box has the focus.
  const olderRow = list.getByRole('listitem').filter({ hasText: older });
  await olderRow.getByRole('checkbox').click();
  await page.keyboard.press('e');
  await expect(olderRow).toHaveCount(0);
  await expect.poll(() => subjectsIn(account, 'Archive')).toContain(older);

  // / goes to the search box, where j is just a letter.
  const box = page.getByRole('searchbox', { name: 'Search mail' });
  await page.keyboard.press('/');
  await expect(box).toBeFocused();
  await page.keyboard.type('jk');
  await expect(box).toHaveValue('jk');
  await box.blur();

  // c composes.
  await page.keyboard.press('c');
  await expect(page.getByRole('region', { name: 'New message' })).toBeVisible();
});
