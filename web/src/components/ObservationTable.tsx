import { Box, Text } from '@mantine/core';
import { DataTable } from 'mantine-datatable';
import type { Observation } from '../api/types';

function formatNumber(value: number | null | undefined): string {
  return value === null || value === undefined ? '—' : new Intl.NumberFormat('en-US', { maximumFractionDigits: 3 }).format(value);
}

export function ObservationTable({
  observations,
  total,
  page,
  pageSize,
  onPageChange,
  onPageSizeChange,
}: {
  observations: Observation[];
  total: number;
  page: number;
  pageSize: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
}) {
  return (
    <Box>
      <Text id="observations-table-title" fw={600} mb="xs">Accessible observation table</Text>
      <DataTable
        aria-label="Observations"
        withTableBorder
        striped
        highlightOnHover
        minHeight={180}
        records={observations}
        totalRecords={total}
        page={page}
        recordsPerPage={pageSize}
        recordsPerPageOptions={[25, 50, 100]}
        onPageChange={onPageChange}
        onRecordsPerPageChange={onPageSizeChange}
        noRecordsText="No source observations match these filters."
        columns={[
          { accessor: 'source_geography.name', title: 'Country or area' },
          { accessor: 'year', title: 'Year' },
          { accessor: 'display_value', title: 'Display value' },
          { accessor: 'numeric_value', title: 'Numeric value', render: ({ numeric_value }) => formatNumber(numeric_value) },
          { accessor: 'lower_bound', title: 'Lower bound', render: ({ lower_bound }) => formatNumber(lower_bound) },
          { accessor: 'upper_bound', title: 'Upper bound', render: ({ upper_bound }) => formatNumber(upper_bound) },
          { accessor: 'status', title: 'Value status' },
          { accessor: 'publish_state', title: 'Publish state' },
        ]}
      />
    </Box>
  );
}
