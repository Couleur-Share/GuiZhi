/** 显式文本模型验收：只读当前模型配置，原文来自隔离夹具，最多两篇各两次文本请求。 */
import fs from "node:fs/promises";
import path from "node:path";
import { parseHTML } from "linkedom";
import { configureRuntimePaths } from "@guizhi/core";
import { getUserDataPath } from "../src/main/runtime-paths";
import { resolveMediaSummaryConfig } from "../src/main/services/media/media-summary";
import { designChapter } from "../src/main/services/themed-reading/design";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";

async function main() {
  if (!process.argv.includes("--run")) throw new Error("传入 --run 才会调用文本模型");
  configureRuntimePaths({ userDataPath: getUserDataPath() });
  const config = resolveMediaSummaryConfig();
  if (!config) throw new Error("当前未配置主文本模型");
  const directory = path.resolve("../../artifacts/themed-reading/composition-live");
  await fs.mkdir(directory, { recursive: true });
  await fs.writeFile(path.join(directory, "attempt.json"), JSON.stringify({ at: new Date().toISOString(), textRequestLimit: 4, imageRequestLimit: 0, model: config.model }), { flag: "wx" });
  const data = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/composition/fixtures.json"), "utf8"));
  const results = [];
  let calls = 0;
  for (const fixture of data.fixtures) {
    const page = structuredClone(fixture.page) as ThemedReadingVersion;
    const urls: Record<string, string> = {};
    const doc = parseHTML(await fs.readFile(fixture.files.offline, "utf8")).document;
    // 素材按自身已验证的摘要恢复映射，原文中也可能复用同一图片。
    const { createHash } = await import("node:crypto");
    for (const image of doc.querySelectorAll("img")) {
      const uri = image.getAttribute("src"), hash = createHash("sha256").update(Buffer.from(uri.split(",")[1], "base64")).digest("hex");
      const asset = page.assets.find(asset => asset.sha256 === hash); if (asset) urls[asset.id] = uri;
    }
    page.design = null; page.designParts = [];
    page.options.generateImages = false;
    page.options.style = "按参考专题网页的完成度设计：衬线封面标题、提要与插画、明确的概念导航、参数卡、对照矩阵和适用的交互工具。内容以原文为依据，保留社群反馈与原文观点的归属。";
    page.designDirection = fixture.id === "fish-oil" ? "marine海蓝专题。先呈现体验分歧，再做食物与补剂比较、品牌参数、判断条件，按需加入成本或成分换算。" : "amber暖色专题。首屏导航分别对应杀菌工艺、新鲜程度、酿造主体三个独立维度。包含生熟对照、原图解释、判断清单与概念探索。";
    const started = Date.now();
    const onRequest = () => {
      if (++calls > 4) throw new Error("已达到本次文本请求上限");
      process.stdout.write(`${JSON.stringify({ id: fixture.id, state: "request", call: calls, model: config.model })}\n`);
    };
    try {
      page.design = await designChapter(page, page.source.blocks, 0, config, AbortSignal.timeout(390000), onRequest);
      if (!page.design.composition) throw new Error("模型未返回专题结构");
      const files: Record<string, string> = {};
      for (const mode of ["offline", "embedded"]) { files[mode] = path.join(directory, `${fixture.id}-${mode}.html`); await fs.writeFile(files[mode], themedReadingDocument(page, mode === "embedded" ? "composition-fixture" : undefined, urls)); }
      results.push({ id: fixture.id, title: page.source.title, page, files, success: true, elapsedMs: Date.now() - started });
      process.stdout.write(`${JSON.stringify({ id: fixture.id, state: "complete", sections: page.design.composition.chapters[0].sections.length, elapsedMs: Date.now() - started })}\n`);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      // 配置不写日志；错误中的地址与授权字段不进入产物。
      const safe = message.replace(/https?:\/\/\S+/g, "[模型服务]").replace(/(?:sk-|Bearer\s+)[\w-]+/g, "[redacted]");
      results.push({ id: fixture.id, success: false, error: safe });
      process.stdout.write(`${JSON.stringify({ id: fixture.id, state: "failed", error: safe })}\n`);
    }
    await fs.writeFile(path.join(directory, "fixtures.json"), JSON.stringify({ model: config.model, modelCalls: calls, imageCalls: 0, fixtures: results }, null, 2));
  }
  if (results.some(result => !result.success)) process.exitCode = 1;
}
void main();
