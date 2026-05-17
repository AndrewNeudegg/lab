export type GoalQuestionChoiceID = 'require_full' | 'record_waiver' | 'custom';

export interface GoalQuestionChoice {
  id: GoalQuestionChoiceID;
  title: string;
  detail: string;
  actionLabel: string;
  defaultAnswer: string;
}

const cleanQuestion = (question = '') =>
  question.trim() || 'the open Goal question';

export const goalQuestionChoices = (question = ''): GoalQuestionChoice[] => {
  const prompt = cleanQuestion(question);
  return [
    {
      id: 'require_full',
      title: 'Require the full requirement',
      detail: 'Use when the missing evidence or stricter path remains required before completion.',
      actionLabel: 'Record answer and resume',
      defaultAnswer: `The full requirement remains in scope for this Goal. Do not claim the Goal complete until this is satisfied with evidence: ${prompt}`
    },
    {
      id: 'record_waiver',
      title: 'Record a waiver or deferment',
      detail: 'Use when the product owner accepts that some requirement is unsupported or deferred for now.',
      actionLabel: 'Record waiver and resume',
      defaultAnswer: `Record an explicit product-owner waiver or deferment for the unsupported or untested requirement, then continue Autopilot using that decision as acceptance context: ${prompt}`
    },
    {
      id: 'custom',
      title: 'Answer another way',
      detail: 'Write the exact product decision or operator instruction that should guide the next run.',
      actionLabel: 'Record custom answer',
      defaultAnswer: ''
    }
  ];
};

export const firstGoalQuestion = (questions: string[] | undefined) =>
  questions?.find((question) => question.trim()) || '';
