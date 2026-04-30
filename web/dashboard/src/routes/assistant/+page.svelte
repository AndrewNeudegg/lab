<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createHomelabdClient,
    Navbar,
    type AssistantCapability,
    type AssistantCatalogue,
    type AssistantUXPattern
  } from '@homelab/shared';
  import {
    activityCountForCapability,
    assistantAreaLabel,
    assistantAutonomyLabel,
    assistantAutonomyTone,
    patternsForCapability,
    selectAssistantCapability
  } from './assistant-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const searchDelayMs = 250;

  let catalogue: AssistantCatalogue | undefined;
  let selectedCapabilityId = '';
  let selectedCapability: AssistantCapability | undefined;
  let selectedPatterns: AssistantUXPattern[] = [];
  let search = '';
  let area = 'all';
  let loading = true;
  let error = '';
  let lastSynced = '';
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let mounted = false;

  $: selectedCapability = selectAssistantCapability(catalogue?.capabilities || [], selectedCapabilityId);
  $: selectedPatterns = patternsForCapability(selectedCapability, catalogue?.ux_patterns || []);

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const refreshAssistant = async () => {
    loading = true;
    error = '';
    try {
      const response = await client.getAssistant({ search, area });
      catalogue = response;
      if (!response.capabilities.some((capability) => capability.id === selectedCapabilityId)) {
        selectedCapabilityId = response.capabilities[0]?.id || '';
      }
      lastSynced = syncTimeLabel();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load Assistant capabilities.';
    } finally {
      loading = false;
    }
  };

  const scheduleRefresh = () => {
    if (!mounted) {
      return;
    }
    if (searchTimer) {
      window.clearTimeout(searchTimer);
    }
    searchTimer = window.setTimeout(() => {
      void refreshAssistant();
    }, searchDelayMs);
  };

  const changeArea = (event: Event) => {
    area = event.currentTarget instanceof HTMLSelectElement ? event.currentTarget.value : 'all';
    scheduleRefresh();
  };

  const changeSearch = (event: Event) => {
    search = event.currentTarget instanceof HTMLInputElement ? event.currentTarget.value : '';
    scheduleRefresh();
  };

  const selectCapability = (capabilityId: string) => {
    selectedCapabilityId = capabilityId;
  };

  const stepDetail = (step: { prompt?: string; tool?: string; workflow_id?: string; condition?: string }) =>
    step.tool || step.workflow_id || step.condition || step.prompt || '';

  onMount(() => {
    mounted = true;
    void refreshAssistant();
    return () => {
      if (searchTimer) {
        window.clearTimeout(searchTimer);
      }
    };
  });
</script>

<svelte:head>
  <title>homelabd Assistant</title>
  <meta name="description" content="Assistant capabilities and agentic UX patterns" />
</svelte:head>

