import {
  Alert, Anchor, Box, Button, Container, Divider, Group, MultiSelect, Select, SimpleGrid, Stack, Tabs, Text, Title,
} from '@mantine/core';
import { Download, Info } from 'lucide-react';
import { parseAsArrayOf, parseAsInteger, parseAsString, parseAsStringEnum, useQueryState } from 'nuqs';
import { useEffect, useMemo, type ReactNode } from 'react';
import { observationsCsvUrl, useAssociation, useCatalog, useChartObservations, useGeographies, useGroups, useMapGeometry, useObservations } from '../api/queries';
import type { ObservationFilters } from '../api/queries';
import type { ChartCapability, Group as M49Group, Measure, Series } from '../api/types';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { AssociationChartView, awareClass, CompositionChartView, isMapEligible, isPublishedNumeric, LineChartView, MapChartView } from '../components/ChartViews';
import { ObservationTable } from '../components/ObservationTable';
import { Provenance } from '../components/Provenance';

const explorerViews = ['line', 'map', 'association', 'composition', 'table'] as const;
type ExplorerView = typeof explorerViews[number];
const viewLabels: Record<ExplorerView, string> = { line: 'Line', map: 'Map', association: 'Association', composition: 'Composition', table: 'Table' };

function seriesOptions(items: Series[]) { return items.map((item) => ({ value: item.id, label: `${item.name}${item.unit ? ` · ${item.unit}` : ''}` })); }
function yearsFor(series?: Series) { return (series?.available_years ?? []).slice().sort((a, b) => b - a).map((year) => ({ value: String(year), label: String(year) })); }
function latestYear(series?: Series): number | null { return series?.available_years.length ? Math.max(...series.available_years) : null; }
function commonYears(series: Series[]): number[] {
  if (!series.length) return [];
  const [first, ...rest] = series;
  return first.available_years.filter((candidate) => rest.every((item) => item.available_years.includes(candidate))).sort((a, b) => b - a);
}
function ControlLabel({ children }: { children: ReactNode }) { return <Text size="sm" fw={600} mb={4}>{children}</Text>; }

type SeriesFamily = { label: string; representative: Series; series: Series[]; availableYears: number[] };

function isSexDimension(code: string): boolean { return code.replace(/^DIM_/, '') === 'SEX'; }
function sexValue(series: Series): string | undefined { return Object.entries(series.dimensions).find(([code]) => isSexDimension(code))?.[1]; }
function dimensionName(code: string): string {
  const words = code.replace(/^DIM_/, '').toLowerCase().replaceAll('_', ' ');
  return words.charAt(0).toUpperCase() + words.slice(1);
}
function buildSeriesFamilies(items: Series[], measures: Measure[]): SeriesFamily[] {
  const measureNames = new Map(measures.map((measure) => [measure.id, measure.name]));
  const grouped = new Map<string, Series[]>();
  items.forEach((series) => {
    const dimensions = Object.entries(series.dimensions).filter(([code]) => !isSexDimension(code)).sort(([a], [b]) => a.localeCompare(b));
    const key = JSON.stringify([series.dataset_id, series.measure_id, series.unit, series.statistic, series.value_kind, dimensions]);
    grouped.set(key, [...(grouped.get(key) ?? []), series]);
  });
  return [...grouped.values()].map((siblings) => {
    siblings.sort((a, b) => {
      const order = (value?: string) => ({ TOTAL: 0, FEMALE: 1, MALE: 2 }[value ?? ''] ?? 3);
      return order(sexValue(a)) - order(sexValue(b)) || a.name.localeCompare(b.name);
    });
    const representative = siblings[0];
    const dimensions = Object.entries(representative.dimensions).filter(([code]) => !isSexDimension(code)).sort(([a], [b]) => a.localeCompare(b));
    const suffix = dimensions.map(([code, value]) => `${dimensionName(code)}: ${value}`).join(', ');
    return {
      label: `${measureNames.get(representative.measure_id) ?? representative.name.split(' · ')[0]}${suffix ? ` · ${suffix}` : ''}`,
      representative,
      series: siblings,
      availableYears: [...new Set(siblings.flatMap((series) => series.available_years))].sort((a, b) => a - b),
    };
  }).sort((a, b) => a.label.localeCompare(b.label));
}
function familyOptions(families: SeriesFamily[]) { return families.map((family) => ({ value: family.representative.id, label: family.label })); }
function formatDimensionValue(value: string): string {
  const lower = value.toLowerCase();
  return lower.charAt(0).toUpperCase() + lower.slice(1);
}
function sliceOptions(family?: SeriesFamily) {
  return (family?.series ?? []).map((series) => ({ value: series.id, label: sexValue(series) ? formatDimensionValue(sexValue(series)!) : series.name }));
}

