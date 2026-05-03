<script lang="ts">
  import { Markdown } from '@homelab/shared';

  export let items: string[] = [];
  export let label = 'Research prompts';
  export let actionLabel = 'Research this';
  export let disabled = false;
  export let onResearch: (prompt: string) => void = () => {};

  let prompts: string[] = [];

  $: prompts = items.map((item) => item.trim()).filter(Boolean);
</script>

{#if prompts.length}
  <ul class="research-prompt-list" aria-label={label}>
    {#each prompts as prompt}
      <li>
        <div class="research-prompt-row">
          <div class="research-prompt-text">
            <Markdown content={prompt} />
          </div>
          <button
            type="button"
            class="research-prompt-action"
            disabled={disabled}
            aria-label={`${actionLabel}: ${prompt}`}
            title={actionLabel}
            on:click={() => onResearch(prompt)}
          >
            <span>{actionLabel}</span>
            <span aria-hidden="true">+</span>
          </button>
        </div>
      </li>
    {/each}
  </ul>
{/if}

<style>
  .research-prompt-list {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
    max-width: 100%;
    margin: 0.65rem 0 0;
    padding-left: 1.2rem;
  }

  .research-prompt-list li {
    min-width: 0;
    padding-left: 0.1rem;
    color: var(--knowledge-muted, #475569);
  }

  .research-prompt-row {
    display: flex;
    align-items: flex-start;
    gap: 0.45rem;
    min-width: 0;
    max-width: 100%;
  }

  .research-prompt-text {
    min-width: 0;
    flex: 1 1 auto;
    color: var(--text, #172033);
    font-size: 0.9rem;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }

  .research-prompt-text :global(.markdown) {
    color: inherit;
    font-size: inherit;
    line-height: inherit;
  }

  .research-prompt-text :global(.markdown p) {
    margin: 0;
  }

  .research-prompt-action {
    display: inline-flex;
    flex: 0 0 auto;
    align-items: center;
    gap: 0.25rem;
    min-height: 1.75rem;
    max-width: 9rem;
    padding: 0.25rem 0.45rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--primary, #2563eb);
    background: var(--panel, #ffffff);
    font-size: 0.78rem;
    font-weight: 850;
    line-height: 1.1;
  }

  .research-prompt-action:hover,
  .research-prompt-action:focus-visible {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .research-prompt-action:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }

  @media (max-width: 540px) {
    .research-prompt-row {
      gap: 0.35rem;
    }

    .research-prompt-action {
      max-width: 7.75rem;
      padding-inline: 0.4rem;
    }
  }
</style>
