import { Box, Group, ScrollArea, Stack, Table, Text, Title } from '@mantine/core';
import type { EChartsOption } from 'echarts';
import { useMemo, useRef, type ReactNode } from 'react';
import EChartsCore, { type EChartsReactRef } from 'react-echarts-library/core';
import { schemeYlGnBu } from 'd3-scale-chromatic';
import type { AssociationResult, Observation, Series } from '../api/types';
import { echarts } from '../echarts';
import { ChartActions } from './ChartActions';

const attribution = 'WHO data as published · Context Atlas · Natural Earth map geometry where shown';

function exportAttribution(text = attribution) {
  return { type: 'text' as const, right: 10, bottom: 4, silent: true, style: { text, fill: '#617078', font: '11px sans-serif' } };
}

function formatNumber(value: number | null | undefined): string {
  return value === null || value === undefined ? '—' : new Intl.NumberFormat('en-US', { maximumFractionDigits: 3 }).format(value);
}

function chartTitle(title: string, unit?: string): string {
  return unit ? `${title} (${unit})` : title;
}

export function isPublishedNumeric(observation: Observation): observation is Observation & { numeric_value: number } {
  return observation.status === 'numeric'
    && observation.publish_state === 'PUBLISHED'
    && observation.numeric_value !== null
    && observation.numeric_value !== undefined;
}

export function isMapEligible(observation: Observation): boolean {
  return isPublishedNumeric(observation)
    && Boolean(observation.source_geography.m49)
    && observation.source_geography.mapped
    && observation.source_geography.leaf;
}

function ChartTable({ children, caption }: { children: ReactNode; caption: string }) {
  return (
    <ScrollArea mt="lg" type="auto">
      <Table striped highlightOnHover withTableBorder captionSide="top" miw={540}>
        <Table.Caption>{caption}</Table.Caption>
        {children}
      </Table>
    </ScrollArea>
  );
}

function AccessibleChartTable({ compact, children, caption }: { compact?: boolean; children: ReactNode; caption: string }) {
  const table = <ChartTable caption={caption}>{children}</ChartTable>;
  return compact ? <details><summary>Accessible data table</summary>{table}</details> : table;
}

export function buildExactYearLineSeries(observations: Observation[], availableYears: number[] = []) {
  const years = [...new Set([...availableYears, ...observations.map((observation) => observation.year)])].sort((a, b) => a - b);
  const byGeography = new Map<string, { name: string; rows: Observation[] }>();
  observations.forEach((observation) => {
    const key = observation.source_geography.source_code;
    const geography = byGeography.get(key) ?? { name: observation.source_geography.name, rows: [] };
    geography.rows.push(observation);
    byGeography.set(key, geography);
  });

  return [...byGeography.entries()]
    .sort(([, a], [, b]) => a.name.localeCompare(b.name))
    .map(([key, geography]) => {
      const numericByYear = new Map(geography.rows.filter(isPublishedNumeric).map((row) => [row.year, row.numeric_value]));
      return {
        id: key,
        name: geography.name,
        type: 'line' as const,
        showSymbol: numericByYear.size < 24,
        connectNulls: false,
        data: years.map((year) => [year, numericByYear.get(year) ?? null] as [number, number | null]),
      };
    });
}

