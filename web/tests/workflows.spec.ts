import { expect, test } from '@playwright/test';

test('run healthy, unhealthy, sequential, and timeout examples and retain history on reload', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Healthy service', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  const results = page.getByRole('region', { name: 'Run results' });
  await expect(results.getByText('All services healthy')).toBeVisible();
  await expect(results.getByText('Received expected HTTP 200')).toBeVisible();

  await page.reload();
  await expect(results.getByText('Received expected HTTP 200')).toBeVisible();

  await page.getByRole('button', { name: /02 Unhealthy service/ }).click();
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  await expect(results.getByText('Expected HTTP 200; received HTTP 503')).toBeVisible();
  await expect(results.getByText('Completed', { exact: true })).toBeVisible();

  await page.getByRole('button', { name: /03 Two services/ }).click();
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  await expect(results.getByText('Expected HTTP 200; received HTTP 503')).toBeVisible();
  await expect(results.getByText('Received expected HTTP 200')).toBeVisible();

  await page.getByRole('button', { name: /04 Service timeout/ }).click();
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  await expect(results.getByText('No response within 500 ms')).toBeVisible();
  await expect(results.getByText('No response', { exact: true })).toBeVisible();
  await page.screenshot({ path: 'test-results/desktop.png', fullPage: true });
  expect(errors).toEqual([]);
});

test('mobile layout and backend-disconnection feedback', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Healthy service', exact: true })).toBeVisible();
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
