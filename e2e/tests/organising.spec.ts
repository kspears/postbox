import { expect, test, type Page } from '@playwright/test';
import { expectInbox, foldersOf, fromIP, imap, seedPassword, signIn, subjectsIn } from '../helpers';

const organiser = 'organiser@example.test';
test.use(fromIP('203.0.113.110'));

function row(page: Page, subject: string) {
  return page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject });
}

function folderButton(page: Page, name: string) {
  return page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: new RegExp(`^${name}`) });
}

async function openConversation(page: Page, subject: string) {
  await row(page, subject).getByRole('link').click();
  await expect(page.getByRole('region', { name: 'Reading pane' }).getByRole('heading', { name: subject })).toBeVisible();
}

function readingAction(page: Page, name: string) {
  return page.getByRole('region', { name: 'Reading pane' }).getByRole('toolbar').getByRole('button', { name, exact: true });
}

test.beforeEach(async ({ page }) => {
  await signIn(page, organiser, seedPassword());
  await expectInbox(page);
});

test('criterion 15: Archive, Delete and Report spam move the conversation, and Undo brings it back', async ({ page }) => {
  for (const [subject, action, folder] of [
    ['Archive me', 'Archive', 'Archive'],
    ['Delete me', 'Delete', 'Trash'],
    ['Spam me', 'Report spam', 'Junk'],
  ] as const) {
    await openConversation(page, subject);
    await readingAction(page, action).click();
    // The list changes at once, before the server answers.
    await expect(row(page, subject)).toHaveCount(0);
    await expect.poll(() => subjectsIn(organiser, folder)).toContain(subject);
    expect(await subjectsIn(organiser, 'INBOX')).not.toContain(subject);

    await page.getByRole('status').getByRole('button', { name: 'Undo' }).click();
    await expect.poll(() => subjectsIn(organiser, 'INBOX')).toContain(subject);
    expect(await subjectsIn(organiser, folder)).not.toContain(subject);
    await expect(row(page, subject)).toHaveCount(1);
  }

  // Inside Junk the action is "Not spam", which moves it to Inbox.
  await openConversation(page, 'Spam me');
  await readingAction(page, 'Report spam').click();
  await expect.poll(() => subjectsIn(organiser, 'Junk')).toContain('Spam me');
  await folderButton(page, 'Junk').click();
  await openConversation(page, 'Spam me');
  await readingAction(page, 'Not spam').click();
  await expect.poll(() => subjectsIn(organiser, 'INBOX')).toContain('Spam me');
  await folderButton(page, 'Inbox').click();

  // Archiving a conversation leaves the reply in Sent where it is.
  await openConversation(page, 'Archive me');
  await readingAction(page, 'Archive').click();
  await expect.poll(() => subjectsIn(organiser, 'Archive')).toContain('Archive me');
  expect(await subjectsIn(organiser, 'Sent')).toContain('Re: Archive me');
});

test('criterion 16: Delete inside Trash asks, then deletes for good', async ({ page }) => {
  await folderButton(page, 'Trash').click();
  await openConversation(page, 'Trash me');
  await readingAction(page, 'Delete').click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Delete permanently? This cannot be undone.');
  await dialog.getByRole('button', { name: 'Delete' }).click();
  await expect(row(page, 'Trash me')).toHaveCount(0);
  await expect.poll(async () => {
    const everywhere = await Promise.all((await foldersOf(organiser)).map((f) => subjectsIn(organiser, f)));
    return everywhere.flat().includes('Trash me');
  }).toBe(false);
});