export function ExplorePage() {
  const [view, setView] = useQueryState('view', parseAsStringEnum<ExplorerView>([...explorerViews]).withDefault('line'));
  const [snapshot, setSnapshot] = useQueryState('snapshot', parseAsString);
  const [seriesID, setSeriesID] = useQueryState('series', parseAsString);
  const [year, setYear] = useQueryState('year', parseAsInteger);
  const [xSeriesID, setXSeriesID] = useQueryState('x_series', parseAsString);
  const [xYear, setXYear] = useQueryState('x_year', parseAsInteger);
  const [ySeriesID, setYSeriesID] = useQueryState('y_series', parseAsString);
  const [yYear, setYYear] = useQueryState('y_year', parseAsInteger);
  const [geographyIDs, setGeographyIDs] = useQueryState('geographies', parseAsArrayOf(parseAsString).withDefault([]));
  const [groupIDs, setGroupIDs] = useQueryState('groups', parseAsArrayOf(parseAsString).withDefault([]));
  const [page, setPage] = useQueryState('page', parseAsInteger.withDefault(1));
  const [pageSize, setPageSize] = useQueryState('page_size', parseAsInteger.withDefault(25));

  const catalog = useCatalog(snapshot ?? undefined);
  const geographies = useGeographies(snapshot ?? undefined);
  const groups = useGroups(snapshot ?? undefined);
  const allSeries = catalog.data?.series ?? [];
  const datasetByID = useMemo(() => new Map((catalog.data?.datasets ?? []).map((dataset) => [dataset.id, dataset])), [catalog.data]);
  const seriesByID = useMemo(() => new Map(allSeries.map((series) => [series.id, series])), [allSeries]);
  const selectedSeries = seriesID ? seriesByID.get(seriesID) : undefined;
  const selectedX = xSeriesID ? seriesByID.get(xSeriesID) : undefined;
  const selectedY = ySeriesID ? seriesByID.get(ySeriesID) : undefined;
  const selectedDataset = selectedSeries ? datasetByID.get(selectedSeries.dataset_id) : undefined;
  const includesSuicideSeries = [selectedSeries, selectedX, selectedY].some((series) => datasetByID.get(series?.dataset_id ?? '')?.who_code === 'SDGSUICIDE');
  const currentDatasetSupports = (series: Series, capability: ChartCapability) => datasetByID.get(series.dataset_id)?.capabilities.includes(capability) ?? false;
  const lineFamilies = useMemo(() => buildSeriesFamilies(allSeries.filter((series) => currentDatasetSupports(series, 'line')), catalog.data?.measures ?? []), [allSeries, catalog.data?.measures, datasetByID]);
  const exactFamilies = useMemo(() => buildSeriesFamilies(allSeries.filter((series) => view === 'table' || currentDatasetSupports(series, view)), catalog.data?.measures ?? []), [allSeries, catalog.data?.measures, datasetByID, view]);
  const associationFamilies = useMemo(() => buildSeriesFamilies(allSeries.filter((series) => currentDatasetSupports(series, 'association')), catalog.data?.measures ?? []), [allSeries, catalog.data?.measures, datasetByID]);
  const lineFamilyBySeriesID = useMemo(() => new Map(lineFamilies.flatMap((family) => family.series.map((series) => [series.id, family] as const))), [lineFamilies]);
  const exactFamilyBySeriesID = useMemo(() => new Map(exactFamilies.flatMap((family) => family.series.map((series) => [series.id, family] as const))), [exactFamilies]);
  const associationFamilyBySeriesID = useMemo(() => new Map(associationFamilies.flatMap((family) => family.series.map((series) => [series.id, family] as const))), [associationFamilies]);
  const selectedLineFamily = seriesID ? lineFamilyBySeriesID.get(seriesID) : undefined;
  const selectedExactFamily = seriesID ? exactFamilyBySeriesID.get(seriesID) : undefined;
  const selectedXFamily = xSeriesID ? associationFamilyBySeriesID.get(xSeriesID) : undefined;
  const selectedYFamily = ySeriesID ? associationFamilyBySeriesID.get(ySeriesID) : undefined;

  const selectedM49 = useMemo(() => {
    if (!groupIDs.length) return undefined;
    return new Set((groups.data?.groups ?? []).filter((group) => groupIDs.includes(group.id)).flatMap((group) => group.members).map((member) => member.m49));
  }, [groupIDs, groups.data]);
  const geographyOptions = useMemo(() => {
    const options = new Map<string, string>();
    (geographies.data?.geographies ?? [])
      .filter((geography) => !selectedM49 || selectedM49.has(geography.m49 ?? ''))
      .sort((a, b) => a.name.localeCompare(b.name) || a.source_code.localeCompare(b.source_code))
      .forEach((geography) => {
        const value = geography.mapped && geography.leaf && geography.m49 ? geography.m49 : geography.source_code;
        if (!options.has(value)) options.set(value, geography.name);
      });
    return [...options].map(([value, label]) => ({ value, label }));
  }, [geographies.data, selectedM49]);
  const iso2ByM49 = useMemo(() => new Map((geographies.data?.geographies ?? []).flatMap((geography) => {
    return geography.m49 && geography.iso2 ? [[geography.m49, geography.iso2] as const] : [];
  })), [geographies.data]);
  const baseFilters = useMemo(() => ({
    snapshot: snapshot ?? undefined,
    geographies: view === 'map' || !geographyIDs.length ? undefined : geographyIDs,
    groups: view === 'map' || !groupIDs.length ? undefined : groupIDs,
  }), [snapshot, geographyIDs, groupIDs, view]);
  const tooManyLineGeographies = view === 'line' && geographyIDs.length > 12;
  const activeSeries = allSeries.filter((series) => view === 'table' || currentDatasetSupports(series, view));
  const compositionSeries = selectedDataset?.capabilities.includes('composition') ? allSeries.filter((series) => series.dataset_id === selectedDataset.id && awareClass(series) !== 'Other') : [];
  const compositionYears = useMemo(() => commonYears(compositionSeries), [compositionSeries]);
  const lineObservations = useChartObservations({ ...baseFilters, series: selectedLineFamily?.series.map((series) => series.id) }, view === 'line' && Boolean(selectedLineFamily && geographyIDs.length) && !tooManyLineGeographies);
  const mapObservations = useChartObservations({ ...baseFilters, series: selectedSeries ? [selectedSeries.id] : undefined, years: year ? [year] : undefined }, view === 'map' && Boolean(selectedSeries && year));
  const tablePageSize = ([25, 50, 100].includes(pageSize) ? pageSize : 25) as 25 | 50 | 100;
  const tableObservations = useObservations({ ...baseFilters, series: selectedSeries ? [selectedSeries.id] : undefined, years: year ? [year] : undefined, page, page_size: tablePageSize }, view === 'table' && Boolean(selectedSeries));
  const compositionObservations = useChartObservations({ ...baseFilters, series: compositionSeries.map((series) => series.id), years: year ? [year] : undefined }, view === 'composition' && compositionSeries.length > 0 && Boolean(year));
  const mapGeometry = useMapGeometry(snapshot ?? undefined, view === 'map' && Boolean(selectedSeries && year));
  const association = useAssociation(selectedX && selectedY && xYear && yYear ? { ...baseFilters, x_series: selectedX.id, x_year: xYear, y_series: selectedY.id, y_year: yYear } : undefined);

  useEffect(() => {
    if (!snapshot && catalog.data?.meta.snapshot.id) {
      void setSnapshot(catalog.data.meta.snapshot.id, { history: 'replace' });
    }
  }, [catalog.data?.meta.snapshot.id, setSnapshot, snapshot]);
  useEffect(() => {
    if (view !== 'map' || year || !selectedSeries) return;
    const next = latestYear(selectedSeries);
    if (next) void setYear(next, { history: 'replace' });
  }, [selectedSeries, setYear, view, year]);
  useEffect(() => {
    if (xYear || !selectedX) return;
    const next = latestYear(selectedX);
    if (next) void setXYear(next, { history: 'replace' });
  }, [selectedX, setXYear, xYear]);
  useEffect(() => {
    if (yYear || !selectedY) return;
    const next = latestYear(selectedY);
    if (next) void setYYear(next, { history: 'replace' });
  }, [selectedY, setYYear, yYear]);

  const selectSeries = (next: string | null) => {
    setSeriesID(next);
    setYear(view === 'map' && next ? latestYear(seriesByID.get(next)) : null);
    setPage(1);
  };
  const selectFamily = (next: string | null) => {
    const family = next ? exactFamilyBySeriesID.get(next) : undefined;
    selectSeries(family?.representative.id ?? null);
  };
  const selectAssociationFamily = (axis: 'x' | 'y', next: string | null) => {
    const family = next ? associationFamilyBySeriesID.get(next) : undefined;
    const representative = family?.representative;
    if (axis === 'x') {
      setXSeriesID(representative?.id ?? null);
      setXYear(latestYear(representative));
    } else {
      setYSeriesID(representative?.id ?? null);
      setYYear(latestYear(representative));
    }
  };
  const invalidYear = (series: Series | undefined, selectedYear: number | null) => Boolean(series && selectedYear && !series.available_years.includes(selectedYear));

  return <Container size="xl" className="page-section"><Stack gap="xl">
    <div><Text className="editorial-kicker">Explorer</Text><Title className="display-title">Choose the question, then the exact data.</Title><Text className="lede">Filters are shareable in the URL. A snapshot pin makes a view reproducible; without one, the atlas clearly uses the latest catalog.</Text></div>
    {catalog.isPending ? <QueryLoading label="Loading the catalog…" /> : catalog.isError ? <QueryError error={catalog.error} title="The catalog could not be loaded." /> : <>
      <Tabs value={view} onChange={(next) => next && setView(next as ExplorerView)} variant="pills" aria-label="Explorer views"><Tabs.List>{explorerViews.map((item) => <Tabs.Tab value={item} key={item}>{viewLabels[item]}</Tabs.Tab>)}</Tabs.List></Tabs>
      <div className="explore-grid"><Stack className="control-panel" gap="md">
        {view !== 'association' && view !== 'composition' && <div><ControlLabel>Indicator</ControlLabel><Select searchable clearable value={(view === 'line' ? selectedLineFamily : selectedExactFamily)?.representative.id ?? null} onChange={selectFamily} placeholder="Choose an indicator" data={familyOptions(view === 'line' ? lineFamilies : exactFamilies)} /></div>}
        {view === 'composition' && <div><ControlLabel>AWaRe series</ControlLabel><Select searchable clearable value={seriesID} onChange={selectSeries} placeholder="Choose a published series" data={seriesOptions(activeSeries)} /></div>}
        {view !== 'line' && view !== 'association' && view !== 'composition' && selectedExactFamily && selectedExactFamily.series.length > 1 && <div><ControlLabel>Sex</ControlLabel><Select value={selectedSeries?.id ?? null} onChange={(value) => value && setSeriesID(value)} data={sliceOptions(selectedExactFamily)} /></div>}
        {view === 'association' && <Stack gap="sm"><div><ControlLabel>X indicator</ControlLabel><Select searchable clearable value={selectedXFamily?.representative.id ?? null} onChange={(value) => selectAssociationFamily('x', value)} data={familyOptions(associationFamilies)} placeholder="Choose X indicator" /></div>{selectedXFamily && selectedXFamily.series.length > 1 && <div><ControlLabel>X sex</ControlLabel><Select value={selectedX?.id ?? null} onChange={(value) => value && setXSeriesID(value)} data={sliceOptions(selectedXFamily)} /></div>}<div><ControlLabel>X exact year</ControlLabel><Select clearable disabled={!selectedX} value={xYear ? String(xYear) : null} onChange={(value) => setXYear(value ? Number(value) : null)} data={yearsFor(selectedX)} placeholder="Choose X year" />{invalidYear(selectedX, xYear) && <Text c="red" size="xs" mt={4}>This year is unavailable and has not been substituted.</Text>}</div><Divider /><div><ControlLabel>Y indicator</ControlLabel><Select searchable clearable value={selectedYFamily?.representative.id ?? null} onChange={(value) => selectAssociationFamily('y', value)} data={familyOptions(associationFamilies)} placeholder="Choose Y indicator" /></div>{selectedYFamily && selectedYFamily.series.length > 1 && <div><ControlLabel>Y sex</ControlLabel><Select value={selectedY?.id ?? null} onChange={(value) => value && setYSeriesID(value)} data={sliceOptions(selectedYFamily)} /></div>}<div><ControlLabel>Y exact year</ControlLabel><Select clearable disabled={!selectedY} value={yYear ? String(yYear) : null} onChange={(value) => setYYear(value ? Number(value) : null)} data={yearsFor(selectedY)} placeholder="Choose Y year" />{invalidYear(selectedY, yYear) && <Text c="red" size="xs" mt={4}>This year is unavailable and has not been substituted.</Text>}</div></Stack>}
        {view !== 'line' && view !== 'association' && <div><ControlLabel>Exact year</ControlLabel><Select clearable value={year ? String(year) : null} onChange={(value) => setYear(value ? Number(value) : null)} placeholder="Choose an exact year" disabled={!selectedSeries} data={view === 'composition' ? compositionYears.map((value) => ({ value: String(value), label: String(value) })) : yearsFor(selectedSeries)} />{(view === 'composition' ? Boolean(year && !compositionYears.includes(year)) : invalidYear(selectedSeries, year)) && <Text c="red" size="xs" mt={4}>This exact year is unavailable. Choose another; it will not be replaced.</Text>}</div>}
        {view !== 'map' && <><Divider />
          <div><ControlLabel>Group filters</ControlLabel><MultiSelect searchable clearable value={groupIDs} onChange={setGroupIDs} placeholder="All countries and areas" data={(groups.data?.groups ?? []).map((group) => ({ value: group.id, label: group.name }))} /></div>
          <div><ControlLabel>Countries and areas{view === 'line' ? ' (up to 12)' : ''}</ControlLabel><MultiSelect searchable clearable maxValues={view === 'line' ? 12 : undefined} value={geographyIDs} onChange={(value) => { setGeographyIDs(value); setPage(1); }} placeholder="All matching source geographies" data={geographyOptions} /></div>
        </>}
        {view !== 'association' && (selectedSeries || selectedLineFamily) && <Button component="a" variant="subtle" color="dark" leftSection={<Download size={15} aria-hidden />} href={observationsCsvUrl({ ...baseFilters, series: view === 'line' ? selectedLineFamily?.series.map((series) => series.id) : selectedSeries ? [selectedSeries.id] : undefined, years: view === 'line' ? undefined : year ? [year] : undefined })}>Download filtered CSV</Button>}
        {view !== 'map' && <Text className="data-note">Groups narrow country selections. Regional aggregates require matching denominator data and are not inferred.</Text>}
      </Stack><Box className="chart-surface">
        {view === 'line' && <LinePanel family={selectedLineFamily} query={lineObservations} tooManyGeographies={tooManyLineGeographies} />}
        {view === 'map' && <MapPanel series={selectedSeries} year={year} query={mapObservations} geometry={mapGeometry} iso2ByM49={iso2ByM49} />}
        {view === 'association' && <AssociationPanel xSeries={selectedX} ySeries={selectedY} xYear={xYear} yYear={yYear} query={association} groups={groups.data?.groups} />}
        {view === 'composition' && <CompositionPanel series={selectedSeries} year={year} query={compositionObservations} seriesByID={seriesByID} />}
        {view === 'table' && <TablePanel series={selectedSeries} query={tableObservations} csvFilters={{ ...baseFilters, series: selectedSeries ? [selectedSeries.id] : undefined, years: year ? [year] : undefined }} page={page} pageSize={tablePageSize} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(1); }} />}
      </Box></div>
      {includesSuicideSeries && <Alert color="gray" title="Suicide data context">Use neutral, non-comparative language when discussing suicide data. <Anchor href="https://www.who.int/news-room/questions-and-answers/item/suicide" target="_blank" rel="noreferrer">Read the WHO suicide Q&amp;A</Anchor>.</Alert>}
    </>}
    {view === 'association' && <Alert icon={<Info size={18} />} color="blue" title="Exact-year association">Association joins numeric observations only when both independently chosen exact years map to the same canonical leaf M49 geography. It reports no p-value, regression, ranking, or causal conclusion.</Alert>}
  </Stack></Container>;
}

