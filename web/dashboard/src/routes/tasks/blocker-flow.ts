import type { HomelabdTask } from '@homelab/shared';

export type GoalBlockerFlowRole =
  | 'waiting_on_blocking_task'
  | 'blocking_task'
  | 'agent_repair'
  | 'goal_blocked';
export type GoalBlockerDecisionChoiceID = 'accept_current' | 'require_more' | 'custom';
export type GoalBlockerDecisionKind = 'resume' | 'reopen' | 'custom';

export interface GoalBlockerDecisionChoice {
  id: GoalBlockerDecisionChoiceID;
  kind: GoalBlockerDecisionKind;
  title: string;
  detail: string;
  actionLabel: string;
  defaultInstruction?: string;
}

export interface GoalBlockerFlow {
  role: GoalBlockerFlowRole;
  title: string;
  decisionLabel: string;
  decisionDetail: string;
  showBlockingTaskLink: boolean;
  showResumeGoalAction: boolean;
  showCheckGoalAction: boolean;
  decisionChoices: GoalBlockerDecisionChoice[];
}

const shortID = (id: string) =>
  id.length > 16 ? `${id.slice(0, 10)}...${id.slice(-4)}` : id;

const goalIdForTask = (task: HomelabdTask) => task.goal_blocker_trace?.goal_id || task.goal_id || '';

const isTerminalAcceptedBlocker = (status: string) => status === 'done' || status === 'cancelled';

const isReviewableBlocker = (status: string) =>
  status === 'awaiting_verification' ||
  status === 'awaiting_approval' ||
  status === 'ready_for_review' ||
  status === 'no_change_required';

const isRepairableBlocker = (status: string) =>
  status === 'blocked' ||
  status === 'timed_out' ||
  status === 'failed' ||
  status === 'conflict_resolution';

const blockerQuestion = (task: HomelabdTask) =>
  task.goal_blocker_trace?.questions?.find((question) => question.trim()) ||
  task.goal_blocker_trace?.reason ||
  'The Goal blocker needs an operator decision.';

const requireMoreInstruction = (task: HomelabdTask) =>
  [
    'Not acceptable.',
    `Require the stricter path before resuming Autopilot: ${blockerQuestion(task)}`,
    'Do not close or resume the Goal until the missing evidence, comparison, implementation, or product decision is completed and reported back with validation.'
  ].join(' ');

const terminalBlockerChoices = (task: HomelabdTask): GoalBlockerDecisionChoice[] => [
  {
    id: 'accept_current',
    kind: 'resume',
    title: 'Accept current evidence',
    detail: 'Use when the answer is yes: this blocker is acceptable and the Goal can continue.',
    actionLabel: 'Accept and resume'
  },
  {
    id: 'require_more',
    kind: 'reopen',
    title: 'Not acceptable: require more work',
    detail: 'Use when the answer is no: reopen the task with a clear missing requirement.',
    actionLabel: 'Reopen with this answer',
    defaultInstruction: requireMoreInstruction(task)
  },
  {
    id: 'custom',
    kind: 'custom',
    title: 'Answer another way',
    detail: 'Write the exact instruction or product decision that should guide the next run.',
    actionLabel: 'Reopen with custom answer'
  }
];

export const taskIsGoalBlocker = (task: HomelabdTask | undefined) =>
  Boolean(task?.id && task.goal_blocker_trace?.blocking_task_id === task.id);

export const goalBlockerFlow = (task: HomelabdTask | undefined): GoalBlockerFlow | undefined => {
  if (!task?.goal_blocker_trace) {
    return undefined;
  }

  const trace = task.goal_blocker_trace;
  const goalID = goalIdForTask(task);
  const resolver = trace.resolver || (trace.human_action_required === false ? 'agent' : 'human');
  const nextAction =
    trace.next_action ||
    trace.operator_action ||
    trace.blockers?.find((blocker) => blocker.trim()) ||
    trace.follow_ups?.find((followUp) => followUp.trim()) ||
    'Create the next repair task and rerun the relevant challenge.';

  if (resolver === 'agent') {
    return {
      role: 'agent_repair',
      title: taskIsGoalBlocker(task) ? 'Autopilot found repair work' : 'Goal needs autonomous repair',
      decisionLabel: 'No human decision needed',
      decisionDetail: nextAction,
      showBlockingTaskLink: Boolean(trace.blocking_task_id && !taskIsGoalBlocker(task)),
      showResumeGoalAction: true,
      showCheckGoalAction: Boolean(goalID),
      decisionChoices: []
    };
  }

  if (!taskIsGoalBlocker(task)) {
    if (trace.blocking_task_id) {
      return {
        role: 'waiting_on_blocking_task',
        title: `Goal is blocked by task ${shortID(trace.blocking_task_id)}`,
        decisionLabel: 'Open the blocking task',
        decisionDetail:
          'This task belongs to the blocked Goal, but the decision is on the linked blocking task.',
        showBlockingTaskLink: true,
        showResumeGoalAction: false,
        showCheckGoalAction: false,
        decisionChoices: []
      };
    }

    return {
      role: 'goal_blocked',
      title: 'Goal Autopilot is blocked',
      decisionLabel: 'Inspect the Goal blocker',
      decisionDetail:
        'The Goal is blocked without a single blocking task. Open the Goal to answer the blocker or change Autopilot.',
      showBlockingTaskLink: false,
      showResumeGoalAction: false,
      showCheckGoalAction: Boolean(goalID),
      decisionChoices: []
    };
  }

  if (isTerminalAcceptedBlocker(task.status)) {
    return {
      role: 'blocking_task',
      title: 'This task is blocking Goal Autopilot',
      decisionLabel: 'Decide whether to resume the Goal',
      decisionDetail:
        'This task is already closed, but its report left a Goal-level blocker. Choose whether the current evidence is acceptable, or reopen the task with the missing requirement.',
      showBlockingTaskLink: false,
      showResumeGoalAction: false,
      showCheckGoalAction: Boolean(goalID),
      decisionChoices: terminalBlockerChoices(task)
    };
  }

  if (isReviewableBlocker(task.status)) {
    return {
      role: 'blocking_task',
      title: 'This task is blocking Goal Autopilot',
      decisionLabel: 'Verify or reject this result',
      decisionDetail:
        'Review the task output. Accept it if it resolves the blocker, or reopen it with the missing evidence or product decision.',
      showBlockingTaskLink: false,
      showResumeGoalAction: false,
      showCheckGoalAction: Boolean(goalID),
      decisionChoices: []
    };
  }

  if (isRepairableBlocker(task.status)) {
    return {
      role: 'blocking_task',
      title: 'This task is blocking Goal Autopilot',
      decisionLabel: 'Repair the blocker',
      decisionDetail:
        'Use Retry or Reopen with the specific evidence, dependency, or operator decision needed before the Goal can continue.',
      showBlockingTaskLink: false,
      showResumeGoalAction: false,
      showCheckGoalAction: Boolean(goalID),
      decisionChoices: []
    };
  }

  return {
    role: 'blocking_task',
    title: 'This task is blocking Goal Autopilot',
    decisionLabel: 'Watch this task complete',
    decisionDetail:
      'Autopilot is waiting for this task to finish. When it reaches review or a blocked state, the task actions above become the decision point.',
    showBlockingTaskLink: false,
    showResumeGoalAction: false,
    showCheckGoalAction: Boolean(goalID),
    decisionChoices: []
  };
};
