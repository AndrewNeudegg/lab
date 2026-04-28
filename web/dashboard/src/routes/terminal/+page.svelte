<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { Navbar } from '@homelab/shared';
  import type { HomelabdAgentsResponse, HomelabdRemoteAgent } from '@homelab/shared';
  import '@xterm/xterm/css/xterm.css';
  import {
    appendQueuedTerminalInput,
    buildTerminalTargets,
    clampTerminalGeometry,
    defaultTerminalTabTitle,
    defaultTerminalGeometry,
    endpoint,
    normaliseStoredTerminalTabs,
    terminalReconnectDelay,
    terminalStatusLabel,
    terminalTabsStorageKey,
    websocketEndpoint,
    type StoredTerminalTab,
    type TerminalSessionSnapshot,
    type TerminalTarget,
    type TerminalGeometry
  } from './terminal-client';

  type TerminalSession = TerminalSessionSnapshot;
  type TerminalTab = StoredTerminalTab;

  type XtermTerminal = {
    open: (element: HTMLElement) => void;
    loadAddon: (addon: unknown) => void;
    onData: (callback: (data: string) => void) => { dispose: () => void };
    onResize: (callback: (geometry: TerminalGeometry) => void) => { dispose: () => void };
    write: (data: string | Uint8Array) => void;
    writeln: (data: string) => void;
    focus: () => void;
    clear: () => void;
    dispose: () => void;
    cols: number;
    rows: number;
  };

  type FitAddonLike = {
    fit: () => void;
  };

  type ControlButton = {
    label: string;
    hint: string;
    value: string;
  };

  type TerminalEvent = {
    type: string;
    seq?: number;
    data?: string;
    code?: number;
  };

  class TerminalRequestError extends Error {
    status: number;
    body: string;

    constructor(status: number, body: string) {
      super(body || `Request failed with ${status}`);
      this.name = 'TerminalRequestError';
      this.status = status;
      this.body = body;
    }
  }

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const controls: ControlButton[] = [
    { label: 'Ctrl-C', hint: 'Interrupt foreground job', value: '\u0003' },
    { label: 'Ctrl-D', hint: 'Send EOF', value: '\u0004' },
    { label: 'Ctrl-Z', hint: 'Suspend foreground job', value: '\u001a' },
    { label: 'Tab', hint: 'Complete command', value: '\t' },
    { label: 'Esc', hint: 'Escape', value: '\u001b' },
    { label: '←', hint: 'Left arrow', value: '\u001b[D' },
    { label: '→', hint: 'Right arrow', value: '\u001b[C' },
    { label: '↑', hint: 'Up arrow', value: '\u001b[A' },
    { label: '↓', hint: 'Down arrow', value: '\u001b[B' }
  ];

  let socket: WebSocket | undefined;
  let eventSource: EventSource | undefined;
  let terminalHost: HTMLElement | undefined;
  let terminal: XtermTerminal | undefined;
  let fitAddon: FitAddonLike | undefined;
  let resizeObserver: ResizeObserver | undefined;
  let agents: HomelabdRemoteAgent[] = [];
  let tabs: TerminalTab[] = [];
  let activeTabId = '';
  let activeTab: TerminalTab | undefined;
  let session: TerminalSession | undefined;
  let selectedTargetId = 'local';
  let loading = true;
  let connected = false;
  let error = '';
  let editingTabId = '';
  let editingTitle = '';
  let renameInput: HTMLInputElement | undefined;
  let lastResize = '';
  let resizeTimer: ReturnType<typeof setTimeout> | undefined;
  let socketFallbackTimer: ReturnType<typeof setTimeout> | undefined;
  let reconnectTimer: ReturnType<typeof setTimeout> | undefined;
  let lifecycleFitTimer: ReturnType<typeof setTimeout> | undefined;
  let reconnectAttempt = 0;
  let reconnecting = false;
  let destroyed = false;
  const lastEventSeqByTab = new Map<string, number>();
  const queuedInputByTab = new Map<string, string>();
  const queuedInputNoticeByTab = new Set<string>();

  $: statusLabel = terminalStatusLabel(connected, loading, reconnecting);
  $: terminalTargets = buildTerminalTargets(agents, apiBase);
  $: selectedTarget = terminalTargets.find((target) => target.id === selectedTargetId) || terminalTargets[0];
  $: if (terminalTargets.length > 0 && !terminalTargets.some((target) => target.id === selectedTargetId)) {
    selectedTargetId = terminalTargets[0].id;
  }
  $: activeTab = tabs.find((tab) => tab.id === activeTabId);
  $: session = activeTab?.session;

  async function requestFrom<T>(base: string, path: string, init: RequestInit = {}) {
    const headers = new Headers(init.headers);
    if (init.body && !headers.has('content-type')) {
      headers.set('content-type', 'application/json');
    }
    const response = await fetch(endpoint(base, path), { ...init, headers });
    if (!response.ok) {
      const details = await response.text();
      throw new TerminalRequestError(response.status, details || `Request failed with ${response.status}`);
    }
    return (await response.json()) as T;
  }

  const request = <T,>(path: string, init: RequestInit = {}) => requestFrom<T>(apiBase, path, init);
  const requestForTab = <T,>(tab: TerminalTab, path: string, init: RequestInit = {}) =>
    requestFrom<T>(tab.apiBase || apiBase, path, init);

  const currentGeometry = () =>
    clampTerminalGeometry({
      cols: terminal?.cols || defaultTerminalGeometry.cols,
      rows: terminal?.rows || defaultTerminalGeometry.rows
    });

  const writeNotice = (message: string) => {
    terminal?.writeln(`\r\n\x1b[90m${message}\x1b[0m`);
  };

  const rememberEventSeq = (tabId: string, event: TerminalEvent) => {
    if (typeof event.seq !== 'number' || !Number.isFinite(event.seq)) {
      return;
    }
    lastEventSeqByTab.set(tabId, Math.max(lastEventSeqByTab.get(tabId) || 0, event.seq));
  };

  const clearReconnect = () => {
    window.clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    reconnectAttempt = 0;
    reconnecting = false;
  };

  const scheduleTerminalFit = () => {
    if (!fitAddon || typeof window === 'undefined') {
      return;
    }
    window.clearTimeout(lifecycleFitTimer);
    window.requestAnimationFrame(() => {
      fitTerminal();
      lifecycleFitTimer = window.setTimeout(fitTerminal, 250);
    });
  };

  const makeTabId = () => {
    if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
      return `tab_${crypto.randomUUID()}`;
    }
    return `tab_${Date.now()}_${Math.random().toString(16).slice(2)}`;
  };

  const persistTabs = () => {
    if (typeof window === 'undefined') {
      return;
    }
    window.localStorage.setItem(terminalTabsStorageKey, JSON.stringify({ activeTabId, tabs }));
  };

  const tabTarget = (target: TerminalTarget | undefined) =>
    target || terminalTargets[0] || {
      id: 'local',
      label: 'homelabd local',
      detail: 'Control plane shell',
      apiBase,
      status: 'online'
    };

  const createTab = (target = tabTarget(selectedTarget)): TerminalTab => ({
    id: makeTabId(),
    title: defaultTerminalTabTitle(tabs.length),
    targetId: target.id,
    targetLabel: target.label,
    apiBase: target.apiBase
  });

  const loadTabs = () => {
    const fallbackTarget = tabTarget(terminalTargets[0]);
    try {
      const raw = window.localStorage.getItem(terminalTabsStorageKey);
      const restored = normaliseStoredTerminalTabs(raw ? JSON.parse(raw) : undefined, fallbackTarget);
      tabs = restored.tabs;
      activeTabId = restored.activeTabId;
    } catch {
      tabs = [];
      activeTabId = '';
    }
    if (tabs.length === 0) {
      const tab = createTab(fallbackTarget);
      tabs = [tab];
      activeTabId = tab.id;
    }
    persistTabs();
  };

  const updateTab = (tabId: string, updater: (tab: TerminalTab) => TerminalTab) => {
    tabs = tabs.map((tab) => (tab.id === tabId ? updater(tab) : tab));
    persistTabs();
  };

  const cleanTabTitle = (title: string) => title.trim().slice(0, 16) || 'Terminal';

  const startRename = async (tab: TerminalTab) => {
    if (tab.id !== activeTabId) {
      await selectTab(tab.id);
      return;
    }
    editingTabId = tab.id;
    editingTitle = (tab.title || 'Terminal').slice(0, 16);
    await tick();
    renameInput?.focus();
    renameInput?.select();
  };

  const commitRename = () => {
    const tabId = editingTabId;
    if (!tabId) {
      return;
    }
    updateTab(tabId, (tab) => ({ ...tab, title: cleanTabTitle(editingTitle) }));
    editingTabId = '';
    editingTitle = '';
    terminal?.focus();
  };

  const cancelRename = () => {
    editingTabId = '';
    editingTitle = '';
    terminal?.focus();
  };

  const handleRenameKeydown = (event: KeyboardEvent) => {
    if (event.key === 'Enter') {
      event.preventDefault();
      commitRename();
    } else if (event.key === 'Escape') {
      event.preventDefault();
      cancelRename();
    }
  };

  const sendResize = (geometry = currentGeometry()) => {
    const tab = activeTab;
    const currentSession = tab?.session;
    if (!tab || !currentSession) {
      return;
    }
    const size = clampTerminalGeometry(geometry);
    const key = `${size.cols}x${size.rows}`;
    if (key === lastResize) {
      return;
    }
    lastResize = key;
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      void requestForTab(tab, `/terminal/sessions/${encodeURIComponent(currentSession.id)}/resize`, {
        method: 'POST',
        body: JSON.stringify(size)
      }).catch((err) => {
        error = err instanceof Error ? err.message : 'Unable to resize terminal.';
      });
    }, 120);
  };

  const fitTerminal = () => {
    if (!fitAddon) {
      return;
    }
    fitAddon.fit();
    sendResize();
  };

  const flushQueuedInput = (tab: TerminalTab, currentSession: TerminalSession) => {
    const queued = queuedInputByTab.get(tab.id) || '';
    if (!queued) {
      return;
    }
    queuedInputByTab.delete(tab.id);
    queuedInputNoticeByTab.delete(tab.id);
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(queued);
      return;
    }
    void requestForTab(tab, `/terminal/sessions/${encodeURIComponent(currentSession.id)}/input`, {
      method: 'POST',
      body: JSON.stringify({ data: queued })
    }).catch((err) => {
      queuedInputByTab.set(tab.id, `${queued}${queuedInputByTab.get(tab.id) || ''}`);
      queuedInputNoticeByTab.add(tab.id);
      error = err instanceof Error ? err.message : 'Unable to flush queued terminal input.';
      scheduleReconnect(tab.id, currentSession.id);
    });
  };

  const queueInput = (tab: TerminalTab, currentSession: TerminalSession, data: string) => {
    const result = appendQueuedTerminalInput(queuedInputByTab.get(tab.id) || '', data);
    if (!result.accepted) {
      error = 'Terminal is reconnecting and queued input is full.';
      return;
    }
    queuedInputByTab.set(tab.id, result.queued);
    if (!queuedInputNoticeByTab.has(tab.id)) {
      writeNotice('[input queued until reconnect]');
      queuedInputNoticeByTab.add(tab.id);
    }
    if (!reconnectTimer && !reconnecting) {
      reconnecting = true;
      void reconnectActiveTab(tab.id, currentSession.id);
    }
  };

  const sendData = (data: string) => {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(data);
      return;
    }
    const tab = activeTab;
    const currentSession = tab?.session;
    if (eventSource && connected && tab && currentSession) {
      void requestForTab(tab, `/terminal/sessions/${encodeURIComponent(currentSession.id)}/input`, {
        method: 'POST',
        body: JSON.stringify({ data })
      }).catch((err) => {
        error = err instanceof Error ? err.message : 'Unable to send terminal input.';
      });
      return;
    }
    if (tab && currentSession) {
      queueInput(tab, currentSession, data);
      return;
    }
    writeNotice('[terminal is connecting]');
  };

  const closeSocket = () => {
    if (typeof window !== 'undefined') {
      window.clearTimeout(socketFallbackTimer);
    }
    const current = socket;
    socket = undefined;
    if (!current) {
      return;
    }
    current.onopen = null;
    current.onmessage = null;
    current.onerror = null;
    current.onclose = null;
    current.close();
  };

  const closeEventSource = () => {
    const current = eventSource;
    eventSource = undefined;
    if (!current) {
      return;
    }
    current.onopen = null;
    current.onerror = null;
    current.close();
  };

  const disconnectTransport = () => {
    clearReconnect();
    closeSocket();
    closeEventSource();
    connected = false;
  };

  const scheduleReconnect = (tabId: string, sessionId: string) => {
    if (destroyed || typeof window === 'undefined') {
      return;
    }
    const tab = tabs.find((candidate) => candidate.id === tabId);
    if (activeTabId !== tabId || tab?.session?.id !== sessionId) {
      return;
    }
    connected = false;
    reconnecting = true;
    loading = false;
    error = 'Terminal disconnected. Reconnecting...';
    window.clearTimeout(reconnectTimer);
    const delay = terminalReconnectDelay(reconnectAttempt);
    reconnectAttempt += 1;
    reconnectTimer = window.setTimeout(() => {
      reconnectTimer = undefined;
      void reconnectActiveTab(tabId, sessionId);
    }, delay);
  };

  const reconnectActiveTab = async (tabId: string, sessionId: string) => {
    if (destroyed) {
      return;
    }
    const tab = tabs.find((candidate) => candidate.id === tabId);
    if (activeTabId !== tabId || tab?.session?.id !== sessionId) {
      return;
    }
    if (typeof navigator !== 'undefined' && navigator.onLine === false) {
      scheduleReconnect(tabId, sessionId);
      return;
    }
    try {
      const resumed = await requestForTab<TerminalSession>(tab, `/terminal/sessions/${encodeURIComponent(sessionId)}`);
      updateTab(tab.id, (current) => ({ ...current, session: resumed }));
      const geometry = currentGeometry();
      lastResize = `${geometry.cols}x${geometry.rows}`;
      connectTransport(tab, resumed, false);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to reconnect terminal.';
      scheduleReconnect(tabId, sessionId);
    }
  };

  const handleTerminalEvent = (event: TerminalEvent, tabId: string, sessionId: string) => {
    if (activeTabId !== tabId || activeTab?.session?.id !== sessionId) {
      return;
    }
    rememberEventSeq(tabId, event);
    switch (event.type) {
      case 'output':
        if (event.data) {
          terminal?.write(event.data);
        }
        break;
      case 'exit':
        terminal?.write(`\r\n[process exited ${event.code ?? 0}]\r\n`);
        disconnectTransport();
        queuedInputByTab.delete(tabId);
        queuedInputNoticeByTab.delete(tabId);
        updateTab(tabId, (tab) => ({ ...tab, session: undefined }));
        break;
      case 'error':
        if (event.data) {
          terminal?.write(`\r\n[terminal error: ${event.data}]\r\n`);
        }
        break;
    }
  };

  const connectEventStream = (tab: TerminalTab, nextSession: TerminalSession, replayHistory = true) => {
    closeSocket();
    closeEventSource();
    const after = replayHistory ? 0 : lastEventSeqByTab.get(tab.id) || 0;
    const streamPath = `/terminal/sessions/${encodeURIComponent(nextSession.id)}/events${after > 0 ? `?after=${after}` : ''}`;
    const source = new EventSource(endpoint(tab.apiBase || apiBase, streamPath));
    eventSource = source;
    source.onopen = () => {
      if (eventSource !== source) {
        return;
      }
      if (activeTabId !== tab.id) {
        return;
      }
      clearReconnect();
      connected = true;
      error = '';
      terminal?.focus();
      scheduleTerminalFit();
      flushQueuedInput(tab, nextSession);
    };
    source.onerror = () => {
      if (eventSource !== source) {
        return;
      }
      if (activeTabId !== tab.id) {
        return;
      }
      source.close();
      eventSource = undefined;
      connected = false;
      scheduleReconnect(tab.id, nextSession.id);
    };
    for (const eventType of ['output', 'exit', 'error']) {
      source.addEventListener(eventType, (message) => {
        try {
          handleTerminalEvent(JSON.parse((message as MessageEvent).data) as TerminalEvent, tab.id, nextSession.id);
        } catch {
          error = 'Terminal event stream returned invalid data.';
        }
      });
    }
  };

  const connectSocket = (tab: TerminalTab, nextSession: TerminalSession) => {
    closeSocket();
    closeEventSource();
    const url = websocketEndpoint(tab.apiBase || apiBase, `/terminal/sessions/${encodeURIComponent(nextSession.id)}/ws`, window.location);
    const nextSocket = new WebSocket(url);
    socket = nextSocket;
    nextSocket.binaryType = 'arraybuffer';
    socketFallbackTimer = window.setTimeout(() => {
      if (socket === nextSocket && nextSocket.readyState !== WebSocket.OPEN) {
        connectEventStream(tab, nextSession, false);
      }
    }, 1000);
    nextSocket.onopen = () => {
      if (socket !== nextSocket) {
        return;
      }
      if (activeTabId !== tab.id) {
        return;
      }
      window.clearTimeout(socketFallbackTimer);
      clearReconnect();
      connected = true;
      error = '';
      terminal?.focus();
      scheduleTerminalFit();
      flushQueuedInput(tab, nextSession);
    };
    nextSocket.onmessage = (event) => {
      if (socket !== nextSocket) {
        return;
      }
      if (activeTabId !== tab.id) {
        return;
      }
      if (event.data instanceof ArrayBuffer) {
        terminal?.write(new Uint8Array(event.data));
      } else if (event.data instanceof Blob) {
        void event.data.arrayBuffer().then((buffer) => terminal?.write(new Uint8Array(buffer)));
      } else {
        terminal?.write(String(event.data));
      }
    };
    nextSocket.onerror = () => {
      if (socket !== nextSocket) {
        return;
      }
      if (activeTabId === tab.id) {
        connectEventStream(tab, nextSession, false);
      }
    };
    nextSocket.onclose = () => {
      if (socket !== nextSocket) {
        return;
      }
      socket = undefined;
      if (activeTabId === tab.id && !eventSource) {
        connected = false;
        scheduleReconnect(tab.id, nextSession.id);
      }
    };
  };

  const connectTransport = (tab: TerminalTab, nextSession: TerminalSession, replayHistory = true) => {
    if (typeof EventSource !== 'undefined') {
      connectEventStream(tab, nextSession, replayHistory);
      return;
    }
    connectSocket(tab, nextSession);
  };

  const ensureSession = async (tab: TerminalTab, geometry: TerminalGeometry) => {
    if (tab.session) {
      try {
        const resumed = await requestForTab<TerminalSession>(tab, `/terminal/sessions/${encodeURIComponent(tab.session.id)}`);
        updateTab(tab.id, (current) => ({ ...current, session: resumed }));
        return resumed;
      } catch (err) {
        if (err instanceof TerminalRequestError && err.status === 404) {
          updateTab(tab.id, (current) => ({ ...current, session: undefined }));
        } else {
          throw err;
        }
      }
    }
    const created = await requestForTab<TerminalSession>(tab, '/terminal/sessions', {
      method: 'POST',
      body: JSON.stringify(geometry)
    });
    updateTab(tab.id, (current) => ({ ...current, session: created }));
    return created;
  };

  const connectActiveTab = async () => {
    const tab = activeTab;
    if (!tab) {
      return;
    }
    loading = true;
    error = '';
    disconnectTransport();
    try {
      terminal?.clear();
      fitAddon?.fit();
      const geometry = currentGeometry();
      const nextSession = await ensureSession(tab, geometry);
      lastEventSeqByTab.set(tab.id, 0);
      lastResize = `${geometry.cols}x${geometry.rows}`;
      terminal?.writeln(`\x1b[90m${tab.title || 'Terminal'} · ${tab.targetLabel} · ${nextSession.cwd}\x1b[0m`);
      connectTransport(tab, nextSession, true);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to connect terminal.';
      writeNotice(`[${error}]`);
      if (tab.session) {
        scheduleReconnect(tab.id, tab.session.id);
      }
    } finally {
      loading = false;
      terminal?.focus();
    }
  };

  const selectTab = async (tabId: string) => {
    if (tabId === activeTabId) {
      terminal?.focus();
      return;
    }
    editingTabId = '';
    editingTitle = '';
    activeTabId = tabId;
    persistTabs();
    await tick();
    await connectActiveTab();
  };

  const addTab = async () => {
    const target = tabTarget(selectedTarget);
    const tab = createTab(target);
    tabs = [...tabs, tab];
    activeTabId = tab.id;
    selectedTargetId = target.id;
    persistTabs();
    await tick();
    await connectActiveTab();
  };

  const closeTab = async (tabId: string) => {
    const closing = tabs.find((tab) => tab.id === tabId);
    if (!closing) {
      return;
    }
    const closingIndex = tabs.findIndex((tab) => tab.id === tabId);
    const wasActive = activeTabId === tabId;
    if (editingTabId === tabId) {
      editingTabId = '';
      editingTitle = '';
    }
    queuedInputByTab.delete(tabId);
    queuedInputNoticeByTab.delete(tabId);
    if (wasActive) {
      disconnectTransport();
    }
    tabs = tabs.filter((tab) => tab.id !== tabId);
    if (tabs.length === 0) {
      tabs = [createTab(tabTarget(selectedTarget))];
    }
    if (wasActive) {
      activeTabId = tabs[Math.max(0, closingIndex - 1)]?.id || tabs[0].id;
    }
    persistTabs();
    if (closing.session) {
      try {
        await requestForTab(closing, `/terminal/sessions/${encodeURIComponent(closing.session.id)}`, { method: 'DELETE' });
      } catch {
        // The shell may already have exited.
      }
    }
    if (wasActive) {
      await tick();
      await connectActiveTab();
    }
  };

  const loadAgents = async () => {
    try {
      const response = await request<HomelabdAgentsResponse>('/agents');
      agents = response.agents || [];
    } catch {
      agents = [];
    }
  };

  const recoverActiveTab = () => {
    scheduleTerminalFit();
    const tab = activeTab;
    const currentSession = tab?.session;
    if (!tab || !currentSession || loading) {
      return;
    }
    if (connected) {
      sendResize();
      return;
    }
    window.clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    reconnecting = true;
    void reconnectActiveTab(tab.id, currentSession.id);
  };

  const handleVisibilityChange = () => {
    if (document.visibilityState === 'visible') {
      recoverActiveTab();
    }
  };

  const initialiseTerminal = async () => {
    const [{ Terminal }, { FitAddon }, { WebLinksAddon }] = await Promise.all([
      import('@xterm/xterm'),
      import('@xterm/addon-fit'),
      import('@xterm/addon-web-links')
    ]);
    terminal = new Terminal({
      cursorBlink: true,
      convertEol: false,
      scrollback: 10000,
      fontFamily: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
      fontSize: 14,
      lineHeight: 1.15,
      allowProposedApi: false,
      theme: {
        background: '#07130e',
        foreground: '#d8f3dc',
        cursor: '#f8fafc',
        selectionBackground: '#334155',
        black: '#0f172a',
        red: '#ef4444',
        green: '#22c55e',
        yellow: '#eab308',
        blue: '#3b82f6',
        magenta: '#a855f7',
        cyan: '#06b6d4',
        white: '#e5e7eb',
        brightBlack: '#64748b',
        brightRed: '#f87171',
        brightGreen: '#4ade80',
        brightYellow: '#fde047',
        brightBlue: '#60a5fa',
        brightMagenta: '#c084fc',
        brightCyan: '#22d3ee',
        brightWhite: '#f8fafc'
      }
    }) as XtermTerminal;
    fitAddon = new FitAddon();
    terminal.loadAddon(fitAddon);
    terminal.loadAddon(new WebLinksAddon());
    terminal.onData(sendData);
    terminal.onResize(sendResize);
    terminal.open(terminalHost!);
    await tick();
    fitTerminal();
    resizeObserver = new ResizeObserver(fitTerminal);
    resizeObserver.observe(terminalHost!);
    window.addEventListener('resize', fitTerminal);
    window.visualViewport?.addEventListener('resize', scheduleTerminalFit);
    window.visualViewport?.addEventListener('scroll', scheduleTerminalFit);
    window.addEventListener('online', recoverActiveTab);
    window.addEventListener('focus', recoverActiveTab);
    window.addEventListener('pageshow', recoverActiveTab);
    document.addEventListener('visibilitychange', handleVisibilityChange);
  };

  onMount(() => {
    destroyed = false;
    void Promise.all([initialiseTerminal(), loadAgents()]).then(async () => {
      loadTabs();
      await tick();
      await connectActiveTab();
    }).catch((err) => {
      loading = false;
      error = err instanceof Error ? err.message : 'Unable to initialise terminal.';
    });
  });

  onDestroy(() => {
    destroyed = true;
    if (typeof window !== 'undefined') {
      window.clearTimeout(resizeTimer);
      window.clearTimeout(socketFallbackTimer);
      window.clearTimeout(reconnectTimer);
      window.clearTimeout(lifecycleFitTimer);
      window.removeEventListener('resize', fitTerminal);
      window.visualViewport?.removeEventListener('resize', scheduleTerminalFit);
      window.visualViewport?.removeEventListener('scroll', scheduleTerminalFit);
      window.removeEventListener('online', recoverActiveTab);
      window.removeEventListener('focus', recoverActiveTab);
      window.removeEventListener('pageshow', recoverActiveTab);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      disconnectTransport();
      persistTabs();
    }
    resizeObserver?.disconnect();
    terminal?.dispose();
  });
