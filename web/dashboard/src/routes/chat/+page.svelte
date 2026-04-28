<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    createHomelabdClient,
    Markdown,
    Navbar,
    persistChatDraft,
    readStoredChatDraft,
    type ChatRole,
    type ChatTranscriptMessage
  } from '@homelab/shared';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const transcriptStorageKey = 'homelabd.dashboard.chatTranscript.v4';
  const taskRefPattern = /(?:[a-f0-9]{6,12}|task_\d{8}_\d{6}_[a-f0-9]{8})/i;
  const workflowRefPattern = /(?:[a-f0-9]{6,12}|workflow_\d{8}_\d{6}_[a-f0-9]{8})/i;
  const approvalRefPattern = /approval_\d{8}_\d{6}_[a-f0-9]{8}/i;

  type PromptAction = {
    label: string;
    command: string;
    hint: string;
  };

  const promptActions: PromptAction[] = [
    {
      label: 'Brief me',
      command: 'brief me on what needs my attention',
      hint: 'Queue summary'
    },
    {
      label: 'Start work',
      command: 'create a task to ',
      hint: 'Describe outcome'
    },
    {
      label: 'Reflect',
      command: 'reflect on our recent interaction and suggest one improvement',
      hint: 'Improve process'
    }
  ];

  const welcomeMessage: ChatTranscriptMessage = {
    id: 'welcome',
    role: 'assistant',
    content: 'This is global chat. Use it for direction, planning, workflows, and broad commands. Use Tasks for queue state and task-specific actions.',
    source: 'program',
    actions: ['tasks'],
    time: 'Now'
  };

  let draft = '';
  let loading = false;
  let error = '';
  let messageId = 0;
  let inputEl: HTMLTextAreaElement | undefined;
  let messagesEl: HTMLElement | undefined;
  let messages: ChatTranscriptMessage[] = [welcomeMessage];

  const timeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit'
    });

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

    return (
      typeof candidate.id === 'string' &&
      validRole &&
      typeof candidate.content === 'string' &&
      typeof candidate.time === 'string' &&
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
      localStorage.setItem(transcriptStorageKey, JSON.stringify(messages.slice(-120)));
    } catch {
      error = 'Chat history could not be persisted locally.';
    }
  };

  const isSafeCommand = (command: string) => {
    const normalized = command.trim().replace(/\s+/g, ' ');
    if (!normalized || normalized.includes('<') || normalized.length > 260) {
      return false;
    }
    const verb = normalized.split(' ')[0]?.toLowerCase() || '';
    if (['tasks', 'workflows', 'status', 'approvals', 'agents', 'help'].includes(normalized.toLowerCase())) {
      return true;
    }
    if (verb === 'workflow') {
      const action = normalized.split(' ')[1]?.toLowerCase() || '';
      if (['list', 'ls'].includes(action)) {
        return true;
      }
      if (['show', 'run', 'start'].includes(action)) {
        return workflowRefPattern.test(normalized);
      }
      if (['new', 'create'].includes(action)) {
        return normalized.length > 'workflow new '.length;
      }
    }
    if (['new', 'task'].includes(verb)) {
      return normalized.length > verb.length + 1;
    }
    if (['show', 'run', 'ux', 'review', 'diff', 'test', 'delete', 'accept', 'verify'].includes(verb)) {
      return taskRefPattern.test(normalized);
    }
    if (verb === 'delegate') {
      const parts = normalized.split(' ');
      return (
        parts.length >= 4 &&
        taskRefPattern.test(parts[1]) &&
        parts[2]?.toLowerCase() === 'to' &&
        ['codex', 'claude', 'gemini', 'ux'].includes(parts[3]?.toLowerCase())
      );
    }
    if (['approve', 'deny'].includes(verb)) {
      return approvalRefPattern.test(normalized);
    }
    return false;
  };

  const extractCommands = (content: string): string[] => {
    const commands = new Set<string>();
    for (const match of content.matchAll(/`([^`\n]+)`/g)) {
      const command = match[1].trim().replace(/\s+/g, ' ');
      if (isSafeCommand(command)) {
        commands.add(command);
      }
    }
    return [...commands].slice(0, 5);
  };

  const focusInput = () => {
    requestAnimationFrame(() => inputEl?.focus());
  };

  const scrollMessages = () => {
    void tick().then(() => {
      if (messagesEl) {
        messagesEl.scrollTop = messagesEl.scrollHeight;
      }
    });
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
    scrollMessages();
  };

  onMount(() => {
    messages = loadStoredMessages();
    draft = inputEl?.value || readStoredChatDraft();
    persistChatDraft(draft);
    messageId = messages.length;
    scrollMessages();
    focusInput();
  });

  const sendMessage = async (content = draft) => {
    const trimmed = content.trim();
    if (!trimmed || loading) {
      return;
    }

    draft = '';
    persistChatDraft(draft);
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

  const sendCommand = (command: string) => {
    void sendMessage(command);
  };

  const handleComposerKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      void sendMessage();
    }
  };

  const handleDraftInput = (event: Event) => {
    draft = (event.currentTarget as HTMLTextAreaElement).value;
    persistChatDraft(draft);
  };
</script>

<svelte:head>
  <title>homelabd Chat</title>
  <meta name="description" content="Global homelabd chat interface" />
</svelte:head>

<div class="chat-shell">
  <Navbar title="Chat" subtitle="homelabd" current="/chat" />

  <main class="chat-card">
    <section class="messages" bind:this={messagesEl} aria-live="polite">
      {#each messages as message (message.id)}
        <article class="message" class:user={message.role === 'user'}>
          <div class="meta">
            <span>{message.role === 'user' ? 'You' : `homelabd - ${sourceLabel(message.source)}`}</span>
            <time>{message.time}</time>
          </div>
          <Markdown content={message.content} />
          {#if message.role === 'assistant' && message.actions?.length}
            <div class="message-actions">
              {#each message.actions as action}
                <button type="button" disabled={loading} on:click={() => sendCommand(action)}>
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
          <p>Working…</p>
        </article>
      {/if}
    </section>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <section class="prompt-actions" aria-label="Prompt shortcuts">
      {#each promptActions as action}
        <button type="button" disabled={loading} on:click={() => sendCommand(action.command)}>
          <strong>{action.label}</strong>
          <span>{action.hint}</span>
        </button>
      {/each}
    </section>

    <form class="composer" on:submit|preventDefault={() => void sendMessage()}>
      <label class="hidden" for="message">Message</label>
      <textarea
        id="message"
        bind:this={inputEl}
        bind:value={draft}
        autocomplete="off"
        placeholder="Tell homelabd what you want done. Use Tasks for queue state."
        disabled={loading}
        rows="3"
        on:input={handleDraftInput}
        on:keydown={handleComposerKeydown}
      ></textarea>
      <button type="submit" disabled={loading || !draft.trim()}>
        {loading ? 'Sending' : 'Send'}
      </button>
    </form>
  </main>
</div>

<style>
  :global(html),
  :global(body),
  :global(body > div) {
    height: 100%;
  }

  :global(body) {
    margin: 0;
    color: #172033;
    background: #eef2f7;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
  }

  button,
  textarea {
    font: inherit;
  }

  .chat-shell {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    height: 100dvh;
  }

  .message-actions,
  .prompt-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
  }

  .message-actions button,
  .prompt-actions button {
    min-height: 2rem;
    padding: 0 0.7rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.55rem;
    color: #243047;
    background: #ffffff;
    font-size: 0.82rem;
    font-weight: 750;
    text-decoration: none;
  }

  .chat-card {
    display: grid;
    grid-template-rows: minmax(0, 1fr) auto auto auto;
    min-height: 0;
    width: min(100%, 58rem);
    margin: 0 auto;
    background: #f8fafc;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: 0.8rem;
    min-height: 0;
    overflow-y: auto;
    padding: 1rem;
  }

  .message {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
    width: min(48rem, 92%);
    padding: 0.85rem 0.95rem;
    border: 1px solid #e2e8f0;
    border-radius: 0.9rem;
    background: #ffffff;
    box-shadow: 0 1px 2px rgb(15 23 42 / 0.04);
  }

  .message :global(.markdown) {
    min-width: 0;
    overflow: hidden;
  }

  .message.user {
    align-self: flex-end;
    border-color: #bbf7d0;
    background: #f0fdf4;
  }

  .message.pending {
    color: #475569;
    border-style: dashed;
  }

  .meta {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.75rem;
    color: #64748b;
    font-size: 0.74rem;
  }

  .meta span {
    color: #243047;
    font-weight: 800;
  }

  .error {
    margin: 0;
    padding: 0.75rem 1rem;
    border-top: 1px solid #fecaca;
    color: #991b1b;
    background: #fef2f2;
    overflow-wrap: anywhere;
  }

  .prompt-actions {
    padding: 0 1rem 0.75rem;
  }

  .prompt-actions button {
    display: grid;
    gap: 0.1rem;
    min-width: 8rem;
    padding-block: 0.45rem;
    text-align: left;
  }

  .prompt-actions strong {
    color: #111827;
    font-size: 0.8rem;
  }

  .prompt-actions span {
    color: #64748b;
    font-size: 0.7rem;
    font-weight: 650;
  }

  .composer {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.75rem;
    padding: 1rem;
    border-top: 1px solid #dde4ef;
    background: #ffffff;
  }

  textarea {
    box-sizing: border-box;
    width: 100%;
    min-height: 4.3rem;
    max-height: 12rem;
    padding: 0.8rem 0.9rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.75rem;
    color: #111827;
    background: #ffffff;
    line-height: 1.45;
    resize: vertical;
  }

  textarea:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  .composer button[type='submit'] {
    min-width: 5.75rem;
    min-height: 4.3rem;
    border: 0;
    border-radius: 0.75rem;
    color: #ffffff;
    background: #2563eb;
    font-weight: 850;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    clip-path: inset(50%);
    white-space: nowrap;
  }

  @media (max-width: 720px) {
    .composer {
      grid-template-columns: 1fr;
    }

    .composer button[type='submit'] {
      min-height: 2.8rem;
    }

    .message {
      max-width: 100%;
    }
  }
</style>
