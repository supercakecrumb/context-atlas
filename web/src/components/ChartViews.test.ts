import { describe, expect, it } from 'vitest';
import type { Observation, Series } from '../api/types';
import { alcoholSeries, geometry, observations } from '../test/fixtures';
import { buildAwareCompositionRows, buildExactYearLineSeries, buildMapTableRows } from './ChartViews';

const unitedStates = observations.observations[0] as Observation;

describe('chart data guards', () => {
  it('emits explicit nulls for years absent from a line series', () => {
    const laterYear: Observation = { ...unitedStates, year: 2002, numeric_value: 9.1, display_value: '9.1', source_row_key: 'a-2' };
    const [series] = buildExactYearLineSeries([unitedStates, laterYear], [2000, 2001, 2002]);

    expect(series.data).toEqual([[2000, 8.2], [2001, null], [2002, 9.1]]);
  });

  it('keeps incomplete AWaRe rows out of the normalized composition', () => {
    const classes = new Map<string, Series>([
      ['access', { ...alcoholSeries, id: 'access', name: 'Access' } as Series],
      ['watch', { ...alcoholSeries, id: 'watch', name: 'Watch' } as Series],
      ['reserve', { ...alcoholSeries, id: 'reserve', name: 'Reserve' } as Series],
    ]);
    const access: Observation = { ...unitedStates, series_id: 'access', numeric_value: 2, display_value: '2', source_row_key: 'access' };
    const watch: Observation = { ...unitedStates, series_id: 'watch', numeric_value: 3, display_value: '3', source_row_key: 'watch' };
    const reserveMissing: Observation = { ...unitedStates, series_id: 'reserve', numeric_value: undefined, display_value: '—', status: 'missing', source_row_key: 'reserve' };

    expect(buildAwareCompositionRows([access, watch, reserveMissing], classes)).toEqual([expect.objectContaining({ status: 'incomplete', total: null, missingClasses: ['Reserve'] })]);
  });

  it('includes boundary areas with no published map value in the accessible map table', () => {
    const rows = buildMapTableRows([unitedStates], geometry as GeoJSON.FeatureCollection);

    expect(rows).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'Canada', displayValue: 'No data', state: 'No published data' })]));
  });
});
