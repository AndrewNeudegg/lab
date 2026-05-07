<script lang="ts">
  import { browser } from '$app/environment';
  import { afterNavigate, goto } from '$app/navigation';
  import { page } from '$app/stores';
  import { onMount, tick } from 'svelte';
  import {
    assistantRunURL,
    assistantRunsURL,
    createHomelabdClient,
    Navbar,
    type AssistantGoal,
    type AssistantGoalTimeline,
	    type AssistantRun,
	    type AssistantRunAction,
	    type AssistantRunFinding,
	    type AssistantSignalCandidate,
	    type HomelabdRemoteWorkspace,
	    type HomelabdTaskTarget
	  } from '@homelab/shared';
  import {
    activeAssistantGoals,
    assistantGoalAutopilotStatusLabel,
    assistantGoalAutopilotTone,
    assistantGoalExecutionLabel,
    assistantGoalKindLabel,
    assistantGoalKindShortLabel,
    assistantGoalStatusLabel,
    assistantGoalStatusTone,
    assistantRouteLabel,
    assistantRunActionCount,
    assistantRunActionStatusLabel,
    assistantRunActionStatusTone,
    assistantRunDecisionLabel,
    assistantRunsForView,
    assistantRunView,
    assistantRunStatusTone,
    assistantSignalStatusLabel,
    assistantSignalStatusTone,
    dueAssistantGoals,
    selectAssistantGoal,
    selectAssistantRun,
    type AssistantRunView
  } from './assistant-model';

  const apiBase = import.meta.env.VITE_HOMELABD_API_BASE || '/api';
	  const client = createHomelabdClient({ baseUrl: apiBase });
	  type MobilePanel = 'runs' | 'detail';
	  type DetailKind = 'goal' | 'run';
	  type GoalTargetMode = 'auto' | 'local' | 'remote';

	  let runs: AssistantRun[] = [];
	  let signals: AssistantSignalCandidate[] = [];
	  let goals: AssistantGoal[] = [];
	  let workspaces: HomelabdRemoteWorkspace[] = [];
  let selectedGoalId = '';
  let selectedGoal: AssistantGoal | undefined;
  let selectedGoalTimeline: AssistantGoalTimeline | undefined;
  let selectedRunId = '';
  let selectedRun: AssistantRun | undefined;
  let runsLoading = true;
  let runStarting = false;
  let goalCreating = false;
  let goalChecking = false;
  let goalAutopilotUpdating = false;
  let runArchiving = false;
  let signalUpdating: string[] = [];
  let actionUpdating: string[] = [];
  let runsError = '';
  let runNotice = '';
  let signalNotice = '';
  let goalNotice = '';
  let goalError = '';
  let lastSynced = '';
  let detailEl: HTMLElement | undefined;
  let mobilePanel: MobilePanel = 'runs';
  let selectedDetailKind: DetailKind = 'run';
  let runView: AssistantRunView = 'active';
  let activeRuns: AssistantRun[] = [];
  let archivedRuns: AssistantRun[] = [];
  let visibleRuns: AssistantRun[] = [];
  let displayedActions: AssistantRunAction[] = [];
	  let activeSignals: AssistantSignalCandidate[] = [];
	  let activeGoals: AssistantGoal[] = [];
	  let dueGoals: AssistantGoal[] = [];
	  let onlineWorkspaceItems: HomelabdRemoteWorkspace[] = [];
	  let selectedGoalWorkspace: HomelabdRemoteWorkspace | undefined;
	  let goalFormOpen = false;
  let goalTitle = '';
  let goalObjective = '';
  let goalKind = 'build';
  let goalExecutionMode = 'guided';
  let goalAutopilotBudget = 4;
	  let goalCadence = 'daily';
	  let goalAutonomy = 'observe';
	  let goalDetails = '';
	  let goalTargetMode: GoalTargetMode = 'auto';
	  let goalTargetWorkspaceId = '';
	  let lastAppliedRouteRunId = '';
  let pendingRouteRunId = '';
  let pendingOverviewRoute = false;

  $: activeRuns = assistantRunsForView(runs, 'active');
  $: archivedRuns = assistantRunsForView(runs, 'archived');
  $: visibleRuns = runView === 'archived' ? archivedRuns : activeRuns;
  $: selectedRun = selectAssistantRun(runs, selectedRunId);
  $: selectedGoal = selectAssistantGoal(goals, selectedGoalId);
  $: displayedActions = visibleRecommendedActions(selectedRun);
  $: openRunActions = activeRuns.reduce((total, run) => total + runOpenActionCount(run), 0);
	  $: activeSignals = signals.filter((signal) => !signal.suppressed && !signal.created_task_id);
	  $: activeGoals = activeAssistantGoals(goals);
	  $: dueGoals = dueAssistantGoals(goals);
	  $: onlineWorkspaceItems = workspaces.filter((workspace) => workspace.status !== 'offline');
	  $: if (
	    workspaces.length &&
	    !workspaces.some((workspace) => workspace.id === goalTargetWorkspaceId)
	  ) {
	    goalTargetWorkspaceId = onlineWorkspaceItems[0]?.id || workspaces[0].id;
	  }
	  $: selectedGoalWorkspace =
	    workspaces.find((workspace) => workspace.id === goalTargetWorkspaceId) ||
	    onlineWorkspaceItems[0] ||
	    workspaces[0];
	  $: runSpaces = [
    {
      id: 'active' as AssistantRunView,
      label: 'Active',
      count: activeRuns.length,
      detail: openRunActions ? plural(openRunActions, 'open decision') : 'No open decisions'
    },
    {
      id: 'archived' as AssistantRunView,
      label: 'Archived',
      count: archivedRuns.length,
      detail: 'Stored for review'
    }
  ];

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
	      const [runResponse, signalResponse, goalResponse, workspaceResponse] = await Promise.all([
	        client.listAssistantRuns({ archived: 'include' }),
	        client.listAssistantSignals(),
	        client.listAssistantGoals(),
	        client.listWorkspaces()
	      ]);
	      runs = runResponse.runs || [];
	      signals = signalResponse.signals || [];
	      goals = goalResponse.goals || [];
	      workspaces = workspaceResponse.workspaces || [];
      applySyncedRunSelection();
      await refreshSelectedGoalAfterGoalList();
      lastSynced = syncTimeLabel();
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to load proactive Assistant runs.';
    } finally {
      runsLoading = false;
    }
  };

  const refreshSelectedGoalAfterGoalList = async () => {
    const due = dueAssistantGoals(goals);
    const active = activeAssistantGoals(goals);
    const nextGoalId =
      selectedGoalId && goals.some((goal) => goal.id === selectedGoalId)
        ? selectedGoalId
        : due[0]?.id || active[0]?.id || goals[0]?.id || '';
    selectedGoalId = nextGoalId;
    if (!nextGoalId) {
      selectedGoalTimeline = undefined;
      return;
    }
    try {
      selectedGoalTimeline = await client.getAssistantGoal(nextGoalId);
    } catch {
      selectedGoalTimeline = undefined;
    }
  };

  const refreshGoalTimeline = async (goalId = selectedGoalId) => {
    if (!goalId) {
      selectedGoalTimeline = undefined;
      return;
    }
    try {
      selectedGoalTimeline = await client.getAssistantGoal(goalId);
    } catch (err) {
      goalError = err instanceof Error ? err.message : 'Unable to load Goal details.';
    }
  };

  const currentRunRouteId = () => (browser ? $page.url.searchParams.get('run') || '' : '');
  const currentRunRouteView = (): AssistantRunView =>
    browser && $page.url.searchParams.get('view') === 'archived' ? 'archived' : 'active';

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

  const applyRouteRunSelection = (runId: string, preferredView: AssistantRunView = runView) => {
    if (!runId) {
      return;
    }
    const routedRun = runs.find((run) => run.id === runId);
    runView = routedRun ? assistantRunView(routedRun) : preferredView;
    selectedRunId = runId;
    selectedDetailKind = 'run';
    mobilePanel = 'detail';
    revealDetailIfCompact();
  };

  const applySyncedRunSelection = () => {
    const routeRunId = currentRunRouteId();
    const routeView = currentRunRouteView();
    runView = routeView;
    const routeRun = runs.find((run) => run.id === routeRunId);
    if (routeRunId && routeRun) {
      runView = assistantRunView(routeRun);
      selectedRunId = routeRunId;
      selectedDetailKind = 'run';
      lastAppliedRouteRunId = routeRunId;
      mobilePanel = 'detail';
      return;
    }
    const candidates = assistantRunsForView(runs, runView);
    if (selectedRunId && !candidates.some((run) => run.id === selectedRunId)) {
      selectedRunId = candidates[0]?.id || '';
    } else if (!selectedRunId) {
      selectedRunId = candidates[0]?.id || '';
    }
    if (!routeRunId) {
      lastAppliedRouteRunId = '';
    }
  };

  const navigateToRun = (runId: string, replaceState = false, view: AssistantRunView = runView) => {
    if (!browser || !runId) {
      return;
    }
    const routedRun = runs.find((run) => run.id === runId);
    const nextView = routedRun ? assistantRunView(routedRun) : view;
    const next = assistantRunURL(runId, nextView);
    if (currentRoutePath() === next) {
      return;
    }
    runView = nextView;
    pendingOverviewRoute = false;
    pendingRouteRunId = runId;
    void goto(next, { keepFocus: true, noScroll: true, replaceState }).catch(() => {
      if (pendingRouteRunId === runId) {
        pendingRouteRunId = '';
      }
    });
  };

  const navigateToRunOverview = (replaceState = true, view: AssistantRunView = runView) => {
    runView = view;
    showRunList();
    const next = assistantRunsURL(view);
    if (!browser || currentRoutePath() === next) {
      pendingOverviewRoute = false;
      pendingRouteRunId = '';
      lastAppliedRouteRunId = '';
      return;
    }
    pendingOverviewRoute = true;
    pendingRouteRunId = '';
    lastAppliedRouteRunId = '';
    void goto(next, { keepFocus: true, noScroll: true, replaceState }).catch(() => {
      pendingOverviewRoute = false;
    });
  };

  const selectRun = (runId: string, replaceState = false, view: AssistantRunView = runView) => {
    applyRouteRunSelection(runId, view);
    navigateToRun(runId, replaceState, view);
  };

  const setRunView = (view: AssistantRunView) => {
    runView = view;
    selectedDetailKind = 'run';
    const candidates = assistantRunsForView(runs, view);
    selectedRunId = candidates[0]?.id || '';
    navigateToRunOverview(false, view);
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
    selectRun(runId, false, runView);
  };

	  const labelFromSlug = (value: unknown) =>
	    String(value || '')
	      .replaceAll('_', ' ')
	      .replaceAll('-', ' ') || 'unknown';

	  const workspaceLabel = (workspace?: HomelabdRemoteWorkspace) => {
	    if (!workspace) {
	      return 'No remote project';
	    }
	    return workspace.project_id || workspace.label || workspace.workdir_id || workspace.workdir;
	  };

	  const workspaceDetail = (workspace?: HomelabdRemoteWorkspace) => {
	    if (!workspace) {
	      return 'No remote project selected';
	    }
	    const location = [workspace.agent_name || workspace.agent_id, workspace.machine].filter(Boolean).join(' on ');
	    return [location, workspace.workdir].filter(Boolean).join(' / ');
	  };

	  const targetLabel = (target?: HomelabdTaskTarget) => {
	    if (!target || target.mode === 'auto') {
	      return target?.project_id ? `Auto route / ${target.project_id}` : 'Auto route';
	    }
	    if (target.mode === 'local') {
	      return 'Local homelabd';
	    }
	    return [target.project_id || 'Remote project', target.agent_id, target.workdir || target.workdir_id]
	      .filter(Boolean)
	      .join(' / ');
	  };

	  const goalTargetFromForm = (): HomelabdTaskTarget | undefined => {
	    if (goalTargetMode === 'local') {
	      return { mode: 'local' };
	    }
	    if (goalTargetMode === 'remote' && selectedGoalWorkspace) {
	      return {
	        mode: 'remote',
	        project_id: selectedGoalWorkspace.project_id,
	        agent_id: selectedGoalWorkspace.agent_id,
	        machine: selectedGoalWorkspace.machine,
	        workdir_id: selectedGoalWorkspace.workdir_id,
	        workdir: selectedGoalWorkspace.workdir,
	        repo_url: selectedGoalWorkspace.repo_url,
	        branch: selectedGoalWorkspace.branch,
	        labels: selectedGoalWorkspace.labels,
	        backend: selectedGoalWorkspace.backend
	      };
	    }
	    return { mode: 'auto' };
	  };

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
    ['created_task', 'dismissed', 'snoozed', 'useful', 'skipped', 'failed'].includes(action.status || '');

  const runOpenActionCount = (run: AssistantRun | undefined) =>
    (run?.recommended_actions || []).filter((action) => !actionIsSettled(action)).length;

  const visibleRecommendedActions = (run: AssistantRun | undefined) =>
    run?.archived
      ? run.recommended_actions || []
      : (run?.recommended_actions || []).filter((action) => !actionIsSettled(action));

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

  const runHasResolvedActions = (run: AssistantRun | undefined) =>
    Boolean((run?.recommended_actions || []).length && runOpenActionCount(run) === 0);

  const runDecisionTone = (run: AssistantRun | undefined) => {
    if (!run || run.status === 'failed' || run.error) {
      return 'red';
    }
    if (run.archived) {
      return 'gray';
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
    if (run.archived) {
      return 'Archived decision';
    }
    const open = runOpenActionCount(run);
    if (open > 0) {
      return `${open} ${open === 1 ? 'recommendation' : 'recommendations'} to decide`;
    }
    if (runHasCreatedTask(run)) {
      return 'Recommendation acted on';
    }
    if (runHasResolvedActions(run)) {
      return 'Recommendation resolved';
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
    if (run.archived) {
      return run.archived_reason
        ? `Archived: ${run.archived_reason}`
        : 'Stored outside the active decision queue. Restore it if the Assistant should surface it again.';
    }
    if (runOpenActionCount(run) > 0) {
      return 'Review the evidence, then create work, mark useful, snooze, or dismiss the recommendation.';
    }
    if (runHasCreatedTask(run)) {
      return 'A task was created from this signal. Open it from the recommendation receipt when you need the work record.';
    }
    if (runHasResolvedActions(run)) {
      return 'No operator action remains. The result is kept with receipts for audit.';
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

  const signalMeta = (signal: AssistantSignalCandidate) =>
    [
      signal.source || signal.surface,
      signal.kind,
      signal.score ? `${signal.score} score` : '',
      signal.seen_count && signal.seen_count > 1 ? `${signal.seen_count} sightings` : ''
    ]
      .filter(Boolean)
      .map(labelFromSlug)
      .join(' / ');

  const signalEvidenceSummary = (signal: AssistantSignalCandidate) =>
    signal.evidence?.[0]?.detail || signal.evidence?.[0]?.title || signal.why_now || signal.detail || '';

  const goalSubtitle = (goal: AssistantGoal) =>
    [
      assistantGoalKindShortLabel(goal.kind),
      assistantGoalExecutionLabel(goal.execution_mode),
      assistantGoalStatusLabel(goal.status),
      goal.cadence ? labelFromSlug(goal.cadence) : '',
      goal.next_check_at ? `next ${formatAssistantTime(goal.next_check_at)}` : 'due'
    ]
      .filter(Boolean)
      .join(' / ');

  const goalAutopilotStatus = (goal: AssistantGoal | undefined) =>
    goal?.autopilot?.status || (goal?.execution_mode === 'autopilot' ? 'ready' : '');

  const goalAutopilotBudgetLabel = (goal: AssistantGoal | undefined) => {
    const autopilot = goal?.autopilot;
    if (!autopilot) {
      return 'Guided';
    }
    const started = autopilot.tasks_started || 0;
    const budget = autopilot.budget_tasks || 1;
    return `${started}/${budget} tasks`;
  };

  const goalProgress = (goal: AssistantGoal | undefined) =>
    goal?.progress_summary || goal?.objective || 'Goal is waiting for its first Assistant assessment.';

  const goalTimelineCount = (kind: 'watches' | 'notes' | 'assessments') =>
    selectedGoalTimeline?.[kind]?.length || 0;

  const compilerStatusTone = (run: AssistantRun | undefined) => {
    switch (run?.compiler?.status) {
      case 'fallback':
        return 'amber';
      case 'repaired':
        return 'blue';
      case 'accepted':
        return 'green';
      default:
        return 'gray';
    }
  };

  const compilerTitle = (run: AssistantRun | undefined) => {
    switch (run?.compiler?.status) {
      case 'fallback':
        return 'Deterministic fallback';
      case 'repaired':
        return 'Repaired decision';
      case 'accepted':
        return 'Accepted decision';
      default:
        return 'Decision compiler';
    }
  };

  const compilerDetail = (run: AssistantRun | undefined) =>
    run?.compiler?.summary || 'Harness checks constrained the model output before this decision was stored.';

  const compilerMessages = (run: AssistantRun | undefined) =>
    [...(run?.compiler?.rejections || []), ...(run?.compiler?.repairs || [])].slice(0, 3);

  const compilerScoreSummary = (run: AssistantRun | undefined) => {
    const scorecard = run?.compiler?.scorecard;
    if (!scorecard) {
      return '';
    }
    return `${scorecard.score}/100 ${scorecard.grade || 'score'} / ${scorecard.kept_action_count || 0} kept / ${scorecard.rejected_action_count || 0} rejected / ${scorecard.plan_preview_count || 0} plans`;
  };

  const compilerPolicySummary = (run: AssistantRun | undefined) => {
    const hint = run?.compiler?.policy_hints?.[0];
    if (!hint) {
      return '';
    }
    return hint.reason || [hint.effect, hint.source].filter(Boolean).map(labelFromSlug).join(' / ');
  };

  const signalMutationKey = (signal: AssistantSignalCandidate) => signal.fingerprint || signal.id || signal.title;

  const isSignalUpdating = (signal: AssistantSignalCandidate) =>
    signalUpdating.includes(signalMutationKey(signal));

  const signalAllows = (signal: AssistantSignalCandidate, action: string) =>
    !signal.safe_actions?.length ||
    signal.safe_actions.some((value) => value.toLowerCase() === action.toLowerCase());

  const actionSupportText = (action: AssistantRunAction) =>
    action.task_goal || action.knowledge_query || action.workflow_hint || '';

  const actionContractSummary = (action: AssistantRunAction) => {
    const contract = action.contract;
    if (!contract) {
      return action.contract_id ? `Contract: ${labelFromSlug(action.contract_id)}` : '';
    }
    const parts = [
      labelFromSlug(contract.id || action.contract_id || contract.action_kind || action.kind),
      contract.requires_approval ? 'approval required' : 'bounded',
      contract.risk ? `${labelFromSlug(contract.risk)} risk` : ''
    ].filter(Boolean);
    return parts.join(' / ');
  };

  const actionPlanSummary = (action: AssistantRunAction) => {
    if (!action.plan) {
      return '';
    }
    return action.plan.summary || labelFromSlug(action.plan.status || 'planned');
  };

  const actionPlanTone = (action: AssistantRunAction) => {
    switch (action.plan?.status) {
      case 'executed':
        return 'green';
      case 'blocked':
        return 'amber';
      case 'approval_required':
        return 'blue';
      default:
        return 'gray';
    }
  };

  const startProactiveRun = async () => {
    runStarting = true;
    runsError = '';
    runNotice = '';
    signalNotice = '';
    try {
      const response = await client.startAssistantRun({
        trigger_kind: 'manual',
        trigger_label: 'Operator requested proactive check',
        goal: 'Review current homelabd state and recommend useful next actions.',
        autonomy: 'propose'
      });
      runs = [response.run, ...runs.filter((run) => run.id !== response.run.id)];
      selectRun(response.run.id, false, 'active');
      runNotice = response.reply || 'Assistant proactive check completed.';
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to start proactive Assistant run.';
    } finally {
      runStarting = false;
    }
  };

  const selectGoal = async (goalId: string) => {
    if (!goalId) {
      return;
    }
    selectedGoalId = goalId;
    selectedDetailKind = 'goal';
    mobilePanel = 'detail';
    goalNotice = '';
    goalError = '';
    await refreshGoalTimeline(goalId);
    revealDetailIfCompact();
  };

  const createGoal = async () => {
    const objective = goalObjective.trim();
    const title = goalTitle.trim() || objective;
    if (!objective || !title) {
      goalError = 'Goal title or objective is required.';
      return;
    }
    goalCreating = true;
    goalError = '';
    goalNotice = '';
    try {
      const timeline = await client.createAssistantGoal({
        title,
        objective,
        details: goalDetails.trim() || undefined,
        kind: goalKind,
        execution_mode: goalExecutionMode,
        autopilot:
          goalExecutionMode === 'autopilot'
            ? {
                status: 'ready',
                budget_tasks: Math.max(1, goalAutopilotBudget || 1)
              }
            : undefined,
	        cadence: goalCadence.trim() || undefined,
	        autonomy: goalAutonomy.trim() || undefined,
	        target: goalTargetFromForm(),
	        created_by: 'dashboard'
	      });
      goals = [timeline.goal, ...goals.filter((goal) => goal.id !== timeline.goal.id)];
      selectedGoalId = timeline.goal.id;
      selectedGoalTimeline = timeline;
      selectedDetailKind = 'goal';
      goalFormOpen = false;
      goalTitle = '';
      goalObjective = '';
	      goalDetails = '';
	      goalKind = 'build';
	      goalExecutionMode = 'guided';
	      goalAutopilotBudget = 4;
	      goalTargetMode = 'auto';
	      goalNotice = `${assistantGoalKindLabel(timeline.goal.kind)} created in ${assistantGoalExecutionLabel(timeline.goal.execution_mode)} mode.`;
      mobilePanel = 'detail';
    } catch (err) {
      goalError = err instanceof Error ? err.message : 'Unable to create Goal.';
    } finally {
      goalCreating = false;
    }
  };

  const checkSelectedGoal = async () => {
    if (!selectedGoalId) {
      return;
    }
    goalChecking = true;
    goalError = '';
    goalNotice = '';
    try {
      const response = await client.checkAssistantGoal(selectedGoalId);
      runs = [response.run, ...runs.filter((run) => run.id !== response.run.id)];
      await refreshGoalTimeline(selectedGoalId);
      selectRun(response.run.id, false, 'active');
      goalNotice = response.reply || 'Goal check completed.';
    } catch (err) {
      goalError = err instanceof Error ? err.message : 'Unable to check Goal.';
    } finally {
      goalChecking = false;
    }
  };

  const updateSelectedGoalStatus = async (status: string) => {
    if (!selectedGoalId) {
      return;
    }
    goalChecking = true;
    goalError = '';
    goalNotice = '';
    try {
      const timeline = await client.updateAssistantGoal(selectedGoalId, { status });
      goals = goals.map((goal) => (goal.id === timeline.goal.id ? timeline.goal : goal));
      selectedGoalTimeline = timeline;
      goalNotice = `Goal ${assistantGoalStatusLabel(status).toLowerCase()}.`;
    } catch (err) {
      goalError = err instanceof Error ? err.message : 'Unable to update Goal.';
    } finally {
      goalChecking = false;
    }
  };

  const updateSelectedGoalAutopilot = async (action: 'start' | 'pause' | 'resume' | 'stop') => {
    if (!selectedGoalId) {
      return;
    }
    goalAutopilotUpdating = true;
    goalError = '';
    goalNotice = '';
    try {
      const request =
        action === 'start' || action === 'resume'
          ? { budget_tasks: Math.max(1, selectedGoal?.autopilot?.budget_tasks || goalAutopilotBudget || 1) }
          : {};
      const response = await client.updateAssistantGoalAutopilot(selectedGoalId, action, request);
      const timeline = response.timeline;
      goals = goals.map((goal) => (goal.id === timeline.goal.id ? timeline.goal : goal));
      selectedGoalTimeline = timeline;
      goalNotice =
        response.reply ||
        `Autopilot ${assistantGoalAutopilotStatusLabel(goalAutopilotStatus(timeline.goal)).toLowerCase()}.`;
    } catch (err) {
      goalError = err instanceof Error ? err.message : 'Unable to update Autopilot.';
    } finally {
      goalAutopilotUpdating = false;
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
    signalNotice = '';
    try {
      const response = await client.updateAssistantRunAction(selectedRun.id, action.id, {
        feedback,
        snooze_seconds: feedback === 'snooze' ? snoozeSeconds : undefined
      });
      runs = runs.map((run) => (run.id === response.run.id ? response.run : run));
      selectedRunId = response.run.id;
      mobilePanel = 'detail';
      navigateToRun(response.run.id, true, assistantRunView(response.run));
      runNotice = response.reply;
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to update Assistant recommendation.';
    } finally {
      actionUpdating = actionUpdating.filter((value) => value !== key);
    }
  };

  const updateSelectedRunArchive = async (archived: boolean) => {
    if (!selectedRun) {
      return;
    }
    runArchiving = true;
    runsError = '';
    runNotice = '';
    signalNotice = '';
    try {
      const response = await client.updateAssistantRunArchive(selectedRun.id, {
        archived,
        actor: 'dashboard',
        reason: archived ? 'No longer required.' : undefined
      });
      runs = runs.map((run) => (run.id === response.run.id ? response.run : run));
      const nextView = assistantRunView(response.run);
      selectedRunId = response.run.id;
      mobilePanel = 'detail';
      navigateToRun(response.run.id, true, nextView);
      runNotice = response.reply;
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to update Assistant archive.';
    } finally {
      runArchiving = false;
    }
  };

  const updateSignal = async (
    signal: AssistantSignalCandidate,
    feedback: 'useful' | 'dismiss' | 'snooze' | 'create_task'
  ) => {
    const key = signalMutationKey(signal);
    signalUpdating = [...signalUpdating, key];
    runsError = '';
    signalNotice = '';
    try {
      const response = await client.updateAssistantSignal(signal.fingerprint, {
        feedback,
        snooze_seconds: feedback === 'snooze' ? 24 * 60 * 60 : undefined
      });
      signals = signals.map((candidate) =>
        candidate.fingerprint === response.signal.fingerprint ? response.signal : candidate
      );
      signalNotice = response.reply || 'Signal updated.';
    } catch (err) {
      runsError = err instanceof Error ? err.message : 'Unable to update Assistant signal.';
    } finally {
      signalUpdating = signalUpdating.filter((value) => value !== key);
    }
  };

  afterNavigate(({ to }) => {
    if (!browser || to?.url.pathname !== '/assistant') {
      return;
    }
    const runId = to.url.searchParams.get('run') || '';
    const routeView: AssistantRunView = to.url.searchParams.get('view') === 'archived' ? 'archived' : 'active';
    if (!runId) {
      runView = routeView;
      const candidates = assistantRunsForView(runs, runView);
      if (selectedRunId && !candidates.some((run) => run.id === selectedRunId)) {
        selectedRunId = candidates[0]?.id || '';
      } else if (!selectedRunId) {
        selectedRunId = candidates[0]?.id || '';
      }
      pendingOverviewRoute = false;
      pendingRouteRunId = '';
      lastAppliedRouteRunId = '';
      showRunList();
      return;
    }
    pendingOverviewRoute = false;
    if (pendingRouteRunId === runId) {
      const pendingRun = runs.find((run) => run.id === runId);
      runView = pendingRun ? assistantRunView(pendingRun) : routeView;
      lastAppliedRouteRunId = runId;
      pendingRouteRunId = '';
      return;
    }
    if (runId === selectedRunId) {
      const selected = runs.find((run) => run.id === runId);
      runView = selected ? assistantRunView(selected) : routeView;
      lastAppliedRouteRunId = runId;
      mobilePanel = 'detail';
      return;
    }
    if (runs.some((run) => run.id === runId)) {
      applyRouteRunSelection(runId, routeView);
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
      applyRouteRunSelection(routeRunId, currentRunRouteView());
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
          <h1>
            {runView === 'archived'
              ? 'Archived decisions'
              : openRunActions
                ? plural(openRunActions, 'decision')
                : 'Ready to review'}
          </h1>
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
        <span><strong>{activeRuns.length}</strong> active</span>
        <span><strong>{archivedRuns.length}</strong> archived</span>
        <span><strong>{openRunActions}</strong> open</span>
        <span><strong>{activeSignals.length}</strong> signals</span>
      </div>

      <section class="goals-panel" aria-label="Assistant Goals">
        <header>
          <div>
            <h2>Goals</h2>
            <span>{activeGoals.length ? `${activeGoals.length} active / ${dueGoals.length} due` : 'No active Goals'}</span>
          </div>
          <button
            type="button"
            class="icon-action"
            aria-label={goalFormOpen ? 'Close Goal form' : 'Create Goal'}
            title={goalFormOpen ? 'Close Goal form' : 'Create Goal'}
            on:click={() => (goalFormOpen = !goalFormOpen)}
          >
            <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
              <path d="M10 4v12M4 10h12" />
            </svg>
          </button>
        </header>

        {#if goalNotice}
          <section class="notice success compact-notice" role="status" aria-live="polite">
            <div>
              <strong>Goal updated</strong>
              <p>{goalNotice}</p>
            </div>
            <button type="button" class="notice-dismiss" aria-label="Clear Goal notice" on:click={() => (goalNotice = '')}>
              <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                <path d="M6 6l8 8M14 6l-8 8" />
              </svg>
            </button>
          </section>
        {/if}

        {#if goalError}
          <section class="notice error compact-notice" role="alert">
            <div>
              <strong>Goal action failed</strong>
              <p>{goalError}</p>
            </div>
            <button type="button" class="notice-dismiss" aria-label="Clear Goal error" on:click={() => (goalError = '')}>
              <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                <path d="M6 6l8 8M14 6l-8 8" />
              </svg>
            </button>
          </section>
        {/if}

        {#if goalFormOpen}
          <form class="goal-form" aria-label="Create Assistant Goal" on:submit|preventDefault={() => void createGoal()}>
            <label>
              <span>Title</span>
              <input bind:value={goalTitle} autocomplete="off" placeholder="Daily brief" />
            </label>
            <label>
              <span>Objective</span>
              <textarea bind:value={goalObjective} rows="3" placeholder="Keep caring about this outcome over time."></textarea>
            </label>
            <div class="form-grid">
              <label>
                <span>Goal type</span>
                <select bind:value={goalKind}>
                  <option value="build">Build</option>
                  <option value="routine">Routine</option>
                  <option value="watch">Watch</option>
                  <option value="maintenance">Maintenance</option>
                </select>
              </label>
              <label>
                <span>Execution mode</span>
                <select bind:value={goalExecutionMode}>
                  <option value="guided">Guided</option>
                  <option value="autopilot">Autopilot</option>
                </select>
              </label>
              <label>
                <span>Cadence</span>
                <select bind:value={goalCadence}>
                  <option value="daily">Daily</option>
                  <option value="hourly">Hourly</option>
                  <option value="weekly">Weekly</option>
                  <option value="">Manual</option>
                </select>
              </label>
	              <label>
	                <span>Autonomy</span>
	                <select bind:value={goalAutonomy}>
	                  <option value="observe">Observe</option>
	                  <option value="propose">Propose</option>
	                  <option value="create_tasks">Create tasks</option>
	                </select>
	              </label>
	              <label>
	                <span>Target</span>
	                <select bind:value={goalTargetMode}>
	                  <option value="auto">Auto route</option>
	                  <option value="remote">Remote project</option>
	                  <option value="local">Local homelabd</option>
	                </select>
	              </label>
	              {#if goalTargetMode === 'remote'}
	                <label>
	                  <span>Project</span>
	                  <select bind:value={goalTargetWorkspaceId} disabled={!workspaces.length}>
	                    {#each workspaces as workspace}
	                      <option value={workspace.id}>
	                        {workspaceLabel(workspace)} / {workspace.agent_name || workspace.agent_id} / {workspace.status}
	                      </option>
	                    {/each}
	                  </select>
	                </label>
	              {/if}
	            </div>
	            {#if goalTargetMode === 'remote'}
	              <p class="goal-target-context">{workspaceDetail(selectedGoalWorkspace)}</p>
	            {/if}
            {#if goalExecutionMode === 'autopilot'}
              <label>
                <span>Autopilot task budget</span>
                <input bind:value={goalAutopilotBudget} type="number" min="1" max="20" inputmode="numeric" />
              </label>
            {/if}
	            <label>
	              <span>Details</span>
	              <textarea bind:value={goalDetails} rows="2" placeholder="Constraints, preferences, examples, and what done means."></textarea>
	            </label>
	            <button
	              type="submit"
	              class="primary-action"
	              disabled={goalCreating || !goalObjective.trim() || Boolean(goalTargetMode === 'remote' && !selectedGoalWorkspace)}
	            >
              {goalCreating ? 'Creating' : 'Create Goal'}
            </button>
          </form>
        {/if}

        {#if goals.length}
          <div class="goal-list">
            {#each goals.slice(0, 8) as goal}
              <button
                type="button"
                class:selected={selectedGoalId === goal.id}
                aria-pressed={selectedGoalId === goal.id}
                on:click={() => void selectGoal(goal.id)}
              >
                <span class={`dot ${assistantGoalStatusTone(goal.status)}`} aria-hidden="true"></span>
                <span>
                  <strong>{goal.title}</strong>
                  <span class="goal-chip-row" aria-label={`Goal type and mode for ${goal.title}`}>
                    <span class="status blue">{assistantGoalKindShortLabel(goal.kind)}</span>
	                    <span class={`status ${goal.execution_mode === 'autopilot' ? assistantGoalAutopilotTone(goalAutopilotStatus(goal)) : 'gray'}`}>
	                      {goal.execution_mode === 'autopilot'
	                        ? `Autopilot ${assistantGoalAutopilotStatusLabel(goalAutopilotStatus(goal))}`
	                        : 'Guided'}
	                    </span>
	                    <span class={`status ${goal.target?.mode === 'remote' ? 'green' : goal.target?.mode === 'local' ? 'gray' : 'blue'}`}>
	                      {targetLabel(goal.target)}
	                    </span>
	                  </span>
                  <small>{goalSubtitle(goal)}</small>
                  <em>{goalProgress(goal)}</em>
                </span>
              </button>
            {/each}
          </div>
        {:else if runsLoading}
          <p class="empty">Loading Goals...</p>
        {:else}
          <p class="empty">Create a Goal to give the Assistant something durable to pursue.</p>
        {/if}
      </section>

      <section class="run-spaces" aria-label="Assistant decision spaces">
        <h2>Decision spaces</h2>
        {#each runSpaces as space}
          <button
            type="button"
            class:active={runView === space.id}
            aria-pressed={runView === space.id}
            on:click={() => setRunView(space.id)}
          >
            <strong>{space.label}</strong>
            <span>{space.count} {space.count === 1 ? 'run' : 'runs'} / {space.detail}</span>
          </button>
        {/each}
      </section>

      <section class="signal-inbox" aria-label="Assistant signal inbox">
        <header>
          <div>
            <h2>Signal inbox</h2>
            <span>{activeSignals.length ? plural(activeSignals.length, 'active signal') : 'No active signals'}</span>
          </div>
        </header>
        {#if signalNotice}
          <section class="notice success" role="status" aria-live="polite" aria-label="Assistant signal status">
            <div>
              <strong>Signal updated</strong>
              <p>{signalNotice}</p>
            </div>
            <button
              type="button"
              class="notice-dismiss"
              aria-label="Clear Assistant signal notice"
              on:click={() => (signalNotice = '')}
            >
              <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                <path d="M6 6l8 8M14 6l-8 8" />
              </svg>
            </button>
          </section>
        {/if}
        {#if activeSignals.length}
          <div class="signal-inbox-list">
            {#each activeSignals.slice(0, 6) as signal}
              <article class="signal-inbox-row">
                <span class={`dot ${assistantSignalStatusTone(signal)}`} aria-hidden="true"></span>
                <div>
                  <strong>{signal.title}</strong>
                  <small>{signalMeta(signal)}</small>
                  {#if signalEvidenceSummary(signal)}
                    <p>{signalEvidenceSummary(signal)}</p>
                  {/if}
                  {#if signal.suppression_reason}
                    <em>{signal.suppression_reason}</em>
                  {/if}
                  <div class="signal-toolbar" role="group" aria-label={`Signal actions for ${signal.title}`}>
                    <span class={`status ${assistantSignalStatusTone(signal)}`}>
                      {assistantSignalStatusLabel(signal)}
                    </span>
                    {#if signalAllows(signal, 'create_task')}
                      <button
                        type="button"
                        class="text-action"
                        disabled={isSignalUpdating(signal) || Boolean(signal.created_task_id)}
                        on:click={() => void updateSignal(signal, 'create_task')}
                      >
                        Follow up
                      </button>
                    {/if}
                    <button
                      type="button"
                      class="text-action"
                      disabled={isSignalUpdating(signal)}
                      on:click={() => void updateSignal(signal, 'useful')}
                    >
                      Useful
                    </button>
                    <button
                      type="button"
                      class="text-action"
                      disabled={isSignalUpdating(signal)}
                      on:click={() => void updateSignal(signal, 'snooze')}
                    >
                      Snooze
                    </button>
                    <button
                      type="button"
                      class="danger-action"
                      disabled={isSignalUpdating(signal)}
                      on:click={() => void updateSignal(signal, 'dismiss')}
                    >
                      Dismiss
                    </button>
                  </div>
                </div>
              </article>
            {/each}
          </div>
        {:else}
          <p class="empty">Signals from chat, tasks, health, workflows, and future sources will appear here.</p>
        {/if}
      </section>

      {#if runsError}
        <section class="notice error" role="alert" aria-label="Assistant sync error">
          <div>
            <strong>Assistant sync failed</strong>
            <p>{runsError}</p>
          </div>
          <button
            type="button"
            class="notice-dismiss"
            aria-label="Clear Assistant sync error"
            on:click={() => (runsError = '')}
          >
            <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
              <path d="M6 6l8 8M14 6l-8 8" />
            </svg>
          </button>
        </section>
      {/if}

      <section class="run-list" aria-label={runView === 'archived' ? 'Archived Assistant runs' : 'Active Assistant runs'}>
        {#if runsLoading}
          <p class="empty">Loading proactive runs...</p>
        {:else if visibleRuns.length}
          {#each visibleRuns as run}
            <a
              href={assistantRunURL(run.id, assistantRunView(run))}
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
            {#if runView === 'archived'}
              <p>No archived decisions yet.</p>
            {:else}
              <p>No active proactive runs.</p>
              <button type="button" class="text-action" on:click={() => void startProactiveRun()}>
                Run first check
              </button>
            {/if}
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
      aria-label="Assistant workbench"
      bind:this={detailEl}
    >
      {#if selectedGoal && selectedDetailKind === 'goal'}
        <article class="assistant-record goal-record" aria-label="Selected Assistant Goal">
          <header class="record-header">
            <button type="button" class="back-to-runs" aria-label="Back to Goal list" on:click={() => showRunList()}>
              <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                <path d="M12.5 4.5 7 10l5.5 5.5" />
              </svg>
              <span>Back</span>
            </button>
            <div>
              <p>{assistantGoalKindLabel(selectedGoal.kind)} / {assistantGoalExecutionLabel(selectedGoal.execution_mode)} mode</p>
              <h2>{selectedGoal.title}</h2>
              <span>{goalProgress(selectedGoal)}</span>
            </div>
            <div class="record-actions">
              <span class={`status ${assistantGoalStatusTone(selectedGoal.status)}`}>
                {assistantGoalStatusLabel(selectedGoal.status)}
              </span>
              {#if selectedGoal.execution_mode === 'autopilot'}
                <span class={`status ${assistantGoalAutopilotTone(goalAutopilotStatus(selectedGoal))}`}>
                  Autopilot {assistantGoalAutopilotStatusLabel(goalAutopilotStatus(selectedGoal))}
                </span>
              {:else}
                <span class="status gray">Guided</span>
              {/if}
              <button
                type="button"
                class="primary-action"
                disabled={goalChecking}
                on:click={() => void checkSelectedGoal()}
              >
                {goalChecking ? 'Checking' : 'Check now'}
              </button>
              {#if selectedGoal.execution_mode === 'autopilot'}
                {#if goalAutopilotStatus(selectedGoal) === 'running'}
                  <button
                    type="button"
                    class="text-action"
                    disabled={goalAutopilotUpdating}
                    on:click={() => void updateSelectedGoalAutopilot('pause')}
                  >
                    Pause Autopilot
                  </button>
                {:else if goalAutopilotStatus(selectedGoal) === 'paused' || goalAutopilotStatus(selectedGoal) === 'blocked'}
                  <button
                    type="button"
                    class="primary-action"
                    disabled={goalAutopilotUpdating}
                    on:click={() => void updateSelectedGoalAutopilot('resume')}
                  >
                    Resume Autopilot
                  </button>
                {:else}
                  <button
                    type="button"
                    class="primary-action"
                    disabled={goalAutopilotUpdating || selectedGoal.status === 'archived'}
                    on:click={() => void updateSelectedGoalAutopilot('start')}
                  >
                    Start Autopilot
                  </button>
                {/if}
                <button
                  type="button"
                  class="text-action"
                  disabled={goalAutopilotUpdating || goalAutopilotStatus(selectedGoal) === 'stopped'}
                  on:click={() => void updateSelectedGoalAutopilot('stop')}
                >
                  Stop Autopilot
                </button>
              {/if}
              <button
                type="button"
                class="text-action"
                disabled={goalChecking || selectedGoal.status === 'paused'}
                on:click={() => void updateSelectedGoalStatus('paused')}
              >
                Pause
              </button>
              <button
                type="button"
                class="danger-action"
                disabled={goalChecking || selectedGoal.status === 'archived'}
                on:click={() => void updateSelectedGoalStatus('archived')}
              >
                Archive
              </button>
            </div>
          </header>

          <dl class="record-summary goal-summary" aria-label="Assistant Goal summary">
            <div>
              <dt>ID</dt>
              <dd>{shortAssistantId(selectedGoal.id)}</dd>
            </div>
            <div>
              <dt>Type</dt>
              <dd>{assistantGoalKindShortLabel(selectedGoal.kind)}</dd>
            </div>
            <div>
              <dt>Mode</dt>
              <dd>{assistantGoalExecutionLabel(selectedGoal.execution_mode)}</dd>
            </div>
            <div>
              <dt>Autopilot</dt>
              <dd>{selectedGoal.execution_mode === 'autopilot' ? `${assistantGoalAutopilotStatusLabel(goalAutopilotStatus(selectedGoal))} / ${goalAutopilotBudgetLabel(selectedGoal)}` : 'human in loop'}</dd>
            </div>
	            <div>
	              <dt>Autonomy</dt>
	              <dd>{labelFromSlug(selectedGoal.autonomy)}</dd>
	            </div>
	            <div>
	              <dt>Target</dt>
	              <dd>{targetLabel(selectedGoal.target)}</dd>
	            </div>
	            <div>
	              <dt>Cadence</dt>
	              <dd>{selectedGoal.cadence ? labelFromSlug(selectedGoal.cadence) : 'manual'}</dd>
	            </div>
            <div>
              <dt>Next</dt>
              <dd>{formatAssistantTime(selectedGoal.next_check_at) || 'due'}</dd>
            </div>
            <div>
              <dt>Tasks</dt>
              <dd>{selectedGoal.linked_tasks?.length || 0}</dd>
            </div>
            <div>
              <dt>Watches</dt>
              <dd>{goalTimelineCount('watches')}</dd>
            </div>
          </dl>

          <section class="route-strip goal-strip" aria-label="Goal objective">
            <span class="status blue">{assistantGoalKindShortLabel(selectedGoal.kind)}</span>
	            <span class={`status ${selectedGoal.execution_mode === 'autopilot' ? assistantGoalAutopilotTone(goalAutopilotStatus(selectedGoal)) : 'gray'}`}>
	              {selectedGoal.execution_mode === 'autopilot'
	                ? `Autopilot ${assistantGoalAutopilotStatusLabel(goalAutopilotStatus(selectedGoal))}`
	                : 'Guided'}
	            </span>
	            <span class={`status ${selectedGoal.target?.mode === 'remote' ? 'green' : selectedGoal.target?.mode === 'local' ? 'gray' : 'blue'}`}>
	              {targetLabel(selectedGoal.target)}
	            </span>
	            <div>
              <strong>{selectedGoal.objective}</strong>
              {#if selectedGoal.details}
                <p>{selectedGoal.details}</p>
              {/if}
            </div>
          </section>

          <div class="goal-detail-grid">
            <section class="detail-section" aria-label="Goal watches">
              <header class="section-heading">
                <div>
                  <p>Attention</p>
                  <h3>Watches</h3>
                </div>
                <span>{plural(goalTimelineCount('watches'), 'watch', 'watches')}</span>
              </header>
              {#if selectedGoalTimeline?.watches?.length}
                <div class="record-list">
                  {#each selectedGoalTimeline.watches.slice(0, 4) as watch}
                    <article class="signal-card">
                      <span class={`dot ${assistantGoalStatusTone(watch.status)}`} aria-hidden="true"></span>
                      <div>
                        <strong>{watch.title}</strong>
                        <p>{watch.suggested_action || watch.condition || 'Stored watch.'}</p>
                        <small>{[watch.source, watch.cadence, watch.severity].filter(Boolean).map(labelFromSlug).join(' / ')}</small>
                      </div>
                    </article>
                  {/each}
                </div>
              {:else}
                <p>No watches recorded for this Goal.</p>
              {/if}
            </section>

            <section class="detail-section" aria-label="Goal recent notes">
              <header class="section-heading">
                <div>
                  <p>Memory</p>
                  <h3>Notes</h3>
                </div>
                <span>{plural(goalTimelineCount('notes'), 'note')}</span>
              </header>
              {#if selectedGoalTimeline?.notes?.length}
                <ol class="receipt-list compact-receipts">
                  {#each selectedGoalTimeline.notes.slice(0, 4) as note}
                    <li>
                      <strong>{note.title || labelFromSlug(note.kind || 'note')}</strong>
                      <span>{formatAssistantTime(note.created_at)}</span>
                      <p>{note.body}</p>
                      {#if note.task_id}
                        <a href={`/tasks?task=${note.task_id}`}>Open linked task</a>
                      {/if}
                    </li>
                  {/each}
                </ol>
              {:else}
                <p>No notes recorded for this Goal.</p>
              {/if}
            </section>
          </div>

          {#if selectedGoal.linked_tasks?.length}
            <section class="detail-section" aria-label="Goal linked tasks">
              <header class="section-heading">
                <div>
                  <p>Execution</p>
                  <h3>Linked tasks</h3>
                </div>
                <span>{plural(selectedGoal.linked_tasks.length, 'task')}</span>
              </header>
              <div class="task-link-list">
                {#each selectedGoal.linked_tasks.slice(0, 8) as taskId}
                  <a href={`/tasks?task=${taskId}`}>
                    <strong>{shortAssistantId(taskId)}</strong>
                    <span>{selectedGoal.execution_mode === 'autopilot' ? 'Autopilot task' : 'Guided task'}</span>
                  </a>
                {/each}
              </div>
            </section>
          {/if}
        </article>
      {/if}

      {#if selectedRun && selectedDetailKind === 'run'}
        <article class="assistant-record" aria-label="Selected Assistant run">
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
            <div class="record-actions">
              <span class={`status ${selectedRun.archived ? 'gray' : assistantRunStatusTone(selectedRun.status)}`}>
                {selectedRun.archived ? 'archived' : labelFromSlug(selectedRun.status)}
              </span>
              <button
                type="button"
                class="text-action"
                disabled={runArchiving}
                aria-label={selectedRun.archived ? 'Restore Assistant decision' : 'Archive Assistant decision'}
                on:click={() => void updateSelectedRunArchive(!selectedRun.archived)}
              >
                {selectedRun.archived ? 'Restore' : 'Archive'}
              </button>
            </div>
          </header>

          {#if runNotice}
            <section class="notice success" role="status" aria-live="polite" aria-label="Assistant run status">
              <div>
                <strong>Assistant updated</strong>
                <p>{runNotice}</p>
              </div>
              <button
                type="button"
                class="notice-dismiss"
                aria-label="Clear Assistant run notice"
                on:click={() => (runNotice = '')}
              >
                <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                  <path d="M6 6l8 8M14 6l-8 8" />
                </svg>
              </button>
            </section>
          {/if}

          {#if runsError}
            <section class="notice error" role="alert" aria-label="Assistant action error">
              <div>
                <strong>Assistant action failed</strong>
                <p>{runsError}</p>
              </div>
              <button
                type="button"
                class="notice-dismiss"
                aria-label="Clear Assistant action error"
                on:click={() => (runsError = '')}
              >
                <svg viewBox="0 0 20 20" aria-hidden="true" focusable="false">
                  <path d="M6 6l8 8M14 6l-8 8" />
                </svg>
              </button>
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
                      selectedRun.archived ||
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
            <div>
              <dt>Route</dt>
              <dd>{assistantRouteLabel(selectedRun.route?.capability)}</dd>
            </div>
          </dl>

          {#if selectedRun.route}
            <section class="route-strip" aria-label="Assistant capability route">
              <span class="status blue">{assistantRouteLabel(selectedRun.route.capability)}</span>
              <div>
                <strong>{labelFromSlug(selectedRun.route.decision || 'selected')}</strong>
                <p>{selectedRun.route.next_step || selectedRun.route.reason || 'Assistant selected a harness capability for this decision.'}</p>
              </div>
              {#if selectedRun.route.requires_approval}
                <span class="status amber">approval needed</span>
              {/if}
            </section>
          {/if}

          {#if selectedRun.compiler}
            <section class="route-strip compiler-strip" aria-label="Assistant decision compiler">
              <span class={`status ${compilerStatusTone(selectedRun)}`}>{labelFromSlug(selectedRun.compiler.status || 'checked')}</span>
              <div>
                <strong>{compilerTitle(selectedRun)}</strong>
                <p>{compilerDetail(selectedRun)}</p>
                {#if compilerScoreSummary(selectedRun)}
                  <p class="compiler-score">{compilerScoreSummary(selectedRun)}</p>
                {/if}
                {#if compilerPolicySummary(selectedRun)}
                  <p class="compiler-policy">{compilerPolicySummary(selectedRun)}</p>
                {/if}
                {#if compilerMessages(selectedRun).length}
                  <ul class="compiler-list">
                    {#each compilerMessages(selectedRun) as message}
                      <li>{message}</li>
                    {/each}
                  </ul>
                {/if}
              </div>
            </section>
          {/if}

          {#if selectedRun.error}
            <section class="notice error" role={selectedRun.archived ? 'status' : 'alert'}>
              <div>
                <strong>Run failed</strong>
                <p>{selectedRun.error}</p>
              </div>
              {#if !selectedRun.archived}
                <button
                  type="button"
                  class="text-action notice-action"
                  disabled={runArchiving}
                  on:click={() => void updateSelectedRunArchive(true)}
                >
                  Archive
                </button>
              {/if}
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
            {#if displayedActions.length}
              <div class="recommendation-list">
                {#each displayedActions as action}
                  <article class="recommendation-card">
                    <header>
                      <div>
                        <strong>{action.title}</strong>
                        <small>{actionMeta(action)}</small>
                      </div>
	                      <span class={`status ${assistantRunActionStatusTone(action.status)}`}>
	                        {assistantRunActionStatusLabel(action.status)}
	                      </span>
	                      {#if action.target}
	                        <span class={`status ${action.target.mode === 'remote' ? 'green' : action.target.mode === 'local' ? 'gray' : 'blue'}`}>
	                          {targetLabel(action.target)}
	                        </span>
	                      {/if}
	                    </header>
                    <p>{action.rationale}</p>
                    {#if actionSupportText(action)}
                      <small class="action-support">{actionSupportText(action)}</small>
                    {/if}
                    {#if actionContractSummary(action)}
                      <small class="action-support">{actionContractSummary(action)}</small>
                    {/if}
                    {#if actionPlanSummary(action)}
                      <div class="plan-preview">
                        <span class={`status ${actionPlanTone(action)}`}>{labelFromSlug(action.plan?.status || 'planned')}</span>
                        <small>{actionPlanSummary(action)}</small>
                      </div>
                    {/if}
                    {#if action.created_task_id}
                      <a href={`/tasks?task=${action.created_task_id}`}>Open created task</a>
                    {/if}
                    <div class="action-toolbar" role="group" aria-label={`Recommendation actions for ${action.title}`}>
                      {#if action.kind === 'task'}
                        <button
                          type="button"
                          class="text-action"
                          disabled={isActionUpdating(action) ||
                            Boolean(action.created_task_id) ||
                            selectedRun.archived ||
                            actionIsSettled(action)}
                          on:click={() => void updateSelectedRunAction(action, 'create_task')}
                        >
                          Create task
                        </button>
                      {/if}
                      <button
                        type="button"
                        class="text-action"
                        disabled={isActionUpdating(action) || selectedRun.archived || actionIsSettled(action)}
                        on:click={() => void updateSelectedRunAction(action, 'useful')}
                      >
                        Useful
                      </button>
                      <button
                        type="button"
                        class="text-action"
                        disabled={isActionUpdating(action) ||
                          selectedRun.archived ||
                          action.status === 'snoozed' ||
                          actionIsSettled(action)}
                        on:click={() => void updateSelectedRunAction(action, 'snooze')}
                      >
                        Snooze
                      </button>
                      <button
                        type="button"
                        class="danger-action"
                        disabled={isActionUpdating(action) ||
                          selectedRun.archived ||
                          action.status === 'dismissed' ||
                          actionIsSettled(action)}
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
      "goals"
      "spaces"
      "notice"
      "list"
      "reference";
    grid-template-rows: auto auto auto auto auto minmax(0, 1fr) auto;
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
  .record-actions,
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
  .record-actions,
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
  .run-spaces button,
  .goal-list button,
  .icon-action,
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
  .run-spaces button:hover,
  .goal-list button:hover,
  .icon-action:hover,
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
  .icon-action svg,
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

  .run-spaces {
    grid-area: spaces;
    display: grid;
    gap: 0.45rem;
  }

  .goals-panel {
    grid-area: goals;
    display: grid;
    gap: 0.55rem;
    min-width: 0;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
  }

  .goals-panel header,
  .goal-list button,
  .goal-list button > span:last-child {
    display: flex;
    min-width: 0;
  }

  .goals-panel header {
    align-items: center;
    justify-content: space-between;
    gap: 0.55rem;
  }

  .goals-panel h2 {
    color: var(--text, #172033);
    font-size: 0.84rem;
  }

  .goals-panel header span,
  .goal-list small,
  .goal-list em,
  .goal-form span {
    color: var(--assistant-muted, #475569);
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .icon-action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 2.45rem;
    padding: 0;
  }

  .goal-form {
    display: grid;
    gap: 0.55rem;
  }

  .goal-form label {
    display: grid;
    gap: 0.25rem;
    min-width: 0;
  }

  .goal-form input,
  .goal-form textarea,
  .goal-form select {
    width: 100%;
    min-width: 0;
    box-sizing: border-box;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 8px;
    padding: 0.55rem 0.6rem;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
    font: inherit;
    font-size: 0.84rem;
  }

	  .goal-form textarea {
	    resize: vertical;
	  }

	  .goal-target-context {
	    margin: -0.1rem 0 0;
	    overflow-wrap: anywhere;
	    color: var(--assistant-muted, #475569);
	    font-size: 0.76rem;
	    line-height: 1.35;
	  }

	  .form-grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 0.55rem;
  }

  .goal-list {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
  }

  .goal-list button {
    align-items: flex-start;
    gap: 0.55rem;
    min-height: 4rem;
    padding: 0.55rem 0.65rem;
    text-align: left;
  }

  .goal-list button.selected {
    border-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .goal-list button > span:last-child {
    flex-direction: column;
    gap: 0.15rem;
  }

  .goal-chip-row {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    min-width: 0;
  }

  .goal-list strong {
    color: var(--text-strong, #0f172a);
    font-size: 0.84rem;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }

  .goal-list em {
    display: -webkit-box;
    overflow: hidden;
    font-style: normal;
    line-clamp: 2;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
  }

  .run-spaces h2 {
    margin: 0;
    color: var(--text, #172033);
    font-size: 0.84rem;
  }

  .run-spaces button {
    display: grid;
    gap: 0.15rem;
    min-height: 3rem;
    padding: 0.55rem 0.65rem;
    text-align: left;
  }

  .run-spaces button.active {
    border-color: var(--accent, #2563eb);
    color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .run-spaces strong {
    color: var(--text-strong, #0f172a);
    font-size: 0.84rem;
  }

  .run-spaces span {
    color: var(--assistant-muted, #475569);
    font-size: 0.72rem;
    line-height: 1.25;
  }

  .signal-inbox {
    display: grid;
    gap: 0.55rem;
    min-width: 0;
    padding: 0.65rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
  }

  .signal-inbox header,
  .signal-toolbar,
  .route-strip {
    display: flex;
    align-items: center;
    gap: 0.55rem;
  }

  .signal-inbox header {
    justify-content: space-between;
  }

  .signal-inbox h2 {
    color: var(--text, #172033);
    font-size: 0.84rem;
  }

  .signal-inbox header span,
  .signal-inbox-row small,
  .signal-inbox-row p,
  .signal-inbox-row em,
  .route-strip p,
  .compiler-score,
  .compiler-policy,
  .compiler-list {
    color: var(--assistant-muted, #475569);
    font-size: 0.75rem;
    line-height: 1.3;
  }

  .signal-inbox-list {
    display: grid;
    gap: 0.5rem;
  }

  .signal-inbox-row {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr);
    gap: 0.5rem;
    min-width: 0;
    padding: 0.6rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface-muted, #f8fafc);
  }

  .signal-inbox-row > div,
  .route-strip > div {
    display: grid;
    gap: 0.22rem;
    min-width: 0;
  }

  .signal-inbox-row strong,
  .route-strip strong {
    color: var(--text-strong, #0f172a);
    overflow-wrap: anywhere;
  }

  .signal-inbox-row em {
    font-style: normal;
  }

  .compiler-strip {
    align-items: flex-start;
  }

  .goal-record {
    margin-bottom: 1rem;
  }

  .goal-strip {
    align-items: flex-start;
  }

  .goal-detail-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.85rem;
    min-width: 0;
  }

  .task-link-list {
    display: flex;
    flex-wrap: wrap;
    gap: 0.45rem;
  }

  .task-link-list a {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    min-height: 2rem;
    padding: 0 0.65rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 8px;
    color: var(--accent, #2563eb);
    background: var(--surface, #ffffff);
    font-size: 0.82rem;
    font-weight: 800;
    text-decoration: none;
  }

  .task-link-list a span {
    color: var(--assistant-muted, #475569);
    font-size: 0.72rem;
    font-weight: 700;
  }

  .compact-receipts {
    padding-left: 1.15rem;
  }

  .compact-notice {
    padding: 0.55rem;
  }

  .compiler-list {
    display: grid;
    gap: 0.12rem;
    margin: 0.1rem 0 0;
    padding-left: 1rem;
  }

  .compiler-score,
  .compiler-policy {
    overflow-wrap: anywhere;
  }

  .signal-toolbar {
    flex-wrap: wrap;
    align-items: flex-start;
    margin-top: 0.15rem;
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

  .notice > div {
    min-width: 0;
  }

  .notice-dismiss {
    display: inline-grid;
    flex: 0 0 auto;
    place-items: center;
    width: 2rem;
    height: 2rem;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 8px;
    color: currentColor;
    background: transparent;
    cursor: pointer;
  }

  .notice-dismiss:hover,
  .notice-dismiss:focus-visible {
    border-color: currentColor;
    background: rgb(255 255 255 / 0.42);
  }

  .notice-dismiss svg {
    width: 1rem;
    height: 1rem;
  }

  .notice-dismiss path {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.8;
    stroke-linecap: round;
  }

  .notice-action {
    flex: 0 0 auto;
  }

  .record-header {
    align-items: flex-start;
    padding: 1rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
  }

  .record-actions {
    flex: 0 0 auto;
    justify-content: flex-end;
    flex-wrap: wrap;
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

  .route-strip {
    justify-content: space-between;
    min-width: 0;
    padding: 0.75rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 8px;
    background: var(--surface, #ffffff);
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

  .plan-preview {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.45rem;
    min-width: 0;
  }

  .plan-preview small {
    min-width: 0;
    overflow-wrap: anywhere;
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
  :global(html[data-theme='dark'] .route-strip),
  :global(html[data-theme='dark'] .goals-panel),
  :global(html[data-theme='dark'] .signal-inbox),
  :global(html[data-theme='dark'] .detail-section),
  :global(html[data-theme='dark'] .empty-record) {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface) !important;
  }

  :global(html[data-theme='dark'] .run-row),
  :global(html[data-theme='dark'] .run-spaces button),
  :global(html[data-theme='dark'] .goal-list button),
  :global(html[data-theme='dark'] .task-link-list a),
  :global(html[data-theme='dark'] .signal-inbox-row),
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
  :global(html[data-theme='dark'] .run-row.selected),
  :global(html[data-theme='dark'] .run-spaces button:hover),
  :global(html[data-theme='dark'] .run-spaces button.active),
  :global(html[data-theme='dark'] .goal-list button:hover),
  :global(html[data-theme='dark'] .goal-list button.selected) {
    background: var(--surface-hover) !important;
  }

  :global(html[data-theme='dark'] .goal-form input),
  :global(html[data-theme='dark'] .goal-form textarea),
  :global(html[data-theme='dark'] .goal-form select) {
    color: var(--text) !important;
    border-color: var(--border-soft) !important;
    background: var(--surface-muted) !important;
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
    .record-actions,
    .decision-header,
    .route-strip,
    .section-heading,
    .recommendation-card header {
      align-items: flex-start;
      flex-direction: column;
    }

    .back-to-runs {
      display: inline-flex;
    }

    .record-summary,
    .goal-detail-grid,
    .form-grid,
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

    .signal-toolbar {
      display: grid;
      grid-template-columns: repeat(2, minmax(0, 1fr));
      width: 100%;
    }

    .action-toolbar button,
    .signal-toolbar button {
      width: 100%;
    }
  }
</style>
