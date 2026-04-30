import type { MermaidConfig } from 'mermaid';
import { mermaidBrandThemeVariables, type BrandDiagramMode } from './brand';

const diagramFont =
  'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';

export const mermaidThemeVariables = mermaidBrandThemeVariables;

export const mermaidConfigForTheme = (mode: BrandDiagramMode): MermaidConfig => ({
  startOnLoad: false,
  securityLevel: 'strict',
  secure: [
    'securityLevel',
    'startOnLoad',
    'theme',
    'themeVariables',
    'themeCSS',
    'darkMode',
    'fontFamily',
    'htmlLabels'
  ],
  theme: 'base',
  darkMode: mode === 'dark',
  htmlLabels: false,
  fontFamily: diagramFont,
  themeVariables: mermaidBrandThemeVariables(mode),
  flowchart: {
    curve: 'rounded',
    diagramPadding: 16,
    nodeSpacing: 42,
    rankSpacing: 48
  },
  sequence: {
    diagramMarginX: 24,
    diagramMarginY: 16,
    actorMargin: 48
  },
  gantt: {
    topPadding: 36,
    leftPadding: 72,
    gridLineStartPadding: 32
  }
});
