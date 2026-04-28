<script lang="ts">
  import { onMount } from 'svelte';
  import {
    createHomelabdClient,
    Navbar,
    type HomelabdWorkflow,
    type HomelabdWorkflowStep
  } from '@homelab/shared';
  import {
    compactWorkflowID,
    filterWorkflows,
    parseWorkflowStepLines,
    workflowStatusTone,
    workflowStepLinePlaceholder,
    type WorkflowFilter
  } from './workflow-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
  const client = createHomelabdClient({ baseUrl: apiBase });

  let workflows: HomelabdWorkflow[] = [];
  let selectedWorkflowId = '';
  let filter: WorkflowFilter = 'active';
  let search = '';
  let loading = false;
  let creating = false;
  let runningWorkflowId = '';
  let error = '';
  let notice = '';
  let lastRefresh = '';

  let nameDraft = '';
  let descriptionDraft = '';
  let goalDraft = '';
  let stepsDraft = '';

  let visibleWorkflows: HomelabdWorkflow[] = [];
  let selectedWorkflow: HomelabdWorkflow | undefined;
  let parsedSteps: HomelabdWorkflowStep[] = [];
  let activeCount = 0;

  $: visibleWorkflows = filterWorkflows(workflows, filter, search);
  $: selectedWorkflow =
    workflows.find((workflow) => workflow.id === selectedWorkflowId) || visibleWorkflows[0];
  $: parsedSteps = parseWorkflowStepLines(stepsDraft);
  $: activeCount = workflows.filter((workflow) =>
    ['draft', 'running', 'waiting', 'awaiting_approval'].includes(workflow.status)
  ).length;

  const syncTimeLabel = () =>
    new Date().toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });

  const statusLabel = (status = '') => status.replaceAll('_', ' ');

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

  const updateWorkflow = (workflow: HomelabdWorkflow) => {
    const existing = workflows.some((item) => item.id === workflow.id);
    workflows = existing
      ? workflows.map((item) => (item.id === workflow.id ? workflow : item))
      : [workflow, ...workflows];
    selectedWorkflowId = workflow.id;
  };

  const refreshWorkflows = async () => {
    loading = true;
    error = '';
    try {
      const response = await client.listWorkflows();
      workflows = [...response.workflows].sort(
        (left, right) => Date.parse(right.updated_at) - Date.parse(left.updated_at)
      );
      if (!workflows.some((workflow) => workflow.id === selectedWorkflowId)) {
        selectedWorkflowId = workflows[0]?.id || '';
      }
      lastRefresh = syncTimeLabel();
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to load workflows.';
    } finally {
      loading = false;
    }
  };

  const createWorkflow = async () => {
    const name = nameDraft.trim();
    if (!name || creating) {
      return;
    }
    creating = true;
    error = '';
    notice = '';
    try {
      const response = await client.createWorkflow({
        name,
        description: descriptionDraft.trim() || undefined,
        goal: goalDraft.trim() || undefined,
        steps: parsedSteps.length ? parsedSteps : undefined
      });
      updateWorkflow(response.workflow);
      nameDraft = '';
      descriptionDraft = '';
      goalDraft = '';
      stepsDraft = '';
      notice = response.reply || 'Workflow created.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to create workflow.';
    } finally {
      creating = false;
    }
  };

  const runWorkflow = async (workflow: HomelabdWorkflow) => {
    if (runningWorkflowId) {
      return;
    }
    runningWorkflowId = workflow.id;
    error = '';
    notice = '';
    try {
      const response = await client.runWorkflow(workflow.id);
      updateWorkflow(response.workflow);
      notice = response.reply || 'Workflow run updated.';
    } catch (err) {
      error = err instanceof Error ? err.message : 'Unable to run workflow.';
    } finally {
      runningWorkflowId = '';
    }
  };

  onMount(() => {
    void refreshWorkflows();
    const interval = window.setInterval(() => {
      void refreshWorkflows();
    }, 8000);
    return () => window.clearInterval(interval);
  });
</script>

<svelte:head>
  <title>homelabd Workflows</title>
  <meta name="description" content="Create and monitor homelabd workflows" />
</svelte:head>

