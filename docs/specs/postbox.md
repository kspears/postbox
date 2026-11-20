# Postbox

Status: Reviewed
Date: 2026-11-18
Summary: Postbox is a standalone webmail client, shipped as one Go binary in one container, that
talks IMAP and SMTP submission to an existing mail server and is an optional companion to
mailstack.

## Problem and goals

mailstack replaces mailcow and drops SOGo. Its users (about 20 accounts, a handful signed in at
once) mostly read mail in desktop and phone clients and need a web client for the times they
cannot. Roundcube and SOGo feel dated and their drag and drop is unreliable.

Goals:

1. A web mail client that feels current: roomy spacing, larger type, few visible controls, and
   no waiting on common actions.
2. Simple to run: one binary, one container, one SQLite file, configured by environment
   variables.
3. Usable without mailstack. Postbox needs only IMAP and SMTP submission, so it can replace
   Roundcube in front of another mail server. Dovecot and Postfix are the tested target. Other
   standards-compliant IMAP servers are best-effort. When the two audiences conflict, mailstack
   users win.
4. Secure from the first release: passkey sign-in, login throttling, and safe rendering of
   untrusted HTML mail.

Postbox lives in its own public repository, licensed AGPL-3.0. mailstack includes it as an
optional compose service. This spec lives in the mailstack repository until the Postbox
repository exists, then moves there.

## Non-goals

Out of scope for good:

- Sieve filters and vacation replies.
- Gmail's "All Mail" model. Archive is a folder.
- Special handling for Gmail or Exchange quirks.
- A "server" field on the login screen. One deployment serves one mail server.
- Any dependency on `mailctl`, `store.db`, or mailstack's generated lookup files.
- Offline mode.
- Authenticator-code (TOTP) sign-in.
- Protecting IMAP and SMTP logins made by other mail clients. "Require passkey" guards Postbox
  only; the account password still works against the mail server directly. Closing that needs
  per-device app passwords in mailstack, which is a separate spec.

Deferred to a later phase:

- Calendar and a full address book (CalDAV, CardDAV). Dropping SOGo also drops its CalDAV and
  CardDAV server; Postbox v1 does not replace it.
- A unified inbox across several accounts.
- A remote-image proxy.
- Joining messages filed in other folders into a conversation (see "Conversations").
- Coloured flags and labels.
- Folder subscribe and unsubscribe management.
- Changing the account password from inside Postbox.
- Plugins, themes, PGP, and languages other than English.
- A local message cache. The browser-facing API must not prevent adding one later.

## Users and flows

Two kinds of people use Postbox: a mail user with an account on the configured mail server, and
an operator who deploys the container and runs its CLI.

### Signing in

1. The login screen has two fields, "Email" and "Password", a "Keep me signed in" checkbox, and
   a "Sign in" button. On a browser that supports passkeys it also shows "Sign in with a
   passkey".
2. Postbox checks the email and password by logging in to the configured IMAP server. There is
   no separate Postbox account and nothing to provision.
3. On success the user lands in the Inbox.
4. The account menu has "Sign out" (this browser) and "Sign out everywhere" (every session of
   the account).

### Passkeys

1. In Settings, a signed-in user chooses "Add a passkey" and completes the browser prompt.
   Postbox prefers a passkey that supports the WebAuthn PRF extension, which is known only after
   the prompt. A passkey without it (1Password and Bitwarden, for example) is accepted when the
   operator has set `POSTBOX_PASSKEY_KEY`. So is one whose provider reports PRF but then gives
   no output, or output that is not 32 bytes. When neither holds, nothing is saved and the user
   sees "This passkey can't be used with Postbox. Try a different passkey provider."
2. From then on, "Sign in with a passkey" signs them in with that passkey, on any device where
   it is available, without typing a password.
3. Settings lists the account's passkeys with the date each was added and a "Remove" action. A
   passkey locked by `POSTBOX_PASSKEY_KEY` says "Protected by the server's key, not the
   passkey", and adding one is logged.
4. Settings has a "Require passkey" switch with the note "This protects Postbox only. Your
   password still works in other mail apps." It can be turned on only when the account has at
   least one passkey. While it is on, password sign-in to Postbox is refused for that account.
   Turning it on ends the account's sessions that were started with a password.
5. Adding a passkey, removing one, and turning "Require passkey" on or off need the password
   entered in the same browser tab in the last 10 minutes; signing in there, with a password
   or a passkey, counts. Otherwise Settings asks "Enter your password to change passkeys."
   with one "Password" field and "Continue", then makes the change (adding a passkey starts
   again from its button). A wrong password shows "Wrong password." and counts as a failed
   sign-in attempt.

### Reading

- Desktop layout is three panes: folders, message list, reading pane. At phone width it
  collapses to one column with back navigation between the three.
- The folder pane shows an unread count beside each folder that has unread mail.
- The message list loads more conversations as the user scrolls.
- New mail appears in the list without a reload.
- A message is marked read when it is shown in the reading pane.
- HTML mail is displayed with remote images blocked. A bar above the message reads "Remote
  images are blocked." with two actions: "Load images" and "Always load from this sender".
  Images embedded in the message itself are always shown.
- Links in a message open in a new tab.
- Attachments are listed on the message and each can be downloaded.
- Dark mode follows the system setting. Settings offers "Light", "Dark", and "System".

### Conversations

- The message list groups the messages of the open folder into conversations by thread.
- A row shows the participants' names, the subject, the start of the latest message, its date,
  the number of messages when more than one, and markers for unread, flagged, and attachments.
  Rows are ordered by the date of the latest message, newest first.
