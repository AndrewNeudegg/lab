import { describe, expect, test } from 'bun:test';
import { dataUrlAttachment, formatAttachmentSize, textAttachment } from './attachments';

describe('dashboard attachments', () => {
  test('formats attachment sizes for chips and task records', () => {
    expect(formatAttachmentSize(12)).toBe('12 B');
    expect(formatAttachmentSize(1536)).toBe('1.5 KB');
    expect(formatAttachmentSize(2 * 1024 * 1024)).toBe('2.0 MB');
  });

  test('builds text and data URL task attachments', () => {
    const text = textAttachment('browser-context.json', 'application/json', '{"url":"/chat"}');
    expect(text.name).toBe('browser-context.json');
    expect(text.text).toBe('{"url":"/chat"}');
    expect(text.size).toBeGreaterThan(0);

    const image = dataUrlAttachment('dashboard-screenshot.png', 'image/png', 'data:image/png;base64,AAAA', 3);
    expect(image.content_type).toBe('image/png');
    expect(image.data_url).toContain('base64');
    expect(image.size).toBe(3);
  });
});
