<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    Navbar,
    createSupervisorClient,
    type SupervisorAppStatus,
    type SupervisorSnapshot
  } from '@homelab/shared';

  const client = createSupervisorClient({ baseUrl: '/supervisord-api' });

  let snapshot: SupervisorSnapshot | undefined;
  let loading = true;
  let error = '';
  let notice = '';
  let busy = '';
  let timer: ReturnType<typeof setInterval> | undefined;

  const refresh = async () => {
    try {
      error = '';
      snapshot = await client.snapshot();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load supervisord.';
    } finally {
      loading = false;
    }
  };

  const act = async (name: string, action: 'start' | 'stop' | 'restart') => {
    busy = `${action}:${name}`;
    try {
      error = '';
      if (action === 'start') {
        snapshot = await client.start(name);
      } else if (action === 'stop') {
        snapshot = await client.stop(name);
      } else {
        snapshot = await client.restart(name);
      }
    } catch (err) {
      error = err instanceof Error ? err.message : `Unable to ${action} ${name}.`;
    } finally {
      busy = '';
    }
  };

  const restartSupervisor = async () => {
    busy = 'restart:supervisord';
    try {
      error = '';
      const response = await client.restartSelf();
      notice = response.reply;
      window.setTimeout(() => void refresh(), 2500);
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to restart supervisord.';
    } finally {
      busy = '';
    }
  };

  const isBusy = (name: string, action: string) => busy === `${action}:${name}`;

  const ageLabel = (value?: string) => {
    if (!value) {
      return 'never';
    }
    const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
    if (seconds < 60) {
      return `${seconds}s ago`;
    }
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) {
      return `${minutes}m ago`;
    }
    return `${Math.floor(minutes / 60)}h ago`;
  };

  const commandLine = (app: SupervisorAppStatus) => [app.command, ...(app.args ?? [])].join(' ');

  onMount(() => {
    void refresh();
    timer = setInterval(() => void refresh(), 3000);
  });

  onDestroy(() => {
    if (timer) {
      clearInterval(timer);
    }
  });
</script>

<svelte:head>
  <title>supervisord</title>
  <meta name="description" content="supervisord process control dashboard" />
</svelte:head>

