import { describe, expect, test } from 'bun:test';
import { apiFetch, createHomelabdClient } from './client';

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' }
  });

describe('homelabd client', () => {
  test('retries safe reads after transient network failures', async () => {
    let attempts = 0;

    const response = await apiFetch<{ ok: boolean }>('/tasks', {
      baseUrl: 'http://homelabd',
      retryDelayMs: 0,
      fetcher: async () => {
        attempts += 1;
        if (attempts < 3) {
          throw new TypeError('Failed to fetch');
        }
        return jsonResponse({ ok: true });
      }
    });

    expect(response.ok).toBe(true);
    expect(attempts).toBe(3);
  });

  test('retries safe reads after retryable server responses', async () => {
    let attempts = 0;

    const response = await apiFetch<{ ok: boolean }>('/tasks', {
      baseUrl: 'http://homelabd',
      retryDelayMs: 0,
      fetcher: async () => {
        attempts += 1;
        if (attempts === 1) {
          return jsonResponse({ error: 'try again' }, 503);
        }
        return jsonResponse({ ok: true });
      }
    });

    expect(response.ok).toBe(true);
    expect(attempts).toBe(2);
  });

  test('does not retry safe reads after non-retryable server responses', async () => {
    let attempts = 0;
    let error: unknown;

    try {
      await apiFetch<{ ok: boolean }>('/tasks', {
        baseUrl: 'http://homelabd',
        retryDelayMs: 0,
        fetcher: async () => {
          attempts += 1;
          return jsonResponse({ error: 'bad request' }, 400);
        }
      });
    } catch (err) {
      error = err;
    }

    expect(error).toBeInstanceOf(Error);
    expect(attempts).toBe(1);
  });

  test('does not retry unsafe writes after a network failure', async () => {
    let attempts = 0;
    let error: unknown;

    try {
      await apiFetch<{ reply: string }>('/message', {
        baseUrl: 'http://homelabd',
        method: 'POST',
        body: JSON.stringify({ content: 'status' }),
        retryDelayMs: 0,
        fetcher: async () => {
          attempts += 1;
          throw new TypeError('Failed to fetch');
        }
      });
    } catch (err) {
      error = err;
    }

    expect(error).toBeInstanceOf(TypeError);
    expect(attempts).toBe(1);
  });

  test('creates a remote-targeted task with explicit target metadata', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        return jsonResponse({ reply: 'created' });
      }
    });

    const response = await client.createTask({
      goal: 'Update the remote checkout',
      target: {
        mode: 'remote',
        agent_id: 'desk',
        machine: 'desk.local',
        workdir_id: 'repo',
        workdir: '/srv/desk/repo',
        backend: 'codex'
      }
    });

    expect(response.reply).toBe('created');
    expect(requests).toHaveLength(1);
    expect(requests[0].url).toBe('http://homelabd/tasks');
    expect(requests[0].init?.method).toBe('POST');
    expect(requests[0].body).toEqual({
      goal: 'Update the remote checkout',
      target: {
        mode: 'remote',
        agent_id: 'desk',
        machine: 'desk.local',
        workdir_id: 'repo',
        workdir: '/srv/desk/repo',
        backend: 'codex'
      }
    });
  });

  test('lists remote agents from the control plane', async () => {
    const paths: string[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input) => {
        paths.push(String(input));
        return jsonResponse({
          agents: [
            {
              id: 'desk',
              name: 'Desk',
              machine: 'desk.local',
              status: 'online',
              last_seen: '2026-04-26T00:00:00Z',
              workdirs: [{ id: 'repo', path: '/srv/desk/repo' }]
            }
          ]
        });
      }
    });

    const response = await client.listAgents();

    expect(paths).toEqual(['http://homelabd/agents']);
    expect(response.agents[0].workdirs?.[0].path).toBe('/srv/desk/repo');
  });
});
