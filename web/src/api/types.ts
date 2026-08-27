import type * as Generated from './generated/models';

// These aliases only remove nullable collection noise after normalization; API DTOs stay generated.
type WithArrays<T, Keys extends keyof T> = Omit<T, Keys> & {
  [Key in Keys]-?: Exclude<T[Key], null | undefined>;
};

export type SnapshotRef = Generated.SnapshotRef;
export type DatasetRelease = Generated.DatasetReleaseRef;
export type ResponseMeta = WithArrays<Generated.ResponseMeta, 'releases'>;
export type Dataset = WithArrays<Generated.Dataset, 'capabilities'>;
export type Measure = Generated.Measure;
export type DimensionDefinition = WithArrays<Generated.DimensionDefinition, 'values'>;
export type Series = WithArrays<Generated.Series, 'available_years'>;
export type Geography = Generated.Geography;
export type GroupMembership = Generated.GroupMembership;
export type Group = WithArrays<Generated.Group, 'members'>;
export type Pagination = Generated.Pagination;
export type Observation = Generated.Observation;
export type AssociationPoint = Generated.AssociationPoint;
export type AssociationCoverage = Generated.AssociationCoverage;
export type RowAccounting = Generated.RowAccounting;
export type ImportRun = Generated.ImportRun;
export type DatasetFreshness = Generated.DatasetFreshness;
export type AdminSession = Generated.AdminSession;
export type FeedbackInput = Generated.FeedbackRequest;
export type ChartCapability = Dataset['capabilities'][number];

export type CatalogResponse = Omit<WithArrays<Generated.CatalogResult, 'datasets' | 'dimensions' | 'measures' | 'series'>, 'datasets' | 'dimensions' | 'measures' | 'meta' | 'series'> & {
  datasets: Dataset[];
  dimensions: DimensionDefinition[];
  measures: Measure[];
  meta: ResponseMeta;
  series: Series[];
};
export type GeographiesResponse = Omit<WithArrays<Generated.GeographyResult, 'geographies'>, 'meta'> & { meta: ResponseMeta };
export type GroupsResponse = Omit<WithArrays<Generated.GroupResult, 'groups'>, 'groups' | 'meta'> & { groups: Group[]; meta: ResponseMeta };
export type ObservationsResponse = Omit<WithArrays<Generated.ObservationResult, 'observations'>, 'meta'> & { meta: ResponseMeta };
export type AssociationResult = Omit<WithArrays<Generated.AssociationResult, 'points' | 'warnings'>, 'meta'> & { meta: ResponseMeta };
export type ImportPreview = WithArrays<Generated.ImportPreview, 'dimensions' | 'headers' | 'measures' | 'units' | 'unmapped_geographies' | 'warnings'>;
export type ImportRunResult = WithArrays<Generated.ImportRunResult, 'runs'>;
export type FreshnessResult = WithArrays<Generated.FreshnessResult, 'datasets'>;

function meta(value: Generated.ResponseMeta): ResponseMeta {
  return { ...value, releases: value.releases ?? [] };
}

function dataset(value: Generated.Dataset): Dataset {
  return { ...value, capabilities: value.capabilities ?? [] };
}

function dimension(value: Generated.DimensionDefinition): DimensionDefinition {
  return { ...value, values: value.values ?? [] };
}

function series(value: Generated.Series): Series {
  return { ...value, available_years: value.available_years ?? [] };
}

function group(value: Generated.Group): Group {
  return { ...value, members: value.members ?? [] };
}

export function normalizeCatalog(value: Generated.CatalogResult): CatalogResponse {
  return {
    ...value,
    datasets: (value.datasets ?? []).map(dataset),
    dimensions: (value.dimensions ?? []).map(dimension),
    measures: value.measures ?? [],
    meta: meta(value.meta),
    series: (value.series ?? []).map(series),
  };
}

export function normalizeGeographies(value: Generated.GeographyResult): GeographiesResponse {
  return { ...value, geographies: value.geographies ?? [], meta: meta(value.meta) };
}

export function normalizeGroups(value: Generated.GroupResult): GroupsResponse {
  return { ...value, groups: (value.groups ?? []).map(group), meta: meta(value.meta) };
}

export function normalizeObservations(value: Generated.ObservationResult): ObservationsResponse {
  return { ...value, observations: value.observations ?? [], meta: meta(value.meta) };
}

export function normalizeAssociation(value: Generated.AssociationResult): AssociationResult {
  return { ...value, points: value.points ?? [], warnings: value.warnings ?? [], meta: meta(value.meta) };
}

export function normalizeImportPreview(value: Generated.ImportPreview): ImportPreview {
  return {
    ...value,
    dimensions: (value.dimensions ?? []).map(dimension),
    headers: value.headers ?? [],
    measures: value.measures ?? [],
    units: value.units ?? [],
    unmapped_geographies: value.unmapped_geographies ?? [],
    warnings: value.warnings ?? [],
  };
}

export function normalizeImportRuns(value: Generated.ImportRunResult): ImportRunResult {
  return { ...value, runs: value.runs ?? [] };
}

export function normalizeFreshness(value: Generated.FreshnessResult): FreshnessResult {
  return { ...value, datasets: value.datasets ?? [] };
}
