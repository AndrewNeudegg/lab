<script lang="ts">
  import { onMount } from 'svelte';
  import {
    applyTheme,
    persistTheme,
    readStoredTheme,
    themeMode,
    toggleThemeMode,
    type ThemeMode
  } from './theme';

  export let compact = false;

  let mode: ThemeMode = 'light';

  onMount(() => {
    mode = readStoredTheme();
    themeMode.set(mode);
    applyTheme(mode);

    const unsubscribe = themeMode.subscribe((value) => {
      mode = value;
      applyTheme(value);
      persistTheme(value);
    });

    return unsubscribe;
  });

  const toggle = () => {
    themeMode.set(toggleThemeMode(mode));
  };
</script>

<button
  type="button"
  class:compact
  class="theme-toggle"
  aria-label={`Switch to ${mode === 'dark' ? 'light' : 'dark'} mode`}
  aria-pressed={mode === 'dark'}
  on:click={toggle}
>
  <span aria-hidden="true">{mode === 'dark' ? '☾' : '☀'}</span>
  {compact ? mode : `${mode} mode`}
</button>

<style>
  .theme-toggle {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.4rem;
    min-height: 2.4rem;
    padding: 0 0.75rem;
    border: 1px solid var(--border, #cbd5e1);
    border-radius: 0.65rem;
    color: var(--text, #243047);
    background: var(--surface, #ffffff);
    font: inherit;
    font-size: 0.86rem;
    font-weight: 850;
    text-transform: capitalize;
  }

  .theme-toggle:hover {
    border-color: var(--accent, #2563eb);
    background: var(--surface-hover, #eef5ff);
  }

  .theme-toggle span {
    font-size: 0.95rem;
  }

  .theme-toggle.compact {
    width: 100%;
    justify-content: flex-start;
  }
</style>
