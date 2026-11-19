<script lang="ts">
  import {
    composer as fetchComposer,
    dismissNotice,
    notices as fetchNotices,
    sendStatus,
    undoSend,
    type ComposeMode,
    type ComposerView,
    type SendStatus,
    conversations as fetchConversations,
    createFolder,
    deleteFolder,
    expunge,
    folders as fetchFolders,
    move,
    renameFolder,
    saveFolderOrder,
    setFlag,
    signOut,
    type Account,
    type Conversation,
    type Destination,
    type ErrorCode,
    type Folder,
    type Message,
  } from './api';
  import { untrack } from 'svelte';
  import { SvelteMap, SvelteSet } from 'svelte/reactivity';
  import ActionBar, { type Action } from './ActionBar.svelte';
  import Composer from './Composer.svelte';
  import ConversationList from './ConversationList.svelte';
  import ConversationView from './ConversationView.svelte';
  import Dialog from './Dialog.svelte';
  import FolderList from './FolderList.svelte';
  import UndoNotice, { type Notice } from './UndoNotice.svelte';
  import { closeConversation, openConversation, openFolder, route } from './route.svelte';
  import { strings } from './strings';

  interface Props {
    account: Account;
    onSignedOut: (reason?: 'password_changed') => void;
  }
  const { account, onSignedOut }: Props = $props();

  let menuOpen = $state(false);
  let signOutFailed = $state(false);
  // Why the folder list and the conversation list last failed to load, each cleared by its own
  // next success, so one succeeding does not hide the other still failing.
  type Failure = 'unreachable' | 'internal' | null;
  let foldersFailure = $state<Failure>(null);
  let listFailure = $state<Failure>(null);
  // Set by the event stream while Postbox cannot reach the mail server.
  let pushDown = $state(false);
  const failure = $derived<Failure>(pushDown ? 'unreachable' : (foldersFailure ?? listFailure));
  // At phone width only one pane shows; this one is for the folder list.
  let showFolders = $state(false);
  const view = $derived(route.conversation ? 'reading' : showFolders ? 'folders' : 'list');
  let folderMissing = $state(false);

  let folderList = $state<Folder[]>([]);
  let rows = $state<Conversation[]>([]);
  let next = $state<number | undefined>(undefined);
  let loading = $state(false);
  // False until the open folder's first page has arrived, so a failed load never reads as empty.
  let listLoaded = $state(false);
  // Bumped whenever the open folder changes, so answers for the old one are dropped.
  let generation = 0;

  // --- writing ---
  // problem: why the last attempt to send this message failed.
  let composing = $state<{ view: ComposerView; problem?: string } | null>(null);
  // Outcomes of earlier sends the person has not been told, from before this page loaded.
  let pageNotices = $state<SendStatus[]>([]);

  async function compose(mode: ComposeMode, folder?: string, uid?: number, problem?: string): Promise<void> {
    const result = await fetchComposer(mode, folder, uid);
    if (!result.ok) {
      if (result.error === 'sending') notice = { text: strings.compose.inWindow };
      else failed(result.error);
      return;
    }
    composing = { view: result.value, problem };
  }

  function composerClosed(): void {
    composing = null;
    if (currentFolder?.role === 'drafts') refreshList();
    loadFolders(true);
  }

  // After "Send": "Sending…" with Undo for the window, then what became of it. A failure reopens
  // the composer saying why. The window runs on the server and does not pause, so the Undo goes
  // when it ends even while the pointer rests on the notice.
  function sent(id: string): void {
    composing = null;
    const sending: Notice = {
      text: strings.compose.sending,
      undo: async () => {
        const back = await undoSend(id);
        if (back.ok) {
          undone = true;
          if (notice === mine) notice = null;
          void compose('draft', back.value.folder, back.value.uid);
        } else show(back.error === 'too_late' ? { text: strings.compose.sending } : { text: strings.actions.failed });
      },
    };
    // This send's latest notice, as the state holds it (a proxy, not the object assigned); a
    // newer one for something else is left alone.
    let mine: Notice | null = null;
    let undone = false;
    const show = (n: Notice): void => {
      if (undone || (notice !== mine && notice !== null)) return;
      notice = n;
      mine = notice;
    };
    notice = sending;
    mine = notice;
    setTimeout(() => show({ text: strings.compose.sending }), 9_000);
    void watchSend(id, show);
  }

  // Nothing changes before the ten-second undo window ends, so the first look waits it out.
  async function watchSend(id: string, show: (n: Notice) => void): Promise<void> {
    for (let tries = 0; tries < 600; tries++) {
      await new Promise((resolve) => setTimeout(resolve, tries === 0 ? 10_000 : 1000));
      const status = await sendStatus(id);
      if (!status.ok) return; // undone, or told elsewhere
      const s = status.value;
      if (s.state === 'waiting' || s.state === 'submitting') continue;
      if (s.state === 'failed') void compose('draft', s.draftsFolder, s.draftUid, failureText(s, strings.compose.rejected));
      else show({ text: noticeText(s) });
      void dismissNotice(id);
      loadFolders(true);
      if (currentFolder?.role === 'sent' || currentFolder?.role === 'drafts') refreshList();
      return;
    }
  }

  // Why a send failed: the server's words after prefix when it refused the message, else ours.
  function failureText(n: SendStatus, prefix: string): string {
    if (n.reason === 'rejected') return `${prefix} ${n.error ?? ''}`;
    return n.reason ? strings.compose.failure[n.reason] : strings.actions.failed;
  }

  function noticeText(n: SendStatus): string {
    switch (n.state) {
      case 'failed':
        return n.reason === 'rejected' ? failureText(n, strings.compose.failedLater) : `${strings.compose.failedLater} ${failureText(n, '')}`;
      case 'unsent':
        return strings.compose.unsent;
      case 'unknown':
        return strings.compose.maybeSent;
      case 'sent':
        return strings.compose.sent;
      default:
        return strings.compose.sentNoCopy;
    }
  }

  $effect(() => {
    fetchNotices().then((result) => {
      if (result.ok) pageNotices = result.value;
    });
  });

  // Read and flag changes made here, kept until a list refresh that began after the server had
  // them, so an older answer cannot undo them on screen.
  const pendingFlags = new SvelteMap<number, { patch: Partial<Conversation>; done?: number }>();
  // The open conversation's messages, as its view loaded them, with which conversation they are.
  let openLoaded = $state<{ folder: string; id: number; messages: Message[] } | null>(null);
  // Conversations moved away by an action here. They stay hidden even if a refresh that started
  // before the move brings them back.
  const hidden = new SvelteSet<number>();
  const shownRows = $derived(rows.filter((r) => !hidden.has(r.id)));
  // Ticked conversations, for actions and drags.
  let checked = $state<number[]>([]);
  let notice = $state<Notice | null>(null);
  // An action waiting on the person: a folder to move to, or a confirmation.
  let asking = $state<
    | { kind: 'moveTo'; ids: number[] }
    | { kind: 'expunge'; ids: number[]; uids: number[] }
    | { kind: 'deleteFolder'; folder: Folder; text: string }
    | { kind: 'moveFolder'; folder: Folder }
    | null
  >(null);

  // The open conversation's row: when a refresh changes it (a reply arrived, a message
  // went), the conversation is fetched again.
  const openRow = $derived(rows.find((r) => r.id === route.conversation));
  const openRowState = $derived(openRow ? `${openRow.date} ${openRow.count}` : '');
  // What actions on the open conversation act on: its messages in this folder.
  const openConversationRow = $derived.by((): Conversation | undefined => {
    const id = route.conversation;
    // Only messages loaded for this very conversation count: acting on another's would move the
    // wrong mail.
    if (!id || openLoaded?.id !== id || openLoaded.folder !== route.folder) return openRow;
    const mine = openLoaded.messages.filter((m) => m.folder === route.folder);
    if (mine.length === 0) return openRow;
    const latest = mine.at(-1)!;
    return {
      id,
      uids: mine.map((m) => m.uid),
      subject: mine[0]!.subject,
      participants: mine.flatMap((m) => m.from),
      snippet: openRow?.snippet ?? '',
      date: latest.date,
      count: mine.length,
      unread: mine.some((m) => m.unread),
      flagged: mine.some((m) => m.flagged),
      attachments: mine.some((m) => m.attachments.length > 0),
    };
  });
  const currentFolder = $derived(folderList.find((f) => f.name === route.folder));

  // Deals with a failed load and returns what to show for it, if anything.
  function handle(error: ErrorCode): Failure {
    if (error === 'password_changed') onSignedOut('password_changed');
    else if (error === 'signed_out') onSignedOut();
    else if (error === 'no_folder') folderMissing = true;
    else return error === 'unreachable' ? 'unreachable' : 'internal';
    return null;
  }

  // One folder refresh at a time, and none within ten seconds of the last, however often the
  // tab gains focus: each one takes one of the account's two mail-server connections.
  let foldersInFlight = false;
  let foldersAt = 0;

  // A forced refresh asked for while one is in flight runs once that one ends, and the caller
  // waits for it: an answer that left before a change must not stand for the state after it.
  let foldersRunning: Promise<void> = Promise.resolve();
  let foldersAgain = false;

  function loadFolders(force = false): Promise<void> {
    if (foldersInFlight) {
      if (force) foldersAgain = true;
      return foldersRunning.then(() => (foldersAgain ? loadFolders(true) : undefined));
    }
    if (!force && Date.now() - foldersAt < 10_000) return Promise.resolve();
    foldersInFlight = true;
    foldersAgain = false;
    foldersRunning = (async () => {
      const result = await fetchFolders();
      foldersInFlight = false;
      foldersAt = Date.now();
      foldersFailure = result.ok ? null : handle(result.error);
      if (result.ok) folderList = result.value;
    })();
    return foldersRunning;
  }

  async function loadPage(first: boolean): Promise<void> {
    if (loading || (!first && next === undefined)) return;
    const mine = generation;
    loading = true;
    const result = await fetchConversations(route.folder, first ? undefined : next, rows.length);
    if (mine !== generation) return;
    loading = false;
    listFailure = result.ok ? null : handle(result.error);
    if (!result.ok) return;
    listLoaded = true;
    const seen = new Set(rows.map((r) => r.id));
    const page = result.value.conversations;
    rows = first ? page : [...rows, ...page.filter((c) => !seen.has(c.id))];
    next = result.value.next;
  }

  // The list's first page again, merged with what is shown: rows that are on the new first
  // page take its version, rows below it stay, and rows that were above its end but are no
  // longer on it (moved, deleted) go.
  async function refreshList(): Promise<void> {
    if (loading || !listLoaded) return;
    const mine = generation;
    const started = Date.now();
    const result = await fetchConversations(route.folder);
    if (mine !== generation || !result.ok) return;
    const page = result.value.conversations;
    const last = page.at(-1);
    if (result.value.next === undefined || !last) {
      rows = page;
      next = undefined;
    } else {
      const ids = new Set(page.map((c) => c.id));
      rows = [...page, ...rows.filter((r) => !ids.has(r.id) && r.date < last.date)];
      next ??= result.value.next;
    }
    // A flag or read change made here outlives a refresh that set off before the server had it.
    for (const [id, change] of pendingFlags) {
      if (change.done && change.done < started) {
        pendingFlags.delete(id);
        continue;
      }
      const row = rows.find((r) => r.id === id);
      if (row) Object.assign(row, change.patch);
    }
  }

  function markedRead(id: number): void {
    const row = rows.find((r) => r.id === id);
    if (row) row.unread = false;
    loadFolders(true);
  }

  // A new folder: start its list from the top. Only the folder is a dependency; the list state
  // this resets must not re-run it.
  $effect(() => {
    void route.folder;
    untrack(() => {
      generation++;
      rows = [];
      next = undefined;
      loading = false;
      listLoaded = false;
      listFailure = null;
      folderMissing = false;
      hidden.clear();
      checked = [];
      loadPage(true);
    });
  });

  // Folder counts and the open list: now, every minute, and whenever the tab comes back into
  // view. The event stream covers the Inbox at once; this catches every other folder.
  $effect(() => {
    loadFolders(true);
    const timer = setInterval(() => {
      loadFolders(true);
      refreshList();
    }, 60_000);
    const onFocus = () => loadFolders();
    addEventListener('focus', onFocus);
    return () => {
      clearInterval(timer);
      removeEventListener('focus', onFocus);
    };
  });

  // News from the mail server, as it happens.
  $effect(() => {
    const events = new EventSource('/api/events');
    events.addEventListener('inbox', () => {
      loadFolders(true);
      if (route.folder === 'INBOX') refreshList();
    });
    events.addEventListener('signed_out', () => onSignedOut());
    events.addEventListener('unreachable', () => (pushDown = true));
    events.addEventListener('reachable', () => {
      pushDown = false;
      loadFolders(true);
      refreshList();
    });
    events.addEventListener('password_changed', () => onSignedOut('password_changed'));
    // A stream refused outright (signed out elsewhere) is not retried by the browser; asking for
    // the folders finds out why and acts on it.
    events.addEventListener('error', () => {
      if (events.readyState === EventSource.CLOSED) loadFolders(true);
    });
    return () => events.close();
  });

  // While loads fail, try again every five seconds: the folders, and the list from where it
  // stopped.
  $effect(() => {
    if (!failure) return;
    const timer = setInterval(() => {
      if (foldersFailure) loadFolders(true);
      if (listFailure) loadPage(!listLoaded);
    }, 5_000);
    return () => clearInterval(timer);
  });

  // --- actions on conversations ---

  function failed(error: ErrorCode): void {
    if (handle(error) === null) return;
    notice = { text: strings.actions.failed };
    refreshList();
    loadFolders(true);
  }

  // The rows for ids. The open conversation is taken from its own messages, which are fresher
  // than its row (a reply may have come since the list was loaded) and are there even when its
  // row is not loaded (opened from a link).
  function rowsOf(ids: number[]): Conversation[] {
    const out = rows.filter((r) => ids.includes(r.id) && r.id !== openConversationRow?.id);
    if (openConversationRow && ids.includes(openConversationRow.id)) out.push(openConversationRow);
    return out;
  }

  // Moves conversations away at once (the list changes before the server answers), then tells
  // the server, and offers an undo that moves the messages back by their new UIDs.
  async function moveAway(ids: number[], dest: Destination, text: (n: number) => string): Promise<void> {
    const targets = rowsOf(ids);
    if (targets.length === 0) return;
    const folder = route.folder;
    const uids = targets.flatMap((r) => r.uids);
    for (const r of targets) hidden.add(r.id);
    checked = checked.filter((id) => !ids.includes(id));
    if (route.conversation && ids.includes(route.conversation)) closeConversation();
    const result = await move(folder, uids, dest);
    if (!result.ok) {
      for (const r of targets) hidden.delete(r.id);
      return failed(result.error);
    }
    loadFolders(true);
    const moved = result.value;
    notice = {
      text: text(targets.length),
      undo: async () => {
        const back = await move(moved.folder, moved.uids, { to: folder });
        if (!back.ok) return failed(back.error);
        if (route.folder === folder) await refreshList();
        loadFolders(true);
      },
    };
  }

  // Read, unread, flag and unflag change the rows at once, then the server.
  async function flagAway(ids: number[], flag: 'seen' | 'flagged', on: boolean): Promise<void> {
    const targets = rowsOf(ids);
    if (targets.length === 0) return;
    // Flagging marks the latest message; unflagging and read state cover every message.
    const uids = flag === 'flagged' && on ? targets.map((r) => r.uids.at(-1)!) : targets.flatMap((r) => r.uids);
    const patch: Partial<Conversation> = flag === 'seen' ? { unread: !on } : { flagged: on };
    for (const r of targets) {
      // r may stand in for the open conversation; its row, if loaded, changes too.
      Object.assign(r, patch);
      const row = rows.find((row) => row.id === r.id);
      if (row) Object.assign(row, patch);
      pendingFlags.set(r.id, { patch });
    }
    const result = await setFlag(route.folder, uids, flag, on);
    // The open conversation shows the change without fetching it again.
    if (openLoaded && targets.some((r) => r.id === openLoaded?.id)) {
      const changed = new Set(uids);
      for (const m of openLoaded.messages) {
        if (m.folder !== route.folder || !changed.has(m.uid)) continue;
        if (flag === 'seen') m.unread = !on;
        else m.flagged = on;
      }
    }
    for (const r of targets) {
      const change = pendingFlags.get(r.id);
      if (change?.patch === patch) change.done = Date.now();
    }
    if (!result.ok) return failed(result.error);
    if (flag === 'seen') loadFolders(true);
  }

  function act(action: Action, ids: number[]): void {
    const role = currentFolder?.role;
    switch (action) {
      case 'archive':
        return void moveAway(ids, { role: 'archive' }, strings.actions.archived);
      case 'delete':
        if (role === 'trash') {
          // The messages are fixed now, so what the person confirms is what goes.
          asking = { kind: 'expunge', ids, uids: rowsOf(ids).flatMap((r) => r.uids) };
          return;
        }
        return void moveAway(ids, { role: 'trash' }, strings.actions.trashed);
      case 'spam':
        return void moveAway(ids, { role: 'junk' }, strings.actions.spammed);
      case 'notSpam':
        return void moveAway(ids, { role: 'inbox' }, strings.actions.unspammed);
      case 'moveTo':
        asking = { kind: 'moveTo', ids };
        return;
      case 'read':
      case 'unread':
        return void flagAway(ids, 'seen', action === 'read');
      case 'flag':
      case 'unflag':
        return void flagAway(ids, 'flagged', action === 'flag');
    }
  }

  function moveTo(ids: number[], folder: Folder): void {
    asking = null;
    void moveAway(ids, { to: folder.name }, (n) => strings.actions.moved(n, folder.label));
  }

  async function expungeNow(ids: number[], uids: number[]): Promise<void> {
    asking = null;
    const targets = rowsOf(ids);
    for (const r of targets) hidden.add(r.id);
    checked = checked.filter((id) => !ids.includes(id));
    if (route.conversation && ids.includes(route.conversation)) closeConversation();
    const result = await expunge(route.folder, uids);
    if (!result.ok) {
      for (const r of targets) hidden.delete(r.id);
      return failed(result.error);
    }
    loadFolders(true);
  }

  const checkedRows = $derived(rowsOf(checked));

  // --- folders ---

  async function create(parent: string, label: string): Promise<ErrorCode | null> {
    const result = await createFolder(parent, label);
    if (!result.ok) return result.error;
    await loadFolders(true);
    return null;
  }

  // Renames or reparents a folder, keeps an open folder at or under it open under its new name,
  // and reloads the folders. Returns the new name, or the error.
  async function renameTo(folder: Folder, parent: string, label: string): Promise<{ name: string } | { error: ErrorCode }> {
    const result = await renameFolder(folder.name, parent, label);
    if (!result.ok) return { error: result.error };
    const { name, delimiter } = result.value;
    const at = route.folder;
    if (at === folder.name || at.startsWith(folder.name + delimiter)) openFolder(name + at.slice(folder.name.length));
    await loadFolders(true);
    return { name };
  }

  async function rename(folder: Folder, label: string): Promise<ErrorCode | null> {
    const result = await renameTo(folder, folder.parent ?? '', label);
    return 'error' in result ? result.error : null;
  }

  // A new parent, from "Move folder…" or a drag: a failure shows as a notice.
  async function reparent(folder: Folder, parent: string): Promise<string | null> {
    asking = null;
    const result = await renameTo(folder, parent, folder.label);
    if (!('error' in result)) return result.name;
    notice = { text: result.error === 'folder_exists' ? strings.folders.exists(folder.label) : strings.actions.failed };
    return null;
  }

  function descendants(folder: Folder): Folder[] {
    // The list is ordered as a tree, so a folder's parent always comes before it.
    const out: Folder[] = [];
    for (const f of folderList) {
      if (f.parent && (f.parent === folder.name || out.some((d) => d.name === f.parent))) out.push(f);
    }
    return out;
  }

  function askDelete(folder: Folder): void {
    const under = descendants(folder);
    const messages = [folder, ...under].reduce((n, f) => n + f.total, 0);
    const text = under.length
      ? strings.folders.confirmDeleteWithSubfolders(folder.label, under.length, messages)
      : strings.folders.confirmDelete(folder.label, messages);
    asking = { kind: 'deleteFolder', folder, text };
  }

  async function deleteNow(folder: Folder): Promise<void> {
    asking = null;
    const gone = new Set([folder.name, ...descendants(folder).map((f) => f.name)]);
    folderList = folderList.filter((f) => !gone.has(f.name));
    if (gone.has(route.folder)) openFolder('INBOX');
    const result = await deleteFolder(folder.name);
    if (!result.ok) failed(result.error);
    loadFolders(true);
  }

  // Nesting renames the folder under its new parent; reordering stores the arrangement, which
  // is Postbox's own since IMAP has no folder order.
  async function moveFolder(folder: Folder, target: Folder, zone: 'before' | 'into' | 'after'): Promise<void> {
    let name = folder.name;
    const parent = zone === 'into' ? target.name : (target.parent ?? '');
    if (parent !== (folder.parent ?? '')) {
      const renamed = await reparent(folder, parent);
      if (!renamed) return;
      name = renamed;
    }
    if (zone === 'into') return;

    // The moved folder and everything under it go before or after the target and its subtree.
    const moved = folderList.find((f) => f.name === name);
    const anchor = folderList.find((f) => f.name === target.name);
    if (!moved || !anchor) return;
    const block = [moved, ...descendants(moved)];
    const rest = folderList.filter((f) => !block.includes(f));
    let at = rest.indexOf(anchor);
    if (zone === 'after') at += 1 + descendants(anchor).length;
    folderList = [...rest.slice(0, at), ...block, ...rest.slice(at)];
    const saved = await saveFolderOrder(folderList.map((f) => f.name));
    if (!saved.ok) failed(saved.error);
  }

  // The sign-in screen appears only once the server confirms: showing it after a failed
  // "Sign out everywhere" would let someone believe a stolen session had ended.
  async function leave(everywhere: boolean): Promise<void> {
    menuOpen = false;
    signOutFailed = false;
    if (await signOut(everywhere)) {
      onSignedOut();
      return;
    }
    signOutFailed = true;
  }

  function closeOnEscape(event: KeyboardEvent): void {
    if (event.key === 'Escape') menuOpen = false;
  }
