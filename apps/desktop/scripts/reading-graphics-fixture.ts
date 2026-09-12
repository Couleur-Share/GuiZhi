import { ensureRequestedReadingImage } from "../src/main/services/themed-reading/required-image";
import fs from "node:fs/promises";
import { writeFileSync } from "node:fs";
import path from "node:path";
import { reconstructionFixture } from "../tests/unit/reading-reconstruction-fixture";
import { compileReadingVisuals, renderReadingGraphic } from "../src/main/services/themed-reading/visual-compiler";
import { reconstructionDocument } from "../src/main/services/themed-reading/reconstruction-document";
import type { ReadingVisual } from "@guizhi/shared/types/reading-visuals";
import { runReconstruction } from "../src/main/services/themed-reading/reconstruction-pipeline";
import type { AIClientConfig } from "@guizhi/core";
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

/** 仅隔离验收构建可调用；读取模型配置，不复制配置或连接正式数据库。 */
export async function createLiveGraphicsFixture(directory: string, configPath: string) {
  const saved = JSON.parse(await fs.readFile(configPath, "utf8"));
  const models = saved.models.filter((m: { capabilities?: { chat?: boolean } }) => m.capabilities?.chat !== false);
  const model = models.find((m: { id: string }) => m.id === saved.modelRouteDefaults?.mainText) ?? models.find((m: { isDefault?: boolean }) => m.isDefault) ?? models[0];
  if (!model?.apiKey || !model?.apiUrl || !model?.model) throw new Error("当前文本模型配置不可用");
  const config: AIClientConfig = { provider: model.provider, apiProtocol: model.apiProtocol, apiKey: model.apiKey, apiUrl: model.apiUrl, model: model.model };
  const page = reconstructionFixture(); page.id = "live-graphics"; page.design = null; page.designDirection = undefined;
  page.source.title = "把阅读材料变成可复用知识";
  page.source.content = "个人知识整理分为采集、理解、整理和复用。采集时保留来源，理解时提取问题与关键证据，整理时建立明确标题和关系，复用时根据具体问题检索并应用。阶段之间允许返回：发现缺失的依据时应回到采集，而不是编造细节。流程图适合展示阶段及返回路径，时间轴可以描述一次整理过程。下面是用于讲解图表的虚构课堂数据，不代表真实用户统计：甲为10个，乙为20个，丙为15个。这三个数值表示三次练习中整理的条目数，仅用于比较练习规模，不能推断学习效果。动画可以展示信息从采集移动到整理，再进入复用；不应持续闪烁或遮挡正文。普通阅读仍应以连贯文字、清晰标题和舒适行距为主，必要时使用图解帮助理解。";
  page.source.blocks = [{ id: "b0", markdown: page.source.content, html: `<p>${page.source.content}</p>`, text: page.source.content }];
  page.reconstruction = { notes: [], queries: [], references: [], draft: [], interactions: [] };
  page.options = { action: "create", style: "克制的知识专题：用时间轴或步骤组件、一个Mermaid流程图、一张明确标注虚构课堂数据的柱状图，以及一个安全SVG描线动画帮助理解。所有重要文字保持静态可读。无需图片。", research: false, generateImages: false, maxImages: 0 };
  const savedFixture = process.env.GUIZHI_READING_PAGE_FIXTURE;
  if (savedFixture) {
    const savedPage = JSON.parse(await fs.readFile(savedFixture, "utf8"));
    Object.assign(page, savedPage, { design: null, designDirection: undefined });
    page.options = { ...savedPage.options, action: "redesign", research: false, generateImages: false, maxImages: 0 };
    page.reconstruction.visuals = []; page.reconstruction.animations = []; page.reconstruction.interactions = [];
  }
  const started = performance.now();
  let calls = 0, receivedChars = 0, lastProgress = 0;
  let stage = "pending";
  await fs.mkdir(directory, { recursive: true });
  const checkpoint = () => {
    // 仅本次验收材料，保留已完成阶段；绝不序列化模型配置。
    writeFileSync(path.join(directory, "live-checkpoint.json"), JSON.stringify(page, null, 2));
    writeFileSync(path.join(directory, "live-progress.json"), JSON.stringify({ stage, calls, receivedChars, elapsedMs: performance.now() - started }, null, 2));
  };
  try {
    await runReconstruction(page, config, AbortSignal.timeout(900000), { checkpoint, stage: value => { stage = value; checkpoint(); }, designProgress: (_attempt, count) => { receivedChars = count; if (performance.now() - lastProgress > 1000) { lastProgress = performance.now(); checkpoint(); } }, request: kind => { if (kind !== "textCalls" || ++calls > (savedFixture ? 2 : 12)) throw new Error("超过本轮十二次文本请求上限"); } });
  } catch (error) {
    const failure = { success: false, model: config.model, calls, stage, error: error instanceof Error ? error.message : "生成失败", cause: (error as { cause?: { code?: string; message?: string } }).cause?.code ?? (error as { cause?: { message?: string } }).cause?.message };
    await fs.writeFile(path.join(directory, "live-evidence.json"), JSON.stringify(failure, null, 2));
    return failure;
  }
  await compileReadingVisuals(page, AbortSignal.timeout(90000), checkpoint, () => {});
  await fs.writeFile(path.join(directory, "live-page.json"), JSON.stringify(page, null, 2));
  await fs.writeFile(path.join(directory, "live-offline.html"), reconstructionDocument(page));
  const result = { model: config.model, calls, webVerified: false, syntheticSource: !savedFixture, elapsedMs: performance.now() - started, receivedChars, htmlChars: page.design.html.length, cssChars: page.design.css.length, visuals: page.reconstruction.visuals?.map(v => ({ id: v.id, kind: v.kind })), animations: page.reconstruction.animations?.length ?? 0, errors: page.design.visualResults?.filter(r => r.status === "failed").map(r => ({ id: r.id, error: r.error })) ?? [] };
  await fs.writeFile(path.join(directory, "live-evidence.json"), JSON.stringify(result, null, 2));
  return result;
}
