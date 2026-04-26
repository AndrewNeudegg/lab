<script lang="ts">
  import { renderMarkdown } from './markdown';

  export let content = '';
  export let headingIds = false;
</script>

<div class="markdown">
  {@html renderMarkdown(content, { headingIds })}
</div>

<style>
  .markdown {
    color: #172033;
    line-height: 1.5;
    min-width: 0;
    max-width: 100%;
    overflow-wrap: anywhere;
  }

  .markdown :global(*) {
    overflow-wrap: anywhere;
  }

  .markdown :global(*:first-child) {
    margin-top: 0;
  }

  .markdown :global(*:last-child) {
    margin-bottom: 0;
  }

  .markdown :global(p) {
    margin: 0.45rem 0;
  }

  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3),
  .markdown :global(h4),
  .markdown :global(h5),
  .markdown :global(h6) {
    margin: 0.7rem 0 0.35rem;
    color: #111827;
    line-height: 1.22;
    letter-spacing: 0;
  }

  .markdown :global(h1) {
    font-size: 1.35rem;
  }

  .markdown :global(h2) {
    font-size: 1.18rem;
  }

  .markdown :global(h3),
  .markdown :global(h4),
  .markdown :global(h5),
  .markdown :global(h6) {
    font-size: 1rem;
  }

  .markdown :global(ul),
  .markdown :global(ol) {
    margin: 0.45rem 0;
    padding-left: 1.25rem;
  }

  .markdown :global(li + li) {
    margin-top: 0.2rem;
  }

  .markdown :global(blockquote) {
    margin: 0.55rem 0;
    padding: 0.2rem 0 0.2rem 0.8rem;
    border-left: 3px solid #cbd5e1;
    color: #475569;
  }

  .markdown :global(pre) {
    box-sizing: border-box;
    min-width: 0;
    min-inline-size: 0;
    width: 100%;
    max-width: 100%;
    margin: 0.6rem 0;
    overflow-x: auto;
    padding: 0.75rem;
    border: 1px solid #cbd5e1;
    border-radius: 0.45rem;
    background: #0f172a;
    color: #e2e8f0;
    line-height: 1.45;
  }

  .markdown :global(code) {
    padding: 0.12rem 0.28rem;
    border-radius: 0.28rem;
    background: #e8eef7;
    color: #0f172a;
    font-family:
      "SFMono-Regular", Consolas, "Liberation Mono", Menlo, ui-monospace, monospace;
    font-size: 0.9em;
  }

  .markdown :global(pre code) {
    display: block;
    min-width: 100%;
    width: max-content;
    padding: 0;
    background: transparent;
    color: inherit;
    overflow-wrap: normal;
    white-space: pre;
  }

  .markdown :global(a) {
    color: #1d4ed8;
    font-weight: 700;
  }

  .markdown :global(img) {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0.75rem 0;
    border: 1px solid #cbd5e1;
    border-radius: 0.45rem;
  }

  .markdown :global(table) {
    display: block;
    max-width: 100%;
    margin: 0.75rem 0;
    overflow-x: auto;
    border-collapse: collapse;
  }

  .markdown :global(th),
  .markdown :global(td) {
    padding: 0.45rem 0.55rem;
    border: 1px solid #cbd5e1;
    text-align: left;
    vertical-align: top;
  }

  .markdown :global(th) {
    background: #f1f5f9;
    color: #0f172a;
    font-weight: 800;
  }
</style>
