import { buildDocsLibrary, findDocBySlug, type DocsSource } from './library';

const modules = import.meta.glob('../../../../../docs/**/*.md', {
  eager: true,
  import: 'default',
  query: '?raw'
});

const sources: DocsSource[] = Object.entries(modules).map(([path, content]) => ({
  path,
  content: String(content)
}));

export const docs = buildDocsLibrary(sources);
export const defaultDoc = docs.find((doc) => doc.slug === 'dashboard') || docs[0];
export const getDocBySlug = (slug = '') => findDocBySlug(docs, slug);