- Opening a conversation shows the thread's messages from the open folder together with the
  user's own replies from Sent, oldest first. Messages of the same thread filed in other
  folders are not joined in v1.
- Archive, Delete, "Report spam", and a move act on the conversation's messages in the open
  folder. The user's replies in Sent stay in Sent.
- Marking read or unread applies to every message of the conversation in the open folder.
- Flagging applies to the latest message. A conversation shows as flagged when any of its
  messages is flagged; unflagging clears all of them.
- Reply, Reply all, and Forward act on the message they are pressed on. The `r` shortcut acts
  on the latest message.

### Organising

- "Archive" moves the conversation to the Archive folder. "Delete" moves it to Trash. Both sit
  side by side on the message and in the list.
- "Report spam" moves the conversation to Junk. Inside Junk the action is "Not spam" and moves
  it to Inbox.
- On mailstack, a move into or out of Junk trains the spam filter. "Report spam" followed by
  "Undo" therefore trains it twice, as spam and then as not spam; the second corrects the first.
- "Move to…" opens a folder picker. It does the same as dragging and is the way to move mail at
  phone width.
- After Archive, Delete, "Report spam", "Not spam", or a move, a notice with an "Undo" action is
  shown for 10 seconds.
- Inside Trash, "Delete" asks "Delete permanently? This cannot be undone." and has no undo.
- Each row has a checkbox. Shift-click selects a range. At phone width a long press on a row
  starts selection. Actions and drags apply to every selected conversation.
- Dragging one or more selected conversations onto a folder moves them there.
- Folders can be created, renamed, deleted, and nested. Dragging a folder onto another nests it.
- Dragging a folder between two others reorders it. Folder order is a Postbox preference: IMAP
  has no folder order, so other mail clients do not see it. Until a user reorders, special
  folders come first and the rest are alphabetical.
- A conversation can be flagged or unflagged, and marked read or unread.
- Search has one box. It searches sender, recipient, subject, and body across every folder
  except Junk and Trash. When the user is inside Junk or Trash, search covers that folder only.
  Results are individual messages, newest first, each showing its folder.

### Writing

- The composer supports rich text (bold, italic, underline, lists, links, block quote, inline
  images) and a plain-text mode. Rich-text mail is sent with a plain-text alternative.
- Reply, Reply all, and Forward are available on every message. Forward carries the original
  attachments. Reply all leaves out the user's own identity addresses.
- Drafts save automatically to the server's Drafts folder. Opening a message in Drafts opens it
  in the composer.
- Dragging a file from the desktop onto the composer attaches it. Dragging an image into the
  message body places it inline.
- The From field lists the user's identities. On a reply, the identity whose address appears in
  the original's To or Cc is preselected; when none matches, the login address is used.
- Recipient fields autocomplete from addresses the user has previously sent mail to (through
  Postbox, or found in the Sent folder at first sign-in), matching on the start of the display
  name or of the address.
- After "Send", a notice reads "Sending…" with an "Undo" action for 10 seconds. Undo reopens the
  composer with the message intact. When the window ends and the server accepts the message,
  the notice reads "Sent."
- A copy of every sent message is saved to the server's Sent folder.

### Identities and signatures

- Every account starts with one identity: the login address.
- In Settings the user can add identities (display name and address), edit them, and remove
  them. Postbox does not check whether the server will accept an address until a message is
  sent from it.
- Each identity has one rich-text signature. It is inserted below a new message's body and
  above the quoted text on a reply or forward. Changing the From identity in the composer
  replaces the signature with the new identity's.

### Keyboard shortcuts

| Key | Action |
| --- | --- |
| `c` | Compose |
| `r` | Reply |
| `e` | Archive |
| `#` | Delete |
| `/` | Focus search |
| `j` / `k` | Next / previous conversation |

Shortcuts are inactive while the cursor is in a text field.

### Operator

- The operator runs the container with the settings under "Configuration" and puts a reverse
  proxy that terminates TLS in front of it.
- `postbox passkeys clear <address>` removes every passkey for the account and turns "Require
  passkey" off. It prints `Cleared <n> passkeys for <address>. Require passkey is off.`

## Data and interfaces

### What Postbox requires from the mail server

- IMAP over TLS and SMTP submission over TLS, both authenticating with the user's email address
  and password.
- The server's own threading, search, and change-notification support. Postbox keeps no copy of
  mail.

### What is stored where

In the mailbox, over IMAP, visible to every other mail client:

- Messages, folders, the read and flagged state (`\Seen`, `\Flagged`).
- Drafts, in the Drafts folder.
- Sent copies, in the Sent folder.

In Postbox's SQLite file, `postbox.db` in the state directory:

- Preferences (appearance, composer mode, folder order).
- Identities and signatures.
- Collected addresses: every recipient of a message the user sent through Postbox, plus, at
  the account's first sign-in, every recipient of the messages already in its Sent folder.
  That first collection runs in the background and does not delay the Inbox. Senders of
  received mail are not collected.
- The per-sender "always load images" list.
- Passkeys, and the account password encrypted for each passkey.
- Sessions, each with the account password encrypted under a key that only the browser's
  session cookie holds.
- A record of each message in its undo-send window, and of each send that failed, so the
  failure notice can be shown later. The message itself is in Drafts, not in the database.

Deleting `postbox.db` loses settings, passkeys, and sessions. It never loses mail.

### Special folders

Postbox uses the folders the server marks with the special-use attributes `\Archive`, `\Trash`,
`\Drafts`, `\Sent`, and `\Junk`. When the server marks none for a role, Postbox uses the first
existing folder from this list:

