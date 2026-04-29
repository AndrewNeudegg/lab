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

export type MarkdownRenderOptions = {
  headingIds?: boolean;
};

export const slugifyMarkdownHeading = (value: string) => {
  const slug = value
    .replace(/`([^`\n]+)`/g, '$1')
    .replace(/!\[([^\]\n]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]\n]+)\]\([^)]+\)/g, '$1')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '');

  return slug || 'section';
};

export const createMarkdownHeadingSlugger = () => {
  const seen = new Map<string, number>();

  return (value: string) => {
    const base = slugifyMarkdownHeading(value);
    const count = seen.get(base) || 0;
    seen.set(base, count + 1);
    return count === 0 ? base : `${base}-${count + 1}`;
  };
};

const createHtmlToken = (tokens: string[], html: string) => {
  const token = `\u0000HTML${tokens.length}\u0000`;
  tokens.push(html);
  return token;
};

const renderInlineMarkdown = (value: string): string => {
  const codeSpans: string[] = [];
  const htmlTokens: string[] = [];
  let rendered = value.replace(/`([^`\n]+)`/g, (_match, code: string) => {
    const token = `\u0000CODE${codeSpans.length}\u0000`;
    codeSpans.push(`<code>${escapeHtml(code)}</code>`);
    return token;
  });

  rendered = rendered.replace(
    /!\[([^\]\n]*)\]\(([^ \n]+)\)/g,
    (_match, alt: string, src: string) =>
      createHtmlToken(
        htmlTokens,
        `<img src="${escapeAttribute(sanitizeHref(src))}" alt="${escapeAttribute(alt)}" loading="lazy">`
      )
  );
  rendered = rendered.replace(
    /\[([^\]\n]+)\]\(([^ \n]+)\)/g,
    (_match, label: string, href: string) =>
      createHtmlToken(
        htmlTokens,
        `<a href="${escapeAttribute(sanitizeHref(href))}" rel="noreferrer">${escapeHtml(label)}</a>`
      )
  );
  rendered = escapeHtml(rendered);
  rendered = rendered.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
  rendered = rendered.replace(/__([^_\n]+)__/g, '<strong>$1</strong>');
  rendered = rendered.replace(/(^|[\s([])\*([^*\n]+)\*/g, '$1<em>$2</em>');
  rendered = rendered.replace(/(^|[\s([])_([^_\n]+)_/g, '$1<em>$2</em>');
  rendered = rendered.replace(/https?:\/\/[^\s<]+[^\s<.,;:!?")\]]/g, (url) => {
    const href = url.replaceAll('&amp;', '&');
    return `<a href="${escapeAttribute(sanitizeHref(href))}" rel="noreferrer">${url}</a>`;
  });

  const withHtmlTokens = htmlTokens.reduce(
    (current, html, index) => current.replace(`\u0000HTML${index}\u0000`, html),
    rendered
  );

  return codeSpans.reduce(
    (current, code, index) => current.replace(`\u0000CODE${index}\u0000`, code),
    withHtmlTokens
  );
};

type ListState = {
  kind: 'ol' | 'ul';
  items: string[];
};

const renderFencedCode = (language: string, lines: string[]) => {
  const code = lines.join('\n');
  const normalizedLanguage = language.toLowerCase();
  const languageClass = language ? ` class="language-${escapeAttribute(language)}"` : '';

  if (normalizedLanguage === 'mermaid') {
    return [
      `<figure class="mermaid-diagram" data-mermaid-source="${escapeAttribute(code)}" data-mermaid-status="pending">`,
      '<div class="mermaid-output" role="img" aria-label="Mermaid diagram" hidden></div>',
      `<pre><code${languageClass}>${escapeHtml(code)}</code></pre>`,
      '</figure>'
    ].join('');
  }

  return `<pre><code${languageClass}>${escapeHtml(code)}</code></pre>`;
};

export const renderMarkdown = (source: string, options: MarkdownRenderOptions = {}) => {
  const lines = source.replace(/\r\n?/g, '\n').split('\n');
  const blocks: string[] = [];
  const paragraph: string[] = [];
  const headingSlug = createMarkdownHeadingSlugger();
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
        blocks.push(renderFencedCode(fenceLanguage, fenceLines));
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
      const id = options.headingIds ? ` id="${escapeAttribute(headingSlug(headingMatch[2]))}"` : '';
      blocks.push(`<h${level}${id}>${renderInlineMarkdown(headingMatch[2])}</h${level}>`);
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
    blocks.push(renderFencedCode(fenceLanguage, fenceLines));
  }
  flushTextBlocks();

  return blocks.join('');
};
