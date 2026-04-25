<script lang="ts">
  import { onMount } from 'svelte';
  import {
    Header,
    MessageList,
    QuickActions,
    createHomelabdClient,
    type ChatTranscriptMessage,
    type ChatRole,
    type QuickAction
  } from '@homelab/shared';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const transcriptStorageKey = 'homelabd.dashboard.chatTranscript';
  const welcomeMessage: ChatTranscriptMessage = {
    id: 'welcome',
    role: 'assistant',
    content: 'Ready for homelabd.',
    source: 'program',
    actions: ['status', 'tasks', 'help'],
    time: 'Now'
  };

  let draft = '';
  let loading = false;
  let error = '';
  let messageId = 0;
  let inputEl: HTMLTextAreaElement | undefined;
  let messages: ChatTranscriptMessage[] = [welcomeMessage];

  const timeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit'
    });

  const taskRefPattern = /^(?:[a-f0-9]{6,12}|task_\d{8}_\d{6}_[a-f0-9]{8})$/i;
  const approvalRefPattern = /^approval_\d{8}_\d{6}_[a-f0-9]{8}$/i;

  const isTranscriptMessage = (value: unknown): value is ChatTranscriptMessage => {
    if (!value || typeof value !== 'object') {
      return false;
    }

    const candidate = value as Record<string, unknown>;
    const validRole = candidate.role === 'user' || candidate.role === 'assistant';
    const validActions =
      candidate.actions === undefined ||
      (Array.isArray(candidate.actions) &&
        candidate.actions.every((action) => typeof action === 'string'));
    const validSource = candidate.source === undefined || typeof candidate.source === 'string';

    return (
      typeof candidate.id === 'string' &&
      validRole &&
      typeof candidate.content === 'string' &&
      typeof candidate.time === 'string' &&
      validSource &&
      validActions
    );
  };

  const loadStoredMessages = () => {
    try {
      const stored = localStorage.getItem(transcriptStorageKey);
      if (!stored) {
        return [welcomeMessage];
      }

      const parsed = JSON.parse(stored);
      if (!Array.isArray(parsed)) {
        return [welcomeMessage];
      }

      const restored = parsed.filter(isTranscriptMessage);
      return restored.length > 0 ? restored : [welcomeMessage];
    } catch {
      return [welcomeMessage];
    }
  };

  const persistMessages = () => {
    try {
      localStorage.setItem(transcriptStorageKey, JSON.stringify(messages));
    } catch {
      // Storage may be unavailable or full; chat still works without persistence.
    }
  };

  const isSafeCommand = (command: string) => {
    if (!command || command.includes('<') || command.endsWith(':')) {
      return false;
    }

    const parts = command.split(/\s+/);
    const verb = parts[0]?.toLowerCase();

    if (parts.length === 1) {
      return ['help', 'tasks', 'status', 'agents', 'approvals'].includes(verb);
    }

    if (['show', 'run', 'work', 'start', 'review', 'diff', 'test', 'cancel', 'stop', 'delete', 'remove', 'rm'].includes(verb)) {
      return parts.length === 2 && taskRefPattern.test(parts[1]);
    }

    if (['approve', 'deny'].includes(verb)) {
      return parts.length === 2 && approvalRefPattern.test(parts[1]);
    }

    if (verb === 'delegate') {
      return parts.length >= 4 && taskRefPattern.test(parts[1]) && parts[2]?.toLowerCase() === 'to' && ['codex', 'claude', 'gemini'].includes(parts[3]?.toLowerCase());
    }

    return false;
  };

  const extractCommands = (content: string): string[] => {
    const commands = new Set<string>();
    const backticked = content.matchAll(/`([^`\n]+)`/g);

    for (const match of backticked) {
      const command = match[1].trim().replace(/\s+/g, ' ');
      if (isSafeCommand(command)) {
        commands.add(command);
      }
    }

    for (const line of content.split('\n')) {
      const command = line
        .trim()
        .replace(/^[>-]\s*/, '')
        .replace(/[.。]$/, '')
        .replace(/\s+/g, ' ');
      if (isSafeCommand(command)) {
        commands.add(command);
      }
    }

    return [...commands].slice(0, 6);
  };

  const focusInput = () => {
    requestAnimationFrame(() => inputEl?.focus());
  };

  const addMessage = (role: ChatRole, content: string, source?: string) => {
    messageId += 1;
    messages = [
      ...messages,
      {
        id: `${role}-${messageId}`,
        role,
        content,
        source,
        actions: role === 'assistant' ? extractCommands(content) : undefined,
        time: timeLabel()
      }
    ];
    persistMessages();
  };

  onMount(() => {
    messages = loadStoredMessages();
    messageId = messages.length;
  });

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

      addMessage('assistant', response.reply || 'No reply returned.', response.source || 'program');
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to reach homelabd.';
    } finally {
      loading = false;
      focusInput();
    }
  };

  const sendQuickAction = (action: QuickAction) => {
    void sendMessage(action);
  };

  const sendCommandAction = (command: string) => {
    void sendMessage(command);
  };

  const handleComposerKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void sendMessage();
    }
  };
</script>

<svelte:head>
  <title>homelabd Chat</title>
  <meta
    name="description"
    content="Simple homelabd chat dashboard"
  />
</svelte:head>

<div class="app-shell">
  <Header title="homelabd" subtitle="Dashboard" {apiBase} />

  <main>
    <section class="chat-shell">
      <MessageList {messages} {loading} disabled={loading} onAction={sendCommandAction} />

      {#if error}
        <p class="error" role="alert">{error}</p>
      {/if}

      <div class="composer">
        <QuickActions disabled={loading} onSelect={sendQuickAction} />

        <form on:submit|preventDefault={() => void sendMessage()}>
          <label for="message">Message</label>
          <textarea
            id="message"
            bind:this={inputEl}
            bind:value={draft}
            autocomplete="off"
            placeholder="Ask homelabd..."
            disabled={loading}
            rows="3"
            on:keydown={handleComposerKeydown}
          ></textarea>
          <button type="submit" disabled={loading || !draft.trim()}>
            {loading ? 'Sending' : 'Send'}
          </button>
        </form>
      </div>
    </section>
  </main>
</div>

<style>
  :global(html) {
    height: 100%;
    overflow: hidden;
  }

  :global(body) {
    margin: 0;
    height: 100%;
    overflow: hidden;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
    color: #111827;
    background: #eef2f7;
  }

  :global(body > div) {
    height: 100%;
  }

  .app-shell {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    height: 100dvh;
    overflow: hidden;
  }

  main {
    display: grid;
    min-height: 0;
    max-width: 980px;
    width: 100%;
    box-sizing: border-box;
    margin: 0 auto;
    padding: 1.25rem;
    overflow: hidden;
  }

  .chat-shell {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
    border: 1px solid #d5dde8;
    border-radius: 0.5rem;
    background: #f8fafc;
  }

  .composer {
    display: grid;
    flex: 0 0 auto;
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

  textarea {
    min-width: 0;
    min-height: 2.75rem;
    max-height: 10rem;
    box-sizing: border-box;
    padding: 0.75rem 0.9rem;
    border: 1px solid #b9c4d2;
    border-radius: 0.5rem;
    color: #111827;
    background: #ffffff;
    font: inherit;
    line-height: 1.4;
    resize: vertical;
    overflow-y: auto;
  }

  textarea:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  button {
    min-width: 5.5rem;
    min-height: 2.75rem;
    box-sizing: border-box;
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
      padding: 0.75rem;
    }

    .composer {
      padding: 0.75rem;
    }

    form {
      grid-template-columns: 1fr;
    }
  }
</style>