<div class="app-shell">
  <Navbar title="Supervisor" subtitle="Runtime" current="/supervisord" />

  <main>
    <section class="hero">
      <div>
        <p>Process control</p>
        <h1 class={`status ${snapshot?.status ?? 'unknown'}`}>{snapshot?.status ?? 'unknown'}</h1>
        {#if snapshot?.restart_hint}
          <small>Restart command: {snapshot.restart_hint}</small>
        {/if}
      </div>
      <div class="hero-actions">
        <button
          type="button"
          class="danger"
          on:click={restartSupervisor}
          disabled={busy !== '' || !snapshot?.restartable}
        >
          {busy === 'restart:supervisord' ? 'Restarting' : 'Restart supervisord'}
        </button>
        <button type="button" on:click={refresh} disabled={loading || busy !== ''}>Refresh</button>
      </div>
    </section>

    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}
    {#if notice}
      <p class="notice">{notice}</p>
    {/if}

    {#if loading}
      <section class="empty">Loading supervisord...</section>
    {:else if !snapshot || snapshot.apps.length === 0}
      <section class="empty">No supervised apps configured.</section>
    {:else}
      <section class="apps" aria-label="Supervised applications">
        {#each snapshot.apps as app}
          <article class="app">
            <header>
              <div class="identity">
                <span class={`dot ${app.state}`} aria-hidden="true"></span>
                <div>
                  <h2>{app.name}</h2>
                  <p>{app.type} · desired {app.desired} · restart {app.restart}</p>
                </div>
              </div>
              <span class={`pill ${app.state}`}>{app.state}</span>
            </header>

            <div class="meta">
              <span>PID {app.pid || '—'}</span>
              <span>Restarts {app.restarts}</span>
              <span>Updated {ageLabel(app.updated_at)}</span>
              {#if app.health_url}
                <span>Health {app.health_url}</span>
              {/if}
            </div>

            <p class="message">{app.message}</p>
            <code>{commandLine(app)}</code>

            {#if app.last_output}
              <details>
                <summary>Recent output</summary>
                <pre>{app.last_output}</pre>
              </details>
            {/if}

            <div class="actions">
              <button
                type="button"
                class="primary"
                disabled={busy !== '' || app.state === 'running'}
                on:click={() => act(app.name, 'start')}
              >
                {isBusy(app.name, 'start') ? 'Starting' : 'Start'}
              </button>
              <button
                type="button"
                disabled={busy !== '' || app.state !== 'running'}
                on:click={() => act(app.name, 'restart')}
              >
                {isBusy(app.name, 'restart') ? 'Restarting' : 'Restart'}
              </button>
              <button
                type="button"
                class="danger"
                disabled={busy !== '' || app.state === 'stopped'}
                on:click={() => act(app.name, 'stop')}
              >
                {isBusy(app.name, 'stop') ? 'Stopping' : 'Stop'}
              </button>
            </div>
          </article>
        {/each}
      </section>
    {/if}
  </main>
</div>

<style>
  :global(body) {
    margin: 0;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
    background: var(--bg, #f3f6fb);
    color: var(--text, #172033);
  }

  .app-shell {
    min-height: 100dvh;
    background: var(--bg, #f3f6fb);
  }

  main {
    display: grid;
    gap: 1rem;
    width: min(1120px, calc(100% - 2rem));
    margin: 0 auto;
    padding: 1rem 0 2rem;
  }

  .hero,
  .app,
  .empty,
  .notice,
  .error {
    border: 1px solid var(--border, #d9e1ea);
    border-radius: 0.75rem;
    background: var(--surface, #ffffff);
    box-shadow: 0 10px 30px var(--shadow, rgb(15 23 42 / 0.08));
  }

  .hero {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: 1rem;
  }

  .hero > div:first-child {
    display: grid;
    gap: 0.25rem;
  }

  .hero-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .hero p,
  .hero small,
  .identity p,
  .meta,
  .message {
    margin: 0;
    color: var(--muted, #64748b);
  }

  h1,
  h2 {
    margin: 0;
    color: var(--text-strong, #0f172a);
  }

  h1 {
    font-size: clamp(2.4rem, 9vw, 4.5rem);
    line-height: 1;
    text-transform: capitalize;
  }

  .apps {
    display: grid;
    gap: 0.9rem;
  }

  .app {
    display: grid;
    gap: 0.9rem;
    padding: 1rem;
  }

  header,
  .identity,
  .meta,
  .actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  header {
    justify-content: space-between;
  }

  .identity {
    min-width: 0;
  }

  .identity h2,
  code {
    overflow-wrap: anywhere;
  }

  .dot {
    width: 0.85rem;
    height: 0.85rem;
    flex: 0 0 auto;
    border-radius: 999px;
    background: #94a3b8;
  }

  .dot.running {
    background: #059669;
  }

  .status.healthy {
    color: #047857;
  }

  .dot.starting,
  .dot.stopping {
    background: #d97706;
  }

  .status.warning {
    color: #b45309;
  }

  .dot.failed {
    background: #dc2626;
  }

  .status.critical {
    color: #b91c1c;
  }

  .status.unknown {
    color: var(--text-strong, #0f172a);
  }

  .meta {
    flex-wrap: wrap;
    font-size: 0.9rem;
  }

  code,
  pre {
    display: block;
    padding: 0.75rem;
    overflow-x: auto;
    border-radius: 0.5rem;
    background: var(--surface-muted, #f8fafc);
    color: var(--text, #172033);
  }

  pre {
    max-height: 16rem;
    white-space: pre-wrap;
  }

  .pill {
    flex: 0 0 auto;
    padding: 0.25rem 0.5rem;
    border-radius: 0.4rem;
    background: var(--surface-muted, #f8fafc);
    color: var(--text, #172033);
    font-size: 0.75rem;
    font-weight: 900;
    text-transform: uppercase;
  }

  button {
    min-height: 2.4rem;
    padding: 0 0.9rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
    color: var(--text-strong, #0f172a);
    font: inherit;
    font-weight: 800;
    cursor: pointer;
  }

  button.primary {
    border-color: #047857;
    background: #047857;
    color: #ffffff;
  }

  button.danger {
    border-color: #b91c1c;
    background: #b91c1c;
    color: #ffffff;
  }

  button:disabled {
    opacity: 0.55;
    cursor: not-allowed;
  }

  .empty,
  .notice,
  .error {
    padding: 1rem;
  }

  .error {
    color: var(--danger-text, #991b1b);
    background: var(--danger-bg, #fef2f2);
  }

  .notice {
    color: #166534;
    background: var(--success-bg, #f0fdf4);
  }

  @media (max-width: 720px) {
    header,
    .hero,
    .hero-actions,
    .actions {
      align-items: stretch;
      flex-direction: column;
    }

    .hero-actions button,
    .actions button {
      width: 100%;
    }
  }
</style>
