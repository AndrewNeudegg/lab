<script lang="ts">
  import { afterUpdate, onMount, tick } from 'svelte';
  import { renderMarkdown } from './markdown';

  export let content = '';
  export let headingIds = false;

  type MermaidThemeMode = 'light' | 'dark';
  type Mermaid = typeof import('mermaid').default;

  const mermaidThemeVariables = {
    light: {
      background: '#f5f7fb',
      primaryColor: '#dbeafe',
      primaryTextColor: '#0f172a',
      primaryBorderColor: '#2563eb',
      secondaryColor: '#ccfbf1',
      secondaryTextColor: '#0f172a',
      secondaryBorderColor: '#0f766e',
      tertiaryColor: '#fef3c7',
      tertiaryTextColor: '#0f172a',
      tertiaryBorderColor: '#d97706',
      lineColor: '#64748b',
      textColor: '#172033',
      mainBkg: '#ffffff',
      secondBkg: '#f8fafc',
      border1: '#cbd5e1',
      border2: '#dbe3ef',
      noteBkgColor: '#fffbeb',
      noteTextColor: '#172033',
      noteBorderColor: '#fde68a',
      clusterBkg: '#f8fafc',
      clusterBorder: '#cbd5e1',
      edgeLabelBackground: '#ffffff',
      actorBkg: '#ffffff',
      actorBorder: '#2563eb',
      actorTextColor: '#172033',
      signalColor: '#2563eb',
      signalTextColor: '#172033',
      activationBkgColor: '#dbeafe',
      activationBorderColor: '#2563eb',
      sequenceNumberColor: '#ffffff'
    },
    dark: {
      background: '#0b1120',
      primaryColor: '#10254a',
      primaryTextColor: '#f8fafc',
      primaryBorderColor: '#60a5fa',
      secondaryColor: '#0f302d',
      secondaryTextColor: '#f8fafc',
      secondaryBorderColor: '#2dd4bf',
      tertiaryColor: '#33270d',
      tertiaryTextColor: '#f8fafc',
      tertiaryBorderColor: '#fbbf24',
      lineColor: '#9fb0c7',
      textColor: '#dbe7f6',
      mainBkg: '#172033',
      secondBkg: '#111827',
      border1: '#334155',
      border2: '#263244',
      noteBkgColor: '#33270d',
      noteTextColor: '#f8fafc',
      noteBorderColor: '#854d0e',
      clusterBkg: '#111827',
      clusterBorder: '#334155',
      edgeLabelBackground: '#172033',
      actorBkg: '#172033',
      actorBorder: '#60a5fa',
      actorTextColor: '#dbe7f6',
      signalColor: '#60a5fa',
      signalTextColor: '#dbe7f6',
      activationBkgColor: '#10254a',
      activationBorderColor: '#60a5fa',
      sequenceNumberColor: '#0b1120'
    }
  };

  let root: HTMLDivElement | undefined;
  let mermaidPromise: Promise<Mermaid> | undefined;
  let renderQueued = false;
  let renderGeneration = 0;

  const currentThemeMode = (): MermaidThemeMode =>
    document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';

  const loadMermaid = () => {
    mermaidPromise ??= import('mermaid').then((module) => module.default);
    return mermaidPromise;
  };

  const mermaidConfig = (mode: MermaidThemeMode) => ({
    startOnLoad: false,
    securityLevel: 'strict' as const,
    theme: 'base' as const,
    secure: ['securityLevel', 'theme', 'themeVariables', 'fontFamily', 'startOnLoad'],
    fontFamily:
      'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
    themeVariables: {
      ...mermaidThemeVariables[mode],
      fontFamily:
        'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif'
    }
  });

  const fallbackDiagram = (element: HTMLElement, source: string, message: string) => {
    const pre = document.createElement('pre');
    const code = document.createElement('code');
    const note = document.createElement('figcaption');

    code.className = 'language-mermaid';
    code.textContent = source;
    pre.append(code);
    note.className = 'mermaid-error';
    note.textContent = message;
    element.replaceChildren(note, pre);
  };

  const renderMermaidDiagrams = async () => {
    if (!root) {
      return;
    }

    const diagrams = Array.from(root.querySelectorAll<HTMLElement>('.mermaid-diagram'));
    if (diagrams.length === 0) {
      return;
    }

    const generation = ++renderGeneration;
    const mode = currentThemeMode();
    const mermaid = await loadMermaid();

    if (generation !== renderGeneration) {
      return;
    }

    mermaid.initialize(mermaidConfig(mode));

    for (const [index, diagram] of diagrams.entries()) {
      const source =
        diagram.dataset.mermaidSource || diagram.querySelector('code')?.textContent || '';

      if (!source.trim()) {
        continue;
      }
      if (
        diagram.dataset.mermaidRendered === 'true' &&
        diagram.dataset.mermaidTheme === mode &&
        diagram.dataset.mermaidSource === source
      ) {
        continue;
      }

      diagram.dataset.mermaidSource = source;

      try {
        const id = `homelab-mermaid-${Date.now()}-${index}`;
        const result = await mermaid.render(id, source);
        if (generation !== renderGeneration) {
          return;
        }
        diagram.innerHTML = result.svg;
        result.bindFunctions?.(diagram);
        diagram.dataset.mermaidTheme = mode;
        diagram.dataset.mermaidRendered = 'true';
      } catch {
        diagram.dataset.mermaidTheme = mode;
        diagram.dataset.mermaidRendered = 'error';
        fallbackDiagram(diagram, source, 'Mermaid diagram could not be rendered.');
      }
    }
  };

  const scheduleMermaidRender = () => {
    if (renderQueued) {
      return;
    }
    renderQueued = true;
    void tick().then(() => {
      renderQueued = false;
      void renderMermaidDiagrams();
    });
  };

  onMount(() => {
    scheduleMermaidRender();

    const observer = new MutationObserver((mutations) => {
      if (mutations.some((mutation) => mutation.attributeName === 'data-theme')) {
        scheduleMermaidRender();
      }
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['data-theme'] });

    return () => observer.disconnect();
  });

  afterUpdate(scheduleMermaidRender);
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
    border: 1px solid #cbd5e1;
    border-radius: 0.5rem;
    background: #ffffff;
  }

  .markdown :global(.mermaid-diagram svg) {
    display: block;
    max-width: 100%;
    height: auto;
    margin: 0 auto;
  }

  .markdown :global(.mermaid-diagram pre) {
    margin: 0.4rem 0 0;
  }

  .markdown :global(.mermaid-error) {
    margin: 0 0 0.45rem;
    color: #991b1b;
    font-size: 0.82rem;
    font-weight: 750;
  }

  :global(html[data-theme='dark'] .markdown .mermaid-diagram) {
    border-color: var(--border-soft, #263244);
    background: var(--surface, #172033);
  }

  :global(html[data-theme='dark'] .markdown .mermaid-error) {
    color: #fecaca;
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
