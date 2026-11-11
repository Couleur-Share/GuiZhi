// 对产品 cleanCss（css-tree 白名单）做基准：59 个真实网页里的全部 style 属性与 <style> 块，加一组攻击用例。
// 每条输入用两种 URL 映射各跑一次：none（拒绝所有 url）与 all（把任何 url 映射为本快照的 local-image）。
// 用法：pnpm dlx tsx spikes/mygo-readability/make-css-golden.mts <样本目录> <golden 目录>
import fs from "node:fs";
import path from "node:path";
import { parseHTML } from "linkedom";
import { cleanCss } from "../../apps/desktop/src/main/services/web-capture/snapshot-sanitize.ts";

const [evalDir, outDir] = process.argv.slice(2);
const routes = JSON.parse(fs.readFileSync(path.join(outDir, "routes.json"), "utf8")) as Record<string, string>;
const H = "a".repeat(64);
const mappers = {
  none: () => undefined,
  all: () => `local-image://wechat-${H}.png`,
} as const;

type Item = { id: string; kind: "inline" | "sheet"; css: string };
const items: Item[] = [];
const attacks: Record<string, [string, string]> = {
  urlJs: ["inline", `background:url(javascript:alert(1))`],
  urlQuoted: ["inline", `background-image:url("https://x/a.png");color:red`],
  urlSingle: ["inline", `background-image:url('https://x/a.png')`],
  expression: ["inline", `width:expression(alert(1));color:red`],
  expressionEsc: ["inline", `width:ex\\70ression(alert(1));height:10px`],
  varFn: ["inline", `color:var(--x);margin:calc(var(--y) + 1px)`],
  env: ["inline", `padding:env(safe-area-inset-top)`],
  imageSet: ["inline", `background-image:image-set("a.png" 1x)`],
  paint: ["inline", `background:paint(houdini)`],
  behavior: ["inline", `behavior:url(x.htc);-moz-binding:url(x.xml#a);color:red`],
  position: ["inline", `position:fixed;top:0`],
  positionOk: ["inline", `position:Relative;top:0;left:1px`],
  positionImp: ["inline", `position:absolute !important;position:sticky`],
  important: ["inline", `color:red !important;margin:0!important`],
  upper: ["inline", `COLOR:RED;Font-Size:12PX;BACKGROUND-COLOR:#FFF`],
  vendor: ["inline", `-webkit-box-shadow:0 0 1px #000;box-shadow:0 0 1px rgba(0,0,0,.5)`],
  custom: ["inline", `--x:1;--y:{a:b};color:red`],
  progid: ["inline", `filter:progid:DXImageTransform.Microsoft.gradient(startColorstr='#000',endColorstr='#fff');color:red`],
  semicolons: ["inline", `;;color:red;;;margin:0;`],
  noValue: ["inline", `color:;margin:0;padding`],
  badParen: ["inline", `color:rgb(1,2,3;margin:0`],
  lt: ["inline", `font-family:"</style><script>alert(1)</script>"`],
  ltEsc: ["inline", `content:"<b>"`],
  comment: ["inline", `color:/* c */red;/* x:y */margin:0`],
  fontFamily: ["inline", `font-family:"Helvetica Neue",Arial,'PingFang SC',sans-serif`],
  calc: ["inline", `width:calc(100% - 10px);height:calc( 1px+2px )`],
  grid: ["inline", `display:grid;grid-template-columns:repeat(2,1fr);gap:1px 2px`],
  transform: ["inline", `transform:translate(10px,20px) rotate(45deg);transform-origin:0 0`],
  numbers: ["inline", `opacity:.5;margin:-0.50px;line-height:1.50;width:+10px;z:1e3`],
  unicodeRange: ["inline", `font-family:x;unicode-range:U+0025-00FF`],
  sheetBasic: ["sheet", `p{color:red}.a>.b{margin:0 auto}a:hover,a:focus{text-decoration:none}`],
  sheetAt: ["sheet", `@import url(x.css);@media (max-width:1px){p{color:red}}@font-face{font-family:x;src:url(a.woff)}p{color:blue}@keyframes k{from{opacity:0}to{opacity:1}}`],
  sheetUrl: ["sheet", `.a{background:url(https://x/a.png)}.b{background:url(data:image/png;base64,AAAA)}`],
  sheetSel: ["sheet", `div#id.class[attr="v"]::before{content:"x";color:red}*{box-sizing:border-box}html,body{margin:0}`],
  sheetBad: ["sheet", `p{color:red`],
  sheetNested: ["sheet", `.a{color:red;.b{color:blue}}`],
  sheetImportant: ["sheet", `p{color:red!important;position:fixed}`],
  sheetEmpty: ["sheet", ``],
  sheetUnknown: ["sheet", `p{foo:bar;color:red}`],
  sheetEsc: ["sheet", `.a\\:b{color:red}.\\31 23{margin:0}`],
};
for (const [id, [kind, css]] of Object.entries(attacks)) items.push({ id: `attack-${id}`, kind: kind as Item["kind"], css });
for (const id of fs.readdirSync(path.join(evalDir, "cache")).sort()) {
  const variant = routes[id];
  const file = path.join(evalDir, "cache", id, `${variant}.html`);
  if (!variant || !fs.existsSync(file)) continue;
  const { document } = parseHTML(fs.readFileSync(file, "utf8"));
  document.querySelectorAll("[style]").forEach((el: Element, i: number) => items.push({ id: `${id}#attr${i}`, kind: "inline", css: el.getAttribute("style") ?? "" }));
  document.querySelectorAll("style").forEach((el: Element, i: number) => items.push({ id: `${id}#sheet${i}`, kind: "sheet", css: el.textContent ?? "" }));
}
const out = items.flatMap((it) =>
  (Object.keys(mappers) as (keyof typeof mappers)[]).map((m) => ({
    ...it, id: `${it.id}|${m}`, map: m, out: cleanCss(it.css, mappers[m], it.kind === "sheet"),
  })),
);
fs.writeFileSync(path.join(outDir, "css-golden.json"), JSON.stringify(out));
console.log("输入", items.length, "条（攻击", Object.keys(attacks).length, "），输出", out.length, "条；行内", items.filter((i) => i.kind === "inline").length, "，样式表", items.filter((i) => i.kind === "sheet").length);
