import { expect, test } from '@playwright/test';
import type { Run, Workflow } from '../src/api';

test('TCP results and legacy HTTP history render from saved output after reload', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  const workflow: Workflow = {
    schemaVersion: 1,
    id: 'mixed-checks',
    name: 'Mixed checks',
    description: 'HTTP and TCP results',
    steps: [
      {
        id: 'http',
        name: 'HTTP endpoint',
        type: 'http.check',
        config: { url: 'http://service.invalid/health', expectedStatus: 200, timeoutMs: 1000 },
      },
      {
        id: 'tcp',
        name: 'TCP port',
        type: 'tcp.check',
        config: { host: '::1', port: 8080, timeoutMs: 1000 },
      },
    ],
  };
  const mixed: Run = {
    id: 'mixed-run',
    workflowId: workflow.id,
    workflowName: workflow.name,
    status: 'succeeded',
    createdAt: '2025-01-02T12:00:00Z',
    startedAt: '2025-01-02T12:00:00Z',
    finishedAt: '2025-01-02T12:00:01Z',
    steps: [
      {
        id: 'http',
        name: 'Saved HTTP result',
        status: 'succeeded',
        output: {
          type: 'http.check',
          healthy: true,
          url: 'http://service.invalid/health',
          expectedStatus: 200,
          statusCode: 200,
          durationMs: 10,
          reason: 'Received expected HTTP 200',
        },
      },
      {
        id: 'tcp',
        name: 'Saved TCP result',
        status: 'succeeded',
        output: {
          type: 'tcp.check',
          healthy: true,
          host: '::1',
          port: 9090,
          durationMs: 4,
          reason: 'TCP connection established',
        },
      },
      {
        id: 'old-port',
        name: 'Unavailable TCP port',
        status: 'succeeded',
        output: {
          type: 'tcp.check',
          healthy: false,
          host: 'closed.invalid',
          port: 22,
          durationMs: 5,
          reason: 'Connection refused',
        },
      },
    ],
  };
  const legacyHTTP: Run = {
    ...mixed,
    id: 'legacy-http',
    steps: [
      {
        // The current definition reuses this ID for TCP. Saved output still describes HTTP.
        id: 'tcp',
        name: 'Original HTTP check',
        status: 'succeeded',
        output: {
          healthy: true,
          url: 'http://original.invalid/health',
          expectedStatus: 204,
          statusCode: 204,
          durationMs: 12,
          reason: 'Received expected HTTP 204',
        },
      },
    ],
  };
  const acceptedTCP: Run = { ...mixed, id: 'accepted-tcp', steps: [mixed.steps[1]] };
  await page.route('**/api/workflows', (route) => route.fulfill({ json: [workflow] }));
  await page.route('**/api/runs', (route) =>
    route.fulfill({ json: [mixed, legacyHTTP, acceptedTCP] }),
  );
  await page.goto('/');

  const definitions = page.getByRole('region', { name: 'Saved workflow steps' });
  const tcpDefinition = definitions.getByRole('listitem').filter({ hasText: 'TCP port' });
  await expect(tcpDefinition.getByText('TCP CHECK', { exact: true })).toBeVisible();
  await expect(tcpDefinition.locator('.endpoint')).toHaveText('[::1]:8080');
  await expect(tcpDefinition).toContainText('TCP connection');
  await expect(tcpDefinition).not.toContainText('GET');
  await expect(tcpDefinition).not.toContainText('HTTP');
  await expect(definitions.locator('.endpoint').first()).toHaveText(
    'GET http://service.invalid/health',
  );

  const results = page.getByRole('region', { name: 'Run results' });
  const connected = results.getByRole('article').filter({ hasText: 'Saved TCP result' });
  const unreachable = results.getByRole('article').filter({ hasText: 'Unavailable TCP port' });
  const expectMixedResults = async () => {
    await expect(results.getByText('Completed', { exact: true })).toBeVisible();
    await expect(results.getByText('1 check did not pass', { exact: true })).toBeVisible();
    await expect(results.getByText('HTTP 200', { exact: true })).toBeVisible();
    await expect(connected.getByText('Connected', { exact: true })).toBeVisible();
    await expect(connected.locator('.endpoint')).toHaveText('[::1]:9090');
    await expect(connected.getByText('Accepted', { exact: true })).toBeVisible();
    await expect(connected.getByText('Time to connect', { exact: false })).toContainText('4 ms');
    await expect(connected).not.toContainText('HTTP');
    await expect(connected).not.toContainText('Time to headers');
    await expect(unreachable.getByText('Unreachable', { exact: true })).toBeVisible();
    await expect(unreachable.getByText('Not established', { exact: true })).toBeVisible();
    await expect(unreachable.locator('.endpoint')).toHaveText('closed.invalid:22');
    await expect(
      results.getByText('it does not verify application health.', { exact: false }),
    ).toBeVisible();
  };
  await expectMixedResults();
  await page.reload();
  await expectMixedResults();

  await page.getByRole('button', { name: /Run legacy-h/ }).click();
  await expect(results.getByText('HTTP 204', { exact: true })).toBeVisible();
  await expect(results.getByText('Time to headers', { exact: false })).toContainText('12 ms');
  await expect(results.getByText('Healthy', { exact: true })).toBeVisible();
  await expect(results.getByText('Time to connect', { exact: false })).toHaveCount(0);

  await page.getByRole('button', { name: /Run accepted/ }).click();
  await expect(results.getByText('All checks passed', { exact: true })).toBeVisible();
  await expect(results.getByText('All services healthy', { exact: true })).toHaveCount(0);
  expect(errors).toEqual([]);
});
