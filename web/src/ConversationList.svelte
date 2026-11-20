<script lang="ts">
  import type { Conversation } from './api';
  import { conversationType } from './FolderList.svelte';
  import { longDate, when, who } from './listFormat';
  import { path } from './route.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    conversations: Conversation[];
    loading: boolean;
    hasMore: boolean;
    folder: string;
    selected?: number;
    // The conversations ticked for an action or a drag.
    checked: number[];
    onCheck: (ids: number[]) => void;
    onMore: () => void;
    onOpen: (id: number) => void;
  }
  const { account, conversations, loading, hasMore, folder, selected, checked, onCheck, onMore, onOpen }: Props =
    $props();

  // A real link, so a middle click or "open in new tab" works; a plain click opens it here.
  // While anything is ticked (on a phone, after a long press), a tap ticks instead.
  function open(event: MouseEvent, id: number): void {
    if (event.button !== 0 || event.metaKey || event.ctrlKey || event.altKey) return;
    // The click a browser sends after a long press: the press has ticked the row already.
    if (Date.now() - longPressAt < 800) {
      event.preventDefault();
      return;
    }
    if (event.shiftKey || (checked.length > 0 && touch)) {
      event.preventDefault();
      tick(id, event.shiftKey);
      return;
    }
    event.preventDefault();
    onOpen(id);
  }

  // The row last ticked, where a shift-click range starts.
  let anchor: number | null = null;

  function tick(id: number, range: boolean): void {
    const ids = conversations.map((c) => c.id);
    const at = ids.indexOf(id);
    const from = anchor === null ? -1 : ids.indexOf(anchor);
    if (range && from >= 0 && at >= 0) {
      const span = ids.slice(Math.min(from, at), Math.max(from, at) + 1);
      onCheck([...new Set([...checked, ...span])]);
    } else {
      onCheck(checked.includes(id) ? checked.filter((c) => c !== id) : [...checked, id]);
    }
    anchor = id;
  }

  // At phone width a long press starts selection.
  let touch = false;
  let pressTimer: ReturnType<typeof setTimeout> | undefined;
  let longPressAt = 0;

  function pressStart(event: PointerEvent, id: number): void {
    touch = event.pointerType === 'touch';
    if (!touch) return;
    pressTimer = setTimeout(() => {
      longPressAt = Date.now();
      tick(id, false);
    }, 500);
  }

  function pressEnd(): void {
    clearTimeout(pressTimer);
  }

  function dragStart(event: DragEvent, id: number): void {
    if (!event.dataTransfer) return;
    const ids = checked.includes(id) ? checked : [id];
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData(conversationType, JSON.stringify({ folder, ids }));
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

</script>

<section class="list" aria-label="Conversations">
  {#if conversations.length === 0 && !loading}
    <p class="note">{strings.list.empty}</p>
  {/if}
  <ul>
    {#each conversations as c (c.id)}
      <li
        class:unread={c.unread}
        class:selected={c.id === selected}
        class:checked={checked.includes(c.id)}
        draggable="true"
        ondragstart={(e) => dragStart(e, c.id)}
      >
        <input
          type="checkbox"
          class="check"
          aria-label={strings.actions.select(c.subject || strings.list.noSubject)}
          checked={checked.includes(c.id)}
          onclick={(e) => {
            tick(c.id, e.shiftKey);
            // A range only ever ticks. The browser untoggled a box that was ticked already, and
            // the state did not change, so nothing else would put the tick back.
            if (e.shiftKey) e.currentTarget.checked = true;
          }}
        />
        <a
          class="row"
          href={path({ folder, conversation: c.id })}
          aria-current={c.id === selected ? 'true' : undefined}
          onclick={(e) => open(e, c.id)}
          onpointerdown={(e) => pressStart(e, c.id)}
          onpointerup={pressEnd}
          onpointercancel={pressEnd}
          onpointerleave={pressEnd}
          oncontextmenu={(e) => touch && e.preventDefault()}
          draggable="false"
        >
        <div class="top">
          <span class="who">{who(c.participants, account)}</span>
          {#if c.count > 1}<span class="count" aria-label={strings.list.messages(c.count)}>{c.count}</span>{/if}
          {#if c.attachments}<span class="mark" role="img" aria-label={strings.list.attachment}>📎</span>{/if}
          {#if c.flagged}<span class="mark flag" role="img" aria-label={strings.list.flagged}>⚑</span>{/if}
          <time datetime={c.date} title={longDate(c.date)}>{when(c.date)}</time>
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
    position: relative;
    border-bottom: 1px solid var(--border);
    min-width: 0;
  }

  li.checked {
    background: var(--selected);
  }

  .check {
    position: absolute;
    top: 0.8rem;
    left: 0.6rem;
    margin: 0;
    width: 1rem;
    height: 1rem;
    accent-color: var(--accent);
    opacity: 0;
  }

  li:hover .check,
  .check:focus-visible,
  .check:checked,
  .list:has(.check:checked) .check {
    opacity: 1;
  }

  @media (hover: none) {
    .check {
      display: none;
    }

    .list:has(li.checked) .check {
      display: block;
      opacity: 1;
    }
  }

  .row {
    display: grid;
    gap: 0.15rem;
    padding: 0.65rem 1rem 0.65rem 2.1rem;
    -webkit-touch-callout: none;
    color: inherit;
    text-decoration: none;
  }

  li:hover {
    background: var(--hover);
  }

  li.selected {
    background: var(--selected);
    box-shadow: inset 3px 0 var(--accent);
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
