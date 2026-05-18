import type {
  AssistantGoalBlockerDecisionChoice,
  AssistantGoalBlockerFlow,
  HomelabdTask
} from '@homelab/shared';

export type GoalBlockerFlowRole = NonNullable<AssistantGoalBlockerFlow['role']>;
export type GoalBlockerDecisionChoiceID = NonNullable<AssistantGoalBlockerDecisionChoice['id']>;
export type GoalBlockerDecisionKind = NonNullable<AssistantGoalBlockerDecisionChoice['kind']>;

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
  question?: string;
  decisionLabel: string;
  decisionDetail: string;
  showBlockingTaskLink: boolean;
  showResumeGoalAction: boolean;
  showCheckGoalAction: boolean;
  decisionChoices: GoalBlockerDecisionChoice[];
}

export const taskIsGoalBlocker = (task: HomelabdTask | undefined) =>
  Boolean(task?.id && task.goal_blocker_trace?.blocking_task_id === task.id);

export const goalBlockerFlow = (task: HomelabdTask | undefined): GoalBlockerFlow | undefined =>
  normaliseGoalBlockerFlow(task?.goal_blocker_trace?.flow);

export const normaliseGoalBlockerFlow = (
  flow: AssistantGoalBlockerFlow | undefined
): GoalBlockerFlow | undefined => {
  if (!flow) {
    return undefined;
  }
  return {
    role: flow.role,
    title: flow.title,
    question: flow.question,
    decisionLabel: flow.decision_label,
    decisionDetail: flow.decision_detail,
    showBlockingTaskLink: flow.show_blocking_task_link,
    showResumeGoalAction: flow.show_resume_goal_action,
    showCheckGoalAction: flow.show_check_goal_action,
    decisionChoices: (flow.decision_choices || []).map((choice) => ({
      id: choice.id,
      kind: choice.kind,
      title: choice.title,
      detail: choice.detail,
      actionLabel: choice.action_label,
      defaultInstruction: choice.default_instruction
    }))
  };
};
