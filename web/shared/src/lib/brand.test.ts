import { describe, expect, test } from 'bun:test';
import { brandDiagramPalettes, mermaidBrandThemeVariables } from './brand';

describe('diagram brand palette', () => {
  test('exposes light and dark colours for Mermaid rendering', () => {
    expect(brandDiagramPalettes.light.primary).toBe('#2563eb');
    expect(brandDiagramPalettes.dark.primary).toBe('#60a5fa');
    expect(mermaidBrandThemeVariables('light')).toMatchObject({
      primaryBorderColor: '#2563eb',
      lineColor: '#2563eb',
      background: '#f8fafc'
    });
    expect(mermaidBrandThemeVariables('dark')).toMatchObject({
      primaryBorderColor: '#60a5fa',
      lineColor: '#60a5fa',
      background: '#0f172a'
    });
  });
});
