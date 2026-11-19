import { expect, test, type Locator, type Page } from '@playwright/test';
import { expectInbox, fetchBySubject, fromIP, openSettings, seedPassword, signIn, structureOf, writeMessage } from '../helpers';

// Milestone 7: rich text, files, identities and signatures, and addresses offered as you type.
test.use(fromIP('203.0.113.140'));

// A 1×1 PNG.
const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';

interface DropFile {
  name: string;
  type: string;
  base64?: string;
  megabytes?: number; // a file of zeros this large instead
}

/** Drops files on target as a drag from the desktop would. */
async function drop(target: Locator, files: DropFile[]): Promise<void> {
  const dataTransfer = await target.page().evaluateHandle((files: DropFile[]) => {
    const dt = new DataTransfer();
    for (const f of files) {
      const parts: BlobPart[] = f.megabytes
        ? Array.from({ length: f.megabytes }, () => new Uint8Array(1 << 20))
        : [Uint8Array.from(atob(f.base64 ?? ''), (c) => c.charCodeAt(0))];
      dt.items.add(new File(parts, f.name, { type: f.type }));
    }
    return dt;
  }, files);
  // Where the drop lands, as a real drag reports it; the editor places images there.
  const box = await target.boundingBox();
  const at = box ? { clientX: box.x + box.width / 2, clientY: box.y + Math.min(box.height / 2, 10) } : {};
  for (const type of ['dragenter', 'dragover', 'drop']) await target.dispatchEvent(type, { dataTransfer, ...at });
}

/** A part fetched over IMAP, with its quoted-printable encoding undone. */
async function partText(account: string, subject: string, part: string): Promise<string> {
  const raw = await fetchBySubject(account, 'INBOX', subject, `BODY.PEEK[${part}]`);
  return raw.replace(/=\r?\n/g, '').replace(/=([0-9A-F]{2})/g, (_, hex: string) => String.fromCharCode(parseInt(hex, 16)));
}


test('criterion 21: rich text is kept in Drafts and arrives as HTML with a plain-text part', async ({ page }) => {
  await signIn(page, 'writer@example.test', seedPassword());
  await expectInbox(page);
  const subject = `Rich ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'Plain words, then ');
  const body = composer.getByLabel('Message');
  await body.press('End');
  await composer.getByRole('button', { name: 'Bold' }).click();
  await body.pressSequentially('bold ones');
  await composer.getByRole('button', { name: 'Close' }).click();
  await expect(composer).toBeHidden();

  await page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: /^Drafts/ }).click();
  await page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject }).getByRole('link').click();
  const reopened = page.getByRole('region', { name: subject });
  await expect(reopened.getByLabel('Message').locator('strong')).toHaveText('bold ones');

  await reopened.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });
  expect(await partText('bob@example.test', subject, '1.2')).toMatch(/<strong> ?bold ones<\/strong>/);
  expect(await partText('bob@example.test', subject, '1.1')).toContain('Plain words, then bold ones');
});

test('criterion 22: dropped files attach, dropped images go inline, and too-large files are refused', async ({ page }) => {
  await signIn(page, 'writer@example.test', seedPassword());
  await expectInbox(page);
  const subject = `Files ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'A file and a picture.');

  await drop(composer, [{ name: 'notes.txt', type: 'text/plain', base64: btoa('bring the ladder') }]);
  await expect(composer.getByRole('list', { name: 'Attachments' })).toContainText('notes.txt');

  await drop(composer.getByLabel('Message'), [{ name: 'dot.png', type: 'image/png', base64: png }]);
  await expect(composer.getByLabel('Message').locator('img')).toHaveCount(1);

  // The test mail server accepts up to 100 MiB.
  await drop(composer, [{ name: 'huge.bin', type: 'application/octet-stream', megabytes: 101 }]);
  await expect(composer.getByRole('alert')).toHaveText(/^This file is too large\. The mail server accepts messages up to 100(\.0)? MB\.$/);
  await expect(composer.getByRole('list', { name: 'Attachments' })).not.toContainText('huge.bin');

  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });
  const structure = await structureOf('bob@example.test', 'INBOX', subject);
  expect(structure).toContain('"notes.txt"');
  expect(structure).toMatch(/"image" "png".*"inline"/);
  // The HTML sits with its image in a related part.
  expect(structure).toContain('"related"');
  expect(await partText('bob@example.test', subject, '1.1.2')).toContain('<img src="cid:');
});

