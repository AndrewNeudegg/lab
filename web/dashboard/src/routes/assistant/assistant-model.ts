import type {
  AssistantActivity,
  AssistantCapability,
  AssistantGoal,
  AssistantRun,
  AssistantSignalCandidate,
  AssistantUXPattern
} from '@homelab/shared';

const areaLabels: Record<string, string> = {
  communication: 'Communication',
  execution: 'Execution',
  focus: 'Daily focus',
  memory: 'Memory',
  planning: 'Planning',
  research: 'Research',
  systems: 'Systems'
};

const autonomyLabels: Record<string, string> = {
  automatic: 'Automatic after approval',
  confirm: 'Act with confirmation',
  observe: 'Observe and suggest',
  plan: 'Plan and propose'
};

export const assistantAreaLabel = (area = '') => areaLabels[area] || area.replaceAll('-', ' ');

export const assistantAutonomyLabel = (autonomy = '') =>
  autonomyLabels[autonomy] || autonomy.replaceAll('_', ' ');

export const assistantAutonomyTone = (autonomy = '') => {
  switch (autonomy) {
    case 'automatic':
      return 'red';
    case 'confirm':
      return 'amber';
    case 'plan':
      return 'blue';
    default:
      return 'green';
  }
};

export const selectAssistantCapability = (
  capabilities: AssistantCapability[],
  selectedCapabilityId: string
) =>
  capabilities.find((capability) => capability.id === selectedCapabilityId) || capabilities[0];

export const patternsForCapability = (
  capability: AssistantCapability | undefined,
  patterns: AssistantUXPattern[]
) => {
  const ids = new Set(capability?.ux_pattern_ids || []);
  return patterns.filter((pattern) => ids.has(pattern.id));
};

export const activityCountForCapability = (
  capability: AssistantCapability | undefined,
  activities: AssistantActivity[]
) => {
  if (!capability) {
    return 0;
  }
  return activities.filter((activity) => activity.capability_ids.includes(capability.id)).length;
};

export const primaryCapabilityForActivity = (
  activity: AssistantActivity | undefined,
  capabilities: AssistantCapability[]
) => {
  const ids = new Set(activity?.capability_ids || []);
  return capabilities.find((capability) => ids.has(capability.id));
};

export const activityForCapability = (
  capability: AssistantCapability | undefined,
  activities: AssistantActivity[]
) => activities.find((activity) => Boolean(capability && activity.capability_ids.includes(capability.id)));

export const assistantRunStatusTone = (status = '') => {
  switch (status) {
    case 'failed':
      return 'red';
    case 'running':
      return 'blue';
    case 'completed':
      return 'green';
    default:
      return 'amber';
  }
};

export const assistantRunDecisionLabel = (decision = '') => {
  switch (decision) {
    case 'recommend':
      return 'Recommended action';
    case 'created_tasks':
      return 'Created tasks';
    case 'no_op':
      return 'No action';
    default:
      return decision.replaceAll('_', ' ') || 'Decision pending';
  }
};

export const selectAssistantRun = (runs: AssistantRun[], selectedRunId: string) =>
  runs.find((run) => run.id === selectedRunId);

export type AssistantRunView = 'active' | 'archived';

export const assistantRunView = (run: AssistantRun | undefined): AssistantRunView =>
  run?.archived ? 'archived' : 'active';

export const assistantRunsForView = (runs: AssistantRun[], view: AssistantRunView) =>
  runs.filter((run) => assistantRunView(run) === view);

export const assistantRunActionCount = (run: AssistantRun | undefined) =>
  run?.recommended_actions?.length || 0;

export const assistantRunActionStatusLabel = (status = '') => {
  switch (status) {
    case 'created_task':
      return 'Task created';
    case 'dismissed':
      return 'Dismissed';
    case 'snoozed':
      return 'Snoozed';
    case 'useful':
      return 'Marked useful';
    case 'failed':
      return 'Failed';
    case 'skipped':
      return 'Skipped';
    case 'recommended':
      return 'Recommended';
    default:
      return status.replaceAll('_', ' ') || 'Recommended';
  }
};

export const assistantRunActionStatusTone = (status = '') => {
  switch (status) {
    case 'created_task':
    case 'useful':
      return 'green';
    case 'dismissed':
    case 'skipped':
      return 'gray';
    case 'failed':
      return 'red';
    case 'snoozed':
      return 'amber';
    default:
      return 'blue';
  }
};

export const assistantSignalStatusLabel = (signal: AssistantSignalCandidate) => {
  if (signal.created_task_id) {
    return 'Task created';
  }
  if (signal.suppressed) {
    return signal.suppression_reason?.toLowerCase().includes('snoozed') ? 'Snoozed' : 'Suppressed';
  }
  if ((signal.useful_count || 0) > 0) {
    return 'Useful';
  }
  return 'Active';
};

export const assistantSignalStatusTone = (signal: AssistantSignalCandidate) => {
  if (signal.created_task_id || (signal.useful_count || 0) > 0) {
    return 'green';
  }
  if (signal.suppressed) {
    return signal.suppression_reason?.toLowerCase().includes('snoozed') ? 'amber' : 'gray';
  }
  return signal.score >= 80 ? 'amber' : 'blue';
};

export const assistantRouteLabel = (capability = '') => {
  switch (capability) {
    case 'tasks':
      return 'Tasks';
    case 'knowledge':
      return 'Knowledge';
    case 'workflows':
      return 'Workflows';
    case 'diagnose':
      return 'Diagnosis';
    case 'observe':
      return 'Observe';
    default:
      return capability.replaceAll('_', ' ') || 'Assistant';
  }
};

export const assistantGoalStatusLabel = (status = '') => {
  switch (status) {
    case 'active':
      return 'Active';
    case 'blocked':
      return 'Blocked';
    case 'paused':
      return 'Paused';
    case 'completed':
      return 'Completed';
    case 'archived':
      return 'Archived';
    default:
      return status.replaceAll('_', ' ') || 'Unknown';
  }
};

export const assistantGoalStatusTone = (status = '') => {
  switch (status) {
    case 'active':
      return 'green';
    case 'blocked':
      return 'amber';
    case 'paused':
      return 'gray';
    case 'completed':
      return 'blue';
    case 'archived':
      return 'gray';
    default:
      return 'blue';
  }
};

export const activeAssistantGoals = (goals: AssistantGoal[]) =>
  goals.filter((goal) => goal.status === 'active' || goal.status === 'blocked');

export const dueAssistantGoals = (goals: AssistantGoal[], now = new Date()) =>
  activeAssistantGoals(goals).filter((goal) => {
    if (!goal.next_check_at) {
      return true;
    }
    const dueAt = new Date(goal.next_check_at);
    return Number.isNaN(dueAt.valueOf()) || dueAt <= now;
  });

export const selectAssistantGoal = (goals: AssistantGoal[], selectedGoalId: string) =>
  goals.find((goal) => goal.id === selectedGoalId);
