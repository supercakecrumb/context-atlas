import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  confirmImportPreview,
  createImportPreview,
  deleteAdminSession,
  getAdminSession,
  getGetAdminSessionQueryKey,
  getGetImportFreshnessQueryKey,
  getGetImportPreviewQueryKey,
  getListImportRunsQueryKey,
  getListObservationsQueryKey,
  getCatalog,
  listObservations,
  refreshCatalog,
  submitFeedback,
  useExploreAssociation,
  useGetAdmin0Map,
  useGetCatalog,
  useGetImportFreshness,
  useGetImportPreview,
  useListGeographies,
  useListGroups,
  useListImportRuns,
  useListObservations,
} from './generated/default/default';
import type {
  ExploreAssociationParams,
  FeedbackRequest,
  GetAdmin0Map200,
  ListObservationsParams,
} from './generated/models';
import { ApiError, setCSRFToken } from './client';
import {
  normalizeAssociation,
  normalizeCatalog,
  normalizeFreshness,
  normalizeGeographies,
  normalizeGroups,
  normalizeImportPreview,
  normalizeImportRuns,
  normalizeObservations,
} from './types';
import type {
  AdminSession,
  AssociationResult,
  CatalogResponse,
  FreshnessResult,
  GeographiesResponse,
  GroupsResponse,
  ImportPreview,
  ImportRun,
  ImportRunResult,
  ObservationsResponse,
} from './types';

export type ObservationFilters = ListObservationsParams;
export type AssociationFilters = ExploreAssociationParams;

function data<T>(response: { data: unknown }): T {
  return response.data as T;
}

export const queryKeys = {
  chartObservations: (filters: ObservationFilters) => ['chart-observations', ...getListObservationsQueryKey(filters)] as const,
  adminSession: getGetAdminSessionQueryKey(),
  importPreview: (previewID?: string) => getGetImportPreviewQueryKey(previewID ?? ''),
  importRuns: getListImportRunsQueryKey(),
  freshness: getGetImportFreshnessQueryKey(),
};

export function useCatalog(snapshot?: string) {
  return useGetCatalog<CatalogResponse>(snapshot ? { snapshot } : undefined, {
    query: { select: (response) => normalizeCatalog(data(response)) },
  });
}

// This deliberately bypasses TanStack Query's short-lived latest-catalog cache.
// Callers use it only when the user explicitly asks to resolve and pin "View latest".
export async function fetchFreshLatestCatalog(): Promise<CatalogResponse> {
  return normalizeCatalog(data(await getCatalog(undefined, { cache: 'no-store' })));
}

export function useGeographies(snapshot?: string) {
  return useListGeographies<GeographiesResponse>(snapshot ? { snapshot } : undefined, {
    query: { select: (response) => normalizeGeographies(data(response)) },
  });
}

export function useGroups(snapshot?: string) {
  return useListGroups<GroupsResponse>(snapshot ? { snapshot } : undefined, {
    query: { select: (response) => normalizeGroups(data(response)) },
  });
}

export function useMapGeometry(snapshot: string | undefined, enabled: boolean) {
  return useGetAdmin0Map(snapshot ? { snapshot } : undefined, {
    query: { enabled, select: (response) => data<GetAdmin0Map200>(response) as unknown as GeoJSON.FeatureCollection },
  });
}

export function useObservations(filters: ObservationFilters, enabled = true) {
  return useListObservations<ObservationsResponse>(filters, {
    query: { enabled: enabled && Boolean(filters.series?.length), select: (response) => normalizeObservations(data(response)) },
  });
}

export function useChartObservations(filters: ObservationFilters, enabled = true) {
  return useQuery({
    queryKey: queryKeys.chartObservations(filters),
    queryFn: () => loadAllChartObservations(filters),
    enabled: enabled && Boolean(filters.series?.length),
  });
}

