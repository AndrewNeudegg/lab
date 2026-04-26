import { describe, expect, test } from 'bun:test';
import { chatDraftStorageKey, persistChatDraft, readStoredChatDraft } from './chatDraft';

const memoryStorage = (initialValue: string | null = null) => {
  const values = new Map<string, string>();
  if (initialValue !== null) {
    values.set(chatDraftStorageKey, initialValue);
  }

  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key)
  };
};

describe('chat draft storage', () => {
  test('loads an existing draft', () => {
    expect(readStoredChatDraft(memoryStorage('finish the deploy plan'))).toBe(
      'finish the deploy plan'
    );
  });

  test('falls back to an empty draft when none is stored', () => {
    expect(readStoredChatDraft(memoryStorage())).toBe('');
  });

  test('persists and clears a draft', () => {
    const storage = memoryStorage();

    expect(persistChatDraft('check the queue', storage)).toBe(true);
    expect(readStoredChatDraft(storage)).toBe('check the queue');

    expect(persistChatDraft('', storage)).toBe(true);
    expect(readStoredChatDraft(storage)).toBe('');
  });

  test('ignores storage failures', () => {
    const storage = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
      removeItem: () => {
        throw new Error('blocked');
      }
    };

    expect(readStoredChatDraft(storage)).toBe('');
    expect(persistChatDraft('still usable', storage)).toBe(false);
  });
});