</script>

<svelte:head>
  <title>homelabd Terminal</title>
  <meta name="description" content="Interactive PTY terminal for homelabd operators" />
</svelte:head>

<div class="terminal-shell">
  <Navbar title="Terminal" subtitle="homelabd" current="/terminal" />

  <main class="terminal-panel">
    <header class="terminal-header">
      <nav class="terminal-tabs" aria-label="Terminal tabs">
        {#each tabs as tab}
          <div class:active={tab.id === activeTabId} class="terminal-tab">
            {#if editingTabId === tab.id}
              <input
                bind:this={renameInput}
                bind:value={editingTitle}
                class="tab-title-editor"
                aria-label="Rename terminal tab"
                maxlength="16"
                spellcheck="false"
                on:keydown={handleRenameKeydown}
                on:blur={commitRename}
              />
            {:else}
              <button
                type="button"
                class="tab-select"
                aria-current={tab.id === activeTabId ? 'page' : undefined}
                title={tab.id === activeTabId ? 'Rename tab' : `Switch to ${tab.title || 'Terminal'}`}
                on:click={() => void startRename(tab)}
              >
                <span>{tab.title || 'Terminal'}</span>
              </button>
            {/if}
            <button
              type="button"
              class="tab-close"
              title="Close tab"
              aria-label={`Close ${tab.title || 'terminal tab'}`}
              on:click={() => void closeTab(tab.id)}
            >x</button>
          </div>
        {/each}
      </nav>
      <div class="terminal-actions">
        <span class:connected class="status-pill">{statusLabel}</span>
        {#if error}
          <span class="status-error" role="alert">{error}</span>
        {/if}
        <div class="new-tab-control">
          <select class="target-picker" aria-label="Session target" bind:value={selectedTargetId} disabled={loading}>
            {#each terminalTargets as target}
              <option value={target.id}>{target.label} - {target.detail}</option>
            {/each}
          </select>
          <button
            type="button"
            class="tab-add"
            title="Add terminal tab"
            aria-label="Add terminal tab"
            disabled={loading}
            on:click={() => void addTab()}
          >+</button>
        </div>
      </div>
    </header>

    <div
      class="terminal-host"
      bind:this={terminalHost}
      role="application"
      aria-label="Interactive terminal"
    ></div>

    <section class="terminal-controls" aria-label="Terminal control keys">
      {#each controls as control}
        <button
          type="button"
          disabled={!session || loading}
          title={control.hint}
          aria-label={`${control.label}: ${control.hint}`}
          on:click={() => {
            sendData(control.value);
            terminal?.focus();
          }}
        >
          <strong>{control.label}</strong>
        </button>
      {/each}
    </section>
  </main>
</div>

<style>
  :global(html),
  :global(body),
  :global(body > div) {
    height: 100%;
  }

  :global(*),
  :global(*::before),
  :global(*::after) {
    box-sizing: border-box;
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
  input,
  select {
    font: inherit;
  }

  button,
  select {
    -webkit-tap-highlight-color: transparent;
  }

  .terminal-shell {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    height: 100vh;
    height: 100svh;
    height: 100dvh;
    min-height: 100vh;
    min-height: 100svh;
    min-height: 100dvh;
    overflow: hidden;
  }

  .terminal-panel {
    display: grid;
    grid-template-areas:
      "header"
      "terminal"
      "controls";
    grid-template-rows: auto minmax(0, 1fr) auto;
    min-height: 0;
    width: 100%;
    margin: 0;
    background: #07130e;
    box-shadow: none;
  }

  .terminal-header {
    grid-area: header;
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 0.5rem;
    min-width: 0;
    min-height: 2.85rem;
    padding: 0.35rem 0.55rem 0;
    overflow: hidden;
    border-bottom: 1px solid #07130e;
    background: #e8edf4;
  }

  .terminal-tabs {
    display: flex;
    flex: 1 1 auto;
    align-items: flex-end;
    gap: 0.12rem;
    min-width: 0;
    overflow-x: auto;
    overscroll-behavior-x: contain;
    scrollbar-width: thin;
  }

  .terminal-tab {
    display: inline-flex;
    flex: 0 0 auto;
    align-items: stretch;
    min-width: 0;
    max-width: 14rem;
    overflow: hidden;
    margin-bottom: -1px;
    color: #243047;
    border: 1px solid #aab6c7;
    border-bottom: 0;
    border-radius: 0.48rem 0.48rem 0 0;
    background: #dbe3ef;
    transition:
      border-color 120ms ease,
      background 120ms ease,
      color 120ms ease;
  }

  .terminal-tab.active {
    color: #d8f3dc;
    border-color: #07130e;
    background: #07130e;
  }

  .tab-select,
  .tab-close,
  .tab-add {
    font-weight: 850;
    cursor: pointer;
    transition:
      background 120ms ease,
      border-color 120ms ease,
      color 120ms ease,
      box-shadow 120ms ease,
      filter 120ms ease;
  }

  .tab-select {
    min-height: 2.4rem;
    min-width: 4.5rem;
    max-width: 10.5rem;
    padding: 0 0.65rem;
    overflow: hidden;
    color: inherit;
    border: 0;
    background: transparent;
    text-align: left;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tab-select span {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .tab-close {
    align-self: center;
    width: 1.65rem;
    height: 1.65rem;
    margin-right: 0.25rem;
    color: inherit;
    border: 1px solid transparent;
    border-radius: 0.35rem;
    background: transparent;
    line-height: 1;
  }

  .terminal-tab:not(.active):hover {
    border-color: #7d8ca3;
    background: #edf2f8;
  }

  .terminal-tab.active:hover {
    background: #0b1f17;
  }

  .tab-select:hover {
    background: rgb(255 255 255 / 0.45);
  }

  .terminal-tab.active .tab-select:hover {
    background: rgb(255 255 255 / 0.08);
  }

  .tab-close:hover {
    color: #991b1b;
    border-color: #fecaca;
    background: #fee2e2;
  }

  .terminal-tab.active .tab-close:hover {
    color: #fecaca;
    border-color: #7f1d1d;
    background: #2a1010;
  }

  .tab-title-editor {
    width: min(10rem, calc(100vw - 13rem));
    height: 2.2rem;
    margin: 0.1rem 0.25rem 0.1rem 0.35rem;
    padding: 0 0.5rem;
    color: #0f172a;
    border: 1px solid #60a5fa;
    border-radius: 0.35rem;
    background: #ffffff;
    font-size: 0.86rem;
    font-weight: 850;
  }

  .terminal-actions {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    justify-content: flex-end;
    min-width: 0;
    padding-bottom: 0.35rem;
  }

  .new-tab-control {
    display: inline-flex;
    align-items: stretch;
    min-width: 0;
  }

  .target-picker {
    width: clamp(11rem, 20vw, 20rem);
    min-height: 2.2rem;
    padding: 0 0.45rem;
    border: 1px solid #cbd5e1;
    border-right: 0;
    border-radius: 0.45rem 0 0 0.45rem;
    color: #243047;
    background: #ffffff;
    font-size: 0.82rem;
    font-weight: 850;
  }

  .target-picker:hover:not(:disabled) {
    border-color: #93a3bb;
    background: #f8fafc;
  }

  .tab-add {
    flex: 0 0 auto;
    width: 2.35rem;
    min-height: 2.2rem;
    color: #ffffff;
    border: 1px solid #1d4ed8;
    border-radius: 0 0.45rem 0.45rem 0;
    background: #2563eb;
    font-size: 1.1rem;
    line-height: 1;
  }

  .tab-add:hover:not(:disabled) {
    border-color: #1e40af;
    background: #1d4ed8;
    box-shadow: 0 0 0 3px rgb(37 99 235 / 0.16);
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    min-height: 2rem;
    padding: 0 0.6rem;
    border: 1px solid #fed7aa;
    border-radius: 999px;
    color: #9a3412;
    background: #fff7ed;
    font-size: 0.78rem;
    font-weight: 900;
  }

  .status-pill.connected {
    color: #166534;
    border-color: #bbf7d0;
    background: #f0fdf4;
  }

  .status-error {
    max-width: 18rem;
    overflow: hidden;
    color: #991b1b;
    font-size: 0.78rem;
    font-weight: 700;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  button:focus-visible,
  .target-picker:focus-visible,
  .tab-title-editor:focus-visible {
    position: relative;
    z-index: 2;
    outline: 2px solid #3b82f6;
    outline-offset: 2px;
  }

  .terminal-tab.active .tab-select:focus-visible,
  .terminal-tab.active .tab-close:focus-visible {
    outline-color: #93c5fd;
  }

  button:active:not(:disabled) {
    filter: brightness(0.94);
    box-shadow: inset 0 1px 3px rgb(15 23 42 / 0.18);
  }

  .terminal-host {
    grid-area: terminal;
    min-width: 0;
    min-height: 0;
    padding: 0.5rem;
    overflow: hidden;
    background: #07130e;
  }

  .terminal-host:focus {
    outline: 3px solid rgb(59 130 246 / 0.35);
    outline-offset: -3px;
  }

  :global(.terminal-host .xterm) {
    width: 100%;
    height: 100%;
    padding: 0.25rem;
  }

  :global(.terminal-host .xterm-screen) {
    height: 100%;
  }

  :global(.terminal-host .xterm-viewport) {
    scrollbar-color: #475569 #07130e;
  }

  .terminal-controls {
    grid-area: controls;
    display: flex;
    align-items: center;
    gap: 0.45rem;
    padding: 0.55rem 1rem;
    border-top: 1px solid #1f2937;
    background: #111827;
  }

  .terminal-controls button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 4.5rem;
    min-height: 2.5rem;
    padding: 0 0.65rem;
    color: #e2e8f0;
    cursor: pointer;
    border: 1px solid #334155;
    border-radius: 0.45rem;
    background: #1f2937;
  }

  .terminal-controls button:hover:not(:disabled) {
    color: #ffffff;
    border-color: #64748b;
    background: #2d3748;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  @media (max-width: 760px) {
    .terminal-shell {
      height: 100vh;
      height: 100svh;
      height: 100dvh;
      min-height: 100vh;
      min-height: 100svh;
      min-height: 100dvh;
    }

    .terminal-panel {
      height: 100%;
      width: 100%;
    }

    .terminal-header {
      display: grid;
      grid-template-columns: minmax(0, 1fr);
      align-items: stretch;
      gap: 0.35rem;
      min-height: 2.7rem;
      padding: 0.3rem 0.4rem;
    }

    .terminal-actions {
      width: 100%;
      gap: 0.3rem;
      justify-content: space-between;
      padding-bottom: 0;
    }

    .terminal-tabs {
      min-width: 0;
    }

    .terminal-tab {
      max-width: 9.5rem;
    }

    .tab-select {
      min-height: 2.35rem;
      min-width: 3.75rem;
      max-width: 7rem;
      padding: 0 0.5rem;
      font-size: 0.78rem;
    }

    .tab-close {
      width: 1.55rem;
      height: 1.55rem;
      margin-right: 0.16rem;
    }

    .tab-title-editor {
      width: 7rem;
      font-size: 0.78rem;
    }

    .new-tab-control {
      flex: 1 1 auto;
    }

    .target-picker {
      flex: 1 1 auto;
      width: auto;
      min-width: 0;
      min-height: 2.15rem;
      padding: 0 0.25rem;
      font-size: 0.72rem;
    }

    .tab-add {
      flex: 0 0 2.15rem;
      width: 2.15rem;
      min-height: 2.15rem;
    }

    .status-pill {
      min-height: 1.95rem;
      padding: 0 0.45rem;
      font-size: 0.72rem;
    }

    .status-error {
      max-width: 10rem;
    }

    .terminal-host {
      padding: 0.35rem;
    }

    .terminal-controls {
      flex-wrap: nowrap;
      gap: 0.45rem;
      padding: 0.55rem 0.65rem;
      overflow-x: auto;
      overscroll-behavior-x: contain;
      scrollbar-width: thin;
    }

    .terminal-controls button {
      flex: 0 0 auto;
      min-width: 3.75rem;
      min-height: 2.75rem;
      padding: 0 0.45rem;
      font-size: 0.78rem;
    }
  }
</style>
