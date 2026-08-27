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

describe('data snapshot URLs', () => {
  it('uses a compact catalog header and collapsed release citations', async () => {
    renderApp('/data');

    expect(await screen.findByRole('heading', { name: 'Data catalog' })).toBeInTheDocument();
    expect(screen.queryByText('Every release, dimension, and row remains inspectable.')).not.toBeInTheDocument();
    expect(await screen.findByText(/1 source · snapshot/)).toHaveTextContent('1 source · snapshot snapshot-2026-08-27');
    expect(screen.getByText('Release details').closest('details')).not.toHaveAttribute('open');
  });

  it('fetches and pins a fresh latest snapshot instead of the cached latest catalog', async () => {
    const user = userEvent.setup();
    let latest = catalogAt('snapshot-A');
    server.use(http.get('/api/v1/catalog', () => HttpResponse.json(latest)));

    renderApp('/data');

    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-A'));
    latest = catalogAt('snapshot-B');
    await user.click(await screen.findByRole('button', { name: 'View latest' }));
    await waitFor(() => expect(window.location.search).toContain('snapshot=snapshot-B'));
  });
});
