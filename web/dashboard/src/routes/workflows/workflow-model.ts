import type { HomelabdWorkflow, HomelabdWorkflowStep } from '@homelab/shared';

export type WorkflowFilter = 'active' | 'all';

const activeStatuses = new Set(['draft', 'running', 'waiting', 'awaiting_approval']);

export const workflowIsActive = (workflow: Pick<HomelabdWorkflow, 'status'>) =>
  activeStatuses.has(workflow.status);

export const workflowStatusTone = (status = '') => {
  switch (status) {
    case 'completed':
      return 'green';
    case 'failed':
    case 'cancelled':
      return 'red';
    case 'waiting':
    case 'awaiting_approval':
      return 'amber';
    case 'running':
      return 'blue';
    default:
      return 'gray';
  }
};

export const compactWorkflowID = (id = '') => {
  const parts = id.split('_');
  const tail = parts[parts.length - 1] || id;
  return tail.length > 8 ? tail.slice(0, 8) : tail;
};

export const filterWorkflows = (
  workflows: HomelabdWorkflow[],
  filter: WorkflowFilter,
  search: string
) => {
  const query = search.trim().toLowerCase();
  return workflows.filter((workflow) => {
    if (filter === 'active' && !workflowIsActive(workflow)) {
      return false;
    }
    if (!query) {
      return true;
    }
    return [workflow.id, workflow.name, workflow.goal || '', workflow.description || '']
      .join(' ')
      .toLowerCase()
      .includes(query);
  });
};

export const parseWorkflowStepLines = (value: string): HomelabdWorkflowStep[] =>
  value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line, index) => parseWorkflowStepLine(line, index));

const parseWorkflowStepLine = (line: string, index: number): HomelabdWorkflowStep => {
  const [rawKind = 'llm', rawName = `Step ${index + 1}`, ...rest] = line
    .split('|')
    .map((part) => part.trim());
  const kind = rawKind.toLowerCase();
  const name = rawName || `Step ${index + 1}`;

  if (kind === 'tool') {
    const [tool = '', rawArgs = '{}'] = rest;
    return {
      name,
      kind,
      tool,
      args: parseJSONArgs(rawArgs)
    };
  }
  if (kind === 'workflow') {
    return {
      name,
      kind,
      workflow_id: rest.join(' | ').trim()
    };
  }
  if (kind === 'wait') {
    const [condition = '', timeout = '300'] = rest;
    return {
      name,
      kind,
      condition,
      timeout_seconds: Number.parseInt(timeout, 10) || 300
    };
  }
  return {
    name,
    kind: 'llm',
    prompt: rest.join(' | ').trim()
  };
};

const parseJSONArgs = (raw: string) => {
  try {
    return JSON.parse(raw || '{}') as unknown;
  } catch {
    return { input: raw };
  }
};

export const workflowStepLinePlaceholder = [
  'llm | Plan approach | Decide the next action from the workflow goal',
  'tool | Search docs | internet.search | {"query":"agent workflow design"}',
  'wait | Health gate | healthd reports healthy | 300',
  'workflow | Chain existing workflow | workflow_123'
].join('\n');
