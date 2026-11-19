// The browser side of Postbox's HTTP API. Failures come back as error codes, never as text.

const knownErrors = [
  'wrong_credentials',
  'throttled',
  'unreachable',
  'signed_out',
  'password_changed',
  'no_folder',
  'not_found',
  'internal',
] as const;
export type ErrorCode = (typeof knownErrors)[number];

export interface Account {
  account: string;
  method: 'password' | 'passkey';
}

export type Role = 'inbox' | 'drafts' | 'sent' | 'archive' | 'junk' | 'trash';

export interface Folder {
  name: string;
  label: string;
  parent?: string;
  role?: Role;
  unread: number;
  total: number;
  selectable: boolean;
}

export interface Person {
  name: string;
  email: string;
}

export interface Conversation {
  id: number;
  uids: number[];
  subject: string;
  participants: Person[];
  snippet: string;
  date: string;
  count: number;
  unread: boolean;
  flagged: boolean;
  attachments: boolean;
}

export interface Attachment {
  part: string;
  name: string;
  type: string;
  size: number;
}

export interface Message {
  folder: string;
  uid: number;
  from: Person[];
  to: Person[];
  cc: Person[];
  date: string;
  subject: string;
  unread: boolean;
  flagged: boolean;
  hasBody: boolean;
  attachments: Attachment[];
  body?: string;
  sender?: string;
  imagesAllowed: boolean;
}

export interface ConversationPage {
  conversations: Conversation[];
  next?: number;
}

export type Result<T> = { ok: true; value: T } | { ok: false; error: ErrorCode };

async function call<T>(method: string, path: string, body?: unknown): Promise<Result<T>> {
  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
    });
  } catch {
    // No answer at all: Postbox itself is unreachable.
    return { ok: false, error: 'internal' };
  }
  if (response.ok) {
    const value = response.status === 204 ? undefined : await response.json();
    return { ok: true, value: value as T };
  }
  try {
    const { error } = (await response.json()) as { error?: string };
    return { ok: false, error: knownErrors.find((code) => code === error) ?? 'internal' };
  } catch {
    return { ok: false, error: 'internal' };
  }
}

/** The signed-in account, or null when the browser has no session. */
export async function currentAccount(): Promise<Account | null> {
  const result = await call<Account>('GET', '/api/session');
  if (result.ok) return result.value;
  if (result.error === 'signed_out') return null;
  throw new Error(`GET /api/session: ${result.error}`);
}

export function signIn(email: string, password: string, keep: boolean): Promise<Result<Account>> {
  return call('POST', '/api/session', { email, password, keep });
}

/** True once the server has ended the session(s); a session that was already gone counts. */
export async function signOut(everywhere: boolean): Promise<boolean> {
  const result = await call<void>('DELETE', everywhere ? '/api/sessions' : '/api/session');
  return result.ok || result.error === 'signed_out';
}

export async function folders(): Promise<Result<Folder[]>> {
  const result = await call<{ folders: Folder[] }>('GET', '/api/folders');
  return result.ok ? { ok: true, value: result.value.folders } : result;
}

/** The page after cursor; shown is how many conversations are already listed. */
export function conversations(folder: string, cursor?: number, shown = 0): Promise<Result<ConversationPage>> {
  const query = new URLSearchParams({ folder });
  if (cursor) {
    query.set('cursor', String(cursor));
    query.set('shown', String(shown));
  }
  return call('GET', `/api/conversations?${query}`);
}

export async function conversation(folder: string, id: number): Promise<Result<Message[]>> {
  const query = new URLSearchParams({ folder, id: String(id) });
  const result = await call<{ messages: Message[] }>('GET', `/api/conversation?${query}`);
  return result.ok ? { ok: true, value: result.value.messages } : result;
}

export function markRead(folder: string, uids: number[]): Promise<Result<void>> {
  return call('POST', '/api/seen', { folder, uids });
}

export function allowImages(sender: string): Promise<Result<void>> {
  return call('POST', '/api/images/allow', { sender });
}

export function attachmentURL(m: Message, part: string): string {
  return `/api/attachment?${new URLSearchParams({ folder: m.folder, uid: String(m.uid), part })}`;
}

/** The body URL with remote images allowed for this one view. */
export function withImages(body: string): string {
  const url = new URL(body, location.origin);
  url.searchParams.set('images', '1');
  return url.pathname + url.search;
}
