<script lang="ts">
  import type { Found } from './api';
  import { longDate, when, who } from './listFormat';
  import { path } from './route.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    results: Found[];
    loading: boolean;
    // The search could not be done: no claim that nothing matches.
    failed: boolean;
    hasMore: boolean;
    // The label to show for a folder's name.
    label: (folder: string) => string;
    selected?: { folder: string; id: number };
    onMore: () => void;
    onOpen: (r: Found) => void;
  }
  const { account, results, loading, failed, hasMore, label, selected, onMore, onOpen }: Props = $props();

  function open(event: MouseEvent, r: Found): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    event.preventDefault();
    onOpen(r);
  }

  let sentinel = $state<HTMLElement>();

  $effect(() => {
    if (!sentinel || !hasMore) return;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) onMore();
      },
      { rootMargin: '400px' },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  const isSelected = (r: Found) => selected?.folder === r.folder && selected.id === r.id;
</script>

<section class="list" aria-label={strings.search.results}>
  {#if results.length === 0 && !loading && !failed}
    <p class="note">{strings.search.none}</p>
  {/if}
  <ul>
    {#each results as r (`${r.folder}\u0000${r.id}`)}
      <li class:unread={r.unread} class:selected={isSelected(r)}>
        <a
          class="row"
          href={path({ folder: r.folder, conversation: r.id })}
          aria-current={isSelected(r) ? 'true' : undefined}
          onclick={(e) => open(e, r)}
        >
          <div class="top">
            <span class="who">{who(r.participants, account)}</span>
            <span class="folder">{label(r.folder)}</span>
            {#if r.attachments}<span class="mark" role="img" aria-label={strings.list.attachment}>📎</span>{/if}
            {#if r.flagged}<span class="mark flag" role="img" aria-label={strings.list.flagged}>⚑</span>{/if}
            <time datetime={r.date} title={longDate(r.date)}>{when(r.date)}</time>
          </div>
          <div class="subject">
            {#if r.unread}<span class="dot" role="img" aria-label={strings.list.unread}></span>{/if}
            {r.subject || strings.list.noSubject}
          </div>
          {#if r.snippet}<div class="snippet">{r.snippet}</div>{/if}
        </a>
      </li>
    {/each}
  </ul>
  {#if loading}<p class="note">{strings.search.searching}</p>{/if}
  {#if hasMore}<div bind:this={sentinel} class="sentinel"></div>{/if}
</section>

<style>
  .list {
    overflow-y: auto;
  }

  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  li {
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }

  li:hover {
    background: var(--hover);
  }

  li.selected {
    background: var(--selected);
    box-shadow: inset 3px 0 var(--accent);
  }

  .row {
    display: grid;
    gap: 0.15rem;
    padding: 0.65rem 1rem;
    color: inherit;
    text-decoration: none;
  }

  .top {
    display: flex;
    align-items: baseline;
    gap: 0.4rem;
    min-width: 0;
  }

  .who {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .who,
  .subject {
    font-weight: 550;
  }

  .unread .who,
  .unread .subject {
    font-weight: 700;
  }

  .folder {
    font-size: 0.75rem;
    padding: 0 0.4rem;
    border-radius: 0.6rem;
    background: var(--hover);
    color: var(--text-muted);
    white-space: nowrap;
  }

  .mark {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  .flag {
    color: var(--danger);
  }

  time {
    font-size: 0.8rem;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .subject,
  .snippet {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .snippet {
    font-size: 0.875rem;
    color: var(--text-muted);
  }

  .dot {
    display: inline-block;
    width: 0.5rem;
    height: 0.5rem;
    margin-right: 0.35rem;
    border-radius: 50%;
    background: var(--accent);
    vertical-align: middle;
  }

  .sentinel {
    height: 1px;
  }
</style>
