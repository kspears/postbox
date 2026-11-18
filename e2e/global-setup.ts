import type { FullConfig } from '@playwright/test';

// Postbox starts in well under a second, but nothing in the stack waits for it.
export default async function globalSetup(config: FullConfig): Promise<void> {
  const url = `${config.projects[0]?.use.baseURL}/healthz`;
  const deadline = Date.now() + 30_000;
  for (;;) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // Not listening yet.
    }
    if (Date.now() > deadline) throw new Error(`Postbox did not answer at ${url} within 30s`);
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
}
