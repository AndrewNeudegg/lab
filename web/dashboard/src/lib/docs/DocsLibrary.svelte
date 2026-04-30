<script lang="ts">
  import { goto } from '$app/navigation';
  import { onMount } from 'svelte';
  import { Markdown, Navbar } from '@homelab/shared';
  import { filterDocs, type DocsEntry } from './library';

  export let docs: DocsEntry[] = [];
  export let selectedDoc: DocsEntry;
  export let selectedSlug = '';

  type DocsNavGroupDefinition = {
    id: string;
    label: string;
    slugs: string[];
  };

  const docsNavigationGroups: DocsNavGroupDefinition[] = [
    {
      id: 'start-here',
      label: 'Start here',
      slugs: ['dashboard', 'homelabctl']
    },
    {
      id: 'operator-workflows',
      label: 'Operator workflows',
      slugs: ['chat-commands', 'task-workflow', 'workflows']
    },
    {
      id: 'agents-runtime',
      label: 'Agents and runtime',
      slugs: ['remote-agents', 'external-agents', 'agent-tools', 'agentic-testing', 'supervisord']
    }
  ];

  const preferredDocOrder = docsNavigationGroups.flatMap((group) => group.slugs);
  const preferredDocSlugs = new Set(preferredDocOrder);

  let search = '';
  let jumpSlug = selectedSlug;
  let lastJumpSourceSlug = selectedSlug;
  let controlsReady = false;

  $: docsBySlug = new Map(docs.map((doc) => [doc.slug, doc]));
  $: navigationDocs = [
    ...preferredDocOrder
      .map((slug) => docsBySlug.get(slug))
      .filter((doc): doc is DocsEntry => Boolean(doc)),
    ...docs.filter((doc) => !preferredDocSlugs.has(doc.slug))
  ];
  $: filteredDocs = filterDocs(navigationDocs, search);
  $: searchActive = search.trim().length > 0;
  $: visibleDocGroups = searchActive
    ? [{ id: 'matches', label: 'Matches', docs: filteredDocs }]
    : [
        ...docsNavigationGroups
          .map((group) => ({
            id: group.id,
            label: group.label,
            docs: group.slugs
              .map((slug) => docsBySlug.get(slug))
              .filter((doc): doc is DocsEntry => Boolean(doc))
          }))
          .filter((group) => group.docs.length > 0),
        ...(() => {
          const ungroupedDocs = navigationDocs.filter((doc) => !preferredDocSlugs.has(doc.slug));
          return ungroupedDocs.length
            ? [{ id: 'more', label: 'More documents', docs: ungroupedDocs }]
            : [];
        })()
      ];
  $: articlePath = `./docs/${selectedDoc.path}`;
  $: readingTime = Math.max(1, Math.ceil(selectedDoc.wordCount / 220));
  $: resultText = searchActive
    ? `${filteredDocs.length} ${filteredDocs.length === 1 ? 'match' : 'matches'}`
    : `${navigationDocs.length} documents`;
  $: currentDocIndex = navigationDocs.findIndex((doc) => doc.slug === selectedSlug);
  $: previousDoc = currentDocIndex > 0 ? navigationDocs[currentDocIndex - 1] : undefined;
  $: nextDoc =
    currentDocIndex >= 0 && currentDocIndex < navigationDocs.length - 1
      ? navigationDocs[currentDocIndex + 1]
      : undefined;
  $: if (selectedSlug !== lastJumpSourceSlug) {
    jumpSlug = selectedSlug;
    lastJumpSourceSlug = selectedSlug;
  }

  const openSelectedDoc = (event: Event) => {
    const slug =
      event.currentTarget instanceof HTMLSelectElement ? event.currentTarget.value : jumpSlug;

    if (slug && slug !== selectedSlug) {
      void goto(`/docs/${slug}`);
    }
  };

  onMount(() => {
    controlsReady = true;
  });
</script>

<svelte:head>
  <title>{selectedDoc.title} - Docs - homelabd</title>
</svelte:head>

<Navbar title="Docs" subtitle="Library" current="/docs" />

