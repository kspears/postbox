<script lang="ts">
  import type { Found } from './api';
  import { path } from './route.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    results: Found[];
    loading: boolean;
    hasMore: boolean;
    // The label to show for a folder's name.
    label: (folder: string) => string;
    selected?: { folder: string; uid: number };
    onMore: () => void;
    onOpen: (r: Found) => void;
  }
  const { account, results, loading, hasMore, label, selected, onMore, onOpen }: Props = $props();

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

  function who(r: Found): string {
    const p = r.from[0];
    if (!p) return '';
    return p.email === account ? 'me' : p.name || p.email.split('@')[0] || p.email;
  }

  const today = new Date();
  const time = new Intl.DateTimeFormat(undefined, { hour: 'numeric', minute: '2-digit' });
  const dayMonth = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric' });
  const full = new Intl.DateTimeFormat(undefined, { dateStyle: 'short' });
  const long = new Intl.DateTimeFormat(undefined, { dateStyle: 'full', timeStyle: 'short' });

  function when(iso: string): string {
    const d = new Date(iso);
    if (d.toDateString() === today.toDateString()) return time.format(d);
    if (d.getFullYear() === today.getFullYear()) return dayMonth.format(d);
    return full.format(d);
  }

  const isSelected = (r: Found) => selected?.folder === r.folder && selected.uid === r.uid;
</script>

<section class="list" aria-label={strings.search.results}>
  {#if results.length === 0 && !loading}
    <p class="note">{strings.search.none}</p>
  {/if}
  <ul>
    {#each results as r (`${r.folder}\u0000${r.uid}`)}
      <li class:unread={r.unread} class:selected={isSelected(r)}>
        <a
          class="row"
          href={path({ folder: r.folder, conversation: r.uid })}
          aria-current={isSelected(r) ? 'true' : undefined}
          onclick={(e) => open(e, r)}
        >
          <div class="top">
            <span class="who">{who(r)}</span>
            <span class="folder">{label(r.folder)}</span>
            {#if r.attachments}<span class="mark" role="img" aria-label={strings.list.attachment}>📎</span>{/if}
            {#if r.flagged}<span class="mark flag" role="img" aria-label={strings.list.flagged}>⚑</span>{/if}
            <time datetime={r.date} title={long.format(new Date(r.date))}>{when(r.date)}</time>
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

  .unread .who,
  .unread .subject {
    font-weight: 650;
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
    font-size: 0.9rem;
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