export function LineChartView({ observations, availableYears, title, unit, compact }: { observations: Observation[]; availableYears?: number[]; title: string; unit?: string; compact?: boolean }) {
  const chartRef = useRef<EChartsReactRef>(null);
  const publishedRows = useMemo(() => observations.filter(isPublishedNumeric), [observations]);
  const series = useMemo(() => buildExactYearLineSeries(observations, availableYears), [availableYears, observations]);

  const option: EChartsOption = {
    animationDuration: 280,
    aria: { enabled: true, description: `${chartTitle(title, unit)} by exact year. ${attribution}.` },
    grid: { left: 54, right: 24, top: 54, bottom: 78, containLabel: false },
    graphic: exportAttribution(),
    legend: { type: 'scroll', top: 4 },
    tooltip: { trigger: 'axis', valueFormatter: (value) => formatNumber(Number(value)) },
    xAxis: { type: 'value', name: 'Exact year', minInterval: 1, axisLabel: { formatter: '{value}' } },
    yAxis: { type: 'value', name: unit, scale: true },
    series,
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between" align="start">
        <Box><Title order={2} size="h3">{title}</Title><Text size="sm" c="dimmed">Each point is a published exact-year observation.</Text></Box>
        <ChartActions chartRef={chartRef} title={title} />
      </Group>
      <EChartsCore ref={chartRef} echarts={echarts} option={option} opts={{ renderer: 'svg' }} autoResize style={{ height: compact ? 260 : 460, width: '100%' }} />
      <Text size="xs" c="dimmed">{attribution}</Text>
      <AccessibleChartTable compact={compact} caption={`${title}: accessible exact-year values`}>
        <Table.Thead><Table.Tr><Table.Th>Country or area</Table.Th><Table.Th>Year</Table.Th><Table.Th>Display value</Table.Th><Table.Th>Lower bound</Table.Th><Table.Th>Upper bound</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{publishedRows.map((row) => <Table.Tr key={`${row.source_geography.source_code}-${row.year}`}><Table.Td>{row.source_geography.name}</Table.Td><Table.Td>{row.year}</Table.Td><Table.Td>{row.display_value}</Table.Td><Table.Td>{formatNumber(row.lower_bound)}</Table.Td><Table.Td>{formatNumber(row.upper_bound)}</Table.Td></Table.Tr>)}</Table.Tbody>
      </AccessibleChartTable>
    </Stack>
  );
}

function m49FromFeature(feature: GeoJSON.Feature): string | undefined {
  const properties = feature.properties as Record<string, unknown> | null;
  const value = properties?.m49 ?? properties?.M49 ?? properties?.iso_n3 ?? properties?.ISO_N3;
  return value === undefined || value === null ? undefined : String(value).padStart(3, '0');
}

function nameFromFeature(feature: GeoJSON.Feature): string | undefined {
  const properties = feature.properties as Record<string, unknown> | null;
  const value = properties?.name ?? properties?.NAME ?? properties?.admin ?? properties?.ADMIN;
  return typeof value === 'string' ? value : undefined;
}

export type MapTableRow = { key: string; name: string; displayValue: string; state: string };

export function buildMapTableRows(observations: Observation[], geometry: GeoJSON.FeatureCollection, selectedM49?: Set<string>): MapTableRow[] {
  const eligibleByM49 = new Map(observations.filter(isMapEligible).map((row) => [row.source_geography.m49!, row]));
  const geometryM49 = new Set<string>();
  const mapRows = geometry.features.flatMap((feature) => {
    const m49 = m49FromFeature(feature);
    if (!m49) return [];
    geometryM49.add(m49);
    const observation = eligibleByM49.get(m49);
    const outsideGroup = Boolean(selectedM49 && !selectedM49.has(m49));
    const state = observation
      ? outsideGroup ? 'Published value; outside selected group (dimmed)' : 'Published value'
      : outsideGroup ? 'No published data; outside selected group (dimmed)' : 'No published data';
    return [{ key: m49, name: nameFromFeature(feature) ?? m49, displayValue: observation?.display_value ?? 'No data', state }];
  });
  const unmatchedRows = [...eligibleByM49.values()]
    .filter((row) => !geometryM49.has(row.source_geography.m49!))
    .map((row) => ({ key: `${row.source_geography.source_code}-${row.year}`, name: row.source_geography.name, displayValue: row.display_value, state: 'No matching boundary geometry' }));
  return [...mapRows, ...unmatchedRows];
}

