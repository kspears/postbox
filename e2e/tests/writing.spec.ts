import { expect, test } from '@playwright/test';
import { deliver, expectInbox, fetchBySubject, fromIP, seedPassword, signIn, structureOf, subjectsIn, writeMessage } from '../helpers';

const writer = 'writer@example.test';
test.use(fromIP('203.0.113.120'));

test.beforeEach(async ({ page }) => {
  await signIn(page, writer, seedPassword());
  await expectInbox(page);
});

test('criterion 21: a closed composer leaves a draft that reopens, and sending files it in Sent', async ({ page }) => {
  const subject = `Draft ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'Written in Postbox.');
  await composer.getByRole('button', { name: 'Close' }).click();
  await expect(composer).toBeHidden();
  await expect.poll(() => subjectsIn(writer, 'Drafts')).toContain(subject);

  // Opening it from Drafts reopens the composer with what was written.
  await page.getByRole('navigation', { name: 'Folders' }).getByRole('button', { name: /^Drafts/ }).click();
  await page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject }).getByRole('link').click();
  const reopened = page.getByRole('region', { name: subject });
  await expect(reopened.getByLabel('Message')).toHaveValue(/Written in Postbox\./);

  await reopened.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toContainText('Sending…');
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });
  expect(await subjectsIn('bob@example.test', 'INBOX')).toContain(subject);
  const delivered = await structureOf('bob@example.test', 'INBOX', subject);
  expect(delivered).toContain('"text" "plain"');
  expect(delivered).toContain('"text" "html"');
  expect(await subjectsIn(writer, 'Sent')).toContain(subject);
  expect(await subjectsIn(writer, 'Drafts')).not.toContain(subject);
});

test('criterion 24: Undo within the window keeps the message from going', async ({ page }) => {
  const subject = `Undone ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'Never sent.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await page.getByRole('status').getByRole('button', { name: 'Undo' }).click();
  // The composer reopens with the message as it was.
  await expect(page.getByRole('region', { name: subject }).getByLabel('Message')).toHaveValue('Never sent.');
  await page.waitForTimeout(12_000);
  expect(await subjectsIn('bob@example.test', 'INBOX')).not.toContain(subject);
  expect(await subjectsIn(writer, 'Drafts')).toContain(subject);
});

test('criterion 24: closing the tab at once does not stop the send', async ({ page, context }) => {
  const subject = `Tab closed ${Date.now()}`;
  const composer = await writeMessage(page, 'bob@example.test', subject, 'Sent from a closed tab.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toContainText('Sending…');
  await page.close();
  await expect.poll(() => subjectsIn('bob@example.test', 'INBOX'), { timeout: 30_000 }).toContain(subject);
  void context;
});

test('criterion 25: a refused message sent from a closed tab is reported on the next visit', async ({ page, context }) => {
  const subject = `Refused ${Date.now()}`;
  const composer = await writeMessage(page, 'nobody@example.test', subject, 'To an address that does not exist.');
  await composer.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toContainText('Sending…');
  await page.close();
  await new Promise((resolve) => setTimeout(resolve, 13_000));

  const again = await context.newPage();
  await again.goto('/');
  await expectInbox(again);
  await expect(again.getByRole('alert').filter({ hasText: 'A message could not be sent and is in Drafts:' })).toContainText('Recipient address rejected');
  expect(await subjectsIn(writer, 'Drafts')).toContain(subject);
});

test('a refusal while the tab is open reopens the composer with the server\'s words', async ({ page }) => {
  const subject = `Refused open ${Date.now()}`;
  const composer = await writeMessage(page, 'nobody@example.test', subject, 'Draft stays intact.');
  await composer.getByRole('button', { name: 'Send' }).click();
  const reopened = page.getByRole('region', { name: subject });
  await expect(reopened.getByRole('alert')).toContainText('The mail server rejected this message:', { timeout: 20_000 });
  await expect(reopened.getByRole('alert')).toContainText('Recipient address rejected');
  await expect(reopened.getByLabel('Message')).toHaveValue('Draft stays intact.');
});

test('Reply quotes the message and addresses its sender', async ({ page }) => {
  // Something to reply to, from outside.
  const subject = `Reply source ${Date.now()}`;
  await deliver(writer, subject, 'Please answer this.');
  await expect(page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject })).toHaveCount(1, { timeout: 10_000 });
  await page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject }).first().getByRole('link').click();
  await page.getByRole('region', { name: 'Reading pane' }).getByRole('button', { name: 'Reply', exact: true }).last().click();
  const reply = page.getByRole('region', { name: `Re: ${subject}` });
  await expect(reply.getByLabel('Message')).toHaveValue(/> Please answer this\./);
  await expect(reply.getByLabel('To', { exact: true })).toHaveValue('Outside Sender <outside@example.org>');
});

test('a forward carries the attachment, through Undo and later saves of the reopened draft', async ({ page }) => {
  const subject = `With a file ${Date.now()}`;
  const content = 'bring the ladder';
  await deliver(
    writer,
    subject,
    [
      '--b1',
      'Content-Type: text/plain',
      '',
      'See the file.',
      '--b1',
      'Content-Type: text/plain; name=plan.txt',
      'Content-Disposition: attachment; filename=plan.txt',
      'Content-Transfer-Encoding: base64',
      '',
      Buffer.from(content).toString('base64'),
      '--b1--',
    ].join('\r\n'),
    ['MIME-Version: 1.0', 'Content-Type: multipart/mixed; boundary=b1'],
  );
  const row = page.getByRole('region', { name: 'Conversations' }).getByRole('listitem').filter({ hasText: subject });
  await expect(row).toHaveCount(1, { timeout: 10_000 });
  await row.getByRole('link').click();
  await page.getByRole('region', { name: 'Reading pane' }).getByRole('button', { name: 'Forward' }).last().click();
  const forward = page.getByRole('region', { name: `Fwd: ${subject}` });
  await expect(forward.getByText('plan.txt')).toBeVisible();
  await forward.getByLabel('To', { exact: true }).fill('bob@example.test');
  await forward.getByRole('button', { name: 'Send' }).click();
  await page.getByRole('status').getByRole('button', { name: 'Undo' }).click();

  // Reopened from Drafts: each save replaces the copy the attachment was read from.
  const reopened = page.getByRole('region', { name: `Fwd: ${subject}` });
  await expect(reopened.getByText('plan.txt')).toBeVisible();
  for (const word of ['First edit.', 'Second edit.']) {
    await reopened.getByLabel('Message').fill(word);
    await expect(reopened.getByText('Draft saved')).toBeVisible({ timeout: 10_000 });
  }
  await reopened.getByRole('button', { name: 'Send' }).click();
  await expect(page.getByRole('status')).toHaveText('Sent.', { timeout: 20_000 });
  const part = await fetchBySubject('bob@example.test', 'INBOX', `Fwd: ${subject}`, 'BODY.PEEK[2]');
  expect(part.replace(/\s/g, '')).toContain(Buffer.from(content).toString('base64'));
});

test('closing a composer whose draft cannot be saved keeps it open and says why', async ({ page }) => {
  const subject = `Unsaved ${Date.now()}`;
  const composer = await writeMessage(page, 'bob', subject, 'Not lost.');
  await composer.getByRole('button', { name: 'Close' }).click();
  await expect(composer.getByRole('alert')).toContainText('is not an email address');
  await expect(composer.getByLabel('Message')).toHaveValue('Not lost.');
});
