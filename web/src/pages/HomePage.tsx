import { Alert, Anchor, Badge, Button, Card, Container, Group, Loader, SimpleGrid, Stack, Text, Title } from '@mantine/core';
import { ArrowRight, ChartNoAxesCombined, Globe2, TableProperties } from 'lucide-react';
import { Link } from 'react-router';
import { useMemo, type ReactNode } from 'react';
import { useAssociation, useCatalog, useChartObservations, useMapGeometry } from '../api/queries';
import type { CatalogResponse, Series } from '../api/types';
import { launchDatasets } from '../catalog';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { AssociationChartView, awareClass, CompositionChartView, isMapEligible, isPublishedNumeric, LineChartView, MapChartView } from '../components/ChartViews';

const editorialCards = [
  { label: 'Explore association', title: 'Alcohol consumption and suicide mortality', detail: 'Choose each measure and year independently, then inspect only matched country-area pairs.', to: '/explore?view=association', icon: ChartNoAxesCombined },
  { label: 'Map', title: 'Homicide mortality by exact year', detail: 'A choropleth with a separate no-data state, source rows, and boundary context.', to: '/explore?view=map', icon: Globe2 },
  { label: 'Compare', title: 'Tobacco prevalence over time', detail: 'Line charts retain gaps instead of filling or estimating missing years.', to: '/explore?view=line', icon: ChartNoAxesCombined },
  { label: 'Composition', title: 'AWaRe antibiotic consumption', detail: 'Access, Watch, and Reserve shares, alongside their individual published series.', to: '/explore?view=composition', icon: TableProperties },
];

export function HomePage() {
  const catalog = useCatalog();
  const snapshot = catalog.data?.meta.snapshot.id;
  const activeCodes = new Set(catalog.data?.datasets.map((dataset) => dataset.who_code));

  return (
    <>
      <section className="page-section">
        <Container size="xl">
          <div className="hero-grid">
            <Stack gap="lg">
              <Text className="editorial-kicker">A public WHO data atlas</Text>
              <Title className="display-title">Health data needs its context.</Title>
              <Text className="lede">Context Atlas lets you compare WHO indicators across countries and areas, inspect the rows behind every visual, and explore exact-year associations without turning them into causal claims.</Text>
              <Group>
                <Button component={Link} to={withSnapshot('/explore', snapshot)} size="md" rightSection={<ArrowRight size={17} aria-hidden />}>Start exploring</Button>
                <Button component={Link} to={withSnapshot('/data', snapshot)} size="md" variant="default">Browse data</Button>
              </Group>
            </Stack>
            <div className="hero-side">
              <Text size="sm" tt="uppercase" fw={700} lts=".08em" c="teal.2">The public release</Text>
              <div><strong>6</strong><Text size="lg">WHO indicators, full source rows, no top-N sampling.</Text></div>
              <Text size="sm" c="gray.3">Every shared analysis resolves to an immutable catalog snapshot and named source releases.</Text>
            </div>
          </div>
        </Container>
      </section>

      <section className="page-section-tight">
        <Container size="xl">
          <Group justify="space-between" mb="lg" align="end"><div><Text className="editorial-kicker">Start with a view</Text><Title order={2}>Four ways into the atlas</Title></div><Anchor component={Link} to={withSnapshot('/explore', snapshot)}>Open the explorer</Anchor></Group>
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 4 }} spacing="lg">
            {editorialCards.map(({ label, title, detail, to, icon: Icon }) => (
              <Card component={Link} to={withSnapshot(to, snapshot)} className="gallery-card" padding="lg" radius="md" key={title} withBorder>
                <Group justify="space-between" mb="xl"><Text className="card-tag">{label}</Text><Icon size={21} aria-hidden /></Group>
                <Title order={3} size="h4" mb="xs">{title}</Title><Text size="sm" c="dimmed">{detail}</Text>
              </Card>
            ))}
          </SimpleGrid>
        </Container>
      </section>

      {catalog.data && <FeaturedViews catalog={catalog.data} />}

      <section className="page-section">
        <Container size="xl">
          <Group justify="space-between" mb="lg" align="end"><div><Text className="editorial-kicker">Launch catalog</Text><Title order={2}>Six WHO datasets, kept whole</Title></div>{catalog.isPending && <Loader size="sm" color="dark" />}</Group>
          {catalog.isError && <Alert color="yellow" mb="lg" title="The live catalog is temporarily unavailable.">The launch catalog below is descriptive only; values are never shown from a fallback source.</Alert>}
          <SimpleGrid cols={{ base: 1, sm: 2, lg: 3 }} spacing="md">
            {launchDatasets.map((dataset) => (
              <Card key={dataset.code} className="gallery-card" padding="lg" withBorder component={Link} to={withSnapshot(`/explore?view=${dataset.views[0]}`, snapshot)}>
                <Group justify="space-between" align="start"><Text className="card-tag">WHO {dataset.who_id}</Text>{activeCodes.has(dataset.code) && <Badge color="teal" variant="light">Live</Badge>}</Group>
                <Title order={3} size="h4" mt="md">{dataset.name}</Title>
                <Text size="sm" c="dimmed" mt="xs">{dataset.description}</Text>
                <Group gap={5} mt="lg">{dataset.views.map((view) => <Badge key={view} variant="outline" color="gray">{view}</Badge>)}</Group>
              </Card>
            ))}
          </SimpleGrid>
        </Container>
      </section>
    </>
  );
}