export function MapChartView({
  observations,
  geometry,
  title,
  unit,
  selectedM49,
  compact,
}: {
  observations: Observation[];
  geometry: GeoJSON.FeatureCollection;
  title: string;
  unit?: string;
  selectedM49?: Set<string>;
  compact?: boolean;
}) {
  const chartRef = useRef<EChartsReactRef>(null);
  echarts.registerMap('context-atlas-admin0-50m', geometry as unknown as Parameters<typeof echarts.registerMap>[1]);

  const eligibleRows = useMemo(() => observations.filter(isMapEligible), [observations]);
  const eligibleByM49 = useMemo(() => new Map(eligibleRows.map((row) => [row.source_geography.m49!, row])), [eligibleRows]);

  const mapRows = useMemo(() => {
    return geometry.features.flatMap((feature) => {
      const m49 = m49FromFeature(feature);
      if (!m49) return [];
      const observation = eligibleByM49.get(m49);
      const outsideGroup = selectedM49 && !selectedM49.has(m49);
      return [{
        name: m49,
        value: observation?.numeric_value ?? undefined,
        itemStyle: outsideGroup ? { areaColor: '#e5e1d8', opacity: 0.45 } : undefined,
      }];
    });
  }, [eligibleByM49, geometry.features, selectedM49]);

  const numericValues = eligibleRows.map((row) => row.numeric_value!);
  const featureNames = useMemo(() => new Map(geometry.features.flatMap((feature) => {
    const m49 = m49FromFeature(feature);
    const name = nameFromFeature(feature);
    return m49 && name ? [[m49, name] as const] : [];
  })), [geometry.features]);
  const accessibleRows = useMemo(() => buildMapTableRows(observations, geometry, selectedM49), [geometry, observations, selectedM49]);
  const option: EChartsOption = {
    animationDuration: 280,
    aria: { enabled: true, description: `${chartTitle(title, unit)} choropleth. No data is shown separately from numeric zero. ${attribution}.` },
    tooltip: {
      trigger: 'item',
      formatter: (params) => {
        const item = Array.isArray(params) ? params[0] : params;
        const m49 = String(item?.name ?? '');
        return `${featureNames.get(m49) ?? m49}<br/>${formatNumber((item?.value as number | null | undefined) ?? null)}${unit ? ` ${unit}` : ''}`;
      },
    },
    visualMap: {
      min: numericValues.length ? Math.min(...numericValues) : 0,
      max: numericValues.length ? Math.max(...numericValues) : 1,
      left: 12,
      bottom: 14,
      text: ['Higher', 'Lower'],
      calculable: true,
      inRange: { color: schemeYlGnBu[8] },
      outOfRange: { color: '#d8d4cb' },
    },
    graphic: exportAttribution('WHO data · Context Atlas · Natural Earth'),
    series: [{
      name: title,
      type: 'map',
      map: 'context-atlas-admin0-50m',
      nameProperty: 'm49',
      roam: true,
      emphasis: { label: { show: false }, itemStyle: { areaColor: '#ee9b48' } },
      data: mapRows,
    }],
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between" align="start">
        <Box><Title order={2} size="h3">{title}</Title><Text size="sm" c="dimmed">No data and numeric zero are deliberately distinct.</Text></Box>
        <ChartActions chartRef={chartRef} title={title} />
      </Group>
      <EChartsCore ref={chartRef} echarts={echarts} option={option} opts={{ renderer: 'svg' }} autoResize style={{ height: compact ? 260 : 460, width: '100%' }} />
      <Group gap="xs" aria-label="Map legend: no published data is shown in grey">
        <Box aria-hidden style={{ width: 13, height: 13, borderRadius: 2, background: '#d8d4cb', border: '1px solid #aaa59b' }} />
        <Text size="xs">No published data</Text>
      </Group>
      <Text size="xs" c="dimmed">{attribution}. Areas outside a selected group are dimmed. Boundaries are illustrative.</Text>
      <AccessibleChartTable compact={compact} caption={`${title}: accessible map values`}>
        <Table.Thead><Table.Tr><Table.Th>Country or area</Table.Th><Table.Th>Display value</Table.Th><Table.Th>Map state</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{accessibleRows.map((row) => <Table.Tr key={row.key}><Table.Td>{row.name}</Table.Td><Table.Td>{row.displayValue}</Table.Td><Table.Td>{row.state}</Table.Td></Table.Tr>)}</Table.Tbody>
      </AccessibleChartTable>
    </Stack>
  );
}

