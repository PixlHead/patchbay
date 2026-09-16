import { expect, test } from '@playwright/test';
import type { Run } from '../src/api';

test('run sequential checks and retain healthy and unhealthy results on reload', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Browser checks', exact: true })).toBeVisible();
  const historyResponse = await page.request.get('/api/runs');
  expect(historyResponse.ok()).toBeTruthy();
  const previousRuns: Run[] = await historyResponse.json();
  const startResponsePromise = page.waitForResponse(
    (response) =>
      response.request().method() === 'POST' &&
      new URL(response.url()).pathname === '/api/workflows/browser-checks/runs',
  );
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  const startResponse = await startResponsePromise;
  expect(startResponse.status()).toBe(202);
  const started: Run = await startResponse.json();
  expect(started.id).toEqual(expect.any(String));
  expect(started.id).not.toBe('');
  expect(previousRuns.map((run) => run.id)).not.toContain(started.id);
  const results = page.getByRole('region', { name: 'Run results' });
  const steps = results.getByRole('article');
  // Check ID and completion together: an older result cannot satisfy this assertion.
  const expectNewRunCompleted = async () => {
    await expect
      .poll(async () => JSON.parse((await results.locator('pre').textContent()) ?? '{}'))
      .toMatchObject({ id: started.id, workflowId: 'browser-checks', status: 'succeeded' });
  };
  await expectNewRunCompleted();
  await expect(results.getByText('Completed', { exact: true })).toBeVisible();
  await expect(steps.getByText('Received expected HTTP 200')).toBeVisible();
  await expect(steps.getByText('Expected HTTP 204; received HTTP 200')).toBeVisible();
  await expect(steps.getByText('Healthy', { exact: true })).toBeVisible();
  await expect(steps.getByText('Unhealthy', { exact: true })).toBeVisible();
  await expect(results.getByRole('heading', { level: 4 })).toHaveText([
    'Expected response',
    'Unexpected response',
  ]);

  await page.reload();
  await expectNewRunCompleted();
  await expect(steps.getByText('Received expected HTTP 200')).toBeVisible();
  await expect(steps.getByText('Expected HTTP 204; received HTTP 200')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run workflow', exact: true })).toBeEnabled();
  await page.screenshot({ path: 'test-results/desktop.png', fullPage: true });
  expect(errors).toEqual([]);
});

test('mobile layout and backend-disconnection feedback', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Browser checks', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run workflow', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );
  await page.screenshot({ path: 'test-results/mobile.png', fullPage: true });
  await page.route('**/api/**', (route) => route.abort());
  await expect(page.getByRole('alert')).toContainText('Retrying automatically');
  await expect(page.getByRole('button', { name: 'Run workflow', exact: true })).toBeDisabled();
  await page.unroute('**/api/**');
  await expect(page.getByRole('alert')).toHaveCount(0);
});

test('an old unfinished run does not disable starting another workflow', async ({ page }) => {
  await page.route('**/api/runs', (route) =>
    route.fulfill({
      json: [
        {
          id: 'old-unfinished',
          workflowId: 'browser-checks',
          workflowName: 'Browser checks',
          status: 'running',
          createdAt: '2025-01-02T12:00:00Z',
          startedAt: '2025-01-02T12:00:00Z',
          steps: [{ id: 'healthy', name: 'Expected response', status: 'running' }],
        },
      ],
    }),
  );
  // Capacity is checked when starting; the UI still displays the server's refusal.
  await page.route('**/api/workflows/browser-checks/runs', (route) =>
    route.fulfill({
      status: 409,
      json: { error: 'this workflow is already queued or running; wait for it to finish' },
    }),
  );
  await page.goto('/');
  await expect(page.getByText('No completion recorded yet', { exact: true })).toBeVisible();
  const start = page.getByRole('button', { name: 'Run workflow', exact: true });
  await expect(start).toBeEnabled();
  await start.click();
  await expect(page.getByRole('alert')).toContainText('this workflow is already queued or running');
  await expect(start).toBeEnabled();
});

test('interrupted history preserves results and shows an unknown finish time', async ({ page }) => {
  await page.route('**/api/runs', (route) =>
    route.fulfill({
      json: [
        {
          id: 'interrupted-run',
          workflowId: 'browser-checks',
          workflowName: 'Browser checks',
          status: 'interrupted',
          createdAt: '2025-01-02T12:00:00Z',
          startedAt: '2025-01-02T12:00:00Z',
          steps: [
            {
              id: 'completed',
              name: 'Completed check',
              status: 'succeeded',
              output: {
                healthy: true,
                url: 'http://service.invalid/health',
                expectedStatus: 200,
                statusCode: 200,
                durationMs: 42,
                reason: 'Received expected HTTP 200',
              },
            },
            { id: 'active', name: 'Unfinished check', status: 'interrupted' },
            { id: 'pending', name: 'Unstarted check', status: 'skipped' },
          ],
        },
      ],
    }),
  );
  await page.goto('/');
  const results = page.getByRole('region', { name: 'Run results' });
  await expect(results.getByText('Interrupted', { exact: true })).toHaveCount(2);
  await expect(results.getByText('Finish time unknown', { exact: true })).toBeVisible();
  await expect(results.getByRole('article').getByText('Received expected HTTP 200')).toBeVisible();
  await expect(results.getByText('Healthy', { exact: true })).toBeVisible();
  await expect(results.getByText('Skipped', { exact: true })).toBeVisible();
  await expect(
    results.getByText('Saved results are preserved; steps were not resumed.', { exact: false }),
  ).toBeVisible();
  await expect(page.getByRole('button', { name: 'Run workflow', exact: true })).toBeEnabled();
});

for (const status of ['queued', 'canceled', 'interrupted']) {
  test(`a ${status} run without a start time has readable history`, async ({ page }) => {
    await page.route('**/api/runs', (route) =>
      route.fulfill({
        json: [
          {
            id: 'waiting-run',
            workflowId: 'browser-checks',
            workflowName: 'Browser checks',
            status,
            createdAt: '2025-01-02T12:00:00Z',
            ...(status === 'canceled' ? { finishedAt: '2025-01-02T12:01:00Z' } : {}),
            steps: [
              {
                id: 'healthy',
                name: 'Expected response',
                status: status === 'queued' ? 'pending' : 'skipped',
              },
            ],
          },
        ],
      }),
    );
    await page.goto('/');
    const results = page.getByRole('region', { name: 'Run results' });
    await expect(
      results.getByText(status === 'queued' ? 'Waiting for an execution slot' : 'Never started', {
        exact: true,
      }),
    ).toBeVisible();
    await expect(page.getByText('Invalid Date', { exact: false })).toHaveCount(0);
    await expect(results.getByText('NaN ms', { exact: false })).toHaveCount(0);
    if (status === 'queued') {
      await expect(results.getByText('Queued', { exact: true })).toBeVisible();
      await expect(
        results.getByText('This run will start automatically when an execution slot is available.'),
      ).toBeVisible();
    }
  });
}
