<script lang="ts">
  import type { Snippet } from 'svelte';
  import { attachmentURL, conversation, markRead, type ErrorCode, type Message, type Person } from './api';
  import MessageBody from './MessageBody.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    folder: string;
    id: number;
    onBack: () => void;
    onRead: (id: number) => void;
    onError: (error: ErrorCode) => void;
    // Changes when the conversation's row in the list changes, so it is fetched again.
    refresh: string;
    // The actions for this conversation.
    children?: Snippet;
  }
  const { account, folder, id, onBack, onRead, onError, refresh, children }: Props = $props();

  let messages = $state<Message[]>([]);
  let gone = $state(false);
  let allowedSenders = $state<string[]>([]);

  let shown = '';

  $effect(() => {
    const want = { folder, id };
    void refresh;
    let current = true;
    // A different conversation starts empty; the same one fetched again keeps what is on
    // screen until the new copy arrives.
    const key = `${want.folder}\n${want.id}`;
    if (key !== shown) {
      messages = [];
      gone = false;
      shown = key;
    }
    conversation(want.folder, want.id).then(async (result) => {
      if (!current) return;
      if (!result.ok) {
        if (result.error === 'not_found') gone = true;
        else onError(result.error);
        return;
      }
      messages = result.value;
      // Shown means read: mark this folder's unread messages of the conversation.
      const unread = messages.filter((m) => m.unread && m.folder === want.folder).map((m) => m.uid);
      if (unread.length === 0) return;
      const marked = await markRead(want.folder, unread);
      if (current && marked.ok) {
        for (const m of messages) if (unread.includes(m.uid)) m.unread = false;
        onRead(want.id);
      }
    });
    return () => (current = false);
  });

  function name(p: Person): string {
    if (p.email === account) return strings.reading.me;
    return p.name || p.email;
  }

  function names(people: Person[]): string {
    return people.map(name).join(', ');
  }

  const long = new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' });

  function size(bytes: number): string {
    // The encoded size; the file is about three quarters of it.
    const n = (bytes * 3) / 4;
    if (n < 1024) return `${Math.max(1, Math.round(n))} B`;
    if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
  }
</script>

<section class="reading" aria-label="Reading pane">
  <div class="bar">
    <button type="button" class="back" onclick={onBack}>← {strings.reading.back}</button>
    {@render children?.()}
  </div>

  {#if gone}
    <p class="note">{strings.reading.gone}</p>
  {:else if messages.length > 0}
    <h2>{messages[0]?.subject || strings.list.noSubject}</h2>
    {#each messages as m (m.folder + ':' + m.uid)}
      <article aria-label={`${name(m.from[0] ?? { name: '', email: '' })}, ${long.format(new Date(m.date))}`}>
        <header>
          <div class="from">
            <strong>{names(m.from)}</strong>
            {#if m.from[0] && m.from[0].email !== account && m.from[0].name}
              <span class="email">&lt;{m.from[0].email}&gt;</span>
            {/if}
          </div>
          <time datetime={m.date}>{long.format(new Date(m.date))}</time>
          {#if m.to.length > 0}
            <div class="to">{strings.reading.to}: {names(m.to)}</div>
          {/if}
          {#if m.cc.length > 0}
            <div class="to">{strings.reading.cc}: {names(m.cc)}</div>
          {/if}
        </header>

        <MessageBody
          message={m}
          senderAllowed={!!m.sender && allowedSenders.includes(m.sender)}
          onAllowSender={(s) => (allowedSenders = [...allowedSenders, s])}
        />

        {#if m.attachments.length > 0}
          <ul class="attachments" aria-label={strings.reading.attachments}>
            {#each m.attachments as a (a.part)}
              <li>
                <a href={attachmentURL(m, a.part)} download={a.name} aria-label={strings.reading.download(a.name)}>
                  <span class="file">{a.name}</span>
                  <span class="size">{size(a.size)}</span>
                </a>
              </li>
            {/each}
          </ul>
        {/if}
      </article>
    {/each}
  {:else}
    <p class="note">{strings.list.loading}</p>
  {/if}
</section>

<style>
  .reading {
    min-width: 0;
    overflow-y: auto;
    padding: 0 1.25rem 2rem;
  }

  .bar {
    position: sticky;
    top: 0;
    z-index: 1;
    padding: 0.6rem 0;
    background: var(--bg);
  }

  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem 1rem;
  }

  /* The back button is for phone width, where the list is not on screen beside this pane. */
  @media (min-width: 768px) {
    .back {
      display: none;
    }
  }

  .back {
    border: none;
    background: transparent;
    color: var(--accent);
    padding: 0.25rem 0;
    cursor: pointer;
  }

  h2 {
    margin: 0.25rem 0 1rem;
    font-size: 1.35rem;
    font-weight: 650;
    letter-spacing: -0.01em;
  }

  article {
    margin-bottom: 1rem;
    padding: 1rem;
    border-radius: 0.75rem;
    background: var(--surface);
    box-shadow: var(--shadow);
  }

  header {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.15rem 1rem;
    margin-bottom: 0.75rem;
  }

  .from {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .email,
  time,
  .to {
    color: var(--text-muted);
    font-size: 0.9rem;
  }

  .to {
    grid-column: 1 / -1;
  }

  .attachments {
    list-style: none;
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
    margin: 0.75rem 0 0;
    padding: 0;
  }

  .attachments a {
    display: flex;
    gap: 0.5rem;
    align-items: baseline;
    padding: 0.4rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    color: var(--text);
    text-decoration: none;
  }

  .size {
    font-size: 0.8rem;
    color: var(--text-muted);
  }
</style>
