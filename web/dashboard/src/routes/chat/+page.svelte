<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    createHomelabdClient,
    fileToTaskAttachment,
    formatAttachmentSize,
    isImageAttachment,
    MAX_DASHBOARD_ATTACHMENTS,
    Markdown,
    Navbar,
    persistChatDraft,
    readStoredChatDraft,
    type ChatRole,
    type ChatTranscriptMessage,
    type HomelabdTaskAttachment
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
  let fileInputEl: HTMLInputElement | undefined;
  let messagesEl: HTMLElement | undefined;
  let messages: ChatTranscriptMessage[] = [welcomeMessage];
  let pendingAttachments: HomelabdTaskAttachment[] = [];
  let selectedFiles: FileList | undefined;
  let attachmentError = '';
  let draggingFiles = false;

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
    const validAttachments =
      candidate.attachments === undefined ||
      (Array.isArray(candidate.attachments) &&
        candidate.attachments.every(
          (attachment) =>
            attachment &&
            typeof attachment === 'object' &&
            typeof (attachment as Record<string, unknown>).name === 'string' &&
            typeof (attachment as Record<string, unknown>).content_type === 'string'
        ));

    return (
      typeof candidate.id === 'string' &&
      validRole &&
      typeof candidate.content === 'string' &&
      typeof candidate.time === 'string' &&
      validActions &&
      validAttachments
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
      const storedMessages = messages.slice(-120).map((message) => ({
        ...message,
        attachments: message.attachments?.map(({ data_url, ...attachment }) => attachment)
      }));
      localStorage.setItem(transcriptStorageKey, JSON.stringify(storedMessages));
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

  const addMessage = (
    role: ChatRole,
    content: string,
    source?: string,
    attachments: HomelabdTaskAttachment[] = []
  ) => {
    messageId += 1;
    messages = [
      ...messages,
      {
        id: `${role}-${messageId}`,
        role,
        content,
        source,
        attachments: attachments.length ? attachments : undefined,
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
    const attachments = pendingAttachments;
    if ((!trimmed && attachments.length === 0) || loading) {
      return;
    }

    draft = '';
    pendingAttachments = [];
    attachmentError = '';
    persistChatDraft(draft);
    error = '';
    addMessage('user', trimmed || 'Attached files', undefined, attachments);
    loading = true;

    try {
      const response = await client.sendMessage({
        from: 'dashboard',
        content: trimmed || 'Attached files',
        attachments
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

  $: if (selectedFiles?.length) {
    const files = selectedFiles;
    selectedFiles = undefined;
    void addFiles(files);
  }

  async function addFiles(files: FileList | File[]) {
    const selected = Array.from(files);
    if (!selected.length || loading) {
      return;
    }
    attachmentError = '';
    const available = MAX_DASHBOARD_ATTACHMENTS - pendingAttachments.length;
    if (available <= 0) {
      attachmentError = `Attach up to ${MAX_DASHBOARD_ATTACHMENTS} files.`;
      return;
    }
    const next: HomelabdTaskAttachment[] = [];
    for (const file of selected.slice(0, available)) {
      try {
        next.push(await fileToTaskAttachment(file));
      } catch (err) {
        attachmentError = err instanceof Error ? err.message : 'Unable to attach file.';
      }
    }
    pendingAttachments = [...pendingAttachments, ...next];
    if (selected.length > available) {
      attachmentError = `Only ${available} more file${available === 1 ? '' : 's'} could be attached.`;
    }
    if (fileInputEl) {
      fileInputEl.value = '';
    }
  }

  const removePendingAttachment = (id?: string) => {
    pendingAttachments = pendingAttachments.filter((attachment) => attachment.id !== id);
  };

  const handleFileInput = (event: Event) => {
    const files = (event.currentTarget as HTMLInputElement).files;
    if (files) {
      void addFiles(files);
    }
  };

  const triggerFileInput = () => {
    if (!loading) {
      fileInputEl?.click();
    }
  };

  const eventHasFiles = (event: DragEvent) =>
    Array.from(event.dataTransfer?.types || []).includes('Files');

  const handleDragEnter = (event: DragEvent) => {
    if (!eventHasFiles(event) || loading) {
      return;
    }
    event.preventDefault();
    draggingFiles = true;
  };

  const handleDragOver = (event: DragEvent) => {
    if (!eventHasFiles(event) || loading) {
      return;
    }
    event.preventDefault();
    draggingFiles = true;
  };

  const handleDragLeave = (event: DragEvent) => {
    if (!(event.currentTarget as HTMLElement).contains(event.relatedTarget as Node | null)) {
      draggingFiles = false;
    }
  };

  const handleDrop = (event: DragEvent) => {
    if (!eventHasFiles(event) || loading) {
      return;
    }
    event.preventDefault();
    draggingFiles = false;
    if (event.dataTransfer?.files) {
      void addFiles(event.dataTransfer.files);
    }
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
          {#if message.attachments?.length}
            <div class="attachment-list message-attachments" aria-label="Message attachments">
              {#each message.attachments as attachment}
                <a
                  class="attachment-chip"
                  href={attachment.data_url || undefined}
                  download={attachment.data_url ? attachment.name : undefined}
                  aria-label={`Attachment ${attachment.name}`}
                >
                  {#if attachment.data_url && isImageAttachment(attachment)}
                    <img src={attachment.data_url} alt="" />
                  {/if}
                  <span>
                    <strong>{attachment.name}</strong>
                    <small>{attachment.content_type} / {formatAttachmentSize(attachment.size)}</small>
                  </span>
                </a>
              {/each}
            </div>
          {/if}
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

    <form
      class="composer"
      class:dragging={draggingFiles}
      on:submit|preventDefault={() => void sendMessage()}
      on:dragenter={handleDragEnter}
      on:dragover={handleDragOver}
      on:dragleave={handleDragLeave}
      on:drop={handleDrop}
    >
      <label class="hidden" for="message">Message</label>
      <div class="composer-field">
        <textarea
          id="message"
          bind:this={inputEl}
          bind:value={draft}
          autocomplete="off"
          placeholder="Tell homelabd what you want done. Drop files here to attach them."
          disabled={loading}
          rows="3"
          on:input={handleDraftInput}
          on:keydown={handleComposerKeydown}
        ></textarea>
        <input
          class="hidden"
          bind:this={fileInputEl}
          bind:files={selectedFiles}
          id="chat-attachments"
          type="file"
          multiple
          disabled={loading}
          on:input={handleFileInput}
          on:change={handleFileInput}
        />
        {#if pendingAttachments.length}
          <div class="attachment-list pending-attachments" aria-label="Pending attachments">
            {#each pendingAttachments as attachment}
              <span class="attachment-chip pending">
                <span>
                  <strong>{attachment.name}</strong>
                  <small>{attachment.content_type} / {formatAttachmentSize(attachment.size)}</small>
                </span>
                <button
                  type="button"
                  aria-label={`Remove ${attachment.name}`}
                  disabled={loading}
                  on:click={() => removePendingAttachment(attachment.id)}
                >
                  Remove
                </button>
              </span>
            {/each}
          </div>
        {/if}
        {#if attachmentError}
          <p class="attachment-error" role="alert">{attachmentError}</p>
        {/if}
      </div>
      <div class="composer-buttons">
        <button type="button" class="attach-button" disabled={loading} on:click={triggerFileInput}>
          Attach
        </button>
        <button type="submit" disabled={loading || (!draft.trim() && pendingAttachments.length === 0)}>
          {loading ? 'Sending' : 'Send'}
        </button>
      </div>
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
  .prompt-actions,
  .attachment-list {
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

  .attachment-chip {
    display: inline-flex;
    align-items: center;
    gap: 0.5rem;
    max-width: 100%;
    padding: 0.42rem 0.55rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.55rem;
    color: #243047;
    background: #f8fafc;
    font-size: 0.78rem;
    font-weight: 750;
    text-decoration: none;
  }

  .attachment-chip img {
    width: 2.4rem;
    height: 2.4rem;
    border-radius: 0.4rem;
    object-fit: cover;
  }

  .attachment-chip span {
    display: grid;
    gap: 0.08rem;
    min-width: 0;
  }

  .attachment-chip strong,
  .attachment-chip small {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .attachment-chip small {
    color: #64748b;
    font-weight: 650;
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
    align-items: end;
    gap: 0.75rem;
    padding: 1rem;
    border-top: 1px solid #dde4ef;
    background: #ffffff;
  }

  .composer.dragging {
    outline: 3px solid rgb(37 99 235 / 0.18);
    outline-offset: -0.5rem;
  }

  .composer-field,
  .composer-buttons {
    display: grid;
    gap: 0.55rem;
  }

  .pending-attachments {
    min-width: 0;
  }

  .attachment-chip.pending {
    justify-content: space-between;
    width: min(100%, 24rem);
  }

  .attachment-chip.pending button {
    min-height: 1.8rem;
    padding: 0 0.5rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.45rem;
    color: #243047;
    background: #ffffff;
    font-size: 0.72rem;
    font-weight: 800;
  }

  .attachment-error {
    margin: 0;
    color: #991b1b;
    font-size: 0.82rem;
    line-height: 1.35;
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

  .composer-buttons button,
  .composer-buttons .attach-button {
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 5.75rem;
    min-height: 2.05rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.75rem;
    color: #243047;
    background: #ffffff;
    font-weight: 850;
    cursor: pointer;
    text-align: center;
  }

  .composer button[type='submit'] {
    min-height: 2.05rem;
    border: 0;
    color: #ffffff;
    background: #2563eb;
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

    .composer-buttons {
      grid-template-columns: 1fr 1fr;
    }

    .composer-buttons button,
    .composer-buttons .attach-button {
      min-height: 2.8rem;
    }

    .message {
      max-width: 100%;
    }
  }
</style>
