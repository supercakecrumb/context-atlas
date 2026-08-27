import type { ChartCapability, Dataset } from './api/types';

export interface LaunchDataset {
  id: string;
  who_id: string;
  code: string;
  name: string;
  description: string;
  views: ChartCapability[];
}

export const launchDatasets: LaunchDataset[] = [
  { id: 'suicide-mortality', who_id: '16BBF41', code: 'SDGSUICIDE', name: 'Suicide mortality', description: 'Age-standardized suicide mortality, with neutral context and uncertainty where published.', views: ['line', 'map', 'association', 'table'] },
  { id: 'alcohol-consumption', who_id: 'EE6F72A', code: 'SA_0000001688', name: 'Alcohol consumption', description: 'Recorded alcohol consumption across countries and areas.', views: ['line', 'map', 'association', 'table'] },
  { id: 'tobacco-prevalence', who_id: '75DDA77', code: 'M_Est_tob_curr_std', name: 'Tobacco prevalence', description: 'Current tobacco use prevalence with source dimensions retained.', views: ['line', 'map', 'association', 'table'] },
  { id: 'homicide-mortality', who_id: '361734E', code: 'VIOLENCE_HOMICIDERATE', name: 'Homicide mortality', description: 'Homicide mortality rate as published by WHO.', views: ['line', 'map', 'association', 'table'] },
  { id: 'aware-antibiotic-consumption', who_id: '19E688D', code: 'GLASSAMC_AWARE', name: 'AWaRe antibiotic consumption', description: 'Access, Watch, and Reserve composition, without losing individual classes.', views: ['composition', 'table'] },
  { id: 'road-traffic-mortality', who_id: 'D6176E2', code: 'RS_198', name: 'Road traffic mortality', description: 'Road traffic mortality rate by country and area.', views: ['line', 'map', 'association', 'table'] },
];

export function datasetSummary(dataset: Dataset | LaunchDataset): string {
  return dataset.description ?? '';
}