<div class="assistant-shell">
  <Navbar title="Assistant" subtitle="homelabd" current="/assistant" taskApiBase={apiBase} />

  <main class="assistant-page">
    <section class="assistant-sidebar" aria-label="Assistant controls">
      <header class="assistant-header">
        <div>
          <h1>Assistant</h1>
          <span>{lastSynced ? `Synced ${lastSynced}` : apiBase}</span>
        </div>
        <button type="button" disabled={loading} on:click={() => void refreshAssistant()}>
          {loading ? 'Syncing' : 'Sync'}
        </button>
      </header>

      {#if catalogue}
        <p class="assistant-summary">{catalogue.summary}</p>

        <div class="metrics" aria-label="Assistant totals">
          <div>
            <strong>{catalogue.capabilities.length}</strong>
            <span>Capabilities</span>
          </div>
          <div>
            <strong>{catalogue.activities.length}</strong>
            <span>Activities</span>
          </div>
          <div>
            <strong>{catalogue.ux_patterns.length}</strong>
            <span>UX patterns</span>
          </div>
        </div>

        <label class="field" for="assistant-area">
          <span>Area</span>
          <select id="assistant-area" bind:value={area} on:change={changeArea}>
            {#each catalogue.filters.areas as option}
              <option value={option.value}>{option.label} ({option.count})</option>
            {/each}
          </select>
        </label>

        <label class="field" for="assistant-search">
          <span>Search</span>
          <input
            id="assistant-search"
            type="search"
            value={search}
            placeholder="Search capabilities"
            on:input={changeSearch}
          />
        </label>

        <section class="activities" aria-label="Assistant activities">
          <h2>Activities</h2>
          {#if catalogue.activities.length}
            <div class="activity-list">
              {#each catalogue.activities as activity}
                <article class="activity">
                  <div>
                    <strong>{activity.name}</strong>
                    <span>{assistantAreaLabel(activity.area)} · {activity.cadence}</span>
                  </div>
                  <p>{activity.outcome}</p>
                </article>
              {/each}
            </div>
          {:else}
            <p class="empty">No activities match this filter.</p>
          {/if}
        </section>
      {:else if error}
        <p class="notice error" role="alert">{error}</p>
      {:else}
        <p class="empty">Loading Assistant capabilities...</p>
      {/if}
    </section>

    <section class="capability-list" aria-label="Assistant capabilities">
      {#if error}
        <p class="notice error" role="alert">{error}</p>
      {/if}

      <header class="section-title">
        <div>
          <p>Operating model</p>
          <h2>Capabilities</h2>
        </div>
        <span>{catalogue?.updated_at ? new Date(catalogue.updated_at).toLocaleDateString() : ''}</span>
      </header>

      {#if catalogue?.capabilities.length}
        <div class="capability-rows">
          {#each catalogue.capabilities as capability}
            <button
              type="button"
              class="capability-row"
              class:selected={selectedCapability?.id === capability.id}
              on:click={() => selectCapability(capability.id)}
            >
              <span class={`tone ${assistantAutonomyTone(capability.autonomy)}`}></span>
              <span class="capability-copy">
                <strong>{capability.name}</strong>
                <small>{assistantAreaLabel(capability.area)} · {assistantAutonomyLabel(capability.autonomy)}</small>
                <span>{capability.summary}</span>
              </span>
              <em>{capability.cadence}</em>
            </button>
          {/each}
        </div>
      {:else}
        <p class="empty">No capabilities match this view.</p>
      {/if}
    </section>

    <section class="capability-detail" aria-label="Assistant capability detail">
      {#if selectedCapability}
        <header class="detail-header">
          <div>
            <p>{assistantAreaLabel(selectedCapability.area)}</p>
            <h2>{selectedCapability.name}</h2>
            <span>{selectedCapability.promise}</span>
          </div>
          <span class={`status ${assistantAutonomyTone(selectedCapability.autonomy)}`}>
            {assistantAutonomyLabel(selectedCapability.autonomy)}
          </span>
        </header>

        <div class="detail-metrics" aria-label="Selected capability metrics">
          <div>
            <span>Inputs</span>
            <strong>{selectedCapability.inputs.length}</strong>
          </div>
          <div>
            <span>Outputs</span>
            <strong>{selectedCapability.outputs.length}</strong>
          </div>
          <div>
            <span>Activities</span>
            <strong>{activityCountForCapability(selectedCapability, catalogue?.activities || [])}</strong>
          </div>
        </div>

        <section class="detail-section" aria-label="Assistant inputs and outputs">
          <h3>Inputs and outputs</h3>
          <div class="io-grid">
            <div>
              <h4>Uses</h4>
              <ul>
                {#each selectedCapability.inputs as input}
                  <li>{input}</li>
                {/each}
              </ul>
            </div>
            <div>
              <h4>Creates</h4>
              <ul>
                {#each selectedCapability.outputs as output}
                  <li>{output}</li>
                {/each}
              </ul>
            </div>
          </div>
        </section>

        <section class="detail-section" aria-label="Assistant safeguards">
          <h3>Safeguards</h3>
          <ul class="checks">
            {#each selectedCapability.safeguards as safeguard}
              <li>{safeguard}</li>
            {/each}
          </ul>
        </section>

        <section class="detail-section" aria-label="Assistant UX patterns">
          <h3>UX patterns</h3>
          <div class="pattern-list">
            {#each selectedPatterns as pattern}
              <article class="pattern">
                <strong>{pattern.name}</strong>
                <p>{pattern.summary}</p>
                <span>{pattern.implementation}</span>
              </article>
            {/each}
          </div>
        </section>

        <section class="detail-section" aria-label="Assistant workflow template">
          <h3>Workflow template</h3>
          <p>{selectedCapability.workflow_template.goal}</p>
          <ol class="steps">
            {#each selectedCapability.workflow_template.steps as step}
              <li>
                <strong>{step.name}</strong>
                <span>{step.kind} · {stepDetail(step)}</span>
              </li>
            {/each}
          </ol>
        </section>

        <nav class="surface-links" aria-label="Related assistant surfaces">
          {#each selectedCapability.surfaces as surface}
            <a href={surface.href}>{surface.label}</a>
          {/each}
        </nav>
      {:else}
        <div class="empty-detail">
          <h2>No capability selected</h2>
          <p>Adjust the filters or clear search to inspect Assistant behaviour.</p>
        </div>
      {/if}
    </section>
  </main>
</div>

<style>
  :global(html),
  :global(body),
  :global(body > div) {
    min-height: 100%;
  }

  :global(body) {
    margin: 0;
    color: var(--text, #172033);
    background: var(--bg, #eef2f7);
    font-family:
      Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
      sans-serif;
  }

  button,
  input,
  select {
    font: inherit;
  }

  .assistant-shell {
    min-height: 100dvh;
    background: var(--bg, #eef2f7);
  }

  .assistant-page {
    display: grid;
    grid-template-columns: minmax(18rem, 24rem) minmax(20rem, 31rem) minmax(0, 1fr);
    min-height: calc(100dvh - 4.15rem);
  }

  .assistant-sidebar,
  .capability-list,
  .capability-detail {
    min-width: 0;
    padding: 1rem;
  }

  .assistant-sidebar,
  .capability-list {
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #f8fafc);
  }

  .capability-detail {
    background: var(--bg, #eef2f7);
  }

  .assistant-header,
  .section-title,
  .detail-header,
  .activity,
  .capability-row {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }

  .assistant-header,
  .section-title,
  .detail-header {
    justify-content: space-between;
  }

  h1,
  h2,
  h3,
  h4,
  p,
  ul,
  ol {
    margin: 0;
  }

  h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.35rem;
  }

  h2 {
    color: var(--text-strong, #0f172a);
    font-size: 1.2rem;
  }

  h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
  }

  h4 {
    color: var(--muted, #64748b);
    font-size: 0.8rem;
    text-transform: uppercase;
  }

  .assistant-header span,
  .assistant-summary,
  .section-title p,
  .section-title span,
  .activity span,
  .activity p,
  .capability-copy small,
  .capability-copy > span,
  .capability-row em,
  .detail-header p,
  .detail-header span,
  .detail-section p,
  .pattern p,
  .pattern span,
  .steps span,
  .empty,
  .empty-detail p {
    color: var(--muted, #64748b);
    font-size: 0.86rem;
  }

  .assistant-summary,
  .detail-header span,
  .detail-section p,
  .pattern p,
  .pattern span,
  .steps span {
    overflow-wrap: anywhere;
  }

  button {
    min-height: 2.35rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.5rem;
    color: var(--text-strong, #0f172a);
    background: var(--surface, #ffffff);
    font-weight: 750;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.65;
  }

  .assistant-header button {
    padding: 0 0.9rem;
    color: #ffffff;
    border-color: var(--accent, #2563eb);
    background: var(--accent, #2563eb);
  }

  .metrics,
  .detail-metrics {
    display: grid;
    gap: 0.65rem;
  }

  .metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .detail-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-bottom: 0.85rem;
  }

  .metrics div,
  .detail-metrics div,
  .activity,
  .detail-section,
  .empty-detail {
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .metrics div,
  .detail-metrics div {
    padding: 0.7rem;
  }

  .metrics strong,
  .detail-metrics strong {
    display: block;
    color: var(--text-strong, #0f172a);
    font-size: 1.08rem;
  }

  .metrics span,
  .detail-metrics span {
    color: var(--muted, #64748b);
    font-size: 0.75rem;
  }

  .field {
    display: grid;
    gap: 0.35rem;
    color: var(--text-strong, #0f172a);
    font-size: 0.82rem;
    font-weight: 800;
  }

  input,
  select {
    width: 100%;
    min-height: 2.5rem;
    box-sizing: border-box;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.5rem;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
    padding: 0 0.72rem;
  }

  .activities {
    display: grid;
    gap: 0.65rem;
  }

  .activity-list,
  .capability-rows,
  .pattern-list,
  .checks,
  .steps {
    display: grid;
    gap: 0.6rem;
  }

  .activity {
    align-items: flex-start;
    flex-direction: column;
    padding: 0.7rem;
  }

  .activity strong,
  .capability-row strong,
  .pattern strong,
  .steps strong {
    color: var(--text-strong, #0f172a);
  }

  .capability-row {
    width: 100%;
    align-items: flex-start;
    padding: 0.75rem;
    text-align: left;
    cursor: pointer;
  }

  .capability-row.selected {
    border-color: var(--accent, #2563eb);
    box-shadow: 0 0 0 1px var(--accent, #2563eb);
  }

  .capability-copy {
    display: grid;
    min-width: 0;
    gap: 0.2rem;
    flex: 1;
  }

  .capability-row em {
    max-width: 7rem;
    font-style: normal;
    text-align: right;
  }

  .tone {
    flex: 0 0 auto;
    width: 0.7rem;
    height: 0.7rem;
    margin-top: 0.25rem;
    border-radius: 999px;
    background: #94a3b8;
  }

  .green {
    background: #16a34a;
  }

  .blue {
    background: #2563eb;
  }

  .amber {
    background: #d97706;
  }

  .red {
    background: #dc2626;
  }

  .detail-header,
  .detail-section,
  .empty-detail {
    max-width: 58rem;
  }

  .detail-header {
    align-items: flex-start;
    margin-bottom: 0.85rem;
  }

  .detail-header h2 {
    margin: 0.15rem 0 0.35rem;
    font-size: 1.45rem;
  }

  .status {
    flex: 0 0 auto;
    padding: 0.38rem 0.58rem;
    border-radius: 999px;
    color: #ffffff;
    font-size: 0.78rem;
    font-weight: 850;
  }

  .detail-section {
    display: grid;
    gap: 0.7rem;
    margin-bottom: 0.85rem;
    padding: 0.85rem;
  }

  .io-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.75rem;
  }

  .io-grid div {
    display: grid;
    gap: 0.45rem;
  }

  ul,
  ol {
    padding-left: 1.15rem;
  }

  li {
    color: var(--text, #172033);
    overflow-wrap: anywhere;
  }

  .pattern,
  .steps li {
    display: grid;
    gap: 0.25rem;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface-muted, #f8fafc);
  }

  .surface-links {
    display: flex;
    flex-wrap: wrap;
    gap: 0.55rem;
    max-width: 58rem;
  }

  .surface-links a {
    min-height: 2.35rem;
    display: inline-flex;
    align-items: center;
    padding: 0 0.85rem;
    border-radius: 0.5rem;
    color: #ffffff;
    background: var(--accent, #2563eb);
    font-weight: 800;
    text-decoration: none;
  }

  .notice,
  .empty,
  .empty-detail {
    padding: 0.85rem;
  }

  .notice.error {
    color: var(--danger-text, #991b1b);
    border: 1px solid var(--danger-text, #991b1b);
    border-radius: 0.5rem;
    background: var(--danger-bg, #fef2f2);
  }

  @media (max-width: 1180px) {
    .assistant-page {
      grid-template-columns: minmax(17rem, 22rem) minmax(0, 1fr);
    }

    .capability-detail {
      grid-column: 1 / -1;
      border-top: 1px solid var(--border-soft, #dbe3ef);
    }
  }

  @media (max-width: 760px) {
    .assistant-page {
      display: block;
      min-height: auto;
    }

    .assistant-sidebar,
    .capability-list {
      border-right: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }

    .assistant-header,
    .section-title,
    .detail-header {
      align-items: flex-start;
      flex-direction: column;
    }

    .metrics,
    .detail-metrics,
    .io-grid {
      grid-template-columns: 1fr;
    }

    .capability-row {
      display: grid;
      grid-template-columns: auto minmax(0, 1fr);
    }

    .capability-row em {
      grid-column: 2;
      max-width: none;
      text-align: left;
    }

    .status {
      border-radius: 0.5rem;
    }
  }
</style>
