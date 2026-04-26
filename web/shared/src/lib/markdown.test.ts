import { describe, expect, test } from 'bun:test';
import { renderMarkdown } from './markdown';

describe('renderMarkdown', () => {
  test('renders paragraphs with soft line breaks', () => {
    expect(renderMarkdown('first line\nsecond line')).toBe('<p>first line<br>second line</p>');
  });

  test('renders fenced code blocks and escapes code content', () => {
    expect(renderMarkdown('```ts\nconst value = "<safe>";\n```')).toBe(
      '<pre><code class="language-ts">const value = &quot;&lt;safe&gt;&quot;;</code></pre>'
    );
  });

  test('renders common inline markdown', () => {
    expect(renderMarkdown('Use **bold**, _emphasis_, and `code`.')).toBe(
      '<p>Use <strong>bold</strong>, <em>emphasis</em>, and <code>code</code>.</p>'
    );
  });

  test('renders lists, headings, quotes, and links', () => {
    expect(renderMarkdown('# Title\n\n- [docs](/docs)\n- item\n\n> quoted')).toBe(
      '<h1>Title</h1><ul><li><a href="/docs" rel="noreferrer">docs</a></li><li>item</li></ul><blockquote><p>quoted</p></blockquote>'
    );
  });

  test('escapes html and blocks unsafe links', () => {
    expect(renderMarkdown('<script>alert(1)</script> [x](javascript:alert(1))')).toBe(
      '<p>&lt;script&gt;alert(1)&lt;/script&gt; <a href="#" rel="noreferrer">x</a></p>'
    );
  });
});
