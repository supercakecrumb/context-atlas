import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { catalog, observations, suicideSeries } from '../test/fixtures';
import { renderApp } from '../test/render';
import { server } from '../test/server';
import { seriesWithDimension } from './ExplorePage';

describe('exact-year explorer', () => {
  it('does not replace an unavailable map year with another year', async () => {
    renderApp('/explore?view=map&series=alcohol-total&year=1999');
    expect(await screen.findByText(/This exact year is unavailable/i)).toBeInTheDocument();
    expect(window.location.search).toContain('year=1999');
  });

  it('writes a user-selected explorer view into the share URL', async () => {
    const user = userEvent.setup();
    renderApp('/explore');
    await user.click(await screen.findByRole('tab', { name: 'Map' }));
    await waitFor(() => expect(window.location.search).toContain('view=map'));
  });

  it('pins the resolved latest snapshot into a shared explorer URL', async () => {
    renderApp('/explore');

    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-2026-08-27'));
    expect(screen.queryByText('Catalog snapshot')).not.toBeInTheDocument();
  });

  it('defaults a map to the latest exact year and hides geography controls', async () => {
    renderApp('/explore?view=map&series=alcohol-total');

    await waitFor(() => expect(window.location.search).toContain('year=2001'));
    expect(screen.queryByText('Group filters')).not.toBeInTheDocument();
    expect(screen.queryByText(/Countries and areas/)).not.toBeInTheDocument();
  });

  it('defaults line charts to the total tuple and exposes dimension controls', async () => {
    const total = { ...suicideSeries, dimensions: { AGE: 'TOTAL', SEX: 'TOTAL' } };
    const female = { ...total, id: 'suicide-female', name: 'Suicide mortality · Age: TOTAL, Sex: FEMALE', dimensions: { AGE: 'TOTAL', SEX: 'FEMALE' } };
    const youngTotal = { ...total, id: 'suicide-young-total', name: 'Suicide mortality · Age: Y15T19, Sex: TOTAL', dimensions: { AGE: 'Y15T19', SEX: 'TOTAL' } };
    const youngFemale = { ...total, id: 'suicide-young-female', name: 'Suicide mortality · Age: Y15T19, Sex: FEMALE', dimensions: { AGE: 'Y15T19', SEX: 'FEMALE' } };
    let requestedSeries = '';
    server.use(
      http.get('/api/v1/catalog', () => HttpResponse.json({
        ...catalog,
        dimensions: [{ code: 'AGE', name: 'Age', values: ['TOTAL', 'Y15T19'] }, { code: 'SEX', name: 'Sex', values: ['TOTAL', 'FEMALE'] }],
        series: [...catalog.series.filter((series) => series.id !== suicideSeries.id), total, female, youngTotal, youngFemale],
      })),
      http.get('/api/v1/observations', ({ request }) => {
        requestedSeries = new URL(request.url).searchParams.get('series') ?? '';
        return HttpResponse.json(observations);
      }),
    );

    renderApp('/explore?view=line&series=suicide-total&geographies=840,124');

    await waitFor(() => expect(requestedSeries).toBe('suicide-total'));
    expect(screen.getByRole('combobox', { name: 'Age' })).toHaveValue('Total');
    expect(screen.getByRole('combobox', { name: 'Sex' })).toHaveValue('Total');
    expect(seriesWithDimension([total, female, youngTotal, youngFemale], total, 'AGE', 'Y15T19')?.id).toBe('suicide-young-total');
    expect(seriesWithDimension([total, female, youngTotal, youngFemale], youngTotal, 'SEX', 'FEMALE')?.id).toBe('suicide-young-female');
    expect(seriesWithDimension([total, youngFemale], total, 'AGE', 'Y15T19')?.id).toBe('suicide-young-female');
  });
});
