<script lang="ts">
  import {
    cancelled,
    confirmPassword,
    createPasskey,
    deleteIdentity,
    passkeysSupported,
    removePasskey,
    saveIdentity,
    savePasskey,
    savePreferences,
    passkeys as fetchPasskeys,
    setRequirePasskey,
    unlockNewPasskey,
    type Identity,
    type NewPasskey,
    type Passkey,
    type Preferences,
  } from './api';
  import { mediumDate } from './listFormat';
  import type RichEditor from './RichEditor.svelte';
  import { strings } from './strings';

  interface Props {
    account: string;
    preferences: Preferences;
    identities: Identity[];
    onPreferences: (p: Preferences) => void;
    onIdentities: (list: Identity[]) => void;
    onClose: () => void;
  }
  const { account, preferences, identities, onPreferences, onIdentities, onClose }: Props = $props();

  let dialog = $state<HTMLDialogElement>();
  $effect(() => {
    dialog?.showModal();
    return () => dialog?.close();
  });

  let problem = $state('');

  async function choose(p: Preferences): Promise<void> {
    onPreferences(p);
    const result = await savePreferences(p);
    problem = result.ok ? '' : strings.settings.saveFailed;
  }

  // The identity being edited: an existing one by id, or a new one (id 0).
  let editing = $state<Identity | null>(null);
  let editor = $state<ReturnType<typeof RichEditor>>();
  const richReady = import('./RichEditor.svelte').then((m) => m.default);
  let invalid = $state(false);

  function edit(i: Identity): void {
    editing = { ...i };
    invalid = false;
  }

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!editing) return;
    const result = await saveIdentity({ ...editing, signature: editor?.html() ?? '' });
    if (!result.ok) {
      invalid = true;
      problem = strings.settings.identityFailed;
      return;
    }
    problem = '';
    const saved = result.value;
    onIdentities(identities.some((i) => i.id === saved.id) ? identities.map((i) => (i.id === saved.id ? saved : i)) : [...identities, saved]);
    editing = null;
  }

  async function remove(i: Identity): Promise<void> {
    const result = await deleteIdentity(i.id);
    if (!result.ok) {
      problem = strings.settings.identityFailed;
      return;
    }
    onIdentities(identities.filter((x) => x.id !== i.id));
  }

  // --- passkeys ---
  // Loaded fresh each time Settings opens.
  let passkeys = $state<Passkey[]>([]);
  let required = $state(false);
  $effect(() => {
    void fetchPasskeys().then((result) => {
      if (!result.ok) return;
      passkeys = result.value.passkeys;
      required = result.value.requirePasskey;
    });
  });

  const addedOn = (p: Passkey): string => mediumDate(p.added);

  // A passkey created whose PRF output still has to be asked for, in a second prompt.
  let unfinished = $state<NewPasskey | null>(null);
  let passkeyProblem = $state('');
  let working = $state(false);

  async function addPasskey(): Promise<void> {
    passkeyProblem = '';
    working = true;
    try {
      const created = await createPasskey().catch(() => null);
      if (created === null) {
        passkeyProblem = strings.settings.passkeyFailed;
        return;
      }
      if (created === cancelled) return;
      if (!created.ok) {
        if (created.error === 'confirm_needed') confirmFirst(addPasskey);
        else passkeyProblem = strings.settings.passkeyFailed;
        return;
      }
      // A provider with PRF that gave no output yet is asked once more, in a second prompt.
      if (created.value.prfEnabled && !created.value.prf) unfinished = created.value;
      else await keep(created.value);
    } finally {
      working = false;
    }
  }

  async function finishPasskey(): Promise<void> {
    if (!unfinished) return;
    working = true;
    try {
      const used = await unlockNewPasskey(unfinished).catch(() => null);
      if (used === cancelled) return;
      unfinished = null;
      // A provider that says it has PRF must give the key: never let it fall back to the server's.
      if (used === null || !used.prf) {
        passkeyProblem = used === null ? strings.settings.passkeyFailed : strings.settings.passkeyUnsupported;
        return;
      }
      await keep(used);
    } finally {
      working = false;
    }
  }

  async function keep(p: NewPasskey): Promise<void> {
    const saved = await savePasskey(p);
    if (saved.ok) passkeys = [...passkeys, saved.value];
    else passkeyProblem = saved.error === 'passkey_unsupported' ? strings.settings.passkeyUnsupported : strings.settings.passkeyFailed;
  }

  async function forget(p: Passkey): Promise<void> {
    const result = await removePasskey(p.id);
    if (!result.ok) {
      if (result.error === 'confirm_needed') confirmFirst(() => forget(p));
      else problem = strings.settings.saveFailed;
      return;
    }
    passkeys = passkeys.filter((x) => x.id !== p.id);
    required = result.value.requirePasskey;
  }

  async function require(on: boolean): Promise<void> {
    required = on;
    const result = await setRequirePasskey(on);
    if (result.ok) return;
    required = !on;
    if (result.error === 'confirm_needed') confirmFirst(() => require(on));
    else problem = strings.settings.saveFailed;
  }

  // --- the password again ---
  // Changing passkeys needs the password entered recently; the change waits here until it is.
  let waiting = $state<(() => Promise<void>) | null>(null);
  let confirmText = $state('');
  let confirmProblem = $state('');

  function confirmFirst(then: () => Promise<void>): void {
    waiting = then;
    confirmText = '';
    confirmProblem = '';
  }

  async function confirm(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const result = await confirmPassword(confirmText);
    if (!result.ok) {
      confirmText = '';
      confirmProblem =
        result.error === 'wrong_credentials'
          ? strings.settings.wrongPassword
          : (strings.errors[result.error as keyof typeof strings.errors] ?? strings.errors.internal);
      return;
    }
    const then = waiting;
    waiting = null;
    await then?.();
  }

  const appearances = [
    { value: 'system', label: strings.settings.system },
    { value: 'light', label: strings.settings.light },
    { value: 'dark', label: strings.settings.dark },
  ] as const;
  const modes = [
    { value: 'rich', label: strings.settings.rich },
    { value: 'plain', label: strings.settings.plain },
  ] as const;
