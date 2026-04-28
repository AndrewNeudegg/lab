import { describe, expect, test } from 'bun:test';
import { createHomelabdClient } from './client';

const jsonResponse = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' }
  });

describe('homelabd client', () => {
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

  test('uses typed task and approval action endpoints', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        return jsonResponse({ reply: 'ok' });
      }
    });

    await client.runTask('task_1');
    await client.reviewTask('task_1');
    await client.acceptTask('task_1');
    await client.reopenTask('task_1', { reason: 'needs mobile verification' });
    await client.cancelTask('task_1');
    await client.retryTask('task_1', { backend: 'codex', instruction: 'fix the blocked state' });
    await client.deleteTask('task_1');
    await client.approveApproval('approval_1');
    await client.denyApproval('approval_2');

    expect(requests.map((request) => `${request.init?.method || 'GET'} ${request.url}`)).toEqual([
      'POST http://homelabd/tasks/task_1/run',
      'POST http://homelabd/tasks/task_1/review',
      'POST http://homelabd/tasks/task_1/accept',
      'POST http://homelabd/tasks/task_1/reopen',
      'POST http://homelabd/tasks/task_1/cancel',
      'POST http://homelabd/tasks/task_1/retry',
      'POST http://homelabd/tasks/task_1/delete',
      'POST http://homelabd/approvals/approval_1/approve',
      'POST http://homelabd/approvals/approval_2/deny'
    ]);
    expect(requests[3].body).toEqual({ reason: 'needs mobile verification' });
    expect(requests[5].body).toEqual({ backend: 'codex', instruction: 'fix the blocked state' });
  });

  test('uses typed workflow endpoints', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        return jsonResponse({
          reply: 'ok',
          workflow: {
            id: 'workflow_1',
            name: 'Research',
            status: 'draft',
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
          },
          workflows: []
        });
      }
    });

    await client.createWorkflow({
      name: 'Research',
      goal: 'Find sources',
      steps: [{ name: 'Search', kind: 'tool', tool: 'internet.search', args: { query: 'agents' } }]
    });
    await client.listWorkflows();
    await client.getWorkflow('workflow_1');
    await client.runWorkflow('workflow_1');

    expect(requests.map((request) => `${request.init?.method || 'GET'} ${request.url}`)).toEqual([
      'POST http://homelabd/workflows',
      'GET http://homelabd/workflows',
      'GET http://homelabd/workflows/workflow_1',
      'POST http://homelabd/workflows/workflow_1/run'
    ]);
    expect(requests[0].body).toEqual({
      name: 'Research',
      goal: 'Find sources',
      steps: [{ name: 'Search', kind: 'tool', tool: 'internet.search', args: { query: 'agents' } }]
    });
  });
});
