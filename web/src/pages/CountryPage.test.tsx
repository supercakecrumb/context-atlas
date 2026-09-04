import { screen, waitFor, within } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { http, HttpResponse } from 'msw';
import { alcoholSeries, observations, catalog, release, meta } from '../test/fixtures';
import { server } from '../test/server';
import { renderApp } from '../test/render';
import { exactCountryValue, portraitSeries } from './CountryPage';

describe('country portraits', () => {
  it('renders World Bank values with their source, unit and country-year note', async () => {
    const giniRelease = { ...release, dataset_id: 'gini', source_system: 'World Bank', citation: 'World Bank WDI: Gini index', source_url: 'https://data.worldbank.org/indicator/SI.POV.GINI' };
    const giniMeta = { ...meta, releases: [giniRelease] };
    const giniSeries = { ...alcoholSeries, id: 'gini-total', dataset_id: 'gini', name: 'Gini index', unit: 'index (0–100)' };
    server.use(
      http.get('/api/v1/catalog', () => HttpResponse.json({ ...catalog, meta: giniMeta, datasets: [{ ...catalog.datasets[0], id: 'gini', name: 'Gini index', source_system: 'World Bank', who_identifier: '', who_code: '', indicator_code: 'SI.POV.GINI', source_url: giniRelease.source_url, citation: giniRelease.citation, release: giniRelease }], series: [giniSeries] })),
      http.get('/api/v1/observations', () => HttpResponse.json({ ...observations, meta: giniMeta, observations: observations.observations.map((row) => ({ ...row, series_id: giniSeries.id, source_geography: { ...row.source_geography, source_system: 'World Bank' }, source_note: 'Based on an income survey.' })) })),
    );
    renderApp('/countries?country=840&year=2000');
    const card = await screen.findByRole('region', { name: 'Gini index' });
    expect(await within(card).findByText('Based on an income survey.')).toBeInTheDocument();
    expect(within(card).getAllByText('index (0–100)').length).toBeGreaterThan(0);
    expect(within(card).getAllByText('World Bank data as published · Context Atlas').length).toBeGreaterThan(0);
    expect(within(card).queryByText(/WHO data/)).not.toBeInTheDocument();
  });

  it('requires an unambiguous matching country, series and exact year, preserving zero', () => {
    const row = { ...observations.observations[0], numeric_value: 0, raw_value: '0', display_value: '0', status: 'numeric' as const };
    expect(exactCountryValue([row], row.series_id, '840', 2000)?.numeric_value).toBe(0);
    expect(exactCountryValue([row], row.series_id, '840', 2001)).toBeUndefined();
    expect(exactCountryValue([row], row.series_id, '124', 2000)).toBeUndefined();
    expect(exactCountryValue([row], 'another-series', '840', 2000)).toBeUndefined();
    expect(exactCountryValue([row, { ...row, source_row_key: 'ambiguous' }], row.series_id, '840', 2000)).toBeUndefined();
    expect(exactCountryValue([{ ...row, source_geography: { ...row.source_geography, leaf: false } }], row.series_id, '840', 2000)).toBeUndefined();
    expect(portraitSeries([{ ...alcoholSeries, id: 'female', dimensions: { SEX: 'FEMALE' } }, alcoholSeries])?.id).toBe(alcoholSeries.id);
  });

  it('pins the portrait and keeps missing headlines separate from historical trend data', async () => {
    renderApp('/countries?country=840&compare=124&year=2001');
    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-2026-08-27'));
    const card = await screen.findByRole('region', { name: 'Alcohol consumption' });
    await waitFor(() => expect(within(card).getByText('No published value')).toBeInTheDocument());
    expect(within(card).getByText('United States of America · 2001')).toBeInTheDocument();
    expect(within(card).getByText('Canada · 2001')).toBeInTheDocument();
    expect(within(card).getByRole('link', { name: 'Explore' })).toHaveAttribute('href', expect.stringContaining('geographies=840%2C124'));
    expect(window.location.search).toContain('year=2001');
  });

  it('does not silently replace an unavailable shared country or series', async () => {
    const page = renderApp('/countries?country=999&year=2000');
    expect(await screen.findByText('This country is unavailable in this snapshot.')).toBeInTheDocument();
    expect(window.location.search).toContain('country=999');
    page.unmount();
    renderApp('/countries?country=840&year=2000&series=deleted-series');
    expect(await screen.findByText('This portrait contains unavailable or conflicting series.')).toBeInTheDocument();
    expect(window.location.search).toContain('series=deleted-series');
  });
});