function LinePanel({ family, query, tooManyGeographies }: { family?: SeriesFamily; query: ReturnType<typeof useChartObservations>; tooManyGeographies: boolean }) {
  if (!family) return <EmptyState title="Choose an indicator.">Sex variants are shown together when the source publishes them.</EmptyState>;
  if (tooManyGeographies) return <EmptyState title="Choose no more than 12 countries or areas.">Line charts never silently trim an over-limit shared selection.</EmptyState>;
  if (!query.isFetching && !query.data && query.fetchStatus === 'idle') return <EmptyState title="Choose 1–12 countries or areas for this line chart.">The atlas will not silently choose countries or render an unbounded set of lines.</EmptyState>;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  if (!query.data?.observations.some(isPublishedNumeric)) return <EmptyState title="No published numeric rows match this selection." />;
  const seriesLabels = new Map(family.series.flatMap((series) => {
    const sex = sexValue(series);
    return sex ? [[series.id, formatDimensionValue(sex)] as const] : [];
  }));
  return <><LineChartView observations={query.data.observations} availableYears={family.availableYears} seriesLabels={seriesLabels} title={family.label} unit={family.representative.unit} /><Provenance snapshot={query.data.meta.snapshot} releases={query.data.meta.releases} /></>;
}

function MapPanel({ series, year, query, geometry, iso2ByM49 }: { series?: Series; year: number | null; query: ReturnType<typeof useChartObservations>; geometry: ReturnType<typeof useMapGeometry>; iso2ByM49: Map<string, string> }) {
  if (!series) return <EmptyState title="Choose one exact series." />;
  if (!year) return <EmptyState title="Choose an exact map year.">The atlas will not pick a nearby year.</EmptyState>;
  if (query.isPending || geometry.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  if (geometry.isError) return <QueryError error={geometry.error} title="Map geometry could not be loaded." />;
  if (!query.data?.observations.some(isMapEligible) || !geometry.data) return <EmptyState title="No published mapped rows match this exact year." />;
  return <><MapChartView observations={query.data.observations} geometry={geometry.data} title={`${series.name} · ${year}`} unit={series.unit} iso2ByM49={iso2ByM49} /><Provenance snapshot={query.data.meta.snapshot} releases={query.data.meta.releases} /></>;
}

function AssociationPanel({ xSeries, ySeries, xYear, yYear, query, groups }: { xSeries?: Series; ySeries?: Series; xYear: number | null; yYear: number | null; query: ReturnType<typeof useAssociation>; groups?: M49Group[] }) {
  if (!xSeries || !ySeries || !xYear || !yYear) return <EmptyState title="Choose X and Y series with independently exact years.">No year will be inferred from the other series.</EmptyState>;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  const result = query.data;
  if (!result) return null;
  const coverage = [['Selected universe', result.coverage.selected_universe], ['Paired n', result.coverage.paired], ['X-only missing', result.coverage.x_only_missing], ['Y-only missing', result.coverage.y_only_missing], ['Both missing', result.coverage.both_missing]];
  return <Stack gap="md"><SimpleGrid cols={{ base: 2, sm: 5 }}>{coverage.map(([label, value]) => <Box className="metadata-row" key={String(label)}><Text size="xs" c="dimmed">{label}</Text><Text fw={700}>{value}</Text></Box>)}</SimpleGrid><Box className="metadata-row"><Text size="xs" c="dimmed">Pearson r</Text><Text fw={700}>{result.pearson_r === null || result.pearson_r === undefined ? 'Not reported' : result.pearson_r.toFixed(3)}</Text></Box>{result.warnings.map((warning) => <Alert key={warning} color="yellow">{warning}</Alert>)}<AssociationChartView result={result} xLabel={`${xSeries.name} · ${xYear}`} yLabel={`${ySeries.name} · ${yYear}`} groups={groups} /><Provenance snapshot={result.meta.snapshot} releases={result.meta.releases} /></Stack>;
}

function CompositionPanel({ series, year, query, seriesByID }: { series?: Series; year: number | null; query: ReturnType<typeof useChartObservations>; seriesByID: Map<string, Series> }) {
  if (!series) return <EmptyState title="Choose an AWaRe series.">The chart reads every published class in that same dataset.</EmptyState>;
  if (!year) return <EmptyState title="Choose an exact composition year." />;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  if (!query.data?.observations.some(isPublishedNumeric)) return <EmptyState title="No published numeric composition rows match this selection." />;
  return <><CompositionChartView observations={query.data.observations} seriesById={seriesByID} title={`AWaRe composition · ${year}`} /><Provenance snapshot={query.data.meta.snapshot} releases={query.data.meta.releases} /></>;
}

function TablePanel({ series, query, csvFilters, page, pageSize, onPageChange, onPageSizeChange }: { series?: Series; query: ReturnType<typeof useObservations>; csvFilters: ObservationFilters; page: number; pageSize: 25 | 50 | 100; onPageChange: (page: number) => void; onPageSizeChange: (pageSize: number) => void }) {
  if (!series) return <EmptyState title="Choose an exact series." />;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  const result = query.data;
  if (!result) return null;
  return <Stack><Group justify="space-between"><div><Title order={2} size="h3">Source observations</Title><Text size="sm" c="dimmed">Server-paginated: 25, 50, or 100 source rows at a time.</Text></div><Button component="a" href={observationsCsvUrl(csvFilters)} variant="light" leftSection={<Download size={16} aria-hidden />}>CSV</Button></Group><ObservationTable observations={result.observations} total={result.pagination.total} page={page} pageSize={pageSize} onPageChange={onPageChange} onPageSizeChange={onPageSizeChange} /><Provenance snapshot={result.meta.snapshot} releases={result.meta.releases} /></Stack>;
}
