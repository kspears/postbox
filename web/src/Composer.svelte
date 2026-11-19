<script lang="ts">
  import { discardDraft, saveDraft, send, type ComposerView, type Draft, type ErrorCode } from './api';
  import { formatAddresses, parseAddresses } from './addresses';
  import { strings } from './strings';

  interface Props {
    view: ComposerView;
    // The server's words when the last attempt to send this message was refused.
    rejected?: string;
    onSent: (id: string) => void;
    onClose: () => void;
    onError: (error: ErrorCode) => void;
  }
  const { view, rejected, onSent, onClose, onError }: Props = $props();

  // The fields the person edits. Addresses are text until sent or saved.
  // svelte-ignore state_referenced_locally
  const start = view.draft;
  let to = $state(formatAddresses(start.to));
  let cc = $state(formatAddresses(start.cc));
  let bcc = $state(formatAddresses(start.bcc));
  let subject = $state(start.subject);
  let text = $state(start.text);
  let attachments = $state(start.attachments);
  let showCopies = $state(start.cc.length + start.bcc.length > 0);
  // Where the draft is now; each save replaces the previous copy.
  let uid = $state(start.uid);
  let messageId = start.messageId;

  // Ties each label to its field; unique, as two composers could share the page.
  const id = `composer-${crypto.randomUUID()}`;

  let problem = $state('');
  let saving = $state(false);
  let saved = $state(false);
  let busy = $state(false);

  function current(): Draft | null {
    const lists = [to, cc, bcc].map(parseAddresses);
    for (const l of lists) {
      if (!l.ok) {
        problem = strings.compose.badAddress(l.bad);
        return null;
      }
    }
    problem = '';
    const [t, c, b] = lists.map((l) => (l.ok ? l.people : []));
    return {
      ...start,
      uid,
      messageId,
      to: t!,
      cc: c!,
      bcc: b!,
      subject,
      text,
      attachments,
    };
  }

  // Saves the draft a little after typing stops, and whenever asked. One save at a time; a change
  // made while one runs is saved after it.
  let timer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;
  let running: Promise<void> = Promise.resolve();

  function changed(): void {
    dirty = true;
    saved = false;
    clearTimeout(timer);
    timer = setTimeout(() => void persist(), 2000);
  }

  function persist(): Promise<void> {
    clearTimeout(timer);
    running = running.then(async () => {
      if (!dirty) return;
      const draft = current();
      if (!draft) return;
      dirty = false;
      saving = true;
      const result = await saveDraft(draft);
      saving = false;
      if (!result.ok) {
        dirty = true;
        onError(result.error);
        return;
      }
      uid = result.value.uid;
      messageId = result.value.messageId;
      saved = true;
    });
    return running;
  }

  $effect(() => () => clearTimeout(timer));

  async function close(): Promise<void> {
    await persist();
    onClose();
  }

  async function discard(): Promise<void> {
    clearTimeout(timer);
    dirty = false;
    await running;
    if (uid) await discardDraft(uid);
    onClose();
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const draft = current();
    if (!draft) return;
    if (draft.to.length + draft.cc.length + draft.bcc.length === 0) {
      problem = strings.compose.noRecipients;
      return;
    }
    clearTimeout(timer);
    dirty = false;
    await running;
    busy = true;
    const result = await send(draft);
    busy = false;
    if (!result.ok) {
      onError(result.error);
      return;
    }
    onSent(result.value.id);
  }

  function size(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }
</script>

