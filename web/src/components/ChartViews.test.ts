import { describe, expect, it } from 'vitest';
import type { AssociationResult, Group as M49Group, Observation, Series } from '../api/types';
import { alcoholSeries, geometry, observations } from '../test/fixtures';
import { buildAssociationRegionSeries, buildAwareCompositionRows, buildExactYearLineSeries, buildMapTableRows, flagForISO2 } from './ChartViews';

const unitedStates = observations.observations[0] as Observation;

describe('chart data guards', () => {
  it('emits explicit nulls for years absent from a line series', () => {
    const laterYear: Observation = { ...unitedStates, year: 2002, numeric_value: 9.1, display_value: '9.1', source_row_key: 'a-2' };
    const [series] = buildExactYearLineSeries([unitedStates, laterYear], [2000, 2001, 2002]);

    expect(series.data).toEqual([[2000, 8.2], [2001, null], [2002, 9.1]]);
  });

  it('keeps sex variants for the same country as separate lines', () => {
    const female: Observation = { ...unitedStates, series_id: 'female', numeric_value: 7, display_value: '7', source_row_key: 'female' };
    const male: Observation = { ...unitedStates, series_id: 'male', numeric_value: 11, display_value: '11', source_row_key: 'male' };
    const total: Observation = { ...unitedStates, series_id: 'total', numeric_value: 9, display_value: '9', source_row_key: 'total' };

    const series = buildExactYearLineSeries([female, male, total], [2000, 2001], new Map([['female', 'FEMALE'], ['male', 'MALE'], ['total', 'TOTAL']]));

    expect(series.map((item) => item.name)).toEqual(['FEMALE', 'MALE', 'TOTAL']);
    expect(series.map((item) => item.data)).toEqual([[[2000, 7], [2001, null]], [[2000, 11], [2001, null]], [[2000, 9], [2001, null]]]);
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
    const rows = buildMapTableRows([unitedStates], geometry as GeoJSON.FeatureCollection, undefined, new Map([['124', 'CA']]));

    expect(rows).toEqual(expect.arrayContaining([expect.objectContaining({ name: 'Canada', iso2: 'CA', displayValue: 'No data', state: 'No published data' })]));
    expect(flagForISO2('CA')).toBe('🇨🇦');
  });

  it('uses only top-level UN M49 regions for association markers', () => {
    const groups: M49Group[] = [
      { id: 'm49:002', name: 'Africa', kind: 'region', parent_id: 'm49:001', classification_version: 'UN-M49', members: [{ m49: '710', name: 'South Africa' }] },
      { id: 'm49:019', name: 'Americas', kind: 'region', parent_id: 'm49:001', classification_version: 'UN-M49', members: [{ m49: '840', name: 'United States of America' }] },
      { id: 'm49:021', name: 'Northern America', kind: 'subregion', parent_id: 'm49:019', classification_version: 'UN-M49', members: [{ m49: '840', name: 'United States of America' }] },
    ];
    const points: AssociationResult['points'] = [
      { m49: '710', geography: 'South Africa', x: 1, y: 2 },
      { m49: '840', geography: 'United States of America', x: 3, y: 4 },
    ];

    const series = buildAssociationRegionSeries(points, groups);

    expect(series.map(({ name }) => name)).toEqual(['Africa', 'Americas']);
    expect(series[0]).toMatchObject({ symbol: 'circle', points: [points[0]] });
    expect(series[1]).toMatchObject({ symbol: 'rect', points: [points[1]] });
    expect(series[0].color).not.toBe(series[1].color);
  });
});
