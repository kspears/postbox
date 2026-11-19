// The browser side of Postbox's HTTP API. Failures come back as error codes, never as text.

const knownErrors = [
  'wrong_credentials',
  'throttled',
  'unreachable',
  'signed_out',
  'password_changed',
  'no_folder',
  'not_found',
  'folder_exists',
  'too_late',
  'sending',
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

export interface Moved {
  folder: string;
  uids: number[];
}

export type Destination = { role: 'inbox' | 'archive' | 'trash' | 'junk' } | { to: string };

/** Moves messages and says where they went, with their new UIDs for an undo. */
export function move(folder: string, uids: number[], dest: Destination): Promise<Result<Moved>> {
  return call('POST', '/api/move', { folder, uids, ...dest });
}

/** Deletes messages for good. */
export function expunge(folder: string, uids: number[]): Promise<Result<void>> {
  return call('POST', '/api/expunge', { folder, uids });
}

export function setFlag(folder: string, uids: number[], flag: 'seen' | 'flagged', on: boolean): Promise<Result<void>> {
  return call('POST', '/api/flags', { folder, uids, flag, on });
}

export function createFolder(parent: string, label: string): Promise<Result<{ name: string }>> {
  return call('POST', '/api/folders', { parent, label });
}

/** Gives a folder a new label or a new parent ("" for the top level). */
export function renameFolder(
  folder: string,
  parent: string,
  label: string,
): Promise<Result<{ name: string; delimiter: string }>> {
  return call('POST', '/api/folders/rename', { folder, parent, label });
}

export function deleteFolder(folder: string): Promise<Result<void>> {
  return call('POST', '/api/folders/delete', { folder });
}

export function saveFolderOrder(order: string[]): Promise<Result<void>> {
  return call('PUT', '/api/folder-order', { order });
}

export interface DraftAttachment {
  folder?: string;
  uid?: number;
  part?: string;
  name: string;
  type: string;
  size: number;
}

export interface Draft {
  uid?: number;
  from: Person;
  to: Person[];
  cc: Person[];
  bcc: Person[];
  subject: string;
  text: string;
  html?: string;
  messageId?: string;
  inReplyTo?: string;
  references?: string[];
  attachments: DraftAttachment[];
}

export interface ComposerView {
  draft: Draft;
  identities: Person[];
  sizeLimit: number;
}

export type ComposeMode = 'new' | 'reply' | 'replyAll' | 'forward' | 'draft';

export function composer(mode: ComposeMode, folder?: string, uid?: number): Promise<Result<ComposerView>> {
  const query = new URLSearchParams({ mode });
  if (folder) query.set('folder', folder);
  if (uid) query.set('uid', String(uid));
  return call('GET', `/api/compose?${query}`);
}

/** Saves the draft; its attachments are then in the new copy, where the answer says. */
export function saveDraft(draft: Draft): Promise<Result<{ uid: number; messageId: string; attachments: DraftAttachment[] }>> {
  return call('POST', '/api/drafts', draft);
}

export function discardDraft(uid: number): Promise<Result<void>> {
  return call('POST', '/api/drafts/discard', { uid });
}

export function send(draft: Draft): Promise<Result<{ id: string }>> {
  return call('POST', '/api/send', draft);
}

export function undoSend(id: string): Promise<Result<{ folder: string; uid: number }>> {
  return call('POST', '/api/send/undo', { id });
}

export type SendState = 'waiting' | 'submitting' | 'sent' | 'sent_no_copy' | 'failed' | 'unsent' | 'unknown';

export interface SendStatus {
  id: string;
  draftsFolder: string;
  draftUid: number;
  state: SendState;
  // Why a send failed, and the server's own words when it refused the message.
  reason?: SendFailure;
  error?: string;
}

export type SendFailure = 'rejected' | 'refused' | 'unreachable' | 'unreadable' | 'invalid';

export function sendStatus(id: string): Promise<Result<SendStatus>> {
  return call('GET', `/api/send?${new URLSearchParams({ id })}`);
}

export async function notices(): Promise<Result<SendStatus[]>> {
  const result = await call<{ notices: SendStatus[] }>('GET', '/api/notices');
  return result.ok ? { ok: true, value: result.value.notices } : result;
}

export function dismissNotice(id: string): Promise<Result<void>> {
  return call('POST', '/api/notices/dismiss', { id });
}
