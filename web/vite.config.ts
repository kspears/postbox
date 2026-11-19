import { svelte } from '@sveltejs/vite-plugin-svelte';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [svelte()],
  build: {
    // Browsers with native light-dark(). With older targets the minifier rewrites it to follow
    // the system's colour scheme only, and the Light and Dark settings stop working.
    cssTarget: ['chrome123', 'edge123', 'firefox120', 'safari17.5'],
  },
});
