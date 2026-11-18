<script lang="ts">
  import { signIn, type Account, type ErrorCode } from './api';
  import { strings } from './strings';

  interface Props {
    onSignedIn: (account: Account) => void;
  }
  const { onSignedIn }: Props = $props();

  let email = $state('');
  let password = $state('');
  let keep = $state(false);
  let working = $state(false);
  let error = $state<ErrorCode | null>(null);

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    working = true;
    error = null;
    const result = await signIn(email, password, keep);
    working = false;
    if (result.ok) {
      onSignedIn(result.value);
      return;
    }
    error = result.error;
    if (error === 'wrong_credentials') password = '';
  }
</script>

<main>
  <form onsubmit={submit} aria-labelledby="sign-in-title">
    <p class="brand">{strings.appName}</p>
    <h1 id="sign-in-title">{strings.signIn.title}</h1>

    <label>
      <span>{strings.signIn.email}</span>
      <!-- svelte-ignore a11y_autofocus -->
      <input type="email" name="email" autocomplete="username" required autofocus bind:value={email} />
    </label>

    <label>
      <span>{strings.signIn.password}</span>
      <input type="password" name="password" autocomplete="current-password" required bind:value={password} />
    </label>

    <label class="keep">
      <input type="checkbox" name="keep" bind:checked={keep} />
      <span>{strings.signIn.keep}</span>
    </label>

    {#if error}
      <p class="error-banner" role="alert">{strings.errors[error]}</p>
    {/if}

    <button type="submit" disabled={working}>
      {working ? strings.signIn.working : strings.signIn.submit}
    </button>
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
    border-radius: 1rem;
    background: var(--surface);
    box-shadow: var(--shadow);
    box-sizing: border-box;
  }

  .brand {
    margin: 0;
    font-weight: 650;
    color: var(--accent);
    letter-spacing: -0.01em;
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

  button:disabled {
    opacity: 0.7;
    cursor: progress;
  }
</style>