async function loadAllChartObservations(filters: ObservationFilters): Promise<ObservationsResponse> {
  const first = normalizeObservations(data(await listObservations({ ...filters, page: 1, page_size: 500 })));
  const pageCount = Math.ceil(first.pagination.total / 500);
  if (pageCount <= 1) return first;

  const remaining = await Promise.all(Array.from({ length: pageCount - 1 }, (_, index) => listObservations({
    ...filters,
    page: index + 2,
    page_size: 500,
  })));
  return {
    ...first,
    observations: [
      ...(first.observations ?? []),
      ...remaining.flatMap((page) => normalizeObservations(data(page)).observations),
    ],
  };
}

export function useAssociation(filters: AssociationFilters | undefined) {
  const requested = filters ?? { x_series: '', x_year: 1, y_series: '', y_year: 1 };
  return useExploreAssociation<AssociationResult>(requested, {
    query: {
      enabled: Boolean(filters?.x_series && filters?.y_series && filters.x_year && filters.y_year),
      select: (response) => normalizeAssociation(data(response)),
    },
  });
}

export function useAdminSession() {
  return useQuery({
    queryKey: queryKeys.adminSession,
    queryFn: async (): Promise<AdminSession | null> => {
      try {
        const session = data<AdminSession>(await getAdminSession());
        setCSRFToken(session.csrf_token);
        return session;
      } catch (error) {
        if (error instanceof ApiError && [401, 403].includes(error.status)) return null;
        throw error;
      }
    },
    retry: false,
  });
}

export function useImportRuns(enabled: boolean, page = 1, pageSize = 25) {
  return useListImportRuns<ImportRunResult>({ page, page_size: pageSize as 25 | 50 | 100 }, {
    query: {
      enabled,
      retry: false,
      select: (response) => normalizeImportRuns(data(response)),
      refetchInterval: (query) => {
        if (!query.state.data) return false;
        return normalizeImportRuns(data(query.state.data)).runs.some((run) => run.status === 'pending' || run.status === 'running') ? 2_000 : false;
      },
    },
  });
}

export function useFreshness(enabled: boolean) {
  return useGetImportFreshness<FreshnessResult>({
    query: { enabled, retry: false, refetchInterval: 30_000, select: (response) => normalizeFreshness(data(response)) },
  });
}

export function useImportPreview(previewID?: string) {
  return useGetImportPreview<ImportPreview>(previewID ?? '', {
    query: {
      enabled: Boolean(previewID),
      retry: false,
      select: (response) => normalizeImportPreview(data(response)),
      refetchInterval: (query) => {
        if (!query.state.data) return false;
        const status = normalizeImportPreview(data(query.state.data)).status;
        return status === 'pending' || status === 'running' ? 2_000 : false;
      },
    },
  });
}

export function usePreviewImport() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (url: string) => normalizeImportPreview(data(await createImportPreview({ url }))),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: queryKeys.importRuns });
    },
  });
}

export function useConfirmImport() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async (previewID: string) => data<ImportRun>(await confirmImportPreview(previewID)),
    onSuccess: (_, previewID) => {
      client.invalidateQueries({ queryKey: queryKeys.importRuns });
      client.invalidateQueries({ queryKey: queryKeys.importPreview(previewID) });
      client.invalidateQueries({ queryKey: queryKeys.freshness });
    },
  });
}

export function useRefresh() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: async () => data<ImportRun>(await refreshCatalog()),
    onSuccess: () => {
      client.invalidateQueries({ queryKey: queryKeys.importRuns });
      client.invalidateQueries({ queryKey: queryKeys.freshness });
    },
  });
}

export function useLogout() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => deleteAdminSession(),
    onSuccess: () => {
      setCSRFToken(undefined);
      client.setQueryData(queryKeys.adminSession, null);
    },
  });
}

export function useFeedback() {
  return useMutation({
    mutationFn: (input: FeedbackRequest) => submitFeedback(input),
  });
}

export { getDownloadObservationsCsvUrl as observationsCsvUrl } from './generated/default/default';