| Role | Names tried, in order |
| --- | --- |
| Archive | "Archive", "Archives" |
| Trash | "Trash", "Deleted Items", "Deleted Messages" |
| Drafts | "Drafts" |
| Sent | "Sent", "Sent Items", "Sent Messages" |
| Junk | "Junk", "Spam", "Junk E-mail" |

When none exists, Postbox creates the first name on first use. In Postbox, special folders
cannot be renamed, deleted, or nested under another folder.

### Configuration

All new. Environment variables:

| Variable | Required | Meaning |
| --- | --- | --- |
| `POSTBOX_IMAP_ADDR` | yes | IMAP server, `host:port`. |
| `POSTBOX_SMTP_ADDR` | yes | Submission server, `host:port`. |
| `POSTBOX_PUBLIC_URL` | yes | The `https://` URL users open, with no path. Passkeys and cookies are bound to it. `http://` is accepted only when the host is `localhost` or ends in `.localhost`. |
| `POSTBOX_STATE` | no | State directory holding `postbox.db`. Default `/data/postbox`. |
| `POSTBOX_LISTEN` | no | HTTP listen address. Default `:8080`. |
| `POSTBOX_CLIENT_IP_HEADER` | no | Request header that carries the real client IP, set by the reverse proxy. Unset means the connection's remote address is used. |
| `POSTBOX_CA_FILE` | no | PEM file of extra certificate authorities trusted for the mail server. |
| `POSTBOX_PASSKEY_KEY` | no | 32 random bytes in base64 (`openssl rand -base64 32`). Lets passkeys without PRF (1Password, Bitwarden) sign in; their stored passwords are encrypted with it. Changing it breaks those passkeys until they are added again. |

Postbox exits at startup with an error naming the variable when a required one is missing or
malformed, for example `error: POSTBOX_IMAP_ADDR is required (host:port)`.

When `POSTBOX_CLIENT_IP_HEADER` is set, the header is trusted from every request, so the
listen port must be reachable only through the reverse proxy. The README states this.

### Commands and endpoints

All new:

- `postbox serve` runs the web server.
- `postbox passkeys clear <address>` as described under "Operator".
- `GET /healthz` returns `200` with body `ok` when the process is up. It does not check the mail
  server.

The browser-facing API is internal to Postbox and is not a public interface.

### What mailstack provides

- Dovecot declares the five special-use folders above.
- Dovecot records the threading headers (`Message-ID`, `References`, `In-Reply-To`, `Subject`,
  and the dates) when mail is saved, and existing mailboxes are warmed once at cutover.
  Otherwise the first time a folder is opened Dovecot reads every message in it.
- Dovecot's full-text search (flatcurve, substring matching) is already on, with Junk, Trash,
  and two other folders excluded from automatic indexing. Postbox relies on it for body search.
- Dovecot and Postfix trust the Postbox container's address to pass on the real client IP, so
  their own logs and login penalties apply to the person's address and not to Postbox's. Without
  this every Postbox user reaches the mail server from one address, and one attacker's failed
  logins slow or block everyone. Postbox passes the client IP on every IMAP and submission
  login; a server that does not trust it ignores it.
- mailstack's compose file gains an optional Postbox service. Where it is published is a
  mailstack decision; the working assumption is Caddy on the mail host, public, without
  Cloudflare Access.

## Edge cases and failure modes

Sign-in:

- Wrong email or password: "Wrong email or password." The same text is used whether or not the
  address exists.
- Five failed attempts for one address from one client IP within 15 minutes block further
  attempts for that address from that IP for 15 minutes. Blocked attempts show "Too many
  attempts. Try again in 15 minutes." and do not reach the mail server. Failed passkey
  sign-ins count the same way. A sign-out caused by a changed password does not count.
- Twenty failed attempts from one client IP within 15 minutes, across all addresses, block
  every sign-in attempt from that IP for 15 minutes, with the same message.
- Password sign-in while "Require passkey" is on: the password is checked first. A wrong
  password shows "Wrong email or password." A correct one shows "This account requires a
  passkey."
- A passkey sign-in where a PRF passkey returns no PRF result, or a passkey without PRF meets a
  Postbox whose `POSTBOX_PASSKEY_KEY` is unset or changed, shows "This passkey can't be used
  here. Sign in with your password."

Sessions:

- A session started with a passkey, or with "Keep me signed in" ticked, lasts 30 days. A
  password session without the tick uses a cookie that ends with the browser session and is
  also refused by the server after 24 hours.
- Sessions survive a Postbox restart.
- Postbox tells "the mail server refused this password" apart from "the mail server is
  unavailable". Only the first signs the user out.
- When the mail server refuses the session's password, the next action signs the user out to
  the login screen with "Your password has changed. Sign in again."
- A passkey outlives a password change. When a passkey sign-in finds that the mail server
  refuses the password stored for that passkey, Postbox asks "Your password has changed. Enter
  your new password." with one "Password" field. A correct password completes the sign-in and
  is stored for that passkey in place of the old one. A wrong one shows "Wrong email or
  password." and counts as a failed attempt. Each of the account's other passkeys asks the same
  the next time it is used. This applies whether "Require passkey" is on or off.

Passkeys:

- A lost passkey: the user signs in with the password and removes it in Settings. When "Require
  passkey" is on and no passkey is available, the operator runs `postbox passkeys clear`.
