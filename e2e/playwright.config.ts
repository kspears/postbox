import { defineConfig } from '@playwright/test';

// Where Postbox answers. Inside the compose network that is http://postbox.localhost: a name
// browsers treat as secure without a certificate, which passkeys and Secure cookies need.
const baseURL = process.env.POSTBOX_URL ?? 'http://localhost:8026';
const host = new URL(baseURL).hostname;

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.ts',
  forbidOnly: !!process.env.CI,
  reporter: 'list',
  use: {
    baseURL,
    browserName: 'chromium',
    launchOptions: {
      // Chromium resolves every *.localhost name to loopback itself and never asks DNS, so
      // point the name at the Postbox container by hand.
      args: host.endsWith('.localhost') ? [`--host-resolver-rules=MAP ${host} postbox`] : [],
    },
  },
});
