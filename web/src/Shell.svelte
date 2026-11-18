<script lang="ts">
  import { signOut, type Account } from './api';
  import { strings } from './strings';

  interface Props {
    account: Account;
    onSignedOut: () => void;
  }
  const { account, onSignedOut }: Props = $props();

  let menuOpen = $state(false);

  async function leave(everywhere: boolean): Promise<void> {
    menuOpen = false;
    await signOut(everywhere);
    onSignedOut();
  }

  function closeOnEscape(event: KeyboardEvent): void {
    if (event.key === 'Escape') menuOpen = false;
  }
</script>

<svelte:window onkeydown={closeOnEscape} />

<div class="shell">
  <header>
    <span class="brand">{strings.appName}</span>
    <div class="account">
      <button
        type="button"
        class="account-button"
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label={`${strings.account.menu}: ${account.account}`}
        onclick={() => (menuOpen = !menuOpen)}
      >
        {account.account}
      </button>
      {#if menuOpen}
        <div class="menu" role="menu">
          <button type="button" role="menuitem" onclick={() => leave(false)}>{strings.account.signOut}</button>
          <button type="button" role="menuitem" onclick={() => leave(true)}>{strings.account.signOutEverywhere}</button>
        </div>
      {/if}
    </div>
  </header>

  <main>
    <h1>{strings.folders.inbox}</h1>
    <p>{strings.inboxComingSoon}</p>
  </main>
</div>

<style>
  .shell {
    min-height: 100dvh;
    display: grid;
    grid-template-rows: auto 1fr;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.75rem 1rem;
    border-bottom: 1px solid var(--border);
  }

  .brand {
    font-weight: 650;
    color: var(--accent);
  }

  .account {
    position: relative;
  }

  .account-button {
    font: inherit;
    font-size: 0.9rem;
    color: var(--text);
    background: transparent;
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.35rem 0.85rem;
    cursor: pointer;
    max-width: 16rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 0.4rem);
    min-width: 12rem;
    display: grid;
    padding: 0.35rem;
    border-radius: 0.75rem;
    background: var(--surface);
    box-shadow: var(--shadow);
    z-index: 10;
  }

  .menu button {
    font: inherit;
    text-align: left;
    color: var(--text);
    background: transparent;
    border: none;
    border-radius: 0.5rem;
    padding: 0.55rem 0.75rem;
    cursor: pointer;
  }

  .menu button:hover,
  .menu button:focus-visible {
    background: var(--hover);
    outline: none;
  }

  main {
    padding: 1.5rem 1rem;
  }

  h1 {
    margin: 0 0 0.5rem;
    font-size: 1.4rem;
    font-weight: 650;
    letter-spacing: -0.02em;
  }

  p {
    margin: 0;
    color: var(--text-muted);
  }
</style>