<div class="workflow-shell">
  <Navbar title="Workflows" subtitle="homelabd" current="/workflows" taskApiBase={apiBase} />

  <main class="workflow-page">
    <section class="workflow-list" aria-label="Workflow list">
      <header class="workflow-header">
        <div>
          <h1>Workflows</h1>
          <span>{lastRefresh ? `Synced ${lastRefresh}` : apiBase}</span>
        </div>
        <button type="button" disabled={loading} on:click={() => void refreshWorkflows()}>
          {loading ? 'Syncing' : 'Sync'}
        </button>
      </header>

      <div class="workflow-metrics" aria-label="Workflow totals">
        <div>
          <strong>{activeCount}</strong>
          <span>Active</span>
        </div>
        <div>
          <strong>{workflows.length}</strong>
          <span>Total</span>
        </div>
      </div>

      <div class="workflow-filters" aria-label="Workflow filters">
        <button
          type="button"
          class:active={filter === 'active'}
          on:click={() => (filter = 'active')}
        >
          Active
        </button>
        <button type="button" class:active={filter === 'all'} on:click={() => (filter = 'all')}>
          All
        </button>
      </div>

      <label class="hidden" for="workflow-search">Search workflows</label>
      <input
        id="workflow-search"
        class="search"
        type="search"
        bind:value={search}
        placeholder="Search workflows"
      />

      <details class="create-workflow">
        <summary>New workflow</summary>
        <form on:submit|preventDefault={() => void createWorkflow()}>
          <label for="workflow-name">Name</label>
          <input id="workflow-name" bind:value={nameDraft} autocomplete="off" />

          <label for="workflow-goal">Goal</label>
          <textarea id="workflow-goal" bind:value={goalDraft} rows="3"></textarea>

          <label for="workflow-description">Description</label>
          <textarea id="workflow-description" bind:value={descriptionDraft} rows="2"></textarea>

          <label for="workflow-steps">Steps</label>
          <textarea
            id="workflow-steps"
            bind:value={stepsDraft}
            rows="6"
            placeholder={workflowStepLinePlaceholder}
          ></textarea>

          <div class="create-footer">
            <span>{parsedSteps.length || 1} estimated step{(parsedSteps.length || 1) === 1 ? '' : 's'}</span>
            <button type="submit" disabled={creating || !nameDraft.trim()}>
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

      <div class="rows" aria-label="Workflow rows">
        {#if visibleWorkflows.length}
          {#each visibleWorkflows as workflow (workflow.id)}
            <button
              type="button"
              class="workflow-row"
              class:selected={selectedWorkflow?.id === workflow.id}
              on:click={() => (selectedWorkflowId = workflow.id)}
            >
              <span class={`dot ${workflowStatusTone(workflow.status)}`}></span>
              <span>
                <strong>{workflow.name}</strong>
                <small>{compactWorkflowID(workflow.id)} · {statusLabel(workflow.status)}</small>
              </span>
              <em>{workflow.estimate?.estimated_minutes || 0}m</em>
            </button>
          {/each}
        {:else}
          <p class="empty">No workflows match this view.</p>
        {/if}
      </div>
    </section>

    <section class="workflow-detail" aria-label="Workflow detail">
      {#if selectedWorkflow}
        <header class="detail-header">
          <div>
            <span class="eyebrow">{compactWorkflowID(selectedWorkflow.id)}</span>
            <h2>{selectedWorkflow.name}</h2>
            <p>{selectedWorkflow.goal || selectedWorkflow.description || 'No goal recorded.'}</p>
          </div>
          <div class="detail-actions" aria-label="Workflow actions">
            <span class={`status ${workflowStatusTone(selectedWorkflow.status)}`}>
              {statusLabel(selectedWorkflow.status)}
            </span>
            <button
              type="button"
              disabled={Boolean(runningWorkflowId)}
              on:click={() => void runWorkflow(selectedWorkflow)}
            >
              {runningWorkflowId === selectedWorkflow.id ? 'Running' : 'Run'}
            </button>
          </div>
        </header>

        <div class="estimate" role="region" aria-label="Workflow cost estimate">
          <div>
            <span>LLM</span>
            <strong>{selectedWorkflow.estimate?.estimated_llm_calls || 0}</strong>
          </div>
          <div>
            <span>Tools</span>
            <strong>{selectedWorkflow.estimate?.estimated_tool_calls || 0}</strong>
          </div>
          <div>
            <span>Waits</span>
            <strong>{selectedWorkflow.estimate?.waits || 0}</strong>
          </div>
          <div>
            <span>Runtime</span>
            <strong>{selectedWorkflow.estimate?.estimated_minutes || 0}m</strong>
          </div>
        </div>

        <section class="steps" aria-label="Workflow steps">
          <h3>Steps</h3>
          {#each selectedWorkflow.steps as step, index}
            <article class="step">
              <span>{index + 1}</span>
              <div>
                <strong>{step.name}</strong>
                <p>{statusLabel(step.kind)} {step.tool || step.workflow_id || step.condition || step.prompt}</p>
              </div>
            </article>
          {/each}
        </section>

        <details class="run-detail" aria-label="Workflow run" open={Boolean(selectedWorkflow.last_run)}>
          <summary>Latest run</summary>
          {#if selectedWorkflow.last_run}
            <div class="run-meta">
              <span>{statusLabel(selectedWorkflow.last_run.status)}</span>
              <span>{compactTime(selectedWorkflow.last_run.started_at)}</span>
              <span>
                {(selectedWorkflow.last_run.outputs || []).length}/{selectedWorkflow.steps.length} steps
              </span>
            </div>
            <ol>
              {#each selectedWorkflow.last_run.outputs || [] as output}
                <li>
                  <strong>{output.step_name}</strong>
                  <span>{statusLabel(output.status)}</span>
                  {#if output.summary}
                    <p>{output.summary}</p>
                  {/if}
                  {#if output.error}
                    <p class="output-error">{output.error}</p>
                  {/if}
                </li>
              {/each}
            </ol>
          {:else}
            <p>No run yet.</p>
          {/if}
        </details>
      {:else}
        <div class="empty-detail">
          <h2>No workflow selected</h2>
          <p>Create or sync workflows to monitor runs.</p>
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
  textarea {
    font: inherit;
  }

  .workflow-shell {
    min-height: 100dvh;
    background: var(--bg, #eef2f7);
  }

  .workflow-page {
    display: grid;
    grid-template-columns: minmax(20rem, 26rem) minmax(0, 1fr);
    min-height: calc(100dvh - 4.15rem);
  }

  .workflow-list {
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    min-width: 0;
    padding: 1rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--panel, #f8fafc);
  }

  .workflow-detail {
    min-width: 0;
    padding: 1.2rem;
    background: var(--bg, #eef2f7);
  }

  .workflow-header,
  .detail-header,
  .create-footer,
  .workflow-filters,
  .run-meta {
    display: flex;
    align-items: center;
    gap: 0.7rem;
  }

  .workflow-header,
  .detail-header,
  .create-footer {
    justify-content: space-between;
  }

  h1,
  h2,
  h3,
  p {
    margin: 0;
  }

  h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.35rem;
  }

  h2 {
    color: var(--text-strong, #0f172a);
    font-size: 1.45rem;
  }

  h3 {
    color: var(--text-strong, #0f172a);
    font-size: 1rem;
  }

  .workflow-header span,
  .detail-header p,
  .create-footer span,
  .run-meta,
  .step p,
  .empty,
  .empty-detail p,
  .eyebrow {
    color: var(--muted, #64748b);
    font-size: 0.86rem;
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
    opacity: 0.6;
  }

  .workflow-header button,
  .detail-actions button,
  .create-footer button {
    padding: 0 0.9rem;
    color: #ffffff;
    border-color: var(--accent, #2563eb);
    background: var(--accent, #2563eb);
  }

  .workflow-metrics,
  .estimate {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.65rem;
  }

  .workflow-metrics div,
  .estimate div,
  .create-workflow,
  .steps,
  .run-detail,
  .empty-detail {
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .workflow-metrics div,
  .estimate div {
    padding: 0.75rem;
  }

  .workflow-metrics strong,
  .estimate strong {
    display: block;
    color: var(--text-strong, #0f172a);
    font-size: 1.15rem;
  }

  .workflow-metrics span,
  .estimate span {
    color: var(--muted, #64748b);
    font-size: 0.8rem;
  }

  .workflow-filters button {
    flex: 1;
    padding: 0 0.75rem;
  }

  .workflow-filters button.active {
    color: #ffffff;
    border-color: var(--accent, #2563eb);
    background: var(--accent, #2563eb);
  }

  .search,
  input,
  textarea {
    width: 100%;
    box-sizing: border-box;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.5rem;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
  }

  .search,
  input {
    min-height: 2.5rem;
    padding: 0 0.75rem;
  }

  textarea {
    min-width: 0;
    padding: 0.65rem 0.75rem;
    resize: vertical;
  }

  .create-workflow summary,
  .run-detail summary {
    cursor: pointer;
    padding: 0.8rem;
    color: var(--text-strong, #0f172a);
    font-weight: 800;
  }

  .create-workflow form {
    display: grid;
    gap: 0.55rem;
    padding: 0 0.8rem 0.8rem;
  }

  .create-workflow label {
    color: var(--text-strong, #0f172a);
    font-size: 0.82rem;
    font-weight: 750;
  }

  .notice {
    padding: 0.7rem 0.8rem;
    border-radius: 0.5rem;
    font-size: 0.86rem;
  }

  .notice.error {
    color: var(--danger-text, #991b1b);
    background: var(--danger-bg, #fef2f2);
  }

  .notice.success {
    border: 1px solid var(--success-border, #bbf7d0);
    background: var(--success-bg, #f0fdf4);
  }

  .rows {
    display: grid;
    gap: 0.55rem;
  }

  .workflow-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.65rem;
    min-height: 4.25rem;
    padding: 0.65rem;
    text-align: left;
  }

  .workflow-row.selected {
    border-color: var(--accent, #2563eb);
    box-shadow: 0 0 0 1px var(--accent, #2563eb);
  }

  .workflow-row strong,
  .workflow-row small {
    display: block;
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .workflow-row small {
    color: var(--muted, #64748b);
    font-size: 0.78rem;
  }

  .workflow-row em {
    color: var(--muted, #64748b);
    font-size: 0.82rem;
    font-style: normal;
  }

  .dot {
    width: 0.72rem;
    height: 0.72rem;
    border-radius: 999px;
    background: #94a3b8;
  }

  .green {
    background: #16a34a;
  }

  .red {
    background: #dc2626;
  }

  .amber {
    background: #d97706;
  }

  .blue {
    background: #2563eb;
  }

  .gray {
    background: #94a3b8;
  }

  .detail-header,
  .estimate,
  .steps,
  .run-detail,
  .empty-detail {
    max-width: 58rem;
  }

  .detail-header {
    margin-bottom: 0.9rem;
  }

  .detail-actions {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    flex-wrap: wrap;
    justify-content: flex-end;
  }

  .status {
    padding: 0.35rem 0.55rem;
    border-radius: 999px;
    color: #ffffff;
    font-size: 0.78rem;
    font-weight: 800;
  }

  .estimate {
    grid-template-columns: repeat(4, minmax(0, 1fr));
    margin-bottom: 0.9rem;
  }

  .steps,
  .run-detail,
  .empty-detail {
    padding: 0.9rem;
  }

  .steps {
    display: grid;
    gap: 0.65rem;
    margin-bottom: 0.9rem;
  }

  .step {
    display: grid;
    grid-template-columns: 2rem minmax(0, 1fr);
    gap: 0.65rem;
    align-items: start;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface-muted, #f8fafc);
  }

  .step > span {
    display: grid;
    place-items: center;
    width: 1.75rem;
    height: 1.75rem;
    border-radius: 999px;
    color: #ffffff;
    background: var(--accent, #2563eb);
    font-size: 0.82rem;
    font-weight: 800;
  }

  .step strong,
  .step p {
    overflow-wrap: anywhere;
  }

  .run-detail ol {
    display: grid;
    gap: 0.65rem;
    margin: 0.8rem 0 0;
    padding-left: 1.1rem;
  }

  .run-detail li {
    color: var(--text, #172033);
  }

  .run-detail li span {
    margin-left: 0.4rem;
    color: var(--muted, #64748b);
    font-size: 0.82rem;
  }

  .run-detail p {
    margin-top: 0.25rem;
    overflow-wrap: anywhere;
  }

  .output-error {
    color: var(--danger-text, #991b1b);
  }

  .empty,
  .empty-detail {
    padding: 1rem;
  }

  .hidden {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
    white-space: nowrap;
  }

  @media (max-width: 760px) {
    .workflow-page {
      display: block;
      min-height: auto;
    }

    .workflow-list {
      border-right: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }

    .workflow-detail {
      padding: 1rem;
    }

    .detail-header {
      align-items: flex-start;
      flex-direction: column;
    }

    .detail-actions {
      justify-content: flex-start;
      width: 100%;
    }

    .detail-actions button {
      flex: 1;
    }

    .estimate {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
</style>
