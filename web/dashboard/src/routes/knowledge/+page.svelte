<script lang="ts">
  import { browser } from '$app/environment';
  import { afterNavigate, goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import {
    createHomelabdClient,
    knowledgeSpaceURL,
    Navbar,
    type HomelabdKnowledgeReport,
    type HomelabdKnowledgeSpace
  } from '@homelab/shared';
  import {
    compactKnowledgeID,
    filterKnowledgeSpaces,
    knowledgeSpacesFromResponse,
    latestReport,
    panelLabel,
    panelItemCount,
    selectKnowledgeSpace,
    spaceSourceCount,
    spaceWordCount,
    sourceSelectionSummary,
    type KnowledgePanel
  } from './view-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  const panels: KnowledgePanel[] = ['sources', 'research', 'reports'];

  let spaces: HomelabdKnowledgeSpace[] = [];
  let selectedSpaceId = '';
  let lastAppliedRouteSpaceId = '';
  let lastSelectedSpaceId = '';
  let activePanel: KnowledgePanel = 'sources';
  let search = '';
  let loading = false;
  let creating = false;
  let addingSource = false;
  let researching = false;
  let error = '';
  let notice = '';
  let lastRefresh = '';
  let selectedSourceIds: string[] = [];
  let ready = false;
  let detailEl: HTMLElement | undefined;
  let createSpaceOpen = false;
  let addSourceOpen = false;

  let titleDraft = '';
  let objectiveDraft = '';
  let descriptionDraft = '';
  let sourceTitleDraft = '';
  let sourceKindDraft = 'text';
  let sourceURIDraft = '';
  let sourceContentDraft = '';
  let questionDraft = '';
  let researchModeDraft = 'research';
  let activeReport: HomelabdKnowledgeReport | undefined;

  let visibleSpaces: HomelabdKnowledgeSpace[] = [];
  let selectedSpace: HomelabdKnowledgeSpace | undefined;
  let latestSelectedReport: HomelabdKnowledgeReport | undefined;
  let totalSourceCount = 0;
  let selectedSourceCount = 0;
  let selectedSourceSummary = '';
  let totalReportCount = 0;
  let totalSpaceSourceCount = 0;

  $: visibleSpaces = filterKnowledgeSpaces(spaces, search);
  $: selectedSpaceId = selectKnowledgeSpace(
    spaces,
    visibleSpaces,
    selectedSpaceId,
    browser ? currentRouteSpaceId() : ''
  );
  $: selectedSpace = spaces.find((space) => space.id === selectedSpaceId);
  $: latestSelectedReport = activeReport || latestReport(selectedSpace);
  $: totalSourceCount = selectedSpace?.sources?.length || 0;
  $: selectedSourceCount = selectedSourceIds.length;
  $: selectedSourceSummary = sourceSelectionSummary(selectedSourceCount, totalSourceCount);
  $: totalReportCount = spaces.reduce((total, space) => total + (space.reports?.length || 0), 0);
  $: totalSpaceSourceCount = spaces.reduce((total, space) => total + spaceSourceCount(space), 0);
  $: if (selectedSpace && selectedSpace.id !== lastSelectedSpaceId) {
    lastSelectedSpaceId = selectedSpace.id;
    activeReport = undefined;
    selectedSourceIds = (selectedSpace.sources || []).map((source) => source.id);
    addSourceOpen = !(selectedSpace.sources?.length);
  }

  const currentRouteSpaceId = () => (browser ? $page.url.searchParams.get('space') || '' : '');

  const routeSpaceIdFromLocation = () =>
    typeof window !== 'undefined'
      ? new URL(window.location.href).searchParams.get('space') || ''
      : '';

  const currentRoutePath = () =>
    browser ? `${$page.url.pathname}${$page.url.search}${$page.url.hash}` : '';

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const compactTime = (value?: string) => {
    if (!value) {
      return 'unknown';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return value;
    }
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  };

  const plural = (count: number, singular: string, pluralLabel = `${singular}s`) =>
    `${count} ${count === 1 ? singular : pluralLabel}`;

  const navigateToSpace = (spaceId: string, replaceState = false) => {
    if (!browser || !spaceId) {
      return;
    }
    const next = knowledgeSpaceURL(spaceId);
    if (currentRoutePath() === next) {
      return;
    }
    lastAppliedRouteSpaceId = spaceId;
    void goto(next, { keepFocus: true, noScroll: true, replaceState });
  };

  const isCompactKnowledgeViewport = () =>
    typeof window !== 'undefined' && window.matchMedia('(max-width: 760px)').matches;

  const revealDetailIfCompact = () => {
    if (!isCompactKnowledgeViewport()) {
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

  const applyRouteSpaceSelection = (spaceId: string) => {
    if (!spaceId) {
      return;
    }
    selectedSpaceId = spaceId;
    search = '';
  };

  const selectSpace = (spaceId: string) => {
    selectedSpaceId = spaceId;
    navigateToSpace(spaceId);
    revealDetailIfCompact();
  };

  const handleSpaceRowClick = (event: MouseEvent, spaceId: string) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
      return;
    }
    event.preventDefault();
    selectSpace(spaceId);
  };

  const handleKnowledgePopState = () => {
    window.setTimeout(() => {
      const spaceId = routeSpaceIdFromLocation();
      if (!spaceId) {
        return;
      }
      applyRouteSpaceSelection(spaceId);
      lastAppliedRouteSpaceId = spaceId;
    }, 0);
  };

  afterNavigate(({ to }) => {
    if (!browser || to?.url.pathname !== '/knowledge') {
      return;
    }
    const spaceId = to.url.searchParams.get('space') || '';
    if (!spaceId || spaceId === selectedSpaceId) {
      return;
    }
    applyRouteSpaceSelection(spaceId);
    lastAppliedRouteSpaceId = spaceId;
  });

  const updateSpace = (space: HomelabdKnowledgeSpace) => {
    const existing = spaces.some((item) => item.id === space.id);
    spaces = existing
      ? spaces.map((item) => (item.id === space.id ? space : item))
      : [space, ...spaces];
    selectedSpaceId = space.id;
    navigateToSpace(space.id);
  };

  const refreshSpaces = async () => {
    loading = true;
    error = '';
    try {
      const response = await client.listKnowledgeSpaces();
      spaces = [...knowledgeSpacesFromResponse(response)].sort(
        (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
      );
      const routeSpaceId = currentRouteSpaceId();
      if (routeSpaceId && spaces.some((space) => space.id === routeSpaceId)) {
        selectedSpaceId = routeSpaceId;
        search = '';
      }
      if (!spaces.some((space) => space.id === selectedSpaceId)) {
        selectedSpaceId = spaces[0]?.id || '';
      }
      if (!spaces.length) {
        createSpaceOpen = true;
      }
      lastRefresh = syncTimeLabel();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load Knowledge Space data.';
    } finally {
      loading = false;
      ready = true;
    }
  };

  const createSpace = async () => {
    const title = titleDraft.trim();
    if (!title || creating) {
      return;
    }
    creating = true;
    error = '';
    notice = '';
    try {
      const response = await client.createKnowledgeSpace({
        title,
        objective: objectiveDraft.trim() || undefined,
        description: descriptionDraft.trim() || undefined
      });
      updateSpace(response.space);
      titleDraft = '';
      objectiveDraft = '';
      descriptionDraft = '';
      notice = response.reply || 'Knowledge Space created.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to create Knowledge Space.';
    } finally {
      creating = false;
    }
  };

  const addSource = async () => {
    if (!selectedSpace || addingSource || !sourceTitleDraft.trim() || !sourceContentDraft.trim()) {
      return;
    }
    addingSource = true;
    error = '';
    notice = '';
    try {
      const response = await client.addKnowledgeSource(selectedSpace.id, {
        title: sourceTitleDraft.trim(),
        kind: sourceKindDraft,
        uri: sourceURIDraft.trim() || undefined,
        content: sourceContentDraft.trim()
      });
      updateSpace(response.space);
      activePanel = 'sources';
      sourceTitleDraft = '';
      sourceURIDraft = '';
      sourceContentDraft = '';
      selectedSourceIds = (response.space.sources || []).map((source) => source.id);
      addSourceOpen = false;
      notice = response.reply || 'Source processed.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to add source.';
    } finally {
      addingSource = false;
    }
  };

  const runResearch = async () => {
    if (!selectedSpace || researching || !questionDraft.trim()) {
      return;
    }
    researching = true;
    error = '';
    notice = '';
    try {
      const response = await client.researchKnowledgeSpace(selectedSpace.id, {
        question: questionDraft.trim(),
        mode: researchModeDraft,
        source_ids: selectedSourceIds.length ? selectedSourceIds : undefined
      });
      activeReport = response.report;
      updateSpace(response.space);
      activePanel = 'research';
      notice = response.reply || 'Research report created.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to run research.';
    } finally {
      researching = false;
    }
  };

  const toggleSourceSelection = (sourceId: string) => {
    selectedSourceIds = selectedSourceIds.includes(sourceId)
      ? selectedSourceIds.filter((id) => id !== sourceId)
      : [...selectedSourceIds, sourceId];
  };

  const selectAllSources = () => {
    selectedSourceIds = (selectedSpace?.sources || []).map((source) => source.id);
  };

  const clearSourceSelection = () => {
    selectedSourceIds = [];
  };

  const clearSearch = () => {
    search = '';
  };

  const useSuggestedQuestion = (question: string) => {
    questionDraft = question;
    activePanel = 'research';
    revealDetailIfCompact();
  };

  const selectReport = (report: HomelabdKnowledgeReport) => {
    activeReport = report;
    activePanel = 'research';
    revealDetailIfCompact();
  };

  const handleTabKeydown = (event: KeyboardEvent, panel: KnowledgePanel) => {
    const index = panels.indexOf(panel);
    const nextPanel = (nextIndex: number) => {
      const panelId = panels[(nextIndex + panels.length) % panels.length];
      activePanel = panelId;
      requestAnimationFrame(() => document.getElementById(`knowledge-tab-${panelId}`)?.focus());
    };
    if (event.key === 'ArrowRight') {
      event.preventDefault();
      nextPanel(index + 1);
    } else if (event.key === 'ArrowLeft') {
      event.preventDefault();
      nextPanel(index - 1);
    } else if (event.key === 'Home') {
      event.preventDefault();
      nextPanel(0);
    } else if (event.key === 'End') {
      event.preventDefault();
      nextPanel(panels.length - 1);
    }
  };

  onMount(() => {
    void refreshSpaces();
    const interval = window.setInterval(() => {
      void refreshSpaces();
    }, 10000);
    window.addEventListener('popstate', handleKnowledgePopState);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener('popstate', handleKnowledgePopState);
    };
  });
</script>

<svelte:head>
  <title>homelabd Knowledge Space</title>
  <meta name="description" content="Organise and research source-grounded Knowledge Space material" />
</svelte:head>

<div class="knowledge-shell">
  <Navbar title="Knowledge Space" subtitle="homelabd" current="/knowledge" taskApiBase={apiBase} />

  <main class="knowledge-page" data-ready={ready ? 'true' : 'false'}>
    <section class="space-list" aria-label="Knowledge Space list">
      <header class="space-header">
        <div>
          <h1>Knowledge Space</h1>
          <span>{lastRefresh ? `Synced ${lastRefresh}` : loading ? 'Loading spaces' : 'Not synced'}</span>
        </div>
        <button
          type="button"
          class="sync-button"
          disabled={loading}
          aria-label={loading ? 'Syncing Knowledge Spaces' : 'Sync Knowledge Spaces'}
          title="Sync Knowledge Spaces"
          on:click={() => void refreshSpaces()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M20 12a8 8 0 0 1-13.7 5.7M4 12A8 8 0 0 1 17.7 6.3M7 18H4v-3M17 6h3v3" />
          </svg>
          <span>{loading ? 'Syncing' : 'Sync'}</span>
        </button>
      </header>

      <div class="space-metrics" aria-label="Knowledge Space totals">
        <span><strong>{spaces.length}</strong> {spaces.length === 1 ? 'space' : 'spaces'}</span>
        <span><strong>{totalSpaceSourceCount}</strong> {totalSpaceSourceCount === 1 ? 'source' : 'sources'}</span>
        <span><strong>{totalReportCount}</strong> {totalReportCount === 1 ? 'report' : 'reports'}</span>
      </div>

      <label class="hidden" for="knowledge-search">Search Knowledge Space</label>
      <span class="search-control">
        <input
          id="knowledge-search"
          class="search"
          type="search"
          bind:value={search}
          placeholder="Search spaces"
        />
        {#if search}
          <button
            type="button"
            class="icon-button"
            aria-label="Clear search input"
            title="Clear search"
            on:click={clearSearch}
          >
            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        {/if}
      </span>

      <details class="create-space" bind:open={createSpaceOpen}>
        <summary>New space</summary>
        <form on:submit|preventDefault={() => void createSpace()}>
          <label for="space-title">Title</label>
          <input id="space-title" bind:value={titleDraft} autocomplete="off" />

          <label for="space-objective">Objective</label>
          <textarea id="space-objective" bind:value={objectiveDraft} rows="3"></textarea>

          <label for="space-description">Description</label>
          <textarea id="space-description" bind:value={descriptionDraft} rows="2"></textarea>

          <div class="form-footer">
            <span>{titleDraft.trim() ? 'Ready' : 'Title required'}</span>
            <button type="submit" disabled={creating || !titleDraft.trim()}>
              {creating ? 'Creating' : 'Create'}
            </button>
          </div>
        </form>
      </details>

      {#if error}
        <p class="notice error" role="alert">{error}</p>
      {/if}
      {#if notice}
        <p class="notice success">{notice}</p>
      {/if}

      <div class="rows" aria-label="Knowledge Space rows">
        {#if visibleSpaces.length}
          {#each visibleSpaces as space (space.id)}
            <a
              href={knowledgeSpaceURL(space.id)}
              class="space-row"
              class:selected={selectedSpace?.id === space.id}
              aria-current={selectedSpace?.id === space.id ? 'page' : undefined}
              on:click={(event) => handleSpaceRowClick(event, space.id)}
            >
              <span class="dot"></span>
              <span>
                <strong>{space.title}</strong>
                <small>{compactKnowledgeID(space.id)} · {spaceSourceCount(space)} sources</small>
              </span>
              <em>{plural(spaceWordCount(space), 'word')}</em>
            </a>
          {/each}
        {:else if loading}
          <p class="empty">Loading Knowledge Spaces...</p>
        {:else}
          <div class="empty">
            <p>{search ? 'No Knowledge Space matches this search.' : 'No Knowledge Spaces yet.'}</p>
            {#if search}
              <button type="button" class="text-action" on:click={clearSearch}>Clear search</button>
            {/if}
          </div>
        {/if}
      </div>
    </section>

    <section class="space-detail" aria-label="Knowledge Space detail" bind:this={detailEl}>
      {#if selectedSpace}
        <header class="detail-header">
          <div>
            <span class="eyebrow">{compactKnowledgeID(selectedSpace.id)}</span>
            <h2>{selectedSpace.title}</h2>
            <p>{selectedSpace.objective || selectedSpace.description || 'No objective recorded.'}</p>
          </div>
          <div class="detail-actions" aria-label="Knowledge Space actions">
            <span>{plural(spaceSourceCount(selectedSpace), 'source')}</span>
            <span>{plural(spaceWordCount(selectedSpace), 'word')}</span>
          </div>
        </header>

        <div class="insight-bar" aria-label="Knowledge Space insight">
          <div class="insight-card">
            <span>Key terms</span>
            {#if selectedSpace.insight?.key_terms?.length}
              <div class="chips">
                {#each selectedSpace.insight.key_terms.slice(0, 6) as term}
                  <span>{term}</span>
                {/each}
              </div>
            {:else}
              <strong>None yet</strong>
            {/if}
          </div>
          <div class="insight-card">
            <span>Suggested questions</span>
            {#if selectedSpace.insight?.suggested_questions?.length}
              <div class="question-chips" aria-label="Research suggestions">
                {#each selectedSpace.insight.suggested_questions.slice(0, 3) as question}
                  <button type="button" on:click={() => useSuggestedQuestion(question)}>
                    {question}
                  </button>
                {/each}
              </div>
            {:else}
              <strong>No suggestions yet</strong>
            {/if}
          </div>
        </div>

        <div class="tabs" role="tablist" aria-label="Knowledge Space panels">
          {#each panels as panel}
            <button
              id={`knowledge-tab-${panel}`}
              type="button"
              role="tab"
              aria-selected={activePanel === panel}
              aria-controls={`knowledge-panel-${panel}`}
              class:active={activePanel === panel}
              tabindex={activePanel === panel ? 0 : -1}
              on:click={() => (activePanel = panel)}
              on:keydown={(event) => handleTabKeydown(event, panel)}
            >
              <span>{panelLabel(panel)}</span>
              <small>{panelItemCount(panel, selectedSpace)}</small>
            </button>
          {/each}
        </div>

        {#if activePanel === 'sources'}
          <div
            id="knowledge-panel-sources"
            class="panel sources-panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-sources"
          >
            <section class="source-list-section" aria-label="Processed sources">
              <header class="panel-title">
                <div>
                  <h3>Processed sources</h3>
                  <p>{spaceSourceCount(selectedSpace)} source{spaceSourceCount(selectedSpace) === 1 ? '' : 's'} available for research.</p>
                </div>
              </header>
              <div class="source-list">
                {#if selectedSpace.sources?.length}
                  {#each selectedSpace.sources as source (source.id)}
                    <article class="source-card">
                      <header>
                        <div>
                          <span>{source.kind}</span>
                          <h3>{source.title}</h3>
                        </div>
                        <strong>{source.word_count} words</strong>
                      </header>
                      <p>{source.summary}</p>
                      {#if source.key_terms?.length}
                        <div class="chips" aria-label={`${source.title} key terms`}>
                          {#each source.key_terms.slice(0, 6) as term}
                            <span>{term}</span>
                          {/each}
                        </div>
                      {/if}
                    </article>
                  {/each}
                {:else}
                  <p class="empty">No sources have been processed. Add source text before running research.</p>
                {/if}
              </div>
            </section>

            <details class="add-source" bind:open={addSourceOpen}>
              <summary>Add source</summary>
              <form class="source-form" on:submit|preventDefault={() => void addSource()}>
                <div class="form-grid">
                  <label for="source-title">Source title</label>
                  <input id="source-title" bind:value={sourceTitleDraft} autocomplete="off" />

                  <label for="source-kind">Source type</label>
                  <select id="source-kind" bind:value={sourceKindDraft}>
                    <option value="text">Text</option>
                    <option value="url">URL</option>
                    <option value="file">File</option>
                    <option value="note">Note</option>
                  </select>

                  <label for="source-uri">Reference</label>
                  <input id="source-uri" bind:value={sourceURIDraft} autocomplete="off" />
                </div>

                <label for="source-content">Source text</label>
                <textarea id="source-content" bind:value={sourceContentDraft} rows="8"></textarea>

                <div class="form-footer">
                  <span>{sourceContentDraft.trim().split(/\s+/).filter(Boolean).length} words</span>
                  <button
                    type="submit"
                    disabled={addingSource || !sourceTitleDraft.trim() || !sourceContentDraft.trim()}
                  >
                    {addingSource ? 'Processing' : 'Process source'}
                  </button>
                </div>
              </form>
            </details>
          </div>
        {:else if activePanel === 'research'}
          <div
            id="knowledge-panel-research"
            class="panel research-panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-research"
          >
            <form class="research-form" on:submit|preventDefault={() => void runResearch()}>
              <div class="panel-title">
                <div>
                  <h3>Research draft</h3>
                  <p>{selectedSourceSummary}</p>
                </div>
              </div>

              <label for="research-question">Question</label>
              <textarea id="research-question" bind:value={questionDraft} rows="3"></textarea>

              <div class="research-controls">
                <label for="research-mode">Mode</label>
                <select id="research-mode" bind:value={researchModeDraft}>
                  <option value="research">Research</option>
                  <option value="brief">Brief</option>
                  <option value="study">Study</option>
                </select>
                <button type="button" disabled={!selectedSpace.sources?.length} on:click={selectAllSources}>
                  Select all
                </button>
                <button type="button" disabled={!selectedSourceIds.length} on:click={clearSourceSelection}>
                  Clear
                </button>
                <button
                  type="submit"
                  disabled={researching || !questionDraft.trim() || !selectedSourceIds.length}
                >
                  {researching ? 'Researching' : researchModeDraft === 'brief' ? 'Create brief' : 'Run research'}
                </button>
              </div>

              {#if selectedSpace.sources?.length}
                <div class="source-select" aria-label="Research source selection">
                  {#each selectedSpace.sources as source (source.id)}
                    <label>
                      <input
                        type="checkbox"
                        checked={selectedSourceIds.includes(source.id)}
                        on:change={() => toggleSourceSelection(source.id)}
                      />
                      <span>{source.title}</span>
                    </label>
                  {/each}
                </div>
              {/if}
            </form>

            {#if latestSelectedReport}
              <article class="report-card" aria-label="Latest research report">
                <header>
                  <div>
                    <span>{latestSelectedReport.mode}</span>
                    <h3>{latestSelectedReport.question}</h3>
                  </div>
                  <strong>{compactTime(latestSelectedReport.created_at)}</strong>
                </header>
                <pre>{latestSelectedReport.answer}</pre>
                {#if latestSelectedReport.evidence?.length}
                  <div class="evidence-list" aria-label="Report evidence">
                    {#each latestSelectedReport.evidence as evidence (evidence.id)}
                      <section>
                        <strong>[{evidence.citation_label}] {evidence.source_title}</strong>
                        <p>{evidence.excerpt}</p>
                      </section>
                    {/each}
                  </div>
                {/if}
                {#if latestSelectedReport.gaps?.length}
                  <div class="gaps">
                    {#each latestSelectedReport.gaps as gap}
                      <span>{gap}</span>
                    {/each}
                  </div>
                {/if}
              </article>
            {:else}
              <p class="empty">No report has been generated.</p>
            {/if}
          </div>
        {:else}
          <div
            id="knowledge-panel-reports"
            class="panel"
            role="tabpanel"
            aria-labelledby="knowledge-tab-reports"
          >
            <div class="reports-list" aria-label="Knowledge Space reports">
              {#if selectedSpace.reports?.length}
                {#each selectedSpace.reports as report (report.id)}
                  <button type="button" class="report-row" on:click={() => selectReport(report)}>
                    <header>
                      <div>
                        <span>{report.mode}</span>
                        <h3>{report.question}</h3>
                      </div>
                      <strong>{compactTime(report.created_at)}</strong>
                    </header>
                    <p>{report.key_findings?.[0] || report.answer}</p>
                  </button>
                {/each}
              {:else}
                <p class="empty">No reports are stored.</p>
              {/if}
            </div>
          </div>
        {/if}
      {:else}
        <div class="empty-detail">
          <h2>No Knowledge Space selected</h2>
          <p>Create or sync spaces to begin.</p>
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
    --knowledge-muted: #475569;
    --knowledge-primary-bg: #172554;
    --knowledge-primary-text: #ffffff;
  }

  :global(html[data-theme='dark']) {
    --knowledge-muted: #b7c6da;
    --knowledge-primary-bg: #172554;
    --knowledge-primary-text: #ffffff;
  }

  button,
  input,
  textarea,
  select {
    font: inherit;
  }

  button,
  summary,
  select,
  input,
  textarea {
    border-radius: 8px;
  }

  button,
  summary {
    cursor: pointer;
  }

  h1,
  h2,
  h3,
  p {
    margin: 0;
  }

  .knowledge-shell {
    min-height: 100dvh;
    background: var(--bg, #eef2f7);
  }

  .knowledge-page {
    display: grid;
    grid-template-columns: minmax(20rem, 25rem) minmax(0, 1fr);
    min-height: calc(100dvh - 4.15rem);
  }

  .space-list {
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    min-width: 0;
    padding: 1rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #f8fafc);
  }

  .space-detail {
    min-width: 0;
    padding: 1.2rem;
    background: var(--bg, #eef2f7);
  }

  .space-header,
  .detail-header,
  .form-footer,
  .research-controls,
  .source-card header,
  .report-card header,
  .report-row header,
  .detail-actions {
    display: flex;
    align-items: center;
    gap: 0.7rem;
  }

  .space-header,
  .detail-header,
  .form-footer,
  .source-card header,
  .report-card header,
  .report-row header {
    justify-content: space-between;
  }

  .space-header h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.45rem;
    line-height: 1.15;
  }

  .space-header span,
  .detail-header p,
  .panel-title p,
  .source-card p,
  .report-row p,
  .empty,
  .empty-detail p {
    color: var(--knowledge-muted, #475569);
  }

  .space-header button,
  .form-footer button,
  .research-controls button,
  .tabs button {
    min-height: 2.4rem;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
    font-weight: 700;
  }

  .space-header button,
  .form-footer button,
  .research-controls button {
    padding: 0.45rem 0.75rem;
  }

  .sync-button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    color: var(--knowledge-primary-text, #ffffff) !important;
    border-color: var(--knowledge-primary-bg, #172554) !important;
    background: var(--knowledge-primary-bg, #172554) !important;
  }

  .sync-button span {
    color: var(--knowledge-primary-text, #ffffff);
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

  button:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }

  .space-metrics,
  .insight-bar {
    display: grid;
    gap: 0.7rem;
  }

  .space-metrics {
    grid-template-columns: repeat(3, minmax(0, 1fr));
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .insight-bar {
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 0.9fr);
    margin: 1rem 0;
  }

  .insight-card {
    min-width: 0;
    padding: 0.8rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #ffffff);
    border-radius: 8px;
  }

  .space-metrics strong,
  .insight-bar strong {
    display: block;
    overflow-wrap: anywhere;
    color: var(--text-strong, #0f172a);
    font-size: 1.15rem;
  }

  .space-metrics span,
  .insight-bar span,
  .eyebrow,
  .source-card header span,
  .report-card header span,
  .report-row header span,
  .form-footer span {
    color: var(--knowledge-muted, #475569);
    font-size: 0.78rem;
    font-weight: 800;
    letter-spacing: 0;
    text-transform: uppercase;
  }

  .space-metrics span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-transform: none;
  }

  .search-control {
    position: relative;
    display: block;
  }

  .search,
  input,
  textarea,
  select {
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    color: var(--text, #172033);
  }

  .search,
  input,
  select {
    min-height: 2.5rem;
    padding: 0.5rem 0.65rem;
  }

  .search-control .search {
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

  textarea {
    padding: 0.65rem;
    resize: vertical;
  }

  label {
    color: var(--text-strong, #0f172a);
    font-weight: 700;
  }

  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  .create-space,
  .add-source {
    border: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #ffffff);
    border-radius: 8px;
  }

  .create-space summary,
  .add-source summary {
    padding: 0.75rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
  }

  .create-space form,
  .source-form,
  .research-form {
    display: grid;
    gap: 0.7rem;
  }

  .create-space form,
  .add-source form {
    padding: 0 0.75rem 0.75rem;
  }

  .notice {
    padding: 0.65rem 0.75rem;
    border-radius: 8px;
    border: 1px solid var(--border, #cbd5e1);
    background: var(--panel, #ffffff);
    font-weight: 700;
  }

  .notice.error {
    color: var(--danger, #dc2626);
    border-color: color-mix(in srgb, var(--danger, #dc2626) 35%, var(--border, #cbd5e1));
  }

  .notice.success {
    color: var(--success, #16a34a);
    border-color: color-mix(in srgb, var(--success, #16a34a) 35%, var(--border, #cbd5e1));
  }

  .text-action {
    width: fit-content;
    min-height: 2.35rem;
    padding: 0.4rem 0.75rem;
    color: var(--knowledge-primary-bg, #172554);
    border: 1px solid var(--knowledge-primary-bg, #172554);
    background: var(--panel, #ffffff);
    font-weight: 800;
  }

  .rows,
  .source-list,
  .reports-list,
  .evidence-list,
  .gaps {
    display: grid;
    gap: 0.7rem;
  }

  .space-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.65rem;
    min-width: 0;
    padding: 0.75rem;
    border: 1px solid transparent;
    border-radius: 8px;
    color: inherit;
    text-decoration: none;
    background: var(--panel, #ffffff);
  }

  .space-row:hover,
  .space-row.selected {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .space-row strong,
  .space-row small,
  .space-row em {
    overflow-wrap: anywhere;
  }

  .space-row strong {
    display: block;
    color: var(--text-strong, #0f172a);
  }

  .space-row small,
  .space-row em {
    color: var(--knowledge-muted, #475569);
    font-style: normal;
  }

  .dot {
    width: 0.65rem;
    height: 0.65rem;
    border-radius: 999px;
    background: var(--secondary, #0f766e);
  }

  .detail-header {
    align-items: flex-start;
    gap: 1rem;
  }

  .detail-header h2 {
    color: var(--text-strong, #0f172a);
    font-size: clamp(1.35rem, 2vw, 2rem);
    line-height: 1.12;
  }

  .detail-actions {
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .detail-actions span {
    padding: 0.35rem 0.6rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 999px;
    background: var(--panel, #ffffff);
    color: var(--knowledge-muted, #475569);
    font-weight: 800;
  }

  .tabs {
    display: flex;
    gap: 0.45rem;
    overflow-x: auto;
    padding-bottom: 0.2rem;
  }

  .tabs button {
    display: inline-flex;
    align-items: center;
    gap: 0.45rem;
    flex: 0 0 auto;
    padding: 0.45rem 0.9rem;
  }

  .tabs button small {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.35rem;
    min-height: 1.35rem;
    padding: 0 0.25rem;
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--bg, #eef2f7);
    font-size: 0.75rem;
    font-weight: 850;
  }

  .tabs button.active {
    border-color: var(--knowledge-primary-bg, #172554);
    background: var(--knowledge-primary-bg, #172554);
    color: var(--knowledge-primary-text, #ffffff);
  }

  .tabs button.active small {
    color: var(--knowledge-primary-bg, #172554);
    background: var(--knowledge-primary-text, #ffffff);
  }

  .panel {
    margin-top: 0.8rem;
  }

  .panel-title {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .panel-title h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
  }

  .sources-panel {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(18rem, 25rem);
    gap: 1rem;
    align-items: start;
  }

  .source-list-section {
    min-width: 0;
  }

  .form-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(8rem, 12rem);
    gap: 0.7rem;
  }

  .form-grid label {
    grid-column: span 1;
  }

  .form-grid label[for='source-uri'],
  .form-grid input#source-uri {
    grid-column: 1 / -1;
  }

  .source-card,
  .report-card,
  .report-row,
  .evidence-list section {
    min-width: 0;
    padding: 0.9rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .report-row {
    width: 100%;
    color: inherit;
    text-align: left;
  }

  .report-row:hover,
  .report-row:focus-visible {
    border-color: var(--primary, #2563eb);
    box-shadow: 0 0 0 1px var(--primary, #2563eb);
  }

  .source-card h3,
  .report-card h3,
  .report-row h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
    line-height: 1.25;
    overflow-wrap: anywhere;
  }

  .source-card p,
  .report-row p,
  .evidence-list p {
    margin-top: 0.55rem;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }

  .source-list,
  .source-list-section {
    margin-top: 1rem;
  }

  .chips,
  .gaps,
  .question-chips {
    display: flex;
    flex-wrap: wrap;
    gap: 0.4rem;
    margin-top: 0.65rem;
  }

  .chips span,
  .gaps span {
    max-width: 100%;
    padding: 0.3rem 0.5rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--knowledge-muted, #475569);
    background: var(--bg, #eef2f7);
    font-size: 0.82rem;
    font-weight: 700;
    overflow-wrap: anywhere;
  }

  .question-chips button {
    max-width: 100%;
    min-height: 2.25rem;
    padding: 0.35rem 0.55rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    color: var(--text, #172033);
    background: var(--bg, #eef2f7);
    font-weight: 750;
    text-align: left;
    overflow-wrap: anywhere;
  }

  .research-panel {
    display: grid;
    grid-template-columns: minmax(18rem, 24rem) minmax(0, 1fr);
    gap: 1rem;
    align-items: start;
  }

  .research-controls {
    flex-wrap: wrap;
  }

  .research-controls label {
    min-width: 100%;
  }

  .research-controls select {
    width: auto;
    min-width: 9rem;
  }

  .source-select {
    display: grid;
    gap: 0.45rem;
    max-height: 16rem;
    overflow: auto;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .source-select label {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
    font-weight: 600;
  }

  .source-select input {
    width: 1rem;
    min-height: 1rem;
  }

  .source-select span {
    overflow-wrap: anywhere;
  }

  .report-card pre {
    margin: 0.75rem 0 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    color: var(--text, #172033);
    font: inherit;
    line-height: 1.5;
  }

  .evidence-list {
    margin-top: 0.8rem;
  }

  .evidence-list strong {
    overflow-wrap: anywhere;
  }

  .empty,
  .empty-detail {
    display: grid;
    gap: 0.65rem;
    padding: 1rem;
    border: 1px dashed var(--border, #cbd5e1);
    border-radius: 8px;
    background: var(--panel, #ffffff);
  }

  .empty-detail {
    display: grid;
    place-content: center;
    min-height: 60vh;
    text-align: center;
  }

  :global([data-theme='dark']) .tabs button.active {
    color: var(--knowledge-primary-text, #ffffff);
  }

  @media (max-width: 1080px) {
    .knowledge-page,
    .research-panel,
    .sources-panel {
      grid-template-columns: 1fr;
    }

    .space-list {
      border-right: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }
  }

  @media (max-width: 760px) {
    .knowledge-page {
      min-height: auto;
    }

    .space-list,
    .space-detail {
      padding: 0.8rem;
    }

    .space-header,
    .detail-header,
    .form-footer,
    .source-card header,
    .report-card header,
    .report-row header {
      align-items: flex-start;
      flex-direction: column;
    }

    .space-header button,
    .form-footer button,
    .research-controls button {
      width: 100%;
    }

    .space-metrics,
    .insight-bar,
    .form-grid {
      grid-template-columns: 1fr;
    }

    .form-grid label,
    .form-grid input,
    .form-grid select {
      grid-column: 1;
    }

    .detail-actions {
      justify-content: flex-start;
    }

    .tabs {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      overflow-x: visible;
    }

    .tabs button {
      justify-content: center;
      min-width: 0;
      padding: 0.45rem 0.35rem;
    }

    .research-controls select {
      width: 100%;
    }
  }
</style>
