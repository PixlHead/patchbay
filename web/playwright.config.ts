import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:18080', headless: true },
  webServer: [
    {
      command: '../bin/patchbay-demo -addr 127.0.0.1:19091',
      url: 'http://127.0.0.1:19091/healthy',
      reuseExistingServer: false,
    },
    {
      command:
        '../bin/patchbay -addr 127.0.0.1:18080 -demo-url http://127.0.0.1:19091 -workflows ../examples -web dist',
      url: 'http://127.0.0.1:18080/api/health',
      reuseExistingServer: false,
    },
  ],
});
