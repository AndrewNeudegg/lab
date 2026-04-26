const escapeHtml = (value: string) =>
  value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;');

const escapeAttribute = (value: string) => escapeHtml(value).replaceAll('`', '&#96;');

const sanitizeHref = (href: string) => {
  const trimmed = href.trim();
  if (/^(https?:|mailto:|\/|#)/i.test(trimmed)) {
    return trimmed;
  }
  return '#';
};

const renderInlineMarkdown = (value: string): string => {
  const codeSpans: string[] = [];
  let rendered = value.replace(/`([^`\n]+)`/g, (_match, code: string) => {
    const token = `\u0000CODE${codeSpans.length}\u0000`;
    codeSpans.push(`<code>${escapeHtml(code)}</code>`);
    return token;
  });

  rendered = escapeHtml(rendered);
  rendered = rendered.replace(
    /\[([^\]\n]+)\]\(([^ \n]+)\)/g,
    (_match, label: string, href: string) =>
      `<a href="${escapeAttribute(sanitizeHref(href))}" rel="noreferrer">${label}</a>`
  );
  rendered = rendered.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
  rendered = rendered.replace(/__([^_\n]+)__/g, '<strong>$1</strong>');
  rendered = rendered.replace(/(^|[\s([])\*([^*\n]+)\*/g, '$1<em>$2</em>');
  rendered = rendered.replace(/(^|[\s([])_([^_\n]+)_/g, '$1<em>$2</em>');

  return codeSpans.reduce(
    (current, code, index) => current.replace(`\u0000CODE${index}\u0000`, code),
    rendered
  );
};

type ListState = {
  kind: 'ol' | 'ul';
  items: string[];
};

export const renderMarkdown = (source: string) => {
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  const blocks: string[] = [];
  const paragraph: string[] = [];
  let list: ListState | undefined;
  let quote: string[] = [];
  let fenceLanguage = '';
  let fenceLines: string[] | undefined;

  const flushParagraph = () => {
    if (paragraph.length === 0) {
      return;
    }
    blocks.push(`<p>${paragraph.map(renderInlineMarkdown).join('<br>')}</p>`);
    paragraph.length = 0;
  };

  const flushList = () => {
    if (!list) {
      return;
    }
    const items = list.items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join('');
    blocks.push(`<${list.kind}>${items}</${list.kind}>`);
    list = undefined;
  };

  const flushQuote = () => {
    if (quote.length === 0) {
      return;
    }
    blocks.push(`<blockquote>${renderMarkdown(quote.join('\n'))}</blockquote>`);
    quote = [];
  };

  const flushTextBlocks = () => {
    flushParagraph();
    flushList();
    flushQuote();
  };

  const pushListItem = (kind: ListState['kind'], value: string) => {
    flushParagraph();
    flushQuote();
    if (list && list.kind !== kind) {
      flushList();
    }
    if (!list) {
      list = { kind, items: [] };
    }
    list.items.push(value);
  };

  for (const line of lines) {
    const fenceMatch = line.match(/^```([A-Za-z0-9_.+-]*)\s*$/);
    if (fenceLines) {
      if (fenceMatch) {
        blocks.push(
          `<pre><code${fenceLanguage ? ` class="language-${escapeAttribute(fenceLanguage)}"` : ''}>${escapeHtml(
            fenceLines.join('\n')
          )}</code></pre>`
        );
        fenceLines = undefined;
        fenceLanguage = '';
      } else {
        fenceLines.push(line);
      }
      continue;
    }

    if (fenceMatch) {
      flushTextBlocks();
      fenceLanguage = fenceMatch[1] || '';
      fenceLines = [];
      continue;
    }

    if (!line.trim()) {
      flushTextBlocks();
      continue;
    }

    const headingMatch = line.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      flushTextBlocks();
      const level = headingMatch[1].length;
      blocks.push(`<h${level}>${renderInlineMarkdown(headingMatch[2])}</h${level}>`);
      continue;
    }

    const quoteMatch = line.match(/^>\s?(.*)$/);
    if (quoteMatch) {
      flushParagraph();
      flushList();
      quote.push(quoteMatch[1]);
      continue;
    }

    const unorderedMatch = line.match(/^\s*[-*+]\s+(.+)$/);
    if (unorderedMatch) {
      pushListItem('ul', unorderedMatch[1]);
      continue;
    }

    const orderedMatch = line.match(/^\s*\d+\.\s+(.+)$/);
    if (orderedMatch) {
      pushListItem('ol', orderedMatch[1]);
      continue;
    }

    flushList();
    flushQuote();
    paragraph.push(line);
  }

  if (fenceLines) {
    blocks.push(
      `<pre><code${fenceLanguage ? ` class="language-${escapeAttribute(fenceLanguage)}"` : ''}>${escapeHtml(
        fenceLines.join('\n')
      )}</code></pre>`
    );
  }
  flushTextBlocks();

  return blocks.join('');
};
