import { Anchor, Badge, Button, Card, Container, Group, Input, Select, SimpleGrid, Stack, Text, Title } from '@mantine/core';
import { Download, Search } from 'lucide-react';
import { parseAsString, useQueryState } from 'nuqs';
import { useEffect, useMemo, useState } from 'react';
import { fetchFreshLatestCatalog, observationsCsvUrl, useCatalog, useObservations } from '../api/queries';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { ObservationTable } from '../components/ObservationTable';
import { Provenance } from '../components/Provenance';

export function DataPage() {
  const [snapshot, setSnapshot] = useQueryState('snapshot', parseAsString);
  const catalog = useCatalog(snapshot ?? undefined);
  const [search, setSearch] = useState('');
  const [seriesId, setSeriesId] = useState<string | null>(null);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState<25 | 50 | 100>(25);
  const [resolvingLatest, setResolvingLatest] = useState(false);
  const [latestError, setLatestError] = useState(false);
  const allSeries = useMemo(() => catalog.data?.series ?? [], [catalog.data]);
  const selectedSeries = allSeries.find((series) => series.id === seriesId);
  const observations = useObservations({ snapshot: snapshot ?? undefined, series: seriesId ? [seriesId] : undefined, page, page_size: pageSize }, Boolean(seriesId));
  const filteredDatasets = (catalog.data?.datasets ?? []).filter((dataset) => `${dataset.name} ${dataset.description ?? ''} ${dataset.who_code}`.toLowerCase().includes(search.toLowerCase()));

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
      // Preserve the existing reproducible pin on a failed latest lookup.
      setLatestError(true);
    } finally {
      setResolvingLatest(false);
    }
  };

  return (
    <Container size="xl" className="page-section"><Stack gap="xl">
      <div><Title order={1}>Data catalog</Title>{catalog.data && <Stack gap={4} mt="sm"><Group gap="xs"><Text size="xs" c="dimmed">Resolved snapshot: {catalog.data.meta.snapshot.id}</Text><Button variant="default" size="compact-sm" loading={resolvingLatest} onClick={() => { void resolveLatest(); }}>View latest</Button></Group>{latestError && <Text c="red" size="xs">The latest snapshot could not be resolved; the current pin remains.</Text>}</Stack>}</div>
      {catalog.isPending ? <QueryLoading label="Loading catalog details…" /> : catalog.isError ? <QueryError error={catalog.error} /> : <>
        <Input value={search} onChange={(event) => setSearch(event.currentTarget.value)} placeholder="Search datasets and measures" leftSection={<Search size={16} aria-hidden />} aria-label="Search data catalog" />
        <SimpleGrid cols={{ base: 1, md: 2, lg: 3 }} spacing="md">{filteredDatasets.map((dataset) => { const count = allSeries.filter((series) => series.dataset_id === dataset.id).length; return <Card withBorder className="gallery-card" key={dataset.id}><Text className="card-tag">{dataset.who_code}</Text><Title order={2} size="h4" mt="sm">{dataset.name}</Title><Text size="sm" c="dimmed" mt="xs">{dataset.description}</Text><Group gap={5} mt="md">{dataset.capabilities.map((capability) => <Badge key={capability} color="gray" variant="outline">{capability}</Badge>)}</Group><Text size="xs" mt="md">{count} complete dimension tuple{count === 1 ? '' : 's'}</Text><Anchor href={dataset.source_url} target="_blank" rel="noreferrer" size="sm" mt="xs">WHO indicator source</Anchor></Card>; })}</SimpleGrid>
        <Card withBorder padding="lg"><Stack gap="sm"><div><Title order={2} size="h3">Dimensions</Title><Text size="sm" c="dimmed">Each series is one complete canonical dimension tuple. Values below are retained without collapsing source meaning.</Text></div>{catalog.data?.dimensions.length ? catalog.data.dimensions.map((dimension) => <details key={dimension.code}><summary>{dimension.code} · {dimension.name}</summary><Text size="sm" mt="xs">{dimension.values.join(', ')}</Text></details>) : <Text size="sm" c="dimmed">No dimensions are defined in this snapshot.</Text>}</Stack></Card>
        <Card withBorder padding="lg"><Stack><div><Title order={2} size="h3">Normalized observation table</Title><Text size="sm" c="dimmed">This UI pages server results. CSV streams every row matching the same series filter.</Text></div><Select searchable clearable label="Exact series" placeholder="Choose a series to inspect rows" value={seriesId} onChange={(value) => { setSeriesId(value); setPage(1); }} data={allSeries.map((series) => ({ value: series.id, label: `${series.name}${series.unit ? ` · ${series.unit}` : ''}` }))} />{selectedSeries && <Group gap={6}>{Object.entries(selectedSeries.dimensions).map(([name, value]) => <Badge key={name} variant="light" color="blue">{name}: {value}</Badge>)}</Group>}{seriesId && <Button component="a" variant="light" leftSection={<Download size={16} aria-hidden />} href={observationsCsvUrl({ snapshot: snapshot ?? undefined, series: [seriesId] })} w="fit-content">Download all filtered rows as CSV</Button>}{!seriesId ? <EmptyState title="Choose a series to load its observations." /> : observations.isPending ? <QueryLoading /> : observations.isError ? <QueryError error={observations.error} /> : observations.data ? <><ObservationTable observations={observations.data.observations} total={observations.data.pagination.total} page={page} pageSize={pageSize} onPageChange={setPage} onPageSizeChange={(size) => { setPageSize(size as 25 | 50 | 100); setPage(1); }} /><Provenance snapshot={observations.data.meta.snapshot} releases={observations.data.meta.releases} /></> : null}</Stack></Card>
        <Provenance snapshot={catalog.data?.meta.snapshot} releases={catalog.data?.meta.releases} />
      </>}
    </Stack></Container>
  );
}
