import { describe, expect, test } from 'bun:test';
import type {
  AssistantActivity,
  AssistantCapability,
  AssistantGoal,
  AssistantRun,
  AssistantUXPattern
} from '@homelab/shared';
import {
  activityCountForCapability,
  activityForCapability,
  activeAssistantGoals,
  assistantAreaLabel,
  assistantGoalAutopilotStatusLabel,
  assistantGoalAutopilotTone,
  assistantGoalExecutionLabel,
  assistantGoalKindLabel,
  assistantGoalKindShortLabel,
  assistantGoalStatusLabel,
  assistantGoalStatusTone,
  assistantRunActionCount,
  assistantRunActionStatusLabel,
  assistantRunActionStatusTone,
  assistantRunDecisionLabel,
  assistantRunsForView,
  assistantRunView,
  assistantRunStatusTone,
  assistantAutonomyLabel,
  assistantAutonomyTone,
  dueAssistantGoals,
  patternsForCapability,
  primaryCapabilityForActivity,
  selectAssistantCapability,
  selectAssistantGoal,
  selectAssistantRun
} from './assistant-model';

const capability = (id: string, uxPatternIds: string[] = []): AssistantCapability => ({
  id,
  name: id,
  area: 'research',
  summary: 'summary',
  promise: 'promise',
  cadence: 'cadence',
  autonomy: 'plan',
  inputs: [],
  outputs: [],
  surfaces: [],
  ux_pattern_ids: uxPatternIds,
  safeguards: [],
  workflow_template: {
    name: 'template',
    goal: 'goal',
    steps: [{ name: 'step', kind: 'llm', prompt: 'prompt' }]
  }
});

const activity = (id: string, capabilityIds: string[]): AssistantActivity => ({
  id,
  name: id,
  area: 'research',
  cadence: 'now',
  description: 'description',
  outcome: 'outcome',
  capability_ids: capabilityIds
});

const run = (id: string, actions = 0): AssistantRun => ({
  id,
  status: 'completed',
  decision: actions > 0 ? 'recommend' : 'no_op',
  trigger: { kind: 'manual', label: 'Manual proactive check' },
  autonomy: 'propose',
  summary: 'summary',
  recommended_actions: Array.from({ length: actions }, (_, index) => ({
    id: `action_${index + 1}`,
    kind: 'task',
    title: `Action ${index + 1}`,
    rationale: 'because'
  })),
  snapshot: { generated_at: '2026-04-30T21:00:00Z' },
  created_at: '2026-04-30T21:00:00Z',
  updated_at: '2026-04-30T21:00:00Z'
});

const goal = (id: string, status = 'active', nextCheckAt = '2026-05-07T08:00:00Z'): AssistantGoal => ({
  id,
  title: id,
  objective: 'objective',
  status,
  autonomy: 'observe',
  next_check_at: nextCheckAt,
  created_at: '2026-05-07T07:00:00Z',
  updated_at: '2026-05-07T07:00:00Z'
});

describe('assistant model', () => {
  test('labels assistant areas and autonomy levels', () => {
    expect(assistantAreaLabel('focus')).toBe('Daily focus');
    expect(assistantAutonomyLabel('confirm')).toBe('Act with confirmation');
    expect(assistantAutonomyTone('automatic')).toBe('red');
    expect(assistantAutonomyTone('observe')).toBe('green');
  });

  test('selects requested capability or falls back to first visible capability', () => {
    const capabilities = [capability('brief'), capability('research')];

    expect(selectAssistantCapability(capabilities, 'research')?.id).toBe('research');
    expect(selectAssistantCapability(capabilities, 'missing')?.id).toBe('brief');
  });

  test('maps ux patterns and activity count to a capability', () => {
    const selected = capability('research', ['source-tray']);
    const patterns: AssistantUXPattern[] = [
      {
        id: 'source-tray',
        name: 'Source tray',
        summary: 'sources',
        applies_to: 'research',
        implementation: 'show sources'
      },
      {
        id: 'audit',
        name: 'Audit',
        summary: 'audit',
        applies_to: 'actions',
        implementation: 'show receipts'
      }
    ];

    expect(patternsForCapability(selected, patterns).map((pattern) => pattern.id)).toEqual([
      'source-tray'
    ]);
    expect(
      activityCountForCapability(selected, [activity('decision', ['research']), activity('brief', ['brief'])])
    ).toBe(1);
  });

  test('maps operator activities to their supporting capability', () => {
    const capabilities = [capability('brief'), capability('research')];
    const researchActivity = activity('decision', ['research']);

    expect(primaryCapabilityForActivity(researchActivity, capabilities)?.id).toBe('research');
    expect(primaryCapabilityForActivity(activity('missing', ['missing']), capabilities)).toBeUndefined();
    expect(activityForCapability(capabilities[1], [activity('briefing', ['brief']), researchActivity])?.id).toBe(
      'decision'
    );
  });

  test('labels and selects proactive assistant runs', () => {
    const archived = { ...run('arun_archived', 1), archived: true };
    const runs = [run('arun_1'), run('arun_2', 2), archived];

    expect(assistantRunStatusTone('failed')).toBe('red');
    expect(assistantRunStatusTone('running')).toBe('blue');
    expect(assistantRunDecisionLabel('created_tasks')).toBe('Created tasks');
    expect(assistantRunActionStatusLabel('created_task')).toBe('Task created');
    expect(assistantRunActionStatusLabel('snoozed')).toBe('Snoozed');
    expect(assistantRunActionStatusTone('dismissed')).toBe('gray');
    expect(assistantRunActionStatusTone('useful')).toBe('green');
    expect(selectAssistantRun(runs, 'arun_2')?.id).toBe('arun_2');
    expect(selectAssistantRun(runs, 'missing')).toBeUndefined();
    expect(assistantRunActionCount(runs[1])).toBe(2);
    expect(assistantRunActionCount(undefined)).toBe(0);
    expect(assistantRunView(archived)).toBe('archived');
    expect(assistantRunsForView(runs, 'active').map((value) => value.id)).toEqual(['arun_1', 'arun_2']);
    expect(assistantRunsForView(runs, 'archived').map((value) => value.id)).toEqual(['arun_archived']);
  });

  test('labels and selects Assistant Goals', () => {
    const goals = [
      goal('goal_due'),
      goal('goal_future', 'active', '2026-05-08T08:00:00Z'),
      goal('goal_blocked', 'blocked'),
      goal('goal_paused', 'paused')
    ];

    expect(assistantGoalStatusLabel('blocked')).toBe('Blocked');
    expect(assistantGoalStatusTone('active')).toBe('green');
    expect(assistantGoalStatusTone('paused')).toBe('gray');
    expect(assistantGoalKindLabel('build')).toBe('Build Goal');
    expect(assistantGoalKindShortLabel('maintenance')).toBe('Maintenance');
    expect(assistantGoalExecutionLabel('autopilot')).toBe('Autopilot');
    expect(assistantGoalAutopilotStatusLabel('budget_exhausted')).toBe('Budget exhausted');
    expect(assistantGoalAutopilotTone('blocked')).toBe('amber');
    expect(activeAssistantGoals(goals).map((value) => value.id)).toEqual([
      'goal_due',
      'goal_future',
      'goal_blocked'
    ]);
    expect(dueAssistantGoals(goals, new Date('2026-05-07T09:00:00Z')).map((value) => value.id)).toEqual([
      'goal_due',
      'goal_blocked'
    ]);
    expect(selectAssistantGoal(goals, 'goal_future')?.id).toBe('goal_future');
    expect(selectAssistantGoal(goals, 'missing')).toBeUndefined();
  });
});
