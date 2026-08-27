import {
  Alert, Anchor, Box, Button, Container, Divider, Group, MultiSelect, Select, SimpleGrid, Stack, Tabs, Text, Title,
} from '@mantine/core';
import { Download, Info } from 'lucide-react';
import { parseAsArrayOf, parseAsInteger, parseAsString, parseAsStringEnum, useQueryState } from 'nuqs';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { fetchFreshLatestCatalog, observationsCsvUrl, useAssociation, useCatalog, useChartObservations, useGeographies, useGroups, useMapGeometry, useObservations } from '../api/queries';
import type { ObservationFilters } from '../api/queries';
import type { ChartCapability, Series } from '../api/types';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { AssociationChartView, awareClass, CompositionChartView, isMapEligible, isPublishedNumeric, LineChartView, MapChartView } from '../components/ChartViews';
import { ObservationTable } from '../components/ObservationTable';
import { Provenance } from '../components/Provenance';

const explorerViews = ['line', 'map', 'association', 'composition', 'table'] as const;
type ExplorerView = typeof explorerViews[number];
const viewLabels: Record<ExplorerView, string> = { line: 'Line', map: 'Map', association: 'Association', composition: 'Composition', table: 'Table' };

function seriesOptions(items: Series[]) { return items.map((item) => ({ value: item.id, label: `${item.name}${item.unit ? ` · ${item.unit}` : ''}` })); }
function yearsFor(series?: Series) { return (series?.available_years ?? []).slice().sort((a, b) => b - a).map((year) => ({ value: String(year), label: String(year) })); }
function commonYears(series: Series[]): number[] {
  if (!series.length) return [];
  const [first, ...rest] = series;
  return first.available_years.filter((candidate) => rest.every((item) => item.available_years.includes(candidate))).sort((a, b) => b - a);
}
function ControlLabel({ children }: { children: ReactNode }) { return <Text size="sm" fw={600} mb={4}>{children}</Text>; }

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
  const [resolvingLatest, setResolvingLatest] = useState(false);
  const [latestError, setLatestError] = useState(false);

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
  const baseFilters = useMemo(() => ({ snapshot: snapshot ?? undefined, geographies: geographyIDs.length ? geographyIDs : undefined, groups: groupIDs.length ? groupIDs : undefined }), [snapshot, geographyIDs, groupIDs]);
  const tooManyLineGeographies = view === 'line' && geographyIDs.length > 12;
  const currentDatasetSupports = (series: Series, capability: ChartCapability) => datasetByID.get(series.dataset_id)?.capabilities.includes(capability) ?? false;
  const activeSeries = allSeries.filter((series) => view === 'table' || currentDatasetSupports(series, view));
  const compositionSeries = selectedDataset?.capabilities.includes('composition') ? allSeries.filter((series) => series.dataset_id === selectedDataset.id && awareClass(series) !== 'Other') : [];
  const compositionYears = useMemo(() => commonYears(compositionSeries), [compositionSeries]);
  const lineObservations = useChartObservations({ ...baseFilters, series: selectedSeries ? [selectedSeries.id] : undefined }, view === 'line' && Boolean(selectedSeries && geographyIDs.length) && !tooManyLineGeographies);
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

  const resolveLatest = async () => {
    setLatestError(false);
    setResolvingLatest(true);
    try {
      const latest = await fetchFreshLatestCatalog();
      await setSnapshot(latest.meta.snapshot.id, { history: 'replace' });
    } catch {
      // Keep the current pin intact if the fresh latest resolution fails.
      setLatestError(true);
    } finally {
      setResolvingLatest(false);
    }
  };
  const selectSnapshot = (value: string | null) => {
    if (value === null) {
      void resolveLatest();
      return;
    }
    void setSnapshot(value);
  };
  const selectSeries = (next: string | null) => { setSeriesID(next); setYear(null); setPage(1); };
  const invalidYear = (series: Series | undefined, selectedYear: number | null) => Boolean(series && selectedYear && !series.available_years.includes(selectedYear));

  return <Container size="xl" className="page-section"><Stack gap="xl">
    <div><Text className="editorial-kicker">Explorer</Text><Title className="display-title">Choose the question, then the exact data.</Title><Text className="lede">Filters are shareable in the URL. A snapshot pin makes a view reproducible; without one, the atlas clearly uses the latest catalog.</Text></div>
    {catalog.isPending ? <QueryLoading label="Loading the catalog…" /> : catalog.isError ? <QueryError error={catalog.error} title="The catalog could not be loaded." /> : <>
      <Tabs value={view} onChange={(next) => next && setView(next as ExplorerView)} variant="pills" aria-label="Explorer views"><Tabs.List>{explorerViews.map((item) => <Tabs.Tab value={item} key={item}>{viewLabels[item]}</Tabs.Tab>)}</Tabs.List></Tabs>
      <div className="explore-grid"><Stack className="control-panel" gap="md">
        <div><ControlLabel>Catalog snapshot</ControlLabel><Group gap="xs" wrap="nowrap"><Select aria-label="Catalog snapshot" flex={1} value={snapshot} onChange={selectSnapshot} clearable placeholder="Latest published snapshot" data={catalog.data ? [{ value: catalog.data.meta.snapshot.id, label: `Current · ${catalog.data.meta.snapshot.id}` }] : []} /><Button variant="default" size="compact-sm" loading={resolvingLatest} onClick={() => { void resolveLatest(); }}>View latest</Button></Group>{latestError && <Text c="red" size="xs" mt={4}>The latest snapshot could not be resolved; the current pin remains.</Text>}</div>
        {view !== 'association' && <div><ControlLabel>{view === 'composition' ? 'AWaRe series' : 'Exact series'}</ControlLabel><Select searchable clearable value={seriesID} onChange={selectSeries} placeholder="Choose a published series" data={seriesOptions(activeSeries)} /></div>}
        {view === 'association' && <Stack gap="sm"><div><ControlLabel>X series</ControlLabel><Select searchable clearable value={xSeriesID} onChange={(value) => { setXSeriesID(value); setXYear(null); }} data={seriesOptions(allSeries.filter((series) => currentDatasetSupports(series, 'association')))} placeholder="Choose X series" /></div><div><ControlLabel>X exact year</ControlLabel><Select clearable disabled={!selectedX} value={xYear ? String(xYear) : null} onChange={(value) => setXYear(value ? Number(value) : null)} data={yearsFor(selectedX)} placeholder="Choose X year" />{invalidYear(selectedX, xYear) && <Text c="red" size="xs" mt={4}>This year is unavailable and has not been substituted.</Text>}</div><Divider /><div><ControlLabel>Y series</ControlLabel><Select searchable clearable value={ySeriesID} onChange={(value) => { setYSeriesID(value); setYYear(null); }} data={seriesOptions(allSeries.filter((series) => currentDatasetSupports(series, 'association')))} placeholder="Choose Y series" /></div><div><ControlLabel>Y exact year</ControlLabel><Select clearable disabled={!selectedY} value={yYear ? String(yYear) : null} onChange={(value) => setYYear(value ? Number(value) : null)} data={yearsFor(selectedY)} placeholder="Choose Y year" />{invalidYear(selectedY, yYear) && <Text c="red" size="xs" mt={4}>This year is unavailable and has not been substituted.</Text>}</div></Stack>}
        {view !== 'line' && view !== 'association' && <div><ControlLabel>Exact year</ControlLabel><Select clearable value={year ? String(year) : null} onChange={(value) => setYear(value ? Number(value) : null)} placeholder="Choose an exact year" disabled={!selectedSeries} data={view === 'composition' ? compositionYears.map((value) => ({ value: String(value), label: String(value) })) : yearsFor(selectedSeries)} />{(view === 'composition' ? Boolean(year && !compositionYears.includes(year)) : invalidYear(selectedSeries, year)) && <Text c="red" size="xs" mt={4}>This exact year is unavailable. Choose another; it will not be replaced.</Text>}</div>}
        <Divider />
        <div><ControlLabel>Group filters</ControlLabel><MultiSelect searchable clearable value={groupIDs} onChange={setGroupIDs} placeholder="All countries and areas" data={(groups.data?.groups ?? []).map((group) => ({ value: group.id, label: group.name }))} /></div>
        <div><ControlLabel>Countries and areas (up to 12 lines)</ControlLabel><MultiSelect searchable clearable maxValues={view === 'line' ? 12 : undefined} value={geographyIDs} onChange={(value) => { setGeographyIDs(value); setPage(1); }} placeholder="All matching source geographies" data={geographyOptions} /></div>
        {view !== 'association' && selectedSeries && <Button component="a" variant="subtle" color="dark" leftSection={<Download size={15} aria-hidden />} href={observationsCsvUrl({ ...baseFilters, series: [selectedSeries.id], years: view === 'line' ? undefined : year ? [year] : undefined })}>Download filtered CSV</Button>}
        <Text className="data-note">Groups filter and de-duplicate country selections. They never create a calculated average or group line.</Text>
      </Stack><Box className="chart-surface">
        {view === 'line' && <LinePanel series={selectedSeries} query={lineObservations} tooManyGeographies={tooManyLineGeographies} />}
        {view === 'map' && <MapPanel series={selectedSeries} year={year} query={mapObservations} geometry={mapGeometry} selectedM49={selectedM49} />}
        {view === 'association' && <AssociationPanel xSeries={selectedX} ySeries={selectedY} xYear={xYear} yYear={yYear} query={association} />}
        {view === 'composition' && <CompositionPanel series={selectedSeries} year={year} query={compositionObservations} seriesByID={seriesByID} />}
        {view === 'table' && <TablePanel series={selectedSeries} query={tableObservations} csvFilters={{ ...baseFilters, series: selectedSeries ? [selectedSeries.id] : undefined, years: year ? [year] : undefined }} page={page} pageSize={tablePageSize} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size); setPage(1); }} />}
      </Box></div>
      {includesSuicideSeries && <Alert color="gray" title="Suicide data context">Use neutral, non-comparative language when discussing suicide data. <Anchor href="https://www.who.int/news-room/questions-and-answers/item/suicide" target="_blank" rel="noreferrer">Read the WHO suicide Q&amp;A</Anchor>.</Alert>}
      <Provenance snapshot={catalog.data?.meta.snapshot} releases={catalog.data?.meta.releases} />
    </>}
    <Alert icon={<Info size={18} />} color="blue" title="Exact-year association">Association joins numeric observations only when both independently chosen exact years map to the same canonical leaf M49 geography. It reports no p-value, regression, ranking, or causal conclusion.</Alert>
  </Stack></Container>;
}

