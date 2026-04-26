export type TerminalGeometry = {
  cols: number;
  rows: number;
};

export const defaultTerminalGeometry: TerminalGeometry = { cols: 100, rows: 30 };

export const clampTerminalGeometry = (geometry: Partial<TerminalGeometry>): TerminalGeometry => {
  const cols = Math.trunc(geometry.cols || defaultTerminalGeometry.cols);
  const rows = Math.trunc(geometry.rows || defaultTerminalGeometry.rows);
  return {
    cols: Math.min(300, Math.max(20, cols)),
    rows: Math.min(120, Math.max(5, rows))
  };
};

export const endpoint = (apiBase: string, path: string) => `${apiBase}${path}`;

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
