<script lang="ts">
  import {
    DEFAULT_HOMELABD_API_BASE,
    Header,
    MessageList,
    QuickActions,
    createHomelabdClient,
    type ChatTranscriptMessage,
    type ChatRole,
    type QuickAction
  } from '@homelab/shared';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || DEFAULT_HOMELABD_API_BASE;
  const client = createHomelabdClient({ baseUrl: apiBase });

  let draft = '';
  let loading = false;
  let error = '';
  let messageId = 0;
  let messages: ChatTranscriptMessage[] = [
    {
      id: 'welcome',
      role: 'assistant',
      content: 'Ready for homelabd.',
      time: 'Now'
    }
  ];

  const timeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit'
    });

  const addMessage = (role: ChatRole, content: string) => {
    messageId += 1;
    messages = [
      ...messages,
      {
        id: `${role}-${messageId}`,
        role,
        content,
        time: timeLabel()
      }
    ];
  };

  const sendMessage = async (content = draft) => {
    const trimmed = content.trim();

    if (!trimmed || loading) {
      return;
    }

    draft = '';
    error = '';
    addMessage('user', trimmed);
    loading = true;

    try {
      const response = await client.sendMessage({
        from: 'dashboard',
        content: trimmed
      });

      addMessage('assistant', response.reply || 'No reply returned.');
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to reach homelabd.';
    } finally {
      loading = false;
    }
  };

  const sendQuickAction = (action: QuickAction) => {
    void sendMessage(action);
  };
</script>

<svelte:head>
  <title>homelabd Chat</title>
  <meta
    name="description"
    content="Simple homelabd chat dashboard"
  />
</svelte:head>

<Header title="homelabd" subtitle="Dashboard" {apiBase} />

<main>
  <section class="chat-shell">
    <MessageList {messages} {loading} />

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <div class="composer">
      <QuickActions disabled={loading} onSelect={sendQuickAction} />

      <form on:submit|preventDefault={() => void sendMessage()}>
        <label for="message">Message</label>
        <input
          id="message"
          bind:value={draft}
          autocomplete="off"
          placeholder="Ask homelabd..."
          disabled={loading}
        />
        <button type="submit" disabled={loading || !draft.trim()}>
          {loading ? 'Sending' : 'Send'}
        </button>
      </form>
    </div>
  </section>
</main>

<style>
  :global(body) {
    margin: 0;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
    color: #111827;
    background: #eef2f7;
  }

  main {
    display: grid;
    min-height: calc(100vh - 82px);
    max-width: 980px;
    margin: 0 auto;
    padding: 1.25rem;
  }

  .chat-shell {
    display: flex;
    flex-direction: column;
    min-height: min(720px, calc(100vh - 122px));
    overflow: hidden;
    border: 1px solid #d5dde8;
    border-radius: 0.5rem;
    background: #f8fafc;
  }

  .composer {
    display: grid;
    gap: 0.75rem;
    padding: 1rem;
    border-top: 1px solid #d5dde8;
    background: #ffffff;
  }

  form {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.75rem;
  }

  label {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    white-space: nowrap;
  }

  input {
    min-width: 0;
    min-height: 2.75rem;
    padding: 0 0.9rem;
    border: 1px solid #b9c4d2;
    border-radius: 0.5rem;
    color: #111827;
    background: #ffffff;
    font: inherit;
  }

  input:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  button {
    min-width: 5.5rem;
    min-height: 2.75rem;
    padding: 0 1rem;
    border: 1px solid #1d4ed8;
    border-radius: 0.5rem;
    color: #ffffff;
    background: #2563eb;
    font: inherit;
    font-weight: 700;
    cursor: pointer;
  }

  button:hover:not(:disabled) {
    background: #1d4ed8;
  }

  button:disabled {
    border-color: #9ca3af;
    background: #9ca3af;
    cursor: not-allowed;
  }

  .error {
    margin: 0;
    padding: 0.75rem 1rem;
    border-top: 1px solid #fecaca;
    color: #991b1b;
    background: #fef2f2;
    overflow-wrap: anywhere;
  }

  @media (max-width: 640px) {
    main {
      min-height: calc(100vh - 110px);
      padding: 0.75rem;
    }

    .chat-shell {
      min-height: calc(100vh - 126px);
    }

    form {
      grid-template-columns: 1fr;
    }
  }
</style>
