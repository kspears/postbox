<script lang="ts">
  import { discardDraft, saveDraft, send, type ComposerView, type Draft, type ErrorCode, type Person } from './api';
  import { formatAddresses, parseAddresses } from './addresses';
  import { fileSize, strings } from './strings';

  interface Props {
    view: ComposerView;
    // Why the last attempt to send this message failed.
    problem?: string;
    onSent: (id: string) => void;
    onClose: () => void;
    onError: (error: ErrorCode) => void;
  }
  const { view, problem: sendProblem, onSent, onClose, onError }: Props = $props();

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
    const people: Person[][] = [];
    for (const field of [to, cc, bcc]) {
      const l = parseAddresses(field);
      if (!l.ok) {
        problem = strings.compose.badAddress(l.bad);
        return null;
      }
      people.push(l.people);
    }
    problem = '';
    const [t = [], c = [], b = []] = people;
    return {
      ...start,
      uid,
      messageId,
      to: t,
      cc: c,
      bcc: b,
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
    // While sending, a save would race the send's own; a failed send saves what changed.
    if (!busy) timer = setTimeout(() => void persist(), 2000);
  }

  // Saves what changed, if anything, and reports whether the draft on the server is now current.
  function persist(): Promise<boolean> {
    clearTimeout(timer);
    const result = running.then(async () => {
      if (!dirty) return true;
      const draft = current();
      if (!draft) return false;
      dirty = false;
      saving = true;
      const result = await saveDraft(draft);
      saving = false;
      if (!result.ok) {
        dirty = true;
        onError(result.error);
        return false;
      }
      uid = result.value.uid;
      messageId = result.value.messageId;
      // The attachments now live in the new copy; the one they pointed into is gone. Any added
      // or removed meanwhile keep their place.
      const now = result.value.attachments;
      attachments = attachments.map((a) => {
        const i = draft.attachments.indexOf(a);
        return i >= 0 ? (now[i] ?? a) : a;
      });
      saved = true;
      return true;
    });
    running = result.then(() => undefined);
    return result;
  }

  // Leaving the page with unsaved changes asks first; closing the composer some other way (a new
  // one opening in its place) saves them.
  $effect(() => {
    const warn = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    addEventListener('beforeunload', warn);
    return () => {
      removeEventListener('beforeunload', warn);
      clearTimeout(timer);
      if (dirty && !busy) void persist();
    };
  });

  // Closes once the draft is safe in Drafts; otherwise the composer stays open, saying why.
  async function close(): Promise<void> {
    if (await persist()) {
      onClose();
      return;
    }
    if (!problem) problem = strings.compose.unsaved;
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
    clearTimeout(timer);
    busy = true;
    // A save in flight changes where the draft is; send from there.
    await running;
    const draft = current();
    if (!draft || draft.to.length + draft.cc.length + draft.bcc.length === 0) {
      if (draft) problem = strings.compose.noRecipients;
      busy = false;
      return;
    }
    dirty = false;
    const result = await send(draft);
    busy = false;
    if (!result.ok) {
      changed();
      onError(result.error);
      return;
    }
    // Sent: anything typed since is not saved over the draft the outbox is about to send.
    dirty = false;
    onSent(result.value.id);
  }
</script>

<section class="composer" aria-label={subject || strings.compose.newMessage}>
  <header>
    <h2>{subject || strings.compose.newMessage}</h2>
    <span class="state" aria-live="polite">{saving ? strings.compose.saving : saved ? strings.compose.saved : ''}</span>
    <button type="button" class="icon" aria-label={strings.compose.close} onclick={close}>×</button>
  </header>

  <form onsubmit={submit} oninput={changed}>
    {#if sendProblem}
      <p class="error-banner" role="alert">{sendProblem}</p>
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
            <span class="size">{fileSize(a.size)}</span>
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
