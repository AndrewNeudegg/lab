import { describe, expect, test } from 'bun:test';
import {
  chatMessageElementID,
  chatMessageURL,
  docsURL,
  taskURL,
  terminalURL,
  workflowURL
} from './urlRefs';

describe('dashboard URL references', () => {
  test('builds stable selected-record URLs', () => {
    expect(taskURL('task_20260428_120000_11111111')).toBe(
      '/tasks?task=task_20260428_120000_11111111'
    );
    expect(workflowURL('workflow_20260428_120000_22222222')).toBe(
      '/workflows?workflow=workflow_20260428_120000_22222222'
    );
    expect(terminalURL({ sessionId: 'term_123' })).toBe('/terminal?session=term_123');
    expect(terminalURL({ tabId: 'tab_abc' })).toBe('/terminal?tab=tab_abc');
    expect(docsURL('task-workflow', 'browser-uat')).toBe('/docs/task-workflow#browser-uat');
  });

  test('builds safe chat message anchors', () => {
    expect(chatMessageElementID('assistant 4/created')).toBe('message-assistant-4-created');
    expect(chatMessageURL('assistant 4/created')).toBe('/chat#message-assistant-4-created');
  });
});
