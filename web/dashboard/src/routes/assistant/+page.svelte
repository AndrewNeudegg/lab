<script lang="ts">
  import { browser } from '$app/environment';
  import { afterNavigate, goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount, tick } from 'svelte';
  import {
    assistantRunURL,
    createHomelabdClient,
    Navbar,
    type AssistantRun,
    type AssistantRunAction,
    type AssistantRunFinding
  } from '@homelab/shared';
  import {
    assistantRunActionCount,
    assistantRunActionStatusLabel,
    assistantRunActionStatusTone,
    assistantRunDecisionLabel,
    assistantRunStatusTone,
    selectAssistantRun
  } from './assistant-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });
  type MobilePanel = 'runs' | 'detail';

  let runs: AssistantRun[] = [];
  let selectedRunId = '';
  let selectedRun: AssistantRun | undefined;
  let runsLoading = true;
  let runStarting = false;
  let actionUpdating: string[] = [];
  let runsError = '';
  let runNotice = '';
  let lastSynced = '';
  let detailEl: HTMLElement | undefined;
  let mobilePanel: MobilePanel = 'runs';
  let lastAppliedRouteRunId = '';
  let pendingRouteRunId = '';
  let pendingOverviewRoute = false;

  $: selectedRun = selectAssistantRun(runs, selectedRunId);
  $: totalRunActions = runs.reduce((total, run) => total + assistantRunActionCount(run), 0);
  $: openRunActions = runs.reduce((total, run) => total + runOpenActionCount(run), 0);

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const refreshAssistantRuns = async () => {
    runsLoading = true;
    runsError = '';
    try {
      const response = await client.listAssistantRuns();
      runs = response.runs || [];
      applySyncedRunSelection();
      lastSynced = syncTimeLabel();
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to load proactive Assistant runs.';
    } finally {
      runsLoading = false;
    }
  };

  const currentRunRouteId = () => (browser ? $page.url.searchParams.get('run') || '' : '');

  const currentRoutePath = () =>
    browser ? `${$page.url.pathname}${$page.url.search}${$page.url.hash}` : '';

  const revealDetailIfCompact = () => {
    if (typeof window === 'undefined' || !window.matchMedia('(max-width: 760px)').matches) {
      return;
    }
    void tick().then(() => {
      if (!detailEl) {
        return;
      }
      const navbarBottom = document.querySelector('.navbar')?.getBoundingClientRect().bottom || 0;
      const detailTop = detailEl.getBoundingClientRect().top + window.scrollY;
      window.scrollTo({ top: Math.max(0, detailTop - navbarBottom - 8) });
    });
  };

  const showRunList = () => {
    mobilePanel = 'runs';
    if (typeof window !== 'undefined' && window.matchMedia('(max-width: 760px)').matches) {
      window.scrollTo({ top: 0 });
    }
  };

  const applyRouteRunSelection = (runId: string) => {
    if (!runId) {
      return;
    }
    selectedRunId = runId;
    mobilePanel = 'detail';
    revealDetailIfCompact();
  };

  const applySyncedRunSelection = () => {
    const routeRunId = currentRunRouteId();
    if (routeRunId && runs.some((run) => run.id === routeRunId)) {
      selectedRunId = routeRunId;
      lastAppliedRouteRunId = routeRunId;
      mobilePanel = 'detail';
      return;
    }
    if (selectedRunId && !runs.some((run) => run.id === selectedRunId)) {
      selectedRunId = runs[0]?.id || '';
    } else if (!selectedRunId) {
      selectedRunId = runs[0]?.id || '';
    }
    if (!routeRunId) {
      lastAppliedRouteRunId = '';
    }
  };

  const navigateToRun = (runId: string, replaceState = false) => {
    if (!browser || !runId) {
      return;
    }
    const next = assistantRunURL(runId);
    if (currentRoutePath() === next) {
      return;
    }
    pendingOverviewRoute = false;
    pendingRouteRunId = runId;
    void goto(next, { keepFocus: true, noScroll: true, replaceState }).catch(() => {
      if (pendingRouteRunId === runId) {
        pendingRouteRunId = '';
      }
    });
  };

  const navigateToRunOverview = (replaceState = true) => {
    showRunList();
    if (!browser || currentRoutePath() === '/assistant') {
      pendingOverviewRoute = false;
      pendingRouteRunId = '';
      lastAppliedRouteRunId = '';
      return;
    }
    pendingOverviewRoute = true;
    pendingRouteRunId = '';
    lastAppliedRouteRunId = '';
    void goto('/assistant', { keepFocus: true, noScroll: true, replaceState }).catch(() => {
      pendingOverviewRoute = false;
    });
  };

  const selectRun = (runId: string, replaceState = false) => {
    applyRouteRunSelection(runId);
    navigateToRun(runId, replaceState);
  };

  const handleRunRowClick = (event: MouseEvent, runId: string) => {
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    ) {
      return;
    }
    event.preventDefault();
    selectRun(runId);
  };

  const labelFromSlug = (value: unknown) =>
    String(value || '')
      .replaceAll('_', ' ')
      .replaceAll('-', ' ') || 'unknown';

  const formatAssistantTime = (value?: string) => {
    if (!value) {
      return '';
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return '';
    }
    return date.toLocaleString([], {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    });
  };

  const compactRunTime = (run: AssistantRun) =>
    formatAssistantTime(run.updated_at || run.finished_at || run.started_at || run.created_at);

  const shortAssistantId = (value = '') => {
    if (!value) {
      return 'unknown';
    }
    return value.length > 12 ? `${value.slice(0, 10)}...` : value;
  };

  const plural = (count: number, singular: string, pluralLabel = `${singular}s`) =>
    `${count} ${count === 1 ? singular : pluralLabel}`;

  const countTotal = (values: Record<string, number> | undefined) =>
    Object.values(values || {}).reduce((total, value) => total + value, 0);

  const countEntries = (values: Record<string, number> | undefined) =>
    Object.entries(values || {})
      .filter(([, value]) => value > 0)
      .sort(([left], [right]) => left.localeCompare(right));

  const actionIsSettled = (action: AssistantRunAction) =>
    ['created_task', 'dismissed', 'snoozed', 'skipped'].includes(action.status || '');

  const runOpenActionCount = (run: AssistantRun | undefined) =>
    (run?.recommended_actions || []).filter((action) => !actionIsSettled(action)).length;

  const primaryRunAction = (run: AssistantRun | undefined) =>
    (run?.recommended_actions || []).find(
      (action) => action.kind === 'task' && !action.created_task_id && !actionIsSettled(action)
    ) ||
    (run?.recommended_actions || []).find((action) => !actionIsSettled(action));

  const runHasCreatedTask = (run: AssistantRun | undefined) =>
    Boolean(
      (run?.recommended_actions || []).some(
        (action) => action.created_task_id || action.status === 'created_task'
      )
    );

  const runDecisionTone = (run: AssistantRun | undefined) => {
    if (!run || run.status === 'failed' || run.error) {
      return 'red';
    }
    if (runOpenActionCount(run) > 0) {
      return 'amber';
    }
    if (runHasCreatedTask(run) || run.decision === 'created_tasks' || run.status === 'completed') {
      return 'green';
    }
    return assistantRunStatusTone(run.status);
  };

  const runDecisionTitle = (run: AssistantRun | undefined) => {
    if (!run) {
      return 'No run selected';
    }
    if (run.status === 'failed' || run.error) {
      return 'Run needs diagnosis';
    }
    const open = runOpenActionCount(run);
    if (open > 0) {
      return `${open} ${open === 1 ? 'recommendation' : 'recommendations'} to decide`;
    }
    if (runHasCreatedTask(run)) {
      return 'Recommendation acted on';
    }
    if (run.decision === 'no_op') {
      return 'No action needed';
    }
    return assistantRunDecisionLabel(run.decision);
  };

  const runDecisionDetail = (run: AssistantRun | undefined) => {
    if (!run) {
      return 'Select a proactive run to inspect its evidence and returned receipts.';
    }
    if (run.error) {
      return run.error;
    }
    if (runOpenActionCount(run) > 0) {
      return 'Review the evidence, then create work, mark useful, snooze, or dismiss the recommendation.';
    }
    if (runHasCreatedTask(run)) {
      return 'A task was created from this signal. Open it from the recommendation receipt when you need the work record.';
    }
    return run.summary || run.goal || 'The Assistant recorded this run without requesting operator action.';
  };

  const runRowSubtitle = (run: AssistantRun) =>
    [assistantRunDecisionLabel(run.decision), labelFromSlug(run.status), compactRunTime(run)]
      .filter(Boolean)
      .join(' / ');

  const findingMeta = (finding: AssistantRunFinding) =>
    [finding.surface, finding.severity].filter(Boolean).map(labelFromSlug).join(' / ');

  const actionMeta = (action: AssistantRunAction) =>
    [
      action.kind,
      action.priority,
      action.target_surface,
      action.seen_count && action.seen_count > 1 ? `${action.seen_count} sightings` : ''
    ]
      .filter(Boolean)
      .map(labelFromSlug)
      .join(' / ');

  const actionSupportText = (action: AssistantRunAction) =>
    action.task_goal || action.knowledge_query || action.workflow_hint || '';

  const startProactiveRun = async () => {
    runStarting = true;
    runsError = '';
    runNotice = '';
    try {
      const response = await client.startAssistantRun({
        trigger_kind: 'manual',
        trigger_label: 'Operator requested proactive check',
        goal: 'Review current homelabd state and recommend useful next actions.',
        autonomy: 'propose'
      });
      runs = [response.run, ...runs.filter((run) => run.id !== response.run.id)];
      selectRun(response.run.id);
      runNotice = response.reply || 'Assistant proactive check completed.';
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to start proactive Assistant run.';
    } finally {
      runStarting = false;
    }
  };

  const actionMutationKey = (action: AssistantRunAction) =>
    `${selectedRun?.id || 'run'}:${action.id || action.fingerprint || action.title}`;

  const isActionUpdating = (action: AssistantRunAction) =>
    actionUpdating.includes(actionMutationKey(action));

  const updateSelectedRunAction = async (
    action: AssistantRunAction,
    feedback: 'useful' | 'dismiss' | 'snooze' | 'create_task',
    snoozeSeconds = 24 * 60 * 60
  ) => {
    if (!selectedRun) {
      return;
    }
    const key = actionMutationKey(action);
    actionUpdating = [...actionUpdating, key];
    runsError = '';
    runNotice = '';
    try {
      const response = await client.updateAssistantRunAction(selectedRun.id, action.id, {
        feedback,
        snooze_seconds: feedback === 'snooze' ? snoozeSeconds : undefined
      });
      runs = runs.map((run) => (run.id === response.run.id ? response.run : run));
      selectedRunId = response.run.id;
      mobilePanel = 'detail';
      if (currentRunRouteId() !== response.run.id) {
        navigateToRun(response.run.id, true);
      }
      runNotice = response.reply;
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to update Assistant recommendation.';
    } finally {
      actionUpdating = actionUpdating.filter((value) => value !== key);
    }
  };

  afterNavigate(({ to }) => {
    if (!browser || to?.url.pathname !== '/assistant') {
      return;
    }
    const runId = to.url.searchParams.get('run') || '';
    if (!runId) {
      pendingOverviewRoute = false;
      pendingRouteRunId = '';
      lastAppliedRouteRunId = '';
      showRunList();
      return;
    }
    pendingOverviewRoute = false;
    if (pendingRouteRunId === runId) {
      lastAppliedRouteRunId = runId;
      pendingRouteRunId = '';
      return;
    }
    if (runId === selectedRunId) {
      lastAppliedRouteRunId = runId;
      mobilePanel = 'detail';
      return;
    }
    if (runs.some((run) => run.id === runId)) {
      applyRouteRunSelection(runId);
      lastAppliedRouteRunId = runId;
    }
  });

  $: if (browser) {
    const routeRunId = currentRunRouteId();
    if (
      routeRunId &&
      runs.some((run) => run.id === routeRunId) &&
      !pendingOverviewRoute &&
      routeRunId !== lastAppliedRouteRunId &&
      routeRunId !== pendingRouteRunId
    ) {
      applyRouteRunSelection(routeRunId);
      lastAppliedRouteRunId = routeRunId;
    }
  }

  onMount(() => {
    void refreshAssistantRuns();
  });
