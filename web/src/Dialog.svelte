<script lang="ts">
  import type { Snippet } from 'svelte';

  interface Props {
    label: string;
    onClose: () => void;
    children: Snippet;
  }
  const { label, onClose, children }: Props = $props();

  let dialog = $state<HTMLDialogElement>();

  // A modal <dialog> keeps focus inside, closes on Escape, and makes the rest of the page inert.
  $effect(() => {
    dialog?.showModal();
    return () => dialog?.close();
  });
</script>

<dialog bind:this={dialog} aria-label={label} onclose={onClose}>
  {@render children()}
</dialog>

<style>
  dialog {
    width: min(26rem, calc(100vw - 2rem));
    max-height: min(32rem, calc(100dvh - 2rem));
    padding: 1.25rem;
    border: none;
    border-radius: 0.9rem;
    background: var(--surface);
    color: var(--text);
    box-shadow: var(--shadow);
  }

  dialog::backdrop {
    background: light-dark(#0003, #0008);
  }
</style>
