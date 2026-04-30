import { describe, expect, test } from 'bun:test';
import { createMarkdownHeadingSlugger, renderMarkdown, slugifyMarkdownHeading } from './markdown';

describe('renderMarkdown', () => {
  test('renders paragraphs with soft line breaks', () => {
    expect(renderMarkdown('first line\nsecond line')).toBe('<p>first line<br>second line</p>');
  });

  test('renders fenced code blocks and escapes code content', () => {
    expect(renderMarkdown('```ts\nconst value = "<safe>";\n```')).toBe(
      '<pre><code class="language-ts">const value = &quot;&lt;safe&gt;&quot;;</code></pre>'
    );
  });

  test('renders mermaid fences as diagram containers with escaped fallback source', () => {
    expect(renderMarkdown('```mermaid\ngraph TD\n  A["<safe>"] --> B\n```')).toBe(
      '<figure class="mermaid-diagram" data-mermaid-source="graph TD\n  A[&quot;&lt;safe&gt;&quot;] --&gt; B" data-mermaid-status="pending"><div class="mermaid-output" role="img" aria-label="Mermaid diagram" hidden></div><pre><code class="language-mermaid">graph TD\n  A[&quot;&lt;safe&gt;&quot;] --&gt; B</code></pre></figure>'
    );
  });

  test('renders mmd fences and strips Mermaid init directives from render source', () => {
    expect(
      renderMarkdown(
        '```mmd\n%%{init: {"theme": "forest"}}%%\nflowchart LR\n  Chat --> Docs\n```'
      )
    ).toBe(
      '<figure class="mermaid-diagram" data-mermaid-source="flowchart LR\n  Chat --&gt; Docs" data-mermaid-status="pending"><div class="mermaid-output" role="img" aria-label="Mermaid diagram" hidden></div><pre><code class="language-mmd">%%{init: {&quot;theme&quot;: &quot;forest&quot;}}%%\nflowchart LR\n  Chat --&gt; Docs</code></pre></figure>'
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

  test('adds stable unique heading ids when requested', () => {
    expect(renderMarkdown('## Task Flow\n\n### Task Flow', { headingIds: true })).toBe(
      '<h2 id="task-flow">Task Flow</h2><h3 id="task-flow-2">Task Flow</h3>'
    );
  });

  test('slugifies markdown headings consistently', () => {
    const slug = createMarkdownHeadingSlugger();
    expect(slugifyMarkdownHeading('Use `run` and [review](/docs)')).toBe('use-run-and-review');
    expect(slug('Use `run`')).toBe('use-run');
    expect(slug('Use run')).toBe('use-run-2');
  });

  test('renders raw URLs and images safely', () => {
    expect(renderMarkdown('See https://example.com/docs.\n\n![Alt <x>](/shot.png)')).toBe(
      '<p>See <a href="https://example.com/docs" rel="noreferrer">https://example.com/docs</a>.</p><p><img src="/shot.png" alt="Alt &lt;x&gt;" loading="lazy"></p>'
    );
  });

  test('escapes html and blocks unsafe links', () => {
    expect(renderMarkdown('<script>alert(1)</script> [x](javascript:alert(1))')).toBe(
      '<p>&lt;script&gt;alert(1)&lt;/script&gt; <a href="#" rel="noreferrer">x</a></p>'
    );
  });
});
