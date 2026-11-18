import { expect, test } from '@playwright/test';
import { alice, bob, expectInbox, expectSignInScreen, failSignIn, fromIP, seedPassword, signIn } from '../helpers';

test.describe('criterion 1: signing in', () => {
  test.use(fromIP('203.0.113.10'));

  test('the login screen asks only for email and password', async ({ page }) => {
    await page.goto('/');
    await expectSignInScreen(page);
    const fields = page.locator('input');
    await expect(fields).toHaveCount(3);
    await expect(page.getByLabel('Email')).toBeVisible();
    await expect(page.getByLabel('Password')).toBeVisible();
    await expect(page.getByLabel('Keep me signed in')).toBeVisible();
    await expect(page.getByLabel(/server/i)).toHaveCount(0);
  });

  test('a valid account opens the Inbox', async ({ page }) => {
    await signIn(page, alice, seedPassword());
    await expectInbox(page);
    await page.reload();
    await expectInbox(page);
  });

  test('a wrong password says so', async ({ page }) => {
    await signIn(page, alice, 'not-the-password');
    await expect(page.getByRole('alert')).toHaveText('Wrong email or password.');
    await expectSignInScreen(page);
  });

  test('an unknown address gets the same message', async ({ page }) => {
    await signIn(page, 'nobody@example.test', 'whatever');
    await expect(page.getByRole('alert')).toHaveText('Wrong email or password.');
  });
});

test.describe('criterion 2: throttling', () => {
  test('five failures for one address block it from that IP', async ({ browser, baseURL }) => {
    // deploy/ci/test.sh checks afterwards that dovecot saw exactly five logins from this address.
    const context = await browser.newContext(fromIP('203.0.113.20'));
    for (let i = 0; i < 5; i++) await failSignIn(context.request, baseURL!, alice);

    const page = await context.newPage();
    await signIn(page, alice, seedPassword());
    await expect(page.getByRole('alert')).toHaveText('Too many attempts. Try again in 15 minutes.');

    // Another address is a different person and is not blocked.
    const other = await browser.newContext(fromIP('203.0.113.21'));
    const otherPage = await other.newPage();
    await signIn(otherPage, alice, seedPassword());
    await expectInbox(otherPage);
    await Promise.all([context.close(), other.close()]);
  });

  test('twenty failures across addresses block the IP', async ({ browser, baseURL }) => {
    const context = await browser.newContext(fromIP('203.0.113.30'));
    await Promise.all(
      Array.from({ length: 20 }, (_, i) => failSignIn(context.request, baseURL!, `guess${i}@example.test`)),
    );
    const page = await context.newPage();
    await signIn(page, bob, seedPassword());
    await expect(page.getByRole('alert')).toHaveText('Too many attempts. Try again in 15 minutes.');
    await context.close();
  });
});

test.describe('signing out', () => {
  test('sign out ends this browser only', async ({ browser }) => {
    const [one, two] = await Promise.all([browser.newContext(fromIP('203.0.113.40')), browser.newContext(fromIP('203.0.113.40'))]);
    const [a, b] = await Promise.all([one.newPage(), two.newPage()]);
    await signIn(a, alice, seedPassword());
    await signIn(b, alice, seedPassword());
    await Promise.all([expectInbox(a), expectInbox(b)]);

    await a.getByRole('button', { name: /^Account/ }).click();
    await a.getByRole('menuitem', { name: 'Sign out', exact: true }).click();
    await expectSignInScreen(a);
    await a.reload();
    await expectSignInScreen(a);

    await b.reload();
    await expectInbox(b);
    await Promise.all([one.close(), two.close()]);
  });

  test('criterion 8: sign out everywhere ends every session of the account', async ({ browser }) => {
    const contexts = await Promise.all([1, 2, 3].map(() => browser.newContext(fromIP('203.0.113.41'))));
    const [a, b, other] = await Promise.all(contexts.map((c) => c.newPage()));
    await signIn(a, alice, seedPassword());
    await signIn(b, alice, seedPassword());
    await signIn(other, bob, seedPassword());
    await Promise.all([expectInbox(a), expectInbox(b), expectInbox(other)]);

    await a.getByRole('button', { name: /^Account/ }).click();
    await a.getByRole('menuitem', { name: 'Sign out everywhere' }).click();
    await expectSignInScreen(a);

    await b.reload();
    await expectSignInScreen(b);
    // Another account's session is untouched.
    await other.reload();
    await expectInbox(other);
    await Promise.all(contexts.map((c) => c.close()));
  });
});
