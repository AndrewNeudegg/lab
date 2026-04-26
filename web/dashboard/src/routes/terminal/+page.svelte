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
    value?: string;
    signal?: string;
  };

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const controls: ControlButton[] = [
    { label: 'Ctrl-C', signal: 'interrupt' },
    { label: 'Ctrl-D', value: '\u0004' },
    { label: 'Ctrl-Z', value: '\u001a' },
    { label: 'Tab', value: '\t' },
    { label: 'Esc', value: '\u001b' },
    { label: 'Up', value: '\u001b[A' },
    { label: 'Down', value: '\u001b[B' },
    { label: 'Left', value: '\u001b[D' },
    { label: 'Right', value: '\u001b[C' }
  ];

  let session: TerminalSession | undefined;
  let source: EventSource | undefined;
  let output = '';
  let command = '';
  let loading = true;
  let busy = false;
  let error = '';
  let terminalEl: HTMLElement | undefined;
  let inputEl: HTMLInputElement | undefined;

  const endpoint = (path: string) => `${apiBase}${path}`;

  const scrollOutput = () => {
    void tick().then(() => {
      if (terminalEl) {
        terminalEl.scrollTop = terminalEl.scrollHeight;
      }
    });
  };

  const appendOutput = (value: string) => {
    output += value;
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
    source = new EventSource(endpoint(`/terminal/sessions/${encodeURIComponent(nextSession.id)}/events`));
    source.addEventListener('output', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as TerminalEvent;
      appendOutput(payload.data || '');
    });
    source.addEventListener('exit', (event) => {
      const payload = JSON.parse((event as MessageEvent).data) as TerminalEvent;
      appendOutput(`\n[process exited ${payload.code ?? 0}]\n`);
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
        <h1>Shell</h1>
        <p>{session ? `${session.shell} · ${session.cwd}` : 'Starting shell'}</p>
      </div>
      <div class="terminal-actions">
        <button type="button" on:click={() => (output = '')}>Clear</button>
        <button type="button" on:click={startSession} disabled={loading}>New</button>
      </div>
    </header>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    <pre class="terminal-output" bind:this={terminalEl} aria-live="polite">{loading ? 'Starting terminal...\n' : output}</pre>

    <section class="terminal-controls" aria-label="Terminal control keys">
      {#each controls as control}
        <button type="button" disabled={!session || busy} on:click={() => sendControl(control)}>
          {control.label}
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
        placeholder="Command"
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
    min-height: 100dvh;
  }

  .terminal-panel {
    display: grid;
    grid-template-rows: auto auto minmax(0, 1fr) auto auto;
    min-height: 0;
    width: min(100%, 72rem);
    margin: 0 auto;
    background: #f8fafc;
  }

  .terminal-header {
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

  .terminal-header h1 {
    color: #0f172a;
    font-size: 1rem;
  }

  .terminal-header p {
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

  .terminal-actions button,
  .terminal-controls button,
  .terminal-composer button {
    min-height: 2.4rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.5rem;
    color: #243047;
    background: #ffffff;
    font-weight: 850;
  }

  .terminal-actions button {
    padding: 0 0.75rem;
  }

  .terminal-output {
    min-width: 0;
    min-height: 0;
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
  }

  .terminal-controls {
    padding: 0.75rem 1rem;
    border-top: 1px solid #1f2937;
    background: #111827;
  }

  .terminal-controls button {
    min-width: 4.8rem;
    color: #e2e8f0;
    border-color: #334155;
    background: #1f2937;
  }

  .terminal-composer {
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

  .error {
    margin: 0;
    padding: 0.75rem 1rem;
    color: #991b1b;
    background: #fef2f2;
    overflow-wrap: anywhere;
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
      padding: 0.7rem 0.75rem;
    }

    .terminal-header p {
      max-width: 58vw;
    }

    .terminal-output {
      padding: 0.75rem;
      font-size: 0.82rem;
    }

    .terminal-controls {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      padding: 0.6rem;
    }

    .terminal-controls button {
      min-width: 0;
      min-height: 2.55rem;
      padding: 0 0.25rem;
      font-size: 0.78rem;
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
