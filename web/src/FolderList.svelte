<script lang="ts" module>
  // What a drag carries. Conversations come from the list; folders from this pane.
  export const conversationType = 'application/x-postbox-conversations';
  const folderType = 'application/x-postbox-folder';
</script>

<script lang="ts">
  import type { ErrorCode, Folder, Role } from './api';
  import { strings } from './strings';

  type Zone = 'before' | 'into' | 'after';

  const folderIcons: Record<Role, string> = {
    inbox: 'M2 9l2-5h8l2 5v4H2z M2 9h3l1 2h4l1-2h3',
    drafts: 'M4 2h5l3 3v9H4z M9 2v3h3 M6 8h4 M6 11h4',
    sent: 'M2 7l12-5-5 12-2-5z M7 9l7-7',
    archive: 'M2 3h12v3H2z M3 6v7h10V6 M6 9h4',
    junk: 'M8 2l5 2v4c0 2-2 4-5 6-3-2-5-4-5-6V4z M8 5v3 M8 10v.1',
    trash: 'M2 4h12 M6 4V2h4v2 M4 4l1 9h6l1-9 M7 6v5 M9 6v5',
  };

  interface Props {
    folders: Folder[];
    current: string;
    onOpen: (name: string) => void;
    // Conversations dropped on a folder: those the drag carries, from the folder it came from.
    onDropConversations: (target: string, from: string, ids: number[]) => void;
    onCreate: (parent: string, label: string) => Promise<ErrorCode | null>;
    onRename: (folder: Folder, label: string) => Promise<ErrorCode | null>;
    onDelete: (folder: Folder) => void;
    // A folder dropped into another (nest) or beside one (reorder).
    onMoveFolder: (folder: Folder, target: Folder, zone: Zone) => void;
    // "Move folder…": choose a new parent without dragging.
    onPickParent: (folder: Folder) => void;
  }
  const { folders, current, onOpen, onDropConversations, onCreate, onRename, onDelete, onMoveFolder, onPickParent }: Props =
    $props();

  const byName = $derived(new Map(folders.map((f) => [f.name, f])));

  // How deep each folder sits; the list already comes ordered as a tree.
  const depths = $derived(
    new Map(
      folders.map((f) => {
        let d = 0;
        for (let p = f.parent; p && d < folders.length; p = byName.get(p)?.parent) d++;
        return [f.name, d];
      }),
    ),
  );

  function within(folder: Folder, ancestor: Folder): boolean {
    for (let p: string | undefined = folder.name; p; p = byName.get(p)?.parent) {
      if (p === ancestor.name) return true;
    }
    return false;
  }

  // --- menus and inline editing ---
  let menuFor = $state<string | null>(null);
  // A folder being renamed, or the parent ("" for the top level) a new folder goes under.
  let editing = $state<{ kind: 'rename'; folder: Folder } | { kind: 'create'; parent: string } | null>(null);
  let draft = $state('');
  let editError = $state('');

  function startCreate(parent: string): void {
    menuFor = null;
    editing = { kind: 'create', parent };
    draft = '';
    editError = '';
  }

  function startRename(folder: Folder): void {
    menuFor = null;
    editing = { kind: 'rename', folder };
    draft = folder.label;
    editError = '';
  }

  async function commit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!editing) return;
    const label = draft.trim();
    if (!label || (editing.kind === 'rename' && label === editing.folder.label)) {
      editing = null;
      return;
    }
    const error =
      editing.kind === 'rename' ? await onRename(editing.folder, label) : await onCreate(editing.parent, label);
    if (!error) {
      editing = null;
      return;
    }
    editError = error === 'folder_exists' ? strings.folders.exists(label) : strings.actions.failed;
  }

  function cancelOnEscape(event: KeyboardEvent): void {
    if (event.key === 'Escape') editing = null;
  }

  function focus(node: HTMLInputElement): void {
    node.focus();
    node.select();
  }

  // --- drag and drop ---
  let dragFolder: Folder | null = null;
  let over = $state<{ name: string; zone: Zone } | null>(null);

  function zoneOf(event: DragEvent, target: Folder): Zone | null {
    const types = event.dataTransfer?.types ?? [];
    if (types.includes(conversationType)) return target.selectable && target.name !== current ? 'into' : null;
    if (!types.includes(folderType) || !dragFolder || within(target, dragFolder)) return null;
    const box = (event.currentTarget as HTMLElement).getBoundingClientRect();
    const y = (event.clientY - box.top) / box.height;
    if (y < 0.25) return 'before';
    if (y > 0.75) return 'after';
    // Dropping a folder into the folder it is already in changes nothing.
    return target.name === dragFolder.parent ? null : 'into';
  }

  function dragOver(event: DragEvent, target: Folder): void {
    const zone = zoneOf(event, target);
    if (!zone) {
      over = null;
      return;
    }
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    over = { name: target.name, zone };
  }

  function drop(event: DragEvent, target: Folder): void {
    const zone = zoneOf(event, target);
    over = null;
    if (!zone) return;
    event.preventDefault();
    if (event.dataTransfer?.types.includes(conversationType)) {
      try {
        const carried = JSON.parse(event.dataTransfer.getData(conversationType)) as { folder: string; ids: number[] };
        onDropConversations(target.name, carried.folder, carried.ids);
      } catch {
        // Not a drag this app made.
      }
      return;
    }
    if (dragFolder) onMoveFolder(dragFolder, target, zone);
    dragFolder = null;
  }

  function dragStart(event: DragEvent, folder: Folder): void {
    if (folder.role || !event.dataTransfer) return;
    dragFolder = folder;
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData(folderType, folder.name);
  }
