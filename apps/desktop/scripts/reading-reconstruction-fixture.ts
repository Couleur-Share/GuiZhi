import fs from "node:fs/promises";
import path from "node:path";
import { reconstructionFixture } from "../tests/unit/reading-reconstruction-fixture";
import { themedReadingDocument } from "../src/main/services/themed-reading/document";

async function main() {
  const directory = path.resolve("../../artifacts/themed-reading/reconstruction-fixture"); await fs.mkdir(directory, { recursive: true });
  const fixtures = [];
  for (const id of ["fish-oil", "beer"]) {
    const page = reconstructionFixture(); page.id = id; page.sourceKind = id === "fish-oil" ? "summary" : "body";
    page.source.title = id === "fish-oil" ? "鱼油重构验收" : "啤酒重构验收";
    page.source.content = "# 原文标题\n\n原文查找唯一词。\n\n## 第二节\n\n原始内容独立保留。";
    page.reconstruction.outline.title = page.source.title;
    page.reconstruction.references = [{ id: "R1", title: "隔离参考资料", url: "https://example.org/reference", capturedAt: 1, status: "ready", text: "隔离测试资料，不代表真实联网采集" }];
    page.reconstruction.draft[0].referenceIds = ["R1"];
    page.design.html = `<main><header><span class="category">AI READING · ${id === "fish-oil" ? "鱼油" : "啤酒"}</span><h1>${page.source.title}</h1><p>围绕核心问题组织的新文章，用比较、解释和工具帮助理解。</p><nav><a href="#overview">01 阅读提要 ↗</a><a href="#compare">02 概念比较 ↗</a><a href="#tools">03 交互工具 ↗</a></nav></header><section id="overview"><h2>从问题开始理解</h2><p>${"重构后的正文包含新的组织方式、背景解释与例子。原始 Markdown 在另一个阅读入口中保留。".repeat(3)}</p></section><section id="compare"><h2>比较不同选择</h2><div class="cards"><article><h3>第一种选择</h3><p>明确前提、适用情景与影响判断的信息。</p></article><article><h3>第二种选择</h3><p>把不同维度区分开来，再比较各自的特点。</p></article></div><table><thead><tr><th>项目</th><th>特点</th><th>判断条件</th></tr></thead><tbody><tr><td>对象甲</td><td>独立维度</td><td>结合实际条件选择</td></tr></tbody></table></section><section id="tools"><h2>用工具帮助理解</h2><div data-reading-tool="cost"></div><h3>切换情景</h3><div data-reading-tool="choice"></div><div data-reading-panel="choice:first"><h3>情景一</h3><p>第一种情景的完整解释。</p></div><div data-reading-panel="choice:second"><h3>情景二</h3><p>第二种情景的独有解释。</p></div></section><footer><a href="#overview">返回开头</a></footer></main>`;
    page.reconstruction.interactions.push({ id: "choice", kind: "scenario", choices: [{ label: "情景一", value: "first" }, { label: "情景二", value: "second" }] });
    page.design.css = `html{--theme-surface:${id === "fish-oil" ? "#f1f7fc" : "#fbf6ee"};--theme-text:#172839;--theme-card:#fff;--theme-accent:${id === "fish-oil" ? "#096f9c" : "#995006"}}html[data-theme=dark]{--theme-surface:#111923;--theme-text:#e5edf5;--theme-card:#1b2837;--theme-accent:#83c4eb}main{max-width:1000px;margin:0 auto;padding:32px 24px}header{text-align:center;padding:24px 0}h1{font:700 clamp(30px,4vw,48px)/1.3 Georgia,serif;margin:24px 0}h2{font:700 28px/1.4 Georgia,serif;color:var(--theme-accent)}.category{color:var(--theme-accent);font-weight:700}nav{display:flex;gap:12px;margin:36px 0}nav a{display:block;flex:1;padding:14px;border:1px solid var(--theme-accent);border-radius:12px;text-decoration:none}section{padding:30px;margin:28px 0;border-radius:24px;background:var(--theme-card);border:1px solid #8595a530}.cards{display:grid;grid-template-columns:1fr 1fr;gap:24px}.cards article{padding:20px;border:1px solid #8595a580;border-radius:16px}table{width:100%;margin:24px 0;border-collapse:collapse}th,td{padding:16px;border:1px solid #8595a580;text-align:left}footer{text-align:center;padding:20px}@media(max-width:600px){main{padding:16px}.cards{grid-template-columns:1fr}section{padding:20px}nav{flex-direction:column}}`;
    const files = { offline: path.join(directory, `${id}-offline.html`), embedded: path.join(directory, `${id}-embedded.html`) };
    await fs.writeFile(files.offline, themedReadingDocument(page)); await fs.writeFile(files.embedded, themedReadingDocument(page, "reconstruction-fixture"));
    fixtures.push({ id, title: page.source.title, page, files, success: true });
  }
  await fs.writeFile(path.join(directory, "fixtures.json"), JSON.stringify({ fixtures, fixtureOnly: true }, null, 2));
}
void main();
