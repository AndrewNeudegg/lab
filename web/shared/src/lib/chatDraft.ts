export type ChatDraftStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>;

export const chatDraftStorageKey = 'homelabd.dashboard.chatDraft.v1';

export const readStoredChatDraft = (
  storage: Pick<Storage, 'getItem'> = localStorage
): string => {
  try {
    return storage.getItem(chatDraftStorageKey) ?? '';
  } catch {
    return '';
  }
};

export const persistChatDraft = (
  draft: string,
  storage: ChatDraftStorage = localStorage
): boolean => {
  try {
    if (draft) {
      storage.setItem(chatDraftStorageKey, draft);
    } else {
      storage.removeItem(chatDraftStorageKey);
    }
    return true;
  } catch {
    return false;
  }
};
