import { describe, expect, test } from 'bun:test';
import type { ChatTranscriptMessage } from '@homelab/shared';
import { formatInteractionStats, messageExchangeNumber } from './interaction-stats';

const message = (
  role: ChatTranscriptMessage['role'],
  id: string,
  stats?: ChatTranscriptMessage['stats']
): ChatTranscriptMessage => ({
  id,
  role,
  content: id,
  time: 'Now',
  stats
});

describe('chat interaction stats', () => {
  test('counts exchanges from user messages without counting the welcome assistant bubble', () => {
    const messages = [
      message('assistant', 'welcome'),
      message('user', 'user-1'),
      message('assistant', 'assistant-1'),
      message('user', 'user-2')
    ];

    expect(messageExchangeNumber(messages, 0)).toBe(0);
    expect(messageExchangeNumber(messages, 1)).toBe(1);
    expect(messageExchangeNumber(messages, 2)).toBe(1);
    expect(messageExchangeNumber(messages, 3)).toBe(2);
  });

  test('formats assistant model, tool, and token counts compactly', () => {
    expect(
      formatInteractionStats(
        message('assistant', 'assistant-1', {
          model_turns: 2,
          tool_calls: 1,
          input_tokens: 30,
          output_tokens: 12
        }),
        3
      )
    ).toBe('Exchange 3 · 2 model turns · 1 tool call · 42 tokens');
  });

  test('shows zero tool calls when a model answered without tools', () => {
    expect(formatInteractionStats(message('assistant', 'assistant-1', { model_turns: 1 }), 1)).toBe(
      'Exchange 1 · 1 model turn · 0 tool calls'
    );
  });

  test('keeps user message footers to the exchange count', () => {
    expect(
      formatInteractionStats(
        message('user', 'user-2', {
          model_turns: 3,
          tool_calls: 4,
          total_tokens: 100
        }),
        2
      )
    ).toBe('Exchange 2');
  });
});
