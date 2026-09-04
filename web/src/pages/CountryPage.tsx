import { Alert, Anchor, Badge, Box, Button, Container, Group, Select, SimpleGrid, Stack, Text, Title } from '@mantine/core';
import { useClipboard } from '@mantine/hooks';
import { ArrowRight, Check, Link as LinkIcon } from 'lucide-react';
import { parseAsArrayOf, parseAsInteger, parseAsString, useQueryState } from 'nuqs';
import { useEffect, useMemo } from 'react';
import { Link } from 'react-router';
import EChartsCore from 'react-echarts-library/core';
import { useCatalog, useChartObservations, useGeographies, useMapGeometry } from '../api/queries';
import type { CatalogResponse, Dataset, Geography, Observation, Series } from '../api/types';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { flagForISO2, isPublishedNumeric, LineChartView } from '../components/ChartViews';
import { Provenance } from '../components/Provenance';
import { PublishedValue } from '../components/PublishedValue';
import { echarts } from '../echarts';

// A default is always one published tuple; dimensions are displayed and selectable.
export function portraitSeries(series: Series[]): Series | undefined {
  return [...series].sort((a, b) => {
    const details = (item: Series) => Object.values(item.dimensions).filter((value) => value.toUpperCase() !== 'TOTAL').length;
    return details(a) - details(b) || a.name.localeCompare(b.name) || a.id.localeCompare(b.id);
  })[0];
}

function seriesLabel(series: Series): string {
  const dimensions = Object.entries(series.dimensions).map(([key, value]) => `${key.replace(/^DIM_/, '')}: ${value}`);
  return `${series.name}${dimensions.length ? ` · ${dimensions.join(' · ')}` : ''} · ${series.unit}`;
}

export function exactCountryValue(rows: Observation[], series: string, country: string, year: number): Observation | undefined {
  const matches = rows.filter((row) => row.series_id === series && row.year === year
    && row.source_geography.mapped && row.source_geography.leaf && row.source_geography.m49 === country);
  // ponytail: ambiguous source-geography rows show no headline; resolve in the source before choosing a value.
  return matches.length === 1 ? matches[0] : undefined;
}

