// 主题阅读渲染层差分基线：真实 content.ts / document.ts / v3-document.ts / export.ts 的 HTML 组装部分（tsx 直接加载）。
// Go 侧用 goja 打包产物（apps/desktop-go/jsgen/build-themed.mts）跑同样的输入，逐字节比对：这里验证的是
// 「goja 里的 TS 原文件」与「V8 里的 TS 原文件」行为一致（正则、Unicode、序列化差异）。
// 用法：run-golden-themed.mts <输出目录>
import { parseHTML } from "linkedom";

/* eslint-disable @typescript-eslint/no-explicit-any */
export interface Rec { kind: string; name: string; input: unknown; output: unknown }
const errMsg = (e: unknown) => (e instanceof Error ? e.message : String(e));

export const MARKDOWNS: Record<string, string> = {
  technical: "# 本地知识文件的整理流程\n\n这是一份纯离线界面验证教程。\n\n## 一、先定义目录\n\n1. 原始资料保留独立目录。\n2. 生成内容保存到派生目录。\n\n```typescript\nconst example = { title: \"长字段\" };\n```\n\n| 输入 | 文件名 |\n| --- | --- |\n| input_0 | a.md |\n\n> 完成条件：正文完整。",
  cjkEmphasis: "这是**“强调”**的中文，也有*斜体*和~~删除~~，还有~单波浪~与**加粗。**后续文字。\n\n中文句末**，**标点。",
  html: "<div class=\"x\">原样 <b>HTML</b></div>\n\n段落中有 <span style=\"color:red\">行内</span> 与 <script>alert(1)</script> 与 <iframe src=\"https://e.example\"></iframe>。\n\n<details><summary>摘要</summary>\n\n内部 **markdown**\n\n</details>",
  links: "[a](https://a.example/?x=1&y=2) [b](javascript:alert(1)) ![图](local-image://p-1.png) ![外](https://e.example/a.png) <https://auto.example>",
  footnote: "正文[^1] 与另一处[^2]。\n\n[^1]: 脚注一。\n[^2]: 脚注二，含 **粗体**。",
  tasks: "- [x] 完成\n- [ ] 未完成\n\n1. 一\n   - 嵌套\n2. 二",
  entities: "&lt;tag&gt; &amp; &copy; &#x1F600; 😀 \\*转义\\* a_b_c",
  forumColor: "<span class=\"forum-color-red\">红色</span> <span class=\"other\">无</span>",
  whitespace: "  \n\n\n段落一\n\n\n\n段落二\n\n  ",
  onlyHtml: "<p>仅 HTML</p>",
  rawText: "没有空行的\n多行\n纯文本",
  longTable: "| a | b | c |\n| - | - | - |\n" + Array.from({ length: 12 }, (_, i) => `| ${i} | 行 ${i} | ${"x".repeat(i)} |`).join("\n"),
  heading: "Setext 标题\n====\n\n次级\n----\n\n正文",
  emptyAfter: "# 仅标题",
  mixed: "段落\n<!-- 注释 -->\n\n- 项\n\n---\n\n*** \n\n末尾",
  cjkPunct: "他说：“你好”（测试）。**重点：**这里；*强调*，继续。",
  emojiZwj: "👨‍👩‍👧‍👦 家庭 **加粗👍** 结束",
};

export function blockSource(content: string, blocks: any[], title = "测试文章") {
  return { title, content, sourceUri: null, fingerprint: "a".repeat(64), blocks };
}

export function v1Page(markdownKey: string, R: any, extra: Record<string, unknown> = {}) {
  const blocks = R.renderThemedReadingBlocks(MARKDOWNS[markdownKey]);
  return {
    id: `v1-${markdownKey}`, itemId: "it", sourceKind: "body", role: "current", formatVersion: 1, createdAt: 1, updatedAt: 1, warnings: [], assets: [],
    options: { style: "x", generateImages: false, maxImages: 0 },
    source: blockSource(MARKDOWNS[markdownKey], blocks),
    design: { direction: "d", assets: [], html: `<main class="reading-sheet">${blocks.map((b: any) => `<div class="reading-block" data-source-block="${b.id}"></div>`).join("")}</main>`, css: ":root{--theme-surface:#f2f7fc;--theme-text:#163044;--theme-accent:#286eac}.reading-sheet{padding:28px;border-top:5px solid var(--theme-accent)}h1{font-size:30px}a{color:red}p{background:url(https://evil.example/x.png)}" },
    ...extra,
  };
}