- Removing the last passkey turns "Require passkey" off.
- A copy of `postbox.db` on its own, without a user's browser cookie or passkey, reveals no
  account password. No key that decrypts a stored password is kept in the database or on disk.
  The one exception is `POSTBOX_PASSKEY_KEY`, kept in the configuration: with it and a copy of
  `postbox.db`, the passwords stored for passkeys without PRF can be read.

Mail server:

- When the IMAP server is unreachable, a banner reads "Can't reach the mail server. Retrying…".
  Content already on screen stays readable. Actions that change mail are disabled. The banner
  clears and actions return without a reload when the server is back.
- An invalid or untrusted mail-server certificate is a hard failure. Postbox has no setting to
  skip certificate checks and no plaintext mode. Sign-in shows "Can't reach the mail server."
  and the reason is logged.
- Postbox stays under Dovecot's default limit of 10 IMAP connections per user, however many
  tabs that user has open.

Sending:

- Pressing "Send" saves the message to Drafts before the undo window starts.
- When the server rejects the message (From address not permitted, recipient refused, message
  too large), the composer reopens with the draft intact and the server's own message shown
  above it, prefixed "The mail server rejected this message:".
- Postbox reads the submission server's advertised size limit and compares it with the
  message's encoded size, including forwarded attachments. A file that would push the message
  over it is refused when added, with "This file is too large. The mail server accepts messages
  up to <limit>."
- Closing the tab or losing the connection during the 10-second undo window does not cancel the
  send.
- When the send fails after the user has left the composer, the message stays in Drafts and the
  next page the user opens shows "A message could not be sent and is in Drafts:" followed by the
  server's message.
- When Postbox restarts during an undo window, the message is not sent and stays in Drafts. The
  next page the user opens shows "A message was not sent and is in Drafts."
- When Postbox stops after handing a message to the mail server and before recording the
  server's answer, it cannot tell whether the message went out. The message stays in Drafts and
  the next page the user opens shows "A message may have been sent. Check Sent before sending
  it again."
- When the server accepts the message but saving the copy to Sent fails, the message is not
  sent again. The notice reads "Sent, but a copy could not be saved to Sent."

Organising:

- Deleting a folder deletes its subfolders with it. The confirmation reads "Delete “<name>” and
  the <n> messages in it? This cannot be undone." or, with subfolders, "Delete “<name>”, its
  <k> subfolders, and the <n> messages in them? This cannot be undone." Both offer "Delete" and
  "Cancel".
- Creating or renaming a folder to a name that exists shows "A folder named “<name>” already
  exists."
- When another client moves or deletes the message being read, it stays on screen until the
  user navigates away, and disappears from the list.
- Undo after the 10-second notice has gone is not offered.

Reading:

- HTML mail never runs scripts and never submits forms.
- While images are blocked for a message, displaying it causes no network request of any kind:
  not for images, fonts, stylesheets, frames, CSS `url()` references, prefetch hints, or link
  pings, and not to Postbox's own address either. Only parts embedded in the message are shown.
- "Load images" and "Always load from this sender" allow remote images only. Remote fonts,
  stylesheets, frames, and scripts are never loaded.
- Links open without passing the Postbox address or a handle on the Postbox tab to the target.
- The message body is shown in a frame that shares the app's address, with scripts switched
  off, so the app can size the frame to the message's height.
- "Always load from this sender" applies to the exact From address, not the domain. The From
  address can be forged; this is accepted for v1.
- A message with no plain or HTML body shows "This message has no content." with its
  attachments.

## Constraints

- One Go binary with the frontend embedded. One container image. No runtime dependency other
  than the mail server.
- Frontend: a single-page app in Svelte and TypeScript.
- Plain HTTP on `POSTBOX_LISTEN`; TLS is the reverse proxy's job. Cookies are `Secure`,
  `HttpOnly`, and `SameSite=Lax` or stricter.
- TLS to the mail server is mandatory, with certificate verification.
- Passkeys: the account password is stored encrypted under a key derived from the passkey
  through the WebAuthn PRF extension. For a passkey without PRF it is encrypted under a key
  derived from `POSTBOX_PASSKEY_KEY`; without that setting such passkeys are refused.
- Sessions: the account password is stored encrypted under a key held only in the browser's
  session cookie.
- Browsers: the current and previous major versions of Safari (macOS and iOS), Chrome, Firefox,
  and Edge.
- English only. User-facing text is kept separable from code so translations can be added.
- Repository conventions match mailstack: `just` recipes, `just check` as the CI gate.
- Licence: AGPL-3.0.
- Scale: about 20 accounts, a handful signed in at once, mailboxes up to 10 GB.

Testing:

- Go unit tests for message parsing, HTML sanitising, session and passkey handling, and
  throttling.
- An integration suite that runs Postbox against real Dovecot and Postfix containers.
- Browser end-to-end tests in Chromium, using its virtual authenticator for passkeys and a test
  clock for expiry. Safari and Firefox are checked by hand before a release.
- A corpus of hostile HTML messages kept in the repository.
- `just check` runs all four.

## Acceptance criteria

Criteria 1–27 are automated and pass under `just check`. Criterion 28 is a review step.
Criteria 29–32 are checked by hand.

Signing in:

1. Given a valid account, when the user submits "Email" and "Password", then the Inbox opens.
   Given a wrong password, the screen shows "Wrong email or password." The login screen has no
   server field.
2. Given five failed attempts for one address from one IP within 15 minutes, when a sixth is
   made with the correct password, then it shows "Too many attempts. Try again in 15 minutes."
   and the IMAP server logs no sixth login. Given twenty failed attempts from one IP spread
   over twenty different addresses within 15 minutes, when a sign-in for a twenty-first address
   is made from that IP with the correct password, then it shows the same message.
