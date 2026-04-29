<script lang="ts">
  import { afterUpdate, onMount, tick } from 'svelte';
  import { renderMarkdown } from './markdown';
  import { themeMode, type ThemeMode } from './theme';

  export let content = '';
  export let headingIds = false;

  type MermaidModule = typeof import('mermaid').default;

  const mermaidThemeVariables: Record<ThemeMode, Record<string, string>> = {
    light: {
      background: '#ffffff',
      primaryColor: '#eef5ff',
      primaryBorderColor: '#2563eb',
      primaryTextColor: '#0f172a',
      secondaryColor: '#f0fdf4',
      secondaryBorderColor: '#1f6f4a',
      secondaryTextColor: '#0f172a',
      tertiaryColor: '#fffbeb',
      tertiaryBorderColor: '#b45309',
      tertiaryTextColor: '#0f172a',
      lineColor: '#64748b',
      textColor: '#172033',
      mainBkg: '#ffffff',
      secondBkg: '#f8fafc',
      clusterBkg: '#f8fafc',
      clusterBorder: '#cbd5e1',
      edgeLabelBackground: '#ffffff',
      noteBkgColor: '#fffbeb',
      noteBorderColor: '#fde68a',
      noteTextColor: '#172033',
      errorBkgColor: '#fef2f2',
      errorTextColor: '#991b1b',
      fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif'
    },
    dark: {
      background: '#172033',
      primaryColor: '#10254a',
      primaryBorderColor: '#60a5fa',
      primaryTextColor: '#f8fafc',
      secondaryColor: '#0f2f22',
      secondaryBorderColor: '#1f6f4a',
      secondaryTextColor: '#f8fafc',
      tertiaryColor: '#33270d',
      tertiaryBorderColor: '#854d0e',
      tertiaryTextColor: '#f8fafc',
      lineColor: '#9fb0c7',
      textColor: '#dbe7f6',
      mainBkg: '#172033',
      secondBkg: '#111827',
      clusterBkg: '#111827',
      clusterBorder: '#334155',
      edgeLabelBackground: '#172033',
      noteBkgColor: '#33270d',
      noteBorderColor: '#854d0e',
      noteTextColor: '#f8fafc',
      errorBkgColor: '#3a1418',
      errorTextColor: '#fecaca',
      fontFamily: 'Inter, ui-sans-serif, system-ui, sans-serif'
    }
  };

  let root: HTMLDivElement | undefined;
  let mounted = false;
  let instance = 0;
  let renderVersion = 0;

  const currentTheme = (): ThemeMode =>
    document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';

  const mermaidConfig = (mode: ThemeMode) => ({
    startOnLoad: false,
    securityLevel: 'strict' as const,
    theme: 'base' as const,
    themeVariables: mermaidThemeVariables[mode],
    flowchart: {
      htmlLabels: false,
      useMaxWidth: true
    },
    sequence: {
      useMaxWidth: true
    },
    gantt: {
      useMaxWidth: true
    }
  });

  const decodeMermaidSource = (encoded: string) => {
    try {
      return decodeURIComponent(encoded);
    } catch {
      return encoded;
    }
  };

  const stripMermaidInitDirectives = (source: string) =>
    source.replace(/^\s*%%\{\s*(?:init|initialize|config)[\s\S]*?\}%%\s*/gi, '');

  const renderMermaidDiagrams = async (mermaid: MermaidModule, version: number) => {
    if (!root) {
      return;
    }
    const diagrams = Array.from(
      root.querySelectorAll<HTMLElement>('.mermaid-diagram[data-mermaid-source]')
    );
    if (diagrams.length === 0) {
      return;
    }

    const mode = currentTheme();
    mermaid.initialize(mermaidConfig(mode));

    await Promise.all(
      diagrams.map(async (diagram, index) => {
        const encoded = diagram.dataset.mermaidSource || '';
        if (!encoded || diagram.dataset.mermaidRenderedTheme === mode) {
          return;
        }

        diagram.dataset.mermaidState = 'loading';
        try {
          const source = stripMermaidInitDirectives(decodeMermaidSource(encoded));
          const { svg } = await mermaid.render(`mermaid-${instance}-${version}-${index}`, source);
          if (!root?.contains(diagram) || version !== renderVersion) {
            return;
          }
          diagram.innerHTML = svg;
          diagram.dataset.mermaidState = 'rendered';
          diagram.dataset.mermaidRenderedTheme = mode;
        } catch (err) {
          diagram.dataset.mermaidState = 'error';
          diagram.dataset.mermaidError =
            err instanceof Error ? err.message.slice(0, 180) : 'Unable to render Mermaid diagram';
        }
      })
    );
  };

  const scheduleMermaidRender = () => {
    if (!mounted) {
      return;
    }
    renderVersion += 1;
    const version = renderVersion;
    void tick().then(async () => {
      if (!root?.querySelector('.mermaid-diagram[data-mermaid-source]')) {
        return;
      }
      const mermaid = (await import('mermaid')).default;
      await renderMermaidDiagrams(mermaid, version);
    });
  };

  onMount(() => {
    mounted = true;
    instance = Math.floor(Math.random() * 1_000_000);
    const unsubscribe = themeMode.subscribe(() => {
      scheduleMermaidRender();
    });
    scheduleMermaidRender();

    return () => {
      mounted = false;
      unsubscribe();
    };
  });

  afterUpdate(() => {
    scheduleMermaidRender();
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

  .markdown :global(.mermaid-diagram) {
    box-sizing: border-box;
    max-width: 100%;
    margin: 0.75rem 0;
    overflow-x: auto;
    padding: 0.75rem;
    border: 1px solid var(--border-soft, #dbe3ef);
    border-radius: 0.5rem;
    background: var(--surface, #ffffff);
  }

  .markdown :global(.mermaid-diagram pre) {
    margin: 0;
  }

  .markdown :global(.mermaid-diagram svg) {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0 auto;
  }

  :global(html[data-theme='dark'] .markdown .mermaid-diagram) {
    border-color: var(--border-soft, #263244);
    background: var(--surface, #172033);
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
