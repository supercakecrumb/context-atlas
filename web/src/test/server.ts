import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { catalog, geometry, meta, observations } from './fixtures';

export const server = setupServer(
  http.get('/api/v1/catalog', () => HttpResponse.json(catalog)),
  http.get('/api/v1/geographies', () => HttpResponse.json({ meta, geographies: observations.observations.map((item) => item.source_geography) })),
  http.get('/api/v1/groups', () => HttpResponse.json({ meta, groups: [] })),
  http.get('/api/v1/maps/admin0-50m.geojson', () => HttpResponse.json(geometry)),
  http.get('/api/v1/observations', () => HttpResponse.json(observations)),
  http.get('/api/v1/association', () => HttpResponse.json({ meta, x_series: 'alcohol-total', x_year: 2000, y_series: 'suicide-total', y_year: 2001, coverage: { selected_universe: 2, x_only_missing: 0, y_only_missing: 0, both_missing: 0, paired: 2 }, pearson_r: null, points: [], warnings: ['Paired n is below 3.'] })),
  http.post('/api/v1/feedback', () => new HttpResponse(null, { status: 202 })),
  http.get('/api/v1/admin/session', () => new HttpResponse(null, { status: 401 })),
);
