import { describe, expect, test } from 'bun:test';
import { filterWorkflows, parseWorkflowStepLines, workflowStatusTone } from './workflow-model';
import type { HomelabdWorkflow } from '@homelab/shared';

const workflow = (id: string, status: string, name: string): HomelabdWorkflow => ({
  id,
  name,
  status,
  steps: [],
  estimate: {
    steps: 0,
    estimated_llm_calls: 0,
    estimated_tool_calls: 0,
    workflow_calls: 0,
    waits: 0,
    estimated_seconds: 0,
    estimated_minutes: 0,
    summary: '0 steps'
  },
  created_at: '2026-04-28T00:00:00Z',
  updated_at: '2026-04-28T00:00:00Z'
});

describe('workflow model', () => {
  test('parses simple workflow step lines', () => {
    const steps = parseWorkflowStepLines(
      [
        'llm | Plan | Decide what to do',
        'tool | Search | internet.search | {"query":"workflow UX"}',
        'wait | Gate | healthd healthy | 120',
        'workflow | Chain | workflow_abc'
      ].join('\n')
    );

    expect(steps).toEqual([
      { name: 'Plan', kind: 'llm', prompt: 'Decide what to do' },
      { name: 'Search', kind: 'tool', tool: 'internet.search', args: { query: 'workflow UX' } },
      { name: 'Gate', kind: 'wait', condition: 'healthd healthy', timeout_seconds: 120 },
      { name: 'Chain', kind: 'workflow', workflow_id: 'workflow_abc' }
    ]);
  });

  test('filters active workflows and search text', () => {
    const workflows = [
      workflow('workflow_a', 'draft', 'Research'),
      workflow('workflow_b', 'completed', 'Deploy'),
      workflow('workflow_c', 'waiting', 'Watch deploy')
    ];

    expect(filterWorkflows(workflows, 'active', '').map((item) => item.id)).toEqual([
      'workflow_a',
      'workflow_c'
    ]);
    expect(filterWorkflows(workflows, 'all', 'deploy').map((item) => item.id)).toEqual([
      'workflow_b',
      'workflow_c'
    ]);
  });

  test('maps workflow status to accessible tone', () => {
    expect(workflowStatusTone('completed')).toBe('green');
    expect(workflowStatusTone('waiting')).toBe('amber');
    expect(workflowStatusTone('failed')).toBe('red');
  });
});
