import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { catalog } from '../test/fixtures';
import { renderApp } from '../test/render';
import { server } from '../test/server';

function catalogAt(snapshotID: string) {
  return { ...catalog, meta: { ...catalog.meta, snapshot: { ...catalog.meta.snapshot, id: snapshotID } } };
}

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
  });

  it('fetches and pins a fresh latest snapshot instead of the cached latest catalog', async () => {
    const user = userEvent.setup();
    let latest = catalogAt('snapshot-A');
    server.use(http.get('/api/v1/catalog', () => HttpResponse.json(latest)));

    renderApp('/explore');
    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-A'));

    latest = catalogAt('snapshot-B');
    await user.click(await screen.findByRole('button', { name: 'View latest' }));

    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-B'));
  });
});
