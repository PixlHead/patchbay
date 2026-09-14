import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:18080', headless: true },
  webServer: {
    command: '../bin/patchbay -addr 127.0.0.1:18080 -workflows tests/fixtures/workflows -web dist',
    url: 'http://127.0.0.1:18080/api/health',
    reuseExistingServer: false,
  },
});
