<script lang="ts">
  import { tick } from 'svelte';
  import { addresses, type Person } from './api';
  import { formatAddresses, splitList } from './addresses';

  interface Props {
    id: string;
    value: string;
  }
  let { id, value = $bindable() }: Props = $props();

  // The addresses offered for the entry being typed: the text after the last comma or semicolon.
  let offers = $state<Person[]>([]);
  let active = $state(-1);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let asked = 0;

  function entry(): string {
    return splitList(value).at(-1)?.trim() ?? '';
  }

  function typed(): void {
    clearTimeout(timer);
    const q = entry();
    if (!q) {
      offers = [];
      return;
    }
    timer = setTimeout(async () => {
      const mine = ++asked;
      const found = await addresses(q);
      if (mine !== asked) return;
      offers = found.ok ? found.value : [];
      active = offers.length > 0 ? 0 : -1;
    }, 150);
  }

  async function pick(p: Person): Promise<void> {
    const earlier = splitList(value).slice(0, -1).map((e) => e.trim()).filter(Boolean);
    value = `${[...earlier, formatAddresses([p])].join(', ')}, `;
    offers = [];
    // The form saves the draft on input; picking is input too. Once the field shows the new
    // value, or the binding would read the old one back.
    await tick();
    input?.dispatchEvent(new Event('input', { bubbles: true }));
  }

  let input = $state<HTMLInputElement>();

  function keydown(event: KeyboardEvent): void {
    if (offers.length === 0) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      active = (active + step + offers.length) % offers.length;
    } else if ((event.key === 'Enter' || event.key === 'Tab') && active >= 0) {
      event.preventDefault();
      void pick(offers[active]!);
    } else if (event.key === 'Escape') {
      event.preventDefault();
      offers = [];
    }
  }
</script>

<div class="recipients">
  <input
    {id}
    bind:this={input}
    bind:value
    autocomplete="off"
    role="combobox"
    aria-autocomplete="list"
    aria-expanded={offers.length > 0}
    aria-controls="{id}-offers"
    aria-activedescendant={active >= 0 ? `${id}-offer-${active}` : undefined}
    oninput={typed}
    onkeydown={keydown}
    onblur={() => (offers = [])}
  />
  {#if offers.length > 0}
    <ul id="{id}-offers" role="listbox">
      {#each offers as p, i (p.email)}
        <li
          id="{id}-offer-{i}"
          role="option"
          aria-selected={i === active}
          onmousedown={(event) => {
            event.preventDefault();
            void pick(p);
          }}
        >
          {#if p.name}<span class="name">{p.name}</span>{/if}
          <span class="email">{p.email}</span>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .recipients {
    position: relative;
    flex: 1;
    min-width: 0;
    display: flex;
  }

  input {
    flex: 1;
    min-width: 0;
    border: none;
    background: transparent;
    color: var(--text);
    padding: 0.55rem 0;
  }

  input:focus-visible {
    outline: none;
  }

  ul {
    position: absolute;
    top: 100%;
    left: 0;
    right: 0;
    z-index: 2;
    margin: 0;
    padding: 0.25rem;
    list-style: none;
    border-radius: 0.5rem;
    background: var(--surface);
    box-shadow: var(--shadow), 0 0 0 1px var(--border);
  }

  li {
    display: flex;
    gap: 0.5rem;
    padding: 0.4rem 0.5rem;
    border-radius: 0.35rem;
    cursor: pointer;
  }

  li[aria-selected='true'] {
    background: var(--selected);
  }

  .email {
    color: var(--text-muted);
  }
</style>
