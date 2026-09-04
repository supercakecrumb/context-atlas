import { act, screen, waitFor } from '@testing-library/react';
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

  it('plays exact calendar years, waits for data, pauses, and stops at the end or on leaving the map', async () => {
    const user = userEvent.setup();
    let releaseNextYear!: () => void;
    const nextYearReady = new Promise<void>((resolve) => { releaseNextYear = resolve; });
    const requestedYears: string[] = [];
    server.use(
      http.get('/api/v1/catalog', () => HttpResponse.json({ ...catalog, series: catalog.series.map((series) => ({ ...series, available_years: [2000, 2002] })) })),
      http.get('/api/v1/observations', async ({ request }) => {
        const year = new URL(request.url).searchParams.get('years') ?? '';
        requestedYears.push(year);
        if (year === '2001') await nextYearReady;
        const rows = year === '2001' ? [] : observations.observations.map((row) => ({ ...row, year: Number(year) }));
        return HttpResponse.json({ ...observations, observations: rows, pagination: { ...observations.pagination, total: rows.length } });
      }),
    );
    renderApp('/explore?snapshot=snapshot-2026-08-27&view=map&series=alcohol-total&year=2002');
    await screen.findByRole('heading', { name: /Alcohol.*2002/i });
    await user.click(screen.getByRole('button', { name: 'Play years' }));
    await screen.findByRole('heading', { name: /Alcohol.*2000/i });
    await waitFor(() => expect(requestedYears).toContain('2001'));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1_100)); });
    expect(screen.getByRole('combobox', { name: 'Exact year' })).toHaveValue('2000');

    await user.click(screen.getByRole('button', { name: 'Pause' }));
    releaseNextYear();
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1_100)); });
    expect(window.location.search).toContain('year=2000');
    await user.click(screen.getByRole('button', { name: 'Play years' }));
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'Exact year' })).toHaveValue('2001'), { timeout: 2_500 });
    expect(screen.getByText('No published mapped rows match this exact year.')).toBeInTheDocument();

    await user.click(screen.getByRole('combobox', { name: 'Exact year' }));
    await user.keyboard('{ArrowUp}{Enter}');
    expect(screen.getByRole('combobox', { name: 'Exact year' })).toHaveValue('2000');
    expect(screen.getByRole('button', { name: 'Play years' })).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: 'Play years' }));
    await waitFor(() => expect(window.location.search).toContain('year=2002'), { timeout: 4_000 });
    expect(screen.getByRole('button', { name: 'Play years' })).toBeInTheDocument();
    expect(window.location.search).toContain('snapshot=snapshot-2026-08-27');

    await user.click(screen.getByRole('button', { name: 'Play years' }));
    await waitFor(() => expect(window.location.search).toContain('year=2000'));
    await user.click(screen.getByRole('tab', { name: 'Table' }));
    await act(async () => { await new Promise((resolve) => setTimeout(resolve, 1_100)); });
    expect(window.location.search).toContain('year=2000');
    expect(screen.queryByRole('button', { name: 'Pause' })).not.toBeInTheDocument();
  }, 15_000);

  it('stops playback and keeps the current map when the next year fails to load', async () => {
    const user = userEvent.setup();
    server.use(http.get('/api/v1/observations', ({ request }) => new URL(request.url).searchParams.get('years') === '2001'
      ? new HttpResponse(null, { status: 503 }) : HttpResponse.json(observations)));
    renderApp('/explore?snapshot=snapshot-2026-08-27&view=map&series=alcohol-total&year=2000');
    await user.click(await screen.findByRole('button', { name: 'Play years' }));
    expect(await screen.findByText('Playback stopped: the next year could not be loaded.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Play years' })).toBeInTheDocument();
    expect(window.location.search).toContain('year=2000');
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