export function CountryPage() {
  const [snapshot, setSnapshot] = useQueryState('snapshot', parseAsString);
  const [country, setCountry] = useQueryState('country', parseAsString);
  const [compare, setCompare] = useQueryState('compare', parseAsString);
  const [year, setYear] = useQueryState('year', parseAsInteger);
  const [selectedSeries, setSelectedSeries] = useQueryState('series', parseAsArrayOf(parseAsString).withDefault([]));
  const catalog = useCatalog(snapshot ?? undefined);
  const geographyQuery = useGeographies(snapshot ?? undefined);
  const clipboard = useClipboard({ timeout: 1800 });

  const countries = useMemo(() => [...new Map((geographyQuery.data?.geographies ?? [])
    .filter((item) => item.mapped && item.leaf && item.m49)
    .map((item) => [item.m49!, item])).values()].sort((a, b) => a.name.localeCompare(b.name)), [geographyQuery.data]);
  const datasets = useMemo(() => (catalog.data?.datasets ?? []).filter((dataset) =>
    dataset.capabilities.includes('line') && catalog.data?.series.some((series) => series.dataset_id === dataset.id && series.value_kind === 'number')),
  [catalog.data]);
  const years = useMemo(() => [...new Set((catalog.data?.series ?? []).filter((item) => datasets.some((dataset) => dataset.id === item.dataset_id))
    .flatMap((item) => item.available_years))].sort((a, b) => b - a), [catalog.data, datasets]);

  useEffect(() => {
    if (!snapshot && catalog.data?.meta.snapshot.id) void setSnapshot(catalog.data.meta.snapshot.id, { history: 'replace' });
  }, [snapshot, catalog.data?.meta.snapshot.id, setSnapshot]);
  useEffect(() => {
    if (!country && countries.length) void setCountry(countries.find((item) => item.m49 === '620')?.m49 ?? countries[0].m49!, { history: 'replace' });
  }, [country, countries, setCountry]);
  useEffect(() => {
    if (year === null && years.length) void setYear(years[0], { history: 'replace' });
  }, [year, years, setYear]);

  const primary = countries.find((item) => item.m49 === country);
  const secondary = countries.find((item) => item.m49 === compare);
  const choices = countries.map((item) => ({ value: item.m49!, label: item.name }));
  const selectedDatasets = selectedSeries.map((id) => catalog.data?.series.find((item) => item.id === id)?.dataset_id);
  const invalidSeries = selectedSeries.some((id) => !catalog.data?.series.some((item) => item.id === id && item.value_kind === 'number' && datasets.some((dataset) => dataset.id === item.dataset_id)))
    || new Set(selectedDatasets).size !== selectedDatasets.length;

  return <Container size="xl" className="portrait-page">
    <Group justify="space-between" align="start" mb="lg">
      <Box><Text className="editorial-kicker">Country portraits</Text><Title className="portrait-title">A place, in perspective.</Title></Box>
      <Button variant="light" color="teal" disabled={!snapshot || !primary || year === null} leftSection={clipboard.copied ? <Check size={16} aria-hidden /> : <LinkIcon size={16} aria-hidden />} onClick={() => clipboard.copy(window.location.href)}>{clipboard.copied ? 'Link copied' : 'Copy portrait link'}</Button>
    </Group>
    {clipboard.error && <Alert color="yellow" mb="md">The link could not be copied. You can copy the address from your browser.</Alert>}
    <div className="portrait-controls">
      <Select label="Country or area" placeholder="Choose a country" searchable data={choices} value={country} onChange={(value) => void setCountry(value)} allowDeselect={false} />
      <Select label="Compare with" placeholder="Add a comparison" searchable clearable data={choices.filter((item) => item.value !== country)} value={compare} onChange={(value) => void setCompare(value)} />
      <Select label="Exact year" data={[...new Set([...years, ...(year !== null ? [year] : [])])].sort((a, b) => b - a).map((item) => ({ value: String(item), label: String(item) }))} value={year === null ? null : String(year)} onChange={(value) => void setYear(value === null ? null : Number(value))} allowDeselect={false} searchable />
    </div>
    {catalog.isPending || geographyQuery.isPending ? <QueryLoading />
      : catalog.isError ? <QueryError error={catalog.error} />
        : geographyQuery.isError ? <QueryError error={geographyQuery.error} />
          : country && !primary ? <EmptyState title="This country is unavailable in this snapshot.">Choose a country from the list. The shared country selection has been kept.</EmptyState>
            : compare && (!secondary || compare === country) ? <EmptyState title="Choose a different comparison country.">The comparison must be another country available in this snapshot.</EmptyState>
              : invalidSeries ? <EmptyState title="This portrait contains unavailable or conflicting series."><Button variant="light" mt="sm" onClick={() => void setSelectedSeries(null)}>Use the default series</Button></EmptyState>
                : !primary || !snapshot || year === null ? <EmptyState title="No country observations are available yet." />
                  : year < 1 || year > 9999 ? <EmptyState title="Choose an exact year between 1 and 9999." />
                    : <>
                      <div className="portrait-intro">
                        <Stack gap="sm" justify="center">
                          <Group gap="sm"><Text component="span" aria-hidden className="portrait-flag">{flagForISO2(primary.iso2)}</Text><Title order={2} className="portrait-country">{primary.name}</Title></Group>
                          {secondary && <Text size="lg">Compared with <strong>{secondary.name}</strong></Text>}
                          <Text c="dimmed">Headlines show <strong>{year}</strong>. Trends show the available published years. Missing observations stay missing.</Text>
                          <Group gap="xs"><Badge variant="light" color="teal">{datasets.length} indicators</Badge><Badge variant="outline" color="gray">Exact published values</Badge></Group>
                        </Stack>
                        <CountryLocator snapshot={snapshot} countries={[primary, ...(secondary ? [secondary] : [])]} />
                      </div>
                      <div className="portrait-grid">
                        {datasets.map((dataset) => <CountryMetric key={`${snapshot}-${dataset.id}`} dataset={dataset} catalog={catalog.data!} countries={[primary, ...(secondary ? [secondary] : [])]} year={year}
                          selected={selectedSeries.find((id) => catalog.data?.series.some((item) => item.id === id && item.dataset_id === dataset.id))}
                          onSelect={(id) => void setSelectedSeries([...selectedSeries.filter((old) => !catalog.data?.series.some((item) => item.id === old && item.dataset_id === dataset.id)), id])} />)}
                      </div>
                      {catalog.data!.datasets.filter((dataset) => !datasets.includes(dataset)).length > 0 && <Text size="sm" c="dimmed" mt="lg">Composition and other datasets remain available in the <Anchor component={Link} to={`/data?snapshot=${encodeURIComponent(snapshot)}`}>full data catalog</Anchor>.</Text>}
                      <Provenance snapshot={catalog.data!.meta.snapshot} />
                    </>}
  </Container>;
}

