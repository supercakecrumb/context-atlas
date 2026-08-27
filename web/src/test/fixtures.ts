export const release = {
  id: 'release-2026-08-27',
  dataset_id: 'alcohol',
  sha256: 'abc123',
  source_url: 'https://data.who.int/indicators/i/EF38E6A/EE6F72A',
  accessed_at: '2026-08-27T02:15:00Z',
  citation: 'World Health Organization (WHO), Global Health Observatory.',
  parser_version: 'v0.1.0',
};

export const meta = { snapshot: { id: 'snapshot-2026-08-27', created_at: '2026-08-27T02:16:00Z', m49_reference_release: 'UN-M49-2026' }, releases: [release] };

export const alcoholSeries = {
  id: 'alcohol-total', dataset_id: 'alcohol', measure_id: 'alcohol-measure', name: 'Total alcohol consumption', unit: 'litres', statistic: 'mean', value_kind: 'number' as const, dimensions: { SEX: 'TOTAL' }, available_years: [2000, 2001],
};

export const suicideSeries = {
  id: 'suicide-total', dataset_id: 'suicide', measure_id: 'suicide-measure', name: 'Suicide mortality', unit: 'per 100 000', statistic: 'rate', value_kind: 'number' as const, dimensions: { SEX: 'TOTAL' }, available_years: [2000, 2001],
};

export const catalog = {
  meta,
  datasets: [
    { id: 'alcohol', name: 'Alcohol consumption', who_identifier: 'EE6F72A', who_code: 'SA_0000001688', source_url: release.source_url, citation: release.citation, capabilities: ['line', 'map', 'association', 'table'], release },
    { id: 'suicide', name: 'Suicide mortality', who_identifier: '16BBF41', who_code: 'SDGSUICIDE', source_url: 'https://data.who.int/indicators/i/F08B4FD/16BBF41', citation: release.citation, capabilities: ['line', 'map', 'association', 'table'], release: { ...release, dataset_id: 'suicide' } },
    { id: 'aware', name: 'AWaRe antibiotic consumption', who_identifier: '19E688D', who_code: 'GLASSAMC_AWARE', source_url: 'https://data.who.int/indicators/i/B4715F3/19E688D', citation: release.citation, capabilities: ['composition', 'table'], release: { ...release, dataset_id: 'aware' } },
  ],
  measures: [],
  series: [alcoholSeries, suicideSeries, { ...alcoholSeries, id: 'aware-access', dataset_id: 'aware', name: 'Access', value_kind: 'composition' as const }, { ...alcoholSeries, id: 'aware-watch', dataset_id: 'aware', name: 'Watch', value_kind: 'composition' as const }],
  dimensions: [],
};

export const observations = {
  meta,
  pagination: { page: 1, page_size: 25, total: 2 },
  observations: [
    { series_id: 'alcohol-total', release_id: release.id, source_geography: { source_code: '840', name: 'United States of America', kind: 'country', m49: '840', mapped: true, leaf: true }, year: 2000, raw_value: '8.2', display_value: '8.2', numeric_value: 8.2, status: 'numeric', publish_state: 'PUBLISHED', source_row_key: 'a-1' },
    { series_id: 'alcohol-total', release_id: release.id, source_geography: { source_code: '124', name: 'Canada', kind: 'country', m49: '124', mapped: true, leaf: true }, year: 2001, raw_value: '7.4', display_value: '7.4', numeric_value: 7.4, status: 'numeric', publish_state: 'PUBLISHED', source_row_key: 'a-2' },
  ],
};

export const geometry = {
  type: 'FeatureCollection',
  features: [
    { type: 'Feature', properties: { m49: '840', name: 'United States of America' }, geometry: { type: 'Polygon', coordinates: [[[-120, 30], [-70, 30], [-70, 50], [-120, 50], [-120, 30]]] } },
    { type: 'Feature', properties: { m49: '124', name: 'Canada' }, geometry: { type: 'Polygon', coordinates: [[[-120, 50], [-70, 50], [-70, 70], [-120, 70], [-120, 50]]] } },
  ],
};
