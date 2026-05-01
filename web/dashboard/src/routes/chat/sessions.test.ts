import { describe, expect, test } from 'bun:test';
import type { ChatTranscriptMessage } from '@homelab/shared';
import {
  chatSessionTitle,
  emptyChatTitle,
  legacyTranscriptStorageKey,
  prepareSessionForStorage,
  restoreChatState
} from './sessions';

const message = (role: ChatTranscriptMessage['role'], content: string): ChatTranscriptMessage => ({
  id: `${role}-${content}`,
  role,
  content,
  time: '12:00'
});

describe('chat sessions', () => {
  test('migrates the legacy single transcript into one resumable chat', () => {
    const state = restoreChatState({
      storedSessions: null,
      storedActiveID: null,
      legacyTranscript: JSON.stringify([message('user', 'continue this previous chat')]),
      createID: () => 'chat_legacy',
      now: '2026-04-30T12:00:00.000Z'
    });

    expect(legacyTranscriptStorageKey).toBe('homelabd.dashboard.chatTranscript.v4');
    expect(state.activeSessionID).toBe('chat_legacy');
    expect(state.sessions).toHaveLength(1);
    expect(state.sessions[0].title).toBe('continue this previous chat');
    expect(state.sessions[0].legacy).toBe(true);
  });

  test('restores the selected session and sorts history by recent activity', () => {
    const state = restoreChatState({
      storedSessions: JSON.stringify([
        {
          id: 'old',
          title: 'Old',
          messages: [message('user', 'old chat')],
          created_at: '2026-04-29T12:00:00.000Z',
          updated_at: '2026-04-29T12:00:00.000Z'
        },
        {
          id: 'new',
          title: 'New',
          messages: [message('user', 'new chat')],
          created_at: '2026-04-30T12:00:00.000Z',
          updated_at: '2026-04-30T12:00:00.000Z'
        }
      ]),
      storedActiveID: 'old',
      legacyTranscript: null,
      createID: () => 'unused',
      now: '2026-04-30T12:00:00.000Z'
    });

    expect(state.activeSessionID).toBe('old');
    expect(state.sessions.map((session) => session.id)).toEqual(['new', 'old']);
  });

  test('uses the first user message as a compact title', () => {
    expect(chatSessionTitle([message('assistant', 'welcome'), message('user', '  fix   this  ')])).toBe(
      'fix this'
    );
    expect(chatSessionTitle([])).toBe(emptyChatTitle);
  });

  test('strips attachment data URLs from stored history', () => {
    const stored = prepareSessionForStorage({
      id: 'chat_123',
      title: 'Attachment chat',
      created_at: '2026-04-30T12:00:00.000Z',
      updated_at: '2026-04-30T12:00:00.000Z',
      messages: [
        {
          ...message('user', 'with attachment'),
          attachments: [
            {
              id: 'att_1',
              name: 'screen.png',
              content_type: 'image/png',
              size: 4,
              data_url: 'data:image/png;base64,AAAA'
            }
          ]
        }
      ]
    });

    expect(stored.messages[0].attachments?.[0].data_url).toBeUndefined();
  });

  test('restores assistant reply buttons from stored history', () => {
    const state = restoreChatState({
      storedSessions: JSON.stringify([
        {
          id: 'buttons',
          title: 'Buttons',
          messages: [{ ...message('assistant', 'Choose one'), buttons: ['Yes', 'No'] }],
          created_at: '2026-04-30T12:00:00.000Z',
          updated_at: '2026-04-30T12:00:00.000Z'
        }
      ]),
      storedActiveID: 'buttons',
      legacyTranscript: null,
      createID: () => 'unused',
      now: '2026-04-30T12:00:00.000Z'
    });

    expect(state.sessions[0].messages[0].buttons).toEqual(['Yes', 'No']);
  });
});
