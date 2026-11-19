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

/** Delivers a message to an account the way mail from outside arrives: SMTP on port 25. headers
 * go after the usual ones, such as a Content-Type for a multipart body. */
export async function deliver(to: string, subject: string, body: string, headers: string[] = []): Promise<void> {
  const net = await import('node:net');
  const socket = net.connect(25, 'postfix');
  const message = [
    `From: Outside Sender <outside@example.org>`,
    `To: ${to}`,
    `Subject: ${subject}`,
    `Date: ${new Date().toUTCString()}`,
    `Message-ID: <${Date.now()}.${Math.random().toString(36).slice(2)}@example.org>`,
    ...headers,
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

/** What another IMAP client fetches (items, such as FLAGS) of the message with subject in one of
 * account's folders. */
export async function fetchBySubject(account: string, folder: string, subject: string, items: string): Promise<string> {
  const found = await imap(account, [`EXAMINE ${quoted(folder)}`, `SEARCH SUBJECT ${quoted(subject)}`]);
  const id = /^\* SEARCH (\d+)/m.exec(found)?.[1];
  if (!id) throw new Error(`no message "${subject}" in ${account}'s ${folder}`);
  return imap(account, [`EXAMINE ${quoted(folder)}`, `FETCH ${id} ${items}`]);
}

/** The flags of the message with subject in account's Inbox, as another IMAP client sees them. */
export async function imapFlags(account: string, subject: string): Promise<string> {
  const flags = await fetchBySubject(account, 'INBOX', subject, 'FLAGS');
  return /FLAGS \(([^)]*)\)/.exec(flags)?.[1] ?? '';
}

/** s as an IMAP quoted string. */
function quoted(s: string): string {
  return `"${s.replace(/["\\]/g, '\\$&')}"`;
}

/** Runs commands as another IMAP client would, logged in as account, and returns everything the
 * server said. Each command is tagged t1, t2…, and its answer is waited for. */
export async function imap(account: string, commands: string[]): Promise<string> {
  const tls = await import('node:tls');
  const fs = await import('node:fs');
  const socket = tls.connect({ host: 'dovecot', port: 993, servername: 'dovecot', ca: fs.readFileSync('/ca/ca.crt') });
  const all = [`LOGIN ${quoted(account)} ${quoted(seedPassword())}`, ...commands];
  return converse(socket, [
    [null, /^\* OK/m],
    ...all.map((c, i): [string, RegExp] => [`t${i} ${c}`, new RegExp(`^t${i} (OK|NO|BAD)`, 'm')]),
  ]);
}

/** The subjects of the messages in one of account's folders; null when the folder does not exist. */
export async function subjectsIn(account: string, folder: string): Promise<string[] | null> {
  const out = await imap(account, [`EXAMINE ${quoted(folder)}`, 'FETCH 1:* (BODY.PEEK[HEADER.FIELDS (SUBJECT)])']);
  if (/^t1 NO/m.test(out)) return null;
  return [...out.matchAll(/^Subject: (.*)$/gim)].map((m) => m[1]!.trim());
}

/** Every folder of account, by full name. */
export async function foldersOf(account: string): Promise<string[]> {
  const out = await imap(account, ['LIST "" "*"']);
  return [...out.matchAll(/^\* LIST \([^)]*\) "[^"]*" (?:"((?:[^"\\]|\\.)*)"|(\S+))$/gm)].map((m) => (m[1] ?? m[2]!).replace(/\\(.)/g, '$1'));
}

/** Opens the composer, fills it and returns it, ready to send. */
export async function writeMessage(page: Page, to: string, subject: string, body: string) {
  await page.getByRole('button', { name: 'Compose', exact: true }).click();
  const composer = page.getByRole('region', { name: 'New message' });
  await composer.getByLabel('To', { exact: true }).fill(to);
  await composer.getByLabel('Subject').fill(subject);
  const named = page.getByRole('region', { name: subject });
  await named.getByLabel('Message').fill(body);
  return named;
}

/** The MIME types in a message's structure, as another IMAP client sees it. */
export async function structureOf(account: string, folder: string, subject: string): Promise<string> {
  return (await fetchBySubject(account, folder, subject, 'BODYSTRUCTURE')).toLowerCase();
}
