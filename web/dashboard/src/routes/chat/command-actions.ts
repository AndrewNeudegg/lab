const taskRefPattern = /(?:[a-f0-9]{6,12}|task_\d{8}_\d{6}_[a-f0-9]{8})/i;
const workflowRefPattern = /(?:[a-f0-9]{6,12}|workflow_\d{8}_\d{6}_[a-f0-9]{8})/i;
const approvalRefPattern = /approval_\d{8}_\d{6}_[a-f0-9]{8}/i;

export const normaliseCommand = (command: string) => command.trim().replace(/\s+/g, ' ');

export const isSafeCommand = (command: string) => {
  const normalised = normaliseCommand(command);
  if (!normalised || normalised.includes('<')) {
    return false;
  }
  const verb = normalised.split(' ')[0]?.toLowerCase() || '';
  if (['tasks', 'workflows', 'status', 'approvals', 'agents', 'help'].includes(normalised.toLowerCase())) {
    return true;
  }
  if (verb === 'workflow') {
    const action = normalised.split(' ')[1]?.toLowerCase() || '';
    if (['list', 'ls'].includes(action)) {
      return true;
    }
    if (['show', 'run', 'start'].includes(action)) {
      return workflowRefPattern.test(normalised);
    }
    if (['new', 'create'].includes(action)) {
      return normalised.length > 'workflow new '.length;
    }
  }
  if (['new', 'task'].includes(verb)) {
    return normalised.length > verb.length + 1;
  }
  if (['show', 'run', 'ux', 'review', 'diff', 'test', 'delete', 'accept', 'verify'].includes(verb)) {
    return taskRefPattern.test(normalised);
  }
  if (verb === 'delegate') {
    const parts = normalised.split(' ');
    return (
      parts.length >= 4 &&
      taskRefPattern.test(parts[1]) &&
      parts[2]?.toLowerCase() === 'to' &&
      ['codex', 'claude', 'gemini', 'ux'].includes(parts[3]?.toLowerCase())
    );
  }
  if (['approve', 'deny'].includes(verb)) {
    return approvalRefPattern.test(normalised);
  }
  return false;
};

export const extractCommands = (content: string): string[] => {
  const commands = new Set<string>();
  for (const match of content.matchAll(/`([^`\n]+)`/g)) {
    const command = normaliseCommand(match[1]);
    if (isSafeCommand(command)) {
      commands.add(command);
    }
  }
  return [...commands].slice(0, 5);
};
