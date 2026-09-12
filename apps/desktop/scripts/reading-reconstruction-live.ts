/** 显式离线模型验收：无搜索密钥时不冒充联网验证。只操作隔离产物。 */
import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import { configureRuntimePaths } from "@guizhi/core";
import { getUserDataPath } from "../src/main/runtime-paths";
import { resolveMediaSummaryConfig } from "../src/main/services/media/media-summary";
import { runReconstruction } from "../src/main/services/themed-reading/reconstruction-pipeline";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";

async function main() {
  if (!process.argv.includes("--run-offline")) throw new Error("必须明确传入 --run-offline；此脚本不验证联网");
  configureRuntimePaths({ userDataPath: getUserDataPath() });
  const config = resolveMediaSummaryConfig(); if (!config) throw new Error("文本模型未配置");
  const directory = path.resolve("../../artifacts/themed-reading/reconstruction-live"); await fs.mkdir(directory, { recursive: true });
  await fs.writeFile(path.join(directory, "attempt.json"), JSON.stringify({ offline: true, model: config.model, maxTextRequests: 36, date: new Date().toISOString() }), { flag: "wx" });
  const input = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/composition/fixtures.json"), "utf8"));
  const results: unknown[] = []; let calls = 0;
  for (const fixture of input.fixtures) {
    const page = structuredClone(fixture.page) as ThemedReadingVersion;
    const urls: Record<string, string> = {};
    for (const image of parseHTML(await fs.readFile(fixture.files.offline, "utf8")).document.querySelectorAll("img")) {
      const uri = image.getAttribute("src"); const hash = createHash("sha256").update(Buffer.from(uri.split(",")[1], "base64")).digest("hex");
      const asset = page.assets.find(a => a.sha256 === hash); if (asset) urls[asset.id] = uri;
    }
    page.formatVersion = 2; page.sourceKind = fixture.id === "fish-oil" ? "summary" : "body";
    page.reconstruction = { notes: [], queries: [], references: [], draft: [], interactions: [] };
    page.design = null; delete page.designParts; delete page.designDirection;
    page.options = { action: "create", research: false, generateImages: false, maxImages: 0, style: fixture.id === "fish-oil" ? "海蓝与奶油色的编辑专题，衬线标题，突出体验、食物替代、品牌差异和成本换算。可以提炼扩写，自由设计完整HTML；参考高品质科普杂志。加入有用的成本工具。" : "琥珀色与奶油色的啤酒专题，衬线标题，三个维度的概念图解、对比和选择式解释。可以提炼扩写，依据内容自由设计完整HTML；参考高品质知识杂志。" };
    try {
      await runReconstruction(page, config, AbortSignal.timeout(1500000), {
        checkpoint: () => { /* 脚本写文件检查点由阶段输出配合最终结果记录，不访问正式库。 */ },
        stage: (stage, done, total) => process.stdout.write(JSON.stringify({ id: fixture.id, stage, done, total }) + "\n"),
        request: kind => { if (kind !== "textCalls") throw new Error("离线验收不能发出搜索请求"); if (++calls > 36) throw new Error("超过本次请求上限"); process.stdout.write(JSON.stringify({ id: fixture.id, request: calls }) + "\n"); },
      });
      const files = { offline: path.join(directory, `${fixture.id}-offline.html`), embedded: path.join(directory, `${fixture.id}-embedded.html`) };
      await fs.writeFile(files.offline, themedReadingDocument(page, undefined, urls));
      await fs.writeFile(files.embedded, themedReadingDocument(page, "reconstruction-fixture", urls));
      results.push({ id: fixture.id, title: fixture.title, success: true, page, files });
    } catch (e) { results.push({ id: fixture.id, success: false, page, error: e instanceof Error ? e.message : String(e) }); }
    await fs.writeFile(path.join(directory, "fixtures.json"), JSON.stringify({ offline: true, webVerified: false, model: config.model, modelCalls: calls, fixtures: results }, null, 2));
  }
}
void main();
