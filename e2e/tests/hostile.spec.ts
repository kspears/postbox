import { expect, test } from '@playwright/test';
import { expectInbox, fromIP, seedPassword, signIn } from '../helpers';

// Criterion 13: every message of the hostile corpus (testdata/hostile) is opened. None may run
// a script, and the page may make no request but Postbox's own for the message and its parts.
// deploy/ci/test.sh also checks that the sink container, where every remote reference in the
// corpus points, logged nothing.
test.use(fromIP('203.0.113.95'));

test('criterion 13: the hostile corpus runs no script and makes no outside request', async ({ page, baseURL }) => {
  test.setTimeout(120_000);
  // Postbox's own requests for the app, the list and each message. Anything else, to Postbox's
  // address or another, came from a message.
  const own = /^\/(folder\/|assets\/|api\/(session|folders|conversations?|body|seen|events|notices|settings)(\?|$))/;
  const outside: string[] = [];
  page.on('request', (r) => {
    const url = new URL(r.url());
    if (url.origin !== new URL(baseURL!).origin || !own.test(url.pathname + url.search)) outside.push(r.url());
  });
  page.on('dialog', (d) => {
    outside.push(`dialog: ${d.message()}`);
    d.dismiss();
  });

  await signIn(page, 'hostile@example.test', seedPassword());
  outside.length = 0; // the sign-in page's own load is not under test
  await expectInbox(page);
  const rows = page.getByRole('region', { name: 'Conversations' }).getByRole('listitem');
  await expect(rows).toHaveCount(29);
  for (let i = 0; i < 29; i++) {
    const row = rows.nth(i);
    const subject = (await row.locator('.subject').innerText()).trim();
    await row.getByRole('link').click();
    const pane = page.getByRole('region', { name: 'Reading pane' });
    await expect(pane.getByRole('heading')).toHaveText(subject);
    await expect(pane.locator('iframe').or(pane.getByText('This message has no content.')).first()).toBeVisible();
    // Give anything that might fire (onload, autofocus, animations, refresh) its chance.
    await page.waitForTimeout(300);
    expect(await page.title(), subject).toBe('Postbox');
  }
  expect(outside).toEqual([]);
});
