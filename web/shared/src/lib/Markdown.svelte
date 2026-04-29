<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { mermaidConfigForTheme } from './mermaid';
  import { renderMarkdown } from './markdown';
  import { themeMode, type ThemeMode } from './theme';

  export let content = '';
  export let headingIds = false;

  let markdownEl: HTMLDivElement | undefined;
  let mounted = false;
  let mode: ThemeMode = 'light';
  let renderSequence = 0;

  $: renderedHtml = renderMarkdown(content, { headingIds });
  $: if (mounted && renderedHtml && mode) {
    void renderMermaidBlocks();
  }

  onMount(() => {
    mounted = true;
    const unsubscribe = themeMode.subscribe((value) => {
      mode = value;
      void renderMermaidBlocks();
    });
    void renderMermaidBlocks();

    return unsubscribe;
  });

  const makeDiagramID = (index: number) =>
    `homelabd-mermaid-${Date.now()}-${index}-${Math.random().toString(16).slice(2)}`;

  const showMermaidFallback = (node: HTMLElement, source: string) => {
    const pre = document.createElement('pre');
    const code = document.createElement('code');
    const note = document.createElement('p');

    code.className = 'language-mermaid';
    code.textContent = source;
    pre.append(code);
    note.className = 'mermaid-error';
    note.textContent = 'Unable to render Mermaid diagram.';
    node.replaceChildren(pre, note);
  };

  async function renderMermaidBlocks() {
    const root = markdownEl;
    if (!root) {
      return;
    }
    const sequence = ++renderSequence;
    await tick();
    if (sequence !== renderSequence || !root.isConnected) {
      return;
    }

    const diagrams = Array.from(
      root.querySelectorAll<HTMLElement>('.mermaid-diagram[data-mermaid-source]')
    );
    if (diagrams.length === 0) {
      return;
    }

    const { default: mermaid } = await import('mermaid');
    if (sequence !== renderSequence) {
      return;
    }
    mermaid.initialize(mermaidConfigForTheme(mode));

    for (const [index, node] of diagrams.entries()) {
      const source = node.dataset.mermaidSource || '';
      if (!source.trim()) {
        continue;
      }
      try {
        const { svg, bindFunctions } = await mermaid.render(makeDiagramID(index), source);
        if (sequence !== renderSequence || !node.isConnected) {
          return;
        }
        node.innerHTML = svg;
        const svgElement = node.querySelector('svg');
        svgElement?.setAttribute('role', 'img');
        svgElement?.setAttribute('aria-label', 'Mermaid diagram');
        bindFunctions?.(node);
      } catch {
        showMermaidFallback(node, source);
      }
    }
  }
</script>

<div class="markdown" bind:this={markdownEl}>
  {@html renderedHtml}
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

  .markdown :global(.mermaid-diagram) {
    box-sizing: border-box;
    min-width: 0;
    max-width: 100%;
    margin: 0.75rem 0;
    overflow-x: auto;
    padding: 0.85rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .markdown :global(.mermaid-diagram svg) {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0 auto;
  }

  .markdown :global(.mermaid-diagram pre) {
    margin: 0;
  }

  .markdown :global(.mermaid-error) {
    margin: 0.5rem 0 0;
    color: var(--danger-text, #991b1b);
    font-size: 0.86rem;
    font-weight: 750;
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