test('criterion 23: signatures follow the From identity, and a refused sender keeps the draft', async ({ page }) => {
  await signIn(page, 'signer@example.test', seedPassword());
  await expectInbox(page);
  const settings = await openSettings(page);
  await settings.getByRole('button', { name: 'Edit signer@example.test' }).click();
  await settings.getByLabel('Name').fill('Sam Signer');
  await settings.getByRole('textbox', { name: 'Signature' }).fill('Sam Signer, Example Co');
  await settings.getByRole('button', { name: 'Save' }).click();
  for (const [name, email, signature] of [
    ['Sales', 'sales@example.test', 'The sales team'],
    ['Boss', 'boss@example.test', 'The boss'],
  ]) {
    await settings.getByRole('button', { name: 'Add identity' }).click();
    await settings.getByLabel('Name').fill(name!);
    await settings.getByLabel('Email address').fill(email!);
    await settings.getByRole('textbox', { name: 'Signature' }).fill(signature!);
    await settings.getByRole('button', { name: 'Save' }).click();
    await expect(settings.getByRole('listitem').filter({ hasText: email! })).toBeVisible();
  }
  await settings.getByRole('button', { name: 'Close' }).click();

  // A new message starts with the login identity's signature; changing From replaces it.
  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  let composer = page.getByRole('region', { name: 'New message' });
  const body = composer.getByLabel('Message');
  await expect(body).toContainText('Sam Signer, Example Co');
  await composer.getByLabel('From').selectOption({ label: 'Sales <sales@example.test>' });
  await expect(body).toContainText('The sales team');
  await expect(body).not.toContainText('Sam Signer, Example Co');

  // Sending as an address the account owns works.
  const allowed = `From sales ${Date.now()}`;
  await composer.getByLabel('To', { exact: true }).fill('bob@example.test');
  await composer.getByLabel('Subject').fill(allowed);
  await page.getByRole('region', { name: allowed }).getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });

  // Sending as one it does not own is refused, and the draft comes back intact.
  const refused = `From boss ${Date.now()}`;
  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  composer = page.getByRole('region', { name: 'New message' });
  await composer.getByLabel('From').selectOption({ label: 'Boss <boss@example.test>' });
  await composer.getByLabel('To', { exact: true }).fill('bob@example.test');
  await composer.getByLabel('Subject').fill(refused);
  composer = page.getByRole('region', { name: refused });
  await composer.getByLabel('Message').click();
  await composer.getByLabel('Message').press('ControlOrMeta+Home');
  await composer.getByLabel('Message').pressSequentially('Keep me.');
  await composer.getByRole('button', { name: 'Send' }).click();
  const reopened = page.getByRole('region', { name: refused });
  await expect(reopened.getByRole('alert').first()).toContainText('The mail server rejected this message:', { timeout: 20_000 });
  await expect(reopened.getByRole('alert').first()).toContainText('not owned by user');
  await expect(reopened.getByLabel('Message')).toContainText('Keep me.');
  await expect(reopened.getByLabel('From').locator('option:checked')).toHaveText('Boss <boss@example.test>');
});

test('criterion 26: an address sent to is offered after three characters of its name or address', async ({ page }) => {
  await signIn(page, 'writer@example.test', seedPassword());
  await expectInbox(page);
  const subject = `To Bob Builder ${Date.now()}`;
  const composer = await writeMessage(page, 'Bob Builder <bob@example.test>', subject, 'Collect me.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });

  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  const next = page.getByRole('region', { name: 'New message' });
  const to = next.getByLabel('To', { exact: true });
  for (const typed of ['Bob', 'bob']) {
    await to.fill('');
    await to.pressSequentially(typed);
    await expect(next.getByRole('option').filter({ hasText: 'bob@example.test' })).toBeVisible();
  }
  await to.press('Enter');
  await expect(to).toHaveValue('Bob Builder <bob@example.test>, ');
});

test('criterion 26: at first sign-in, the addresses in Sent are collected', async ({ page }) => {
  await signIn(page, 'collector@example.test', seedPassword());
  await expectInbox(page);
  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  const to = page.getByRole('region', { name: 'New message' }).getByLabel('To', { exact: true });
  for (const typed of ['Dan', 'dan', 'ops']) {
    await expect(async () => {
      await to.fill('');
      await to.pressSequentially(typed);
      await expect(page.getByRole('option').first()).toBeVisible({ timeout: 1000 });
    }).toPass({ timeout: 20_000 });
  }
  await expect(page.getByRole('option')).toContainText('ops@example.org');
});

test('Settings: appearance applies and stays; plain text makes new messages plain', async ({ page }) => {
  await signIn(page, 'collector@example.test', seedPassword());
  await expectInbox(page);
  const settings = await openSettings(page);
  await settings.getByLabel('Dark').check();
  await settings.getByLabel('Plain text').check();
  await settings.getByRole('button', { name: 'Close' }).click();
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe('dark');

  await page.reload();
  await expectInbox(page);
  await expect.poll(() => page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe('dark');
  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  const body = page.getByRole('region', { name: 'New message' }).getByLabel('Message');
  await expect(body).toHaveJSProperty('tagName', 'TEXTAREA');
});
