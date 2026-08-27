import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { alcoholSeries, catalog, observations } from '../test/fixtures';
import { renderApp } from '../test/render';
import { server } from '../test/server';

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

  it('loads all sex variants for one line indicator', async () => {
    const female = { ...alcoholSeries, id: 'alcohol-female', name: 'Total alcohol consumption · Sex: FEMALE', dimensions: { SEX: 'FEMALE' } };
    const male = { ...alcoholSeries, id: 'alcohol-male', name: 'Total alcohol consumption · Sex: MALE', dimensions: { SEX: 'MALE' } };
    let requestedSeries = '';
    server.use(
      http.get('/api/v1/catalog', () => HttpResponse.json({ ...catalog, series: [...catalog.series, female, male] })),
      http.get('/api/v1/observations', ({ request }) => {
        requestedSeries = new URL(request.url).searchParams.get('series') ?? '';
        return HttpResponse.json(observations);
      }),
    );

    renderApp('/explore?view=line&series=alcohol-total&geographies=840');

    await waitFor(() => expect(requestedSeries.split(',')).toEqual(expect.arrayContaining(['alcohol-total', 'alcohol-female', 'alcohol-male'])));
  });
});
