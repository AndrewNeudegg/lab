<script lang="ts">
  import { onDestroy, onMount, tick } from 'svelte';
  import { Navbar } from '@homelab/shared';

  type TerminalSession = {
    id: string;
    shell: string;
    cwd: string;
  };

  type TerminalEvent = {
    type: string;
    data?: string;
    code?: number;
  };

  type ControlButton = {
    label: string;
    hint: string;
    ariaLabel?: string;
    value?: string;
    signal?: string;
  };

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const controls: ControlButton[] = [
    { label: 'Ctrl-C', hint: 'Interrupt', signal: 'interrupt' },
    { label: 'Ctrl-D', hint: 'EOF', value: '\u0004' },
    { label: 'Ctrl-Z', hint: 'Suspend', value: '\u001a' },
    { label: 'Tab', hint: 'Complete', value: '\t' },
    { label: 'Esc', hint: 'Escape', value: '\u001b' },
    { label: '↑', hint: 'History up', ariaLabel: 'Up', value: '\u001b[A' },
    { label: '↓', hint: 'History down', ariaLabel: 'Down', value: '\u001b[B' },
    { label: '←', hint: 'Cursor left', ariaLabel: 'Left', value: '\u001b[D' },
    { label: '→', hint: 'Cursor right', ariaLabel: 'Right', value: '\u001b[C' }
  ];

  let session: TerminalSession | undefined;
  let source: EventSource | undefined;
  let output = '';
  let command = '';
  let loading = true;
  let busy = false;
  let error = '';
  let connected = false;
  let terminalEl: HTMLElement | undefined;
  let inputEl: HTMLInputElement | undefined;
  let renderedOutput = 'Starting terminal...\n';

  const endpoint = (path: string) => `${apiBase}${path}`;

  const cleanOutput = (value: string) =>
    value
      .replace(/\[?\\\[\x1b\]0;.*?\x07\\\]/g, '')
      .replace(/\\\[\x1b\[[0-9;?]*[ -/]*[@-~]\\\]/g, '')
      .replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '')
      .replace(/\\\[\\\]0;.*?\\\]/g, '')
      .replace(/\\\[\[[0-9;?]*[ -/]*[@-~]\\\]/g, '')
      .replace(/\\\[\\\]/g, '')
      .replace(/(\S+@\S+:[^\n\]]+)\]\$/g, '$1$')
      .replace(/\r\n/g, '\n')
      .replace(/\r/g, '\n');

  $: renderedOutput = loading ? 'Starting terminal...\n' : cleanOutput(output) || 'Waiting for shell output...\n';

  const scrollOutput = () => {
    void tick().then(() => {
      if (terminalEl) {
        terminalEl.scrollTop = terminalEl.scrollHeight;
      }
    });
  };

  const appendOutput = (value: string) => {
    output += value;
    const maxOutputBytes = 512 * 1024;
    if (output.length > maxOutputBytes) {
      output = output.slice(output.length - maxOutputBytes);
    }
    scrollOutput();
  };

  async function request<T>(path: string, init: RequestInit = {}) {
    const headers = new Headers(init.headers);
    if (init.body && !headers.has('content-type')) {
      headers.set('content-type', 'application/json');
    }
    const response = await fetch(endpoint(path), { ...init, headers });
    if (!response.ok) {
      const details = await response.text();
      throw new Error(details || `Request failed with ${response.status}`);
    }
    return (await response.json()) as T;
  }

  const connectEvents = (nextSession: TerminalSession) => {
    source?.close();
    connected = false;
    source = new EventSource(endpoint(`/terminal/sessions/${encodeURIComponent(nextSession.id)}/events`));
    source.addEventListener('ready', () => {
      connected = true;
      error = '';
    });
    source.addEventListener('output', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as TerminalEvent;
      appendOutput(payload.data || '');
    });
    source.addEventListener('exit', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as TerminalEvent;
      appendOutput(`\n[process exited ${payload.code ?? 0}]\n`);
      connected = false;
      source?.close();
      source = undefined;
    });
    source.onerror = () => {
      error = 'Terminal stream disconnected.';
    };
  };

  const startSession = async () => {
    loading = true;
    error = '';
    try {
      await closeSession();
      session = await request<TerminalSession>('/terminal/sessions', {
        method: 'POST',
        body: JSON.stringify({})
      });
      output = '';
      connectEvents(session);
      requestAnimationFrame(() => inputEl?.focus());
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to start terminal.';
    } finally {
      loading = false;
    }
  };

  const sendInput = async (data: string) => {
    if (!session || !data || busy) {
      return;
    }
    busy = true;
    error = '';
    try {
      await request(`/terminal/sessions/${encodeURIComponent(session.id)}/input`, {
        method: 'POST',
        body: JSON.stringify({ data })
      });
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to send input.';
    } finally {
      busy = false;
      inputEl?.focus();
    }
  };

  const sendSignal = async (signal: string) => {
    if (!session || busy) {
      return;
    }
    busy = true;
    error = '';
    try {
      await request(`/terminal/sessions/${encodeURIComponent(session.id)}/signal`, {
        method: 'POST',
        body: JSON.stringify({ signal })
      });
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to send signal.';
    } finally {
      busy = false;
      inputEl?.focus();
    }
  };

  const sendControl = (control: ControlButton) => {
    if (control.signal) {
      void sendSignal(control.signal);
      return;
    }
    void sendInput(control.value || '');
  };

  const submitCommand = () => {
    const next = command;
    if (!next.trim()) {
      return;
    }
    command = '';
    void sendInput(`${next}\n`);
  };

  const closeSession = async () => {
    source?.close();
    source = undefined;
    connected = false;
    if (!session) {
      return;
    }
    const closing = session.id;
    session = undefined;
    try {
      await request(`/terminal/sessions/${encodeURIComponent(closing)}`, { method: 'DELETE' });
    } catch {
      // The shell may already have exited.
    }
  };

  onMount(() => {
    void startSession();
  });

  onDestroy(() => {
    void closeSession();
  });
