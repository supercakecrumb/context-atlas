import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { describe, expect, it } from 'vitest';
import { renderApp } from '../test/render';
import { server } from '../test/server';
import { isWHOIndicatorPage } from './AdminPage';

const preview = {
  conflicting_duplicates: 0,
  created_at: '2026-08-27T02:15:00Z',
  dimensions: [],
  exact_duplicates: 0,
  expires_at: '2026-08-28T02:15:00Z',
  headers: ['YEAR', 'GEO_NUMERIC', 'VALUE_N'],
  id: 'preview-1',
  indicator_url: 'https://data.who.int/indicators/i/16BBF41',
  measures: [],
  rows: { accepted_rows: 1, collapsed_duplicates: 0, rejected_rows: 0, source_rows: 1 },
  status: 'ready',
  units: [],
  unmapped_geographies: [],
  warnings: [],
};

describe('WHO indicator page validation', () => {
  it.each([
    'https://data.who.int/indicators/i/16BBF41',
    'https://data.who.int/indicators/i/F08B4FD/16BBF41',
  ])('accepts canonical page %s', (url) => {
    expect(isWHOIndicatorPage(url)).toBe(true);
  });

  it.each([
    'http://data.who.int/indicators/i/16BBF41',
    'https://data.who.int/indicators/i/SHORT',
    'https://data.who.int/indicators/i/F08B4FD/16BBF41?download=1',
  ])('rejects non-canonical page %s', (url) => {
    expect(isWHOIndicatorPage(url)).toBe(false);
  });

  it('loads a created preview through the generated preview query', async () => {
    const user = userEvent.setup();
    let sessionReads = 0;
    let previewReads = 0;
    server.use(
      http.get('/api/v1/admin/session', () => {
        sessionReads += 1;
        return HttpResponse.json({ csrf_token: 'csrf', expires_at: '2026-09-03T02:15:00Z', owner_telegram_id: 1 });
      }),
      http.get('/api/v1/admin/import-runs', () => HttpResponse.json({ pagination: { page: 1, page_size: 25, total: 0 }, runs: [] })),
      http.get('/api/v1/admin/freshness', () => HttpResponse.json({ datasets: [] })),
      http.post('/api/v1/admin/import-previews', () => HttpResponse.json({ ...preview, status: 'pending' }, { status: 202 })),
      http.get('/api/v1/admin/import-previews/:previewID', () => {
        previewReads += 1;
        return HttpResponse.json(preview);
      }),
    );

    renderApp('/admin');
    await waitFor(() => expect(sessionReads).toBe(1));
    await waitFor(() => expect(document.body.textContent).toContain('Import carefully, preserve every release.'));
    await user.type(await screen.findByPlaceholderText('https://data.who.int/indicators/i/.../...'), preview.indicator_url);
    await user.click(screen.getByRole('button', { name: 'Create staged preview' }));

    expect(await screen.findByRole('heading', { name: 'Staged import preview' })).toBeInTheDocument();
    await waitFor(() => expect(previewReads).toBe(1));
    expect(screen.getByText('ready')).toBeInTheDocument();
  });
});
