<script lang="ts">
  import {
    discardDraft,
    partURL,
    saveDraft,
    send,
    type ComposerView,
    type Draft,
    type DraftAttachment,
    type ErrorCode,
    type Identity,
    type Person,
    type Preferences,
  } from './api';
  import { untrack } from 'svelte';
  import { SvelteMap } from 'svelte/reactivity';
  import { formatAddresses, parseAddresses } from './addresses';
  import RecipientInput from './RecipientInput.svelte';
  import type RichEditor from './RichEditor.svelte';
  import { htmlToText, plainSignature, richSignature, textToHtml } from './rich';
  import { fileSize, strings } from './strings';

  interface Props {
    view: ComposerView;
    preferences: Preferences;
    // Why the last attempt to send this message failed.
    problem?: string;
    onSent: (id: string) => void;
    onClose: () => void;
    onError: (error: ErrorCode) => void;
  }
  const { view, preferences, problem: sendProblem, onSent, onClose, onError }: Props = $props();

  // The fields the person edits. Addresses are text until sent or saved.
  // svelte-ignore state_referenced_locally
  const start = view.draft;
  // svelte-ignore state_referenced_locally
  const identities = view.identities;
  let to = $state(formatAddresses(start.to));
  let cc = $state(formatAddresses(start.cc));
  let bcc = $state(formatAddresses(start.bcc));
  let subject = $state(start.subject);
  let attachments = $state(start.attachments);
  let showCopies = $state(start.cc.length + start.bcc.length > 0);
  let fromId = $state((identities.find((i) => i.email.toLowerCase() === start.from.email.toLowerCase()) ?? identities[0])?.id);
  const identity = (idNumber: number | undefined): Identity | undefined => identities.find((i) => i.id === idNumber);
  // Where the draft is now; each save replaces the previous copy.
  let uid = $state(start.uid);
  let messageId = start.messageId;

  // A reopened draft keeps its mode and has its signature already; a new message, reply or
  // forward starts in the chosen mode with the sender's signature.
  const fresh = !start.uid;
  // svelte-ignore state_referenced_locally
  let mode = $state<'rich' | 'plain'>(fresh ? preferences.composer : start.html ? 'rich' : 'plain');
  const startMode = untrack(() => mode);
  const signature = identity(untrack(() => fromId))?.signature ?? '';
  let text = $state(fresh && startMode === 'plain' ? plainSignature(signature) + start.text : start.text);
  let richStart = $state(fresh ? withSignature(start.html ?? '', signature) : start.html || textToHtml(start.text));
  let editor = $state<ReturnType<typeof RichEditor>>();

  function withSignature(html: string, sig: string): string {
    const rest = html.startsWith('<p></p>') ? html.slice('<p></p>'.length) : html;
    return `<p></p>${richSignature(sig)}${rest}`;
  }

  // A message names its own parts' types; only the bitmap kinds the server renders are passed on.
  const bitmapTypes = ['image/png', 'image/gif', 'image/jpeg', 'image/webp', 'image/bmp'];
  const bitmapType = (type: string): string => (bitmapTypes.includes(type) ? type : 'application/octet-stream');

  // Where each inline image shows from, by Content-ID; images added here are local files.
  const images = new SvelteMap<string, string>();
  // Images already in a reopened draft are fetched before the editor shows them.
  const imagesReady = Promise.all(
    start.attachments
      .filter((a) => a.inline && a.contentId && a.folder && a.uid && a.part)
      .map(async (a) => {
        try {
          const response = await fetch(partURL(a.folder!, a.uid!, a.part!), { credentials: 'same-origin' });
          if (response.ok) images.set(a.contentId!, URL.createObjectURL(new Blob([await response.blob()], { type: bitmapType(a.type) })));
        } catch {
          // The image shows empty; it is still sent.
        }
      }),
  );
  $effect(() => () => images.forEach((url) => URL.revokeObjectURL(url)));
  // The editor is loaded with the first composer, not with the page.
  const richReady = Promise.all([import('./RichEditor.svelte'), imagesReady]).then(([m]) => m.default);

  // Ties each label to its field; unique, as two composers could share the page.
  const id = `composer-${crypto.randomUUID()}`;

  let problem = $state('');
  // Why files the person just added were refused; kept until they add more.
  let refused = $state('');
  let saving = $state(false);
  let saved = $state(false);
  let busy = $state(false);
  let dragging = $state(false);
  let picker = $state<HTMLInputElement>();

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
    // Until the editor has loaded, the body is still what it started as.
    const html = mode === 'rich' ? (editor ? editor.html() : richStart) : '';
    // Images taken out of the body are not sent.
    const kept = attachments.filter((a) => !(mode === 'rich' && a.inline && a.contentId && !html.includes(`cid:${a.contentId}`)));
    const from = identity(fromId);
    return {
      ...start,
      uid,
      messageId,
      from: from ? { name: from.name, email: from.email } : start.from,
      to: t,
      cc: c,
      bcc: b,
      subject,
      text: mode === 'plain' ? text : '',
      html,
      attachments: kept,
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

  // Changing From swaps the signature for the new identity's.
  function fromChanged(previous: number | undefined): void {
    const before = identity(previous)?.signature ?? '';
    const after = identity(fromId)?.signature ?? '';
    if (mode === 'rich') {
      editor?.setSignature(after);
    } else {
      const old = plainSignature(before);
      if (old && text.includes(old)) text = text.replace(old, plainSignature(after));
      else text = withPlainSignature(text, plainSignature(after));
    }
    changed();
  }

  // Adds a signature below the person's own words: above a quoted original, else at the end.
  function withPlainSignature(body: string, sig: string): string {
    if (!sig) return body;
    const lines = body.split('\n');
    const quote = lines.findIndex((l) => l.startsWith('>'));
    if (quote < 0) return `${body.trimEnd()}\n${sig}`;
    // The line before a quote says who wrote it.
    const at = Math.max(0, quote - 1);
    return [lines.slice(0, at).join('\n').trimEnd(), sig, '', ...lines.slice(at)].join('\n');
  }

  function switchMode(): void {
    if (mode === 'rich') {
      text = htmlToText(editor?.html() ?? '');
      // Plain text cannot show images; they stay with the message as attachments.
      attachments = attachments.map((a) => (a.inline ? { ...a, inline: false } : a));
      mode = 'plain';
    } else {
      richStart = textToHtml(text);
      mode = 'rich';
    }
    changed();
  }

  // --- files ---

  // About how large the message will be once sent: attachments grow by a third in base64.
  function encodedSize(): number {
    const files = attachments.reduce((sum, a) => sum + a.size, 0);
    return Math.ceil((files * 4) / 3) + text.length + (editor?.html().length ?? 0);
  }

  function read(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const r = new FileReader();
      r.onload = () => resolve(String(r.result).replace(/^data:[^,]*,/, ''));
      r.onerror = () => reject(r.error ?? new Error('read failed'));
      r.readAsDataURL(file);
    });
  }

  /** Attaches files, refusing any that would take the message past the server's limit. Inline
   * ones are images for the body; the answer says where each accepted one shows from. */
  async function attach(files: File[], inline = false): Promise<{ contentId: string; url: string }[]> {
    const placed: { contentId: string; url: string }[] = [];
    refused = '';
    for (const file of files) {
      if (view.sizeLimit > 0 && encodedSize() + Math.ceil((file.size * 4) / 3) > view.sizeLimit) {
        refused = strings.compose.tooLarge(fileSize(view.sizeLimit));
        continue;
      }
      let data: string;
      try {
        data = await read(file);
      } catch {
        continue;
      }
      const a: DraftAttachment = { name: file.name || 'image', type: file.type || 'application/octet-stream', size: file.size, data };
      if (inline) {
        a.inline = true;
        a.contentId = `${crypto.randomUUID()}@postbox`;
        const url = URL.createObjectURL(file);
        images.set(a.contentId, url);
        placed.push({ contentId: a.contentId, url });
      }
      attachments = [...attachments, a];
      changed();
    }
    return placed;
  }

  function dropped(event: DragEvent): void {
    dragging = false;
    // The editor takes drops into the body itself.
    if (event.defaultPrevented) return;
    const files = Array.from(event.dataTransfer?.files ?? []);
    if (files.length === 0) return;
    event.preventDefault();
    void attach(files);
  }

  function dragover(event: DragEvent): void {
    if (!event.dataTransfer?.types.includes('Files')) return;
    event.preventDefault();
    dragging = true;
  }

  // Images in the body show there; the list holds the rest.
  const shown = $derived(attachments.filter((a) => !(mode === 'rich' && a.inline)));