</script>

<svelte:head>
  <title>homelabd Terminal</title>
  <meta name="description" content="Web terminal for homelabd operators" />
</svelte:head>

<div class="terminal-shell">
  <Navbar title="Terminal" subtitle="homelabd" current="/terminal" />

  <main class="terminal-panel">
    <header class="terminal-header">
      <div>
        <p class="eyebrow">Operator shell</p>
        <h1>{session ? session.cwd : 'Starting shell'}</h1>
        <p class="shell-meta">{session?.shell || 'Preparing a new shell session'}</p>
      </div>
      <div class="terminal-actions">
        <span class:connected class="status-pill">{connected ? 'Connected' : loading ? 'Starting' : 'Disconnected'}</span>
        <button type="button" on:click={() => (output = '')}>Clear</button>
        <button type="button" on:click={startSession} disabled={loading}>New session</button>
      </div>
    </header>

    <p class:error class="terminal-notice" role={error ? 'alert' : 'status'}>
      {error || 'Commands run as the homelabd user. Use this for trusted operator work only.'}
    </p>

    <pre
      class="terminal-output"
      bind:this={terminalEl}
      role="log"
      aria-label="Terminal output"
      aria-live="polite"
      aria-atomic="false">{renderedOutput}</pre>

    <section class="terminal-controls" aria-label="Terminal control keys">
      {#each controls as control}
        <button
          type="button"
          disabled={!session || busy}
          title={control.hint}
          aria-label={`${control.ariaLabel || control.label} ${control.hint}`}
          on:click={() => sendControl(control)}
        >
          <strong>{control.label}</strong>
          <span>{control.hint}</span>
        </button>
      {/each}
    </section>

    <form class="terminal-composer" on:submit|preventDefault={submitCommand}>
      <label class="hidden" for="terminal-command">Command</label>
      <input
        id="terminal-command"
        bind:this={inputEl}
        bind:value={command}
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        inputmode="text"
        placeholder="Type a command, then press Enter"
        disabled={!session || loading}
      />
      <button type="submit" disabled={!session || loading || !command.trim()}>Send</button>
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
  input {
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
      "output"
      "controls"
      "composer";
    grid-template-rows: auto auto minmax(0, 1fr) auto auto;
    min-height: 0;
    width: min(100%, 72rem);
    margin: 0 auto;
    background: #f8fafc;
    box-shadow: 0 18px 50px rgb(15 23 42 / 0.08);
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
  .terminal-controls button,
  .terminal-composer button {
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

  .terminal-output {
    grid-area: output;
    min-width: 0;
    min-height: 0;
    max-height: 100%;
    margin: 0;
    padding: 1rem;
    overflow: auto;
    color: #d8f3dc;
    background: #07130e;
    font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
    font-size: 0.9rem;
    line-height: 1.5;
    overflow-wrap: anywhere;
    white-space: pre-wrap;
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
    gap: 0.4rem;
    min-width: 5.6rem;
    min-height: 2.5rem;
    padding: 0 0.65rem;
    color: #e2e8f0;
    border-color: #334155;
    background: #1f2937;
  }

  .terminal-controls button span {
    color: #94a3b8;
    font-size: 0.72rem;
  }

  .terminal-composer {
    grid-area: composer;
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0.75rem;
    padding: 0.85rem 1rem 1rem;
    border-top: 1px solid #dde4ef;
    background: #ffffff;
  }

  .terminal-composer input {
    box-sizing: border-box;
    width: 100%;
    min-height: 2.8rem;
    padding: 0 0.85rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.55rem;
    color: #111827;
    background: #ffffff;
    font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  }

  .terminal-composer input:focus {
    border-color: #2563eb;
    outline: 3px solid rgb(37 99 235 / 0.14);
  }

  .terminal-composer button {
    min-width: 5.5rem;
    color: #ffffff;
    border-color: #2563eb;
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

  @media (max-width: 760px) {
    .terminal-shell {
      height: 100dvh;
      min-height: 0;
    }

    .terminal-panel {
      height: 100%;
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

    .terminal-actions button {
      min-height: 2.75rem;
      padding: 0 0.7rem;
    }

    .terminal-notice {
      padding: 0.55rem 0.75rem;
      font-size: 0.76rem;
    }

    .terminal-output {
      padding: 0.75rem;
      font-size: 0.82rem;
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
      min-width: 4.7rem;
      min-height: 2.75rem;
      padding: 0 0.45rem;
      font-size: 0.78rem;
      flex-direction: column;
      gap: 0.1rem;
    }

    .terminal-controls button span {
      font-size: 0.66rem;
    }

    .terminal-composer {
      grid-template-columns: 1fr;
      gap: 0.55rem;
      padding: 0.65rem 0.75rem 0.8rem;
    }

    .terminal-composer button {
      width: 100%;
      min-height: 2.65rem;
    }
  }
</style>
