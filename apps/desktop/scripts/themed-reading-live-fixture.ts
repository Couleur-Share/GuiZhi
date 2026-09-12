/** 保存的真实 AI 布局离屏验收夹具；只读独立验收目录，不调用模型。 */
import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

async function main() {
  const directory = path.resolve("../../artifacts/themed-reading/live-2026-09-07T07-11-46-747Z");
  const page: ThemedReadingVersion = JSON.parse(await fs.readFile(path.join(directory, "page-resume-2026-09-07T07-28-33-985Z.json"), "utf8"));
  const assetsDirectory = path.join(directory, "data/assets/images");
  for (const asset of page.assets.filter((entry) => entry.status === "ready")) {
    if (!asset.fileName || !/^[\w.-]+\.(png|jpe?g|webp|gif)$/.test(asset.fileName) || asset.fileName.includes("..")) throw new Error("夹具图片路径不合法");
    const bytes = await fs.readFile(path.join(assetsDirectory, asset.fileName));
    if (asset.sha256 && createHash("sha256").update(bytes).digest("hex") !== asset.sha256) throw new Error(`夹具图片校验失败：${asset.id}`);
  }
  const output = path.resolve("../../artifacts/themed-reading/live-layout-fixture.json");
  await fs.writeFile(output, JSON.stringify({ page, document: themedReadingDocument(page, "live-layout-fixture"), assetsDirectory }, null, 2), "utf8");
  process.stdout.write(`${output}\n`);
}
void main();