export function AssociationChartView({ result, xLabel, yLabel, compact }: { result: AssociationResult; xLabel: string; yLabel: string; compact?: boolean }) {
  const chartRef = useRef<EChartsReactRef>(null);
  const option: EChartsOption = {
    animationDuration: 280,
    aria: { enabled: true, description: `Scatter plot of ${xLabel} and ${yLabel} using independently selected exact years. ${attribution}.` },
    grid: { left: 60, right: 24, top: 22, bottom: 78 },
    graphic: exportAttribution(),
    tooltip: { formatter: (params) => {
      const item = Array.isArray(params) ? params[0] : params;
      const data = item?.data as [number, number, string];
      return `${data[2]}<br/>${xLabel}: ${formatNumber(data[0])}<br/>${yLabel}: ${formatNumber(data[1])}`;
    } },
    xAxis: { type: 'value', name: xLabel, nameLocation: 'middle', nameGap: 35, scale: true },
    yAxis: { type: 'value', name: yLabel, nameLocation: 'middle', nameGap: 48, scale: true },
    series: [{ type: 'scatter', symbolSize: 9, data: result.points.map((point) => [point.x, point.y, point.geography]) }],
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between" align="start">
        <Box><Title order={2} size="h3">Explore association</Title><Text size="sm" c="dimmed">Paired by canonical leaf M49 geography; this does not establish cause.</Text></Box>
        <ChartActions chartRef={chartRef} title="context-atlas-association" />
      </Group>
      <EChartsCore ref={chartRef} echarts={echarts} option={option} opts={{ renderer: 'svg' }} autoResize style={{ height: compact ? 260 : 460, width: '100%' }} />
      <Text size="xs" c="dimmed">{attribution}</Text>
      <AccessibleChartTable compact={compact} caption="Explore association: accessible paired values">
        <Table.Thead><Table.Tr><Table.Th>Country or area</Table.Th><Table.Th>{xLabel}</Table.Th><Table.Th>{yLabel}</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{result.points.map((point) => <Table.Tr key={point.m49}><Table.Td>{point.geography}</Table.Td><Table.Td>{formatNumber(point.x)}</Table.Td><Table.Td>{formatNumber(point.y)}</Table.Td></Table.Tr>)}</Table.Tbody>
      </AccessibleChartTable>
    </Stack>
  );
}

export function awareClass(series: Series | undefined): 'Access' | 'Watch' | 'Reserve' | 'Other' {
  const text = `${series?.name ?? ''} ${Object.values(series?.dimensions ?? {}).join(' ')}`.toLowerCase();
  if (text.includes('access')) return 'Access';
  if (text.includes('watch')) return 'Watch';
  if (text.includes('reserve')) return 'Reserve';
  return 'Other';
}

const awareClasses = ['Access', 'Watch', 'Reserve'] as const;
type AwareClass = typeof awareClasses[number];
type CompositionStatus = 'complete' | 'incomplete' | 'zero_total';

export type AwareCompositionRow = {
  key: string;
  name: string;
  values: Partial<Record<AwareClass, number>>;
  missingClasses: AwareClass[];
  status: CompositionStatus;
  total: number | null;
};

