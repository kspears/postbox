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
  'passkey_unsupported',
  'passkey_no_key',
  'passkey_failed',
  'passkey_required',
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

async function call<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<Result<T>> {
  let response: Response;
  try {
    response = await fetch(path, {
      method,
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
      credentials: 'same-origin',
      signal,
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

export interface Found extends Conversation {
  folder: string;
}

export interface SearchPage {
  messages: Found[];
  next?: number;
}

/** A page of the messages matching q, searched from folder. */
export function search(q: string, folder: string, offset: number, signal: AbortSignal): Promise<Result<SearchPage>> {
  const query = new URLSearchParams({ q, folder });
  if (offset) query.set('offset', String(offset));
  return call('GET', `/api/search?${query}`, undefined, signal);
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
  return partURL(m.folder, m.uid, part);
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
  // Where it is on the server: a part of the draft, or of the message being forwarded.
  folder?: string;
  uid?: number;
  part?: string;
  name: string;
  type: string;
  size: number;
  // The file's content, base64, for one added here and not yet saved.
  data?: string;
  // An image shown in the body, which refers to it as cid:<contentId>.
  inline?: boolean;
  contentId?: string;
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

export interface Identity {
  id: number;
  name: string;
  email: string;
  // Sanitised HTML; empty for none.
  signature: string;
}

export interface Preferences {
  appearance: 'system' | 'light' | 'dark';
  composer: 'rich' | 'plain';
}

export interface ComposerView {
  draft: Draft;
  identities: Identity[];
  sizeLimit: number;
}

export interface Passkey {
  id: string;
  added: string;
}

export interface SettingsView {
  preferences: Preferences;
  identities: Identity[];
  passkeys: Passkey[];
  requirePasskey: boolean;
}

export function settings(): Promise<Result<SettingsView>> {
  return call('GET', '/api/settings');
}

export function savePreferences(p: Preferences): Promise<Result<void>> {
  return call('PUT', '/api/preferences', p);
}

/** Adds an identity (id 0) or changes one; the answer is it as stored. */
export function saveIdentity(id: Identity): Promise<Result<Identity>> {
  return call('POST', '/api/identities', id);
}

export function deleteIdentity(id: number): Promise<Result<void>> {
  return call('POST', '/api/identities/delete', { id });
}

/** Addresses the person has sent mail to whose name or address starts with q. */
export async function addresses(q: string): Promise<Result<Person[]>> {
  const result = await call<{ addresses: Person[] }>('GET', `/api/addresses?${new URLSearchParams({ q })}`);
  return result.ok ? { ok: true, value: result.value.addresses } : result;
}

/** The address of a part of a message on the server. */
export function partURL(folder: string, uid: number, part: string): string {
  return `/api/attachment?${new URLSearchParams({ folder, uid: String(uid), part })}`;
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

// --- passkeys ---
// The server speaks WebAuthn's JSON forms (bytes as base64url); the browser's API wants bytes.

/** Whether this browser can use passkeys at all. */
export const passkeysSupported = typeof window !== 'undefined' && 'PublicKeyCredential' in window;

function fromBase64url(s: string): Uint8Array<ArrayBuffer> {
  const binary = atob(s.replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(binary, (c) => c.charCodeAt(0));
}

function toBase64url(bytes: ArrayBuffer | ArrayBufferView): string {
  const view = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  return btoa(String.fromCharCode(...view)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

interface DescriptorJSON {
  id: string;
  type: 'public-key';
  transports?: AuthenticatorTransport[];
}
interface PRFJSON {
  prf?: { eval?: { first: string } };
}

const descriptors = (list?: DescriptorJSON[]): PublicKeyCredentialDescriptor[] | undefined =>
  list?.map((d) => ({ ...d, id: fromBase64url(d.id) }));

const prfInput = (ext?: PRFJSON): AuthenticationExtensionsClientInputs | undefined =>
  ext?.prf?.eval ? { prf: { eval: { first: fromBase64url(ext.prf.eval.first) } } } : undefined;

type CreationJSON = Omit<PublicKeyCredentialCreationOptions, 'challenge' | 'user' | 'excludeCredentials' | 'extensions'> & {
  challenge: string;
  user: { id: string; name: string; displayName: string };
  excludeCredentials?: DescriptorJSON[];
  extensions?: PRFJSON;
};
type RequestJSON = Omit<PublicKeyCredentialRequestOptions, 'challenge' | 'allowCredentials' | 'extensions'> & {
  challenge: string;
  allowCredentials?: DescriptorJSON[];
  extensions?: PRFJSON;
};

function credentialJSON(c: PublicKeyCredential): unknown {
  const r = c.response;
  const response =
    r instanceof AuthenticatorAttestationResponse
      ? { clientDataJSON: toBase64url(r.clientDataJSON), attestationObject: toBase64url(r.attestationObject), transports: r.getTransports() }
      : {
          clientDataJSON: toBase64url(r.clientDataJSON),
          authenticatorData: toBase64url((r as AuthenticatorAssertionResponse).authenticatorData),
          signature: toBase64url((r as AuthenticatorAssertionResponse).signature),
          userHandle: (r as AuthenticatorAssertionResponse).userHandle
            ? toBase64url((r as AuthenticatorAssertionResponse).userHandle!)
            : undefined,
        };
  // The PRF output goes to the server on its own, never as part of the credential.
  return { id: c.id, rawId: toBase64url(c.rawId), type: c.type, authenticatorAttachment: c.authenticatorAttachment, response, clientExtensionResults: {} };
}

function prfOutput(c: PublicKeyCredential): string {
  const first = c.getClientExtensionResults().prf?.results?.first;
  return first ? toBase64url(first) : '';
}

/** The person closed the browser's passkey prompt, or it timed out. */
export const cancelled = Symbol('cancelled');

async function prompt(run: () => Promise<Credential | null>): Promise<PublicKeyCredential | typeof cancelled> {
  try {
    const c = await run();
    return c instanceof PublicKeyCredential ? c : cancelled;
  } catch (e) {
    if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) return cancelled;
    throw e;
  }
}

/** A passkey just created, waiting for its PRF output when create() gave none. */
export interface NewPasskey {
  nonce: string;
  credential: unknown;
  rawId: ArrayBuffer;
  salt?: string;
  prf: string;
  // The provider said it supports PRF. One that does not (1Password, Bitwarden) is saved
  // without, and the server locks its password instead.
  prfEnabled: boolean;
}

/** Asks the browser to create a passkey for this account (the first prompt of "Add a passkey"). */
export async function createPasskey(): Promise<Result<NewPasskey> | typeof cancelled> {
  const started = await call<{ nonce: string; options: CreationJSON }>('POST', '/api/passkeys/options');
  if (!started.ok) return started;
  const o = started.value.options;
  const publicKey: PublicKeyCredentialCreationOptions = {
    ...o,
    challenge: fromBase64url(o.challenge),
    user: { ...o.user, id: fromBase64url(o.user.id) },
    excludeCredentials: descriptors(o.excludeCredentials),
    extensions: prfInput(o.extensions),
  };
  const c = await prompt(() => navigator.credentials.create({ publicKey }));
  if (c === cancelled) return c;
  return {
    ok: true,
    value: {
      nonce: started.value.nonce,
      credential: credentialJSON(c),
      rawId: c.rawId,
      salt: o.extensions?.prf?.eval?.first,
      prf: prfOutput(c),
      prfEnabled: c.getClientExtensionResults().prf?.enabled === true,
    },
  };
}

/** Uses a passkey just created once, for the PRF output its creation did not give. Some browsers
 * allow one prompt per click, so this is a second step the person starts. */
export async function unlockNewPasskey(p: NewPasskey): Promise<NewPasskey | typeof cancelled> {
  const c = await prompt(() =>
    navigator.credentials.get({
      publicKey: {
        challenge: crypto.getRandomValues(new Uint8Array(32)),
        allowCredentials: [{ type: 'public-key', id: p.rawId }],
        userVerification: 'required',
        extensions: p.salt ? { prf: { eval: { first: fromBase64url(p.salt) } } } : undefined,
      },
    }),
  );
  return c === cancelled ? c : { ...p, prf: prfOutput(c) };
}

/** Stores a new passkey; it fails with passkey_unsupported when it gave no PRF output and the
 * server has no key to stand in. */
export function savePasskey(p: NewPasskey): Promise<Result<Passkey>> {
  return call('POST', '/api/passkeys', { nonce: p.nonce, credential: p.credential, prf: p.prf });
}

export function removePasskey(id: string): Promise<Result<{ requirePasskey: boolean }>> {
  return call('POST', '/api/passkeys/delete', { id });
}

export function setRequirePasskey(on: boolean): Promise<Result<void>> {
  return call('PUT', '/api/require-passkey', { on });
}

/** "Sign in with a passkey": the account, or a nonce when the mail server refused the password
 * stored for the passkey and a new one is needed. */
export async function passkeySignIn(): Promise<Result<Account | { newPassword: string }> | typeof cancelled> {
  const started = await call<{ nonce: string; options: RequestJSON }>('POST', '/api/passkey-session/options');
  if (!started.ok) return started;
  const o = started.value.options;
  const publicKey: PublicKeyCredentialRequestOptions = {
    ...o,
    challenge: fromBase64url(o.challenge),
    allowCredentials: descriptors(o.allowCredentials),
    extensions: prfInput(o.extensions),
  };
  const c = await prompt(() => navigator.credentials.get({ publicKey }));
  if (c === cancelled) return c;
  return call('POST', '/api/passkey-session', { nonce: started.value.nonce, credential: credentialJSON(c), prf: prfOutput(c) });
}

/** Finishes a passkey sign-in with the account's new password. */
export function passkeyNewPassword(nonce: string, password: string): Promise<Result<Account>> {
  return call('POST', '/api/passkey-session/password', { nonce, password });
}
