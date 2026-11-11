import fs from "node:fs";
// 产品的静态/渲染分流（web-static-route.ts 的 assessStaticPage）：决定每个样本实际用哪份 HTML 做正文提取。
// 用法：pnpm dlx tsx spikes/mygo-readability/route-check.mts <样本目录> <golden 目录>
import { assessStaticPage } from "../../apps/desktop/src/main/services/web-capture/web-static-route.ts";
const base = process.argv[2];
const out = process.argv[3];
const ids = fs.readdirSync(base + "/cache");
const rows = [];
for (const id of ids) {
  const f = `${base}/cache/${id}/static.html`;
  if (!fs.existsSync(f)) continue;
  const meta = JSON.parse(fs.readFileSync(`${base}/cache/${id}/meta.json`, "utf8"));
  const d = assessStaticPage(fs.readFileSync(f, "utf8"), meta.status);
  rows.push({ id, route: meta.productDecodeOk === false ? "render(decode)" : d.route });
}
fs.writeFileSync(`${out}/routes.json`, JSON.stringify(Object.fromEntries(rows.map((r) => [r.id, r.route.startsWith("render") ? "rendered" : "static"]))));
const by = {};
for (const r of rows) (by[r.route] ??= []).push(r.id);
for (const [k, v] of Object.entries(by)) console.log(k, v.length);
for (const id of ["08-","09-","10-","11-","12-","50-","49-"]) { const r = rows.find(x => x.id.startsWith(id)); console.log(id, r?.route); }
