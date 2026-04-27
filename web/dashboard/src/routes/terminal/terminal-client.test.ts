import { describe, expect, it } from 'bun:test';
import {
  buildTerminalTargets,
  clampTerminalGeometry,
  defaultTerminalTabTitle,
  endpoint,
  normaliseStoredTerminalTabs,
  terminalBaseFromAgent,
  terminalStatusLabel,
  websocketEndpoint
} from './terminal-client';

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

  it('builds local and online remote terminal targets', () => {
    const targets = buildTerminalTargets([
      {
        id: 'desk',
        name: 'Desk',
        machine: 'desk.local',
        status: 'online',
        metadata: { terminal_base_url: 'http://desk.local:18083/' }
      },
      {
        id: 'stale',
        name: 'Stale',
        machine: 'stale.local',
        status: 'offline',
        metadata: { terminal_base_url: 'http://stale.local:18083' }
      },
      {
        id: 'worker',
        name: 'Worker',
        machine: 'worker.local',
        status: 'online',
        metadata: {}
      }
    ]);

    expect(targets).toEqual([
      {
        id: 'local',
        label: 'homelabd local',
        detail: 'Control plane shell',
        apiBase: '/api',
        status: 'online'
      },
      {
        id: 'agent:desk',
        label: 'Desk',
        detail: 'desk.local · desk',
        apiBase: 'http://desk.local:18083',
        status: 'online'
      }
    ]);
  });

  it('supports the legacy terminal_url metadata key', () => {
    expect(
      terminalBaseFromAgent({
        id: 'nuc',
        name: 'Nuc',
        machine: 'nuc.local',
        status: 'online',
        metadata: { terminal_url: 'http://nuc.local:18083/' }
      })
    ).toBe('http://nuc.local:18083');
  });

  it('normalises persisted terminal tabs for reload reattachment', () => {
    const state = normaliseStoredTerminalTabs(
      {
        activeTabId: 'tab_two',
        tabs: [
          {
            id: 'tab_one',
            title: 'Build',
            targetId: 'local',
            targetLabel: 'homelabd local',
            apiBase: '/api/',
            session: {
              id: 'term_existing',
              shell: '/run/current-system/sw/bin/bash',
              cwd: '/home/lab/lab',
              created_at: '2026-04-27T00:00:00Z',
              persistent: true
            }
          },
          {
            id: 'tab_two',
            title: '',
            targetId: '',
            targetLabel: '',
            apiBase: ''
          }
        ]
      },
      {
        id: 'local',
        label: 'homelabd local',
        detail: 'Control plane shell',
        apiBase: '/api',
        status: 'online'
      }
    );

    expect(state.activeTabId).toBe('tab_two');
    expect(state.tabs[0].apiBase).toBe('/api');
    expect(state.tabs[0].session?.id).toBe('term_existing');
    expect(state.tabs[1].title).toBe(defaultTerminalTabTitle(1));
    expect(state.tabs[1].targetId).toBe('local');
  });
});
