// 对产品 cleanHtml（sanitize-html）做基准：真实网页（按静态/渲染分流的那份 HTML）加一组攻击用例。
// 样式属性由 css-tree 白名单另行处理，本对拍不涉及；两边输出都去掉 style 后比较。
// 用法：pnpm dlx tsx spikes/mygo-readability/make-sanitize-golden.mts <样本目录> <golden 目录>
import fs from "node:fs";
import path from "node:path";
import { cleanHtml } from "../../apps/desktop/src/main/services/web-capture/snapshot-sanitize.ts";

const [evalDir, outDir] = process.argv.slice(2);
const routes = JSON.parse(fs.readFileSync(path.join(outDir, "routes.json"), "utf8")) as Record<string, string>;
const H = "a".repeat(64);
// 与 sanitizeSnapshot 的映射一致：只放行本快照登记的 local-image
const map = (u: string) => (u === `local-image://wechat-${H}.png` ? u : undefined);

const attacks: Record<string, string> = {
  script: `<p>a</p><script>alert(1)</script><p>b</p>`,
  onerror: `<img src="https://x/a.png" onerror="alert(1)" alt="x">`,
  jsHref: `<a href="javascript:alert(1)">x</a>`,
  jsHrefEntity: `<a href="jav&#x61;script:alert(1)">x</a>`,
  jsHrefTab: `<a href="java\tscript:alert(1)">x</a>`,
  jsHrefCase: `<a href="JaVaScRiPt:alert(1)">x</a>`,
  dataSrc: `<img src="data:image/svg+xml;base64,PHN2Zz48L3N2Zz4=">`,
  localImage: `<img src="local-image://wechat-${H}.png" alt="ok"><img src="local-image://wechat-${"b".repeat(64)}.png">`,
  protoRel: `<img src="//evil.example/a.png"><a href="//evil.example">x</a>`,
  httpHref: `<a href="HTTPS://example.com/a?b=1&amp;c=2">x</a><a href="http://example.com">y</a><a href="#top">z</a><a href="#">w</a>`,
  relHref: `<a href="/relative/path">x</a><a href="mailto:a@b.c">m</a><a href="tel:123">t</a>`,
  mxssNoscript: `<noscript><p title="</noscript><img src=x onerror=alert(1)>"></noscript>`,
  svgStyle: `<svg viewBox="0 0 10 10"><style>*{x:y}</style><path d="M0 0L10 10" fill="#fff"/></svg>`,
  svgForeign: `<svg><foreignObject><div onclick="x()">hi</div></foreignObject><circle cx="1" cy="1" r="1" fill="url(javascript:x)"/></svg>`,
  svgFill: `<svg><rect fill="red" stroke="rgb(1,2,3)"/><rect fill="url(#x)"/><rect fill="expression(1)"/></svg>`,
  math: `<math><mi xlink:href="javascript:alert(1)">x</mi></math>`,
  iframe: `<iframe src="https://evil"></iframe><object data="x"></object><embed src="x"><form action="x"><input name="a"></form>`,
  textarea: `<textarea><img src=x onerror=1></textarea><select><option>one</option></select>`,
  comment: `<!-- <script>alert(1)</script> --><p>a</p><![CDATA[x]]>`,
  unclosed: `<div><p>one<p>two<div><span>three`,
  table: `<table><tr><td colspan="2">a</td><td>b</td></tr></table>`,
  tableLoose: `<table><div>in</div><tr><td>x</td></tr></table>`,
  nestedA: `<a href="https://a"><a href="https://b">x</a></a>`,
  upperTags: `<DIV CLASS="A" ID="b"><P>x</P></DIV>`,
  entities: `<p>&lt;script&gt;alert(1)&lt;/script&gt; &amp; &copy; &#x1F600; &nbsp;|</p>`,
  attrQuote: `<div title='a"b' lang="zh" dir="rtl" data-x="1" aria-label="l" class="c d">x</div>`,
  attrNoValue: `<div id=x class>a</div><ol start=3><li>a</li></ol><ol start="x" reversed><li>b</li></ol>`,
  base: `<base href="https://evil/"><link rel="stylesheet" href="x"><meta http-equiv="refresh" content="0;url=x"><p>t</p>`,
  styleAttr: `<p style="color:red;position:fixed">x</p>`,
  brHr: `<p>a<br>b<br/>c</p><hr><hr/>`,
  fig: `<figure><img src="https://x/y.png" width="10" height="5" alt="a"><figcaption>c</figcaption></figure>`,
  pre: `<pre><code class="language-js">a &lt; b\n  c</code></pre>`,
  nulls: `<p>a\u0000b</p><a href="java\u0000script:1">x</a>`,
  tagSoup: `<p<script>alert(1)</script>>x</p>`,
  onclickQuote: `<div onclick=alert(1) class=x>y</div><p onmouseover="a" id="ok">z</p>`,
};

const items: { id: string; html: string; out: string }[] = [];
for (const [id, html] of Object.entries(attacks)) items.push({ id: `attack-${id}`, html, out: cleanHtml(html, map) });
for (const id of fs.readdirSync(path.join(evalDir, "cache")).sort()) {
  const variant = routes[id];
  const file = path.join(evalDir, "cache", id, `${variant}.html`);
  if (!variant || !fs.existsSync(file)) continue;
  const html = fs.readFileSync(file, "utf8");
  items.push({ id, html, out: cleanHtml(html, map) });
}
fs.writeFileSync(path.join(outDir, "sanitize-golden.json"), JSON.stringify(items));
console.log("输出", items.length, "条（攻击用例", Object.keys(attacks).length, "）");
