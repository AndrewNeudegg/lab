<script lang="ts" context="module">
  let markdownInstance = 0;
</script>

<script lang="ts">
  import { afterUpdate, onMount, tick } from 'svelte';
  import { mermaidBrandThemeVariables, type BrandDiagramMode } from './brand';
  import { renderMarkdown } from './markdown';

  export let content = '';
  export let headingIds = false;
  export let navigate: ((href: string) => void) | undefined = undefined;

  let root: HTMLDivElement | undefined;
  let mounted = false;
  let renderVersion = 0;
  let renderQueued = false;
  let themeMode: BrandDiagramMode = 'light';
  const instanceID = ++markdownInstance;

  const currentThemeMode = (): BrandDiagramMode =>
    typeof document !== 'undefined' && document.documentElement.dataset.theme === 'dark'
      ? 'dark'
      : 'light';

  const resetMermaidDiagrams = () => {
    if (!root) {
      return;
    }
    for (const diagram of root.querySelectorAll<HTMLElement>('.mermaid-diagram')) {
      delete diagram.dataset.mermaidRendered;
      diagram.dataset.mermaidStatus = 'pending';
      const output = diagram.querySelector<HTMLElement>('.mermaid-output');
      const fallback = diagram.querySelector<HTMLElement>('pre');
      if (output) {
        output.replaceChildren();
        output.hidden = true;
      }
      if (fallback) {
        fallback.hidden = false;
      }
    }
  };

  const renderMermaidDiagrams = async () => {
    if (!mounted || !root) {
      return;
    }
    await tick();
    const diagrams = Array.from(
      root.querySelectorAll<HTMLElement>('.mermaid-diagram[data-mermaid-source]')
    );
    if (diagrams.length === 0) {
      return;
    }

    const version = ++renderVersion;
    const mode = currentThemeMode();
    if (themeMode !== mode) {
      themeMode = mode;
    }
    const { default: mermaid } = await import('mermaid');
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: 'strict',
      secure: ['securityLevel', 'theme', 'themeVariables', 'themeCSS', 'darkMode', 'fontFamily'],
      theme: 'base',
      darkMode: mode === 'dark',
      fontFamily:
        'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
      themeVariables: mermaidBrandThemeVariables(mode)
    });

    await Promise.all(
      diagrams.map(async (diagram, index) => {
        const source = diagram.dataset.mermaidSource || '';
        const fingerprint = `${mode}:${source}`;
        if (!source.trim() || diagram.dataset.mermaidRendered === fingerprint) {
          return;
        }
        const output = diagram.querySelector<HTMLElement>('.mermaid-output');
        const fallback = diagram.querySelector<HTMLElement>('pre');
        if (!output) {
          return;
        }
        diagram.dataset.mermaidStatus = 'rendering';
        try {
          const { svg, bindFunctions } = await mermaid.render(
            `markdown-mermaid-${instanceID}-${version}-${index}`,
            source
          );
          if (version !== renderVersion) {
            return;
          }
          output.innerHTML = svg;
          bindFunctions?.(output);
          output.hidden = false;
          if (fallback) {
            fallback.hidden = true;
          }
          diagram.dataset.mermaidRendered = fingerprint;
          diagram.dataset.mermaidStatus = 'rendered';
        } catch (error) {
          if (version !== renderVersion) {
            return;
          }
          output.replaceChildren();
          output.hidden = true;
          if (fallback) {
            fallback.hidden = false;
          }
          diagram.dataset.mermaidStatus = 'error';
          diagram.dataset.mermaidError = error instanceof Error ? error.message : String(error);
        }
      })
    );
  };

  const queueMermaidRender = () => {
    if (!mounted || renderQueued) {
      return;
    }
    renderQueued = true;
    void tick().then(() => {
      renderQueued = false;
      void renderMermaidDiagrams();
    });
  };

  const handleMarkdownClick = (event: MouseEvent) => {
    if (
      !navigate ||
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    ) {
      return;
    }
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const anchor = target.closest('a');
    if (!(anchor instanceof HTMLAnchorElement) || !root?.contains(anchor)) {
      return;
    }
    const href = anchor.getAttribute('href') || '';
    if (!href || anchor.target || anchor.hasAttribute('download')) {
      return;
    }
    if (!href.startsWith('/') && !href.startsWith('#')) {
      return;
    }
    event.preventDefault();
    navigate(href);
  };

  onMount(() => {
    mounted = true;
    themeMode = currentThemeMode();
    root?.addEventListener('click', handleMarkdownClick);
    const observer = new MutationObserver(() => {
      const nextMode = currentThemeMode();
      if (nextMode !== themeMode) {
        themeMode = nextMode;
        resetMermaidDiagrams();
        queueMermaidRender();
      }
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });
    queueMermaidRender();
    return () => {
      mounted = false;
      renderVersion += 1;
      root?.removeEventListener('click', handleMarkdownClick);
      observer.disconnect();
    };
  });

  afterUpdate(() => {
    queueMermaidRender();
  });
</script>

<div class="markdown" bind:this={root}>
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

  .markdown :global(.mermaid-diagram) {
    box-sizing: border-box;
    min-width: 0;
    max-width: 100%;
    margin: 0.75rem 0;
    overflow-x: auto;
    padding: 0.75rem;
    border: 1px solid var(--markdown-diagram-border, #cbd5e1);
    border-radius: 0.5rem;
    background: var(--markdown-diagram-bg, #f8fafc);
    color: var(--markdown-diagram-text, #172033);
  }

  .markdown :global(.mermaid-output) {
    min-width: min(38rem, 100%);
  }

  .markdown :global(.mermaid-output svg) {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0 auto;
  }

  .markdown :global(.mermaid-diagram pre) {
    margin: 0;
  }

  .markdown :global(.mermaid-diagram[data-mermaid-status='error']) {
    border-color: #d97706;
    background: #fffbeb;
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

  :global(html[data-theme='dark'] .markdown) {
    --markdown-diagram-bg: #0f172a;
    --markdown-diagram-text: #e2e8f0;
    --markdown-diagram-border: #334155;
  }

  :global(html[data-theme='dark'] .markdown .mermaid-diagram[data-mermaid-status='error']) {
    border-color: #fbbf24;
    background: #422006;
  }
</style>
