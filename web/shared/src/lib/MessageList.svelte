<script lang="ts">
  import { tick } from 'svelte';
  import Markdown from './Markdown.svelte';
  import type { ChatTranscriptMessage } from './types';

  export let messages: ChatTranscriptMessage[] = [];
  export let loading = false;
  export let disabled = false;
  export let onAction: (command: string) => void = () => {};

  let messagesEl: HTMLElement | undefined;
  let lastMessageCount = messages.length;

  const sourceLabel = (source = 'program') => {
    switch (source.toLowerCase()) {
      case 'gemini':
        return 'Gemini';
      case 'openai':
        return 'OpenAI';
      default:
        return source;
    }
  };

  const scrollToBottom = () => {
    if (messagesEl) {
      messagesEl.scrollTop = messagesEl.scrollHeight;
    }
  };

  $: {
    const messageCount = messages.length;
    if (messageCount !== lastMessageCount) {
      lastMessageCount = messageCount;

      void tick().then(() => {
        scrollToBottom();
      });
    }
  }
</script>

<section class="messages" bind:this={messagesEl} aria-live="polite">
  {#each messages as message (message.id)}
    <article class="message" class:user={message.role === 'user'}>
      <div class="meta">
        <span>{message.role === 'user' ? 'You' : `homelabd - ${sourceLabel(message.source)}`}</span>
        <time>{message.time}</time>
      </div>
      <Markdown content={message.content} />
      {#if message.role === 'assistant' && message.actions?.length}
        <div class="actions" aria-label="Suggested commands">
          {#each message.actions as action}
            <button type="button" disabled={disabled} on:click={() => onAction(action)}>
              {action}
            </button>
          {/each}
        </div>
      {/if}
    </article>
  {/each}

  {#if loading}
    <article class="message pending">
      <div class="meta">
        <span>homelabd - working</span>
        <time>Now</time>
      </div>
      <p>Thinking...</p>
    </article>
  {/if}
</section>

<style>
  .messages {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 0.85rem;
    min-height: 0;
    overflow-y: auto;
    padding: 1rem;
  }

  .message {
    display: grid;
    gap: 0.4rem;
    max-width: min(42rem, 92%);
    padding: 0.85rem 0.95rem;
    border: 1px solid #d9e0ea;
    border-radius: 0.5rem;
    background: #ffffff;
    box-shadow: 0 1px 2px rgb(15 23 42 / 0.05);
  }

  .message.user {
    align-self: flex-end;
    border-color: #abc7b1;
    background: #f2faf3;
  }

  .message.pending {
    color: #4b5563;
  }

  .meta {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.75rem;
    color: #6b7280;
    font-size: 0.75rem;
  }

  .meta span {
    color: #111827;
    font-weight: 700;
  }

  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
    padding-top: 0.2rem;
  }

  button {
    min-height: 2rem;
    max-width: 100%;
    padding: 0 0.65rem;
    border: 1px solid #c5cfdb;
    border-radius: 0.4rem;
    color: #1f2937;
    background: #f8fafc;
    font: inherit;
    font-size: 0.82rem;
    font-weight: 700;
    cursor: pointer;
    overflow-wrap: anywhere;
  }

  button:hover:not(:disabled) {
    border-color: #637083;
    background: #eef4fb;
  }

  button:disabled {
    color: #9ca3af;
    cursor: not-allowed;
  }

  @media (max-width: 640px) {
    .messages {
      padding: 0.75rem;
    }

    .message {
      max-width: 100%;
    }
  }
</style>
