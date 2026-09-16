import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:18080', headless: true },
  webServer: {
    // Make a fresh database only when starting the test server. Keep it for debugging.
    command: [
      'mkdir -p ../.cache/e2e &&',
      'test_data_dir=$(mktemp -d ../.cache/e2e/run.XXXXXX) &&',
      'exec ../bin/patchbay -addr 127.0.0.1:18080 -workflows tests/fixtures/workflows -web dist',
      '-db "$test_data_dir/patchbay.db"',
    ].join(' '),
    url: 'http://127.0.0.1:18080/api/health',
    reuseExistingServer: false,
  },
});
