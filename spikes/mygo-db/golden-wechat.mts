// 公众号采集基线：把真实 wechat.ts（经 esbuild 打包，出站抓取与图片目录换成桩）跑在一组夹具页面上，
// 录制快照（净化 HTML/CSS、资产、失败清单、哈希）、元数据与错误文案，供 Go 端 captureWechat 对拍。
// 用法：run-golden-wechat.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）。
import { build } from "esbuild";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "..", "..");
const stubs = path.join(here, "wechat-stubs");

const b64 = (...parts: (number[] | string)[]) =>
  Buffer.concat(parts.map((p) => (typeof p === "string" ? Buffer.from(p, "latin1") : Buffer.from(p)))).toString("base64");
const PNG = (tag: string) => b64([137, 80, 78, 71, 13, 10, 26, 10], `IHDR${tag}`);
const JPG = (tag: string) => b64([255, 216, 255, 224], `JFIF${tag}`);
const GIF = (tag: string) => b64("GIF89a", tag);
const WEBP = (tag: string) => b64("RIFF", [0, 0, 0, 0], "WEBP", tag);

const IMG = "http://mmbiz.qpic.cn";
const url = (id: string) => `http://mp.weixin.qq.com/s/${id}`;

function article(opts: { head?: string; title?: string; account?: string; body: string; bodyAttrs?: string }): string {
  return `<!doctype html><html><head><meta charset="utf-8">${opts.head ?? ""}</head><body>
${opts.title === undefined ? "" : `<h1 id="activity-name"> ${opts.title} </h1>`}${opts.account === undefined ? "" : `<a id="js_name"> ${opts.account} </a>`}
<div id="js_content"${opts.bodyAttrs ?? ""}>${opts.body}</div></body></html>`;
}

interface Case {
  name: string;
  url: string;
  pages: Record<string, { html: string; finalUrl?: string }>;
  images: Record<string, { b64?: string; status?: number }>;
}

const cases: Case[] = [];
const add = (name: string, id: string, html: string, images: Case["images"] = {}, finalUrl?: string) =>
  cases.push({ name, url: url(id), pages: { [url(id)]: { html, finalUrl } }, images });

// 1. 完整文章：隐藏容器、懒加载图片、封面、锚点、样式、媒体占位
add(
  "full",
  "full",
  article({
    head: `<title>页面标题</title><meta property="og:title" content=" OG 标题 "><meta property="og:image" content="${IMG}/cover.png">
<meta name="author" content=" 作者甲 "><meta property="article:published_time" content="2024-05-01T12:00:00+08:00">
<style>.title{color:#333;font-size:20px}#js_content img{max-width:100%}.bg{background:url(${IMG}/bg.jpg)}@import url(http://evil.test/a.css);.x{position:fixed}</style>`,
    title: "文章标题",
    account: "公众号乙",
    bodyAttrs: ` style="visibility: hidden; opacity: 0; display: none; color: red;"`,
    body: `<p style="text-align:center">第一段 <strong>加粗</strong> <em>斜体</em> <a href="/s/other">相对链接</a> <a href="#top">锚点</a> <a href="javascript:alert(1)">脚本</a> <a href="http://example.com/x?y=1&z=2">外链</a></p>
<section class="bg" style="background-image:url(${IMG}/bg.jpg);padding:8px"><img data-src="${IMG}/a.png" src="data:image/gif;base64,R0lGOD" alt="甲"><img src="${IMG}/b.jpg" alt="乙"></section>
<p><img data-src="${IMG}/c.gif"><img src="${IMG}/d.webp"><img src="${IMG}/bad.png"></p>
<h2>小节</h2><ul><li>一</li><li>二<ul><li>二点一</li></ul></li></ul><ol><li>甲</li><li>乙</li></ol>
<blockquote>引用文字</blockquote><pre><code class="language-js">const a = 1;\nconsole.log(a);</code></pre><table><tr><th>表头</th><th>列</th></tr><tr><td>单元</td><td>格</td></tr></table>
<iframe src="https://v.qq.com/x"></iframe><video poster="${IMG}/poster.png" src="https://x/y.mp4"></video><mpvoice name="语音"></mpvoice>`,
  }),
  {
    [`${IMG}/a.png`]: { b64: PNG("a") },
    [`${IMG}/b.jpg`]: { b64: JPG("b") },
    [`${IMG}/c.gif`]: { b64: GIF("c") },
    [`${IMG}/d.webp`]: { b64: WEBP("d") },
    [`${IMG}/cover.png`]: { b64: PNG("cover") },
    [`${IMG}/bg.jpg`]: { b64: JPG("bg") },
    [`${IMG}/poster.png`]: { b64: PNG("poster") },
    // bad.png 缺失 → HTTP 404（唯一的下载失败，避免失败清单顺序依赖并发）
  },
);