</script>

<dialog bind:this={dialog} aria-label={strings.settings.heading} onclose={onClose}>
  <header>
    <h2>{strings.settings.heading}</h2>
    <button type="button" class="icon" aria-label={strings.compose.close} onclick={() => dialog?.close()}>×</button>
  </header>

  {#if problem}<p class="error-banner" role="alert">{problem}</p>{/if}

  <fieldset>
    <legend>{strings.settings.appearance}</legend>
    {#each appearances as a (a.value)}
      <label>
        <input type="radio" name="appearance" checked={preferences.appearance === a.value} onchange={() => choose({ ...preferences, appearance: a.value })} />
        {a.label}
      </label>
    {/each}
  </fieldset>

  <fieldset>
    <legend>{strings.settings.newMessages}</legend>
    {#each modes as m (m.value)}
      <label>
        <input type="radio" name="composer" checked={preferences.composer === m.value} onchange={() => choose({ ...preferences, composer: m.value })} />
        {m.label}
      </label>
    {/each}
  </fieldset>

  <section aria-labelledby="identities-heading">
    <h3 id="identities-heading">{strings.settings.identities}</h3>
    <ul class="identities">
      {#each identities as i (i.id)}
        <li>
          <span class="who">{i.name ? `${i.name} <${i.email}>` : i.email}</span>
          <button type="button" onclick={() => edit(i)} aria-label={strings.settings.edit(i.email)}>{strings.settings.editShort}</button>
          {#if i.email.toLowerCase() !== account.toLowerCase()}
            <button type="button" onclick={() => remove(i)} aria-label={strings.settings.remove(i.email)}>{strings.settings.removeShort}</button>
          {/if}
        </li>
      {/each}
    </ul>
    {#if !editing}
      <button type="button" class="add" onclick={() => edit({ id: 0, name: '', email: '', signature: '' })}>{strings.settings.addIdentity}</button>
    {/if}
  </section>

  {#if passkeysSupported}
    <section aria-labelledby="passkeys-heading">
      <h3 id="passkeys-heading">{strings.settings.passkeys}</h3>
      <ul class="identities">
        {#each passkeys as p (p.id)}
          <li>
            <span class="who">{strings.settings.passkeyAdded(addedOn(p))}</span>
            <button type="button" onclick={() => forget(p)} aria-label={strings.settings.removePasskey(addedOn(p))}>
              {strings.settings.removeShort}
            </button>
          </li>
        {/each}
      </ul>
      {#if passkeyProblem}<p class="error-banner" role="alert">{passkeyProblem}</p>{/if}
      {#if waiting}
        <form class="identity" onsubmit={confirm} aria-label={strings.settings.confirm}>
          <p class="note">{strings.settings.confirm}</p>
          <label>
            {strings.settings.confirmPassword}
            <!-- svelte-ignore a11y_autofocus -->
            <input type="password" autocomplete="current-password" required autofocus bind:value={confirmText} />
          </label>
          {#if confirmProblem}<p class="error-banner" role="alert">{confirmProblem}</p>{/if}
          <div class="buttons">
            <button type="submit" class="primary">{strings.settings.confirmContinue}</button>
            <button type="button" onclick={() => (waiting = null)}>{strings.settings.cancel}</button>
          </div>
        </form>
      {/if}
      {#if unfinished}
        <p class="note">{strings.settings.finishPasskey}</p>
        <button type="button" class="add" disabled={working} onclick={finishPasskey}>{strings.settings.finish}</button>
      {:else}
        <button type="button" class="add" disabled={working} onclick={addPasskey}>{strings.settings.addPasskey}</button>
      {/if}
      <label class="require">
        <input
          type="checkbox"
          role="switch"
          checked={required}
          disabled={passkeys.length === 0}
          onchange={(e) => require(e.currentTarget.checked)}
        />
        {strings.settings.requirePasskey}
      </label>
      <p class="note">{strings.settings.requireNote}</p>
    </section>
  {/if}

  {#if editing}
    <form class="identity" onsubmit={save} aria-label={editing.id ? strings.settings.edit(editing.email) : strings.settings.addIdentity}>
      <label>
        {strings.settings.name}
        <input bind:value={editing.name} />
      </label>
      <label>
        {strings.settings.email}
        <input
          type="email"
          required
          bind:value={editing.email}
          readonly={editing.id !== 0 && editing.email.toLowerCase() === account.toLowerCase()}
          aria-invalid={invalid}
        />
      </label>
      <div class="signature">
        <span class="label">{strings.settings.signature}</span>
        {#await richReady then Editor}
          {#key editing.id}
            <Editor
            bind:this={editor}
            content={editing.signature}
            label={strings.settings.signature}
            images={new Map()}
            onChange={() => {}}
            onImages={async () => []}
              onFiles={() => {}}
            />
          {/key}
        {/await}
      </div>
      <div class="buttons">
        <button type="submit" class="primary">{strings.settings.save}</button>
        <button type="button" onclick={() => (editing = null)}>{strings.settings.cancel}</button>
      </div>
    </form>
  {/if}
</dialog>

<style>
  .require {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .note {
    margin: 0.25rem 0 0.5rem;
    font-size: 0.85rem;
    color: var(--text-muted);
  }

  dialog {
    width: min(36rem, calc(100vw - 2rem));
    max-height: calc(100dvh - 2rem);
    padding: 1rem 1.25rem 1.25rem;
    border: none;
    border-radius: 0.9rem;
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow);
  }

  @media (max-width: 767px) {
    dialog {
      width: 100vw;
      max-width: 100vw;
      height: 100dvh;
      max-height: 100dvh;
      margin: 0;
      border-radius: 0;
    }
  }

  dialog::backdrop {
    background: light-dark(#0003, #0008);
  }

  header {
    display: flex;
    align-items: center;
  }

  h2 {
    flex: 1;
    margin: 0;
    font-size: 1.15rem;
  }

  h3 {
    margin: 1rem 0 0.5rem;
    font-size: 1rem;
  }

  fieldset {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
    margin: 1rem 0 0;
    padding: 0;
    border: none;
  }

  legend {
    width: 100%;
    margin-bottom: 0.4rem;
    font-weight: 600;
  }

  .identities {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .identities li {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.4rem 0;
    border-bottom: 1px solid var(--border);
  }

  .who {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .identity {
    display: flex;
    flex-direction: column;
    gap: 0.6rem;
    margin-top: 1rem;
    padding: 0.75rem;
    border: 1px solid var(--border);
    border-radius: 0.6rem;
  }

  .identity label {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    font-size: 0.9rem;
    color: var(--text-muted);
  }

  .identity input {
    padding: 0.45rem 0.55rem;
    border: 1px solid var(--border);
    border-radius: 0.4rem;
    background: var(--surface);
    color: var(--text);
  }

  .signature {
    display: flex;
    flex-direction: column;
    min-height: 10rem;
    font-size: 0.9rem;
  }

  .signature .label {
    color: var(--text-muted);
  }

  button {
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    padding: 0.35rem 0.8rem;
    background: var(--surface);
    color: var(--text);
    cursor: pointer;
  }

  .icon {
    border: none;
    font-size: 1.2rem;
    color: var(--text-muted);
  }

  .add {
    margin-top: 0.6rem;
  }

  .buttons {
    display: flex;
    gap: 0.5rem;
  }

  .primary {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--on-accent);
    font-weight: 600;
  }

  .error-banner {
    margin-top: 0.75rem;
  }
</style>