function CountryMetric({ dataset, catalog, countries, year, selected, onSelect }: { dataset: Dataset; catalog: CatalogResponse; countries: Geography[]; year: number; selected?: string; onSelect: (id: string) => void }) {
  const seriesOptions = catalog.series.filter((item) => item.dataset_id === dataset.id && item.value_kind === 'number');
  const series = seriesOptions.find((item) => item.id === selected) ?? portraitSeries(seriesOptions);
  const query = useChartObservations({ snapshot: catalog.meta.snapshot.id, series: series ? [series.id] : undefined, geographies: countries.map((item) => item.m49!) }, Boolean(series));
  const rows = (query.data?.observations ?? []).filter((row) => row.series_id === series?.id && row.source_geography.mapped && row.source_geography.leaf && countries.some((country) => country.m49 === row.source_geography.m49));
  const explore = new URLSearchParams({ snapshot: catalog.meta.snapshot.id, view: 'line', series: series?.id ?? '', geographies: countries.map((item) => item.m49).join(',') });

  return <section className="portrait-metric" aria-label={dataset.name}>
    <Text size="xs" c="dimmed" mb="xs">{dataset.source_system ?? 'WHO'}</Text>
    {dataset.description && <Text size="sm" c="dimmed" mb="md">{dataset.description}</Text>}
    <Group justify="space-between" mb="md"><Text className="editorial-kicker">{dataset.name}</Text><Anchor component={Link} to={`/explore?${explore}`} size="sm">Explore <ArrowRight size={13} aria-hidden /></Anchor></Group>
    {seriesOptions.length > 1 && <Select label={`${dataset.name}: published series`} value={series?.id} data={seriesOptions.map((item) => ({ value: item.id, label: seriesLabel(item) }))} onChange={(value) => { if (value) onSelect(value); }} searchable allowDeselect={false} mb="md" />}
    {seriesOptions.length === 1 && series && <Text size="xs" c="dimmed" mb="md">{seriesLabel(series)}</Text>}
    {query.isPending ? <QueryLoading /> : query.isError ? <QueryError error={query.error} /> : series && <>
      <SimpleGrid cols={countries.length} spacing="md" mb="lg">
        {countries.map((country) => {
          const observation = exactCountryValue(rows, series.id, country.m49!, year);
          return <Box key={country.m49} className="portrait-value-block">
            <Text size="sm" fw={600}>{country.name} · {year}</Text>
            {observation && isPublishedNumeric(observation) ? <>
              <Text className="portrait-value"><PublishedValue displayValue={observation.display_value} numericValue={observation.numeric_value} /></Text>
              <Text size="xs" c="dimmed">{series.unit}</Text>
              {(observation.lower_bound != null || observation.upper_bound != null) && <Text size="xs" c="dimmed">Published bounds: {observation.lower_bound ?? '—'}–{observation.upper_bound ?? '—'}</Text>}
            </> : <Text className="portrait-missing">{observation?.status === 'suppressed' ? 'Suppressed by source' : observation?.status === 'not_applicable' ? 'Not applicable' : 'No published value'}<Text component="span" display="block" size="xs" c="dimmed">for this exact year and series</Text></Text>}
            {observation?.source_note && <Text size="xs" c="dimmed" mt="sm">{observation.source_note}</Text>}
            {observation?.source_status && <Text size="xs" c="dimmed">Source status: {observation.source_status}</Text>}
          </Box>;
        })}
      </SimpleGrid>
      {rows.some(isPublishedNumeric) ? <LineChartView compact observations={rows} availableYears={series.available_years} title={series.name} unit={series.unit} /> : <Text size="sm" c="dimmed">No published trend is available for this selection.</Text>}
      <Anchor href={dataset.source_url} target="_blank" rel="noreferrer" size="xs" mt="md" display="block">{dataset.citation}</Anchor>
      <Provenance snapshot={query.data?.meta.snapshot} releases={query.data?.meta.releases.filter((release) => release.dataset_id === dataset.id)} />
    </>}
  </section>;
}

function CountryLocator({ snapshot, countries }: { snapshot: string; countries: Geography[] }) {
  const map = useMapGeometry(snapshot, true);
  const mapName = useMemo(() => {
    if (!map.data) return undefined;
    const name = `portrait-${snapshot}`;
    echarts.registerMap(name, map.data as Parameters<typeof echarts.registerMap>[1]);
    return name;
  }, [map.data, snapshot]);
  if (map.isError) return <Text size="sm" c="dimmed">The location map is unavailable. Country data remains available below.</Text>;
  if (!mapName || !map.data) return <QueryLoading />;
  const highlights = map.data.features.flatMap((feature) => {
    const m49 = String(feature.properties?.m49 ?? '').padStart(3, '0');
    const index = countries.findIndex((country) => country.m49 === m49);
    return index < 0 ? [] : [{ name: String(feature.properties?.name ?? countries[index].name), itemStyle: { areaColor: index === 0 ? '#0b6e76' : '#bf673e' } }];
  });
  return <Box className="portrait-locator" role="img" aria-label={`Location of ${countries.map((item) => item.name).join(' and ')}`}>
    <EChartsCore echarts={echarts} opts={{ renderer: 'svg' }} autoResize style={{ height: 210, width: '100%' }} option={{
      animation: false,
      series: [{ type: 'map', map: mapName, roam: false, silent: true, itemStyle: { areaColor: '#e4e7dd', borderColor: '#fffdf8', borderWidth: .4 }, data: highlights }],
    }} />
    <Group justify="center" gap="md">{countries.map((country, index) => <Text key={country.m49} size="xs" fw={600} c={index === 0 ? '#0b6e76' : '#bf673e'}>{country.name}</Text>)}</Group>
    <Text ta="center" size="xs" c="dimmed">Natural Earth boundaries</Text>
  </Box>;
}
