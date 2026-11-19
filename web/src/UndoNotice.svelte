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

  // Ten seconds, then the notice and its undo are gone (spec: no undo after that). The clock
  // stops while the pointer or the keyboard focus is on the notice, so reaching for Undo is
  // never a race.
  const shown = 10_000;
  let left = shown;
  let since = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function run(): void {
    since = Date.now();
    timer = setTimeout(onDone, left);
  }

  function pause(): void {
    if (timer === undefined) return;
    clearTimeout(timer);
    timer = undefined;
    left -= Date.now() - since;
  }

  function resume(): void {
    if (timer === undefined) run();
  }

  $effect(() => {
    void notice;
    left = shown;
    clearTimeout(timer);
    timer = undefined;
    run();
    return () => clearTimeout(timer);
  });
</script>

<div
  class="notice"
  role="status"
  onpointerenter={pause}
  onpointerleave={resume}
  onfocusin={pause}
  onfocusout={resume}
>
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
