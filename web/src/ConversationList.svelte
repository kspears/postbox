<script lang="ts">
  import type { Conversation } from './api';
  import { path } from './route.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    conversations: Conversation[];
    loading: boolean;
    hasMore: boolean;
    folder: string;
    selected?: number;
    onMore: () => void;
    onOpen: (id: number) => void;
  }
  const { account, conversations, loading, hasMore, folder, selected, onMore, onOpen }: Props = $props();

  // A real link, so a middle click or "open in new tab" works; a plain click opens it here.
  function open(event: MouseEvent, id: number): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpen(id);
  }

  let sentinel = $state<HTMLElement>();

  // Ask for the next page when the end of the list comes near.
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

  function who(c: Conversation): string {
    const names = c.participants.map((p) =>
      p.email === account ? 'me' : p.name || p.email.split('@')[0] || p.email,
    );
    return names.slice(0, 3).join(', ') + (names.length > 3 ? ' …' : '');
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
</script>

<section class="list" aria-label="Conversations">
  {#if conversations.length === 0 && !loading}
    <p class="note">{strings.list.empty}</p>
  {/if}
  <ul>
    {#each conversations as c (c.id)}
      <li class:unread={c.unread} class:selected={c.id === selected}>
        <a
          class="row"
          href={path({ folder, conversation: c.id })}
          aria-current={c.id === selected ? 'true' : undefined}
          onclick={(e) => open(e, c.id)}
        >
        <div class="top">
          <span class="who">{who(c)}</span>
          {#if c.count > 1}<span class="count" aria-label={strings.list.messages(c.count)}>{c.count}</span>{/if}
          {#if c.attachments}<span class="mark" role="img" aria-label={strings.list.attachment}>📎</span>{/if}
          {#if c.flagged}<span class="mark flag" role="img" aria-label={strings.list.flagged}>⚑</span>{/if}
          <time datetime={c.date} title={long.format(new Date(c.date))}>{when(c.date)}</time>
        </div>
        <div class="subject">
          {#if c.unread}<span class="dot" role="img" aria-label={strings.list.unread}></span>{/if}
          {c.subject || strings.list.noSubject}
        </div>
        {#if c.snippet}<div class="snippet">{c.snippet}</div>{/if}
        </a>
      </li>
    {/each}
  </ul>
  {#if loading}<p class="note">{strings.list.loading}</p>{/if}
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

  .row {
    display: grid;
    gap: 0.15rem;
    padding: 0.65rem 1rem;
    color: inherit;
    text-decoration: none;
  }

  li:hover {
    background: var(--hover);
  }

  li.selected {
    background: var(--selected);
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

  .count {
    font-size: 0.8rem;
    color: var(--text-muted);
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
