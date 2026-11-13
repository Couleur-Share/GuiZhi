// 真实页面正文对拍（TS 侧）：每个页面只请求 1 次，保存原始 HTML，并用产品的 extractWebArticle 提取，
// 结果写成 JSON，供 Go 侧 TestRealPageParity 对同一份 HTML 提取后逐字节比较。
// 范围：静态页面的「提取」一环（同一份字节、同一个提取函数的两种实现）。
// 不覆盖：请求头/重定向/编码探测的差异、需浏览器渲染页面（那需要真实 Electron/WebView2）。
//   用法：tsx real-parity.mts <输出目录> <id=url> [<id=url> ...]
import fs from "node:fs";
import path from "node:path";
import { extractWebArticle } from "../../../apps/desktop/src/main/services/web-capture/web-extract-article.ts";

const [outDir, ...pairs] = process.argv.slice(2);
if (!outDir || pairs.length === 0) throw new Error("用法: real-parity.mts <输出目录> <id=url>...");
fs.mkdirSync(outDir, { recursive: true });
const UA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 GuiZhiParityCheck/1.0";
for (const pair of pairs) {
  const eq = pair.indexOf("=");
  const id = pair.slice(0, eq), url = pair.slice(eq + 1);
  const res = await fetch(url, { headers: { "user-agent": UA, accept: "text/html,application/xhtml+xml;q=0.9,*/*;q=0.8", "accept-language": "en-US,en;q=0.9" }, redirect: "follow" });
  const bytes = Buffer.from(await res.arrayBuffer());
  const html = bytes.toString("utf8"); // 两页均为 UTF-8；编码探测差异不在本次对拍范围
  const finalUrl = res.url || url;
  fs.writeFileSync(path.join(outDir, `${id}.html`), bytes);
  const article = extractWebArticle(html, finalUrl, res.status);
  fs.writeFileSync(path.join(outDir, `${id}.ts.json`), JSON.stringify({
    id, url: finalUrl, status: res.status, source: article.source, readabilityChars: article.readabilityChars, regionChars: article.regionChars,
    title: article.result.title, markdown: article.result.markdown, error: article.result.error?.code ?? null,
  }));
  console.log(`${id}: HTTP ${res.status} ${bytes.length} 字节 → 来源 ${article.source}，标题 ${JSON.stringify(article.result.title)}，Markdown ${article.result.markdown.length} 字符`);
}
