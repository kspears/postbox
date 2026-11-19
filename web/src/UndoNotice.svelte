<script lang="ts" module>
  export interface Notice {
    text: string;
    undo?: () => void;
  }
</script>

<script lang="ts">
  import { strings } from './strings';

  interface Props {
    notice: Notice;
    onDone: () => void;
  }
  const { notice, onDone }: Props = $props();

  // Ten seconds, then the notice and its undo are gone (spec: no undo after that).
  $effect(() => {
    void notice;
    const timer = setTimeout(onDone, 10_000);
    return () => clearTimeout(timer);
  });
</script>

<div class="notice" role="status">
  <span>{notice.text}</span>
  {#if notice.undo}
    <button
      type="button"
      onclick={() => {
        notice.undo?.();
        onDone();
      }}>{strings.actions.undo}</button
    >
  {/if}
</div>

<style>
  .notice {
    position: fixed;
    left: 50%;
    bottom: 1.25rem;
    transform: translateX(-50%);
    z-index: 20;
    display: flex;
    align-items: center;
    gap: 1rem;
    max-width: calc(100vw - 2rem);
    padding: 0.65rem 0.75rem 0.65rem 1rem;
    border-radius: 0.75rem;
    background: var(--text);
    color: var(--bg);
    box-shadow: var(--shadow);
  }

  button {
    border: none;
    background: transparent;
    color: var(--accent-on-dark);
    font-weight: 650;
    cursor: pointer;
  }
</style>
