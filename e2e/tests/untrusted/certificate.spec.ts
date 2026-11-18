import { expect, test } from '@playwright/test';
import { alice, fromIP, seedPassword, signIn } from '../../helpers';

// Criterion 28, certificate part. This project runs against postbox-untrusted, which has no
// POSTBOX_CA_FILE and so does not trust the test mail server's certificate.
test.use(fromIP('203.0.113.80'));

test('criterion 28: an untrusted certificate shows "Can\'t reach the mail server."', async ({ page }) => {
  await signIn(page, alice, seedPassword());
  await expect(page.getByRole('alert')).toHaveText("Can't reach the mail server.");
});
