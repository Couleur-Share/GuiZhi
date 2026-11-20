#!/usr/bin/env node
/**
 * 在 React 参照里查表格面板左缘那两列像素（台账 B-046 (7)(c)）的来源：
 * 浅色主题表格正文行在 x=378、379 两列比行底色暗 3 与 1 级（252/254 对 255），表头行没有；Viva 没有这两列。
 * 本探针对指定点列出 `elementsFromPoint` 的每一层（标签、类名、外框、背景、阴影、边框、轮廓、定位、z-index），
 * 并给出面板、滚动容器与表格的外框，用来判断是哪个元素画了这两列。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-table-edge.mjs [--out <文件>] [--variants light-royal-blue-large-1280x800,…] [--points 378.5:400,379.5:400]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据；知识库数据来自夹具（lib/react-library.mjs）。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";
import { enterLibrary, leaveLibrary } from "../lib/react-library.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);
const variants = String(arg("--variants", "light-royal-blue-large-1280x800")).split(",");
const points = String(arg("--points", "378.5:400,379.5:400,380.5:400,378.5:120")).split(",").map((p) => p.split(":").map(Number));

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variants: [] };

/** 页内：读每个点上各层元素的几何与样式。 */
function collectEdge(pts) {
  const round = (n) => Math.round(n * 1000) / 1000;
  const rect = (r) => [round(r.x), round(r.y), round(r.width), round(r.height)];
  const props = [
    "background-color", "background-image", "box-shadow", "border-left-width", "border-left-color", "border-top-width",
    "outline", "position", "z-index", "opacity", "overflow", "filter", "backdrop-filter", "mask-image",
  ];
  const layer = (el) => {
    const cs = getComputedStyle(el);
    const o = { tag: el.localName, cls: String(el.className).slice(0, 110), rect: rect(el.getBoundingClientRect()) };
    for (const p of props) {
      const v = cs.getPropertyValue(p);
      if (v && v !== "none" && v !== "normal" && v !== "auto" && v !== "0px" && v !== "rgba(0, 0, 0, 0)" && v !== "visible" && v !== "static") o[p] = v;
    }
    return o;
  };
  const out = { dpr: window.devicePixelRatio, root: getComputedStyle(document.documentElement).fontSize, points: [] };
  for (const [x, y] of pts) {
    out.points.push({ x, y, layers: document.elementsFromPoint(x, y).slice(0, 8).map(layer) });
  }
  // elementsFromPoint 看不到 pointer-events: none 的装饰层：另按外框遍历全部元素，列出盖住该点、且有背景图 / 阴影 / 边框 / 绝对定位的。
  out.covering = pts.map(([x, y]) => ({
    x, y,
    els: [...document.querySelectorAll("*")].filter((el) => {
      const r = el.getBoundingClientRect();
      if (!(r.x <= x && x < r.x + r.width && r.y <= y && y < r.y + r.height)) return false;
      const cs = getComputedStyle(el);
      return cs.backgroundImage !== "none" || cs.boxShadow !== "none" || cs.position === "absolute" || cs.position === "fixed" || cs.position === "sticky" ||
        parseFloat(cs.borderLeftWidth) > 0 || parseFloat(cs.borderRightWidth) > 0 || cs.pointerEvents === "none";
    }).slice(0, 14).map(layer),
  }));
  // 相邻元素的外阴影也会投到点上：列出所有带阴影且外框右缘或左缘离 x 不超过 24px 的元素。
  const xs = pts.map((p) => p[0]);
  out.nearbyShadows = [...document.querySelectorAll("*")].filter((el) => {
    const cs = getComputedStyle(el);
    if (cs.boxShadow === "none") return false;
    const r = el.getBoundingClientRect();
    return xs.some((x) => Math.abs(r.right - x) <= 24 || Math.abs(r.left - x) <= 24);
  }).slice(0, 20).map(layer);
  const table = document.querySelector('[data-testid="item-table"]');
  if (table) {
    out.table = rect(table.getBoundingClientRect());
    let scroller = table.parentElement;
    while (scroller && !/overflow/.test(String(scroller.className))) scroller = scroller.parentElement;
    if (scroller) {
      out.scroller = { ...layer(scroller), scrollLeft: scroller.scrollLeft, scrollTop: scroller.scrollTop };
      out.panelAncestors = [];
      for (let el = scroller.parentElement, n = 0; el && n < 4; el = el.parentElement, n++) out.panelAncestors.push(layer(el));
    }
    out.inner = [];
    for (let el = table.parentElement, n = 0; el && el !== scroller && n < 6; el = el.parentElement, n++) out.inner.push(layer(el));
  }
  return out;
}

try {
  const win = session.page;
  for (const name of variants) {
    const variant = parseVariantKey(name, matrix);
    await applyVariant(session, variant, matrix);
    await enterLibrary(session, { variant: "data", list: { fault: "" } });
    await win.waitForFunction(() => document.querySelectorAll('[data-testid="item-list"] [data-index]').length > 3);
    await win.getByRole("button", { name: "列表视图", exact: true }).click();
    await win.getByTestId("item-table").waitFor({ state: "visible" });
    await win.waitForTimeout(300);
    report.variants.push({ variant: name, edge: await win.evaluate(collectEdge, points) });
    await leaveLibrary(session);
  }
} finally {
  try { await leaveLibrary(session); } catch { /* 会话关闭前的清理失败不影响读数 */ }
  await session.close();
}

const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(path.resolve(outFile), text);
  console.log(`已写 ${path.resolve(outFile)}`);
} else {
  console.log(text);
}
