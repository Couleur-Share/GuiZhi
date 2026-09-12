/** 显式运行的真实模型验收；用户库只读，所有结果写入隔离目录，不复制模型凭证。 */
import fs from "node:fs/promises";
import path from "node:path";
import { randomUUID } from "node:crypto";
import Database from "@guizhi/db/adapter";
import { KnowledgeItemDB } from "@guizhi/db";
import { ThemedReadingDB, validateThemedReadingVersion } from "@guizhi/db/themed-reading";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { configureRuntimePaths as configureCore } from "@guizhi/core";
import type { ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";
import { getUserDataPath, configureRuntimePaths } from "../src/main/runtime-paths";
import { ThemedReadingRuntime } from "../src/main/services/themed-reading/runtime";
import { exportThemedReadingHtml } from "../src/main/services/themed-reading/export";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";
import { readThemeAsset } from "../src/main/services/themed-reading/assets";
import { escapeThemedText } from "../src/main/services/themed-reading/content";
import { isSafeAssetFileName } from "@guizhi/shared/utils/media-refs";

const artifactsRoot = path.resolve(__dirname, "../../../artifacts/themed-reading");
const stamp = () => new Date().toISOString().replace(/[:.]/g, "-");
const describe = (error: unknown) => error instanceof Error ? error.message : String(error);

async function isolatedDirectory(value: string): Promise<string> {
  const directory = await fs.realpath(path.resolve(value));
  const base = await fs.realpath(artifactsRoot);
  if (path.dirname(directory).toLowerCase() !== base.toLowerCase() || !/^live-\d{4}-/.test(path.basename(directory))) {
    throw new Error("仅接受artifacts/themed-reading/live-日期隔离目录，未调用模型");
  }
  return directory;
}

/** 验收预览允许缺图但保留所有正文和现有图片；不改变正式完整导出的缺图拒绝策略。 */
async function writePreview(version: ThemedReadingVersion, directory: string, label: string) {
  validateThemedReadingVersion(version);
  const view = structuredClone(version), urls: Record<string, string> = {}, failures: string[] = [];
  let originalImages = 0, generatedImages = 0;
  for (const asset of view.assets) {
    try {
      if (asset.status !== "ready") throw new Error(asset.error || "图片尚未完成");
      const image = await readThemeAsset(asset);
      urls[asset.id] = `data:${image.mime};base64,${image.data.toString("base64")}`;
      if (asset.role === "original") originalImages++; else generatedImages++;
    } catch (error) {
      asset.status = "failed"; asset.error = describe(error);
      failures.push(`${asset.alt || asset.purpose}：${asset.error}`);
    }
  }
  let html = themedReadingDocument(view, undefined, urls);
  if (failures.length) {
    const warning = `<aside role="status" style="padding:16px;margin:16px;border:1px solid #986000;border-radius:12px;background:#fff3d6;color:#593900;font:15px/1.6 system-ui"><strong>验收预览：部分图片未完成</strong><p>以下保留完整正文、${originalImages} 张原图和 ${generatedImages} 张成功生成图。缺图未替换为虚构结果。</p><p>${failures.map(escapeThemedText).join("<br>")}</p></aside>`;
    html = html.replace("<body>", `<body>${warning}`);
  }
  const file = path.join(directory, `preview-${label}.html`);
  await fs.writeFile(file, html, { flag: "wx" });
  const report = { file, originalImages, generatedImages, failures, sourceBlocks: view.source.blocks.length,
    sourceCharacters: view.source.content.length, bytes: Buffer.byteLength(html), selfContained: true, createdAt: new Date().toISOString() };
  await fs.writeFile(path.join(directory, `preview-${label}.json`), JSON.stringify(report, null, 2), { flag: "wx" });
  process.stdout.write(`PROBE_PREVIEW=${file}\n${JSON.stringify(report)}\n`);
  return report;
}

async function saveResults(directory: string, label: string, runtime: ThemedReadingRuntime, task: ThemedReadingTask,
  startedAt: number, originalContent: string, events: unknown[], previousReport?: string) {
  const final = runtime.pages.getTask(task.id);
  const page = runtime.pages.getVersion(task.versionId) ?? runtime.pages.get(task.itemId, task.sourceKind);
  const report = { task: final, elapsedMs: Date.now() - startedAt,
    originalUnchanged: runtime.items.get(task.itemId)?.content === originalContent,
    textModel: page?.textModel, imageModel: page?.imageModel,
    imageCount: page?.assets.filter(asset => asset.role === "generated").length ?? 0,
    generatedReady: page?.assets.filter(asset => asset.role === "generated" && asset.status === "ready").length ?? 0,
    textDesignReused: Boolean(previousReport), additionalImageRequestsAuthorized: previousReport ? 1 : undefined,
    previousReport, events };
  const suffix = label === "initial" ? "" : `-${label}`;
  await fs.writeFile(path.join(directory, `report${suffix}.json`), JSON.stringify(report, null, 2), { flag: "wx" });
  if (page) {
    await fs.writeFile(path.join(directory, `page${suffix}.json`), JSON.stringify(page, null, 2), { flag: "wx" });
    await writePreview(page, directory, label);
    try { await fs.writeFile(path.join(directory, `reading${suffix}.html`), await exportThemedReadingHtml(page), { flag: "wx" }); }
    catch (error) { await fs.writeFile(path.join(directory, `export${suffix}.json`), JSON.stringify({ success: false, error: describe(error) }, null, 2), { flag: "wx" }); }
    await fs.writeFile(path.join(directory, `iframe${suffix}.html`), themedReadingDocument(page, randomUUID()), { flag: "wx" });
  }
  process.stdout.write(`PROBE_OUTPUT=${directory}\nPROBE_REPORT=${path.join(directory, `report${suffix}.json`)}\n`);
  if (!page || final?.state === "failed") process.exitCode = 1;
}

async function run(directory: string, input: { itemId: string; originalContent: string; retryAssetId?: string; label: string }) {
  configureRuntimePaths({ userDataPath: directory });
  const db = new Database(path.join(directory, "data/knowledge.db"));
  const startedAt = Date.now(), events: unknown[] = [];
  const runtime = new ThemedReadingRuntime(db, task => {
    const event = { state: task.state, stage: task.stage, completed: task.completed, total: task.total,
      elapsedMs: Date.now() - startedAt, error: task.error };
    events.push(event); process.stdout.write(`${JSON.stringify(event)}\n`);
  });
  try {
    const current = runtime.pages.get(input.itemId, "body");
    const task = await runtime.generate({ itemId: input.itemId, sourceKind: "body", options: input.retryAssetId ?
      { ...current!.options, fromCurrent: true, generateImages: false } : {
        style: "暖色啤酒专题杂志：琥珀色、麦穗与酒花主题。请制作一张有留白的宽幅主题插画融入顶部，可按需再补章节元素，全文完整且层级清晰。",
        generateImages: true, maxImages: 3,
      } }, input.retryAssetId);
    while (["queued", "running"].includes(runtime.pages.getTask(task.id)?.state ?? "")) await new Promise(resolve => setTimeout(resolve, 500));
    await saveResults(directory, input.label, runtime, task, startedAt, input.originalContent, events, input.retryAssetId ? "report.json" : undefined);
  } finally { await runtime.shutdown(); db.close(); }
}

async function main() {
  const [mode, location] = process.argv.slice(2);
  if (!["--new", "--render-only", "--resume"].includes(mode)) throw new Error("用法：--new | --render-only <隔离目录> | --resume <隔离目录>；不传模式不会调用模型");
  if (mode === "--render-only") {
    if (!location) throw new Error("缺少隔离目录");
    const directory = await isolatedDirectory(location);
    configureRuntimePaths({ userDataPath: directory });
    const page: unknown = JSON.parse(await fs.readFile(path.join(directory, "page.json"), "utf8"));
    validateThemedReadingVersion(page);
    await writePreview(page, directory, `saved-${stamp()}`);
    return;
  }
  const userData = getUserDataPath();
  configureCore({ userDataPath: userData });
  if (mode === "--resume") {
    if (!location) throw new Error("缺少隔离目录");
    const directory = await isolatedDirectory(location);
    const db = new Database(path.join(directory, "data/knowledge.db"), { readOnly: true });
    let itemId: string, originalContent: string, retryAssetId: string;
    try {
      const report = JSON.parse(await fs.readFile(path.join(directory, "report.json"), "utf8"));
      const page = new ThemedReadingDB(db).get(report.task.itemId, "body");
      if (!page?.design) throw new Error("没有可复用的完整设计，未调用模型");
      const failed = page.assets.filter(asset => asset.role === "generated" && asset.status !== "ready");
      if (failed.length !== 1 || page.assets.some(asset => asset.role === "original" && asset.status !== "ready")) {
        throw new Error("本次只允许对恰好一张失败生成图继续，必须复用全部成功原图");
      }
      itemId = page.itemId; retryAssetId = failed[0].id;
      originalContent = new KnowledgeItemDB(db).get(itemId)!.content;
    } finally { db.close(); }
    const label = `resume-${stamp()}`;
    // wx 为一次性请求预算锁；崩溃或结果未知也不能自动开始第三次请求。
    await fs.writeFile(path.join(directory, "resume-attempt.json"), JSON.stringify({ label, imageRequestsAuthorized: 1,
      priorImageRequests: 1, totalImageRequestLimit: 2, retryAssetId, createdAt: new Date().toISOString() }, null, 2), { flag: "wx" });
    await run(directory, { itemId, originalContent, retryAssetId, label });
    return;
  }
  const directory = path.join(artifactsRoot, `live-${stamp()}`);
  await fs.mkdir(path.join(directory, "data/assets/images"), { recursive: true });
  const sourceDb = new Database(path.join(userData, "data/knowledge.db"), { readOnly: true });
  let sourceItem: ReturnType<KnowledgeItemDB["get"]>;
  try {
    const row = sourceDb.get("SELECT id FROM knowledge_items WHERE deleted_at IS NULL AND title LIKE ? ORDER BY updated_at DESC LIMIT 1", "%生啤%熟啤%") as { id: string } | undefined;
    if (!row) throw new Error("用户库中没有找到指定的啤酒文章，未调用模型");
    sourceItem = new KnowledgeItemDB(sourceDb).get(row.id);
  } finally { sourceDb.close(); }
  if (!sourceItem) throw new Error("来源条目不存在");
  for (const file of new Set([...sourceItem.content.matchAll(/local-image:\/\/([\w.-]+)/g)].map(match => match[1]))) {
    if (!isSafeAssetFileName(file)) throw new Error("原图引用不合法");
    await fs.copyFile(path.join(userData, "data/assets/images", file), path.join(directory, "data/assets/images", file));
  }
  configureRuntimePaths({ userDataPath: directory });
  const db = new Database(path.join(directory, "data/knowledge.db")); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  let itemId: string;
  try { itemId = new KnowledgeItemDB(db).create({ title: sourceItem.title, content: sourceItem.content, itemType: sourceItem.itemType, sourceUri: sourceItem.sourceUri }).id; }
  finally { db.close(); }
  await run(directory, { itemId, originalContent: sourceItem.content, label: "initial" });
}
void main().catch(error => { process.stderr.write(`${describe(error)}\n`); process.exitCode = 1; });