</script>

<svelte:window onkeydown={closeOnEscape} />

<div class="shell">
  <header>
    <span class="brand">{strings.appName}</span>
    <button type="button" class="compose" onclick={() => compose('new')}>{strings.compose.compose}</button>
    <div class="account">
      <button
        type="button"
        class="account-button"
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label={`${strings.account.menu}: ${account.account}`}
        onclick={() => (menuOpen = !menuOpen)}
      >
        {account.account}
      </button>
      {#if menuOpen}
        <div class="menu" role="menu">
          <button type="button" role="menuitem" onclick={() => leave(false)}>{strings.account.signOut}</button>
          <button type="button" role="menuitem" onclick={() => leave(true)}>{strings.account.signOutEverywhere}</button>
        </div>
      {/if}
    </div>
  </header>

  {#if failure}
    <p class="error-banner banner" role="alert">
      {failure === 'unreachable' ? strings.unreachableBanner : strings.failedBanner}
    </p>
  {/if}
  {#if signOutFailed}
    <p class="error-banner banner" role="alert">{strings.account.signOutFailed}</p>
  {/if}
  {#each pageNotices as n (n.id)}
    <div class="error-banner banner page-notice" role="alert">
      <span>{noticeText(n)}</span>
      <button
        type="button"
        onclick={() => {
          pageNotices = pageNotices.filter((p) => p.id !== n.id);
          void dismissNotice(n.id);
        }}>{strings.compose.dismiss}</button
      >
    </div>
  {/each}

  <div class="panes" data-view={view}>
    <FolderList
      folders={folderList}
      current={route.folder}
      onOpen={(name) => {
        showFolders = false;
        openFolder(name);
      }}
      onDropConversations={(name, from, ids) => {
        const target = folderList.find((f) => f.name === name);
        // Only a drag from the list on screen: another tab's rows are not these.
        if (target && from === route.folder) moveTo(ids, target);
      }}
      onPickParent={(folder) => (asking = { kind: 'moveFolder', folder })}
      onCreate={create}
      onRename={rename}
      onDelete={askDelete}
      onMoveFolder={moveFolder}
    />

    <div class="middle">
      <div class="title">
        <button type="button" class="folders-button" onclick={() => (showFolders = true)}>
          ☰ <span class="visually-hidden">{strings.reading.folders}</span>
        </button>
        <h1>{currentFolder?.label ?? route.folder}</h1>
      </div>
      {#if checkedRows.length > 0}
        <div class="selection">
          <span>{strings.actions.selected(checkedRows.length)}</span>
          <button type="button" class="clear" onclick={() => (checked = [])}>{strings.actions.clearSelection}</button>
          <ActionBar
            role={currentFolder?.role}
            unread={checkedRows.some((r) => r.unread)}
            flagged={checkedRows.some((r) => r.flagged)}
            onAction={(a) => act(a, checked)}
          />
        </div>
      {/if}
      {#if folderMissing}
        <p class="note">{strings.folders.missing}</p>
      {:else}
        <ConversationList
          account={account.account}
          conversations={shownRows}
          loading={loading || !listLoaded}
          hasMore={next !== undefined}
          folder={route.folder}
          selected={route.conversation}
          {checked}
          onCheck={(ids) => (checked = ids)}
          onMore={() => loadPage(false)}
          onOpen={(id) => {
            // A draft opens in the composer, not the reading pane.
            const row = rows.find((r) => r.id === id);
            if (currentFolder?.role === 'drafts' && row) void compose('draft', route.folder, row.uids.at(-1));
            else openConversation(id);
          }}
        />
      {/if}
    </div>

    {#if route.conversation}
      <ConversationView
        account={account.account}
        folder={route.folder}
        id={route.conversation}
        onBack={closeConversation}
        onRead={markedRead}
        onError={(e) => (listFailure = handle(e))}
        refresh={openRowState}
        onLoaded={(folder, id, messages) => (openLoaded = { folder, id, messages })}
        onCompose={(mode, m) => compose(mode, m.folder, m.uid)}
      >
        {#if openConversationRow}
          {@const target = openConversationRow}
          <ActionBar
            role={currentFolder?.role}
            unread={target.unread}
            flagged={target.flagged}
            onAction={(a) => act(a, [target.id])}
          />
        {/if}
      </ConversationView>
    {:else}
      <section class="empty" aria-label="Reading pane">
        <p class="note">{strings.reading.nothingSelected}</p>
      </section>
    {/if}
  </div>
</div>

{#snippet picker(choices: Folder[], pick: (f: Folder) => void)}
  <ul class="picker">
    {#each choices as f (f.name)}
      <li><button type="button" onclick={() => pick(f)}>{f.parent ? `${f.parent} / ${f.label}` : f.label}</button></li>
    {/each}
  </ul>
  <div class="buttons">
    <button type="button" onclick={() => (asking = null)}>{strings.actions.cancel}</button>
  </div>
{/snippet}

{#if composing}
  {#key composing}
    <Composer
      view={composing.view}
      problem={composing.problem}
      onSent={sent}
      onClose={composerClosed}
      onError={failed}
    />
  {/key}
{/if}

{#if notice}
  <UndoNotice {notice} onDone={() => (notice = null)} />
{/if}

{#if asking?.kind === 'moveTo'}
  {@const ids = asking.ids}
  <Dialog label={strings.actions.moveHeading} onClose={() => (asking = null)}>
    <h2 class="dialog-title">{strings.actions.moveHeading}</h2>
    {@render picker(
      folderList.filter((f) => f.selectable && f.name !== route.folder),
      (f) => moveTo(ids, f),
    )}
  </Dialog>
{:else if asking?.kind === 'expunge'}
  {@const { ids, uids } = asking}
  <Dialog label={strings.actions.deleteForever} onClose={() => (asking = null)}>
    <p>{strings.actions.deleteForever}</p>
    <div class="buttons">
      <button type="button" onclick={() => (asking = null)}>{strings.actions.cancel}</button>
      <button type="button" class="danger" onclick={() => expungeNow(ids, uids)}>{strings.actions.confirmDelete}</button>
    </div>
  </Dialog>
{:else if asking?.kind === 'moveFolder'}
  {@const folder = asking.folder}
  <Dialog label={strings.folders.moveFolderHeading(folder.label)} onClose={() => (asking = null)}>
    <h2 class="dialog-title">{strings.folders.moveFolderHeading(folder.label)}</h2>
    {@const under = new Set(descendants(folder))}
    {#if folder.parent}
      <button type="button" class="top-level" onclick={() => reparent(folder, '')}>{strings.folders.topLevel}</button>
    {/if}
    {@render picker(
      folderList.filter((f) => f.name !== folder.name && f.name !== folder.parent && !under.has(f)),
      (f) => reparent(folder, f.name),
    )}
  </Dialog>
{:else if asking?.kind === 'deleteFolder'}
  {@const folder = asking.folder}
  <Dialog label={asking.text} onClose={() => (asking = null)}>
    <p>{asking.text}</p>
    <div class="buttons">
      <button type="button" onclick={() => (asking = null)}>{strings.actions.cancel}</button>
      <button type="button" class="danger" onclick={() => deleteNow(folder)}>{strings.actions.confirmDelete}</button>
    </div>
  </Dialog>
{/if}

<style>
  .shell {
    height: 100dvh;
    display: grid;
    grid-template-rows: auto auto auto 1fr;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.6rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .brand {
    font-weight: 650;
    color: var(--accent);
  }

  .account {
    position: relative;
    margin-left: auto;
  }

  .compose {
    margin-left: 1.5rem;
    border: none;
    border-radius: 999px;
    padding: 0.4rem 1rem;
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
    cursor: pointer;
  }

  .page-notice {
    display: flex;
    justify-content: center;
    align-items: center;
    gap: 1rem;
  }

  .page-notice button {
    border: none;
    background: transparent;
    color: inherit;
    text-decoration: underline;
    cursor: pointer;
  }

  .account-button {
    font-size: 0.9rem;
    color: var(--text);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.35rem 0.85rem;
    cursor: pointer;
    max-width: 16rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 0.4rem);
    min-width: 12rem;
    display: grid;
    padding: 0.35rem;
    border-radius: 0.75rem;
    background: var(--surface);
    box-shadow: var(--shadow);
    z-index: 10;
  }

  .menu button {
    text-align: left;
    color: var(--text);
    background: transparent;
    border: none;
    border-radius: 0.5rem;
    padding: 0.55rem 0.75rem;
    cursor: pointer;
  }

  .menu button:hover,
  .menu button:focus-visible {
    background: var(--hover);
    outline: none;
  }

  .banner {
    border-radius: 0;
    text-align: center;
  }

  .panes {
    min-height: 0;
    display: grid;
    grid-template-columns: 14rem minmax(20rem, 28rem) minmax(0, 1fr);
  }

  /* Phone width: one pane at a time, chosen by what is open. */
  @media (max-width: 767px) {
    .panes {
      grid-template-columns: minmax(0, 1fr);
    }

    .panes > :global(*) {
      display: none !important;
    }

    .panes[data-view='list'] > .middle,
    .panes[data-view='reading'] > :global(.reading) {
      display: grid !important;
    }

    .panes[data-view='folders'] > :global(nav) {
      display: block !important;
    }

    .folders-button {
      display: inline-block !important;
    }
  }

  .selection {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 0.75rem;
    padding: 0.5rem 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--surface);
    font-size: 0.9rem;
  }

  .clear {
    border: none;
    background: transparent;
    color: var(--accent);
    cursor: pointer;
  }

  .dialog-title {
    margin: 0 0 0.75rem;
    font-size: 1.1rem;
  }

  .picker {
    list-style: none;
    margin: 0 0 1rem;
    padding: 0;
    max-height: 18rem;
    overflow-y: auto;
  }

  .picker button,
  .top-level {
    width: 100%;
    text-align: left;
    border: none;
    background: transparent;
    color: var(--text);
    padding: 0.5rem 0.6rem;
    border-radius: 0.4rem;
    cursor: pointer;
  }

  .picker button:hover,
  .top-level:hover {
    background: var(--hover);
  }

  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }

  .buttons button {
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    padding: 0.45rem 0.9rem;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
  }

  .buttons .danger {
    border-color: var(--danger);
    background: var(--danger);
    color: var(--on-accent);
  }

  .title {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    border-bottom: 1px solid var(--border);
  }

  .folders-button {
    display: none;
    margin-left: 0.5rem;
    border: none;
    background: transparent;
    color: var(--text);
    font-size: 1.1rem;
    padding: 0.4rem 0.5rem;
    cursor: pointer;
  }

  .visually-hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }

  .panes > :global(nav) {
    border-right: 1px solid var(--border);
  }

  .middle {
    min-height: 0;
    display: grid;
    grid-template-rows: auto 1fr;
    border-right: 1px solid var(--border);
  }

  h1 {
    margin: 0;
    padding: 0.85rem 1rem 0.6rem;
    font-size: 1.25rem;
    font-weight: 650;
    letter-spacing: -0.02em;
  }

  .empty {
    display: grid;
    place-items: center;
  }
</style>
