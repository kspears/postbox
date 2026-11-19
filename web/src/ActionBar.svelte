<script lang="ts" module>
  export type Action = 'archive' | 'delete' | 'spam' | 'notSpam' | 'moveTo' | 'read' | 'unread' | 'flag' | 'unflag';
</script>

<script lang="ts">
  import type { Role } from './api';
  import { strings } from './strings';

  interface Props {
    // The open folder's role decides which actions make sense.
    role?: Role;
    unread: boolean;
    flagged: boolean;
    onAction: (action: Action) => void;
  }
  const { role, unread, flagged, onAction }: Props = $props();
</script>

<div class="actions" role="toolbar" aria-label="Conversation actions">
  {#if role !== 'archive'}
    <button type="button" onclick={() => onAction('archive')}>{strings.actions.archive}</button>
  {/if}
  <button type="button" onclick={() => onAction('delete')}>{strings.actions.delete}</button>
  {#if role === 'junk'}
    <button type="button" onclick={() => onAction('notSpam')}>{strings.actions.notSpam}</button>
  {:else if role !== 'trash'}
    <button type="button" onclick={() => onAction('spam')}>{strings.actions.spam}</button>
  {/if}
  <button type="button" onclick={() => onAction('moveTo')}>{strings.actions.moveTo}</button>
  <button type="button" onclick={() => onAction(unread ? 'read' : 'unread')}>
    {unread ? strings.actions.markRead : strings.actions.markUnread}
  </button>
  <button type="button" aria-pressed={flagged} onclick={() => onAction(flagged ? 'unflag' : 'flag')}>
    {flagged ? strings.actions.unflag : strings.actions.flag}
  </button>
</div>

<style>
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }

  button {
    border: 1px solid var(--border);
    border-radius: 999px;
    padding: 0.3rem 0.75rem;
    background: var(--surface);
    color: var(--text);
    font-size: 0.85rem;
    cursor: pointer;
  }

  button:hover {
    background: var(--hover);
  }

  button[aria-pressed='true'] {
    color: var(--danger);
  }
</style>
