<script lang="ts">
  import {
    conversations as fetchConversations,
    folders as fetchFolders,
    signOut,
    type Account,
    type Conversation,
    type ErrorCode,
    type Folder,
  } from './api';
  import { untrack } from 'svelte';
  import ConversationList from './ConversationList.svelte';
  import FolderList from './FolderList.svelte';
  import { openFolder, route } from './route.svelte';
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
  const failure = $derived(foldersFailure ?? listFailure);
  let folderMissing = $state(false);

  let folderList = $state<Folder[]>([]);
  let rows = $state<Conversation[]>([]);
  let next = $state<number | undefined>(undefined);
  let loading = $state(false);
  // False until the open folder's first page has arrived, so a failed load never reads as empty.
  let listLoaded = $state(false);
  // Bumped whenever the open folder changes, so answers for the old one are dropped.
  let generation = 0;

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
      loadPage(true);
    });
  });

  // Folder counts: now, every minute, and whenever the tab comes back into view.
  $effect(() => {
    loadFolders(true);
    const timer = setInterval(() => loadFolders(true), 60_000);
    const onFocus = () => loadFolders();
    addEventListener('focus', onFocus);
    return () => {
      clearInterval(timer);
      removeEventListener('focus', onFocus);
    };
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

  <div class="panes">
    <FolderList folders={folderList} current={route.folder} onOpen={openFolder} />

    <div class="middle">
      <h1>{currentFolder?.label ?? route.folder}</h1>
      {#if folderMissing}
        <p class="note">{strings.folders.missing}</p>
      {:else}
        <ConversationList
          account={account.account}
          conversations={rows}
          loading={loading || !listLoaded}
          hasMore={next !== undefined}
          onMore={() => loadPage(false)}
        />
      {/if}
    </div>

    <section class="reading" aria-label="Reading pane">
      <p class="note">{strings.reading.nothingSelected}</p>
    </section>
  </div>
</div>

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
    grid-template-columns: 14rem minmax(20rem, 28rem) 1fr;
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
    border-bottom: 1px solid var(--border);
  }

  .reading {
    display: grid;
    place-items: center;
  }
</style>