test('criterion 17: two selected conversations dragged to a folder, Move to…, and a folder nested by dragging', async ({ page }) => {
  // Folders to move into, made the way another mail app would.
  await imap(organiser, ['CREATE "Projects"', 'CREATE "Receipts"', 'CREATE "Old"']);
  await page.reload();
  await expect(folderButton(page, 'Projects')).toBeVisible();

  await row(page, 'Drag one').getByRole('checkbox').check();
  await row(page, 'Drag two').getByRole('checkbox').check();
  await expect(page.getByText('2 selected')).toBeVisible();
  await row(page, 'Drag one').dragTo(folderButton(page, 'Projects'));
  await expect.poll(() => subjectsIn(organiser, 'Projects')).toEqual(expect.arrayContaining(['Drag one', 'Drag two']));
  await expect(row(page, 'Drag one')).toHaveCount(0);

  await row(page, 'Move one').getByRole('checkbox').check();
  await row(page, 'Move two').getByRole('checkbox').check();
  await page.getByRole('toolbar').getByRole('button', { name: 'Move to…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Receipts' }).click();
  await expect.poll(() => subjectsIn(organiser, 'Receipts')).toEqual(expect.arrayContaining(['Move one', 'Move two']));

  // The middle of a folder nests the dragged one inside it.
  const target = page.getByRole('navigation', { name: 'Folders' }).getByRole('listitem').filter({ hasText: /^Projects/ });
  await page.getByRole('navigation', { name: 'Folders' }).getByRole('listitem').filter({ hasText: /^Old/ }).dragTo(target);
  await expect.poll(() => foldersOf(organiser)).toContain('Projects/Old');

  // The same without dragging: "Move folder…" from the folder's menu.
  await imap(organiser, ['CREATE "Loose"']);
  await page.reload();
  const nav = page.getByRole('navigation', { name: 'Folders' });
  await nav.getByRole('button', { name: 'Actions for Loose' }).click();
  await nav.getByRole('menuitem', { name: 'Move folder…' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Receipts', exact: true }).click();
  await expect.poll(() => foldersOf(organiser)).toContain('Receipts/Loose');
});

test('subfolders start collapsed, count toward their parent, and stay open once opened', async ({ page }) => {
  await imap(organiser, [
    'CREATE "Clients"',
    'CREATE "Clients/Acme"',
    'SELECT INBOX',
    'COPY 1 "Clients/Acme"',
    'SELECT "Clients/Acme"',
    'STORE 1 -FLAGS (\\Seen)',
  ]);
  await page.reload();
  const nav = page.getByRole('navigation', { name: 'Folders' });
  await expect(folderButton(page, 'Clients')).toBeVisible();
  await expect(folderButton(page, 'Acme')).toHaveCount(0);
  await expect(folderButton(page, 'Clients').getByLabel('1 unread')).toBeVisible();

  await nav.getByRole('button', { name: 'Expand Clients' }).click();
  await expect(folderButton(page, 'Acme').getByLabel('1 unread')).toBeVisible();
  await expect(folderButton(page, 'Clients').getByLabel(/unread/)).toHaveCount(0);

  await page.reload();
  await expect(folderButton(page, 'Acme')).toBeVisible();
  await nav.getByRole('button', { name: 'Collapse Clients' }).click();
  await expect(folderButton(page, 'Acme')).toHaveCount(0);
});

test('permanent delete is refused outside Trash', async ({ page, baseURL }) => {
  const response = await page.request.post('/api/expunge', {
    headers: { Origin: baseURL! },
    data: { folder: 'INBOX', uids: [1] },
  });
  expect(response.status()).toBe(400);
});

test('criterion 18: folders are created, renamed, deleted and reordered from the folder pane', async ({ page }) => {
  const nav = page.getByRole('navigation', { name: 'Folders' });
  await nav.getByRole('button', { name: '+ New folder' }).click();
  await nav.getByRole('textbox', { name: 'Folder name' }).fill('Trips');
  await nav.getByRole('textbox', { name: 'Folder name' }).press('Enter');
  await expect(folderButton(page, 'Trips')).toBeVisible();
  await expect.poll(() => foldersOf(organiser)).toContain('Trips');

  // A name that exists already is refused with the spec's message.
  await nav.getByRole('button', { name: '+ New folder' }).click();
  await nav.getByRole('textbox', { name: 'Folder name' }).fill('Trips');
  await nav.getByRole('textbox', { name: 'Folder name' }).press('Enter');
  await expect(nav.getByRole('alert')).toHaveText('A folder named “Trips” already exists.');
  await nav.getByRole('textbox', { name: 'Folder name' }).press('Escape');

  await nav.getByRole('button', { name: 'Actions for Trips' }).click();
  await nav.getByRole('menuitem', { name: 'Rename' }).click();
  await nav.getByRole('textbox', { name: 'Folder name' }).fill('Travel');
  await nav.getByRole('textbox', { name: 'Folder name' }).press('Enter');
  await expect(folderButton(page, 'Travel')).toBeVisible();
  await expect.poll(() => foldersOf(organiser)).toContain('Travel');
  expect(await foldersOf(organiser)).not.toContain('Trips');

  await nav.getByRole('button', { name: 'Actions for Travel' }).click();
  await nav.getByRole('menuitem', { name: 'New subfolder' }).click();
  await nav.getByRole('textbox', { name: 'Folder name' }).fill('Tickets');
  await nav.getByRole('textbox', { name: 'Folder name' }).press('Enter');
  await expect.poll(() => foldersOf(organiser)).toContain('Travel/Tickets');

  await nav.getByRole('button', { name: 'Actions for Travel' }).click();
  await nav.getByRole('menuitem', { name: 'Delete folder' }).click();
  await expect(page.getByRole('dialog')).toContainText('Delete “Travel”, its 1 subfolders, and the 0 messages in them? This cannot be undone.');
  await page.getByRole('dialog').getByRole('button', { name: 'Delete' }).click();
  await expect.poll(async () => (await foldersOf(organiser)).filter((f) => f.startsWith('Travel'))).toEqual([]);

  // Reorder: drop Zebra onto the top edge of Alpha, so it comes before it.
  await imap(organiser, ['CREATE "Alpha"', 'CREATE "Zebra"']);
  await page.reload();
  const items = nav.getByRole('listitem');
  const alpha = items.filter({ hasText: /^Alpha/ });
  await items.filter({ hasText: /^Zebra/ }).dragTo(alpha, { targetPosition: { x: 20, y: 2 } });
  const labels = async () => (await nav.getByRole('button').allInnerTexts()).map((t) => t.split('\n')[0]!.trim());
  await expect.poll(async () => { const l = await labels(); return l.indexOf('Zebra') < l.indexOf('Alpha'); }).toBe(true);

  // The order is Postbox's own and outlasts signing out and in again.
  await page.getByRole('button', { name: /^Account/ }).click();
  await page.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
  await signIn(page, organiser, seedPassword());
  await expectInbox(page);
  await expect.poll(async () => { const l = await labels(); return l.indexOf('Zebra') < l.indexOf('Alpha'); }).toBe(true);
});

test('criterion 19: flagged and marked unread in Postbox, as another client sees it', async ({ page }) => {
  await openConversation(page, 'Flag me');
  await readingAction(page, 'Flag').click();
  await expect(readingAction(page, 'Unflag')).toBeVisible();
  await readingAction(page, 'Mark unread').click();
  await expect.poll(async () => {
    const out = await imap(organiser, ['EXAMINE INBOX', 'SEARCH SUBJECT "Flag me" FLAGGED UNSEEN']);
    return /^\* SEARCH \d/m.test(out);
  }).toBe(true);
});