3. Given a signed-in user who adds a passkey, when they sign out and choose "Sign in with a
   passkey", then the Inbox opens without a password being typed. Given a virtual authenticator
   without PRF and `POSTBOX_PASSKEY_KEY` set, the same holds. Given a virtual authenticator
   without PRF and no `POSTBOX_PASSKEY_KEY`, "Add a passkey" shows "This passkey can't be used
   with Postbox. Try a different passkey provider." and Settings lists no passkey.
4. Given an account with a passkey and an open session, when `postbox.db` is searched for the
   account password as plain text, base64, and hex, then nothing is found.
5. Given "Require passkey" is on, when the correct password is submitted, then the screen shows
   "This account requires a passkey."; when a wrong password is submitted, it shows "Wrong
   email or password." After `postbox passkeys clear <address>` prints
   `Cleared 1 passkeys for <address>. Require passkey is off.`, password sign-in works.
6. Given a signed-in user whose password is changed on the mail server, when they open a
   folder, then the login screen shows "Your password has changed. Sign in again." Given
   Dovecot is stopped instead, the user stays signed in. Given the same account with a passkey
   and "Require passkey" on, when they choose "Sign in with a passkey", then "Your password has
   changed. Enter your new password." shows; after the new password is entered the Inbox
   opens, and a later passkey sign-in opens the Inbox without asking.
7. Given a passkey session, when the test clock advances 30 days, then the next request shows
   the login screen. Given a password session without "Keep me signed in", its cookie has no
   expiry date, and when the test clock advances 24 hours the next request shows the login
   screen. Given a session, when Postbox restarts, then the user is still signed in.
8. Given two sessions for one account, when "Sign out everywhere" is pressed in one, then the
   next request in the other shows the login screen.

Reading:

9. At a 1280-pixel-wide viewport the folder list, message list, and reading pane are visible
   together. At a 390-pixel-wide viewport only one of them is visible at a time.
10. Given a thread with a received message in Inbox and the user's reply in Sent, when the
    conversation is opened from Inbox, then both messages are shown, oldest first.
11. Given the Inbox is open, when a message is delivered, then it appears in the list without a
    reload, and the Inbox unread count rises by one. When it is opened, a second IMAP client
    sees it with `\Seen` set.
12. Given an HTML message with a remote image and an embedded image, when it is opened, then
    the bar "Remote images are blocked." shows, the embedded image is displayed, and the
    browser makes no request to the remote image's host. After "Always load from this sender",
    a second message from that address loads its image without asking.
13. For every message in the hostile HTML corpus, opening it runs no script and causes no
    network request other than those the Postbox app makes to fetch the message and its
    embedded parts.
14. Given a message whose body contains a unique word, filed in a folder other than Inbox,
    Junk, or Trash, when that word is searched from Inbox, then the message is found and its
    folder is shown. Given the same in Trash, it is not found from Inbox and is found when
    searching inside Trash.

Organising:

15. Given a conversation in Inbox, when "Archive" is pressed, then a second IMAP client sees it
    in the Archive folder and not in Inbox. When "Undo" is pressed within 10 seconds, it is back
    in Inbox. The same holds for "Delete" and Trash, and for "Report spam" and Junk. Given a
    conversation with a reply in Sent, after "Archive" the reply is still in Sent.
16. Given a message in Trash, when "Delete" is pressed and "Delete permanently? This cannot be
    undone." is confirmed, then a second IMAP client no longer sees it in any folder.
17. Given two selected conversations, when they are dragged onto a folder, then a second IMAP
    client sees both in that folder. The same result follows from "Move to…". Given a folder
    dragged onto another, a second IMAP client sees it nested.
18. A folder can be created, renamed, and deleted from the folder pane. Given a folder with one
    subfolder, deleting it shows the subfolder confirmation text and a second IMAP client sees
    neither folder afterwards. Given a folder dragged between two others, the new order is
    still shown after signing out and in again.
19. Given a message flagged and marked unread in Postbox, then a second IMAP client sees it
    with `\Flagged` set and `\Seen` unset.
20. Given a mail server that marks no special-use folders and has a folder named "Sent Items",
    when a message is sent, then the copy is in "Sent Items" and no folder named "Sent" exists.

Writing:

21. Given a rich-text message typed in the composer, when the composer is closed, then a second
    IMAP client sees it in Drafts, and opening it from Drafts reopens the composer. When it is
    sent, the recipient receives a message with both HTML and plain-text parts, a second IMAP
    client sees a copy in Sent, and Drafts no longer holds it.
22. Given a file dragged onto the composer, then it is listed as an attachment and arrives with
    the message. Given an image dragged into the body, it arrives inline. Given a file larger
    than the server's limit, then "This file is too large. The mail server accepts messages up
    to <limit>." shows and nothing is attached.
23. Given an identity with a signature, when a new message is started from it, then the
    signature is in the body, and switching From to another identity replaces it. Given an
    identity the server does not permit, when a message is sent from it, then the composer
    reopens showing "The mail server rejected this message:" with the server's text, and the
    draft is intact.
24. Given "Send" is pressed, when "Undo" is pressed within 10 seconds, then no message is
    delivered and the composer reopens. Given "Send" is pressed and the tab is closed at once,
    then the message is delivered. Given "Send" is pressed and Postbox is restarted within the
    window, then no message is delivered, it is in Drafts, and the next page shows "A message
    was not sent and is in Drafts."
25. Given "Send" is pressed for a message the server will reject and the tab is closed at once,
    when the user opens Postbox again, then the page shows "A message could not be sent and is
    in Drafts:" with the server's text.