export function buildAwareCompositionRows(observations: Observation[], seriesById: Map<string, Series>): AwareCompositionRow[] {
  const grouped = new Map<string, { name: string; values: Partial<Record<AwareClass, number>> }>();
  observations.forEach((observation) => {
    const className = awareClass(seriesById.get(observation.series_id));
    if (className === 'Other') return;
    const key = observation.source_geography.source_code;
    const group = grouped.get(key) ?? { name: observation.source_geography.name, values: {} };
    if (isPublishedNumeric(observation)) group.values[className] = observation.numeric_value;
    grouped.set(key, group);
  });
  return [...grouped.entries()]
    .map(([key, group]) => {
      const missingClasses = awareClasses.filter((className) => group.values[className] === undefined);
      const total = missingClasses.length ? null : awareClasses.reduce((sum, className) => sum + group.values[className]!, 0);
      const status: CompositionStatus = missingClasses.length ? 'incomplete' : total === 0 ? 'zero_total' : 'complete';
      return { key, name: group.name, values: group.values, missingClasses, total, status };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function CompositionChartView({ observations, seriesById, title, compact }: { observations: Observation[]; seriesById: Map<string, Series>; title: string; compact?: boolean }) {
  const chartRef = useRef<EChartsReactRef>(null);
  const rows = useMemo(() => buildAwareCompositionRows(observations, seriesById), [observations, seriesById]);
  const chartRows = rows.filter((row) => row.status === 'complete');
  const option: EChartsOption = {
    animationDuration: 280,
    aria: { enabled: true, description: `${title} as a 100% stacked composition. ${attribution}.` },
    grid: { left: 54, right: 26, top: 48, bottom: 110 },
    graphic: exportAttribution(),
    legend: { top: 4 },
    tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, valueFormatter: (value) => `${formatNumber(Number(value))}%` },
    xAxis: { type: 'category', data: chartRows.map((row) => row.name), axisLabel: { rotate: 55, interval: 0 } },
    yAxis: { type: 'value', min: 0, max: 100, axisLabel: { formatter: '{value}%' } },
    series: awareClasses.map((className, index) => ({
      name: className,
      type: 'bar' as const,
      stack: 'share',
      emphasis: { focus: 'series' },
      itemStyle: { color: ['#2f8075', '#e59a49', '#9d5b8d', '#526f9e'][index % 4] },
      data: chartRows.map((row) => row.values[className]! / row.total! * 100),
    })),
  };

  return (
    <Stack gap="sm">
      <Group justify="space-between" align="start">
        <Box><Title order={2} size="h3">{title}</Title><Text size="sm" c="dimmed">Only complete published Access, Watch, and Reserve triples make the 100% stack. Incomplete rows remain no data.</Text></Box>
        {chartRows.length > 0 && <ChartActions chartRef={chartRef} title={title} />}
      </Group>
      {chartRows.length > 0 ? <><EChartsCore ref={chartRef} echarts={echarts} option={option} opts={{ renderer: 'svg' }} autoResize style={{ height: compact ? 260 : 460, width: '100%' }} /><Text size="xs" c="dimmed">{attribution}</Text></> : <Text role="status" c="dimmed">No country or area has all three published numeric classes for this exact year.</Text>}
      <AccessibleChartTable compact={compact} caption={`${title}: accessible composition table`}>
        <Table.Thead><Table.Tr><Table.Th>Country or area</Table.Th>{awareClasses.map((className) => <Table.Th key={className}>{className} share</Table.Th>)}<Table.Th>Composition status</Table.Th></Table.Tr></Table.Thead>
        <Table.Tbody>{rows.map((row) => <Table.Tr key={row.key}><Table.Td>{row.name}</Table.Td>{awareClasses.map((className) => <Table.Td key={className}>{row.status === 'complete' ? `${formatNumber(row.values[className]! / row.total! * 100)}%` : 'No data'}</Table.Td>)}<Table.Td>{row.status === 'complete' ? 'Included in chart' : row.status === 'zero_total' ? 'Published numeric classes total zero; no percentage share' : `Missing published ${row.missingClasses.join(', ')}`}</Table.Td></Table.Tr>)}</Table.Tbody>
      </AccessibleChartTable>
    </Stack>
  );
}
