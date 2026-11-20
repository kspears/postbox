import { expect, test, type Page } from '@playwright/test';
import { deliver, expectInbox, fromIP, imapFlags, seedPassword, signIn } from '../helpers';

const reader = 'reader@example.test';
test.use(fromIP('203.0.113.90'));

function rows(page: Page) {
  return page.getByRole('region', { name: 'Conversations' }).getByRole('listitem');
}

async function open(page: Page, subject: string) {
  await rows(page).filter({ hasText: subject }).getByRole('link').click();
  await expect(page.getByRole('region', { name: 'Reading pane' }).getByRole('heading', { name: subject })).toBeVisible();
}

// Every request the page and its frames make, so a test can tell what reached the network.
function recordRequests(page: Page): string[] {
  const urls: string[] = [];
  page.on('request', (r) => urls.push(r.url()));
  return urls;
}

test('criterion 9: three panes on a desktop, one at a time on a phone', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await signIn(page, reader, seedPassword());
  await expectInbox(page);
  const nav = page.getByRole('navigation', { name: 'Folders' });
  const list = page.getByRole('region', { name: 'Conversations' });
  const reading = page.getByRole('region', { name: 'Reading pane' });
  await expect(nav).toBeVisible();
  await expect(list).toBeVisible();
  await expect(reading).toBeVisible();
  await expect.poll(() => reading.evaluate((pane) => pane.getBoundingClientRect().bottom)).toBe(800);

  await page.setViewportSize({ width: 320, height: 800 });
  const header = page.getByRole('banner');
  await expect(header.getByRole('button', { name: 'Compose' })).toBeInViewport({ ratio: 1 });
  await expect(header.getByRole('button', { name: /Account/ })).toBeInViewport({ ratio: 1 });
  await expect(header.getByRole('searchbox', { name: 'Search mail' })).toBeInViewport({ ratio: 1 });
  await expect(list).toBeVisible();
  await expect(nav).toBeHidden();
  await expect(reading).toBeHidden();

  await open(page, 'Garden shed');
  await expect(reading).toBeVisible();
  await expect(list).toBeHidden();
  await page.getByRole('button', { name: /Back/ }).click();
  await expect(list).toBeVisible();
  await expect(reading).toBeHidden();

  await page.getByRole('button', { name: 'Folders' }).click();
  await expect(nav).toBeVisible();
  await expect(list).toBeHidden();
});

test('criterion 10: the conversation joins the reply from Sent, oldest first', async ({ page }) => {
  await signIn(page, reader, seedPassword());
  await open(page, 'Garden shed');
  const messages = page.getByRole('region', { name: 'Reading pane' }).getByRole('article');
  await expect(messages).toHaveCount(2);
  await expect(messages.nth(0)).toContainText('Bob');
  await expect(messages.nth(0).frameLocator('iframe').locator('body')).toContainText('lend me the ladder');
  await expect(messages.nth(1)).toContainText('me');
  await expect(messages.nth(1).frameLocator('iframe').locator('body')).toContainText('by the back door');
});

test('criterion 12: remote images stay blocked until allowed for the sender', async ({ page }) => {
  const requests = recordRequests(page);
  await signIn(page, reader, seedPassword());
  await open(page, 'Pictures 1');
  const message = page.getByRole('region', { name: 'Reading pane' }).getByRole('article');
  await expect(message.getByRole('status')).toContainText('Remote images are blocked.');

  // The embedded logo shows; the remote photo was never asked for.
  const frame = message.frameLocator('iframe');
  const logo = frame.getByRole('img', { name: 'logo' });
  await expect(logo).toBeVisible();
  expect(await logo.evaluate((img: HTMLImageElement) => img.complete && img.naturalWidth > 0)).toBe(true);
  expect(requests.filter((u) => u.includes('sink'))).toEqual([]);

  await message.getByRole('button', { name: 'Always load from this sender' }).click();
  await expect(frame.getByRole('img', { name: 'photo' })).toBeVisible();

  // Another message from the same sender loads its images without asking.
  await open(page, 'Pictures 2');
  const second = page.getByRole('region', { name: 'Reading pane' }).getByRole('article');
  const photo = second.frameLocator('iframe').getByRole('img', { name: 'photo' });
  await expect(photo).toBeVisible();
  await expect.poll(() => photo.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);
  await expect(second.getByRole('status')).toHaveCount(0);
});

test('attachments are listed and download', async ({ page }) => {
  await signIn(page, reader, seedPassword());
  await open(page, 'Quarterly report');
  const download = page.waitForEvent('download');
  await page.getByRole('link', { name: 'Download figures.csv' }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe('figures.csv');
  const stream = await file.createReadStream();
  let text = '';
  for await (const chunk of stream) text += chunk;
  expect(text).toContain('Q3,42');
});

test('criterion 11: new mail appears without a reload and opening it marks it read', async ({ page }) => {
  await signIn(page, reader, seedPassword());
  await expectInbox(page);
  const inbox = page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: /^Inbox/ });
  const unread = async () => {
    const { folders } = (await (await page.request.get('/api/folders')).json()) as { folders: { name: string; unread: number }[] };
    return folders.find((f) => f.name === 'INBOX')?.unread ?? 0;
  };
  const shown = (n: number) => (n > 0 ? inbox.getByLabel(`${n} unread`) : inbox.locator('[aria-label$="unread"]'));
  const before = await unread();

  const subject = `Delivered ${Date.now()}`;
  await deliver(reader, subject, 'This arrived while the Inbox was open.');
  await expect(rows(page).first()).toContainText(subject, { timeout: 10_000 });
  await expect(shown(before + 1)).toBeVisible();

  await open(page, subject);
  await expect.poll(() => imapFlags(reader, subject)).toContain('\\Seen');
  if (before > 0) await expect(shown(before)).toBeVisible();
  else await expect(shown(0)).toHaveCount(0);
});
