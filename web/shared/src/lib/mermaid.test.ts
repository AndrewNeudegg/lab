import { describe, expect, test } from 'bun:test';
import { brandDiagramPalettes, mermaidConfigForTheme, mermaidThemeVariables } from './mermaid';

describe('mermaid brand configuration', () => {
  test('uses the dashboard brand palettes for light and dark diagrams', () => {
    expect(brandDiagramPalettes.light.accent).toBe('#2563eb');
    expect(brandDiagramPalettes.dark.accent).toBe('#60a5fa');
  });

  test('maps brand colours into Mermaid theme variables', () => {
    expect(mermaidThemeVariables('light')).toMatchObject({
      primaryBorderColor: '#2563eb',
      lineColor: '#2563eb',
      textColor: '#172033',
      background: '#ffffff'
    });
    expect(mermaidThemeVariables('dark')).toMatchObject({
      primaryBorderColor: '#60a5fa',
      lineColor: '#60a5fa',
      textColor: '#dbe7f6',
      background: '#172033'
    });
  });

  test('keeps Mermaid rendering strict and theme-controlled', () => {
    expect(mermaidConfigForTheme('dark')).toMatchObject({
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'base',
      darkMode: true,
      htmlLabels: false
    });
  });
});
