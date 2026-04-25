<script lang="ts">
  import type { ChatTranscriptMessage } from './types';

  export let messages: ChatTranscriptMessage[] = [];
  export let loading = false;
</script>

<section class="messages" aria-live="polite">
  {#each messages as message (message.id)}
    <article class="message" class:user={message.role === 'user'}>
      <div class="meta">
        <span>{message.role === 'user' ? 'You' : 'homelabd'}</span>
        <time>{message.time}</time>
      </div>
      <p>{message.content}</p>
    </article>
  {/each}

  {#if loading}
    <article class="message pending">
      <div class="meta">
        <span>homelabd</span>
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

  p {
    margin: 0;
    color: #1f2937;
    line-height: 1.45;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
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