export const TEXT = "这是一份用于验证独立阅读环境的完整文章。页面中的交互只改变本页的演示数值，原始知识条目始终独立保存。关闭增强交互以后，正文、图注和说明仍然可读。点击下方按钮，可以观察计数器变化；缩小阅读区以后，正文应自动换行。";
export function v3Page(over: Record<string, unknown> = {}, design: Record<string, unknown> = {}) {
  return {
    id: "v3", itemId: "it", sourceKind: "body", role: "current", formatVersion: 3, createdAt: 1, updatedAt: 1, warnings: [], assets: [],
    options: { style: "", generateImages: false, maxImages: 0, research: false, action: "create", enhancedInteraction: true, researchDepth: "standard" },
    source: blockSource(TEXT, [{ id: "b0", markdown: TEXT, html: `<p>${TEXT}</p>`, text: TEXT }]),
    reconstruction: { notes: [], queries: [], references: [], draft: [{ title: "完整正文", markdown: TEXT, referenceIds: [] }], interactions: [], outline: { title: "互动阅读验证", direction: "阅读优先", questions: [], sections: [{ title: "完整正文", brief: "验证" }] } },
    design: {
      html: `<main><h1>互动阅读验证</h1><h2 id="body">完整正文</h2><p>${TEXT}</p><section id="counter"><button id="add">增加</button> <output id="value">0</output><p>静态说明：按钮每次将计数增加一。</p></section><details><summary>补充说明</summary><p>这段正文支持原生折叠，静态导出会展开。</p></details></main>`,
      css: "", direction: "阅读优先", assets: [],
      scripts: [{ id: "counter-script", code: "document.getElementById('add').addEventListener('click',()=>{document.getElementById('value').textContent=String(Number(document.getElementById('value').textContent)+1)});", status: "ready" }],
      libraries: [], ...design,
    },
    ...over,
  };
}

