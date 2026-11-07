import { ensureRequestedReadingImage } from "../src/main/services/themed-reading/required-image";
import fs from "node:fs/promises";
import path from "node:path";
import { reconstructionFixture } from "../tests/unit/reading-reconstruction-fixture";
import { compileReadingVisuals, renderReadingGraphic } from "../src/main/services/themed-reading/visual-compiler";
import { reconstructionDocument } from "../src/main/services/themed-reading/reconstruction-document";
import type { ReadingVisual } from "@guizhi/shared/types/reading-visuals";
import * as css from "css-tree";

export async function createGraphicsFixtures(directory: string) {
  const page = reconstructionFixture(); page.id = "graphics-fixture";
  page.reconstruction.draft[0].markdown = "甲为10个，乙为20个，丙为15个。";
  const visuals: ReadingVisual[] = [
    { id: "flow", kind: "mermaid", title: "知识形成过程", description: "采集资料、分析内容，再形成可复用的知识。", source: "flowchart TD\nA[采集资料] --> B[分析内容]\nB --> C[形成知识]" },
    { id: "sequence", kind: "mermaid", title: "请求与响应", description: "用户提出问题，服务返回整理后的结果。", source: "sequenceDiagram\nparticipant U as 用户\nparticipant S as 服务\nU->>S: 提出问题\nS-->>U: 返回结果" },
    { id: "state", kind: "mermaid", title: "任务状态", description: "任务从等待转为执行，执行后完成。", source: "stateDiagram-v2\n等待 --> 执行\n执行 --> 完成" },
    { id: "mind", kind: "mermaid", title: "阅读与知识", description: "阅读包含理解和记忆两个相互关联的部分。", source: "mindmap\n  root((阅读))\n    理解\n    记忆" },
    { id: "chart", kind: "chart", title: "样例数据对比", description: "隔离测试数据：甲10个、乙20个、丙15个。", chart: { type: "bar", categories: ["甲", "乙", "丙"], series: [{ name: "数量", values: [10, 20, 15] }], unit: "个", evidence: { section: 0, quote: "甲为10个，乙为20个，丙为15个。" } } },
    { id: "custom", kind: "svg", title: "沿线理解过程", description: "从开始沿路径移动到结果。" },
  ];
  page.reconstruction.visualVersion = 1; page.reconstruction.visuals = visuals;
  page.reconstruction.animations = [{ visualId: "custom", preset: "draw", targetId: "custom-path", duration: 1600 }, { visualId: "custom", preset: "motion", targetId: "dot", pathId: "custom-track", duration: 1600 }, { visualId: "custom", preset: "reveal", targetId: "box", duration: 1000 }, { visualId: "custom", preset: "morph", targetId: "shape", pathId: "shape-end", duration: 1000 }];
  page.design.html += '<section><h2>复用的阅读组件</h2><aside class="gz-ui-alert">只有帮助理解的信息才需要视觉强调。</aside><ol class="gz-ui-steps gz-ui-steps-vertical"><li class="gz-ui-step">阅读</li><li class="gz-ui-step">理解</li></ol></section>';
  for (const v of visuals) page.design.html += `<section><h2>${v.title}</h2><figure data-reading-visual="${v.id}">${v.kind === "svg" ? '<svg viewBox="0 0 700 220"><path id="custom-path" d="M40 80L600 80" fill="none" stroke="var(--theme-visual-1)" stroke-width="4"/><path id="custom-track" d="M40 80L600 80" fill="none" stroke="none"/><circle id="dot" cx="0" cy="0" r="9" fill="var(--theme-visual-2)"/><rect id="box" x="550" y="120" width="80" height="40" fill="var(--theme-visual-3)"/><path id="shape" d="M40 130L90 130L90 180L40 180Z" fill="var(--theme-visual-4)"/><path id="shape-end" d="M65 120L100 180L30 180Z" fill="none"/><text x="40" y="60">开始</text><text x="560" y="60">结果</text></svg>' : ''}<figcaption>${v.description}</figcaption></figure></section>`;
  await fs.mkdir(directory, { recursive: true });
  if (process.env.GUIZHI_REQUIRED_IMAGE_VALIDATION === "1") {
    const required = reconstructionFixture(); required.options.generateImages = true; required.options.maxImages = 1;
    required.assets = [{id:"requested",role:"generated",purpose:"验证补图展示",prompt:"测试替身",alt:"测试占位图片（应用图标，非真实生图）",aspectRatio:"16:9",status:"ready",fileName:"fixture.png",bytes:1,sha256:"a".repeat(64)}];
    ensureRequestedReadingImage(required);
    const icon = await fs.readFile(path.join(__dirname, "../../resources/icon.png"));
    await fs.writeFile(path.join(directory, "required-image.html"), reconstructionDocument(required, undefined, {requested:`data:image/png;base64,${icon.toString("base64")}`}));
  }
  if (process.env.GUIZHI_READING_PAGE_FIXTURE) {
    const saved = JSON.parse(await fs.readFile(process.env.GUIZHI_READING_PAGE_FIXTURE, "utf8"));
    await fs.writeFile(path.join(directory, "user-page.html"), reconstructionDocument(saved));
  }
  const started = performance.now();
  await compileReadingVisuals(page, AbortSignal.timeout(90000), () => {}, () => {}, async (v, signal) => {
    const raw = await renderReadingGraphic(v, signal); await fs.writeFile(path.join(directory, `raw-${v.id}.svg`), raw); return raw;
  });
  const compilationMs = performance.now() - started;
  await fs.writeFile(path.join(directory, "page.json"), JSON.stringify(page, null, 2));
  const errors = page.design.visualResults.filter(r => r.status === "failed");
  if (errors.length) return { success: false, errors, compilationMs };
  const offline = reconstructionDocument(page), embedded = reconstructionDocument(page, "graphics-fixture");
  await fs.writeFile(path.join(directory, "offline.html"), offline); await fs.writeFile(path.join(directory, "embedded.html"), embedded);
  const extraCharts = [];
  for (const type of ["line", "area", "pie", "donut"] as const) {
    const extra = reconstructionFixture(), v = structuredClone(visuals.find(v => v.kind === "chart")); v.chart.type = type;
    extra.reconstruction.draft = page.reconstruction.draft; extra.reconstruction.visuals = [v];
    extra.design.html += `<figure data-reading-visual="${v.id}"><figcaption>${v.description}</figcaption></figure>`;
    await compileReadingVisuals(extra, AbortSignal.timeout(15000), () => {}, () => {});
    const compiled = extra.design.visualResults[0];
    extraCharts.push({ type, status: compiled.status, error: compiled.error });
    await fs.writeFile(path.join(directory, `chart-${type}.html`), reconstructionDocument(extra));
  }
  const plain = reconstructionFixture(), components = reconstructionFixture(), diagrams = structuredClone(page);
  components.design.html += '<aside class="gz-ui-alert">只在必要时强调内容</aside><ol class="gz-ui-steps gz-ui-steps-vertical"><li class="gz-ui-step">理解</li><li class="gz-ui-step">复用</li></ol>';
  diagrams.reconstruction.animations = [];
  for (const result of diagrams.design.visualResults) {
    const tree = css.parse(result.css);
    css.walk(tree, (node, item, list) => { if ((node.type === "Atrule" && node.name === "keyframes") || (node.type === "Declaration" && node.property.startsWith("animation"))) list.remove(item); });
    result.css = css.generate(tree);
  }
  const measurements = [];
  for (const [name, variant] of [["plain", plain], ["components", components], ["diagrams", diagrams], ["animated", page]] as const) {
    const start = performance.now(), html = reconstructionDocument(variant), assemblyMs = performance.now() - start;
    measurements.push({ name, bytes: Buffer.byteLength(html), assemblyMs, heapUsedSample: process.memoryUsage().heapUsed, animationRuntime: html.includes('id="gz-system-motion-data"') });
    await fs.writeFile(path.join(directory, `variant-${name}.html`), html);
  }
  await fs.writeFile(path.join(directory, "performance.json"), JSON.stringify({ compilationMs, note: "单次隔离进程样本，heapUsed 为采样值而非峰值，非跨机器基准", measurements }, null, 2));
  return { success: extraCharts.every(r => r.status === "ready"), compilationMs, extraCharts, bytes: { offline: Buffer.byteLength(offline), embedded: Buffer.byteLength(embedded) }, visuals: page.design.visualResults.map(r => ({ id: r.id, bytes: r.svg.length })) };
}
