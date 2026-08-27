import { Alert, Anchor, Badge, Button, Card, Container, Group, Loader, SimpleGrid, Text, Title } from '@mantine/core';
import { Link } from 'react-router';
import { useMemo, type ReactNode } from 'react';
import { useAssociation, useCatalog, useChartObservations, useGroups, useMapGeometry } from '../api/queries';
import type { CatalogResponse, Series } from '../api/types';
import { launchDatasets } from '../catalog';
import { EmptyState, QueryError, QueryLoading } from '../components/AsyncState';
import { AssociationChartView, awareClass, CompositionChartView, isMapEligible, isPublishedNumeric, LineChartView, MapChartView } from '../components/ChartViews';

export function HomePage() {
  const catalog = useCatalog();
  const snapshot = catalog.data?.meta.snapshot.id;
  const activeCodes = new Set(catalog.data?.datasets.map((dataset) => dataset.who_code));

  return (
    <>
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

function explorePreset(snapshot: string, params: Record<string, string | number>): string {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => search.set(key, String(value)));
  return withSnapshot(`/explore?${search.toString()}`, snapshot);
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
  const groups = useGroups(snapshot);
  const homicideRows = useChartObservations({ snapshot, series: homicide ? [homicide.id] : undefined, years: homicideYear ? [homicideYear] : undefined }, Boolean(homicide && homicideYear));
  const map = useMapGeometry(snapshot, Boolean(homicide && homicideYear));
  const tobaccoRows = useChartObservations({ snapshot, series: tobacco ? [tobacco.id] : undefined, geographies: ['840'] }, Boolean(tobacco));
  const awareRows = useChartObservations({ snapshot, series: aware.map((series) => series.id), years: awareYear ? [awareYear] : undefined }, Boolean(aware.length && awareYear));
  const seriesByID = useMemo(() => new Map(catalog.series.map((series) => [series.id, series])), [catalog.series]);
  const associationTo = alcohol && suicide && alcoholYear !== undefined && suicideYear !== undefined ? explorePreset(snapshot, { view: 'association', x_series: alcohol.id, x_year: alcoholYear, y_series: suicide.id, y_year: suicideYear }) : undefined;
  const homicideTo = homicide && homicideYear !== undefined ? explorePreset(snapshot, { view: 'map', series: homicide.id, year: homicideYear }) : undefined;
  const tobaccoTo = tobacco ? explorePreset(snapshot, { view: 'line', series: tobacco.id, geographies: '840' }) : undefined;
  const awareTo = aware[0] && awareYear !== undefined ? explorePreset(snapshot, { view: 'composition', series: aware[0].id, year: awareYear }) : undefined;

  return <section className="page-section-tight"><Container size="xl"><Group justify="space-between" mb="lg" align="end"><div><Text className="editorial-kicker">Live gallery</Text><Title order={2}>Four compact views, backed by this snapshot</Title><Text size="sm" c="dimmed">These editorial presets name their exact source series and years. Open any view to change the question.</Text></div></Group><SimpleGrid cols={{ base: 1, lg: 2 }} spacing="lg">
    <FeaturedCard title="Alcohol and suicide" detail={alcohol && suicide && alcoholYear && suicideYear ? `Exact years: ${alcoholYear} and ${suicideYear}` : 'Waiting for compatible published series.'} to={associationTo}>
      {association.isPending ? <QueryLoading /> : association.isError ? <QueryError error={association.error} /> : association.data?.points.length ? <><AssociationChartView compact result={association.data} xLabel={`${alcohol?.name} · ${alcoholYear}`} yLabel={`${suicide?.name} · ${suicideYear}`} groups={groups.data?.groups} /><Anchor href="https://www.who.int/news-room/questions-and-answers/item/suicide" target="_blank" rel="noreferrer" size="xs">WHO suicide Q&amp;A</Anchor></> : <EmptyState title="No paired published observations are available for this preset." />}
    </FeaturedCard>
    <FeaturedCard title="Homicide mortality map" detail={homicideYear ? `Exact year: ${homicideYear}` : 'Waiting for a published exact year.'} to={homicideTo}>
      {homicideRows.isPending || map.isPending ? <QueryLoading /> : homicideRows.isError ? <QueryError error={homicideRows.error} /> : map.isError ? <QueryError error={map.error} title="Map geometry could not be loaded." /> : homicideRows.data?.observations.some(isMapEligible) && map.data ? <MapChartView compact observations={homicideRows.data.observations} geometry={map.data} title={`Homicide mortality · ${homicideYear}`} unit={homicide?.unit} /> : <EmptyState title="No published map values are available for this preset." />}
    </FeaturedCard>
    <FeaturedCard title="Tobacco trend" detail="United States of America (M49 840), exact published years" to={tobaccoTo}>
      {tobaccoRows.isPending ? <QueryLoading /> : tobaccoRows.isError ? <QueryError error={tobaccoRows.error} /> : tobaccoRows.data?.observations.some(isPublishedNumeric) ? <LineChartView compact observations={tobaccoRows.data.observations} availableYears={tobacco?.available_years} title={tobacco?.name ?? 'Tobacco prevalence'} unit={tobacco?.unit} /> : <EmptyState title="No published tobacco observations are available for this exact preset." />}
    </FeaturedCard>
    <FeaturedCard title="AWaRe composition" detail={awareYear ? `Exact year: ${awareYear}; Access, Watch, Reserve only` : 'Waiting for a common published exact year.'} to={awareTo}>
      {awareRows.isPending ? <QueryLoading /> : awareRows.isError ? <QueryError error={awareRows.error} /> : awareRows.data?.observations.some(isPublishedNumeric) ? <CompositionChartView compact observations={awareRows.data.observations} seriesById={seriesByID} title={`AWaRe composition · ${awareYear}`} /> : <EmptyState title="No published AWaRe rows are available for this exact preset." />}
    </FeaturedCard>
  </SimpleGrid></Container></section>;
}

function FeaturedCard({ title, detail, to, children }: { title: string; detail: string; to?: string; children: ReactNode }) {
  return <Card withBorder className="gallery-card" padding="lg"><Group justify="space-between" align="start" mb="xs"><div><Title order={3} size="h4">{title}</Title><Text size="xs" c="dimmed">{detail}</Text></div>{to && <Button component={Link} to={to} size="compact-sm" variant="subtle" aria-label={`Explore ${title}`}>Explore</Button>}</Group>{children}</Card>;
}