function LinePanel({ series, query, tooManyGeographies }: { series?: Series; query: ReturnType<typeof useChartObservations>; tooManyGeographies: boolean }) {
  if (!series) return <EmptyState title="Choose one exact series.">Line charts show one complete published dimension tuple across exact years.</EmptyState>;
  if (tooManyGeographies) return <EmptyState title="Choose no more than 12 countries or areas.">Line charts never silently trim an over-limit shared selection.</EmptyState>;
  if (!query.isFetching && !query.data && query.fetchStatus === 'idle') return <EmptyState title="Choose 1–12 countries or areas for this line chart.">The atlas will not silently choose countries or render an unbounded set of lines.</EmptyState>;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  if (!query.data?.observations.some(isPublishedNumeric)) return <EmptyState title="No published numeric rows match this selection." />;
  return <><LineChartView observations={query.data.observations} availableYears={series.available_years} title={series.name} unit={series.unit} /><Provenance snapshot={query.data.meta.snapshot} releases={query.data.meta.releases} /></>;
}

function MapPanel({ series, year, query, geometry, selectedM49 }: { series?: Series; year: number | null; query: ReturnType<typeof useChartObservations>; geometry: ReturnType<typeof useMapGeometry>; selectedM49?: Set<string> }) {
  if (!series) return <EmptyState title="Choose one exact series." />;
  if (!year) return <EmptyState title="Choose an exact map year.">The atlas will not pick a nearby year.</EmptyState>;
  if (query.isPending || geometry.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  if (geometry.isError) return <QueryError error={geometry.error} title="Map geometry could not be loaded." />;
  if (!query.data?.observations.some(isMapEligible) || !geometry.data) return <EmptyState title="No published mapped rows match this exact year." />;
  return <><MapChartView observations={query.data.observations} geometry={geometry.data} title={`${series.name} · ${year}`} unit={series.unit} selectedM49={selectedM49} /><Provenance snapshot={query.data.meta.snapshot} releases={query.data.meta.releases} /></>;
}

function AssociationPanel({ xSeries, ySeries, xYear, yYear, query }: { xSeries?: Series; ySeries?: Series; xYear: number | null; yYear: number | null; query: ReturnType<typeof useAssociation> }) {
  if (!xSeries || !ySeries || !xYear || !yYear) return <EmptyState title="Choose X and Y series with independently exact years.">No year will be inferred from the other series.</EmptyState>;
  if (query.isPending) return <QueryLoading />;
  if (query.isError) return <QueryError error={query.error} />;
  const result = query.data;
  if (!result) return null;
  const coverage = [['Selected universe', result.coverage.selected_universe], ['Paired n', result.coverage.paired], ['X-only missing', result.coverage.x_only_missing], ['Y-only missing', result.coverage.y_only_missing], ['Both missing', result.coverage.both_missing]];
  return <Stack gap="md"><SimpleGrid cols={{ base: 2, sm: 5 }}>{coverage.map(([label, value]) => <Box className="metadata-row" key={String(label)}><Text size="xs" c="dimmed">{label}</Text><Text fw={700}>{value}</Text></Box>)}</SimpleGrid><Box className="metadata-row"><Text size="xs" c="dimmed">Pearson r</Text><Text fw={700}>{result.pearson_r === null || result.pearson_r === undefined ? 'Not reported' : result.pearson_r.toFixed(3)}</Text></Box>{result.warnings.map((warning) => <Alert key={warning} color="yellow">{warning}</Alert>)}<AssociationChartView result={result} xLabel={`${xSeries.name} · ${xYear}`} yLabel={`${ySeries.name} · ${yYear}`} /><Provenance snapshot={result.meta.snapshot} releases={result.meta.releases} /></Stack>;
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