26. Given the user has sent a message to an address, when they type the first three characters
    of its display name or of the address in a recipient field, then the address is offered.
    Given an account whose Sent folder already holds a message to an address, when the user
    signs in to Postbox for the first time and the background collection has finished, then
    that address is offered the same way.
27. Each shortcut in the keyboard table performs its action, and none fires while typing in the
    composer or search box.

Running it:

28. Given only `POSTBOX_IMAP_ADDR`, `POSTBOX_SMTP_ADDR`, and `POSTBOX_PUBLIC_URL` set, then
    `postbox serve` starts and `GET /healthz` returns `200` `ok`. Given `POSTBOX_IMAP_ADDR`
    unset, it exits non-zero with `error: POSTBOX_IMAP_ADDR is required (host:port)`. Given a
    mail server with a certificate from an unknown authority and no `POSTBOX_CA_FILE`, sign-in
    shows "Can't reach the mail server." Given Dovecot is stopped while a user is signed in, the
    banner "Can't reach the mail server. Retrying…" shows, and it clears without a reload after
    Dovecot starts. Given `postbox.db` is deleted, the user's mail is unchanged after signing in
    again. (These are automated. The review step: a person reads the storage code, following
    `docs/storage-review.md`, and confirms that no key able to decrypt a stored password is
    written to the database or to disk, and that the only one read from configuration is
    `POSTBOX_PASSKEY_KEY`, used only for passkeys without PRF.)

Speed, by hand. Conditions: the largest mailstack mailbox (about 10 GB), desktop Chrome on a
home broadband connection, timed from the click to content on screen with the browser's
performance panel, median of five tries.

29. Opening the Inbox and opening a message each show content within 1 second.
30. Archive, Delete, flag, and move change the list within 100 milliseconds, before the server
    confirms.
31. A body search from Inbox returns results within 2 seconds.
32. A message sent to the account from another client appears in the open Inbox within 5
    seconds of the mail server logging its delivery.

## Decisions

