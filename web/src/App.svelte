<script lang="ts">
  import { currentAccount, type Account } from './api';
  import Shell from './Shell.svelte';
  import SignIn from './SignIn.svelte';

  // undefined while the first check is in flight, so neither screen flashes.
  let account = $state<Account | null | undefined>(undefined);
  let notice = $state<'password_changed' | undefined>(undefined);

  currentAccount().then(
    (found) => (account = found),
    () => (account = null),
  );
</script>

{#if account === null}
  <SignIn
    {notice}
    onSignedIn={(a) => {
      notice = undefined;
      account = a;
    }}
  />
{:else if account}
  <Shell
    {account}
    onSignedOut={(reason) => {
      notice = reason;
      account = null;
    }}
  />
{/if}