</script>

<svelte:head>
  <title>homelabd Assistant</title>
  <meta name="description" content="Review proactive Assistant runs and recommendation receipts" />
</svelte:head>

<div class="assistant-shell">
  <Navbar title="Assistant" subtitle="homelabd" current="/assistant" taskApiBase={apiBase} />

  <main class="assistant-page" data-ready={!runsLoading ? 'true' : 'false'}>
    <aside class="run-pane" data-mobile-hidden={mobilePanel !== 'runs'} aria-label="Assistant runs">
      <header class="run-header">
        <div>
          <p>Assistant runs</p>
          <h1>{openRunActions ? plural(openRunActions, 'decision') : 'Ready to review'}</h1>
          <span>{lastSynced ? `Synced ${lastSynced}` : runsLoading ? 'Loading runs' : 'Not synced'}</span>
        </div>
        <button
          type="button"
          class="run-button"
          disabled={runStarting}
          aria-label={runStarting ? 'Running proactive Assistant check' : 'Run proactive Assistant check'}
          title="Run proactive Assistant check"
          on:click={() => void startProactiveRun()}
        >
          <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <path d="M12 3v4M12 17v4M4.9 4.9l2.8 2.8M16.3 16.3l2.8 2.8M3 12h4M17 12h4M4.9 19.1l2.8-2.8M16.3 7.7l2.8-2.8" />
          </svg>
          <span>{runStarting ? 'Checking' : 'Run check'}</span>
        </button>
      </header>

      <div class="run-metrics" aria-label="Assistant run totals">
        <span><strong>{runs.length}</strong> {runs.length === 1 ? 'run' : 'runs'}</span>
        <span><strong>{totalRunActions}</strong> {totalRunActions === 1 ? 'action' : 'actions'}</span>
        <span><strong>{openRunActions}</strong> open</span>
      </div>

      {#if runsError}
        <section class="notice error" role="alert">
          <div>
            <strong>Assistant sync failed</strong>
            <p>{runsError}</p>
          </div>
        </section>
      {/if}

      <section class="run-list" aria-label="Proactive Assistant runs">
        {#if runsLoading}
          <p class="empty">Loading proactive runs...</p>
        {:else if runs.length}
          {#each runs as run}
            <a
              href={assistantRunURL(run.id)}
              class="run-row"
              class:selected={selectedRun?.id === run.id}
              aria-current={selectedRun?.id === run.id ? 'page' : undefined}
              on:click={(event) => handleRunRowClick(event, run.id)}
            >
              <span
                class={`dot ${assistantRunStatusTone(run.status)}`}
                class:pulse={run.status === 'running'}
                aria-hidden="true"
              ></span>
              <span class="run-copy">
                <strong>{run.trigger.label}</strong>
                <small>
                  <span>{runRowSubtitle(run)}</span>
                  <span class={`status ${runDecisionTone(run)}`}>{runDecisionTitle(run)}</span>
                </small>
                <em>{run.summary || run.goal || 'Snapshot captured for Assistant review.'}</em>
              </span>
            </a>
          {/each}
        {:else}
          <div class="empty">
            <p>No proactive runs yet.</p>
            <button type="button" class="text-action" on:click={() => void startProactiveRun()}>
              Run first check
            </button>
          </div>
        {/if}
      </section>

      <a class="docs-link" href="/docs/dashboard#assistant" aria-label="Open Assistant documentation">
        <span>Assistant docs</span>
        <strong>Capabilities, triggers, and safeguards</strong>
      </a>
    </aside>

    <section
      class="assistant-workbench"
      data-mobile-hidden={mobilePanel !== 'detail'}
      aria-label="Selected Assistant run"
      bind:this={detailEl}
    >
      {#if selectedRun}
        <article class="assistant-record">
          <header class="record-header">
            <button type="button" class="back-to-runs" aria-label="Back to runs" on:click={() => navigateToRunOverview()}>
              <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                <path d="M12.5 4.5 7 10l5.5 5.5" />
              </svg>
              <span>Back to runs</span>
            </button>
            <div>
              <p>{labelFromSlug(selectedRun.trigger.kind)}</p>
              <h2>{selectedRun.trigger.label}</h2>
              <span>{selectedRun.summary || selectedRun.goal || 'Assistant run is waiting for output.'}</span>
            </div>
            <span class={`status ${assistantRunStatusTone(selectedRun.status)}`}>
              {labelFromSlug(selectedRun.status)}
            </span>
          </header>

          {#if runNotice}
            <section class="notice success" role="status" aria-live="polite">
              <div>
                <strong>Assistant updated</strong>
                <p>{runNotice}</p>
              </div>
            </section>
          {/if}

          {#if runsError}
            <section class="notice error" role="alert">
              <div>
                <strong>Assistant action failed</strong>
                <p>{runsError}</p>
              </div>
            </section>
          {/if}

          <section class={`decision-panel ${runDecisionTone(selectedRun)}`} aria-label="Assistant decision">
            <header class="decision-header">
              <div class="decision-copy">
                <span
                  class={`dot ${runDecisionTone(selectedRun)}`}
                  class:pulse={selectedRun.status === 'running'}
                  aria-hidden="true"
                ></span>
                <div>
                  <p>{assistantRunDecisionLabel(selectedRun.decision)}</p>
                  <h3>{runDecisionTitle(selectedRun)}</h3>
                  <span>{runDecisionDetail(selectedRun)}</span>
                </div>
              </div>
              {#if primaryRunAction(selectedRun)}
                {@const primaryAction = primaryRunAction(selectedRun)}
                {#if primaryAction}
                  <button
                    type="button"
                    class="primary-action"
                    disabled={isActionUpdating(primaryAction) ||
                      primaryAction.status === 'created_task' ||
                      Boolean(primaryAction.created_task_id)}
                    on:click={() =>
                      void updateSelectedRunAction(
                        primaryAction,
                        primaryAction.kind === 'task' ? 'create_task' : 'useful'
                      )}
                  >
                    {primaryAction.kind === 'task' ? 'Create task' : 'Mark useful'}
                  </button>
                {/if}
              {/if}
            </header>
          </section>

          <dl class="record-summary" aria-label="Assistant run summary">
            <div>
              <dt>ID</dt>
              <dd>{shortAssistantId(selectedRun.id)}</dd>
            </div>
            <div>
              <dt>Updated</dt>
              <dd>{compactRunTime(selectedRun)}</dd>
            </div>
            <div>
              <dt>Tasks</dt>
              <dd>{countTotal(selectedRun.snapshot.task_counts)}</dd>
            </div>
            <div>
              <dt>Concerns</dt>
              <dd>{selectedRun.concerns?.length || 0}</dd>
            </div>
            <div>
              <dt>Actions</dt>
              <dd>{assistantRunActionCount(selectedRun)}</dd>
            </div>
          </dl>

          {#if selectedRun.error}
            <section class="notice error" role="alert">
              <div>
                <strong>Run failed</strong>
                <p>{selectedRun.error}</p>
              </div>
            </section>
          {/if}

          <section class="detail-section recommendation-section" aria-label="Assistant recommended actions">
            <header class="section-heading">
              <div>
                <p>Decision</p>
                <h3>Recommended actions</h3>
              </div>
              <span>{plural(assistantRunActionCount(selectedRun), 'action')}</span>
            </header>
            {#if selectedRun.recommended_actions?.length}
              <div class="recommendation-list">
                {#each selectedRun.recommended_actions as action}
                  <article class="recommendation-card">
                    <header>
                      <div>
                        <strong>{action.title}</strong>
                        <small>{actionMeta(action)}</small>
                      </div>
                      <span class={`status ${assistantRunActionStatusTone(action.status)}`}>
                        {assistantRunActionStatusLabel(action.status)}
                      </span>
                    </header>
                    <p>{action.rationale}</p>
                    {#if actionSupportText(action)}
                      <small class="action-support">{actionSupportText(action)}</small>
                    {/if}
                    {#if action.created_task_id}
                      <a href={`/tasks?task=${action.created_task_id}`}>Open created task</a>
                    {/if}
                    <div class="action-toolbar" role="group" aria-label={`Recommendation actions for ${action.title}`}>
                      {#if action.kind === 'task'}
                        <button
                          type="button"
                          class="text-action"
                          disabled={isActionUpdating(action) || Boolean(action.created_task_id)}
                          on:click={() => void updateSelectedRunAction(action, 'create_task')}
                        >
                          Create task
                        </button>
                      {/if}
                      <button
                        type="button"
                        class="text-action"
                        disabled={isActionUpdating(action)}
                        on:click={() => void updateSelectedRunAction(action, 'useful')}
                      >
                        Useful
                      </button>
                      <button
                        type="button"
                        class="text-action"
                        disabled={isActionUpdating(action) || action.status === 'snoozed'}
                        on:click={() => void updateSelectedRunAction(action, 'snooze')}
                      >
                        Snooze
                      </button>
                      <button
                        type="button"
                        class="danger-action"
                        disabled={isActionUpdating(action) || action.status === 'dismissed'}
                        on:click={() => void updateSelectedRunAction(action, 'dismiss')}
                      >
                        Dismiss
                      </button>
                    </div>
                  </article>
                {/each}
              </div>
            {:else}
              <p>No follow-up action was recommended.</p>
            {/if}
          </section>

          <section class="detail-section" aria-label="What Assistant noticed">
            <header class="section-heading">
              <div>
                <p>Evidence</p>
                <h3>What it noticed</h3>
              </div>
              <span>{plural((selectedRun.concerns?.length || 0) + (selectedRun.opportunities?.length || 0), 'signal')}</span>
            </header>
            <p>{selectedRun.summary || 'The Assistant did not return a summary for this run.'}</p>
            <div class="signal-grid">
              <div>
                <h4>Concerns</h4>
                {#if selectedRun.concerns?.length}
                  <div class="record-list">
                    {#each selectedRun.concerns as concern}
                      <article class="signal-card">
                        <span
                          class={`dot ${assistantRunStatusTone(concern.severity || 'failed')}`}
                          aria-hidden="true"
                        ></span>
                        <div>
                          <strong>{concern.title}</strong>
                          {#if concern.detail}
                            <p>{concern.detail}</p>
                          {/if}
                          <small>{findingMeta(concern)}</small>
                          {#if concern.object_url}
                            <a href={concern.object_url}>Open related item</a>
                          {/if}
                        </div>
                      </article>
                    {/each}
                  </div>
                {:else}
                  <p>No immediate concerns were found.</p>
                {/if}
              </div>
              <div>
                <h4>Opportunities</h4>
                {#if selectedRun.opportunities?.length}
                  <div class="record-list">
                    {#each selectedRun.opportunities as opportunity}
                      <article class="signal-card">
                        <span
                          class={`dot ${assistantRunStatusTone(opportunity.severity || 'completed')}`}
                          aria-hidden="true"
                        ></span>
                        <div>
                          <strong>{opportunity.title}</strong>
                          {#if opportunity.detail}
                            <p>{opportunity.detail}</p>
                          {/if}
                          <small>{findingMeta(opportunity)}</small>
                          {#if opportunity.object_url}
                            <a href={opportunity.object_url}>Open related item</a>
                          {/if}
                        </div>
                      </article>
                    {/each}
                  </div>
                {:else}
                  <p>No opportunities were recommended for this run.</p>
                {/if}
              </div>
            </div>
          </section>

          <details class="detail-section" aria-label="Assistant evidence snapshot" open>
            <summary>
              <span>Evidence snapshot</span>
              <strong>{formatAssistantTime(selectedRun.snapshot.generated_at) || 'Snapshot'}</strong>
            </summary>
            <div class="detail-body">
              <div class="snapshot-grid">
                <div>
                  <h4>Tasks</h4>
                  {#if countEntries(selectedRun.snapshot.task_counts).length}
                    <ul class="token-list">
                      {#each countEntries(selectedRun.snapshot.task_counts) as [name, count]}
                        <li><strong>{count}</strong> {labelFromSlug(name)}</li>
                      {/each}
                    </ul>
                  {:else}
                    <p>No task counts were available.</p>
                  {/if}
                </div>
                <div>
                  <h4>Workflows</h4>
                  {#if countEntries(selectedRun.snapshot.workflow_counts).length}
                    <ul class="token-list">
                      {#each countEntries(selectedRun.snapshot.workflow_counts) as [name, count]}
                        <li><strong>{count}</strong> {labelFromSlug(name)}</li>
                      {/each}
                    </ul>
                  {:else}
                    <p>No workflow counts were available.</p>
                  {/if}
                </div>
                <div>
                  <h4>Agents</h4>
                  {#if countEntries(selectedRun.snapshot.remote_agent_counts).length}
                    <ul class="token-list">
                      {#each countEntries(selectedRun.snapshot.remote_agent_counts) as [name, count]}
                        <li><strong>{count}</strong> {labelFromSlug(name)}</li>
                      {/each}
                    </ul>
                  {:else}
                    <p>No remote agent counts were available.</p>
                  {/if}
                </div>
              </div>

              {#if selectedRun.snapshot.attention_tasks?.length}
                <div class="object-list" aria-label="Attention tasks">
                  <h4>Attention tasks</h4>
                  {#each selectedRun.snapshot.attention_tasks as item}
                    <article>
                      {#if item.url}
                        <a href={item.url}>{item.title}</a>
                      {:else}
                        <strong>{item.title}</strong>
                      {/if}
                      <span>{[item.status, item.summary].filter(Boolean).map(labelFromSlug).join(' / ')}</span>
                    </article>
                  {/each}
                </div>
              {/if}
            </div>
          </details>

          <details class="detail-section" aria-label="Assistant system signals">
            <summary>
              <span>System signals</span>
              <strong>{selectedRun.snapshot.health?.status || selectedRun.snapshot.supervisor?.status || 'Recorded'}</strong>
            </summary>
            <div class="detail-body system-grid">
              <div>
                <h4>Health</h4>
                <p>{selectedRun.snapshot.health?.status || selectedRun.snapshot.health?.error || 'No health snapshot.'}</p>
                {#if selectedRun.snapshot.health?.items?.length}
                  <div class="object-list">
                    {#each selectedRun.snapshot.health.items as item}
                      <article>
                        <strong>{item.title}</strong>
                        <span>{[item.status, item.summary].filter(Boolean).map(labelFromSlug).join(' / ')}</span>
                      </article>
                    {/each}
                  </div>
                {/if}
              </div>
              <div>
                <h4>Supervisor</h4>
                <p>
                  {selectedRun.snapshot.supervisor?.status ||
                    selectedRun.snapshot.supervisor?.error ||
                    'No supervisor snapshot.'}
                </p>
                {#if selectedRun.snapshot.supervisor?.items?.length}
                  <div class="object-list">
                    {#each selectedRun.snapshot.supervisor.items as item}
                      <article>
                        <strong>{item.title}</strong>
                        <span>{[item.status, item.summary].filter(Boolean).map(labelFromSlug).join(' / ')}</span>
                      </article>
                    {/each}
                  </div>
                {/if}
              </div>
            </div>
          </details>

          <details class="detail-section" aria-label="Assistant run receipts">
            <summary>
              <span>Receipts</span>
              <strong>{plural(selectedRun.receipts?.length || 0, 'receipt')}</strong>
            </summary>
            <div class="detail-body">
              {#if selectedRun.receipts?.length}
                <ol class="receipt-list">
                  {#each selectedRun.receipts as receipt}
                    <li>
                      <strong>{labelFromSlug(receipt.kind)}</strong>
                      <span>{formatAssistantTime(receipt.created_at)}</span>
                      <p>{receipt.message}</p>
                      {#if receipt.object_url}
                        <a href={receipt.object_url}>Open receipt item</a>
                      {/if}
                    </li>
                  {/each}
                </ol>
              {:else}
                <p>No receipts were recorded.</p>
              {/if}
            </div>
          </details>
        </article>
      {:else if runsLoading}
        <div class="empty-record">
          <h2>Loading Assistant runs</h2>
          <p>Fetching proactive checks, recommendations, evidence, and receipts.</p>
        </div>
      {:else}
        <div class="empty-record">
          <h2>No Assistant run selected</h2>
          <p>Select a run from the queue or start a proactive check.</p>
          <button type="button" class="text-action" on:click={() => void startProactiveRun()}>
            Run check
          </button>
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
    --assistant-muted: #9fb0c7;
    --assistant-primary-bg: #1e3a8a;
    --assistant-primary-text: #e0f2fe;
  }

  button {
    font: inherit;
  }

  h1,
  h2,
  h3,
  h4,
  p,
  ul,
  ol,
  dl,
  dd {
    margin: 0;
  }

  .assistant-shell {
    min-height: 100dvh;
    background: var(--bg, #eef2f7);
    overflow-x: clip;
  }

  .assistant-page {
    display: grid;
    grid-template-columns: minmax(20rem, 28rem) minmax(0, 1fr);
    min-height: calc(100dvh - 4.15rem);
    min-width: 0;
    max-width: 100%;
    overflow-x: clip;
  }

  .run-pane {
    display: grid;
    grid-template-areas:
      "header"
      "metrics"
      "notice"
      "list"
      "reference";
    grid-template-rows: auto auto auto minmax(0, 1fr) auto;
    gap: 0.8rem;
    min-width: 0;
    padding: 1rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #f8fafc);
  }

  .assistant-workbench {
    min-width: 0;
    padding: 1.15rem;
    background: var(--bg, #eef2f7);
    overflow-x: clip;
  }

  .assistant-record,
  .empty-record {
    display: grid;
    gap: 0.85rem;
    width: min(100%, 70rem);
  }

  .run-header,
  .record-header,
  .decision-header,
  .decision-copy,
  .notice,
  .section-heading,
  .recommendation-card header,
  .action-toolbar {
    display: flex;
    align-items: center;
    gap: 0.7rem;
  }

  .run-header,
  .record-header,
  .decision-header,
  .section-heading,
  .recommendation-card header {
    justify-content: space-between;
  }

  .run-header {
    grid-area: header;
  }

  .run-header > div,
  .record-header > div,
  .decision-copy > div,
  .section-heading > div,
  .recommendation-card header > div {
    min-width: 0;
  }

  .run-header p,
  .run-header span,
  .record-header p,
  .record-header span,
  .record-summary dt,
  .decision-copy p,
  .section-heading p,
  .detail-section > summary > span,
  .docs-link span,
  h4 {
    color: var(--assistant-muted, #475569);
    font-size: 0.72rem;
    font-weight: 800;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .run-header h1,
  .record-header h2,
  .empty-record h2 {
    color: var(--text-strong, #0f172a);
    font-size: 1.35rem;
    line-height: 1.15;
  }

  .record-header h2 {
    font-size: clamp(1.35rem, 2vw, 2rem);
  }

  .record-header > div > span,
  .decision-copy span,
  .run-copy em,
  .run-copy small,
  .detail-section p,
  .signal-card p,
  .signal-card small,
  .recommendation-card p,
  .recommendation-card small,
  .object-list span,
  .receipt-list span,
  .empty,
  .empty-record p {
    color: var(--assistant-muted, #475569);
    font-size: 0.86rem;
    line-height: 1.35;
  }

  .record-header > div > span,
  .decision-copy span,
  .run-copy em,
  .detail-section p,
  .signal-card p,
  .recommendation-card p,
  .recommendation-card small,
  .object-list span,
  .receipt-list p,
  .empty-record p {
    overflow-wrap: anywhere;
  }

  button,
  .run-row,
  .text-action,
  .danger-action,
  .back-to-runs {
    min-height: 2.45rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 8px;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
    font-weight: 800;
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.58;
  }

  button:hover:not(:disabled),
  .run-row:hover,
  .text-action:hover,
  .danger-action:hover:not(:disabled) {
    border-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .run-button,
  .primary-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    padding: 0 0.85rem;
    color: var(--assistant-primary-text, #ffffff);
    border-color: var(--assistant-primary-bg, #172554);
    background: var(--assistant-primary-bg, #172554);
  }

  .run-button:hover:not(:disabled),
  .primary-action:hover:not(:disabled) {
    border-color: var(--assistant-primary-bg, #172554);
    background: var(--assistant-primary-bg, #172554);
  }

  .run-button svg,
  .back-to-runs svg {
    width: 1rem;
    height: 1rem;
    fill: none;
    stroke: currentColor;
    stroke-width: 2;
    stroke-linecap: round;
    stroke-linejoin: round;
  }

  .run-button span {
    color: var(--assistant-primary-text, #ffffff);
  }

  .record-summary {
    display: grid;
    gap: 0.65rem;
  }

  .run-metrics {
    grid-area: metrics;
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem 0.75rem;
    min-width: 0;
    padding: 0.1rem 0.05rem 0;
    color: var(--assistant-muted, #475569);
  }

  .record-summary dd {
    display: block;
    color: var(--text-strong, #0f172a);
    font-size: 1.08rem;
    font-weight: 850;
  }

  .run-metrics span {
    display: inline-flex;
    align-items: baseline;
    gap: 0.22rem;
    color: var(--assistant-muted, #475569);
    font-size: 0.76rem;
    font-weight: 800;
    white-space: nowrap;
  }

  .run-metrics strong {
    color: var(--text, #172033);
    font-size: 0.82rem;
    font-weight: 850;
  }

  .run-list,
  .recommendation-list,
  .record-list,
  .receipt-list {
    display: grid;
    gap: 0.65rem;
  }

  .run-list {
    grid-area: list;
    align-content: start;
    min-height: 0;
    overflow: auto;
  }

  .run-row {
    display: grid;
    box-sizing: border-box;
    grid-template-columns: auto minmax(0, 1fr);
    align-items: flex-start;
    gap: 0.65rem;
    width: 100%;
    min-width: 0;
    padding: 0.75rem;
    text-align: left;
    text-decoration: none;
    box-shadow: none;
    -webkit-tap-highlight-color: transparent;
  }

  .run-row.selected {
    border-color: var(--border-soft, #dbe3ef);
    background: var(--surface-hover, #eef5ff);
    box-shadow: inset 3px 0 0 var(--accent, #2563eb);
  }

  .run-copy {
    display: grid;
    min-width: 0;
    gap: 0.22rem;
  }

  .run-copy strong,
  .recommendation-card strong,
  .signal-card strong,
  .object-list strong,
  .receipt-list strong,
  .detail-section > summary strong,
  .section-heading h3,
  .decision-copy h3 {
    color: var(--text-strong, #0f172a);
    overflow-wrap: anywhere;
  }

  .run-copy small {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
  }

  .dot {
    --pulse-ring: rgb(148 163 184 / 0.18);
    --pulse-ring-wide: rgb(148 163 184 / 0.09);
    flex: 0 0 auto;
    position: relative;
    width: 0.72rem;
    height: 0.72rem;
    margin-top: 0.22rem;
    border-radius: 999px;
    background: #94a3b8;
    box-shadow: 0 0 0 3px var(--pulse-ring);
  }

  .dot.red,
  .red {
    --pulse-ring: rgb(239 68 68 / 0.18);
    --pulse-ring-wide: rgb(239 68 68 / 0.09);
    background: #ef4444;
  }

  .dot.amber,
  .amber {
    --pulse-ring: rgb(245 158 11 / 0.2);
    --pulse-ring-wide: rgb(245 158 11 / 0.1);
    background: #f59e0b;
  }

  .dot.blue,
  .blue {
    --pulse-ring: rgb(59 130 246 / 0.18);
    --pulse-ring-wide: rgb(59 130 246 / 0.09);
    background: #3b82f6;
  }

  .dot.green,
  .green {
    --pulse-ring: rgb(34 197 94 / 0.18);
    --pulse-ring-wide: rgb(34 197 94 / 0.09);
    background: #22c55e;
  }

  .dot.gray,
  .gray {
    background: #94a3b8;
  }

  .dot.pulse {
    animation: activity-ring 2.4s ease-in-out infinite;
  }

  @keyframes activity-ring {
    0%,
    100% {
      box-shadow: 0 0 0 3px var(--pulse-ring);
    }
    50% {
      box-shadow: 0 0 0 6px var(--pulse-ring-wide);
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .dot.pulse {
      animation: none;
    }
  }

  .status {
    flex: 0 0 auto;
    width: fit-content;
    padding: 0.16rem 0.48rem;
    border-radius: 999px;
    font-size: 0.68rem;
    font-weight: 850;
    line-height: 1.25;
  }

  .status.red {
    color: #991b1b;
    background: #fee2e2;
  }

  .status.amber {
    color: #92400e;
    background: #fef3c7;
  }

  .status.blue {
    color: #1d4ed8;
    background: #dbeafe;
  }

  .status.green {
    color: #166534;
    background: #dcfce7;
  }

  .status.gray {
    color: #475569;
    background: #e2e8f0;
  }

  .notice {
    justify-content: space-between;
    padding: 0.75rem;
    border: 1px solid #fecaca;
    border-radius: 8px;
    color: #7f1d1d;
    background: #fef2f2;
  }

  .run-pane > .notice {
    grid-area: notice;
  }

  .notice.success {
    color: #166534;
    border-color: #bbf7d0;
    background: #f0fdf4;
  }

  .notice strong,
  .notice p {
    margin: 0;
  }

  .notice p {
    overflow-wrap: anywhere;
    font-size: 0.82rem;
    line-height: 1.35;
  }

  .record-header {
    align-items: flex-start;
    padding: 1rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
  }

  .back-to-runs {
    display: none;
    align-items: center;
    gap: 0.35rem;
    width: fit-content;
    padding: 0 0.7rem;
  }

  .decision-panel,
  .record-summary,
  .detail-section,
  .empty-record {
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
  }

  .decision-panel {
    padding: 0.9rem;
    border-left-width: 0.32rem;
  }

  .decision-panel.red {
    border-left-color: #ef4444;
  }

  .decision-panel.amber {
    border-left-color: #f59e0b;
  }

  .decision-panel.blue {
    border-left-color: #3b82f6;
  }

  .decision-panel.green {
    border-left-color: #22c55e;
  }

  .decision-copy {
    align-items: flex-start;
  }

  .record-summary {
    grid-template-columns: repeat(5, minmax(0, 1fr));
    padding: 0.75rem;
  }

  .record-summary div {
    min-width: 0;
  }

  .record-summary dd {
    overflow-wrap: anywhere;
  }

  .detail-section {
    display: grid;
    gap: 0.75rem;
    padding: 0.9rem;
  }

  .detail-section > summary {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
    cursor: pointer;
    list-style: none;
  }

  .detail-section > summary::-webkit-details-marker {
    display: none;
  }

  .detail-section > summary::before {
    content: "▸";
    color: var(--assistant-muted, #475569);
    font-size: 0.8rem;
  }

  .detail-section[open] > summary::before {
    content: "▾";
  }

  .detail-section > summary span {
    margin-right: auto;
  }

  .detail-body {
    display: grid;
    gap: 0.75rem;
  }

  .section-heading > span {
    padding: 0.22rem 0.52rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 999px;
    color: var(--assistant-muted, #475569);
    background: var(--surface-muted, #f8fafc);
    font-size: 0.78rem;
    font-weight: 850;
  }

  .recommendation-card,
  .signal-card,
  .object-list article,
  .receipt-list li {
    min-width: 0;
    padding: 0.75rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface-muted, #f8fafc);
  }

  .recommendation-card {
    display: grid;
    gap: 0.55rem;
  }

  .action-support {
    display: block;
  }

  .recommendation-card a,
  .signal-card a,
  .object-list a,
  .receipt-list a {
    color: var(--accent, #2563eb);
    font-weight: 850;
    text-decoration: none;
  }

  .action-toolbar {
    flex-wrap: wrap;
    align-items: flex-start;
    margin-top: 0.15rem;
  }

  .text-action,
  .danger-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: fit-content;
    min-height: 2.25rem;
    padding: 0.35rem 0.7rem;
    text-decoration: none;
  }

  .text-action {
    color: var(--assistant-primary-bg, #172554);
    border-color: var(--assistant-primary-bg, #172554);
  }

  .danger-action {
    color: var(--danger-text, #991b1b);
    border-color: var(--danger-text, #991b1b);
  }

  .signal-grid,
  .snapshot-grid,
  .system-grid {
    display: grid;
    gap: 0.75rem;
  }

  .signal-grid,
  .system-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .snapshot-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .signal-card {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 0.55rem;
  }

  .token-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
    padding: 0;
    list-style: none;
  }

  .token-list li {
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface-muted, #f8fafc);
    color: var(--text, #172033);
    font-size: 0.84rem;
  }

  .token-list strong {
    margin-right: 0.2rem;
  }

  .object-list {
    display: grid;
    gap: 0.55rem;
  }

  .object-list article {
    display: grid;
    gap: 0.15rem;
  }

  .receipt-list {
    padding-left: 1.1rem;
  }

  .receipt-list li {
    padding-left: 0.9rem;
  }

  .docs-link {
    grid-area: reference;
    display: grid;
    gap: 0.15rem;
    min-width: 0;
    padding: 0.65rem 0.05rem 0;
    border-top: 1px solid var(--border-soft, #dbe3ef);
    color: var(--assistant-muted, #475569);
    text-decoration: none;
  }

  .docs-link strong {
    color: var(--accent, #2563eb);
    font-size: 0.84rem;
    font-weight: 850;
    line-height: 1.3;
    overflow-wrap: anywhere;
  }

  .docs-link:hover strong {
    text-decoration: underline;
  }

  .empty,
  .empty-record {
    padding: 1rem;
    color: var(--assistant-muted, #475569);
  }

  .empty {
    display: grid;
    gap: 0.65rem;
    text-align: center;
  }

  .empty-record {
    align-content: start;
  }

  :global(html[data-theme='dark'] .assistant-shell),
  :global(html[data-theme='dark'] .assistant-page),
  :global(html[data-theme='dark'] .assistant-workbench) {
    background: var(--bg) !important;
  }

  :global(html[data-theme='dark'] .run-pane),
  :global(html[data-theme='dark'] .record-header),
  :global(html[data-theme='dark'] .decision-panel),
  :global(html[data-theme='dark'] .record-summary),
  :global(html[data-theme='dark'] .detail-section),
  :global(html[data-theme='dark'] .empty-record) {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark'] .run-row),
  :global(html[data-theme='dark'] .recommendation-card),
  :global(html[data-theme='dark'] .signal-card),
  :global(html[data-theme='dark'] .object-list article),
  :global(html[data-theme='dark'] .receipt-list li),
  :global(html[data-theme='dark'] .token-list li),
  :global(html[data-theme='dark'] .section-heading > span) {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface-muted) !important;
  }

  :global(html[data-theme='dark'] .run-row:hover),
  :global(html[data-theme='dark'] .run-row.selected) {
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark'] .docs-link) {
    border-color: var(--border-soft) !important;
    color: var(--assistant-muted) !important;
  }

  :global(html[data-theme='dark'] .docs-link strong) {
    color: #93c5fd !important;
  }

  :global(html[data-theme='dark'] .text-action) {
    color: #bfdbfe !important;
    border-color: #60a5fa !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark'] .text-action:hover:not(:disabled)) {
    color: #e0f2fe !important;
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark'] .danger-action) {
    color: #fecaca !important;
    border-color: #f87171 !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark'] .danger-action:hover:not(:disabled)) {
    background: var(--danger-bg) !important;
  }

  :global(html[data-theme='dark'] .notice.success) {
    color: #bbf7d0 !important;
    border-color: rgb(74 222 128 / 0.55) !important;
    background: #163723 !important;
  }

  :global(html[data-theme='dark'] .notice.error) {
    color: #fecaca !important;
    border-color: rgb(248 113 113 / 0.55) !important;
    background: #451a1a !important;
  }

  @media (max-width: 760px) {
    .assistant-page {
      display: block;
      min-height: auto;
    }

    .run-pane,
    .assistant-workbench {
      padding: 0.85rem;
    }

    .run-pane {
      border-right: 0;
    }

    .run-pane[data-mobile-hidden='true'],
    .assistant-workbench[data-mobile-hidden='true'] {
      display: none;
    }

    .assistant-record,
    .empty-record {
      width: 100%;
    }

    .record-header,
    .decision-header,
    .section-heading,
    .recommendation-card header {
      align-items: flex-start;
      flex-direction: column;
    }

    .back-to-runs {
      display: inline-flex;
    }

    .record-summary,
    .signal-grid,
    .snapshot-grid,
    .system-grid {
      grid-template-columns: 1fr;
    }

    .primary-action,
    .run-button {
      width: fit-content;
    }

    .action-toolbar {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      width: 100%;
    }

    .action-toolbar button {
      width: 100%;
    }
  }
</style>
