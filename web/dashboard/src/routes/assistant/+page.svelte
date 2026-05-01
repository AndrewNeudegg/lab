<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createHomelabdClient,
    Navbar,
    type AssistantActivity,
    type AssistantCapability,
    type AssistantCatalogue,
    type AssistantUXPattern
  } from '@homelab/shared';
  import {
    activityCountForCapability,
    assistantAreaLabel,
    assistantAutonomyLabel,
    assistantAutonomyTone,
    activityForCapability,
    patternsForCapability,
    primaryCapabilityForActivity,
    selectAssistantCapability
  } from './assistant-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const searchDelayMs = 250;

  let catalogue: AssistantCatalogue | undefined;
  let selectedCapabilityId = '';
  let selectedCapability: AssistantCapability | undefined;
  let selectedActivity: AssistantActivity | undefined;
  let selectedPatterns: AssistantUXPattern[] = [];
  let search = '';
  let area = 'all';
  let hasActiveFilters = false;
  let loading = true;
  let error = '';
  let lastSynced = '';
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  let mounted = false;
  let detailEl: HTMLElement | undefined;

  $: selectedCapability = selectAssistantCapability(catalogue?.capabilities || [], selectedCapabilityId);
  $: selectedActivity = activityForCapability(selectedCapability, catalogue?.activities || []);
  $: selectedPatterns = patternsForCapability(selectedCapability, catalogue?.ux_patterns || []);
  $: hasActiveFilters = Boolean(search.trim() || area !== 'all');

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

  const revealDetailIfCompact = () => {
    if (typeof window === 'undefined' || !window.matchMedia('(max-width: 760px)').matches) {
      return;
    }
    requestAnimationFrame(() => {
      if (!detailEl) {
        return;
      }
      const navbarBottom = document.querySelector('.navbar')?.getBoundingClientRect().bottom || 0;
      const detailTop = detailEl.getBoundingClientRect().top + window.scrollY;
      window.scrollTo({ top: Math.max(0, detailTop - navbarBottom - 8) });
    });
  };

  const selectCapability = (capabilityId: string, revealDetail = true) => {
    selectedCapabilityId = capabilityId;
    if (revealDetail) {
      revealDetailIfCompact();
    }
  };

  const selectActivity = (activity: AssistantActivity) => {
    const capability = primaryCapabilityForActivity(activity, catalogue?.capabilities || []);
    if (capability) {
      selectCapability(capability.id);
    }
  };

  const activityTone = (activity: AssistantActivity) =>
    assistantAutonomyTone(
      primaryCapabilityForActivity(activity, catalogue?.capabilities || [])?.autonomy
    );

  const resetFilters = () => {
    search = '';
    area = 'all';
    scheduleRefresh();
  };

  const clearSearch = () => {
    search = '';
    scheduleRefresh();
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

  <main class="assistant-page" data-ready={!loading && catalogue ? 'true' : 'false'}>
    <section class="assistant-sidebar" aria-label="Assistant controls">
      <header class="assistant-header">
        <div>
          <h1>Assistant</h1>
          <span>{lastSynced ? `Synced ${lastSynced}` : loading ? 'Loading catalogue' : 'Not synced'}</span>
        </div>
        <button
          type="button"
          class="sync-button"
          disabled={loading}
          aria-label={loading ? 'Syncing assistant catalogue' : 'Sync assistant catalogue'}
          title="Sync assistant catalogue"
          on:click={() => void refreshAssistant()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
          </svg>
          <span>{loading ? 'Syncing' : 'Sync'}</span>
        </button>
      </header>

      {#if catalogue}
        <p class="assistant-summary">{catalogue.summary}</p>

        <div class="catalogue-strip" aria-label="Assistant totals">
          <span><strong>{catalogue.capabilities.length}</strong> capabilities</span>
          <span><strong>{catalogue.activities.length}</strong> activities</span>
          <span><strong>{catalogue.ux_patterns.length}</strong> UX patterns</span>
        </div>

        <div class="filter-panel" aria-label="Assistant filters">
          <label class="field area-field" for="assistant-area">
            <span>Area</span>
            <select id="assistant-area" bind:value={area} on:change={changeArea}>
              {#each catalogue.filters.areas as option}
                <option value={option.value}>{option.label} ({option.count})</option>
              {/each}
            </select>
          </label>

          <label class="field search-field" for="assistant-search">
            <span>Search</span>
            <span class="search-control">
              <input
                id="assistant-search"
                type="search"
                value={search}
                placeholder="Search"
                on:input={changeSearch}
              />
              {#if search}
                <button
                  type="button"
                  class="icon-button"
                  aria-label="Clear search"
                  title="Clear search"
                  on:click={clearSearch}
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                    <path d="M6 6l12 12M18 6 6 18" />
                  </svg>
                </button>
              {/if}
            </span>
          </label>
        </div>

        <section class="activities" aria-label="Assistant activities">
          <div class="subsection-title">
            <h2>Useful outcomes</h2>
            <span>Choose what you want improved.</span>
          </div>
          {#if catalogue.activities.length}
            <div class="activity-list">
              {#each catalogue.activities as activity}
                <button
                  type="button"
                  class="activity"
                  class:selected={selectedActivity?.id === activity.id}
                  aria-pressed={selectedActivity?.id === activity.id}
                  on:click={() => selectActivity(activity)}
                >
                  <span class={`tone ${activityTone(activity)}`}></span>
                  <span class="activity-copy">
                    <strong>{activity.name}</strong>
                    <small>{assistantAreaLabel(activity.area)} · {activity.cadence}</small>
                    <span>{activity.outcome}</span>
                  </span>
                </button>
              {/each}
            </div>
          {:else}
            <div class="empty">
              <p>No activities match this filter.</p>
              {#if hasActiveFilters}
                <button type="button" class="text-action" on:click={resetFilters}>Clear filters</button>
              {/if}
            </div>
          {/if}
        </section>
      {:else if error}
        <p class="notice error" role="alert">{error} Use Sync to retry.</p>
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
        <span>
          {catalogue?.capabilities.length
            ? `${catalogue.capabilities.length} visible`
            : catalogue?.updated_at
              ? new Date(catalogue.updated_at).toLocaleDateString()
              : ''}
        </span>
      </header>

      {#if catalogue?.capabilities.length}
        <div class="capability-rows">
          {#each catalogue.capabilities as capability}
            <button
              type="button"
              class="capability-row"
              class:selected={selectedCapability?.id === capability.id}
              aria-pressed={selectedCapability?.id === capability.id}
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
      {:else if loading}
        <p class="empty">Loading visible capabilities...</p>
      {:else}
        <div class="empty">
          <p>No capabilities match this view.</p>
          {#if hasActiveFilters}
            <button type="button" class="text-action" on:click={resetFilters}>Clear filters</button>
          {/if}
        </div>
      {/if}
    </section>

    <section class="capability-detail" aria-label="Assistant capability detail" bind:this={detailEl}>
      {#if selectedCapability}
        <header class="detail-header">
          <div>
            <p>{assistantAreaLabel(selectedCapability.area)}</p>
            <h2>{selectedCapability.name}</h2>
            <span>{selectedCapability.promise}</span>
          </div>
          <div class="detail-actions">
            <span class={`status ${assistantAutonomyTone(selectedCapability.autonomy)}`}>
              {assistantAutonomyLabel(selectedCapability.autonomy)}
            </span>
            {#if selectedCapability.surfaces.length}
              <nav class="surface-links" aria-label="Related assistant surfaces">
                {#each selectedCapability.surfaces as surface}
                  <a href={surface.href}>{surface.label}</a>
                {/each}
              </nav>
            {/if}
          </div>
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
              <ul class="token-list">
                {#each selectedCapability.inputs as input}
                  <li>{input}</li>
                {/each}
              </ul>
            </div>
            <div>
              <h4>Creates</h4>
              <ul class="token-list">
                {#each selectedCapability.outputs as output}
                  <li>{output}</li>
                {/each}
              </ul>
            </div>
          </div>
        </section>

        <section class="detail-section" aria-label="Assistant safeguards">
          <h3>Safeguards</h3>
          <ul class="checks token-list">
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
      {:else if loading}
        <div class="empty-detail">
          <h2>Loading Assistant</h2>
          <p>Fetching capabilities, safeguards, workflow templates, and related surfaces.</p>
        </div>
      {:else}
        <div class="empty-detail">
          <h2>No capability selected</h2>
          <p>Adjust the filters or clear search to inspect Assistant behaviour.</p>
          {#if hasActiveFilters}
            <button type="button" class="text-action" on:click={resetFilters}>Clear filters</button>
          {/if}
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

  :global(:root) {
    --assistant-muted: #475569;
    --assistant-primary-bg: #172554;
    --assistant-primary-text: #ffffff;
  }

  :global(html[data-theme='dark']) {
    --assistant-muted: #b7c6da;
    --assistant-primary-bg: #172554;
    --assistant-primary-text: #ffffff;
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
  .subsection-title span,
  .activity small,
  .activity-copy > span,
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
    color: var(--assistant-muted, #475569);
    font-size: 0.86rem;
  }

  .assistant-summary,
  .activity-copy > span,
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

  .sync-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    padding: 0 0.9rem;
    color: var(--assistant-primary-text, #ffffff);
    border-color: var(--assistant-primary-bg, #1d4ed8);
    background: var(--assistant-primary-bg, #1d4ed8);
  }

  .sync-button svg,
  .icon-button svg {
    width: 1rem;
    height: 1rem;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .sync-button span {
    color: var(--assistant-primary-text, #ffffff);
  }

  .catalogue-strip,
  .detail-metrics {
    display: grid;
    gap: 0.65rem;
  }

  .catalogue-strip {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .detail-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    margin-bottom: 0.85rem;
  }

  .detail-metrics div,
  .activity,
  .detail-section,
  .empty-detail {
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .detail-metrics div {
    padding: 0.7rem;
  }

  .catalogue-strip strong,
  .detail-metrics strong {
    color: var(--text-strong, #0f172a);
    font-size: 1.08rem;
  }

  .catalogue-strip span,
  .detail-metrics span {
    color: var(--assistant-muted, #475569);
    font-size: 0.76rem;
  }

  .catalogue-strip span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .field {
    display: grid;
    gap: 0.35rem;
    color: var(--text-strong, #0f172a);
    font-size: 0.82rem;
    font-weight: 800;
  }

  .filter-panel {
    display: grid;
    grid-template-columns: minmax(8rem, 0.8fr) minmax(0, 1.2fr);
    gap: 0.65rem;
  }

  .search-control {
    position: relative;
    display: block;
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

  .search-control input {
    padding-right: 2.8rem;
  }

  .icon-button {
    position: absolute;
    top: 0.25rem;
    right: 0.25rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2rem;
    min-height: 2rem;
    padding: 0;
  }

  .activities {
    display: grid;
    gap: 0.65rem;
  }

  .subsection-title {
    display: grid;
    gap: 0.12rem;
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
    width: 100%;
    align-items: flex-start;
    gap: 0.65rem;
    padding: 0.7rem;
    text-align: left;
    cursor: pointer;
  }

  .activity strong,
  .capability-row strong,
  .pattern strong,
  .steps strong {
    color: var(--text-strong, #0f172a);
  }

  .activity-copy {
    display: grid;
    min-width: 0;
    gap: 0.18rem;
  }

  .capability-row {
    width: 100%;
    align-items: flex-start;
    padding: 0.75rem;
    text-align: left;
    cursor: pointer;
  }

  .activity.selected,
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
    background: #172554;
  }

  .status.blue {
    color: #ffffff;
    background: #172554;
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

  .detail-actions {
    display: grid;
    justify-items: end;
    gap: 0.55rem;
    min-width: 12rem;
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

  .token-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
    padding-left: 0;
    list-style: none;
  }

  .token-list li {
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.45rem;
    background: var(--surface-muted, #f8fafc);
    font-size: 0.84rem;
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
    justify-content: flex-end;
    gap: 0.55rem;
  }

  .surface-links a {
    min-height: 2.35rem;
    display: inline-flex;
    align-items: center;
    padding: 0 0.85rem;
    border-radius: 0.5rem;
    color: var(--assistant-primary-text, #ffffff);
    background: var(--assistant-primary-bg, #1d4ed8);
    font-weight: 800;
    text-decoration: none;
  }

  .notice,
  .empty,
  .empty-detail {
    padding: 0.85rem;
  }

  .empty {
    display: grid;
    gap: 0.65rem;
  }

  .text-action {
    width: fit-content;
    padding: 0 0.75rem;
    color: var(--assistant-primary-bg, #1d4ed8);
    border-color: var(--assistant-primary-bg, #1d4ed8);
    background: var(--surface, #ffffff);
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

    .section-title,
    .detail-header {
      align-items: flex-start;
      flex-direction: column;
    }

    .catalogue-strip,
    .filter-panel,
    .detail-metrics,
    .io-grid {
      grid-template-columns: 1fr;
    }

    .assistant-header {
      align-items: center;
    }

    .detail-actions {
      justify-items: start;
      min-width: 0;
    }

    .surface-links {
      justify-content: flex-start;
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
