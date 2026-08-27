import { ActionIcon, Group, Tooltip } from '@mantine/core';
import { Download, FileImage, FileType2 } from 'lucide-react';
import { exportToPNG, exportToSVG, type EChartsReactRef } from 'react-echarts-library/core';
import type { RefObject } from 'react';

function safeFilename(title: string): string {
  return `${title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/(^-|-$)/g, '') || 'context-atlas-chart'}`;
}

export function ChartActions({ chartRef, title }: { chartRef: RefObject<EChartsReactRef | null>; title: string }) {
  const exportChart = (format: 'png' | 'svg') => {
    const chart = chartRef.current?.getEchartsInstance();
    if (!chart) return;
    const filename = `${safeFilename(title)}.${format}`;
    if (format === 'png') exportToPNG(chart, { filename, pixelRatio: 2 });
    else exportToSVG(chart, { filename });
  };

  return (
    <Group gap={4} aria-label="Chart export actions">
      <Tooltip label="Export chart as PNG" openDelay={300}>
        <ActionIcon variant="subtle" color="dark" aria-label="Export chart as PNG" onClick={() => exportChart('png')}><FileImage size={18} aria-hidden /></ActionIcon>
      </Tooltip>
      <Tooltip label="Export chart as SVG" openDelay={300}>
        <ActionIcon variant="subtle" color="dark" aria-label="Export chart as SVG" onClick={() => exportChart('svg')}><FileType2 size={18} aria-hidden /></ActionIcon>
      </Tooltip>
      <Tooltip label="Exports retain the chart attribution" openDelay={300}>
        <Download size={15} aria-hidden color="var(--mantine-color-dimmed)" />
      </Tooltip>
    </Group>
  );
}
