<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { Navbar } from '@homelab/shared';
  import type { HomelabdAgentsResponse, HomelabdRemoteAgent } from '@homelab/shared';
  import '@xterm/xterm/css/xterm.css';
  import {
    buildTerminalTargets,
    clampTerminalGeometry,
    defaultTerminalGeometry,
    endpoint,
    terminalStatusLabel,
    websocketEndpoint,
    type TerminalTarget,
    type TerminalGeometry
  } from './terminal-client';

  type TerminalSession = {
    id: string;
    shell: string;
    cwd: string;
    created_at: string;
  };

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
    data?: string;
    code?: number;
  };

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

  let session: TerminalSession | undefined;
  let socket: WebSocket | undefined;
  let eventSource: EventSource | undefined;
  let terminalHost: HTMLElement | undefined;
  let terminal: XtermTerminal | undefined;
  let fitAddon: FitAddonLike | undefined;
  let resizeObserver: ResizeObserver | undefined;
  let agents: HomelabdRemoteAgent[] = [];
  let selectedTargetId = 'local';
  let activeTarget: TerminalTarget | undefined;
  let loading = true;
  let connected = false;
  let error = '';
  let lastResize = '';
  let resizeTimer: ReturnType<typeof setTimeout> | undefined;
  let socketFallbackTimer: ReturnType<typeof setTimeout> | undefined;

  $: statusLabel = terminalStatusLabel(connected, loading);
  $: terminalTargets = buildTerminalTargets(agents, apiBase);
  $: selectedTarget = terminalTargets.find((target) => target.id === selectedTargetId) || terminalTargets[0];
  $: if (terminalTargets.length > 0 && !terminalTargets.some((target) => target.id === selectedTargetId)) {
    selectedTargetId = terminalTargets[0].id;
  }

  async function requestFrom<T>(base: string, path: string, init: RequestInit = {}) {
    const headers = new Headers(init.headers);
    if (init.body && !headers.has('content-type')) {
      headers.set('content-type', 'application/json');
    }
    const response = await fetch(endpoint(base, path), { ...init, headers });
    if (!response.ok) {
      const details = await response.text();
      throw new Error(details || `Request failed with ${response.status}`);
    }
    return (await response.json()) as T;
  }

  const request = <T,>(path: string, init: RequestInit = {}) => requestFrom<T>(apiBase, path, init);
  const terminalRequest = <T,>(path: string, init: RequestInit = {}) =>
    requestFrom<T>(activeTarget?.apiBase || selectedTarget?.apiBase || apiBase, path, init);

  const currentGeometry = () =>
    clampTerminalGeometry({
      cols: terminal?.cols || defaultTerminalGeometry.cols,
      rows: terminal?.rows || defaultTerminalGeometry.rows
    });

  const writeNotice = (message: string) => {
    terminal?.writeln(`\r\n\x1b[90m${message}\x1b[0m`);
  };

  const sendResize = (geometry = currentGeometry()) => {
    if (!session) {
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
      void terminalRequest(`/terminal/sessions/${encodeURIComponent(session!.id)}/resize`, {
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

  const sendData = (data: string) => {
    if (socket?.readyState === WebSocket.OPEN) {
      socket.send(data);
      return;
    }
    if (eventSource && session) {
      void terminalRequest(`/terminal/sessions/${encodeURIComponent(session.id)}/input`, {
        method: 'POST',
        body: JSON.stringify({ data })
      }).catch((err) => {
        error = err instanceof Error ? err.message : 'Unable to send terminal input.';
      });
      return;
    }
    writeNotice('[terminal is connecting]');
  };

  const closeSocket = () => {
    if (typeof window !== 'undefined') {
      window.clearTimeout(socketFallbackTimer);
    }
    socket?.close();
    socket = undefined;
  };

  const closeEventSource = () => {
    eventSource?.close();
    eventSource = undefined;
  };

  const disconnectTransport = () => {
    closeSocket();
    closeEventSource();
    connected = false;
  };

  const closeSession = async () => {
    disconnectTransport();
    if (!session) {
      return;
    }
    const closing = session.id;
    session = undefined;
    try {
      await terminalRequest(`/terminal/sessions/${encodeURIComponent(closing)}`, { method: 'DELETE' });
    } catch {
      // The shell may already have exited.
    }
  };

  const handleTerminalEvent = (event: TerminalEvent) => {
    switch (event.type) {
      case 'output':
        if (event.data) {
          terminal?.write(event.data);
        }
        break;
      case 'exit':
        terminal?.write(`\r\n[process exited ${event.code ?? 0}]\r\n`);
        disconnectTransport();
        break;
      case 'error':
        if (event.data) {
          terminal?.write(`\r\n[terminal error: ${event.data}]\r\n`);
        }
        break;
    }
  };

  const connectEventStream = (nextSession: TerminalSession) => {
    closeSocket();
    closeEventSource();
    eventSource = new EventSource(endpoint(activeTarget?.apiBase || apiBase, `/terminal/sessions/${encodeURIComponent(nextSession.id)}/events`));
    eventSource.onopen = () => {
      connected = true;
      error = '';
      terminal?.focus();
      fitTerminal();
    };
    eventSource.onerror = () => {
      connected = false;
      error = 'Terminal event stream disconnected.';
    };
    for (const eventType of ['output', 'exit', 'error']) {
      eventSource.addEventListener(eventType, (message) => {
        try {
          handleTerminalEvent(JSON.parse((message as MessageEvent).data) as TerminalEvent);
        } catch {
          error = 'Terminal event stream returned invalid data.';
        }
      });
    }
  };

  const connectSocket = (nextSession: TerminalSession) => {
    closeSocket();
    closeEventSource();
    const url = websocketEndpoint(activeTarget?.apiBase || apiBase, `/terminal/sessions/${encodeURIComponent(nextSession.id)}/ws`, window.location);
    socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    socketFallbackTimer = window.setTimeout(() => {
      if (socket?.readyState !== WebSocket.OPEN) {
        connectEventStream(nextSession);
      }
    }, 1000);
    socket.onopen = () => {
      window.clearTimeout(socketFallbackTimer);
      connected = true;
      error = '';
      terminal?.focus();
      fitTerminal();
    };
    socket.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        terminal?.write(new Uint8Array(event.data));
      } else if (event.data instanceof Blob) {
        void event.data.arrayBuffer().then((buffer) => terminal?.write(new Uint8Array(buffer)));
      } else {
        terminal?.write(String(event.data));
      }
    };
    socket.onerror = () => {
      connectEventStream(nextSession);
    };
    socket.onclose = () => {
      if (!eventSource) {
        connected = false;
      }
    };
  };

  const startSession = async () => {
    loading = true;
    error = '';
    try {
      await closeSession();
      terminal?.clear();
      fitAddon?.fit();
      const geometry = currentGeometry();
      activeTarget = selectedTarget;
      session = await terminalRequest<TerminalSession>('/terminal/sessions', {
        method: 'POST',
        body: JSON.stringify(geometry)
      });
      lastResize = `${geometry.cols}x${geometry.rows}`;
      terminal?.writeln(`\x1b[90mConnected to ${session.shell} in ${session.cwd} on ${activeTarget?.label || 'homelabd local'}\x1b[0m`);
      connectSocket(session);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to start terminal.';
      writeNotice(`[${error}]`);
    } finally {
      loading = false;
      terminal?.focus();
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
  };

  onMount(() => {
    void Promise.all([initialiseTerminal(), loadAgents()]).then(startSession).catch((err) => {
      loading = false;
      error = err instanceof Error ? err.message : 'Unable to initialise terminal.';
    });
  });

  onDestroy(() => {
    if (typeof window !== 'undefined') {
      window.clearTimeout(resizeTimer);
      window.clearTimeout(socketFallbackTimer);
      window.removeEventListener('resize', fitTerminal);
      void closeSession();
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
      <div>
        <p class="eyebrow">Operator PTY</p>
        <h1>{session ? session.cwd : 'Starting shell'}</h1>
        <p class="shell-meta">{session?.shell || 'Preparing a real interactive terminal'}</p>
      </div>
      <div class="terminal-actions">
        <span class:connected class="status-pill">{statusLabel}</span>
        <label class="target-picker">
          <span>Session target</span>
          <select bind:value={selectedTargetId} disabled={loading}>
            {#each terminalTargets as target}
              <option value={target.id}>{target.label} — {target.detail}</option>
            {/each}
          </select>
        </label>
        <button type="button" on:click={() => terminal?.clear()}>Clear</button>
        <button type="button" on:click={() => void startSession()} disabled={loading}>New session</button>
      </div>
    </header>

    <p class:error class="terminal-notice" role={error ? 'alert' : 'status'}>
      {error || 'Click in the terminal and type normally. ANSI colours, prompts, tab completion, and interactive CLIs use a real PTY.'}
    </p>

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
          disabled={!session || !connected}
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

  :global(body) {
    margin: 0;
    color: #172033;
    background: #eef2f7;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
  }

  button {
    font: inherit;
  }

  .terminal-shell {
    display: grid;
    grid-template-rows: auto minmax(0, 1fr);
    height: 100dvh;
    min-height: 100dvh;
    overflow: hidden;
  }

  .terminal-panel {
    display: grid;
    grid-template-areas:
      "header"
      "notice"
      "terminal"
      "controls";
    grid-template-rows: auto auto minmax(0, 1fr) auto;
    min-height: 0;
    width: min(100%, 76rem);
    margin: 0 auto;
    background: #07130e;
    box-shadow: 0 18px 50px rgb(15 23 42 / 0.12);
  }

  .terminal-header {
    grid-area: header;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.85rem 1rem;
    border-bottom: 1px solid #dde4ef;
    background: #ffffff;
  }

  .terminal-header h1,
  .terminal-header p {
    margin: 0;
  }

  .eyebrow {
    color: #64748b;
    font-size: 0.72rem;
    font-weight: 900;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .terminal-header h1 {
    max-width: min(58vw, 44rem);
    overflow: hidden;
    color: #0f172a;
    font-size: 1.02rem;
    line-height: 1.2;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .terminal-header .shell-meta {
    max-width: min(62vw, 48rem);
    overflow: hidden;
    color: #64748b;
    font-size: 0.78rem;
    font-weight: 700;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .terminal-actions,
  .terminal-controls {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
  }

  .terminal-actions {
    align-items: center;
    justify-content: flex-end;
  }

  .terminal-actions button,
  .terminal-controls button {
    min-height: 2.5rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.5rem;
    color: #243047;
    background: #ffffff;
    font-weight: 850;
  }

  .terminal-actions button {
    padding: 0 0.75rem;
  }

  .target-picker {
    display: grid;
    gap: 0.15rem;
    color: #475569;
    font-size: 0.68rem;
    font-weight: 900;
    text-transform: uppercase;
  }

  .target-picker select {
    max-width: min(34rem, 44vw);
    min-height: 2.5rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.5rem;
    color: #243047;
    background: #ffffff;
    font: inherit;
    font-size: 0.82rem;
    font-weight: 800;
    text-transform: none;
  }

  :global(html[data-theme='dark'] .target-picker select) {
    color: #e2e8f0;
    border-color: #334155;
    background: #111827;
  }

  .status-pill {
    display: inline-flex;
    align-items: center;
    min-height: 2rem;
    padding: 0 0.65rem;
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

  .terminal-notice {
    grid-area: notice;
    margin: 0;
    padding: 0.62rem 1rem;
    color: #475569;
    border-bottom: 1px solid #dde4ef;
    background: #f8fafc;
    font-size: 0.82rem;
    font-weight: 700;
    overflow-wrap: anywhere;
  }

  .terminal-notice.error {
    color: #991b1b;
    background: #fef2f2;
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
    height: 100%;
    padding: 0.25rem;
  }

  :global(.terminal-host .xterm-viewport) {
    scrollbar-color: #475569 #07130e;
  }

  .terminal-controls {
    grid-area: controls;
    align-items: center;
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
    border-color: #334155;
    background: #1f2937;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  @media (max-width: 760px) {
    .terminal-shell {
      height: 100dvh;
      min-height: 0;
    }

    .terminal-panel {
      height: 100%;
      width: 100%;
    }

    .terminal-header {
      align-items: flex-start;
      flex-direction: column;
      gap: 0.65rem;
      padding: 0.7rem 0.75rem;
    }

    .terminal-header h1,
    .terminal-header .shell-meta {
      max-width: calc(100vw - 1.5rem);
    }

    .terminal-actions {
      width: 100%;
      justify-content: space-between;
    }

    .target-picker,
    .target-picker select {
      width: 100%;
      max-width: none;
    }

    .terminal-actions button {
      min-height: 2.75rem;
      padding: 0 0.7rem;
    }

    .terminal-notice {
      padding: 0.55rem 0.75rem;
      font-size: 0.76rem;
    }

    .terminal-host {
      padding: 0.35rem;
    }

    .terminal-controls {
      display: flex;
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
