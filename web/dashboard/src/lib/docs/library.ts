import { createMarkdownHeadingSlugger } from '@homelab/shared';

export type DocsSource = {
  path: string;
  content: string;
};

export type DocsHeading = {
  id: string;
  level: number;
  title: string;
};

export type DocsEntry = {
  slug: string;
  path: string;
  title: string;
  summary: string;
  content: string;
  body: string;
  headings: DocsHeading[];
  wordCount: number;
};

const docsPathPattern = /(?:^|\/)docs\/(.+)\.md$/;

export const docsSlugFromPath = (path: string) => {
  const match = path.replaceAll('\\', '/').match(docsPathPattern);
  const relativePath = match?.[1] || path.replace(/\.md$/i, '');
  return relativePath
    .split('/')
    .map((segment) =>
      segment
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9_-]+/g, '-')
        .replace(/^-+|-+$/g, '')
    )
    .filter(Boolean)
    .join('/');
};

export const docsPathFromSlug = (slug: string) => `${slug}.md`;

export const titleFromSlug = (slug: string) =>
  slug
    .split('/')
    .at(-1)
    ?.split(/[-_]+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join(' ') || 'Untitled';

export const cleanMarkdownText = (value: string) =>
  value
    .replace(/`([^`\n]+)`/g, '$1')
    .replace(/!\[([^\]\n]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]\n]+)\]\([^)]+\)/g, '$1')
    .replace(/[*_~#>`-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();

export const extractDocsTitle = (content: string, fallback: string) => {
  const heading = content.match(/^#\s+(.+)$/m)?.[1];
  return heading ? cleanMarkdownText(heading) : titleFromSlug(fallback);
};

export const stripLeadingTitle = (content: string) =>
  content.replace(/^#\s+.+(?:\r?\n){1,2}/, '').trimStart();

export const extractDocsSummary = (content: string, fallback: string) => {
  const withoutFences = stripLeadingTitle(content).replace(/```[\s\S]*?```/g, '');
  const paragraphs = withoutFences.split(/\n{2,}/).map((paragraph) => paragraph.trim());
  const paragraph =
    paragraphs.find(
      (candidate) =>
        candidate &&
        !candidate.startsWith('#') &&
        !candidate.startsWith('|') &&
        !candidate.match(/^[-*+]\s+/)
    ) || fallback;
  const summary = cleanMarkdownText(paragraph);
  return summary.length > 180 ? `${summary.slice(0, 177).trimEnd()}...` : summary;
};

export const extractDocsHeadings = (content: string): DocsHeading[] => {
  const slug = createMarkdownHeadingSlugger();
  const headings: DocsHeading[] = [];

  for (const match of content.matchAll(/^(#{2,4})\s+(.+)$/gm)) {
    const title = cleanMarkdownText(match[2]);
    headings.push({
      id: slug(match[2]),
      level: match[1].length,
      title
    });
  }

  return headings;
};

export const countWords = (content: string) => {
  const text = cleanMarkdownText(content.replace(/```[\s\S]*?```/g, ' '));
  return text ? text.split(/\s+/).length : 0;
};

export const buildDocsLibrary = (sources: DocsSource[]) =>
  sources
    .map((source): DocsEntry => {
      const slug = docsSlugFromPath(source.path);
      const title = extractDocsTitle(source.content, slug);
      const body = stripLeadingTitle(source.content);
      return {
        slug,
        path: docsPathFromSlug(slug),
        title,
        summary: extractDocsSummary(source.content, title),
        content: source.content,
        body,
        headings: extractDocsHeadings(body),
        wordCount: countWords(source.content)
      };
    })
    .sort((a, b) => a.title.localeCompare(b.title, 'en-GB'));

export const normaliseDocsQuery = (query: string) => cleanMarkdownText(query).toLowerCase();

export const filterDocs = (docs: DocsEntry[], query: string) => {
  const normalised = normaliseDocsQuery(query);
  if (!normalised) {
    return docs;
  }

  return docs.filter((doc) =>
    [doc.title, doc.summary, doc.path, doc.content].some((value) =>
      value.toLowerCase().includes(normalised)
    )
  );
};

export const findDocBySlug = (docs: DocsEntry[], slug = '') =>
  docs.find((doc) => doc.slug === slug.replace(/^\/+|\/+$/g, ''));
