/** 只重绘已保存的真实模型结果，绝不发送模型请求。 */
import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

async function main() {
  const directory = path.resolve("../../artifacts/themed-reading/composition-live");
  const data = JSON.parse(await fs.readFile(path.join(directory, "fixtures.json"), "utf8"));
  for (const fixture of data.fixtures) {
    if (!fixture.success) continue;
    const urls: Record<string, string> = {};
    const doc = parseHTML(await fs.readFile(fixture.files.offline, "utf8")).document;
    for (const image of doc.querySelectorAll("img")) {
      const uri = image.getAttribute("src"), hash = createHash("sha256").update(Buffer.from(uri.split(",")[1], "base64")).digest("hex");
      const asset = fixture.page.assets.find(asset => asset.sha256 === hash); if (asset) urls[asset.id] = uri;
    }
    for (const mode of ["offline", "embedded"]) await fs.writeFile(fixture.files[mode], themedReadingDocument(fixture.page, mode === "embedded" ? "composition-fixture" : undefined, urls));
  }
  process.stdout.write("已按最新渲染器重绘保存结果，模型调用 0 次\n");
}
void main();
