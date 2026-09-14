import { expect, test } from '@playwright/test';

test('run sequential checks and retain healthy and unhealthy results on reload', async ({
  page,
}) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Browser checks', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Run workflow', exact: true }).click();
  const results = page.getByRole('region', { name: 'Run results' });
  const steps = results.getByRole('article');
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
