/** 模型只描述图形与动画；编译结果由主进程保存。 */
export interface ReadingChart {
  type: "bar" | "line" | "area" | "pie" | "donut";
  categories: string[];
  series: { name: string; values: number[] }[];
  unit: string;
  evidence: { section: number; quote: string };
}
export interface ReadingVisual {
  id: string;
  kind: "mermaid" | "chart" | "svg";
  title: string;
  description: string;
  source?: string;
  chart?: ReadingChart;
}
export interface ReadingAnimation {
  visualId: string;
  preset: "draw" | "reveal" | "motion" | "morph";
  targetId: string;
  pathId?: string;
  duration?: number;
  order?: number;
}
export interface ReadingVisualResult {
  id: string;
  sourceHash: string;
  compilerVersion: string;
  status: "ready" | "failed";
  svg?: string;
  css?: string;
  error?: string;
}
