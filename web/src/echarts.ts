import * as echarts from 'echarts/core';
import { BarChart, LineChart, MapChart, ScatterChart } from 'echarts/charts';
import {
  AriaComponent,
  DatasetComponent,
  GraphicComponent,
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
  VisualMapComponent,
} from 'echarts/components';
import { CanvasRenderer, SVGRenderer } from 'echarts/renderers';

echarts.use([
  AriaComponent,
  BarChart,
  CanvasRenderer,
  DatasetComponent,
  GraphicComponent,
  GridComponent,
  LegendComponent,
  LineChart,
  MapChart,
  ScatterChart,
  SVGRenderer,
  TitleComponent,
  TooltipComponent,
  VisualMapComponent,
]);

export { echarts };