<section class="composer" aria-label={subject || strings.compose.newMessage}>
  <header>
    <h2>{subject || strings.compose.newMessage}</h2>
    <span class="state" aria-live="polite">{saving ? strings.compose.saving : saved ? strings.compose.saved : ''}</span>
    <button type="button" class="icon" aria-label={strings.compose.close} onclick={close}>×</button>
  </header>

  <form onsubmit={submit} oninput={changed}>
    {#if rejected}
      <p class="error-banner" role="alert">{strings.compose.rejected} {rejected}</p>
    {/if}
    <div class="field">
      <span class="label">{strings.compose.from}</span>
      <span class="from">{view.identities[0]?.email}</span>
    </div>
    <div class="field">
      <label class="label" for="{id}-to">{strings.compose.to}</label>
      <input id="{id}-to" autocomplete="off" bind:value={to} />
      {#if !showCopies}
        <button type="button" class="link" onclick={() => (showCopies = true)}>{strings.compose.showCcBcc}</button>
      {/if}
    </div>
    {#if showCopies}
      <div class="field">
        <label class="label" for="{id}-cc">{strings.compose.cc}</label>
        <input id="{id}-cc" autocomplete="off" bind:value={cc} />
      </div>
      <div class="field">
        <label class="label" for="{id}-bcc">{strings.compose.bcc}</label>
        <input id="{id}-bcc" autocomplete="off" bind:value={bcc} />
      </div>
    {/if}
    <div class="field">
      <label class="label" for="{id}-subject">{strings.compose.subject}</label>
      <input id="{id}-subject" bind:value={subject} />
    </div>
    <textarea aria-label={strings.compose.body} bind:value={text}></textarea>

    {#if attachments.length > 0}
      <ul class="attachments">
        {#each attachments as a, i (a.part ?? a.name + i)}
          <li>
            <span>{a.name}</span>
            <span class="size">{size(a.size)}</span>
            <button
              type="button"
              class="icon"
              aria-label={strings.compose.removeAttachment(a.name)}
              onclick={() => {
                attachments = attachments.filter((_, j) => j !== i);
                changed();
              }}>×</button
            >
          </li>
        {/each}
      </ul>
    {/if}

    {#if problem}<p class="error-banner" role="alert">{problem}</p>{/if}

    <div class="buttons">
      <button type="submit" class="send" disabled={busy}>{strings.compose.send}</button>
      <button type="button" onclick={discard}>{strings.compose.discard}</button>
    </div>
  </form>
</section>

<style>
  .composer {
    position: fixed;
    right: 1.25rem;
    bottom: 0;
    z-index: 15;
    width: min(38rem, calc(100vw - 2.5rem));
    height: min(36rem, calc(100dvh - 4rem));
    display: grid;
    grid-template-rows: auto 1fr;
    border-radius: 0.9rem 0.9rem 0 0;
    background: var(--surface);
    box-shadow: var(--shadow), 0 0 0 1px var(--border);
  }

  @media (max-width: 767px) {
    .composer {
      inset: 0;
      width: auto;
      height: auto;
      border-radius: 0;
    }
  }

  header {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.6rem 0.75rem 0.6rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  h2 {
    flex: 1;
    margin: 0;
    font-size: 1rem;
    font-weight: 650;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .state {
    font-size: 0.8rem;
    color: var(--text-muted);
  }

  form {
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 1rem 0.75rem;
  }

  .field {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    border-bottom: 1px solid var(--border);
  }

  .label {
    width: 3.75rem;
    flex: none;
    font-size: 0.9rem;
    color: var(--text-muted);
  }

  .from {
    padding: 0.55rem 0;
  }

  .field input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    color: var(--text);
    padding: 0.55rem 0;
  }

  .field input:focus-visible,
  textarea:focus-visible {
    outline: none;
  }

  textarea {
    flex: 1;
    min-height: 8rem;
    margin-top: 0.5rem;
    border: none;
    resize: none;
    background: transparent;
    color: var(--text);
    font: inherit;
    line-height: 1.5;
  }

  .attachments {
    list-style: none;
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    margin: 0.5rem 0 0;
    padding: 0;
  }

  .attachments li {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 0.2rem 0.3rem 0.2rem 0.65rem;
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    font-size: 0.9rem;
  }

  .size {
    color: var(--text-muted);
    font-size: 0.8rem;
  }

  .error-banner {
    margin: 0.5rem 0 0;
  }

  .buttons {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .buttons button {
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    padding: 0.5rem 1rem;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
  }

  .buttons .send {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }

  .icon,
  .link {
    border: none;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }

  .icon {
    font-size: 1.2rem;
    line-height: 1;
    padding: 0.2rem 0.4rem;
  }

  .link {
    color: var(--accent);
    font-size: 0.85rem;
    white-space: nowrap;
  }
</style>
