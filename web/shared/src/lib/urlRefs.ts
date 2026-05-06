const encodeRef = (value: string) => encodeURIComponent(value.trim());

export const taskURL = (taskId: string) =>
  taskId.trim() ? `/tasks?task=${encodeRef(taskId)}` : '/tasks';

export const assistantRunsURL = (view: 'active' | 'archived' = 'active') =>
  view === 'archived' ? '/assistant?view=archived' : '/assistant';

export const assistantRunURL = (runId: string, view: 'active' | 'archived' = 'active') => {
  if (!runId.trim()) {
    return assistantRunsURL(view);
  }
  const params = new URLSearchParams();
  if (view === 'archived') {
    params.set('view', 'archived');
  }
  params.set('run', runId.trim());
  return `/assistant?${params.toString()}`;
};

export const workflowURL = (workflowId: string) =>
  workflowId.trim() ? `/workflows?workflow=${encodeRef(workflowId)}` : '/workflows';

export const knowledgeSpaceURL = (spaceId: string) =>
  spaceId.trim() ? `/knowledge?space=${encodeRef(spaceId)}` : '/knowledge';

export const terminalURL = (ref: { tabId?: string; sessionId?: string } = {}) => {
  if (ref.sessionId?.trim()) {
    return `/terminal?session=${encodeRef(ref.sessionId)}`;
  }
  if (ref.tabId?.trim()) {
    return `/terminal?tab=${encodeRef(ref.tabId)}`;
  }
  return '/terminal';
};

export const docsURL = (slug: string, headingId = '') => {
  const path = slug.trim() ? `/docs/${encodeRef(slug).replaceAll('%2F', '/')}` : '/docs';
  return headingId.trim() ? `${path}#${encodeRef(headingId)}` : path;
};

export const chatMessageElementID = (messageId: string) => `message-${messageId.replace(/[^A-Za-z0-9_-]+/g, '-')}`;

export const chatMessageURL = (messageId: string) =>
  messageId.trim() ? `/chat#${chatMessageElementID(messageId)}` : '/chat';