// 2. 标题回退：#activity-name 缺失 → og:title；两者都缺 → <title>；都缺 → URL
add("title-og", "t-og", article({ head: `<meta property="og:title" content="只有 OG">`, body: "<p>正文</p>" }));
add("title-doc", "t-doc", article({ head: `<title>文档标题</title>`, body: "<p>正文</p>" }));
add("title-url", "t-url", article({ body: "<p>正文</p>" }));

// 3. 发布时间：无时区 → null；带时区 → 毫秒；非法 → null
add("date-no-tz", "d1", article({ head: `<meta property="article:published_time" content="2024-05-01T12:00:00">`, title: "T", body: "<p>x</p>" }));
add("date-z", "d2", article({ head: `<meta property="article:published_time" content="2024-05-01T12:00:00Z">`, title: "T", body: "<p>x</p>" }));
add("date-bad", "d3", article({ head: `<meta property="article:published_time" content="2024-13-45T99:00:00Z">`, title: "T", body: "<p>x</p>" }));

// 4. 无正文 / 只有图片 / 空白正文
add("no-body", "nb", `<html><body><div id="other">x</div></body></html>`);
add("blank-body", "bb", article({ body: "   \n  " }));
add("img-only", "io", article({ title: "图集", body: `<img data-src="${IMG}/only.png">` }), { [`${IMG}/only.png`]: { b64: PNG("only") } });

// 5. 动态 SVG、特殊协议资源
add("svg-motion", "svg", article({ title: "SVG", body: `<svg width="10" height="10"><circle r="3"><animate attributeName="r" from="3" to="6" dur="1s"/></circle></svg><p>文字</p>` }));
add(
  "unsupported-urls",
  "uns",
  article({ title: "资源", body: `<p style="background:url(ftp://x/y.png)"><img src="blob:abc"><img src="${IMG}/ok.png"><a href="mailto:a@b.c">邮件</a></p>` }),
  { [`${IMG}/ok.png`]: { b64: PNG("ok") } },
);
add("not-image", "ni", article({ title: "非图", body: `<img src="${IMG}/text.png">` }), { [`${IMG}/text.png`]: { b64: Buffer.from("not an image at all").toString("base64") } });

// 6. 跳转到其他平台
add("redirect-away", "ra", article({ title: "T", body: "<p>x</p>" }), {}, "http://example.com/other");

// 7. 同一图片多个来源（内容相同 → 同一文件名）；懒加载优先级
add(
  "dup-assets",
  "dup",
  article({ title: "重复", body: `<img src="${IMG}/one.png"><img data-src="${IMG}/two.png" src="${IMG}/ignored.png">` }),
  { [`${IMG}/one.png`]: { b64: PNG("same") }, [`${IMG}/two.png`]: { b64: PNG("same") }, [`${IMG}/ignored.png`]: { b64: PNG("ignored") } },
);