</script>

<section
  class="composer"
  class:dragging
  aria-label={subject || strings.compose.newMessage}
  ondragover={dragover}
  ondragleave={(event) => {
    if (event.currentTarget === event.target) dragging = false;
  }}
  ondrop={dropped}
>
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
      <label class="label" for="{id}-from">{strings.compose.from}</label>
      {#if identities.length > 1}
        <select
          id="{id}-from"
          value={fromId}
          onchange={(event) => {
            const previous = fromId;
            fromId = Number(event.currentTarget.value);
            fromChanged(previous);
          }}
        >
          {#each identities as i (i.id)}
            <option value={i.id}>{i.name ? `${i.name} <${i.email}>` : i.email}</option>
          {/each}
        </select>
      {:else}
        <output id="{id}-from" class="from">{identities[0]?.email}</output>
      {/if}
    </div>
    <div class="field">
      <label class="label" for="{id}-to">{strings.compose.to}</label>
      <RecipientInput id="{id}-to" bind:value={to} />
      {#if !showCopies}
        <button type="button" class="link" onclick={() => (showCopies = true)}>{strings.compose.showCcBcc}</button>
      {/if}
    </div>
    {#if showCopies}
      <div class="field">
        <label class="label" for="{id}-cc">{strings.compose.cc}</label>
        <RecipientInput id="{id}-cc" bind:value={cc} />
      </div>
      <div class="field">
        <label class="label" for="{id}-bcc">{strings.compose.bcc}</label>
        <RecipientInput id="{id}-bcc" bind:value={bcc} />
      </div>
    {/if}
    <div class="field">
      <label class="label" for="{id}-subject">{strings.compose.subject}</label>
      <input id="{id}-subject" bind:value={subject} />
    </div>
    {#if mode === 'rich'}
      {#await richReady then Editor}
        {#key richStart}
          <Editor
            bind:this={editor}
            content={richStart}
            label={strings.compose.body}
            {images}
            onChange={changed}
            onImages={(files) => attach(files, true)}
            onFiles={(files) => void attach(files)}
          />
        {/key}
      {/await}
    {:else}
      <textarea aria-label={strings.compose.body} bind:value={text}></textarea>
    {/if}

    {#if shown.length > 0}
      <ul class="attachments" aria-label={strings.reading.attachments}>
        {#each shown as a, i (a.contentId ?? a.part ?? a.name + i)}
          <li>
            <span>{a.name}</span>
            <span class="size">{fileSize(a.size)}</span>
            <button
              type="button"
              class="icon"
              aria-label={strings.compose.removeAttachment(a.name)}
              onclick={() => {
                attachments = attachments.filter((x) => x !== a);
                changed();
              }}>×</button
            >
          </li>
        {/each}
      </ul>
    {/if}

    {#if problem}<p class="error-banner" role="alert">{problem}</p>{/if}
    {#if refused}<p class="error-banner" role="alert">{refused}</p>{/if}

    <div class="buttons">
      <button type="submit" class="send" disabled={busy}>{strings.compose.send}</button>
      <button type="button" onclick={() => picker?.click()}>{strings.compose.attach}</button>
      <input
        bind:this={picker}
        type="file"
        multiple
        hidden
        onchange={(event) => {
          const files = Array.from(event.currentTarget.files ?? []);
          event.currentTarget.value = '';
          void attach(files);
        }}
      />
      <button type="button" class="mode" onclick={switchMode}>{mode === 'rich' ? strings.compose.toPlain : strings.compose.toRich}</button>
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

  .buttons .mode {
    margin-left: auto;
  }

  .composer.dragging {
    box-shadow: var(--shadow), 0 0 0 2px var(--accent);
  }

  select {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    color: var(--text);
    padding: 0.55rem 0;
    font: inherit;
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
