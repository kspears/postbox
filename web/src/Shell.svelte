<script lang="ts">
  import {
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
  } from './api';
  import { untrack } from 'svelte';
  import { SvelteSet } from 'svelte/reactivity';
  import ActionBar, { type Action } from './ActionBar.svelte';
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

  // Conversations moved away by an action here. They stay hidden even if a refresh that started
  // before the move brings them back.
  const hidden = new SvelteSet<number>();
  const shownRows = $derived(rows.filter((r) => !hidden.has(r.id)));
  // Ticked conversations, for actions and drags.
  let checked = $state<number[]>([]);
  // The conversations a drag carries, until it is dropped.
  let dragged: number[] = [];
  let notice = $state<Notice | null>(null);
  // An action waiting on the person: a folder to move to, or a confirmation.
  let asking = $state<
    | { kind: 'moveTo'; ids: number[] }
    | { kind: 'expunge'; ids: number[] }
    | { kind: 'deleteFolder'; folder: Folder; text: string }
    | null
  >(null);

  // The open conversation's row: when a refresh changes it (a reply arrived, a message
  // went), the conversation is fetched again.
  const openRow = $derived(rows.find((r) => r.id === route.conversation));
  const openRowState = $derived(openRow ? `${openRow.date} ${openRow.count}` : '');
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

  async function loadFolders(force = false): Promise<void> {
    if (foldersInFlight || (!force && Date.now() - foldersAt < 10_000)) return;
    foldersInFlight = true;
    const result = await fetchFolders();
    foldersInFlight = false;
    foldersAt = Date.now();
    foldersFailure = result.ok ? null : handle(result.error);
    if (result.ok) folderList = result.value;
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
    const result = await fetchConversations(route.folder);
    if (mine !== generation || !result.ok) return;
    const page = result.value.conversations;
    const last = page.at(-1);
    if (result.value.next === undefined || !last) {
      rows = page;
      next = undefined;
      return;
    }
    const ids = new Set(page.map((c) => c.id));
    rows = [...page, ...rows.filter((r) => !ids.has(r.id) && r.date < last.date)];
    next ??= result.value.next;
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

  function rowsOf(ids: number[]): Conversation[] {
    return rows.filter((r) => ids.includes(r.id));
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
    for (const r of targets) {
      if (flag === 'seen') r.unread = !on;
      else r.flagged = on;
    }
    const result = await setFlag(route.folder, uids, flag, on);
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
          asking = { kind: 'expunge', ids };
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

  async function expungeNow(ids: number[]): Promise<void> {
    asking = null;
    const targets = rowsOf(ids);
    for (const r of targets) hidden.add(r.id);
    checked = checked.filter((id) => !ids.includes(id));
    if (route.conversation && ids.includes(route.conversation)) closeConversation();
    const result = await expunge(route.folder, targets.flatMap((r) => r.uids));
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

  async function rename(folder: Folder, label: string): Promise<ErrorCode | null> {
    const result = await renameFolder(folder.name, folder.parent ?? '', label);
    if (!result.ok) return result.error;
    if (route.folder === folder.name) openFolder(result.value.name);
    await loadFolders(true);
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
      const result = await renameFolder(folder.name, parent, folder.label);
      if (!result.ok) {
        notice = { text: result.error === 'folder_exists' ? strings.folders.exists(folder.label) : strings.actions.failed };
        return;
      }
      name = result.value.name;
      if (route.folder === folder.name) openFolder(name);
      await loadFolders(true);
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

  <div class="panes" data-view={view}>
    <FolderList
      folders={folderList}
      current={route.folder}
      onOpen={(name) => {
        showFolders = false;
        openFolder(name);
      }}
      onDropConversations={(name) => {
        const target = folderList.find((f) => f.name === name);
        if (target) moveTo(dragged, target);
        dragged = [];
      }}
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
          onDrag={(ids) => (dragged = ids)}
          onMore={() => loadPage(false)}
          onOpen={openConversation}
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
      >
        {#if openRow}
          <ActionBar
            role={currentFolder?.role}
            unread={openRow.unread}
            flagged={openRow.flagged}
            onAction={(a) => openRow && act(a, [openRow.id])}
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

{#if notice}
  <UndoNotice {notice} onDone={() => (notice = null)} />
{/if}

{#if asking?.kind === 'moveTo'}
  {@const ids = asking.ids}
  <Dialog label={strings.actions.moveHeading} onClose={() => (asking = null)}>
    <h2 class="dialog-title">{strings.actions.moveHeading}</h2>
    <ul class="picker">
      {#each folderList.filter((f) => f.selectable && f.name !== route.folder) as f (f.name)}
        <li><button type="button" onclick={() => moveTo(ids, f)}>{f.parent ? `${f.parent} / ${f.label}` : f.label}</button></li>
      {/each}
    </ul>
    <div class="buttons">
      <button type="button" onclick={() => (asking = null)}>{strings.actions.cancel}</button>
    </div>
  </Dialog>
{:else if asking?.kind === 'expunge'}
  {@const ids = asking.ids}
  <Dialog label={strings.actions.deleteForever} onClose={() => (asking = null)}>
    <p>{strings.actions.deleteForever}</p>
    <div class="buttons">
      <button type="button" onclick={() => (asking = null)}>{strings.actions.cancel}</button>
      <button type="button" class="danger" onclick={() => expungeNow(ids)}>{strings.actions.confirmDelete}</button>
    </div>
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

  .picker button {
    width: 100%;
    text-align: left;
    border: none;
    background: transparent;
    color: var(--text);
    padding: 0.5rem 0.6rem;
    border-radius: 0.4rem;
    cursor: pointer;
  }

  .picker button:hover {
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