export async function buildThemedGolden(): Promise<{ recs: Rec[] }> {
  const recs: Rec[] = [];
  const add = (kind: string, name: string, input: unknown, output: unknown) => recs.push({ kind, name, input, output });
  const R: any = await import("../../apps/desktop/src/main/services/themed-reading/content.ts");
  const D: any = await import("../../apps/desktop/src/main/services/themed-reading/document.ts");
  const V: any = await import("../../apps/desktop/src/main/services/themed-reading/v3-document.ts");
  const run = (fn: () => unknown) => { try { return { ok: fn() }; } catch (e) { return { error: errMsg(e) }; } };
  const guarded = (fn: () => unknown) => { try { return { ok: fn() }; } catch (e) { if (e instanceof V.ReadingPageError) return { issue: e.issue }; return { error: errMsg(e) }; } };

  for (const [name, md] of Object.entries(MARKDOWNS)) add("thmBlocks", name, md, run(() => R.renderThemedReadingBlocks(md)));
  add("thmBlocks", "empty", "  \n ", run(() => R.renderThemedReadingBlocks("  \n ")));

  // v1：自由布局 + 占位符回填
  for (const key of ["technical", "html", "links", "cjkEmphasis", "footnote", "forumColor", "tasks"]) {
    const page = v1Page(key, R);
    add("thmDoc", `v1-${key}`, { version: page, instanceId: null, assetUrls: null }, run(() => D.themedReadingDocument(page)));
    add("thmDoc", `v1-${key}-instance`, { version: page, instanceId: "inst-1", assetUrls: null }, run(() => D.themedReadingDocument(page, "inst-1")));
  }
  {
    const page = v1Page("links", R, { assets: [{ id: "a1", role: "original", purpose: "p", prompt: "", alt: "图一", aspectRatio: "16:9", status: "ready", fileName: "p-1.png", sha256: "b".repeat(64), bytes: 3 }] });
    page.design.html = `<main><img data-theme-asset="a1" alt="x">${page.source.blocks.map((b: any) => `<div data-source-block="${b.id}"></div>`).join("")}</main>`;
    const urls = { a1: "data:image/png;base64,QUJD" };
    add("thmDoc", "v1-assets-export", { version: page, instanceId: null, assetUrls: urls }, run(() => D.themedReadingDocument(page, undefined, urls)));
    add("thmDoc", "v1-assets-local", { version: page, instanceId: "i", assetUrls: null }, run(() => D.themedReadingDocument(page, "i")));
    const bad = JSON.parse(JSON.stringify(page)); bad.design.html = "<main><div data-source-block=\"b9\"></div></main>";
    add("thmDoc", "v1-bad-slot", { version: bad, instanceId: null, assetUrls: null }, run(() => D.themedReadingDocument(bad)));
    const badLink = JSON.parse(JSON.stringify(page)); badLink.design.html += "<a href=\"https://evil.example\">x</a>";
    add("thmDoc", "v1-bad-link", { version: badLink, instanceId: null, assetUrls: null }, run(() => D.themedReadingDocument(badLink)));
    add("thmDoc", "v0-unsupported", { version: { formatVersion: 9 }, instanceId: null, assetUrls: null }, run(() => D.themedReadingDocument({ formatVersion: 9 } as any)));
  }

  // v3
  const pages: Record<string, any> = {
    basic: v3Page(),
    noScripts: v3Page({}, { scripts: [] }),
    noEnhanced: v3Page({ options: { ...v3Page().options, enhancedInteraction: false } }, { scripts: [] }),
    failedScript: v3Page({}, { scripts: [{ id: "counter-script", code: "x", status: "failed", error: "bad" }] }),
    echarts: v3Page({}, { libraries: ["echarts"], html: `<main><h1>图表</h1><h2 id="c">章节</h2><p>${TEXT}</p><figure><canvas id="chart" aria-label="图表"></canvas><figcaption>说明</figcaption></figure></main>`, scripts: [{ id: "chart-script", rootId: "c", code: "var a=1;", status: "ready" }] }),
    withImage: v3Page({ assets: [{ id: "a1", role: "generated", purpose: "p", prompt: "x", alt: "图", aspectRatio: "16:9", status: "ready", fileName: "g-1.png", sha256: "c".repeat(64), bytes: 3 }] }, { html: `<main><h1>图</h1><h2 id="s">节</h2><p>${TEXT}</p><img data-theme-asset="a1" alt="图"></main>`, scripts: [], assets: [] }),
    styled: v3Page({}, { css: ".a{color:red;background:url(https://evil.example/x)}@media(max-width:600px){.b{display:none}}h1{font-size:2em}", scripts: [] }),
    ui: v3Page({}, { scripts: [], html: `<main><h1>组件</h1><h2 id="s">节</h2><p>${TEXT}</p><div class="gz-ui-timeline gz-ui-card">x</div></main>` }),
    stylePosition: v3Page({}, { scripts: [], html: `<main><h1>标题</h1><h2 id="s">节</h2><p>${TEXT}</p><p style="position:fixed;color:red">x</p></main>` }),
  };
  const assetUrls = { a1: "data:image/png;base64,QUJD" };
  for (const [name, page] of Object.entries(pages)) {
    add("thmV3Validate", name, { version: page, partial: false }, guarded(() => { V.validateV3Page(page, false); return null; }));
    add("thmV3Validate", `${name}-partial`, { version: page, partial: true }, guarded(() => { V.validateV3Page(page, true); return null; }));
    for (const [flags, args] of Object.entries({ "ihost": [true, false, true], "static": [false, false, true], "partial": [true, true, true], "nohost": [true, false, false] }) as [string, boolean[]][]) {
      add("thmV3Inner", `${name}-${flags}`, { version: page, assets: assetUrls, interactive: args[0], partial: args[1], host: args[2] }, guarded(() => V.v3InnerDocument(page, assetUrls, args[0], args[1], args[2])));
    }
    add("thmDoc", `v3-${name}`, { version: page, instanceId: null, assetUrls: null }, run(() => D.themedReadingDocument(page)));
    for (const staticOnly of [false, true]) {
      add("thmExport", `${name}-${staticOnly ? "static" : "live"}`, { version: page, urls: assetUrls, staticOnly }, run(() => {
        let html = V.v3InnerDocument(page, assetUrls, !staticOnly, false, false);
        if (staticOnly) {
          const doc = parseHTML(html).document;
          for (const script of doc.querySelectorAll("script")) script.remove();
          doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.setAttribute("content", "default-src 'none';script-src 'none';style-src 'unsafe-inline';img-src data:;base-uri 'none';form-action 'none'");
          html = "<!doctype html>" + doc.documentElement.outerHTML;
        } else html = V.v3WrapperDocument(html);
        return html;
      }));
    }
  }
  const badPages: Record<string, any> = {
    noBody: v3Page({}, { html: "  " }),
    noH2: v3Page({}, { html: "<main><h1>只有标题</h1><p>" + TEXT + "</p></main>", scripts: [] }),
    dupId: v3Page({}, { html: `<main><h1>a</h1><h2 id="x">b</h2><p id="x">${TEXT}</p></main>`, scripts: [] }),
    badLink: v3Page({}, { html: `<main><h1>a</h1><h2 id="x">b</h2><p>${TEXT}</p><a href="#nope">去</a></main>`, scripts: [] }),
    unknownImg: v3Page({}, { html: `<main><h1>a</h1><h2 id="x">b</h2><p>${TEXT}</p><img data-theme-asset="zz"></main>`, scripts: [] }),
    canvasNoText: v3Page({}, { html: `<main><h1>a</h1><h2 id="x">b</h2><p>${TEXT}</p><canvas id="c"></canvas></main>`, scripts: [] }),
    badScript: v3Page({}, { scripts: [{ id: "s1", code: "let = ;", status: "ready" }] }),
    scriptNoRoot: v3Page({}, { scripts: [{ id: "s1", rootId: "missing", code: "var a;", status: "ready" }] }),
    enhancedOffWithScript: v3Page({ options: { ...v3Page().options, enhancedInteraction: false } }),
    inlineStyle: v3Page({}, { html: `<main><style>.a{}</style><h1>a</h1><h2 id="x">b</h2><p>${TEXT}</p></main>`, scripts: [] }),
  };
  for (const [name, page] of Object.entries(badPages)) add("thmV3Validate", `bad-${name}`, { version: page, partial: false }, guarded(() => { V.validateV3Page(page, false); return null; }));

  const sections: [string, string, boolean][] = [
    ["plain", `<h2 id="a">标题</h2><p>${TEXT}</p>`, true],
    ["style", `<style>.a{color:red}</style><h2 id="a">标题</h2><p class="a">x</p>`, true],
    ["script", `<h2 id="a">标题</h2><script>document.title='x'</script>`, true],
    ["scriptOff", `<h2 id="a">标题</h2><script>document.title='x'</script>`, false],
    ["scriptSrc", `<script src="https://e.example/x.js"></script>`, true],
    ["scriptType", `<script type="module">x</script>`, true],
    ["iframe", `<iframe src="https://e.example"></iframe>`, true],
    ["onclick", `<button onclick="x()">b</button>`, true],
    ["externalImg", `<img src="https://e.example/a.png">`, true],
    ["externalHref", `<a href="https://e.example">x</a>`, true],
    ["anchor", `<a href="#sec-a">x</a><img data-theme-asset="a1" src="x">`, true],
    ["svg", `<svg viewBox="0 0 10 10"><path d="M0 0L10 10" stroke="#000"/></svg>`, true],
    ["svgBad", `<svg><foreignObject><div>x</div></foreignObject></svg>`, true],
    ["fullDoc", `<html><head><style>p{color:red}</style></head><body><p>x</p></body></html>`, true],
    ["css", `<style>@import url(https://e.example/x.css);</style><p>x</p>`, true],
    ["tooBig", "x".repeat(2000001), true],
    ["meta", `<meta http-equiv="refresh" content="0">`, true],
  ];
  for (const [name, html, enhanced] of sections) add("thmV3Section", name, { html, unit: "sec", enhanced }, guarded(() => V.normalizeV3Section(html, "sec", enhanced)));

  const clean: [string, string, Record<string, string>][] = [
    ["mixed", `<div onclick="x" class="a" style="color:red;position:fixed"><b>粗</b><script>x</script><img data-theme-asset="a1" src="https://e.example/a.png"><a href="https://e.example">x</a><a href="#ok">y</a><input type="file"><button type="submit">b</button></div>`, assetUrls],
    ["upper", `<DIV CLASS="a"><SVG viewBox="0 0 1 1"><LINEARGRADIENT id="g"></LINEARGRADIENT></SVG></DIV>`, {}],
    ["aria", `<p aria-label="x" data-x="1" role="note">t</p>`, {}],
  ];
  for (const [name, html, assets] of clean) add("thmV3Clean", name, { html, assets }, run(() => V.cleanV3Html(html, assets)));

  for (const [name, code] of Object.entries({ ok: "var a = 1;", syntax: "let = ;", unterminated: "function(", asyncok: "(async()=>{await 1})()", strictBad: "with(a){}", octal: "var a = 010;", newline: "var a = 1;\nvar b = ;", dup: "let a; let a;" }))
    add("thmV3Script", name, { id: "s1", code, status: "ready" }, run(() => V.checkReadingScript({ id: "s1", code, status: "ready" }) ?? null));
  return { recs };
}
