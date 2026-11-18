import { expect, test } from '@playwright/test';
import { existsSync, rmSync, writeFileSync } from 'node:fs';
import { expectInbox, fromIP, seedPassword, signIn } from '../../helpers';

// Criterion 28, banner part. deploy/ci/test.sh runs this in the background and talks to it
// through files in the shared .state volume: it stops dovecot once the Inbox is open, and
// starts it again once the banner shows.
test.use(fromIP('203.0.113.99'));

const ready = '.state/outage-ready';
const bannerShown = '.state/outage-banner';
const restarted = '.state/outage-restarted';

async function until(file: string): Promise<void> {
  await expect.poll(() => existsSync(file), { timeout: 60_000, intervals: [250] }).toBe(true);
}

test('criterion 28: the banner shows while dovecot is down and clears when it is back', async ({ page }) => {
  test.setTimeout(150_000);
  for (const f of [ready, bannerShown, restarted]) rmSync(f, { force: true });
  await signIn(page, 'alice@example.test', seedPassword());
  await expectInbox(page);
  const rows = page.getByRole('region', { name: 'Conversations' }).getByRole('listitem');
  await expect(rows.first()).toBeVisible();
  writeFileSync(ready, '');

  const banner = page.getByRole('alert').filter({ hasText: "Can't reach the mail server. Retrying…" });
  await expect(banner).toBeVisible({ timeout: 30_000 });
  // What was on screen stays readable.
  await expect(rows.first()).toBeVisible();
  writeFileSync(bannerShown, '');

  await until(restarted);
  await expect(banner).toBeHidden({ timeout: 60_000 });
  await expect(rows.first()).toBeVisible();
});
