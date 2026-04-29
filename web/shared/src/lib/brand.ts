export type BrandDiagramMode = 'light' | 'dark';

export type BrandDiagramPalette = {
  background: string;
  surface: string;
  surfaceAccent: string;
  primary: string;
  secondary: string;
  success: string;
  warning: string;
  danger: string;
  text: string;
  muted: string;
  border: string;
};

export const brandDiagramPalettes: Record<BrandDiagramMode, BrandDiagramPalette> = {
  light: {
    background: '#f8fafc',
    surface: '#ffffff',
    surfaceAccent: '#e0f2fe',
    primary: '#2563eb',
    secondary: '#0f766e',
    success: '#16a34a',
    warning: '#d97706',
    danger: '#dc2626',
    text: '#172033',
    muted: '#64748b',
    border: '#cbd5e1'
  },
  dark: {
    background: '#0f172a',
    surface: '#111827',
    surfaceAccent: '#164e63',
    primary: '#60a5fa',
    secondary: '#2dd4bf',
    success: '#4ade80',
    warning: '#fbbf24',
    danger: '#f87171',
    text: '#e2e8f0',
    muted: '#94a3b8',
    border: '#334155'
  }
};

const diagramFont =
  'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif';

export const mermaidBrandThemeVariables = (mode: BrandDiagramMode) => {
  const palette = brandDiagramPalettes[mode];

  return {
    background: palette.background,
    mainBkg: palette.surface,
    secondBkg: palette.surfaceAccent,
    tertiaryColor: palette.background,
    primaryColor: palette.surface,
    primaryTextColor: palette.text,
    primaryBorderColor: palette.primary,
    secondaryColor: palette.surfaceAccent,
    secondaryTextColor: palette.text,
    secondaryBorderColor: palette.secondary,
    tertiaryTextColor: palette.text,
    tertiaryBorderColor: palette.border,
    lineColor: palette.primary,
    textColor: palette.text,
    border1: palette.primary,
    border2: palette.secondary,
    clusterBkg: palette.background,
    clusterBorder: palette.border,
    edgeLabelBackground: palette.surface,
    nodeBorder: palette.primary,
    actorBkg: palette.surface,
    actorBorder: palette.primary,
    actorTextColor: palette.text,
    actorLineColor: palette.primary,
    signalColor: palette.text,
    signalTextColor: palette.text,
    labelBoxBkgColor: palette.surface,
    labelBoxBorderColor: palette.border,
    labelTextColor: palette.text,
    loopTextColor: palette.text,
    activationBkgColor: palette.surfaceAccent,
    activationBorderColor: palette.secondary,
    noteBkgColor: mode === 'dark' ? '#422006' : '#fffbeb',
    noteTextColor: palette.text,
    noteBorderColor: palette.warning,
    fontFamily: diagramFont
  };
};
