<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount, tick } from 'svelte';
  import {
    createHomelabdClient,
    chatMessageElementID,
    chatMessageURL,
    fileToTaskAttachment,
    formatAttachmentSize,
    isImageAttachment,
    MAX_DASHBOARD_ATTACHMENTS,
    Markdown,
    Navbar,
    persistChatDraft,
    readStoredChatDraft,
    type ChatInteractionStats,
    type ChatRole,
    type ChatTranscriptMessage,
    type HomelabdMessageRequest,
    type HomelabdTaskAttachment
  } from '@homelab/shared';
  import { extractCommands } from './command-actions';
  import { formatInteractionStats, messageExchangeNumber } from './interaction-stats';
  import {
    activeChatSessionStorageKey,
    chatSessionTitle,
    chatSessionsStorageKey,
    emptyChatTitle,
    legacyTranscriptStorageKey,
    prepareSessionForStorage,
    restoreChatState,
    sortChatSessions,
    type ChatSession
  } from './sessions';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const defaultChatSendTimeoutMs = 20_000;
  const configuredChatSendTimeoutMs = Number.parseInt(
    import.meta.env.VITE_HOMELABD_CHAT_SEND_TIMEOUT_MS || '',
    10
  );
  type PromptAction = {
    label: string;
    command: string;
    hint: string;
  };

  type ChatWindow = Window & {
    __homelabdChatSendTimeoutMs?: number;
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

  let draft = '';
  let loading = false;
  let clearing = false;
  let error = '';
  let messageId = 0;
  let inputEl: HTMLTextAreaElement | undefined;
  let fileInputEl: HTMLInputElement | undefined;
  let messagesEl: HTMLElement | undefined;
  let currentSendAbortController: AbortController | undefined;
  let currentSendCancelled = false;
  let sessions: ChatSession[] = [];
  let activeSessionID = '';
  let currentSession: ChatSession | undefined;
  let currentSessionTitle = emptyChatTitle;
  let messages: ChatTranscriptMessage[] = [];
  let pendingAttachments: HomelabdTaskAttachment[] = [];
  let selectedFiles: FileList | undefined;
  let attachmentError = '';
  let draggingFiles = false;

  $: currentSession = sessions.find((session) => session.id === activeSessionID);
  $: currentSessionTitle = currentSession?.title || emptyChatTitle;
  $: hasCurrentMessages = messages.length > 0;

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

  const newSessionID = () => {
    const random =
      typeof crypto !== 'undefined' && 'randomUUID' in crypto
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    return `chat_${random}`;
  };

  const activeSession = () => currentSession;

  const nextMessageIndex = (items: ChatTranscriptMessage[]) =>
    items.reduce((max, message) => {
      const suffix = Number.parseInt(message.id.split('-').at(-1) || '', 10);
      return Number.isFinite(suffix) ? Math.max(max, suffix) : max;
    }, items.length);

  const persistSessions = () => {
    try {
      const storedSessions = sortChatSessions(sessions).map(prepareSessionForStorage);
      localStorage.setItem(chatSessionsStorageKey, JSON.stringify(storedSessions));
      localStorage.setItem(activeChatSessionStorageKey, activeSessionID);
      localStorage.removeItem(legacyTranscriptStorageKey);
    } catch {
      error = 'Chat history could not be persisted locally.';
    }
  };

  const persistMessages = () => {
    const session = activeSession();
    if (!session) {
      return;
    }
    const now = new Date().toISOString();
    const updated: ChatSession = {
      ...session,
      title: chatSessionTitle(messages),
      messages,
      updated_at: now,
      legacy: undefined
    };
    sessions = sortChatSessions(sessions.map((item) => (item.id === updated.id ? updated : item)));
    persistSessions();
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

  const scrollToMessageHash = () => {
    if (typeof window === 'undefined' || !window.location.hash.startsWith('#message-')) {
      return false;
    }
    void tick().then(() => {
      document.getElementById(window.location.hash.slice(1))?.scrollIntoView({
        block: 'center'
      });
    });
    return true;
  };

  const navigateMarkdownLink = (href: string) => {
    if (href.startsWith('#')) {
      window.location.hash = href;
      return;
    }
    void goto(href, { keepFocus: true });
  };

  const sessionUpdatedLabel = (session: ChatSession) => {
    const updated = new Date(session.updated_at);
    if (Number.isNaN(updated.getTime())) {
      return '';
    }
    return updated.toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  const sessionMessageLabel = (session: ChatSession) => {
    const count = session.messages.length;
    return `${count} message${count === 1 ? '' : 's'}`;
  };

  const blankSession = (): ChatSession => {
    const now = new Date().toISOString();
    return {
      id: newSessionID(),
      title: emptyChatTitle,
      messages: [],
      created_at: now,
      updated_at: now
    };
  };

  const activateSession = (sessionID: string) => {
    const session = sessions.find((item) => item.id === sessionID);
    if (!session || loading || clearing) {
      return;
    }
    activeSessionID = session.id;
    messages = session.messages;
    messageId = nextMessageIndex(messages);
    pendingAttachments = [];
    attachmentError = '';
    persistSessions();
    if (!scrollToMessageHash()) {
      scrollMessages();
    }
    focusInput();
  };

  const startNewChat = () => {
    if (loading || clearing) {
      return;
    }
    error = '';
    const existingBlank = sessions.find((session) => session.messages.length === 0);
    if (existingBlank) {
      draft = '';
      persistChatDraft(draft);
      activateSession(existingBlank.id);
      return;
    }
    const session = blankSession();
    sessions = sortChatSessions([session, ...sessions]);
    activeSessionID = session.id;
    messages = [];
    messageId = 0;
    draft = '';
    pendingAttachments = [];
    attachmentError = '';
    persistChatDraft(draft);
    persistSessions();
    scrollMessages();
    focusInput();
  };

  const clearChatContext = async (all = false, conversationID = activeSessionID) => {
    const client = createHomelabdClient({ baseUrl: apiBase });
    await client.clearChat(all ? { all: true } : { conversation_id: conversationID });
  };

  const useSessionAfterClear = (remaining: ChatSession[]) => {
    if (remaining.length > 0) {
      sessions = sortChatSessions(remaining);
      activeSessionID = sessions[0].id;
      messages = sessions[0].messages;
    } else {
      const session = blankSession();
      sessions = [session];
      activeSessionID = session.id;
      messages = [];
    }
    messageId = nextMessageIndex(messages);
    draft = '';
    pendingAttachments = [];
    attachmentError = '';
    persistChatDraft(draft);
    persistSessions();
    scrollMessages();
    focusInput();
  };

  const clearCurrentChat = async () => {
    const session = activeSession();
    if (!session || !hasCurrentMessages || loading || clearing) {
      return;
    }
    if (
      !window.confirm(
        'Clear this chat? This removes its messages from this browser and homelabd chat context.'
      )
    ) {
      return;
    }
    clearing = true;
    error = '';
    try {
      await clearChatContext(false, session.id);
      useSessionAfterClear(sessions.filter((item) => item.id !== session.id));
    } catch (err) {
      error = err instanceof Error ? err.message : 'Chat could not be cleared.';
    } finally {
      clearing = false;
    }
  };

  const clearAllChats = async () => {
    if (loading || clearing) {
      return;
    }
    if (
      !window.confirm(
        'Clear all chats? This removes every local chat and clears homelabd chat context.'
      )
    ) {
      return;
    }
    clearing = true;
    error = '';
    try {
      await clearChatContext(true);
      useSessionAfterClear([]);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Chat history could not be cleared.';
    } finally {
      clearing = false;
    }
  };

  const addMessage = (
    role: ChatRole,
    content: string,
    source?: string,
    attachments: HomelabdTaskAttachment[] = [],
    stats?: ChatInteractionStats
  ) => {
    messageId += 1;
    const message: ChatTranscriptMessage = {
      id: `${role}-${messageId}`,
      role,
      content,
      source,
      attachments: attachments.length ? attachments : undefined,
      actions: role === 'assistant' ? extractCommands(content) : undefined,
      stats,
      time: timeLabel()
    };
    messages = [...messages, message];
    persistMessages();
    scrollMessages();
    return message;
  };

  const updateMessage = (messageID: string, updates: Partial<ChatTranscriptMessage>) => {
    messages = messages.map((message) =>
      message.id === messageID ? { ...message, ...updates } : message
    );
    persistMessages();
    scrollMessages();
  };

  const failedDeliveryMessage = (err: unknown) =>
    err instanceof Error ? err.message : 'Unable to reach homelabd.';

  const positiveTimeout = (milliseconds: number) =>
    Number.isFinite(milliseconds) && milliseconds > 0 ? milliseconds : undefined;

  const chatSendTimeoutMs = () =>
    (typeof window !== 'undefined'
      ? positiveTimeout(Number((window as ChatWindow).__homelabdChatSendTimeoutMs))
      : undefined) ||
    positiveTimeout(configuredChatSendTimeoutMs) ||
    defaultChatSendTimeoutMs;

  const isAbortError = (err: unknown) =>
    err instanceof DOMException && (err.name === 'AbortError' || err.name === 'TimeoutError');

  const sendMessageRequest = async (request: HomelabdMessageRequest) => {
    const timeoutMs = chatSendTimeoutMs();
    const controller = new AbortController();
    currentSendAbortController = controller;
    currentSendCancelled = false;
    const timeout = window.setTimeout(() => controller.abort(), timeoutMs);
    const timedClient = createHomelabdClient({
      baseUrl: apiBase,
      fetcher: (input, init) => fetch(input, { ...init, signal: controller.signal })
    });
    try {
      return await timedClient.sendMessage(request);
    } catch (err) {
      if (isAbortError(err)) {
        if (currentSendCancelled && currentSendAbortController === controller) {
          throw new Error('Message send cancelled.');
        }
        const timeoutSeconds = Math.max(1, Math.ceil(timeoutMs / 1000));
        throw new Error(
          `Message send timed out after ${timeoutSeconds}s. Check the task queue before retrying.`
        );
      }
      throw err;
    } finally {
      window.clearTimeout(timeout);
      if (currentSendAbortController === controller) {
        currentSendAbortController = undefined;
        currentSendCancelled = false;
      }
    }
  };

  const cancelCurrentSend = () => {
    if (!loading || !currentSendAbortController) {
      return;
    }
    currentSendCancelled = true;
    currentSendAbortController.abort();
  };

  const deliverUserMessage = async (message: ChatTranscriptMessage) => {
    loading = true;
    error = '';
    const conversationID = activeSessionID;
    try {
      const response = await sendMessageRequest({
        from: 'dashboard',
        content: message.content.trim() || 'Attached files',
        conversation_id: conversationID,
        attachments: message.attachments || []
      });
      updateMessage(message.id, { delivery_status: undefined, delivery_error: undefined });
      addMessage(
        'assistant',
        response.reply || 'No reply returned.',
        response.source || 'program',
        [],
        response.stats
      );
    } catch (err) {
      updateMessage(message.id, {
        delivery_status: 'failed',
        delivery_error: failedDeliveryMessage(err)
      });
    } finally {
      loading = false;
      focusInput();
    }
  };

  onMount(() => {
    const restored = restoreChatState({
      storedSessions: localStorage.getItem(chatSessionsStorageKey),
      storedActiveID: localStorage.getItem(activeChatSessionStorageKey),
      legacyTranscript: localStorage.getItem(legacyTranscriptStorageKey),
      createID: newSessionID,
      now: new Date().toISOString()
    });
    sessions = restored.sessions;
    activeSessionID = restored.activeSessionID;
    messages = sessions.find((session) => session.id === activeSessionID)?.messages || [];
    draft = inputEl?.value || readStoredChatDraft();
    persistChatDraft(draft);
    messageId = nextMessageIndex(messages);
    persistSessions();
    if (!scrollToMessageHash()) {
      scrollMessages();
    }
    window.addEventListener('hashchange', scrollToMessageHash);
    focusInput();
    return () => window.removeEventListener('hashchange', scrollToMessageHash);
  });

  const sendMessage = async (content = draft) => {
    const trimmed = content.trim();
    const attachments = pendingAttachments;
    if ((!trimmed && attachments.length === 0) || loading || clearing) {
      return;
    }

    draft = '';
    pendingAttachments = [];
    attachmentError = '';
    persistChatDraft(draft);
    error = '';
    const message = addMessage('user', trimmed || 'Attached files', undefined, attachments);
    await deliverUserMessage(message);
  };

  const resendFailedMessage = (message: ChatTranscriptMessage) => {
    if (loading || clearing || message.delivery_status !== 'failed') {
      return;
    }
    void deliverUserMessage(message);
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

  const messageFooter = (message: ChatTranscriptMessage, index: number) =>
    formatInteractionStats(message, messageExchangeNumber(messages, index));

  async function addFiles(files: FileList | File[]) {
    const selected = Array.from(files);
    if (!selected.length || loading || clearing) {
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
    if (!loading && !clearing) {
      fileInputEl?.click();
    }
  };

  const eventHasFiles = (event: DragEvent) =>
    Array.from(event.dataTransfer?.types || []).includes('Files');

  const handleDragEnter = (event: DragEvent) => {
    if (!eventHasFiles(event) || loading || clearing) {
      return;
    }
    event.preventDefault();
    draggingFiles = true;
  };

  const handleDragOver = (event: DragEvent) => {
    if (!eventHasFiles(event) || loading || clearing) {
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
    if (!eventHasFiles(event) || loading || clearing) {
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

<div class="chat-nav-scope">
  <Navbar title="Chat" subtitle="homelabd" current="/chat" taskApiBase={apiBase} />
</div>

<div class="chat-shell">
  <aside class="session-sidebar" aria-label="Chat history">
    <div class="session-sidebar-header">
      <strong>Chats</strong>
      <div class="session-sidebar-actions">
        <button
          type="button"
          class="icon-button new-chat-button"
          aria-label="New chat"
          title="New chat"
          disabled={loading || clearing}
          on:click={startNewChat}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>
        <button
          type="button"
          class="icon-button clear-all-button"
          aria-label="Clear all chats"
          title="Clear all chats"
          disabled={loading || clearing}
          on:click={clearAllChats}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M4 7h16M9 7V5h6v2M9 11v6M15 11v6M6 7l1 13h10l1-13" />
          </svg>
        </button>
      </div>
    </div>
    <nav class="session-list" aria-label="Previous chats">
      {#each sessions as session (session.id)}
        <button
          type="button"
          class="session-item"
          class:active={session.id === activeSessionID}
          aria-current={session.id === activeSessionID ? 'page' : undefined}
          disabled={loading || clearing}
          on:click={() => activateSession(session.id)}
        >
          <span class="session-title">{session.title || chatSessionTitle(session.messages)}</span>
          <span class="session-meta">
            {sessionMessageLabel(session)}
            {#if sessionUpdatedLabel(session)}
              · {sessionUpdatedLabel(session)}
            {/if}
          </span>
        </button>
      {/each}
    </nav>
  </aside>

  <main class="chat-card">
    <header class="chat-toolbar">
      <div class="chat-title-group">
        <h1>{currentSessionTitle}</h1>
        <span>{messages.length} message{messages.length === 1 ? '' : 's'}</span>
      </div>
      <div class="chat-toolbar-actions">
        <button
          type="button"
          class="icon-button clear-current-button"
          aria-label={clearing ? 'Clearing chat' : 'Clear current chat'}
          title={clearing ? 'Clearing chat' : 'Clear current chat'}
          disabled={!hasCurrentMessages || loading || clearing}
          on:click={clearCurrentChat}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M4 7h16M9 7V5h6v2M9 11v6M15 11v6M6 7l1 13h10l1-13" />
          </svg>
        </button>
      </div>
    </header>

    <section class="messages" bind:this={messagesEl} aria-live="polite">
      {#if messages.length === 0 && !loading}
        <div class="empty-chat" role="status">
          <h2>New chat</h2>
        </div>
      {/if}

      {#each messages as message, index (message.id)}
        <article
          id={chatMessageElementID(message.id)}
          class="message"
          class:user={message.role === 'user'}
          class:failed={message.delivery_status === 'failed'}
        >
          <div class="meta">
            <span>{message.role === 'user' ? 'You' : `homelabd - ${sourceLabel(message.source)}`}</span>
            <span class="message-meta-actions">
              <a href={chatMessageURL(message.id)} aria-label="Link to message">#</a>
              <time>{message.time}</time>
            </span>
          </div>
          <Markdown content={message.content} navigate={navigateMarkdownLink} />
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
                <button type="button" disabled={loading || clearing} on:click={() => sendCommand(action)}>
                  {action}
                </button>
              {/each}
            </div>
          {/if}
          {#if message.role === 'user' && message.delivery_status === 'failed'}
            <div class="delivery-status">
              <span role="status">Message failed to send</span>
              <button
                type="button"
                class="resend-button"
                disabled={loading || clearing}
                aria-label="Resend failed message"
                title={message.delivery_error || 'Resend failed message'}
                on:click={() => resendFailedMessage(message)}
              >
                <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                  <path
                    d="M21 12a9 9 0 0 1-15.1 6.6M3 12A9 9 0 0 1 18.1 5.4M18 2v5h-5M6 22v-5h5"
                  />
                </svg>
              </button>
            </div>
          {/if}
          {#if messageFooter(message, index)}
            <div class="message-footer">{messageFooter(message, index)}</div>
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
        <button
          type="button"
          class="prompt-action-chip"
          disabled={loading || clearing}
          on:click={() => sendCommand(action.command)}
        >
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
      <div class="composer-row">
        <input
          class="hidden"
          bind:this={fileInputEl}
          bind:files={selectedFiles}
          id="chat-attachments"
          type="file"
          multiple
          aria-label="Selected files"
          disabled={loading || clearing}
          on:input={handleFileInput}
          on:change={handleFileInput}
        />
        <button
          type="button"
          class="icon-button attach-button"
          aria-label="Attach"
          title="Attach files"
          disabled={loading || clearing}
          on:click={triggerFileInput}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="m21 8.5-9.6 9.6a5 5 0 0 1-7.1-7.1l9.9-9.9a3.4 3.4 0 0 1 4.8 4.8l-9.9 9.9a1.8 1.8 0 0 1-2.5-2.5l8.7-8.7" />
          </svg>
        </button>
        <div class="composer-field">
          <textarea
            id="message"
            bind:this={inputEl}
            bind:value={draft}
            autocomplete="off"
            placeholder="Tell homelabd what you want done."
            disabled={loading || clearing}
            rows="2"
            on:input={handleDraftInput}
            on:keydown={handleComposerKeydown}
          ></textarea>
        </div>
        <div class="composer-buttons">
          {#if loading}
            <button
              type="button"
              class="icon-button cancel-send-button"
              aria-label="Cancel current message send"
              title="Cancel current message send"
              on:click={cancelCurrentSend}
            >
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          {/if}
          <button
            type="submit"
            class="icon-button send-button"
            aria-label={loading ? 'Sending' : 'Send'}
            title={loading ? 'Sending' : 'Send'}
            disabled={loading || clearing || (!draft.trim() && pendingAttachments.length === 0)}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
              <path d="M4 12 20 4l-5 16-3-7-8-1zM12 13l8-9" />
            </svg>
          </button>
        </div>
      </div>
      <div class="composer-secondary">
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
                  title={`Remove ${attachment.name}`}
                  disabled={loading || clearing}
                  on:click={() => removePendingAttachment(attachment.id)}
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                    <path d="M6 6l12 12M18 6 6 18" />
                  </svg>
                </button>
              </span>
            {/each}
          </div>
        {/if}
        {#if attachmentError}
          <p class="attachment-error" role="alert">{attachmentError}</p>
        {/if}
      </div>
    </form>
  </main>
</div>

<style>
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

  .chat-nav-scope {
    display: contents;
  }

  .chat-shell {
    --chat-navbar-height: 4rem;
    --chat-shell-max: 112rem;
    box-sizing: border-box;
    position: fixed;
    inset: 0;
    display: grid;
    grid-template-columns: clamp(16rem, 20vw, 22rem) minmax(0, 1fr);
    gap: 0.75rem;
    height: auto;
    overflow: hidden;
    padding:
      calc(var(--chat-navbar-height) + 0.75rem)
      max(0.75rem, calc((100vw - var(--chat-shell-max)) / 2))
      0.75rem;
    background: #eef2f7;
  }

  .chat-nav-scope :global(.navbar) {
    position: fixed !important;
    top: 0 !important;
    right: 0;
    left: 0;
    z-index: 20;
  }

  .message-actions,
  .prompt-actions,
  .attachment-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
  }

  .icon-button {
    box-sizing: border-box;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.2rem;
    height: 2.2rem;
    padding: 0;
    border: 1px solid #cbd5e1;
    border-radius: 0.45rem;
    color: #243047;
    background: #ffffff;
    cursor: pointer;
  }

  .icon-button:hover:not(:disabled),
  .icon-button:focus-visible {
    border-color: #93c5fd;
    color: #1d4ed8;
    background: #eff6ff;
    outline: none;
  }

  .icon-button svg {
    width: 1.05rem;
    height: 1.05rem;
  }

  .icon-button path {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.9;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .session-sidebar {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    min-width: 0;
    min-height: 0;
    border: 1px solid #dbe3ef;
    border-radius: 0.5rem;
    background: #ffffff;
    overflow: hidden;
  }

  .session-sidebar-header,
  .chat-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .session-sidebar-header {
    padding: 0.58rem 0.65rem;
    border-bottom: 1px solid #e2e8f0;
  }

  .session-sidebar-header strong {
    color: #0f172a;
    font-size: 0.9rem;
  }

  .session-sidebar-actions {
    display: flex;
    flex-wrap: nowrap;
    justify-content: flex-end;
    gap: 0.35rem;
  }

  .session-list {
    display: grid;
    align-content: start;
    gap: 0.25rem;
    min-height: 0;
    overflow-y: auto;
    padding: 0.5rem;
  }

  .session-item {
    display: grid;
    gap: 0.18rem;
    min-width: 0;
    width: 100%;
    padding: 0.55rem 0.6rem;
    border: 1px solid transparent;
    border-radius: 0.45rem;
    color: #243047;
    background: transparent;
    text-align: left;
    cursor: pointer;
  }

  .session-item:hover:not(:disabled),
  .session-item:focus-visible,
  .session-item.active {
    border-color: #cbd5e1;
    background: #f8fafc;
    outline: none;
  }

  .session-item.active {
    border-color: #bfdbfe;
    background: #eff6ff;
  }

  .session-title,
  .session-meta {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .session-title {
    color: #172033;
    font-size: 0.84rem;
    font-weight: 800;
  }

  .session-meta {
    color: #64748b;
    font-size: 0.7rem;
    font-weight: 650;
  }

  .clear-all-button,
  .chat-toolbar-actions .clear-current-button {
    border-color: #fecaca;
    color: #991b1b;
    background: #fff7f7;
  }

  .chat-toolbar {
    min-width: 0;
    padding: 0.55rem 0.75rem;
    border-bottom: 1px solid #dde4ef;
    background: #ffffff;
  }

  .chat-title-group {
    min-width: 0;
  }

  .chat-title-group span {
    display: block;
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 700;
  }

  .chat-toolbar h1 {
    margin: 0;
    color: #0f172a;
    font-size: 1rem;
    line-height: 1.25;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .chat-toolbar-actions {
    display: flex;
    flex-wrap: nowrap;
    justify-content: flex-end;
    gap: 0.35rem;
    flex-shrink: 0;
  }

  .chat-toolbar-actions .clear-current-button {
    border-color: #fecaca;
    color: #991b1b;
    background: #fff7f7;
  }

  .message-actions button,
  .prompt-actions button {
    min-height: 1.85rem;
    max-width: 100%;
    padding: 0 0.55rem;
    border: 1px solid #cbd5e1;
    border-radius: 999px;
    color: #243047;
    background: #ffffff;
    font-size: 0.76rem;
    font-weight: 750;
    text-decoration: none;
    overflow-wrap: anywhere;
    white-space: normal;
  }

  .chat-card {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr) auto auto auto;
    min-height: 0;
    min-width: 0;
    width: 100%;
    border: 1px solid #dbe3ef;
    border-radius: 0.5rem;
    background: #f8fafc;
    overflow: hidden;
  }

  .messages {
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
    min-height: 0;
    overflow-y: auto;
    padding: 0.75rem;
  }

  .message {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
    width: min(44rem, 90%);
    padding: 0.75rem 0.85rem;
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

  .message.user.failed {
    border-color: #cbd5e1;
    color: #334155;
    background: #f1f5f9;
  }

  .message.user.failed .meta span {
    color: #475569;
  }

  :global(html[data-theme='dark']) .message.user.failed {
    border-color: var(--border) !important;
    color: var(--text) !important;
    background: var(--surface-muted) !important;
  }

  :global(html[data-theme='dark']) .message.user.failed .meta span,
  :global(html[data-theme='dark']) .delivery-status,
  :global(html[data-theme='dark']) .resend-button {
    color: var(--muted) !important;
  }

  :global(html[data-theme='dark']) .resend-button:hover:not(:disabled),
  :global(html[data-theme='dark']) .resend-button:focus-visible {
    border-color: var(--border) !important;
    color: #bfdbfe !important;
    background: var(--surface-hover) !important;
  }

  .message.pending {
    color: #475569;
    border-style: dashed;
  }

  .empty-chat {
    display: grid;
    place-items: start center;
    min-height: 0;
    padding-top: 2rem;
    color: #64748b;
    text-align: center;
  }

  .empty-chat h2 {
    margin: 0;
    color: #172033;
    font-size: 1rem;
    line-height: 1.2;
  }

  .delivery-status {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.35rem;
    margin-top: 0.05rem;
    color: #64748b;
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .resend-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.65rem;
    height: 1.65rem;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 999px;
    color: #475569;
    background: transparent;
    cursor: pointer;
  }

  .resend-button:hover:not(:disabled),
  .resend-button:focus-visible {
    border-color: #cbd5e1;
    color: #1d4ed8;
    background: #ffffff;
    outline: none;
  }

  .resend-button svg {
    width: 1rem;
    height: 1rem;
  }

  .resend-button path {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
    stroke-linejoin: round;
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

  .message-meta-actions {
    display: inline-flex;
    align-items: baseline;
    gap: 0.45rem;
  }

  .meta > span:first-child {
    color: #243047;
    font-weight: 800;
  }

  .message-footer {
    color: #64748b;
    font-size: 0.68rem;
    font-weight: 600;
    line-height: 1.35;
    overflow-wrap: anywhere;
  }

  .meta a {
    color: #64748b;
    font-weight: 900;
    text-decoration: none;
  }

  .meta a:hover,
  .meta a:focus-visible {
    color: #1d4ed8;
    text-decoration: underline;
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
    padding: 0.5rem 0.75rem;
    border-top: 1px solid #dde4ef;
    background: #f8fafc;
  }

  .prompt-actions button {
    display: inline-flex;
    align-items: baseline;
    gap: 0.35rem;
    min-width: 0;
    padding-block: 0.25rem;
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
    gap: 0.5rem;
    padding: 0.65rem 0.75rem;
    border-top: 1px solid #dde4ef;
    background: #ffffff;
  }

  .composer.dragging {
    outline: 3px solid rgb(37 99 235 / 0.18);
    outline-offset: -0.5rem;
  }

  .composer-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: end;
    gap: 0.5rem;
    min-width: 0;
  }

  .composer-field,
  .composer-secondary,
  .composer-buttons {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
  }

  .composer-buttons {
    display: flex;
    flex-wrap: nowrap;
  }

  .composer .icon-button {
    width: 2.75rem;
    height: 2.75rem;
    border-radius: 0.6rem;
  }

  .composer-secondary {
    padding-left: 3.25rem;
  }

  .pending-attachments {
    min-width: 0;
  }

  .attachment-chip.pending {
    justify-content: space-between;
    width: min(100%, 24rem);
  }

  .attachment-chip.pending button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 1.8rem;
    height: 1.8rem;
    padding: 0;
    border: 1px solid #cbd5e1;
    border-radius: 0.45rem;
    color: #243047;
    background: #ffffff;
    font-size: 0.72rem;
    font-weight: 800;
  }

  .attachment-chip.pending button svg {
    width: 0.9rem;
    height: 0.9rem;
  }

  .attachment-chip.pending button path {
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
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
    min-height: 2.75rem;
    max-height: 9rem;
    padding: 0.6rem 0.75rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.6rem;
    color: #111827;
    background: #ffffff;
    line-height: 1.45;
    resize: vertical;
  }

  textarea:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  .composer .send-button {
    border: 0;
    color: #ffffff;
    background: #2563eb;
  }

  .composer .send-button:hover:not(:disabled),
  .composer .send-button:focus-visible {
    color: #ffffff;
    background: #1d4ed8;
  }

  .composer .cancel-send-button {
    border-color: #fecaca;
    color: #991b1b;
    background: #fff7f7;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  :global(html[data-theme='dark']) .composer .cancel-send-button {
    border-color: #7f1d1d !important;
    color: #fecaca !important;
    background: #450a0a !important;
  }

  :global(html[data-theme='dark']) .chat-shell {
    background: var(--bg) !important;
  }

  :global(html[data-theme='dark']) .session-sidebar,
  :global(html[data-theme='dark']) .chat-toolbar,
  :global(html[data-theme='dark']) .prompt-actions,
  :global(html[data-theme='dark']) .composer,
  :global(html[data-theme='dark']) .session-item:hover:not(:disabled),
  :global(html[data-theme='dark']) .session-item:focus-visible,
  :global(html[data-theme='dark']) .session-item.active,
  :global(html[data-theme='dark']) .icon-button:not(.send-button),
  :global(html[data-theme='dark']) .prompt-actions button,
  :global(html[data-theme='dark']) .attachment-chip.pending button,
  :global(html[data-theme='dark']) textarea {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark']) .session-sidebar-header,
  :global(html[data-theme='dark']) .chat-toolbar,
  :global(html[data-theme='dark']) .prompt-actions,
  :global(html[data-theme='dark']) .composer {
    border-color: var(--border-soft) !important;
  }

  :global(html[data-theme='dark']) .session-item.active {
    border-color: var(--border) !important;
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark']) .session-title,
  :global(html[data-theme='dark']) .session-sidebar-header strong,
  :global(html[data-theme='dark']) .chat-toolbar h1,
  :global(html[data-theme='dark']) .empty-chat h2,
  :global(html[data-theme='dark']) .prompt-actions strong {
    color: var(--text-strong) !important;
  }

  :global(html[data-theme='dark']) .session-meta,
  :global(html[data-theme='dark']) .chat-title-group span,
  :global(html[data-theme='dark']) .prompt-actions span {
    color: var(--muted) !important;
  }

  :global(html[data-theme='dark']) .clear-all-button,
  :global(html[data-theme='dark']) .chat-toolbar-actions .clear-current-button {
    border-color: #7f1d1d !important;
    color: #fecaca !important;
    background: #450a0a !important;
  }

  :global(html[data-theme='dark']) .icon-button:hover:not(:disabled),
  :global(html[data-theme='dark']) .icon-button:focus-visible,
  :global(html[data-theme='dark']) .prompt-actions button:hover:not(:disabled),
  :global(html[data-theme='dark']) .prompt-actions button:focus-visible {
    color: #bfdbfe !important;
    border-color: var(--border) !important;
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark']) .composer .send-button {
    color: #ffffff !important;
    background: #2563eb !important;
  }

  :global(html[data-theme='dark']) .composer .send-button:hover:not(:disabled),
  :global(html[data-theme='dark']) .composer .send-button:focus-visible {
    background: #1d4ed8 !important;
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

  @media (max-width: 760px) {
    .chat-shell {
      --chat-navbar-height: 4.25rem;
      grid-template-columns: minmax(0, 1fr);
      grid-template-rows: auto minmax(0, 1fr);
      gap: 0.5rem;
      padding: calc(var(--chat-navbar-height) + 0.5rem) 0.5rem 0.5rem;
    }

    .session-sidebar {
      max-height: 7.8rem;
    }

    .session-sidebar-header {
      padding: 0.5rem 0.55rem;
    }

    .session-sidebar-actions {
      gap: 0.35rem;
    }

    .session-sidebar .icon-button {
      width: 2rem;
      height: 2rem;
    }

    .session-list {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 0.35rem;
      max-height: 4.8rem;
      padding: 0.45rem;
    }

    .session-item {
      min-height: 2.15rem;
      padding: 0.42rem 0.5rem;
    }

    .session-title {
      font-size: 0.78rem;
    }

    .session-meta {
      font-size: 0.64rem;
    }
  }

  @media (max-width: 720px) {
    .chat-toolbar {
      padding: 0.5rem 0.65rem;
    }

    .chat-toolbar-actions {
      display: flex;
    }

    .messages {
      padding: 0.6rem;
    }

    .prompt-actions {
      padding: 0.45rem 0.6rem;
      flex-wrap: nowrap;
      overflow-x: auto;
    }

    .prompt-actions button {
      flex: 0 0 auto;
    }

    .prompt-actions span {
      display: none;
    }

    .composer {
      padding: 0.55rem 0.6rem;
    }

    .composer-row {
      gap: 0.4rem;
    }

    .composer .icon-button {
      width: 2.65rem;
      height: 2.65rem;
    }

    .composer-secondary {
      padding-left: 0;
    }

    .message {
      max-width: 100%;
    }
  }
</style>
