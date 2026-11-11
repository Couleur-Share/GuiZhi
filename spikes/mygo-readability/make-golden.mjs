// 正文提取等价性基准：对缓存的真实网页（59 个样本，静态与渲染两份 HTML），
// 用产品实际依赖的 @mozilla/readability 0.6.0 + linkedom 得到提取结果，供 Go 侧对拍。
// 两种模式：
//   stock   仅 charThreshold=100，其余为库的默认行为，用来衡量 Go 移植版本身与 JS 的差异；
//   product 再加上产品对 Readability 的两处改动（linkDensityModifier=0.1、保留与标题重复的首个 h1/h2）。
// 用法：node spikes/mygo-readability/make-golden.mjs <样本目录> <输出目录>
// 样本目录是 crawl4ai 评估时留下的缓存（含 samples.json 与 cache/<id>/{meta.json,static.html,rendered.html}）。
import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

const [evalDir, outDir] = process.argv.slice(2);
if (!evalDir || !outDir) throw new Error("用法: make-golden.mjs <样本目录> <输出目录>");
const require = createRequire(path.resolve("apps/desktop/package.json"));
const { Readability } = require("@mozilla/readability");
const { parseHTML } = require("linkedom");

// 与 web-extract-article.ts 的 parsePage 一致。
function parsePage(html) {
  const { document } = parseHTML(/<html(?:\s|>)/i.test(html) ? html : `<html><body>${html}</body></html>`);
  return document;
}

function run(html, mode) {
  const document = parsePage(html);
  const options = { charThreshold: 100 };
  if (mode === "product") options.linkDensityModifier = 0.1;
  try {
    const reader = new Readability(document, options);
    if (mode === "product" && typeof reader._headerDuplicatesTitle === "function") reader._headerDuplicatesTitle = () => false;
    const r = reader.parse();
    if (!r) return null;
    return { title: r.title ?? "", text: r.textContent ?? "" };
  } catch (error) {
    return { error: String(error?.message ?? error) };
  }
}

const samples = JSON.parse(fs.readFileSync(path.join(evalDir, "samples.json"), "utf8"));
const out = [];
for (const sample of samples) {
  const dir = path.join(evalDir, "cache", sample.id);
  if (!fs.existsSync(path.join(dir, "meta.json"))) continue;
  const meta = JSON.parse(fs.readFileSync(path.join(dir, "meta.json"), "utf8"));
  for (const variant of ["static", "rendered"]) {
    const file = path.join(dir, `${variant}.html`);
    if (!fs.existsSync(file)) continue;
    const html = fs.readFileSync(file, "utf8");
    const finalUrl = meta.responseUrl ?? sample.url;
    const url = variant === "static" ? finalUrl : (meta.finalUrl ?? finalUrl);
    for (const mode of ["stock", "product"]) {
      out.push({ id: sample.id, variant, mode, url, htmlBytes: html.length, result: run(html, mode) });
    }
  }
}
fs.mkdirSync(outDir, { recursive: true });
fs.writeFileSync(path.join(outDir, "golden.json"), JSON.stringify(out));
const nulls = out.filter((o) => o.result === null).length;
const errors = out.filter((o) => o.result?.error).length;
console.log(`样本 ${samples.length} 个，输出 ${out.length} 条（null ${nulls}，异常 ${errors}）-> ${outDir}`);
