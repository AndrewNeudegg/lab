import { describe, expect, test } from 'bun:test';
import { defaultTheme, readStoredTheme, themeStorageKey, toggleThemeMode } from './theme';

describe('dashboard theme state', () => {
  test('falls back to light when no valid stored theme exists', () => {
    expect(readStoredTheme({ getItem: () => null })).toBe(defaultTheme);
    expect(readStoredTheme({ getItem: () => 'sepia' })).toBe(defaultTheme);
  });

  test('reads a valid stored theme', () => {
    expect(readStoredTheme({ getItem: (key) => (key === themeStorageKey ? 'dark' : null) })).toBe(
      'dark'
    );
  });

  test('toggles between light and dark', () => {
    expect(toggleThemeMode('light')).toBe('dark');
    expect(toggleThemeMode('dark')).toBe('light');
  });
});
