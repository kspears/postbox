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
  // One at a time: the tests share accounts, and signing out everywhere ends every session of one.
  workers: 1,
  outputDir: './.state/test-results',
  use: {
    baseURL,
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    launchOptions: {
      // Chromium resolves every *.localhost name to loopback itself and never asks DNS, so
      // point the name at its compose service by hand: <service>.localhost is <service>.
      args: host.endsWith('.localhost') ? [`--host-resolver-rules=MAP ${host} ${host.slice(0, -'.localhost'.length)}`] : [],
    },
  },
  // deploy/ci/test.sh runs these in order, restarting Postbox between the two restart halves.
  projects: [
    { name: 'main', testMatch: /tests\/[^/]+\.spec\.ts$/ },
    { name: 'restart-before', testMatch: /restart\/before\.spec\.ts$/ },
    { name: 'restart-after', testMatch: /restart\/after\.spec\.ts$/ },
    { name: 'untrusted', testMatch: /untrusted\/[^/]+\.spec\.ts$/ },
    { name: 'outage', testMatch: /outage\/[^/]+\.spec\.ts$/ },
    // Moves the server's clock forward for good, so it runs last.
    { name: 'clock', testMatch: /clock\/[^/]+\.spec\.ts$/ },
  ],
});
