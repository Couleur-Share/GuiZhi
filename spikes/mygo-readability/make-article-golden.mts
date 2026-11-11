// 产品完整的正文提取（extractWebArticle）基准：对缓存网页实际会使用的那份 HTML（按静态/渲染分流），
// 记录来源（readability / region / page / wechat）、两个段落字数与最终 Markdown，供 Go 侧移植对拍。
// 用法：pnpm dlx tsx spikes/mygo-readability/make-article-golden.mts <样本目录> <golden 目录>
// 需要先运行 route-check.mts 生成 routes.json。
import fs from "node:fs";
import path from "node:path";
import { extractWebArticle } from "../../apps/desktop/src/main/services/web-capture/web-extract-article.ts";

const [evalDir, outDir] = process.argv.slice(2);
if (!evalDir || !outDir) throw new Error("用法: make-article-golden.mts <样本目录> <golden 目录>");
const routes = JSON.parse(fs.readFileSync(path.join(outDir, "routes.json"), "utf8")) as Record<string, string>;
const out: unknown[] = [];
for (const id of fs.readdirSync(path.join(evalDir, "cache")).sort()) {
  const dir = path.join(evalDir, "cache", id);
  const variant = routes[id];
  const file = path.join(dir, `${variant}.html`);
  if (!variant || !fs.existsSync(file)) continue;
  const meta = JSON.parse(fs.readFileSync(path.join(dir, "meta.json"), "utf8"));
  const finalUrl = meta.responseUrl ?? meta.url;
  const url = variant === "static" ? finalUrl : (meta.finalUrl ?? finalUrl);
  const article = extractWebArticle(fs.readFileSync(file, "utf8"), url, meta.status ?? 200);
  out.push({
    id, variant, url, status: meta.status ?? 200,
    source: article.source, readabilityChars: article.readabilityChars, regionChars: article.regionChars,
    title: article.result.title, markdown: article.result.markdown, error: article.result.error?.code ?? null,
  });
}
fs.writeFileSync(path.join(outDir, "article-golden.json"), JSON.stringify(out));
const bySource: Record<string, number> = {};
for (const o of out as { source: string }[]) bySource[o.source] = (bySource[o.source] ?? 0) + 1;
console.log(`输出 ${out.length} 条`, bySource);