<main class="docs-shell" data-docs-library-ready={controlsReady ? 'true' : 'false'}>
  <div class="docs-layout">
    <aside class="library" aria-label="Docs library">
      <div class="library-header">
        <p class="eyebrow">Documentation</p>
        <p class="library-title">Browse docs</p>
        <span>{navigationDocs.length} Markdown files</span>
      </div>

      <div class="mobile-jump">
        <label for="docs-jump">Current document</label>
        <select
          id="docs-jump"
          bind:value={jumpSlug}
          disabled={!controlsReady}
          on:change={openSelectedDoc}
          aria-label="Jump to document"
        >
          {#each navigationDocs as doc}
            <option value={doc.slug}>{doc.title}</option>
          {/each}
        </select>
      </div>

      <div class="search-block">
        <label class="search-label" for="docs-search">Search documentation</label>
        <input
          id="docs-search"
          type="search"
          bind:value={search}
          placeholder="Search titles, paths, and contents"
          autocomplete="off"
          aria-controls="docs-list"
        />
        <p id="docs-result-count" class="result-count" aria-live="polite">{resultText}</p>
      </div>

      <nav id="docs-list" class="doc-list" aria-label="Documents" aria-describedby="docs-result-count">
        {#if filteredDocs.length > 0}
          {#each visibleDocGroups as group}
            <section class="doc-group" aria-labelledby={`docs-group-${group.id}`}>
              <h2 id={`docs-group-${group.id}`}>{group.label}</h2>
              <div class="doc-group-links">
                {#each group.docs as doc}
                  <a
                    class:selected={doc.slug === selectedSlug}
                    href={`/docs/${doc.slug}`}
                    aria-current={doc.slug === selectedSlug ? 'page' : undefined}
                  >
                    <span class="doc-title">{doc.title}</span>
                    <span class="doc-meta">{doc.path}</span>
                    {#if searchActive}
                      <span class="doc-summary">{doc.summary}</span>
                    {/if}
                  </a>
                {/each}
              </div>
            </section>
          {/each}
        {:else}
          <p class="empty">No matching documents.</p>
        {/if}
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

      {#if previousDoc || nextDoc}
        <nav class="doc-pagination" aria-label="Document pagination">
          {#if previousDoc}
            <a class="previous" href={`/docs/${previousDoc.slug}`}>
              <span>Previous</span>
              <strong>{previousDoc.title}</strong>
            </a>
          {/if}

          {#if nextDoc}
            <a class="next" href={`/docs/${nextDoc.slug}`}>
              <span>Next</span>
              <strong>{nextDoc.title}</strong>
            </a>
          {/if}
        </nav>
      {/if}
    </article>
  </div>
</main>

<style>
  .docs-shell {
    padding: 1.25rem clamp(1rem, 2.5vw, 2rem) 3rem;
    min-height: calc(100vh - 4rem);
    background: var(--bg, #f5f7fb);
    color: var(--text, #172033);
  }

  .docs-layout {
    display: grid;
    grid-template-columns: minmax(14rem, 18rem) minmax(0, 1fr);
    gap: 1.5rem;
    align-items: start;
    max-width: 88rem;
    margin: 0 auto;
  }

  .library {
    position: sticky;
    top: 5.25rem;
    align-self: start;
    display: grid;
    gap: 1.1rem;
    max-height: calc(100vh - 6.5rem);
    min-width: 0;
    overflow: auto;
    padding: 1rem 0.8rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .library-header {
    display: grid;
    gap: 0.25rem;
    padding: 0 0.35rem;
  }

  .library-header p,
  .library-header span,
  .article-header p,
  .article-header h1,
  .article-header span,
  .doc-group h2,
  .toc h2,
  .empty,
  .result-count,
  .doc-pagination span,
  .doc-pagination strong {
    margin: 0;
  }

  .eyebrow,
  .article-header p {
    color: var(--muted, #64748b);
    font-size: 0.75rem;
    font-weight: 850;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .library-title {
    color: var(--text-strong, #0f172a);
    font-size: 1.05rem;
    font-weight: 900;
    line-height: 1.2;
    letter-spacing: 0;
  }

  .library-header span,
  .article-header span {
    color: var(--muted, #64748b);
    font-size: 0.86rem;
    font-weight: 750;
  }

  .mobile-jump,
  .search-block {
    display: grid;
    gap: 0.45rem;
  }

  .mobile-jump {
    display: none;
  }

  .mobile-jump label,
  .search-label {
    color: var(--text-strong, #0f172a);
    font-size: 0.82rem;
    font-weight: 850;
  }

  input,
  select {
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

  input:focus,
  select:focus {
    border-color: var(--accent, #2563eb);
    outline: 3px solid color-mix(in srgb, var(--accent, #2563eb) 22%, transparent);
  }

  .result-count {
    color: var(--muted, #64748b);
    font-size: 0.78rem;
    font-weight: 750;
  }

  .doc-list {
    display: grid;
    gap: 0.9rem;
    min-width: 0;
  }

  .doc-group {
    display: grid;
    gap: 0.3rem;
    min-width: 0;
  }

  .doc-group h2 {
    padding: 0 0.35rem;
    color: var(--muted, #64748b);
    font-size: 0.72rem;
    font-weight: 900;
    letter-spacing: 0.08em;
    line-height: 1.2;
    text-transform: uppercase;
  }

  .doc-group-links {
    display: grid;
    gap: 0.15rem;
    min-width: 0;
  }

  .doc-list a {
    display: grid;
    gap: 0.15rem;
    min-width: 0;
    padding: 0.55rem 0.6rem;
    border: 1px solid transparent;
    border-left: 3px solid transparent;
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
    border-color: var(--border-soft, #dbe3ef);
    border-left-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .doc-title {
    overflow: hidden;
    color: var(--text-strong, #0f172a);
    font-size: 0.9rem;
    font-weight: 850;
    line-height: 1.25;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .doc-meta,
  .doc-summary {
    overflow: hidden;
    color: var(--muted, #64748b);
    line-height: 1.35;
  }

  .doc-meta {
    font-size: 0.72rem;
    font-weight: 750;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .doc-summary {
    display: -webkit-box;
    font-size: 0.8rem;
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
    padding: 0.75rem 0 0;
  }

  .article-header {
    display: grid;
    gap: 0.45rem;
    max-width: 58rem;
    margin: 0 auto 1.75rem;
  }

  .article-header h1 {
    color: var(--text-strong, #0f172a);
    font-size: clamp(2rem, 4vw, 2.85rem);
    line-height: 1.08;
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

  .doc-pagination {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.8rem;
    max-width: 58rem;
    margin: 2.4rem auto 0;
  }

  .doc-pagination a {
    display: grid;
    gap: 0.25rem;
    min-width: 0;
    padding: 0.85rem 1rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    color: var(--text, #172033);
    background: var(--surface, #ffffff);
    text-decoration: none;
  }

  .doc-pagination a:hover {
    border-color: var(--border, #cbd5e1);
    background: var(--surface-muted, #f8fafc);
  }

  .doc-pagination span {
    color: var(--muted, #64748b);
    font-size: 0.75rem;
    font-weight: 850;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .doc-pagination strong {
    overflow-wrap: anywhere;
    color: var(--text-strong, #0f172a);
    font-size: 0.95rem;
    line-height: 1.3;
  }

  .doc-pagination .next {
    text-align: right;
  }

  .doc-pagination .next:first-child {
    grid-column: 2;
  }

  @media (max-width: 1060px) {
    .docs-layout {
      grid-template-columns: minmax(13rem, 16rem) minmax(0, 1fr);
      gap: 1rem;
    }

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
      padding: 0.75rem 0.8rem 2.5rem;
    }

    .docs-layout {
      grid-template-columns: minmax(0, 1fr);
      gap: 1rem;
    }

    .library {
      position: static;
      max-height: none;
      padding: 0.9rem;
    }

    .mobile-jump {
      display: grid;
    }

    .doc-list {
      max-height: 18rem;
      overflow-y: auto;
      padding-right: 0.2rem;
    }

    .doc-title {
      white-space: normal;
    }

    .doc-meta {
      display: none;
    }

    .article {
      padding: 0.25rem 0 0;
    }

    .article-header {
      margin-bottom: 1rem;
    }

    .article-header h1 {
      font-size: clamp(1.8rem, 10vw, 2.35rem);
    }

    .doc-pagination {
      grid-template-columns: minmax(0, 1fr);
      margin-top: 2rem;
    }

    .doc-pagination .next,
    .doc-pagination .next:first-child {
      grid-column: auto;
      text-align: left;
    }
  }
</style>
