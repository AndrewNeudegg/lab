import type { ChatInteractionStats, ChatTranscriptMessage } from '@homelab/shared';

const plural = (count: number, singular: string, pluralLabel = `${singular}s`) =>
  `${count} ${count === 1 ? singular : pluralLabel}`;

const positiveInteger = (value: number | undefined) =>
  typeof value === 'number' && Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;

const hasStats = (stats: ChatInteractionStats | undefined) =>
  Boolean(
    stats &&
      (positiveInteger(stats.model_turns) > 0 ||
        positiveInteger(stats.tool_calls) > 0 ||
        positiveInteger(stats.total_tokens) > 0 ||
        positiveInteger(stats.input_tokens) > 0 ||
        positiveInteger(stats.output_tokens) > 0)
  );

export const messageExchangeNumber = (messages: ChatTranscriptMessage[], index: number) => {
  if (index < 0 || index >= messages.length) {
    return 0;
  }
  let exchanges = 0;
  for (let i = 0; i <= index; i += 1) {
    if (messages[i]?.role === 'user') {
      exchanges += 1;
    }
  }
  return exchanges;
};

export const formatInteractionStats = (
  message: ChatTranscriptMessage,
  exchangeNumber: number
) => {
  const parts: string[] = [];
  if (exchangeNumber > 0) {
    parts.push(`Exchange ${exchangeNumber}`);
  }

  if (message.role === 'assistant' && hasStats(message.stats)) {
    const modelTurns = positiveInteger(message.stats?.model_turns);
    const toolCalls = positiveInteger(message.stats?.tool_calls);
    const totalTokens =
      positiveInteger(message.stats?.total_tokens) ||
      positiveInteger(message.stats?.input_tokens) + positiveInteger(message.stats?.output_tokens);

    if (modelTurns > 0) {
      parts.push(plural(modelTurns, 'model turn'));
      parts.push(plural(toolCalls, 'tool call'));
    } else if (toolCalls > 0) {
      parts.push(plural(toolCalls, 'tool call'));
    }
    if (totalTokens > 0) {
      parts.push(plural(totalTokens, 'token'));
    }
  }

  return parts.join(' · ');
};