</script>

<svelte:window onclick={() => (menuFor = null)} />

<nav aria-label={strings.folders.heading}>
  <ul>
    {#each folders as folder (folder.name)}
      <li
        style:--depth={depths.get(folder.name) ?? 0}
        class:drop-into={over?.name === folder.name && over.zone === 'into'}
        class:drop-before={over?.name === folder.name && over.zone === 'before'}
        class:drop-after={over?.name === folder.name && over.zone === 'after'}
        draggable={!folder.role && editing?.kind !== 'rename'}
        ondragstart={(e) => dragStart(e, folder)}
        ondragend={() => {
          dragFolder = null;
          over = null;
        }}
        ondragover={(e) => dragOver(e, folder)}
        ondragleave={() => over?.name === folder.name && (over = null)}
        ondrop={(e) => drop(e, folder)}
      >
        {#if editing?.kind === 'rename' && editing.folder.name === folder.name}
          <form onsubmit={commit}>
            <input
              aria-label={strings.folders.name}
              bind:value={draft}
              use:focus
              onkeydown={cancelOnEscape}
              onblur={() => !editError && (editing = null)}
            />
            {#if editError}<p class="error" role="alert">{editError}</p>{/if}
          </form>
        {:else}
          <div class="row">
            {#if folder.selectable}
              <button
                type="button"
                class="open"
                class:current={folder.name === current}
                aria-current={folder.name === current ? 'page' : undefined}
                onclick={() => onOpen(folder.name)}
              >
                {#if folder.role}
                  <svg class="icon" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d={folderIcons[folder.role]} />
                  </svg>
                {/if}
                <span class="label">{folder.label}</span>
                {#if folder.unread > 0}
                  <span class="count" aria-label={strings.folders.unread(folder.unread)}>{folder.unread}</span>
                {/if}
              </button>
            {:else}
              <span class="holder">{folder.label}</span>
            {/if}
            {#if folder.selectable || !folder.role}
              <button
                type="button"
                class="more"
                aria-label={strings.folders.actionsFor(folder.label)}
                aria-haspopup="menu"
                aria-expanded={menuFor === folder.name}
                onclick={(e) => {
                  e.stopPropagation();
                  menuFor = menuFor === folder.name ? null : folder.name;
                }}>⋯</button
              >
            {/if}
          </div>
          {#if menuFor === folder.name}
            <div class="menu" role="menu">
              <button type="button" role="menuitem" onclick={() => startCreate(folder.name)}>{strings.folders.newSubfolder}</button>
              {#if !folder.role}
                <button type="button" role="menuitem" onclick={() => startRename(folder)}>{strings.folders.rename}</button>
                <button
                  type="button"
                  role="menuitem"
                  onclick={() => {
                    menuFor = null;
                    onPickParent(folder);
                  }}>{strings.folders.moveFolder}</button
                >
                <button
                  type="button"
                  role="menuitem"
                  onclick={() => {
                    menuFor = null;
                    onDelete(folder);
                  }}>{strings.folders.delete}</button
                >
              {/if}
            </div>
          {/if}
        {/if}
        {#if editing?.kind === 'create' && editing.parent === folder.name}
          <form class="child" onsubmit={commit}>
            <input aria-label={strings.folders.name} bind:value={draft} use:focus onkeydown={cancelOnEscape} />
            {#if editError}<p class="error" role="alert">{editError}</p>{/if}
          </form>
        {/if}
      </li>
    {/each}
  </ul>

  {#if editing?.kind === 'create' && editing.parent === ''}
    <form onsubmit={commit}>
      <input aria-label={strings.folders.name} bind:value={draft} use:focus onkeydown={cancelOnEscape} />
      {#if editError}<p class="error" role="alert">{editError}</p>{/if}
    </form>
  {:else}
    <button type="button" class="new" onclick={() => startCreate('')}>+ {strings.folders.newFolder}</button>
  {/if}
</nav>

<style>
  nav {
    padding: 0.75rem 0.5rem;
    overflow-y: auto;
    background: var(--rail);
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 1px;
  }

  li {
    position: relative;
    padding-left: calc(var(--depth) * 0.9rem);
    border-radius: 0.5rem;
  }

  li.drop-into {
    outline: 2px solid var(--accent);
    outline-offset: -2px;
  }

  li.drop-before {
    box-shadow: inset 0 2px 0 var(--accent);
  }

  li.drop-after {
    box-shadow: inset 0 -2px 0 var(--accent);
  }

  .row {
    display: flex;
    align-items: center;
  }

  .open,
  .holder {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.45rem 0.65rem;
    border-radius: 0.5rem;
    box-sizing: border-box;
  }

  .open {
    border: none;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }

  .open:hover {
    background: var(--hover);
  }

  .open.current {
    background: var(--selected);
    font-weight: 600;
  }

  .icon {
    flex: none;
    width: 1rem;
    height: 1rem;
    color: var(--text-muted);
  }

  .current .icon {
    color: var(--accent);
  }

  .holder {
    color: var(--text-muted);
  }

  .label {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .count {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--accent);
  }

  .more {
    border: none;
    background: transparent;
    color: var(--text-muted);
    padding: 0.25rem 0.4rem;
    border-radius: 0.4rem;
    cursor: pointer;
    opacity: 0;
  }

  li:hover .more,
  .more:focus-visible,
  .more[aria-expanded='true'] {
    opacity: 1;
  }

  @media (hover: none) {
    .more {
      opacity: 1;
    }
  }

  .menu {
    position: absolute;
    right: 0;
    top: 100%;
    z-index: 5;
    display: grid;
    min-width: 11rem;
    padding: 0.3rem;
    border-radius: 0.6rem;
    background: var(--surface);
    box-shadow: var(--shadow);
  }

  .menu button {
    border: none;
    background: transparent;
    color: var(--text);
    text-align: left;
    padding: 0.45rem 0.65rem;
    border-radius: 0.4rem;
    cursor: pointer;
  }

  .menu button:hover {
    background: var(--hover);
  }

  form {
    padding: 0.2rem 0.3rem;
  }

  form.child {
    padding-left: 1.2rem;
  }

  input {
    width: 100%;
    box-sizing: border-box;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--accent);
    border-radius: 0.4rem;
    background: var(--bg);
    color: var(--text);
  }

  .error {
    margin: 0.3rem 0 0;
    font-size: 0.85rem;
    color: var(--danger);
  }

  .new {
    margin-top: 0.5rem;
    width: 100%;
    border: none;
    background: transparent;
    color: var(--text-muted);
    text-align: left;
    padding: 0.45rem 0.65rem;
    border-radius: 0.5rem;
    cursor: pointer;
  }

  .new:hover {
    background: var(--hover);
    color: var(--text);
  }
</style>
