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

  test('clears chat history through the typed chat endpoint', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        return jsonResponse({ reply: 'cleared', removed_events: 2 });
      }
    });

    const response = await client.clearChat({ conversation_id: 'chat_123' });

    expect(response.reply).toBe('cleared');
    expect(requests).toHaveLength(1);
    expect(requests[0].url).toBe('http://homelabd/chat/clear');
    expect(requests[0].init?.method).toBe('POST');
    expect(requests[0].body).toEqual({ conversation_id: 'chat_123' });
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
      attachments: [
        {
          name: 'browser-context.json',
          content_type: 'application/json',
          size: 18,
          text: '{"url":"/tasks"}'
        }
      ],
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
      attachments: [
        {
          name: 'browser-context.json',
          content_type: 'application/json',
          size: 18,
          text: '{"url":"/tasks"}'
        }
      ],
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

  test('loads the assistant catalogue with API-owned filters', async () => {
    const paths: string[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input) => {
        paths.push(String(input));
        return jsonResponse({
          name: 'Assistant',
          summary: 'Life-improving operating layer.',
          updated_at: '2026-04-30T21:00:00Z',
          principles: [],
          activities: [],
          capabilities: [
            {
              id: 'research-prepare',
              name: 'Research and prepare',
              area: 'research',
              summary: 'Sourced research.',
              promise: 'Current and cited.',
              cadence: 'On demand',
              autonomy: 'plan',
              inputs: ['Question'],
              outputs: ['Brief'],
              surfaces: [{ label: 'Open Chat', href: '/chat', surface: 'chat' }],
              ux_pattern_ids: ['source-tray'],
              safeguards: ['Show sources'],
              workflow_template: {
                name: 'Research brief',
                goal: 'Research the question.',
                steps: [{ name: 'Search', kind: 'tool', tool: 'internet.search' }]
              }
            }
          ],
          ux_patterns: [],
          research_sources: [],
          filters: { areas: [{ value: 'research', label: 'Research', count: 1 }] }
        });
      }
    });

    const response = await client.getAssistant({ search: 'sources', area: 'research' });

    expect(paths).toEqual(['http://homelabd/assistant?q=sources&area=research']);
    expect(response.name).toBe('Assistant');
    expect(response.capabilities[0].workflow_template.steps[0].tool).toBe('internet.search');
  });

  test('uses typed assistant proactive run endpoints', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        if (String(input).endsWith('/assistant/runs/arun_1/actions/action_1')) {
          return jsonResponse({
            reply: 'Marked recommendation as useful.',
            run: {
              id: 'arun_1',
              status: 'completed',
              decision: 'recommend',
              trigger: { kind: 'manual', label: 'Manual proactive check' },
              autonomy: 'propose',
              summary: 'Action recommended.',
              recommended_actions: [
                {
                  id: 'action_1',
                  fingerprint: 'sig_action',
                  kind: 'task',
                  title: 'Review finding',
                  rationale: 'Useful.',
                  status: 'useful'
                }
              ],
              snapshot: { generated_at: '2026-04-30T21:00:00Z' },
              created_at: '2026-04-30T21:00:00Z',
              updated_at: '2026-04-30T21:00:00Z'
            }
          });
        }
        if (String(input).endsWith('/assistant/runs/arun_1') && init?.method === 'PATCH') {
          return jsonResponse({
            reply: 'Archived Assistant decision.',
            run: {
              id: 'arun_1',
              status: 'completed',
              decision: 'recommend',
              trigger: { kind: 'manual', label: 'Manual proactive check' },
              autonomy: 'propose',
              summary: 'Action recommended.',
              archived: true,
              archived_by: 'codex',
              archived_reason: 'No longer required.',
              snapshot: { generated_at: '2026-04-30T21:00:00Z' },
              created_at: '2026-04-30T21:00:00Z',
              updated_at: '2026-04-30T21:00:00Z'
            }
          });
        }
        if (String(input).endsWith('/assistant/runs/arun_1')) {
          return jsonResponse({
            id: 'arun_1',
            status: 'completed',
            decision: 'recommend',
            trigger: { kind: 'manual', label: 'Manual proactive check' },
            autonomy: 'propose',
            summary: 'Action recommended.',
            snapshot: { generated_at: '2026-04-30T21:00:00Z' },
            created_at: '2026-04-30T21:00:00Z',
            updated_at: '2026-04-30T21:00:00Z'
          });
        }
        if (init?.method === 'PATCH') {
          return jsonResponse({
            reply: 'Marked signal as useful.',
            signal: {
              id: 'sig_chat',
              fingerprint: 'sig_chat',
              source: 'chat',
              kind: 'chat_quality_feedback',
              title: 'Review subpar chat answer',
              surface: 'chat',
              score: 88,
              useful_count: 1
            }
          });
        }
        if (init?.method === 'POST') {
          return jsonResponse({
            reply: 'Assistant run completed.',
            run: {
              id: 'arun_2',
              status: 'completed',
              decision: 'recommend',
              trigger: { kind: 'manual', label: 'Operator requested proactive check' },
              autonomy: 'propose',
              summary: 'Follow-up recommended.',
              snapshot: { generated_at: '2026-04-30T21:01:00Z' },
              created_at: '2026-04-30T21:01:00Z',
              updated_at: '2026-04-30T21:01:00Z'
            }
          });
        }
        return jsonResponse({
          runs: [
            {
              id: 'arun_1',
              status: 'completed',
              decision: 'recommend',
              trigger: { kind: 'manual', label: 'Manual proactive check' },
              autonomy: 'propose',
              summary: 'Action recommended.',
              snapshot: { generated_at: '2026-04-30T21:00:00Z' },
              created_at: '2026-04-30T21:00:00Z',
              updated_at: '2026-04-30T21:00:00Z'
            }
          ]
        });
      }
    });

    const runs = await client.listAssistantRuns({ archived: 'include' });
    const run = await client.getAssistantRun('arun_1');
    const started = await client.startAssistantRun({
      trigger_kind: 'manual',
      trigger_label: 'Operator requested proactive check',
      autonomy: 'propose'
    });
    const feedback = await client.updateAssistantRunAction('arun_1', 'action_1', {
      feedback: 'useful'
    });
    const archived = await client.updateAssistantRunArchive('arun_1', {
      archived: true,
      actor: 'codex',
      reason: 'No longer required.'
    });

    expect(runs.runs[0].id).toBe('arun_1');
    expect(run.id).toBe('arun_1');
    expect(started.run.id).toBe('arun_2');
    expect(feedback.run.recommended_actions?.[0].status).toBe('useful');
    expect(archived.run.archived).toBe(true);
    expect(requests.map((request) => request.url)).toEqual([
      'http://homelabd/assistant/runs?archived=include',
      'http://homelabd/assistant/runs/arun_1',
      'http://homelabd/assistant/runs',
      'http://homelabd/assistant/runs/arun_1/actions/action_1',
      'http://homelabd/assistant/runs/arun_1'
    ]);
    expect(requests[2].init?.method).toBe('POST');
    expect(requests[2].body).toEqual({
      trigger_kind: 'manual',
      trigger_label: 'Operator requested proactive check',
      autonomy: 'propose'
    });
    expect(requests[3].init?.method).toBe('POST');
    expect(requests[3].body).toEqual({ feedback: 'useful' });
    expect(requests[4].init?.method).toBe('PATCH');
    expect(requests[4].body).toEqual({
      archived: true,
      actor: 'codex',
      reason: 'No longer required.'
    });
  });

  test('uses typed assistant signal candidate endpoints', async () => {
    const requests: { url: string; init?: RequestInit; body?: unknown }[] = [];
    const client = createHomelabdClient({
      baseUrl: 'http://homelabd',
      fetcher: async (input, init) => {
        requests.push({
          url: String(input),
          init,
          body: init?.body ? JSON.parse(String(init.body)) : undefined
        });
        if (init?.method === 'PATCH') {
          return jsonResponse({
            signal: {
              id: 'sig_chat',
              fingerprint: 'sig_chat',
              source: 'chat',
              kind: 'chat_quality_feedback',
              title: 'Review subpar chat answer',
              surface: 'chat',
              score: 88,
              useful_count: 1
            },
            reply: 'Marked signal as useful.'
          });
        }
        if (init?.method === 'POST') {
          return jsonResponse({
            signal: {
              id: 'sig_chat',
              fingerprint: 'sig_chat',
              source: 'chat',
              kind: 'chat_quality_feedback',
              title: 'Review subpar chat answer',
              surface: 'chat',
              score: 88,
              action_kind: 'task',
              safe_actions: ['create_task', 'useful', 'snooze', 'dismiss'],
              updated_at: '2026-05-06T12:00:00Z'
            }
          });
        }
        return jsonResponse({
          signals: [
            {
              id: 'sig_chat',
              fingerprint: 'sig_chat',
              source: 'chat',
              kind: 'chat_quality_feedback',
              title: 'Review subpar chat answer',
              surface: 'chat',
              score: 88
            }
          ]
        });
      }
    });

    const listed = await client.listAssistantSignals();
    const submitted = await client.submitAssistantSignal({
      source: 'chat',
      kind: 'chat_quality_feedback',
      title: 'Review subpar chat answer',
      surface: 'chat',
      score: 88,
      evidence: [{ source: 'chat', kind: 'user_feedback', title: 'Operator feedback' }],
      safe_actions: ['create_task', 'useful']
    });
    const updated = await client.updateAssistantSignal('sig_chat', { feedback: 'useful' });

    expect(listed.signals[0].source).toBe('chat');
    expect(submitted.signal.action_kind).toBe('task');
    expect(updated.signal.useful_count).toBe(1);
    expect(requests.map((request) => request.url)).toEqual([
      'http://homelabd/assistant/signals',
      'http://homelabd/assistant/signals',
      'http://homelabd/assistant/signals/sig_chat'
    ]);
    expect(requests[1].init?.method).toBe('POST');
    expect(requests[1].body).toEqual({
      source: 'chat',
      kind: 'chat_quality_feedback',
      title: 'Review subpar chat answer',
      surface: 'chat',
      score: 88,
      evidence: [{ source: 'chat', kind: 'user_feedback', title: 'Operator feedback' }],
      safe_actions: ['create_task', 'useful']
    });
    expect(requests[2].init?.method).toBe('PATCH');
    expect(requests[2].body).toEqual({ feedback: 'useful' });
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
    await client.moveTaskInMergeQueue('task_1', { direction: 'up' });
    await client.acceptTask('task_1');
    await client.restartTask('task_1');
    await client.reopenTask('task_1', { reason: 'needs mobile verification' });
    await client.cancelTask('task_1');
    await client.retryTask('task_1', { backend: 'codex', instruction: 'fix the blocked state' });
    await client.deleteTask('task_1');
    await client.approveApproval('approval_1');
    await client.denyApproval('approval_2');

    expect(requests.map((request) => `${request.init?.method || 'GET'} ${request.url}`)).toEqual([
      'POST http://homelabd/tasks/task_1/run',
      'POST http://homelabd/tasks/task_1/review',
      'POST http://homelabd/tasks/task_1/merge-queue',
      'POST http://homelabd/tasks/task_1/accept',
      'POST http://homelabd/tasks/task_1/restart',
      'POST http://homelabd/tasks/task_1/reopen',
      'POST http://homelabd/tasks/task_1/cancel',
      'POST http://homelabd/tasks/task_1/retry',
      'POST http://homelabd/tasks/task_1/delete',
      'POST http://homelabd/approvals/approval_1/approve',
      'POST http://homelabd/approvals/approval_2/deny'
    ]);
    expect(requests[2].body).toEqual({ direction: 'up' });
    expect(requests[5].body).toEqual({ reason: 'needs mobile verification' });
    expect(requests[7].body).toEqual({ backend: 'codex', instruction: 'fix the blocked state' });
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

  test('uses typed Knowledge Space endpoints', async () => {
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
          space: {
            id: 'kspace_1',
            title: 'Research',
            insight: { source_count: 1, word_count: 12 },
            sources: [],
            reports: [],
            created_at: '2026-04-30T00:00:00Z',
            updated_at: '2026-04-30T00:00:00Z'
          },
          source: {
            id: 'ksrc_1',
            title: 'Source',
            kind: 'text',
            content: 'source text',
            summary: 'source text',
            word_count: 2,
            created_at: '2026-04-30T00:00:00Z',
            updated_at: '2026-04-30T00:00:00Z'
          },
          report: {
            id: 'kreport_1',
            question: 'What matters?',
            mode: 'research',
            answer: 'Answer',
            created_at: '2026-04-30T00:00:00Z'
          },
          result: {
            query: 'evidence',
            question: 'What matters?',
            answer: 'Answer',
            evidence: [],
            created_at: '2026-04-30T00:00:00Z'
          },
          run: {
            id: 'krun_1',
            objective: 'Compare sources',
            depth: 'standard',
            status: 'completed',
            mode: 'research',
            created_at: '2026-04-30T00:00:00Z',
            updated_at: '2026-04-30T00:00:00Z'
          },
          spaces: []
        });
      }
    });

    await client.createKnowledgeSpace({
      title: 'Research',
      objective: 'Understand source-grounded answers'
    });
    await client.listKnowledgeSpaces();
    await client.getKnowledgeSpace('kspace_1');
    await client.updateKnowledgeSpace('kspace_1', {
      title: 'Research corpus',
      objective: 'Manage sources'
    });
    await client.addKnowledgeSource('kspace_1', {
      title: 'Source',
      kind: 'text',
      content: 'source text'
    });
    await client.deleteKnowledgeSource('kspace_1', 'ksrc_1');
    await client.researchKnowledgeSpace('kspace_1', {
      question: 'What matters?',
      mode: 'research',
      source_ids: ['ksrc_1']
    });
    await client.queryKnowledgeSpace('kspace_1', {
      query: 'evidence',
      limit: 4,
      source_ids: ['ksrc_1']
    });
    await client.askKnowledgeSpace('kspace_1', {
      question: 'What matters?',
      source_ids: ['ksrc_1']
    });
    await client.createKnowledgeResearchRun('kspace_1', {
      objective: 'Compare sources',
      depth: 'standard',
      source_ids: ['ksrc_1']
    });
    await client.resumeKnowledgeResearchRun('kspace_1', 'krun_1');
    await client.deleteKnowledgeSpace('kspace_1');

    expect(requests.map((request) => `${request.init?.method || 'GET'} ${request.url}`)).toEqual([
      'POST http://homelabd/knowledge/spaces',
      'GET http://homelabd/knowledge/spaces',
      'GET http://homelabd/knowledge/spaces/kspace_1',
      'PATCH http://homelabd/knowledge/spaces/kspace_1',
      'POST http://homelabd/knowledge/spaces/kspace_1/sources',
      'DELETE http://homelabd/knowledge/spaces/kspace_1/sources/ksrc_1',
      'POST http://homelabd/knowledge/spaces/kspace_1/research',
      'POST http://homelabd/knowledge/spaces/kspace_1/query',
      'POST http://homelabd/knowledge/spaces/kspace_1/ask',
      'POST http://homelabd/knowledge/spaces/kspace_1/research-runs',
      'POST http://homelabd/knowledge/spaces/kspace_1/research-runs/krun_1/resume',
      'DELETE http://homelabd/knowledge/spaces/kspace_1'
    ]);
    expect(requests[0].body).toEqual({
      title: 'Research',
      objective: 'Understand source-grounded answers'
    });
    expect(requests[3].body).toEqual({
      title: 'Research corpus',
      objective: 'Manage sources'
    });
    expect(requests[4].body).toEqual({
      title: 'Source',
      kind: 'text',
      content: 'source text'
    });
    expect(requests[6].body).toEqual({
      question: 'What matters?',
      mode: 'research',
      source_ids: ['ksrc_1']
    });
    expect(requests[7].body).toEqual({
      query: 'evidence',
      limit: 4,
      source_ids: ['ksrc_1']
    });
    expect(requests[8].body).toEqual({
      question: 'What matters?',
      source_ids: ['ksrc_1']
    });
    expect(requests[9].body).toEqual({
      objective: 'Compare sources',
      depth: 'standard',
      source_ids: ['ksrc_1']
    });
  });
});