function withSnapshot(to: string, snapshot?: string): string {
  return snapshot ? `${to}${to.includes('?') ? '&' : '?'}snapshot=${encodeURIComponent(snapshot)}` : to;
}

function seriesFor(catalog: CatalogResponse, whoCode: string): Series | undefined {
  const dataset = catalog.datasets.find((item) => item.who_code === whoCode);
  const candidates = catalog.series.filter((series) => series.dataset_id === dataset?.id).sort((a, b) => `${a.name}:${a.id}`.localeCompare(`${b.name}:${b.id}`));
  return candidates.find((series) => Object.values(series.dimensions).every((value) => value.toUpperCase() === 'TOTAL')) ?? candidates[0];
}

function latestYear(series?: Series): number | undefined {
  return series?.available_years.length ? Math.max(...series.available_years) : undefined;
}

function latestCommonYear(series: Series[]): number | undefined {
  if (!series.length) return undefined;
  const [first, ...rest] = series;
  return first.available_years.filter((year) => rest.every((item) => item.available_years.includes(year))).sort((a, b) => b - a)[0];
}

function FeaturedViews({ catalog }: { catalog: CatalogResponse }) {
  const snapshot = catalog.meta.snapshot.id;
  const alcohol = seriesFor(catalog, 'SA_0000001688');
  const suicide = seriesFor(catalog, 'SDGSUICIDE');
  const homicide = seriesFor(catalog, 'VIOLENCE_HOMICIDERATE');
  const tobacco = seriesFor(catalog, 'M_Est_tob_curr_std');
  const awareDataset = catalog.datasets.find((dataset) => dataset.who_code === 'GLASSAMC_AWARE');
  const aware = catalog.series.filter((series) => series.dataset_id === awareDataset?.id && awareClass(series) !== 'Other');
  const alcoholYear = latestYear(alcohol);
  const suicideYear = latestYear(suicide);
  const homicideYear = latestYear(homicide);
  const awareYear = latestCommonYear(aware);
  const association = useAssociation(alcohol && suicide && alcoholYear && suicideYear ? { snapshot, x_series: alcohol.id, x_year: alcoholYear, y_series: suicide.id, y_year: suicideYear } : undefined);
  const homicideRows = useChartObservations({ snapshot, series: homicide ? [homicide.id] : undefined, years: homicideYear ? [homicideYear] : undefined }, Boolean(homicide && homicideYear));
  const map = useMapGeometry(snapshot, Boolean(homicide && homicideYear));
  const tobaccoRows = useChartObservations({ snapshot, series: tobacco ? [tobacco.id] : undefined, geographies: ['840'] }, Boolean(tobacco));
  const awareRows = useChartObservations({ snapshot, series: aware.map((series) => series.id), years: awareYear ? [awareYear] : undefined }, Boolean(aware.length && awareYear));
  const seriesByID = useMemo(() => new Map(catalog.series.map((series) => [series.id, series])), [catalog.series]);

  return <section className="page-section-tight"><Container size="xl"><Group justify="space-between" mb="lg" align="end"><div><Text className="editorial-kicker">Live gallery</Text><Title order={2}>Four compact views, backed by this snapshot</Title><Text size="sm" c="dimmed">These editorial presets name their exact source series and years. Open any view to change the question.</Text></div></Group><SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
    <FeaturedCard title="Alcohol and suicide" detail={alcohol && suicide && alcoholYear && suicideYear ? `Exact years: ${alcoholYear} and ${suicideYear}` : 'Waiting for compatible published series.'} to={withSnapshot('/explore?view=association', snapshot)}>
      {association.isPending ? <QueryLoading /> : association.isError ? <QueryError error={association.error} /> : association.data?.points.length ? <><AssociationChartView compact result={association.data} xLabel={`${alcohol?.name} · ${alcoholYear}`} yLabel={`${suicide?.name} · ${suicideYear}`} /><Anchor href="https://www.who.int/news-room/questions-and-answers/item/suicide" target="_blank" rel="noreferrer" size="xs">WHO suicide Q&amp;A</Anchor></> : <EmptyState title="No paired published observations are available for this preset." />}
    </FeaturedCard>
    <FeaturedCard title="Homicide mortality map" detail={homicideYear ? `Exact year: ${homicideYear}` : 'Waiting for a published exact year.'} to={withSnapshot('/explore?view=map', snapshot)}>
      {homicideRows.isPending || map.isPending ? <QueryLoading /> : homicideRows.isError ? <QueryError error={homicideRows.error} /> : map.isError ? <QueryError error={map.error} title="Map geometry could not be loaded." /> : homicideRows.data?.observations.some(isMapEligible) && map.data ? <MapChartView compact observations={homicideRows.data.observations} geometry={map.data} title={`Homicide mortality · ${homicideYear}`} unit={homicide?.unit} /> : <EmptyState title="No published map values are available for this preset." />}
    </FeaturedCard>
    <FeaturedCard title="Tobacco trend" detail="United States of America (M49 840), exact published years" to={withSnapshot('/explore?view=line&geographies=840', snapshot)}>
      {tobaccoRows.isPending ? <QueryLoading /> : tobaccoRows.isError ? <QueryError error={tobaccoRows.error} /> : tobaccoRows.data?.observations.some(isPublishedNumeric) ? <LineChartView compact observations={tobaccoRows.data.observations} availableYears={tobacco?.available_years} title={tobacco?.name ?? 'Tobacco prevalence'} unit={tobacco?.unit} /> : <EmptyState title="No published tobacco observations are available for this exact preset." />}
    </FeaturedCard>
    <FeaturedCard title="AWaRe composition" detail={awareYear ? `Exact year: ${awareYear}; Access, Watch, Reserve only` : 'Waiting for a common published exact year.'} to={withSnapshot('/explore?view=composition', snapshot)}>
      {awareRows.isPending ? <QueryLoading /> : awareRows.isError ? <QueryError error={awareRows.error} /> : awareRows.data?.observations.some(isPublishedNumeric) ? <CompositionChartView compact observations={awareRows.data.observations} seriesById={seriesByID} title={`AWaRe composition · ${awareYear}`} /> : <EmptyState title="No published AWaRe rows are available for this exact preset." />}
    </FeaturedCard>
  </SimpleGrid></Container></section>;
}

function FeaturedCard({ title, detail, to, children }: { title: string; detail: string; to: string; children: ReactNode }) {
  return <Card withBorder className="gallery-card" padding="lg"><Group justify="space-between" align="start" mb="xs"><div><Title order={3} size="h4">{title}</Title><Text size="xs" c="dimmed">{detail}</Text></div><Button component={Link} to={to} size="compact-sm" variant="subtle">Explore</Button></Group>{children}</Card>;
}
