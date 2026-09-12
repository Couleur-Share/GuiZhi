import type { ReadingAnimation, ReadingVisual, ReadingVisualResult } from "../types/reading-visuals";

const id = (v: unknown): v is string => typeof v === "string" && /^[a-zA-Z][\w-]{0,79}$/.test(v) && !v.startsWith("gz-system");
const text = (v: unknown, max: number): v is string => typeof v === "string" && v.trim().length > 0 && v.length <= max;
function fail(message: string): never { throw new Error(`阅读图形：${message}`); }
export function validateReadingVisuals(visuals: ReadingVisual[] = [], animations: ReadingAnimation[] = [], draft: { markdown: string }[] = []): void {
  if (!Array.isArray(visuals) || visuals.length > 8 || !Array.isArray(animations) || animations.length > 40) fail("定义数量超限");
  const ids = new Set<string>();
  for (const v of visuals) {
    if (!v || !id(v.id) || ids.has(v.id) || !["mermaid", "chart", "svg"].includes(v.kind) || !text(v.title, 200) || !text(v.description, 2000)) fail("图形标识或说明无效");
    ids.add(v.id);
    if (v.kind === "mermaid") {
      if (!text(v.source, 20000) || !/^(flowchart\s+(?:TD|TB|LR|RL|BT)|sequenceDiagram\b|stateDiagram-v2\b|mindmap\b)/.test(v.source.trim())) fail("仅支持流程、时序、状态与思维导图");
      if (/%%\{|^\s*---|(?:^|;)\s*(?:click|classDef|style|linkStyle)\s|https?:|javascript:|data:|<|>|@\{/im.test(v.source.replace(/<-->|-->>|->>|-->|==>|--\)|-\)|<--/g, ""))) fail("图解不能覆盖配置、样式或加载资源");
    }
    if (v.kind === "chart") {
      const c = v.chart;
      if (!c || !["bar", "line", "area", "pie", "donut"].includes(c.type) || !Array.isArray(c.categories) || !c.categories.length || c.categories.length > 200 || c.categories.some(x => !text(x, 160)) || !Array.isArray(c.series) || !c.series.length || c.series.length > 6 || c.series.length * c.categories.length > 200 || typeof c.unit !== "string" || c.unit.length > 40) fail("图表数据结构无效");
      if ((c.type === "pie" || c.type === "donut") && c.series.length !== 1) fail("占比图只接受一个系列");
      for (const s of c.series) if (!s || !text(s.name, 100) || !Array.isArray(s.values) || s.values.length !== c.categories.length || s.values.some(n => typeof n !== "number" || !Number.isFinite(n) || Math.abs(n) > 1e12 || ((c.type === "pie" || c.type === "donut") && n < 0))) fail("图表数值无效");
      if ((c.type === "pie" || c.type === "donut") && !c.series[0].values.some(n => n > 0)) fail("占比图不能全部为零");
      if (!c.evidence || !Number.isInteger(c.evidence.section) || !text(c.evidence.quote, 4000) || !draft[c.evidence.section]?.markdown.includes(c.evidence.quote)) fail("图表必须引用现有编辑稿中的数据依据");
      const quotedNumbers = new Set((c.evidence.quote.replace(/−/g, "-").match(/[+-]?\d+(?:,\d{3})*(?:\.\d+)?(?:e[+-]?\d+)?/gi) ?? []).map(n => Number(n.replace(/,/g, ""))));
      if (c.series.some(s => s.values.some(n => !quotedNumbers.has(n)))) fail("图表数字必须直接出现在引用的编辑稿原句中");
    }
  }
  const totals = new Map<string, number>(), targets = new Set<string>();
  for (const a of animations) {
    const v = visuals.find(v => v.id === a?.visualId);
    if (!v || v.kind !== "svg" || !["draw", "reveal", "motion", "morph"].includes(a.preset) || !id(a.targetId) || (["motion", "morph"].includes(a.preset) && !id(a.pathId))) fail("动画只能指向当前自定义 SVG 的已声明目标");
    if ((a.duration !== undefined && (!Number.isFinite(a.duration) || a.duration < 400 || a.duration > 1600)) || (a.order !== undefined && (!Number.isInteger(a.order) || a.order < 0 || a.order > 39))) fail("动画时长或顺序无效");
    const key = `${a.visualId}:${a.targetId}`;
    if (targets.has(key)) fail("同一元素不能同时使用多个动画");
    targets.add(key);
    const total = (totals.get(a.visualId) ?? 0) + (a.duration ?? 1000);
    totals.set(a.visualId, total);
    if (total > 8000) fail("单段动画超过八秒");
  }
}
export function validateReadingVisualResults(results: ReadingVisualResult[] = []): void {
  if (!Array.isArray(results) || results.length > 8) fail("编译结果数量超限");
  const ids = new Set<string>();
  for (const r of results) {
    if (!r || !id(r.id) || ids.has(r.id) || !/^[a-f0-9]{64}$/.test(r.sourceHash) || !text(r.compilerVersion, 100) || !["ready", "failed"].includes(r.status)) fail("编译记录无效");
    ids.add(r.id);
    if (r.status === "ready" && (!text(r.svg, 2000000) || typeof r.css !== "string" || r.css.length > 300000)) fail("编译图形无效");
    if (r.status === "failed" && !text(r.error, 1000)) fail("编译失败缺少原因");
  }
}
