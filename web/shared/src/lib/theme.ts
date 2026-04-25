import { writable } from 'svelte/store';

export type ThemeMode = 'light' | 'dark';

export const themeStorageKey = 'homelabd.dashboard.theme';
export const defaultTheme: ThemeMode = 'light';

export const themeMode = writable<ThemeMode>(defaultTheme);

export const isThemeMode = (value: unknown): value is ThemeMode => value === 'light' || value === 'dark';

export const readStoredTheme = (storage: Pick<Storage, 'getItem'> = localStorage): ThemeMode => {
  try {
    const stored = storage.getItem(themeStorageKey);
    return isThemeMode(stored) ? stored : defaultTheme;
  } catch {
    return defaultTheme;
  }
};

export const applyTheme = (mode: ThemeMode, root: HTMLElement = document.documentElement) => {
  root.dataset.theme = mode;
  root.style.colorScheme = mode;
};

export const persistTheme = (
  mode: ThemeMode,
  storage: Pick<Storage, 'setItem'> = localStorage
) => {
  try {
    storage.setItem(themeStorageKey, mode);
  } catch {
    // Ignore storage failures; the applied runtime theme still works.
  }
};

export const toggleThemeMode = (mode: ThemeMode): ThemeMode => (mode === 'dark' ? 'light' : 'dark');
