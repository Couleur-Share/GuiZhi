/** 恢复隔离真实验收的编辑稿，每篇最多两次设计请求，不改正式数据库。 */
import fs from "node:fs/promises";
import path from "node:path";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import { configureRuntimePaths } from "@guizhi/core";
import { getUserDataPath } from "../src/main/runtime-paths";
import { resolveMediaSummaryConfig } from "../src/main/services/media/media-summary";
import { runReconstruction } from "../src/main/services/themed-reading/reconstruction-pipeline";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

async function main() {
  const id = process.argv[2]; if (!["fish-oil", "beer"].includes(id)) throw new Error("须指定 fish-oil 或 beer");
  const suffix = process.argv.includes("--stream") ? "-stream" : "";
  configureRuntimePaths({ userDataPath: getUserDataPath() });
  const config = resolveMediaSummaryConfig(); if (!config) throw new Error("未配置文本模型");
  const directory = path.resolve("../../artifacts/themed-reading/reconstruction-live");
  const saved = JSON.parse(await fs.readFile(path.join(directory, "fixtures.json"), "utf8"));
  const fixture = saved.fixtures.find(f => f.id === id);
  if (!fixture || fixture.success || !fixture.page.reconstruction.draft.length) throw new Error("没有可恢复的失败稿");
  const original = JSON.parse(await fs.readFile(path.resolve("../../artifacts/themed-reading/composition/fixtures.json"), "utf8")).fixtures.find(f => f.id === id);
  const page = fixture.page, urls: Record<string,string> = {};
  for (const image of parseHTML(await fs.readFile(original.files.offline,"utf8")).document.querySelectorAll("img")) {
    const uri = image.getAttribute("src"), hash = createHash("sha256").update(Buffer.from(uri.split(",")[1],"base64")).digest("hex");
    const asset = page.assets.find(a => a.sha256 === hash); if (asset) urls[asset.id] = uri;
  }
  await fs.writeFile(path.join(directory, `${id}-resume${suffix}-attempt.json`), JSON.stringify({id, model:config.model, date:new Date().toISOString(), maxRequests:2}), {flag:"wx"});
  let calls = 0;
  try {
    await runReconstruction(page, config, AbortSignal.timeout(1250000), {
      checkpoint: () => {}, stage: stage => process.stdout.write(JSON.stringify({id,stage})+"\n"),
      request: kind => { if (kind !== "textCalls" || ++calls > 2) throw new Error("恢复设计请求超过上限"); process.stdout.write(JSON.stringify({id,request:calls})+"\n"); },
    });
    const files = {offline:path.join(directory,`${id}-offline.html`),embedded:path.join(directory,`${id}-embedded.html`)};
    await fs.writeFile(files.offline,themedReadingDocument(page,undefined,urls));
    await fs.writeFile(files.embedded,themedReadingDocument(page,"reconstruction-fixture",urls));
    await fs.writeFile(path.join(directory,`${id}-resumed${suffix}.json`), JSON.stringify({id,title:original.title,success:true,page,files,calls},null,2));
  } catch(e) {
    await fs.writeFile(path.join(directory,`${id}-resumed${suffix}.json`),JSON.stringify({id,success:false,page,calls,error:e instanceof Error?e.message:String(e)},null,2));
    throw e;
  }
}
void main();
