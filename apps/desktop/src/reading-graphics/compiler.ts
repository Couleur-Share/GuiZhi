import mermaid from "mermaid";
import * as echarts from "echarts/core";
import { BarChart, LineChart, PieChart } from "echarts/charts";
import { GridComponent, LegendComponent } from "echarts/components";
import { SVGRenderer } from "echarts/renderers";
import type { ReadingVisual } from "@guizhi/shared/types/reading-visuals";

echarts.use([BarChart, LineChart, PieChart, GridComponent, LegendComponent, SVGRenderer]);
const colors = ["#4477aa", "#228866", "#aa6677", "#997733", "#8866aa", "#337788"];
mermaid.initialize({ startOnLoad: false, securityLevel: "strict", htmlLabels: false, theme: "base", fontFamily: "Arial, sans-serif", maxTextSize: 20000, maxEdges: 200, flowchart: { htmlLabels: false, useMaxWidth: false }, themeVariables: { primaryColor: "#e5eef8", primaryTextColor: "#182536", primaryBorderColor: "#4477aa", lineColor: "#4477aa", secondaryColor: "#e5eef8", tertiaryColor: "#e5eef8", mainBkg: "#e5eef8", textColor: "#182536", nodeBorder: "#4477aa" } });
declare global { interface Window { readingGraphics: { onJob(callback: (job: { id: string; visual: ReadingVisual }) => void): void; complete(result: unknown): void }; } }
window.readingGraphics.onJob(async job => {
  try {
    const v = job.visual;
    let svg: string;
    if (v.kind === "mermaid") svg = (await mermaid.render("diagram", v.source)).svg;
    else {
      const c = v.chart;
      const pie = c.type === "pie" || c.type === "donut";
      const chart = echarts.init(null, null, { renderer: "svg", ssr: true, width: 760, height: 420 });
      try {
        chart.setOption({ backgroundColor: "transparent", color: colors, animation: true, animationDuration: 1000, animationEasing: "cubicOut", textStyle: { color: "#182536", fontFamily: "Arial, sans-serif", fontSize: 16 }, legend: { top: 0, textStyle: { color: "#182536", fontSize: 14 } }, grid: { left: 70, right: 40, top: 70, bottom: 85, containLabel: true }, ...(pie ? {} : { xAxis: { type: "category", data: c.categories, axisLabel: { color: "#182536", width: 100, overflow: "truncate" } }, yAxis: { type: "value", name: c.unit, axisLabel: { color: "#182536" } } }), series: c.series.map(s => ({ name: s.name, type: pie ? "pie" : c.type === "bar" ? "bar" : "line", ...(pie ? { radius: c.type === "donut" ? ["35%", "65%"] : "65%", data: c.categories.map((name, i) => ({ name, value: s.values[i] })), label: { color: "#182536", fontSize: 14 } } : { data: s.values, ...(c.type === "area" ? { areaStyle: { opacity: 0.2 } } : {}) }) })) });
        svg = chart.renderToSVGString();
      } finally { chart.dispose(); }
    }
    window.readingGraphics.complete({ id: job.id, svg });
  } catch (error) { window.readingGraphics.complete({ id: job.id, error: String(error instanceof Error ? error.message : error).slice(0, 1000) }); }
});
