export type TerminalGeometry = {
  cols: number;
  rows: number;
};

export type TerminalRemoteAgent = {
  id: string;
  name: string;
  machine: string;
  status: string;
  metadata?: Record<string, string>;
};

export type TerminalTarget = {
  id: string;
  label: string;
  detail: string;
  apiBase: string;
  status: string;
};

export type TerminalSessionSnapshot = {
  id: string;
  shell: string;
  cwd: string;
  created_at: string;
  persistent?: boolean;
};

export type StoredTerminalTab = {
  id: string;
  title: string;
  targetId: string;
  targetLabel: string;
  apiBase: string;
  session?: TerminalSessionSnapshot;
};

export type StoredTerminalTabsState = {
  activeTabId: string;
  tabs: StoredTerminalTab[];
};

export const defaultTerminalGeometry: TerminalGeometry = { cols: 100, rows: 30 };
export const terminalTabsStorageKey = 'homelab-terminal-tabs:v1';

export const clampTerminalGeometry = (geometry: Partial<TerminalGeometry>): TerminalGeometry => {
  const cols = Math.trunc(geometry.cols || defaultTerminalGeometry.cols);
  const rows = Math.trunc(geometry.rows || defaultTerminalGeometry.rows);
  return {
    cols: Math.min(300, Math.max(20, cols)),
    rows: Math.min(120, Math.max(5, rows))
  };
};

export const endpoint = (apiBase: string, path: string) => `${apiBase}${path}`;

export const normaliseAPIBase = (value: string) => value.trim().replace(/\/+$/, '');

export const terminalBaseFromAgent = (agent: TerminalRemoteAgent) =>
  normaliseAPIBase(agent.metadata?.terminal_base_url || agent.metadata?.terminal_url || '');

export const buildTerminalTargets = (agents: TerminalRemoteAgent[], localAPIBase = '/api'): TerminalTarget[] => [
  {
    id: 'local',
    label: 'homelabd local',
    detail: 'Control plane shell',
    apiBase: normaliseAPIBase(localAPIBase),
    status: 'online'
  },
  ...agents
    .filter((agent) => agent.status === 'online' && terminalBaseFromAgent(agent) !== '')
    .map((agent) => ({
      id: `agent:${agent.id}`,
      label: agent.name || agent.id,
      detail: `${agent.machine || 'remote agent'} · ${agent.id}`,
      apiBase: terminalBaseFromAgent(agent),
      status: agent.status
    }))
];

export const websocketEndpoint = (apiBase: string, path: string, locationLike: Pick<Location, 'origin' | 'protocol'>) => {
  const httpURL = apiBase.startsWith('http://') || apiBase.startsWith('https://')
    ? new URL(endpoint(apiBase, path))
    : new URL(endpoint(apiBase, path), locationLike.origin);
  httpURL.protocol = httpURL.protocol === 'https:' ? 'wss:' : 'ws:';
  return httpURL.toString();
};

export const terminalStatusLabel = (connected: boolean, loading: boolean) => {
  if (connected) {
    return 'Connected';
  }
  if (loading) {
    return 'Starting';
  }
  return 'Disconnected';
};

const isRecord = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);

const cleanString = (value: unknown) => (typeof value === 'string' ? value.trim() : '');

export const defaultTerminalTabTitle = (index: number) => `Terminal ${index + 1}`;

export const normaliseStoredTerminalTabs = (
  value: unknown,
  fallbackTarget: TerminalTarget
): StoredTerminalTabsState => {
  if (!isRecord(value) || !Array.isArray(value.tabs)) {
    return { activeTabId: '', tabs: [] };
  }
  const tabs = value.tabs.flatMap((candidate, index): StoredTerminalTab[] => {
    if (!isRecord(candidate)) {
      return [];
    }
    const sessionCandidate = isRecord(candidate.session) ? candidate.session : undefined;
    const sessionId = cleanString(sessionCandidate?.id);
    const id = cleanString(candidate.id) || sessionId;
    if (!id) {
      return [];
    }
    const session = sessionId
      ? {
          id: sessionId,
          shell: cleanString(sessionCandidate?.shell),
          cwd: cleanString(sessionCandidate?.cwd),
          created_at: cleanString(sessionCandidate?.created_at),
          persistent: Boolean(sessionCandidate?.persistent)
        }
      : undefined;
    return [
      {
        id,
        title: cleanString(candidate.title) || defaultTerminalTabTitle(index),
        targetId: cleanString(candidate.targetId) || fallbackTarget.id,
        targetLabel: cleanString(candidate.targetLabel) || fallbackTarget.label,
        apiBase: normaliseAPIBase(cleanString(candidate.apiBase) || fallbackTarget.apiBase),
        session
      }
    ];
  });
  const requestedActiveTabId = cleanString(value.activeTabId);
  const activeTabId = tabs.some((tab) => tab.id === requestedActiveTabId) ? requestedActiveTabId : tabs[0]?.id || '';
  return { activeTabId, tabs };
};
