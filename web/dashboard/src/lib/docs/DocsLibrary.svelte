<script lang="ts">
  import { Markdown, Navbar } from '@homelab/shared';
  import { filterDocs, type DocsEntry } from './library';

  export let docs: DocsEntry[] = [];
  export let selectedDoc: DocsEntry;
  export let selectedSlug = '';

  let search = '';

  $: filteredDocs = filterDocs(docs, search);
  $: articlePath = `./docs/${selectedDoc.path}`;
  $: readingTime = Math.max(1, Math.ceil(selectedDoc.wordCount / 220));
</script>

<svelte:head>
  <title>{selectedDoc.title} - Docs - homelabd</title>
</svelte:head>

<Navbar title="Docs" subtitle="Library" current="/docs" />

<main class="docs-shell">
  <aside class="library" aria-label="Docs library">
    <div class="library-header">
      <p>Catalogue</p>
      <h1>Docs</h1>
      <span>{docs.length} Markdown files</span>
    </div>

    <label class="search-label" for="docs-search">Search documentation</label>
    <input
      id="docs-search"
      type="search"
      bind:value={search}
      placeholder="Search docs"
      autocomplete="off"
      aria-controls="docs-list"
    />

    <nav id="docs-list" class="doc-list" aria-label="Documents">
      {#each filteredDocs as doc}
        <a
          class:selected={doc.slug === selectedSlug}
          href={`/docs/${doc.slug}`}
          aria-current={doc.slug === selectedSlug ? 'page' : undefined}
        >
          <strong>{doc.title}</strong>
          <span>{doc.summary}</span>
        </a>
      {:else}
        <p class="empty">No matching documents.</p>
      {/each}
    </nav>
  </aside>

  <article class="article" aria-labelledby="doc-title">
    <header class="article-header">
      <p>{articlePath}</p>
      <h1 id="doc-title">{selectedDoc.title}</h1>
      <span>{readingTime} min read</span>
    </header>

    <div class="article-layout">
      <div class="content">
        <Markdown content={selectedDoc.body} headingIds />
      </div>

      {#if selectedDoc.headings.length > 0}
        <nav class="toc" aria-label="On this page">
          <h2>On This Page</h2>
          {#each selectedDoc.headings as heading}
            <a class={`level-${heading.level}`} href={`#${heading.id}`}>{heading.title}</a>
          {/each}
        </nav>
      {/if}
    </div>
  </article>
</main>

<style>
  .docs-shell {
    display: grid;
    grid-template-columns: minmax(17rem, 21rem) minmax(0, 1fr);
    min-height: calc(100vh - 4rem);
    background: var(--bg, #f5f7fb);
    color: var(--text, #172033);
  }

  .library {
    position: sticky;
    top: 4rem;
    align-self: start;
    display: grid;
    gap: 1rem;
    height: calc(100vh - 4rem);
    min-width: 0;
    overflow: auto;
    padding: 1.25rem;
    border-right: 1px solid var(--border-soft, #dbe3ef);
    background: var(--surface, #ffffff);
  }

  .library-header {
    display: grid;
    gap: 0.25rem;
  }

  .library-header p,
  .library-header h1,
  .library-header span,
  .article-header p,
  .article-header h1,
  .article-header span,
  .toc h2,
  .empty {
    margin: 0;
  }

  .library-header p,
  .article-header p {
    color: var(--muted, #64748b);
    font-size: 0.75rem;
    font-weight: 850;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .library-header h1 {
    color: var(--text-strong, #0f172a);
    font-size: 1.45rem;
    line-height: 1.1;
    letter-spacing: 0;
  }

  .library-header span,
  .article-header span {
    color: var(--muted, #64748b);
    font-size: 0.86rem;
    font-weight: 750;
  }

  .search-label {
    color: var(--text-strong, #0f172a);
    font-size: 0.82rem;
    font-weight: 850;
  }

  input {
    box-sizing: border-box;
    width: 100%;
    min-height: 2.65rem;
    padding: 0 0.85rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.55rem;
    color: var(--text, #172033);
    background: var(--surface-muted, #f8fafc);
    font: inherit;
    font-size: 0.95rem;
  }

  input:focus {
    border-color: var(--accent, #2563eb);
    outline: 3px solid color-mix(in srgb, var(--accent, #2563eb) 22%, transparent);
  }

  .doc-list {
    display: grid;
    gap: 0.45rem;
    min-width: 0;
  }

  .doc-list a {
    display: grid;
    gap: 0.25rem;
    min-width: 0;
    padding: 0.7rem 0.75rem;
    border: 1px solid transparent;
    border-radius: 0.5rem;
    color: var(--text, #172033);
    text-decoration: none;
  }

  .doc-list a:hover {
    border-color: var(--border, #cbd5e1);
    background: var(--surface-muted, #f8fafc);
  }

  .doc-list a.selected,
  .doc-list a[aria-current='page'] {
    border-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .doc-list strong {
    overflow: hidden;
    color: var(--text-strong, #0f172a);
    font-size: 0.94rem;
    line-height: 1.25;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .doc-list span {
    display: -webkit-box;
    overflow: hidden;
    color: var(--muted, #64748b);
    font-size: 0.82rem;
    line-height: 1.35;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    -webkit-line-clamp: 2;
  }

  .empty {
    padding: 0.75rem;
    color: var(--muted, #64748b);
    font-size: 0.9rem;
  }

  .article {
    min-width: 0;
    padding: 2rem clamp(1rem, 4vw, 3rem) 3rem;
  }

  .article-header {
    display: grid;
    gap: 0.45rem;
    max-width: 58rem;
    margin: 0 auto 1.5rem;
  }

  .article-header h1 {
    color: var(--text-strong, #0f172a);
    font-size: clamp(2rem, 5vw, 3.35rem);
    line-height: 1.04;
    letter-spacing: 0;
  }

  .article-layout {
    display: grid;
    grid-template-columns: minmax(0, 58rem) minmax(12rem, 15rem);
    gap: 2rem;
    justify-content: center;
    align-items: start;
  }

  .content {
    min-width: 0;
  }

  .content :global(.markdown) {
    color: var(--text, #172033);
    font-size: 1rem;
    line-height: 1.72;
  }

  .content :global(.markdown h1),
  .content :global(.markdown h2),
  .content :global(.markdown h3),
  .content :global(.markdown h4) {
    scroll-margin-top: 5.5rem;
    color: var(--text-strong, #0f172a);
  }

  .content :global(.markdown h2) {
    margin-top: 2rem;
    padding-top: 1.1rem;
    border-top: 1px solid var(--border-soft, #dbe3ef);
    font-size: 1.45rem;
  }

  .content :global(.markdown h3) {
    margin-top: 1.5rem;
    font-size: 1.15rem;
  }

  .content :global(.markdown p),
  .content :global(.markdown li) {
    max-width: 72ch;
  }

  .content :global(.markdown pre) {
    border-color: var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
  }

  .content :global(.markdown code) {
    border: 1px solid var(--border-soft, #dbe3ef);
  }

  .content :global(.markdown pre code) {
    border: 0;
  }

  .toc {
    position: sticky;
    top: 5.25rem;
    display: grid;
    gap: 0.35rem;
    padding-left: 1rem;
    border-left: 1px solid var(--border-soft, #dbe3ef);
  }

  .toc h2 {
    color: var(--text-strong, #0f172a);
    font-size: 0.78rem;
    font-weight: 900;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .toc a {
    color: var(--muted, #64748b);
    font-size: 0.85rem;
    font-weight: 750;
    line-height: 1.35;
    text-decoration: none;
  }

  .toc a:hover {
    color: var(--accent, #2563eb);
  }

  .toc .level-3 {
    padding-left: 0.65rem;
  }

  .toc .level-4 {
    padding-left: 1.3rem;
  }

  @media (max-width: 1060px) {
    .article-layout {
      grid-template-columns: minmax(0, 1fr);
    }

    .toc {
      position: static;
      grid-row: 1;
      max-width: 58rem;
      padding: 0 0 1rem;
      border-left: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }
  }

  @media (max-width: 760px) {
    .docs-shell {
      grid-template-columns: 1fr;
    }

    .library {
      position: static;
      height: auto;
      max-height: none;
      padding: 1rem;
      border-right: 0;
      border-bottom: 1px solid var(--border-soft, #dbe3ef);
    }

    .doc-list {
      grid-auto-flow: column;
      grid-auto-columns: minmax(13rem, 72vw);
      overflow-x: auto;
      padding-bottom: 0.15rem;
      scroll-snap-type: x mandatory;
    }

    .doc-list a {
      scroll-snap-align: start;
    }

    .doc-list strong {
      white-space: normal;
    }

    .article {
      padding: 1.25rem 1rem 2.5rem;
    }

    .article-header {
      margin-bottom: 1rem;
    }

    .article-header h1 {
      font-size: clamp(1.8rem, 10vw, 2.35rem);
    }
  }
</style>
