import AxeBuilder from '@axe-core/playwright';
import { expect, test, type Page } from '@playwright/test';

const release = { id: 'release-1', dataset_id: 'alcohol', sha256: 'abc', source_url: 'https://data.who.int/indicators/i/EF38E6A/EE6F72A', accessed_at: '2026-08-27T02:15:00Z', citation: 'WHO.', parser_version: 'v0.1.0' };
const meta = { snapshot: { id: 'snapshot-1', created_at: '2026-08-27T02:16:00Z', m49_reference_release: 'UN-M49-2026' }, releases: [release] };
const alcohol = { id: 'alcohol-total', dataset_id: 'alcohol', measure_id: 'm1', name: 'Alcohol consumption', unit: 'litres', statistic: 'mean', value_kind: 'number', dimensions: { SEX: 'TOTAL' }, available_years: [2000, 2001] };
const catalog = { meta, datasets: [{ id: 'alcohol', name: 'Alcohol consumption', who_identifier: 'EE6F72A', who_code: 'SA_0000001688', source_url: release.source_url, citation: 'WHO.', capabilities: ['line', 'map', 'association', 'table'], release }], measures: [], series: [alcohol], dimensions: [] };
const rows = { meta, pagination: { page: 1, page_size: 25, total: 2 }, observations: [{ series_id: 'alcohol-total', release_id: 'release-1', source_geography: { source_code: '840', name: 'United States of America', kind: 'country', m49: '840', mapped: true, leaf: true }, year: 2000, raw_value: '8.2', display_value: '8.2', numeric_value: 8.2, status: 'numeric', publish_state: 'PUBLISHED', source_row_key: '1' }, { series_id: 'alcohol-total', release_id: 'release-1', source_geography: { source_code: '124', name: 'Canada', kind: 'country', m49: '124', mapped: true, leaf: true }, year: 2001, raw_value: '7.4', display_value: '7.4', numeric_value: 7.4, status: 'numeric', publish_state: 'PUBLISHED', source_row_key: '2' }] };

async function mockApi(page: Page) {
  await page.route('**/api/v1/catalog**', (route) => route.fulfill({ json: catalog }));
  await page.route('**/api/v1/geographies**', (route) => route.fulfill({ json: { meta, geographies: rows.observations.map((row) => row.source_geography) } }));
  await page.route('**/api/v1/groups**', (route) => route.fulfill({ json: { meta, groups: [] } }));
  await page.route('**/api/v1/observations**', (route) => {
    if (new URL(route.request().url()).pathname.endsWith('.csv')) {
      return route.fulfill({ contentType: 'text/csv', headers: { 'content-disposition': 'attachment; filename="context-atlas-observations.csv"' }, body: 'year,value\n2000,8.2\n' });
    }
    return route.fulfill({ json: rows });
  });
  await page.route('**/api/v1/maps/admin0-50m.geojson**', (route) => route.fulfill({ json: { type: 'FeatureCollection', features: [] } }));
  await page.route('**/api/v1/admin/session**', (route) => route.fulfill({ status: 401, contentType: 'application/problem+json', body: '{"title":"Unauthorized"}' }));
  await page.route('**/api/v1/feedback**', (route) => route.fulfill({ status: 202 }));
}

test('gallery leads to a shareable explorer, accessible by keyboard', async ({ page }) => {
  await mockApi(page);
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Four compact views, backed by this snapshot' })).toBeVisible();
  await page.getByRole('link', { name: /WHO EE6F72A.*Alcohol consumption/ }).click();
  await expect(page).toHaveURL(/\/explore/);
  await expect(page).toHaveURL(/snapshot=snapshot-1/);
  await page.keyboard.press('Tab');
  await expect(page.locator(':focus')).toBeVisible();
  await expect(new AxeBuilder({ page }).include('main').analyze()).resolves.toMatchObject({ violations: [] });
});

test('line chart offers native PNG and SVG exports', async ({ page }) => {
  await mockApi(page);
  await page.goto('/explore?view=line&series=alcohol-total&geographies=840&snapshot=snapshot-1');
  await expect(page).toHaveURL(/snapshot=snapshot-1/);
  await expect(page.getByRole('heading', { name: 'Alcohol consumption' })).toBeVisible();
  const [csv] = await Promise.all([page.waitForEvent('download'), page.getByRole('link', { name: /download filtered csv/i }).click()]);
  await expect(csv.suggestedFilename()).toMatch(/\.csv$/);
  const [png] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: 'Export chart as PNG' }).click()]);
  await expect(png.suggestedFilename()).toMatch(/\.png$/);
  const [svg] = await Promise.all([page.waitForEvent('download'), page.getByRole('button', { name: 'Export chart as SVG' }).click()]);
  await expect(svg.suggestedFilename()).toMatch(/\.svg$/);
});

test('mobile navigation remains available', async ({ page }) => {
  await mockApi(page);
  await page.setViewportSize({ width: 375, height: 720 });
  await page.goto('/');
  await page.getByRole('button', { name: 'Toggle navigation' }).click();
  await expect(page.getByRole('dialog', { name: 'Context Atlas' }).getByRole('link', { name: 'Explore', exact: true })).toBeVisible();
});