// 8. 容器样式：保留其他声明；只有隐藏相关声明
add("style-only-hidden", "soh", article({ title: "T", bodyAttrs: ` style="visibility:hidden"`, body: "<p>x</p>" }));
add("style-display-block", "sdb", article({ title: "T", bodyAttrs: ` style="display:block;opacity:.5"`, body: "<p>x</p>" }));
add("style-important", "simp", article({ title: "T", bodyAttrs: ` style="display: none !important; visibility: hidden !important"`, body: "<p>x</p>" }));


// 9. 脏 HTML 与样式资源：未闭合标签（跨标签的格式化标签错位嵌套属已知差异：x/net/html 按规范重建 <b>，linkedom 不重建）、大小写、表格有无 tbody、实体、@font-face / @media 里的 url、协议相对地址
add(
  "messy-html",
  "messy",
  article({
    head: `<style>@font-face{font-family:x;src:url(${IMG}/f.png)}@media(max-width:1px){.a{background:url("${IMG}/m.png")}}.b{background:url('//mmbiz.qpic.cn/n.png') !important}</style>`,
    title: "脏页面",
    body: `<p>未闭合<p>第二段<div>块<span>跨<b>粗</b></span>体</div><br><BR/><P CLASS=A>大写</P><img src="//mmbiz.qpic.cn/p.png">
<table><tr><td>无 tbody</td></tr></table><table><tbody><tr><td>有 tbody</td></tr></tbody></table><table><thead><tr><th>头</th></tr></thead><tr><td>体</td></tr></table>
&nbsp;&amp;&lt;&gt;&copy;&#x4e2d;<span style="background:url('//mmbiz.qpic.cn/n.png') !important;color:#f00">样式</span><!-- 注释 --><script>var x = "<div id='js_content'>";</script>`,
  }),
  {
    [`${IMG}/f.png`]: { b64: PNG("f") },
    [`${IMG}/m.png`]: { b64: PNG("m") },
    [`${IMG}/n.png`]: { b64: PNG("n") },
    [`${IMG}/p.png`]: { b64: PNG("p") },
  },
);
const bundlePath = path.join(here, `.wechat-bundle-${process.pid}.mjs`);
await build({
  entryPoints: [path.join(repo, "apps/desktop/src/main/services/web-capture/wechat.ts")],
  outfile: bundlePath,
  bundle: true,
  format: "esm",
  banner: { js: "import { createRequire as __gzCreateRequire } from 'node:module'; const require = __gzCreateRequire(import.meta.url);" },
  platform: "node",
  target: "node20",
  logLevel: "error",
  packages: "external",
  plugins: [
    {
      name: "wechat-stubs",
      setup(b) {
        b.onResolve({ filter: /import\/safe-fetch$/ }, () => ({ path: path.join(stubs, "safe-fetch.ts") }));
        b.onResolve({ filter: /runtime-paths$/ }, () => ({ path: path.join(stubs, "runtime-paths.ts") }));
      },
    },
  ],
});
const wechat = (await import(pathToFileURL(bundlePath).href)) as {
  captureWechat: (url: string, taskId: string) => Promise<Record<string, unknown> & { snapshot?: Record<string, unknown> }>;
};

export async function buildWechatGolden() {
  const out: unknown[] = [];
  for (const c of cases) {
    const images = fs.mkdtempSync(path.join(os.tmpdir(), "gz-wx-img-"));
    process.env.GZ_WX_IMAGES = images;
    (globalThis as Record<string, unknown>).__WX = { pages: c.pages, images: c.images };
    let result: unknown, error: string | undefined;
    try {
      const r = await wechat.captureWechat(c.url, "task-" + c.name);
      // capturedAt 是当前时间，不参与对拍
      result = { ...r, capturedAt: 0 };
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
    const files = fs.existsSync(images) ? fs.readdirSync(images).sort() : [];
    out.push({ name: c.name, url: c.url, pages: c.pages, images: c.images, result, error, files });
    fs.rmSync(images, { recursive: true, force: true });
  }
  fs.rmSync(bundlePath, { force: true });
  return { cases: out };
}
