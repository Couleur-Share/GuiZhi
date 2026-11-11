// 合成输入的整条提取对拍：超长截断（200,000 码点，含代理对）、边界长度。
// 用法：pnpm dlx tsx spikes/mygo-readability/make-synthetic-golden.mts <输出.json>
import fs from "node:fs";
import { extractWebArticle } from "../../apps/desktop/src/main/services/web-capture/web-extract-article.ts";

const page = (body: string) => `<!doctype html><html><head><title>合成页面</title></head><body><article>${body}</article></body></html>`;
const paras = (unit: string, n: number) => Array.from({ length: n }, () => `<p>${unit}</p>`).join("");
const cases: { name: string; html: string }[] = [];
// 精确边界：单段 a×N，Markdown 长度即 N
for (const n of [199_999, 200_000, 200_001]) cases.push({ name: `ascii-${n}`, html: page(`<p>${"a".repeat(n)}</p>`) });
// 全是代理对：UTF-16 长度是码点数的 2 倍，切点必须落在码点边界
for (const n of [100_000, 100_001, 250_000]) cases.push({ name: `emoji-${n}`, html: page(`<p>${"😀".repeat(n)}</p>`) });
// 码点数恰为 200000、UTF-16 长度超过 200000：不应被截断
cases.push({ name: "emoji-100000-plus-ascii-100000", html: page(`<p>${"😀".repeat(100_000)}${"a".repeat(100_000)}</p>`) });
// 多段中文，约 30 万字
cases.push({ name: "zh-paras-300k", html: page(paras("归知是本地优先的个人知识库，".repeat(8), 2600)) });
// 中英混排加表情，切点落在段落中间
cases.push({ name: "mixed-paras", html: page(paras("Hello 世界 😀 data ".repeat(10), 1500)) });
const out = cases.map(({ name, html }) => {
  const a = extractWebArticle(html, "https://example.com/post", 200);
  return { name, html, source: a.source, markdown: a.result.markdown, truncated: a.result.truncated, error: a.result.error?.code ?? null };
});
fs.writeFileSync(process.argv[2], JSON.stringify(out));
console.log(out.map((o) => `${o.name}: ${o.source} truncated=${o.truncated} mdChars=${[...o.markdown].length}`).join("\n"));