| Decision | Choice | Why | Decided by |
| --- | --- | --- | --- |
| Standalone or part of mailstack | Standalone repo and image; optional for mailstack | Needs only IMAP and submission; usable as a Roundcube replacement | user |
| Priority between audiences | mailstack users first; Dovecot and Postfix tested, others best-effort | Keeps scope small | user |
| Mail storage | Stateless over IMAP plus a small SQLite file | Simple to run; cache can come later | user |
| v1 scope | Mail only, plus addresses collected from sent mail | Calendar and contacts deferred | user |
| Folders, flags, signatures | In v1 | Required by the user | user |
| Sieve and vacation replies | Out for good | Not used | user |
| Sign-in | Password, plus passkeys in v1 | Secure from the first release | user |
| Second factor | "Require passkey" switch; no authenticator codes | Less to build than codes; guards Postbox only, stated in the UI | agent default |
| Passkeys without PRF | Accepted when `POSTBOX_PASSKEY_KEY` is set, with the password encrypted under a key derived from it; refused otherwise | 1Password and Bitwarden must work as passkey providers. The database alone still reveals nothing; the database and the server's configuration together reveal those passwords | user |
| PRF claimed but not given | Treated as a passkey without PRF, not refused | 1Password's extension reports PRF and returns out-of-spec output (its FS-5593); refusing made it unusable. Such a passkey gets the server key's protection, not PRF's | user |
| Enrolment with a provider that reports no PRF support | Saved straight away under the server key, with no second prompt | Asking again cannot produce a key such a provider lacks | agent default |
| Removing a passkey, or `postbox passkeys clear` | Also ends the account's passkey sessions, other than the one that asked | A passkey is removed because it was lost; its sessions should not outlive it. Sessions do not record which passkey started them, so all passkey sessions go | agent default |
| Failed sign-ins with a passkey Postbox does not know | Count against that passkey and the client IP's limit, not an account | One removed passkey retried must not block every passkey sign-in from the address | agent default |
| Changing passkeys or "Require passkey" | Needs the password entered in the same tab in the last 10 minutes (a sign-in counts), proved by a token the tab keeps in session storage and sends with the change; checked against the session's own password, without asking the mail server, and wrong attempts are logged | A stolen session cookie must not be able to add its own passkey or lock the owner out, even while the owner has just confirmed. A passkey session confirms against the password unsealed at its sign-in, so a password changed elsewhere is refused until the next sign-in | user (the 10 minutes, the tab token and password-only: agent default) |
| Lost passkey | Remove after password sign-in; operator CLI clears when "Require passkey" is on | Simple recovery path | agent default |
| Password change | Ends password sessions; a passkey asks for the new password once and is kept | No lockout when "Require passkey" is on | user |
| Accounts | One account per login, several identities | Unified inbox deferred | user |
| Identities | User-defined; the server's rejection is shown at send time | No coupling to `mailctl` | user |
| Conversations | Gmail style, including the user's replies from Sent | Shows both sides of a thread | user |
| Conversation reach | Open folder plus Sent; other folders not joined | IMAP threads one folder at a time; joining every folder without a cache cannot meet the speed targets | agent default |
| Conversation actions | Act on the open folder's messages; Sent copies stay; flag the latest message | Predictable for users of other mail clients | agent default |
| Archive and Delete | Archive folder and Trash; no "All Mail" | Agrees with users' other mail clients | user |
| Report spam / Not spam | In v1 | A mail client without it strands junk in Inbox | agent default |
| Delete inside Trash | Permanent, confirmed, no undo | Standard behaviour | agent default |
| Special folders when the server marks none | Fall back to common names; create when missing | Works on servers that omit special-use | agent default |
| Drag and drop | Messages to folders, files to composer, images inline, folders to nest and reorder | The user's main complaint about existing clients | user |
| Folder order | Stored in Postbox; not visible to other clients | IMAP has no folder order | agent default |
| Folder names typed in Postbox | No `/`, `.`, `\`, `%`, `*`, `"` or control characters | Servers use them to separate or match folder names | agent default |
| Undo notice wording | "Conversation archived.", "Conversation moved to Trash.", "Conversation reported as spam.", "Conversation moved to Inbox.", "Conversation moved to <folder>." ("<n> conversations …" for several) | The spec named only the "Undo" action | agent default |
| Undo notice timing | Ten seconds, paused while the pointer or keyboard focus is on it | Reaching for "Undo" must not be a race (WCAG 2.2.1) | agent default |
| Undo for "Send" | Ten seconds on the server's clock, not paused; the Undo goes when the window ends, and "Sending…" stays until the outcome | The send goes ahead with the tab closed, so the window cannot wait on the page | agent default |
| A draft opened during its send | Not opened; "This message is being sent. Use Undo to change it." | Saving it would replace the copy being sent | agent default |
| Submission server briefly unreachable | One more try two seconds later, if none of the message had gone; then "could not be reached" and the message stays in Drafts | Rides out a restart or a dropped connection without risking a double send | agent default |
| Quoted text in a rich-text reply or forward | The original's text, as a block quote | The original's HTML can carry remote images and styles the editor cannot hold; its text is safe to edit | agent default |
| Images in composed mail | Only images that travel with the message (cid:); a pasted remote image is dropped | A message must not make its reader fetch something the sender chose | agent default |
| Images in signatures | Not supported; signatures hold text, formatting and links | An image would have to travel with every message as a part | agent default |
| Embedded images of a forwarded message | Attached as files | The forward quotes the original's text, which does not show them | agent default |
| Identity chosen for a reply | The one in the original's To, then Cc, then the login address | The address the mail was sent to is the one to answer from | agent default |
| A paragraph in the plain-text part of rich mail | One line | The editor makes a paragraph per press of Enter | agent default |
| Where Settings lives | A dialog from the account menu | Settings are few; no page of their own is needed | agent default |
| HTML part of a plain-text message | Made from the text: escaped, line breaks kept, links and quoted lines marked | Every message goes out with both parts, as criterion 21 asks, before the rich-text editor exists | agent default |
| Reply to one's own message | Goes to the original recipients, or back to oneself when there were none | Matches other mail clients | agent default |
| Submission port | 465 is TLS from the start; any other port must offer STARTTLS, which is used | No plaintext submission | agent default |
| Permanent delete | Only in the Trash folder, checked by the server | A request for it anywhere else is refused, so a mistake cannot wipe Inbox | agent default |
| "Move to…" action | In v1 | Dragging is not possible at phone width | agent default |
| Flags | Read/unread and one flag | Coloured flags deferred | user |
| One mail server per deployment | Yes; no server field | Simpler login | user |
| Collected addresses | Recipients of sent mail only, seeded from the existing Sent folder at first sign-in | Avoids collecting spam senders; autocomplete is not empty for users arriving from mailcow | user |
| Search scope | All folders except Junk and Trash; those only from inside them; results are messages | Matches what mailstack indexes automatically | agent default |
| Session length | 30 days with a passkey or "Keep me signed in"; otherwise until the browser closes, capped at 24 hours | Low friction on trusted devices | user (the "Keep me signed in" length and the 24-hour cap: agent default) |
| Sessions survive a restart | Yes, with the password key held in the cookie | A deploy must not sign everyone out, and the database alone must reveal nothing | agent default |
| Remote images | Blocked; per-message load and per-sender allow list by From address | No proxy in v1; a forged From is an accepted risk | user |
| Undo send | 10 seconds, held by the server; a restart in the window leaves the message in Drafts | A closed tab must not lose mail | agent default |
| Undo for Archive, Delete, spam, move | 10 seconds | Same window as send | agent default |
| Send failures and oversized files | Composer reopens with the server's message; oversized files refused when added | No lost drafts, no wasted uploads | agent default |
| Mail server unreachable | Banner, read-only, no offline mode | Stateless design | agent default |
| Login throttling | 5 failures in 15 minutes blocks that address from that IP for 15 minutes | Slows guessing without locking out the real user elsewhere | agent default |
| Login throttling per IP | 20 failures from one IP across all addresses in 15 minutes blocks that IP for 15 minutes | One IP guessing across many addresses is slowed too | user |
| "IP" for an IPv6 client | Its /64 network | One ordinary IPv6 connection is given a whole /64; counting single addresses would let one person make every attempt from a fresh address | agent default |
| Session cookie on a local `http://` address | Not `Secure` and without the `__Host-` prefix; both apply whenever `POSTBOX_PUBLIC_URL` is `https://` | Safari does not keep `Secure` cookies on `http://localhost`, which would break local runs; nothing but localhost names may use `http://` | agent default |
| When a password changed elsewhere is noticed | At the next action that needs a fresh IMAP login; Postbox logs in again at least every 10 minutes per account | Dovecot keeps logged-in connections working after a password change, and logging in on every action would slow every action | agent default |
| Real client IP passed to the mail server | Yes, when the server trusts Postbox | One attacker must not get every user blocked | agent default |
| Speed targets | 1 s open, 2 s search, 5 s new mail | "A user should not be waiting" | user |
| "Instant" for actions | Within 100 milliseconds | Makes the target measurable | agent default |
| Keyboard shortcuts | `c`, `r`, `e`, `#`, `/`, `j`, `k` | Familiar from Gmail | user |
| Frontend | Svelte and TypeScript single-page app | Small bundle, fast feel | agent default |
| Browsers | Current and previous major Safari, Chrome, Firefox, Edge | | user |
| Exposure | Plain HTTP behind any reverse proxy; client-IP header configurable | Publishing is the deployer's decision | agent default |
| `POSTBOX_PUBLIC_URL` required | Yes | Passkeys and cookies are bound to the site's address | agent default |
| Password change inside Postbox | Deferred | Not requested | agent default |
| Name | Postbox | A discontinued commercial mail client used the same name; rename before a public release if that matters | user |
| Licence | AGPL-3.0 | Open for self-hosting; commercial licences remain possible | user |
| Spec location | `docs/specs/postbox.md` in the Postbox repository | Moved from mailstack when the repository was created | agent default |
| Testing | Unit, integration against Dovecot and Postfix, browser end-to-end in Chromium, hostile HTML corpus | | user |
| `http://` in `POSTBOX_PUBLIC_URL` | Accepted for `localhost` and names under `.localhost` only | Browsers treat those as secure, so local runs and CI need no certificate | agent default |
| A stop between handing over a message and recording the answer | The message stays in Drafts with the notice "A message may have been sent. Check Sent before sending it again." | Postbox cannot know which happened; a silent resend would duplicate mail | agent default |
| "Report spam" then "Undo" on mailstack | Trains the spam filter twice | The second training corrects the first; avoiding it would mean delaying the move | agent default |
| How the message body is isolated | A frame on the app's own address with scripts off | The app can read the frame's height; a frame on a foreign address could not be sized to fit | agent default |
| Remote images once allowed | `http:` and `https:` both load | Mail still links images over plain http; an https Postbox has the browser upgrade or block them as mixed content | agent default |
| Where the blocked-image count comes from | The body document carries it in a `meta` element that the app reads from the frame | The app learns it without fetching and rendering each body twice | agent default |
| How the client IP reaches the IMAP server | Postbox sends the `ID` command with `x-originating-ip` itself, before handing the connection to the IMAP library | The library's `ID` cannot carry that field; a PROXY-protocol listener would need extra ports on every mail server | user |
| Threading headers on the mail server | Dovecot records them when mail is saved (`mail_always_cache_fields`), and existing mailboxes are warmed once at cutover | Measured: without it the first open of a 90,000-message folder took 6.4 minutes; with it, well under a second | agent default |
| How a search query matches | A message matches when every word appears in its sender, To, Cc, Bcc, subject or body; a phrase in double quotes counts as one word | What people expect of a single search box; the server's own search does the matching | agent default |
| Search wording | The results are headed "Results for “<query>”", each shows its folder's name, an empty search shows "No messages match.", and "Close search" returns to the folder | The spec quoted no text for search | agent default |
| Opening a search result | Opens the conversation that holds the message, in the message's folder; a result in Drafts opens the composer | The reading pane shows conversations everywhere else | agent default |
| What `r` replies to | The latest message of the open conversation that is in the open folder, so the person's own reply in Sent is not answered | Replying to one's own sent copy would address the wrong people | agent default |
| When shortcuts are inactive | While typing in any field or the composer's body, and while a composer, dialog or menu is open | A key in an open composer is text; a dialog has its own keys | agent default |
| `e` and `#` with conversations ticked | They act on the ticked conversations; with none ticked, on the open one | As the action bar does | agent default |
| Passkey enrolment when creation gives no PRF output | Settings shows "Use the new passkey once more to finish adding it." with a "Finish" button, which asks the passkey for its PRF output | Some browsers allow one passkey prompt per click | agent default |
| Turning "Require passkey" on | Ends the account's other password sessions; the session that turned it on stays | Signing the person out in the middle of Settings would surprise them, and they have just shown they hold the account | agent default |
| A passkey that does not verify, is not registered, or took longer than two minutes | "That passkey didn't work. Try again." | The spec quotes no text for it | agent default |
| Passkey wording in Settings | Each passkey is listed as "Added <date>"; its Remove button is named "Remove the passkey added <date>"; a failed enrolment for any other reason says "Couldn't add the passkey. Try again." | The spec quotes no text for these | agent default |

## Open questions

1. Which two folders, besides Junk and Trash, mailstack excludes from automatic indexing. When
   they are folders users search, the first search in them is slower than criterion 31 allows.
   Owner: Kevin, from the mailstack Dovecot configuration.
2. Do any mailcow users sync contacts or calendars through SOGo today? Postbox v1 does not
   replace that. Owner: Kevin, as a mailstack cutover question.
3. The size of the fixture mailbox used by the integration suite, and whether any speed
   criterion is automated against it. Owner: whoever writes the plan, confirmed by Kevin.
4. Whether the name "Postbox" stays for a public release. Owner: Kevin.

## Existing code touched

Postbox itself is a new repository; nothing in mailstack's Go code changes.

- mailstack Dovecot configuration (not yet in this repository): extended to declare the
  `\Archive`, `\Trash`, `\Drafts`, `\Sent`, and `\Junk` special-use folders and to trust the
  Postbox container's address for client-IP forwarding.
- mailstack Postfix configuration (not yet in this repository): extended to trust the Postbox
  container's address for client-IP forwarding on submission.
- mailstack compose files: extended with an optional Postbox service.
- `cmd/mailctl/`, `internal/`, `store.db`, and the generated lookup files: left alone.
