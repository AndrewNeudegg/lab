import { describe, expect, it } from 'bun:test';
import { clampTerminalGeometry, endpoint, terminalStatusLabel, websocketEndpoint } from './terminal-client';

describe('terminal client helpers', () => {
  it('clamps unsafe terminal geometry', () => {
    expect(clampTerminalGeometry({ cols: 1, rows: 1 })).toEqual({ cols: 20, rows: 5 });
    expect(clampTerminalGeometry({ cols: 9999, rows: 9999 })).toEqual({ cols: 300, rows: 120 });
    expect(clampTerminalGeometry({ cols: 132.9, rows: 41.1 })).toEqual({ cols: 132, rows: 41 });
  });

  it('builds HTTP endpoints', () => {
    expect(endpoint('/api', '/terminal/sessions')).toBe('/api/terminal/sessions');
  });

  it('converts relative API paths to websocket URLs', () => {
    expect(
      websocketEndpoint('/api', '/terminal/sessions/term_123/ws', {
        origin: 'http://lab:5173',
        protocol: 'http:'
      } as Location)
    ).toBe('ws://lab:5173/api/terminal/sessions/term_123/ws');
  });

  it('converts absolute HTTPS API paths to secure websocket URLs', () => {
    expect(
      websocketEndpoint('https://lab.example/api', '/terminal/sessions/term_123/ws', {
        origin: 'http://ignored',
        protocol: 'http:'
      } as Location)
    ).toBe('wss://lab.example/api/terminal/sessions/term_123/ws');
  });

  it('reports concise connection status labels', () => {
    expect(terminalStatusLabel(true, false)).toBe('Connected');
    expect(terminalStatusLabel(false, true)).toBe('Starting');
    expect(terminalStatusLabel(false, false)).toBe('Disconnected');
  });
});
