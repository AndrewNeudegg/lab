<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import {
    DEFAULT_HEALTHD_API_BASE,
    Navbar,
    apiFetch,
    type HealthdSample,
    type HealthdSnapshot,
    type HealthdSLOReport
  } from '@homelab/shared';

  const apiBase = '/healthd-api';
  const chartWidth = 420;
  const chartHeight = 130;
  const pad = 14;

  let snapshot: HealthdSnapshot | undefined;
  let loading = true;
  let checking = false;
  let error = '';
  let timer: ReturnType<typeof setInterval> | undefined;

  const fetchHealthd = async (requestPath: string, init: RequestInit = {}) => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 4000);
    try {
      return await apiFetch<HealthdSnapshot>(requestPath, {
        ...init,
        baseUrl: apiBase,
        signal: controller.signal
      });
    } finally {
      window.clearTimeout(timeout);
    }
  };

  const healthdError = (err: unknown) => {
    const message = err instanceof Error ? err.message : 'Unable to load healthd.';
    if (err instanceof DOMException && err.name === 'AbortError') {
      return `Timed out waiting for ${apiBase}/healthd. Confirm healthd is running on ${DEFAULT_HEALTHD_API_BASE} and restart the dashboard dev server.`;
    }
    if (
      message.includes('500 Internal Server Error') ||
      message.includes('Failed to fetch') ||
      message.includes('NetworkError')
    ) {
      return `healthd API is not reachable through ${apiBase}. Start it with ./run.sh serve-healthd or ./.bin/healthd, then restart the dashboard dev server if the proxy config changed.`;
    }
    return message;
  };

  const refresh = async () => {
    try {
      error = '';
      snapshot = await fetchHealthd('/healthd?window=5m');
    } catch (err) {
      error = healthdError(err);
    } finally {
      loading = false;
    }
  };

  const runChecks = async () => {
    checking = true;
    try {
      error = '';
      snapshot = await fetchHealthd('/healthd/checks/run', {
        method: 'POST'
      });
    } catch (err) {
      error = healthdError(err);
    } finally {
      checking = false;
    }
  };

  onMount(() => {
    void refresh();
    timer = setInterval(() => void refresh(), 5000);
  });

  onDestroy(() => {
    if (timer) {
      clearInterval(timer);
    }
  });

  const number = (value = 0, digits = 1) =>
    new Intl.NumberFormat(undefined, {
      maximumFractionDigits: digits,
      minimumFractionDigits: digits
    }).format(value);

  const compactBytes = (value = 0) =>
    new Intl.NumberFormat(undefined, {
      notation: 'compact',
      maximumFractionDigits: 1
    }).format(value);

  const duration = (seconds = 0) => {
    const total = Math.max(0, Math.floor(seconds));
    const days = Math.floor(total / 86400);
    const hours = Math.floor((total % 86400) / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (days > 0) {
      return `${days}d ${hours}h`;
    }
    if (hours > 0) {
      return `${hours}h ${minutes}m`;
    }
    return `${minutes}m`;
  };

  const timeLabel = (value: string) =>
    new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  const pathFor = (samples: HealthdSample[], field: keyof HealthdSample, max = 100) => {
    if (samples.length === 0) {
      return '';
    }
    if (samples.length === 1) {
      const y = chartY(Number(samples[0][field]), max);
      return `${pad},${y} ${chartWidth - pad},${y}`;
    }
    return samples
      .map((sample, index) => {
        const x = pad + (index / (samples.length - 1)) * (chartWidth - pad * 2);
        const y = chartY(Number(sample[field]), max);
        return `${x.toFixed(1)},${y.toFixed(1)}`;
      })
      .join(' ');
  };

  const chartY = (value: number, max: number) => {
    const bounded = Math.min(max, Math.max(0, value));
    return pad + (1 - bounded / max) * (chartHeight - pad * 2);
  };

  const latest = (samples: HealthdSample[]) => samples[samples.length - 1];

  const sloStroke = (slo: HealthdSLOReport) => {
    if (slo.status === 'critical') {
      return '#dc2626';
    }
    if (slo.status === 'warning') {
      return '#d97706';
    }
    return '#059669';
  };
</script>

<svelte:head>
  <title>healthd</title>
  <meta name="description" content="healthd service health dashboard" />
</svelte:head>

<div class="app-shell">
  <Navbar title="Health" subtitle="Reliability" current="/healthd" />

  <main>
    {#if error}
      <p class="error" role="alert">{error}</p>
    {/if}

    {#if loading}
      <section class="empty">Loading healthd...</section>
    {:else if snapshot}
      <section class="toolbar">
        <div>
          <p>Overall</p>
          <h2 class={`status ${snapshot.status}`}>{snapshot.status}</h2>
        </div>
        <div class="toolbar-actions">
          <span>Process uptime {duration(snapshot.uptime_seconds)}</span>
          <button type="button" on:click={runChecks} disabled={checking}>
            {checking ? 'Checking' : 'Run checks'}
          </button>
        </div>
      </section>

      <section class="metrics-grid">
        <article class="metric">
          <p>CPU</p>
          <strong>{number(snapshot.current.cpu_usage_percent)}%</strong>
          <span>load {number(snapshot.current.load1)} / {number(snapshot.current.load5)} / {number(snapshot.current.load15)}</span>
        </article>
        <article class="metric">
          <p>Memory</p>
          <strong>{number(snapshot.current.memory_usage_percent)}%</strong>
          <span>{compactBytes(snapshot.current.memory_used_bytes)} of {compactBytes(snapshot.current.memory_total_bytes)}</span>
        </article>
        <article class="metric">
          <p>System uptime</p>
          <strong>{duration(snapshot.current.system_uptime_seconds)}</strong>
          <span>{snapshot.current.goroutines} goroutines</span>
        </article>
        <article class="metric">
          <p>Samples</p>
          <strong>{snapshot.samples.length}</strong>
          <span>last {Math.round(snapshot.window_seconds / 60)} minutes</span>
        </article>
      </section>

      <section class="charts">
        <article class="chart-panel">
          <div class="panel-title">
            <h2>CPU usage</h2>
            <span>{number(snapshot.current.cpu_usage_percent)}%</span>
          </div>
          <svg viewBox={`0 0 ${chartWidth} ${chartHeight}`} role="img" aria-label="CPU usage over five minutes">
            <line x1={pad} y1={chartY(80, 100)} x2={chartWidth - pad} y2={chartY(80, 100)} />
            <polyline points={pathFor(snapshot.samples, 'cpu_usage_percent')} />
          </svg>
          {#if latest(snapshot.samples)}
            <p>{timeLabel(snapshot.samples[0].time)} to {timeLabel(latest(snapshot.samples).time)}</p>
          {/if}
        </article>

        <article class="chart-panel">
          <div class="panel-title">
            <h2>Memory usage</h2>
            <span>{number(snapshot.current.memory_usage_percent)}%</span>
          </div>
          <svg viewBox={`0 0 ${chartWidth} ${chartHeight}`} role="img" aria-label="Memory usage over five minutes">
            <line x1={pad} y1={chartY(85, 100)} x2={chartWidth - pad} y2={chartY(85, 100)} />
            <polyline points={pathFor(snapshot.samples, 'memory_usage_percent')} />
          </svg>
          {#if latest(snapshot.samples)}
            <p>{timeLabel(snapshot.samples[0].time)} to {timeLabel(latest(snapshot.samples).time)}</p>
          {/if}
        </article>
      </section>

      <section class="split">
        <div>
          <div class="section-title">
            <h2>SLOs</h2>
          </div>
          <div class="list">
            {#each snapshot.slos as slo}
              <article class="slo">
                <div class="row">
                  <div>
                    <h3>{slo.name}</h3>
                    <p>{number(slo.sli_percent, 3)}% SLI against {number(slo.target_percent, 3)}%</p>
                  </div>
                  <span class={`pill ${slo.status}`}>{slo.status}</span>
                </div>
                <div class="budget">
                  <span style={`width: ${Math.min(100, Math.max(0, slo.error_budget_remaining_percent))}%; background: ${sloStroke(slo)}`}></span>
                </div>
                <div class="slo-stats">
                  <span>{number(slo.error_budget_remaining_percent)}% budget</span>
                  <span>{number(slo.burn_rate, 2)}x burn</span>
                  <span>{slo.good_events}/{slo.total_events} good</span>
                </div>
              </article>
            {/each}
          </div>
        </div>

        <div>
          <div class="section-title">
            <h2>Checks</h2>
          </div>
          <div class="list">
            {#each snapshot.checks as check}
              <article class="check">
                <div>
                  <h3>{check.name}</h3>
                  <p>{check.type} · {check.message}</p>
                </div>
                <div class="check-side">
                  <span class={`pill ${check.status}`}>{check.status}</span>
                  <small>{check.latency_ms} ms</small>
                </div>
              </article>
            {/each}
          </div>
        </div>
      </section>

      <section>
        <div class="section-title">
          <h2>Notifications</h2>
        </div>
        <div class="notifications">
          {#if snapshot.notifications.length === 0}
            <p class="muted">No notifications.</p>
          {:else}
            {#each snapshot.notifications.slice(0, 8) as notification}
              <article class="notification">
                <span class={`pill ${notification.severity}`}>{notification.severity}</span>
                <div>
                  <h3>{notification.source}</h3>
                  <p>{notification.message}</p>
                </div>
                <time>{timeLabel(notification.time)}</time>
              </article>
            {/each}
          {/if}
        </div>
      </section>
    {:else}
      <section class="empty">No healthd snapshot loaded.</section>
    {/if}
  </main>
</div>

<style>
  :global(html) {
    min-height: 100%;
  }

  :global(body) {
    margin: 0;
    min-height: 100%;
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
    color: #111827;
    background: #f3f6fb;
  }

  .app-shell {
    min-height: 100dvh;
  }

  main {
    display: grid;
    gap: 1rem;
    width: min(1180px, calc(100% - 2rem));
    margin: 0 auto;
    padding: 1rem 0 2rem;
  }

  .toolbar,
  .metrics-grid,
  .charts,
  .split {
    display: grid;
    gap: 1rem;
  }

  .toolbar {
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    padding: 1rem;
    border: 1px solid #d9e1ea;
    border-radius: 0.5rem;
    background: #ffffff;
  }

  .toolbar p,
  .metric p,
  .chart-panel p,
  .slo p,
  .check p,
  .notification p,
  .muted {
    margin: 0;
    color: #64748b;
  }

  .toolbar h2,
  .panel-title h2,
  .section-title h2,
  .slo h3,
  .check h3,
  .notification h3 {
    margin: 0;
  }

  .toolbar-actions {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    color: #475569;
    font-size: 0.9rem;
  }

  button {
    min-height: 2.4rem;
    padding: 0 0.9rem;
    border: 1px solid #0f766e;
    border-radius: 0.45rem;
    color: #ffffff;
    background: #0f766e;
    font: inherit;
    font-weight: 800;
    cursor: pointer;
  }

  button:disabled {
    border-color: #94a3b8;
    background: #94a3b8;
    cursor: not-allowed;
  }

  .status {
    font-size: clamp(2rem, 8vw, 4rem);
    line-height: 1;
    text-transform: capitalize;
  }

  .healthy {
    color: #047857;
  }

  .warning,
  .warn {
    color: #b45309;
  }

  .critical,
  .page {
    color: #b91c1c;
  }

  .metrics-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }

  .metric,
  .chart-panel,
  .slo,
  .check,
  .notification,
  .empty,
  .notifications {
    border: 1px solid #d9e1ea;
    border-radius: 0.5rem;
    background: #ffffff;
  }

  .metric {
    display: grid;
    gap: 0.35rem;
    min-height: 6.25rem;
    padding: 1rem;
  }

  .metric strong {
    font-size: 1.9rem;
    line-height: 1;
  }

  .metric span,
  .slo-stats,
  .check-side,
  time {
    color: #64748b;
    font-size: 0.85rem;
  }

  .charts,
  .split {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .chart-panel {
    display: grid;
    gap: 0.7rem;
    padding: 1rem;
  }

  .panel-title,
  .section-title,
  .row,
  .check,
  .notification {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
  }

  .panel-title span {
    color: #0f766e;
    font-size: 1.45rem;
    font-weight: 900;
  }

  svg {
    display: block;
    width: 100%;
    height: auto;
    overflow: visible;
  }

  line {
    stroke: #f59e0b;
    stroke-dasharray: 5 5;
    stroke-width: 1.5;
  }

  polyline {
    fill: none;
    stroke: #2563eb;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-width: 3;
  }

  .list,
  .notifications {
    display: grid;
    gap: 0.75rem;
  }

  .slo,
  .check,
  .notification,
  .empty,
  .notifications {
    padding: 1rem;
  }

  .budget {
    height: 0.6rem;
    margin: 0.9rem 0;
    overflow: hidden;
    border-radius: 0.3rem;
    background: #e5e7eb;
  }

  .budget span {
    display: block;
    height: 100%;
  }

  .slo-stats {
    display: flex;
    flex-wrap: wrap;
    gap: 0.8rem;
  }

  .pill {
    flex: 0 0 auto;
    padding: 0.25rem 0.45rem;
    border-radius: 0.35rem;
    background: #edf2f7;
    color: #334155;
    font-size: 0.75rem;
    font-weight: 900;
    text-transform: uppercase;
  }

  .pill.healthy {
    background: #dcfce7;
    color: #047857;
  }

  .pill.warning,
  .pill.warn {
    background: #fef3c7;
    color: #b45309;
  }

  .pill.critical,
  .pill.page {
    background: #fee2e2;
    color: #b91c1c;
  }

  .check-side {
    display: grid;
    justify-items: end;
    gap: 0.35rem;
  }

  .notification {
    align-items: flex-start;
  }

  .notification div {
    min-width: 0;
    flex: 1 1 auto;
  }

  .error {
    margin: 0;
    padding: 0.75rem 1rem;
    border: 1px solid #fecaca;
    border-radius: 0.5rem;
    color: #991b1b;
    background: #fef2f2;
    overflow-wrap: anywhere;
  }

  @media (max-width: 860px) {
    .toolbar,
    .metrics-grid,
    .charts,
    .split {
      grid-template-columns: 1fr;
    }

    .toolbar-actions {
      align-items: flex-start;
      flex-direction: column;
    }
  }
</style>
