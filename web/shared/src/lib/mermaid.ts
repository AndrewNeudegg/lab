import type { MermaidConfig } from 'mermaid';
import type { ThemeMode } from './theme';

export type BrandDiagramPalette = {
  bg: string;
  surface: string;
  surfaceMuted: string;
  surfaceHover: string;
  text: string;
  textStrong: string;
  muted: string;
  border: string;
  borderSoft: string;
  accent: string;
  accentHover: string;
  successBg: string;
  successBorder: string;
  dangerBg: string;
  dangerText: string;
  warningBg: string;
  warningBorder: string;
  warningText: string;
};

export const brandDiagramPalettes: Record<ThemeMode, BrandDiagramPalette> = {
  light: {
    bg: '#f5f7fb',
    surface: '#ffffff',
    surfaceMuted: '#f8fafc',
    surfaceHover: '#eef5ff',
    text: '#172033',
    textStrong: '#0f172a',
    muted: '#64748b',
    border: '#cbd5e1',
    borderSoft: '#dbe3ef',
    accent: '#2563eb',
    accentHover: '#1d4ed8',
    successBg: '#f0fdf4',
    successBorder: '#bbf7d0',
    dangerBg: '#fef2f2',
    dangerText: '#991b1b',
    warningBg: '#fffbeb',
    warningBorder: '#fde68a',
    warningText: '#92400e'
  },
  dark: {
    bg: '#0b1120',
    surface: '#172033',
    surfaceMuted: '#1f2937',
    surfaceHover: '#243047',
    text: '#dbe7f6',
    textStrong: '#f8fafc',
    muted: '#9fb0c7',
    border: '#334155',
    borderSoft: '#263244',
    accent: '#60a5fa',
    accentHover: '#3b82f6',
    successBg: '#0f2f22',
    successBorder: '#1f6f4a',
    dangerBg: '#3a1418',
    dangerText: '#fecaca',
    warningBg: '#33270d',
    warningBorder: '#854d0e',
    warningText: '#fde68a'
  }
};

export const mermaidThemeVariables = (mode: ThemeMode) => {
  const palette = brandDiagramPalettes[mode];

  return {
    background: palette.surface,
    mainBkg: palette.surface,
    secondBkg: palette.surfaceMuted,
    tertiaryBkg: palette.surfaceHover,
    primaryColor: palette.surface,
    primaryTextColor: palette.textStrong,
    primaryBorderColor: palette.accent,
    secondaryColor: palette.surfaceMuted,
    secondaryTextColor: palette.text,
    secondaryBorderColor: palette.border,
    tertiaryColor: palette.surfaceHover,
    tertiaryTextColor: palette.text,
    tertiaryBorderColor: palette.borderSoft,
    lineColor: palette.accent,
    textColor: palette.text,
    titleColor: palette.textStrong,
    nodeTextColor: palette.textStrong,
    clusterBkg: palette.surfaceMuted,
    clusterBorder: palette.border,
    edgeLabelBackground: palette.surface,
    actorBkg: palette.surface,
    actorBorder: palette.accent,
    actorTextColor: palette.textStrong,
    actorLineColor: palette.border,
    signalColor: palette.accent,
    signalTextColor: palette.textStrong,
    labelBoxBkgColor: palette.surface,
    labelBoxBorderColor: palette.border,
    labelTextColor: palette.text,
    loopTextColor: palette.textStrong,
    noteBkgColor: palette.warningBg,
    noteTextColor: mode === 'dark' ? palette.warningText : palette.text,
    noteBorderColor: palette.warningBorder,
    activationBkgColor: palette.surfaceHover,
    activationBorderColor: palette.accent,
    sequenceNumberColor: palette.surface,
    sectionBkgColor: palette.surfaceMuted,
    altSectionBkgColor: palette.surfaceHover,
    taskBkgColor: palette.surface,
    taskTextColor: palette.textStrong,
    taskTextLightColor: palette.text,
    taskTextOutsideColor: palette.text,
    taskTextClickableColor: palette.accent,
    activeTaskBkgColor: palette.successBg,
    activeTaskBorderColor: palette.successBorder,
    doneTaskBkgColor: palette.surfaceMuted,
    doneTaskBorderColor: palette.border,
    critBkgColor: palette.dangerBg,
    critBorderColor: palette.dangerText,
    todayLineColor: palette.accent,
    gridColor: palette.borderSoft,
    c0: palette.accent,
    c1: palette.successBorder,
    c2: palette.warningBorder,
    c3: palette.dangerText,
    c4: palette.accentHover
  };
};

export const mermaidConfigForTheme = (mode: ThemeMode): MermaidConfig => ({
  startOnLoad: false,
  securityLevel: 'strict',
  secure: ['securityLevel', 'startOnLoad', 'theme', 'themeVariables', 'themeCSS'],
  theme: 'base',
  darkMode: mode === 'dark',
  htmlLabels: false,
  fontFamily: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  themeVariables: mermaidThemeVariables(mode),
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
