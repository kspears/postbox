// The browser side of Postbox's HTTP API. Failures come back as error codes, never as text.

const knownErrors = ['wrong_credentials', 'throttled', 'unreachable', 'internal'] as const;
export type ErrorCode = (typeof knownErrors)[number];

export interface Account {
  account: string;
  method: 'password' | 'passkey';
}

export type Result<T> = { ok: true; value: T } | { ok: false; error: ErrorCode };

async function call(method: string, path: string, body?: unknown): Promise<Response> {
  return fetch(path, {
    method,
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    credentials: 'same-origin',
  });
}

async function errorOf(response: Response): Promise<ErrorCode> {
  try {
    const { error } = (await response.json()) as { error?: string };
    return knownErrors.find((code) => code === error) ?? 'internal';
  } catch {
    return 'internal';
  }
}

/** The signed-in account, or null when the browser has no session. */
export async function currentAccount(): Promise<Account | null> {
  const response = await call('GET', '/api/session');
  if (response.status === 401) return null;
  if (!response.ok) throw new Error(`GET /api/session: ${response.status}`);
  return (await response.json()) as Account;
}

export async function signIn(email: string, password: string, keep: boolean): Promise<Result<Account>> {
  try {
    const response = await call('POST', '/api/session', { email, password, keep });
    if (response.ok) return { ok: true, value: (await response.json()) as Account };
    return { ok: false, error: await errorOf(response) };
  } catch {
    // The request never got an answer: Postbox itself is unreachable.
    return { ok: false, error: 'internal' };
  }
}

/** True once the server has ended the session(s); a session that was already gone counts. */
export async function signOut(everywhere: boolean): Promise<boolean> {
  try {
    const response = await call('DELETE', everywhere ? '/api/sessions' : '/api/session');
    return response.ok || response.status === 401;
  } catch {
    return false;
  }
}
