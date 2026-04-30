import type { ChatInteractionStats, ChatTranscriptMessage } from '@homelab/shared';

export const chatSessionsStorageKey = 'homelabd.dashboard.chatSessions.v1';
export const activeChatSessionStorageKey = 'homelabd.dashboard.activeChatSession.v1';
export const legacyTranscriptStorageKey = 'homelabd.dashboard.chatTranscript.v4';
export const emptyChatTitle = 'New chat';

export interface ChatSession {
  id: string;
  title: string;
  messages: ChatTranscriptMessage[];
  created_at: string;
  updated_at: string;
  legacy?: boolean;
}

export interface RestoredChatState {
  sessions: ChatSession[];
  activeSessionID: string;
}

interface RestoreInput {
  storedSessions: string | null;
  storedActiveID: string | null;
  legacyTranscript: string | null;
  createID: () => string;
  now: string;
}

const statKeys: Array<keyof ChatInteractionStats> = [
  'model_turns',
  'tool_calls',
  'input_tokens',
  'output_tokens',
  'total_tokens',
  'elapsed_ms'
];

export const isTranscriptMessage = (value: unknown): value is ChatTranscriptMessage => {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  const validRole = candidate.role === 'user' || candidate.role === 'assistant';
  const validActions =
    candidate.actions === undefined ||
    (Array.isArray(candidate.actions) &&
      candidate.actions.every((action) => typeof action === 'string'));
  const validStats =
    candidate.stats === undefined ||
    (candidate.stats !== null &&
      typeof candidate.stats === 'object' &&
      statKeys.every((key) => {
        const stat = (candidate.stats as Record<string, unknown>)[key];
        return stat === undefined || (typeof stat === 'number' && Number.isFinite(stat) && stat >= 0);
      }));
  const validAttachments =
    candidate.attachments === undefined ||
    (Array.isArray(candidate.attachments) &&
      candidate.attachments.every(
        (attachment) =>
          attachment &&
          typeof attachment === 'object' &&
          typeof (attachment as Record<string, unknown>).name === 'string' &&
          typeof (attachment as Record<string, unknown>).content_type === 'string'
      ));
  const validDeliveryStatus =
    candidate.delivery_status === undefined || candidate.delivery_status === 'failed';
  const validDeliveryError =
    candidate.delivery_error === undefined || typeof candidate.delivery_error === 'string';

  return (
    typeof candidate.id === 'string' &&
    validRole &&
    typeof candidate.content === 'string' &&
    typeof candidate.time === 'string' &&
    validActions &&
    validStats &&
    validAttachments &&
    validDeliveryStatus &&
    validDeliveryError
  );
};

export const chatSessionTitle = (messages: ChatTranscriptMessage[]) => {
  const firstUserMessage = messages.find(
    (message) => message.role === 'user' && message.content.trim()
  );
  const text = firstUserMessage?.content.trim() || emptyChatTitle;
  const collapsed = text.replace(/\s+/g, ' ');
  return collapsed.length > 56 ? `${collapsed.slice(0, 53).trim()}...` : collapsed;
};

export const sanitiseSessionMessages = (messages: ChatTranscriptMessage[]) =>
  messages.slice(-120).map((message) => ({
    ...message,
    attachments: message.attachments?.map(({ data_url, ...attachment }) => attachment)
  }));

export const prepareSessionForStorage = (session: ChatSession): ChatSession => ({
  ...session,
  title: chatSessionTitle(session.messages),
  messages: sanitiseSessionMessages(session.messages)
});

export const sortChatSessions = (sessions: ChatSession[]) =>
  [...sessions].sort((a, b) => b.updated_at.localeCompare(a.updated_at));

export const restoreChatState = ({
  storedSessions,
  storedActiveID,
  legacyTranscript,
  createID,
  now
}: RestoreInput): RestoredChatState => {
  const sessions = parseStoredSessions(storedSessions);
  if (sessions.length > 0) {
    const sorted = sortChatSessions(sessions);
    const activeSessionID = sorted.some((session) => session.id === storedActiveID)
      ? String(storedActiveID)
      : sorted[0].id;
    return { sessions: sorted, activeSessionID };
  }

  const legacyMessages = parseLegacyTranscript(legacyTranscript);
  if (legacyMessages.length > 0) {
    const id = createID();
    return {
      sessions: [
        {
          id,
          title: chatSessionTitle(legacyMessages),
          messages: legacyMessages,
          created_at: now,
          updated_at: now,
          legacy: true
        }
      ],
      activeSessionID: id
    };
  }

  const id = createID();
  return {
    sessions: [
      {
        id,
        title: emptyChatTitle,
        messages: [],
        created_at: now,
        updated_at: now
      }
    ],
    activeSessionID: id
  };
};

const parseStoredSessions = (stored: string | null): ChatSession[] => {
  if (!stored) {
    return [];
  }
  try {
    const parsed = JSON.parse(stored);
    if (!Array.isArray(parsed)) {
      return [];
    }
    return parsed.filter(isChatSession).map((session) => {
      const messages = session.messages.filter(isTranscriptMessage);
      return {
        ...session,
        title: session.title.trim() || chatSessionTitle(messages),
        messages
      };
    });
  } catch {
    return [];
  }
};

const parseLegacyTranscript = (stored: string | null) => {
  if (!stored) {
    return [];
  }
  try {
    const parsed = JSON.parse(stored);
    return Array.isArray(parsed) ? parsed.filter(isTranscriptMessage) : [];
  } catch {
    return [];
  }
};

const isChatSession = (value: unknown): value is ChatSession => {
  if (!value || typeof value !== 'object') {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  return (
    typeof candidate.id === 'string' &&
    typeof candidate.title === 'string' &&
    typeof candidate.created_at === 'string' &&
    typeof candidate.updated_at === 'string' &&
    Array.isArray(candidate.messages)
  );
};
