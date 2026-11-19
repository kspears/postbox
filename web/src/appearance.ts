// Light, dark, or as the system says. The choice is kept in the browser too, so the next page
// load starts in it before the settings arrive.

import type { Preferences } from './api';

const key = 'postbox.appearance';

export function applyAppearance(appearance: Preferences['appearance']): void {
  document.documentElement.style.colorScheme = appearance === 'system' ? '' : appearance;
  try {
    localStorage.setItem(key, appearance);
  } catch {
    // Storage may be off; the setting still applies once loaded from the server.
  }
}

/** Applies the appearance this browser last used, if any. */
export function restoreAppearance(): void {
  try {
    const saved = localStorage.getItem(key);
    if (saved === 'light' || saved === 'dark') document.documentElement.style.colorScheme = saved;
  } catch {
    // As above.
  }
}
