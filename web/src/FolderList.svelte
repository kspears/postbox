<script lang="ts">
  import type { Folder } from './api';
  import { strings } from './strings';

  interface Props {
    folders: Folder[];
    current: string;
    onOpen: (name: string) => void;
  }
  const { folders, current, onOpen }: Props = $props();

  function depth(folder: Folder): number {
    let d = 0;
    let parent = folder.parent;
    while (parent) {
      d++;
      parent = folders.find((f) => f.name === parent)?.parent;
    }
    return d;
  }
</script>

<nav aria-label={strings.folders.heading}>
  <ul>
    {#each folders as folder (folder.name)}
      <li style:--depth={depth(folder)}>
        {#if folder.selectable}
          <button
            type="button"
            class:current={folder.name === current}
            aria-current={folder.name === current ? 'page' : undefined}
            onclick={() => onOpen(folder.name)}
          >
            <span class="label">{folder.label}</span>
            {#if folder.unread > 0}
              <span class="count" aria-label={strings.folders.unread(folder.unread)}>{folder.unread}</span>
            {/if}
          </button>
        {:else}
          <span class="holder">{folder.label}</span>
        {/if}
      </li>
    {/each}
  </ul>
</nav>

<style>
  nav {
    padding: 0.5rem;
    overflow-y: auto;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 1px;
  }

  li {
    padding-left: calc(var(--depth) * 0.9rem);
  }

  button,
  .holder {
    width: 100%;
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.45rem 0.65rem;
    border-radius: 0.5rem;
    box-sizing: border-box;
  }

  button {
    border: none;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
  }

  button:hover {
    background: var(--hover);
  }

  button.current {
    background: var(--selected);
    font-weight: 600;
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
</style>
