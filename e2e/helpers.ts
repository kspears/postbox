import { expect, type APIRequestContext, type Page } from '@playwright/test';

export const alice = 'alice@example.test';
export const bob = 'bob@example.test';

export function seedPassword(): string {
  const password = process.env.SEED_PASSWORD;
  if (!password) throw new Error('SEED_PASSWORD is not set; run the tests through deploy/ci/test.sh');
  return password;
}

// Postbox trusts X-Test-Client-IP in the test stack. Each test that signs in picks its own
// address from 203.0.113.0/24, so one test's failed attempts never throttle another's.
export function fromIP(ip: string): { extraHTTPHeaders: Record<string, string> } {
  return { extraHTTPHeaders: { 'X-Test-Client-IP': ip } };
}

export async function signIn(page: Page, email: string, password: string, keep = false): Promise<void> {
  await page.goto('/');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  if (keep) await page.getByLabel('Keep me signed in').check();
  await page.getByRole('button', { name: 'Sign in' }).click();
}

export async function expectInbox(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible();
}

export async function expectSignInScreen(page: Page): Promise<void> {
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible();
}

// A failed sign-in straight through the API, for tests that need many of them quickly. The
// browser would send Origin itself; a bare API request has to say it.
export async function failSignIn(request: APIRequestContext, baseURL: string, email: string): Promise<void> {
  const response = await request.post('/api/session', {
    headers: { Origin: baseURL },
    data: { email, password: 'not-the-password', keep: false },
  });
  expect(response.status(), `failed sign-in for ${email}`).toBe(401);
}

export async function advanceClock(request: APIRequestContext, baseURL: string, seconds: number): Promise<void> {
  const response = await request.post('/api/test/clock', { headers: { Origin: baseURL }, data: { seconds } });
  expect(response.status()).toBe(204);
}

// A line-at-a-time conversation with a mail server, for the tests that play another client.
async function converse(socket: import('node:net').Socket, steps: [send: string | null, expect: RegExp][]): Promise<string> {
  let buffer = '';
  let transcript = '';
  socket.setEncoding('utf8');
  const waitFor = (re: RegExp) =>
    new Promise<void>((resolve, reject) => {
      const check = () => {
        if (re.test(buffer)) {
          transcript += buffer;
          buffer = '';
          socket.off('data', onData);
          resolve();
        }
      };
      const onData = (chunk: string) => {
        buffer += chunk;
        check();
      };
      socket.on('data', onData);
      socket.once('error', reject);
      setTimeout(() => reject(new Error(`no ${re} from the mail server; got ${JSON.stringify(buffer)}`)), 10_000);
      check();
    });
  for (const [send, expect] of steps) {
    if (send !== null) socket.write(send + '\r\n');
    await waitFor(expect);
  }
  socket.end();
  return transcript;
}

/** Delivers a message to an account the way mail from outside arrives: SMTP on port 25. */
export async function deliver(to: string, subject: string, body: string): Promise<void> {
  const net = await import('node:net');
  const socket = net.connect(25, 'postfix');
  const message = [
    `From: Outside Sender <outside@example.org>`,
    `To: ${to}`,
    `Subject: ${subject}`,
    `Date: ${new Date().toUTCString()}`,
    `Message-ID: <${Date.now()}.${Math.random().toString(36).slice(2)}@example.org>`,
    '',
    body,
    '.',
  ].join('\r\n');
  await converse(socket, [
    [null, /^220 /m],
    ['EHLO e2e.example.org', /^250 /m],
    ['MAIL FROM:<outside@example.org>', /^250 /m],
    [`RCPT TO:<${to}>`, /^250 /m],
    ['DATA', /^354 /m],
    [message, /^250 /m],
    ['QUIT', /^221 /m],
  ]);
}

/** The flags of the message with subject in account's Inbox, as another IMAP client sees them. */
export async function imapFlags(account: string, subject: string): Promise<string> {
  const tls = await import('node:tls');
  const fs = await import('node:fs');
  const socket = tls.connect({ host: 'dovecot', port: 993, servername: 'dovecot', ca: fs.readFileSync('/ca/ca.crt') });
  const quoted = (s: string) => `"${s.replace(/["\\]/g, '\\$&')}"`;
  const transcript = await converse(socket, [
    [null, /^\* OK/m],
    [`a LOGIN ${quoted(account)} ${quoted(seedPassword())}`, /^a OK/m],
    ['b EXAMINE INBOX', /^b OK/m],
    [`c SEARCH SUBJECT ${quoted(subject)}`, /^c OK/m],
  ]);
  const ids = /^\* SEARCH ([\d ]+)/m.exec(transcript)?.[1]?.trim();
  if (!ids) throw new Error(`no message "${subject}" in ${account}'s Inbox`);
  const again = tls.connect({ host: 'dovecot', port: 993, servername: 'dovecot', ca: fs.readFileSync('/ca/ca.crt') });
  const flags = await converse(again, [
    [null, /^\* OK/m],
    [`a LOGIN ${quoted(account)} ${quoted(seedPassword())}`, /^a OK/m],
    ['b EXAMINE INBOX', /^b OK/m],
    [`c FETCH ${ids.split(' ')[0]} FLAGS`, /^c OK/m],
  ]);
  return /FLAGS \(([^)]*)\)/.exec(flags)?.[1] ?? '';
}
