<script lang="ts">
  import {
    cancelled,
    passkeyNewPassword,
    passkeySignIn,
    passkeysSupported,
    signIn,
    type Account,
    type ErrorCode,
  } from './api';
  import Brand from './Brand.svelte';
  import { strings } from './strings';

  interface Props {
    notice?: 'password_changed';
    onSignedIn: (account: Account) => void;
  }
  const { notice, onSignedIn }: Props = $props();

  let email = $state('');
  let password = $state('');
  let keep = $state(false);
  let working = $state(false);
  let error = $state<ErrorCode | null>(null);

  // A passkey sign-in whose stored password the mail server refused: the nonce that takes the
  // new one.
  let newPassword = $state<string | null>(null);

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    working = true;
    error = null;
    const result = newPassword ? await passkeyNewPassword(newPassword, password) : await signIn(email, password, keep);
    working = false;
    if (result.ok) {
      onSignedIn(result.value);
      return;
    }
    error = result.error;
    if (error === 'wrong_credentials') password = '';
    // The new-password step has run out: back to the start.
    if (newPassword && error === 'passkey_failed') newPassword = null;
  }

  async function withPasskey(): Promise<void> {
    working = true;
    error = null;
    try {
      const result = await passkeySignIn().catch(() => null);
      if (result === null) {
        error = 'passkey_failed';
        return;
      }
      if (result === cancelled) return;
      if (!result.ok) {
        error = result.error;
        return;
      }
      if ('newPassword' in result.value) {
        newPassword = result.value.newPassword;
        password = '';
        return;
      }
      onSignedIn(result.value);
    } finally {
      working = false;
    }
  }
</script>

<main>
  <form onsubmit={submit} aria-labelledby="sign-in-title">
    <Brand />
    <h1 id="sign-in-title">{strings.signIn.title}</h1>

    {#if newPassword}
      <p class="notice" role="status">{strings.signIn.newPassword}</p>
    {:else}
      {#if notice === 'password_changed' && !error}
        <p class="notice" role="status">{strings.signIn.passwordChanged}</p>
      {/if}

      <label>
        <span>{strings.signIn.email}</span>
        <!-- svelte-ignore a11y_autofocus -->
        <input type="email" name="email" autocomplete="username" required autofocus bind:value={email} />
      </label>
    {/if}

    <label>
      <span>{strings.signIn.password}</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input type="password" name="password" autocomplete="current-password" required autofocus={!!newPassword} bind:value={password} />
    </label>

    {#if !newPassword}
      <label class="keep">
        <input type="checkbox" name="keep" bind:checked={keep} />
        <span>{strings.signIn.keep}</span>
      </label>
    {/if}

    {#if error}
      <p class="error-banner" role="alert">{strings.errors[error as keyof typeof strings.errors] ?? strings.errors.internal}</p>
    {/if}

    <button type="submit" disabled={working}>
      {working ? strings.signIn.working : strings.signIn.submit}
    </button>
    {#if passkeysSupported && !newPassword}
      <button type="button" class="secondary" disabled={working} onclick={withPasskey}>{strings.signIn.passkey}</button>
    {/if}
  </form>
</main>

<style>
  main {
    min-height: 100dvh;
    display: grid;
    place-items: center;
    padding: 1rem;
    box-sizing: border-box;
  }

  form {
    width: min(100%, 22rem);
    display: grid;
    gap: 1rem;
    padding: 2rem;
    border: 1px solid var(--border);
    border-radius: 1rem;
    background: var(--surface);
    box-shadow: var(--shadow);
    box-sizing: border-box;
  }

  .notice {
    margin: 0;
    padding: 0.6rem 0.75rem;
    border-radius: 0.5rem;
    background: var(--hover);
    font-size: 0.9rem;
  }

  h1 {
    margin: 0 0 0.25rem;
    font-size: 1.5rem;
    font-weight: 650;
    letter-spacing: -0.02em;
  }

  label {
    display: grid;
    gap: 0.35rem;
    font-size: 0.9rem;
    color: var(--text-muted);
  }

  input[type='email'],
  input[type='password'] {
    font-size: 1rem;
    color: var(--text);
    background: var(--bg);
    border: 1px solid var(--border);
    border-radius: 0.5rem;
    padding: 0.6rem 0.75rem;
  }

  input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }

  .keep {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    color: var(--text);
  }

  .keep input {
    width: 1rem;
    height: 1rem;
    accent-color: var(--accent);
  }

  button {
    font-weight: 600;
    padding: 0.65rem 1rem;
    border: none;
    border-radius: 0.5rem;
    background: var(--accent);
    color: var(--on-accent);
    cursor: pointer;
  }

  button.secondary {
    background: transparent;
    color: var(--accent);
    border: 1px solid var(--border);
  }

  button:disabled {
    opacity: 0.7;
    cursor: progress;
  }
</style>
