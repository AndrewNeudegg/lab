<script lang="ts">
  import type { QuickAction } from './types';

  export let disabled = false;
  export let onSelect: (action: QuickAction) => void = () => {};

  const actions: QuickAction[] = ['help', 'status', 'agents'];
</script>

<div class="quick-actions" aria-label="Quick actions">
  {#each actions as action}
    <button type="button" {disabled} on:click={() => onSelect(action)}>
      {action}
    </button>
  {/each}
</div>

<style>
  .quick-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.5rem;
  }

  button {
    min-height: 2.25rem;
    padding: 0 0.8rem;
    border: 1px solid #c5cfdb;
    border-radius: 0.45rem;
    color: #1f2937;
    background: #ffffff;
    font: inherit;
    font-size: 0.9rem;
    font-weight: 700;
    cursor: pointer;
  }

  button:hover:not(:disabled) {
    border-color: #637083;
    background: #f5f8fb;
  }

  button:disabled {
    color: #9ca3af;
    cursor: not-allowed;
  }
</style>
