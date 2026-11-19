#!/usr/bin/env node
/**
 * 在 React 参照里量知识库表格表头单元格的次像素几何（台账 B-046 第 (4) 类：浅色主题小字号档「更新时间」表头墨迹水平错位 +1）：
 * 每个表头 th 的外框、里面文字的 Range 矩形（保留 3 位小数）、计算样式（对齐、内边距、字体、字距、颜色），
 * 以及整张表的外框与滚动容器——用来判断两侧的差是文字起点的次像素差，还是别的。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-header-ink.mjs [--out <文件>] [--variants light-royal-blue-small-1280x800,…]
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
const variants = String(arg("--variants", "light-royal-blue-small-1280x800")).split(",");

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variants: [] };

/** 页内：读每个表头单元格的几何与样式。 */
function collectHeader() {
  const round = (n) => Math.round(n * 1000) / 1000;
  const rect = (r) => [round(r.x), round(r.y), round(r.width), round(r.height)];
  const table = document.querySelector('[data-testid="item-table"]');
  if (!table) return { error: "没有 item-table" };
  const style = (el, props) => {
    const cs = getComputedStyle(el);
    return Object.fromEntries(props.map((p) => [p, cs.getPropertyValue(p)]));
  };
  const cells = [...table.querySelectorAll("thead th")].map((th) => {
    const texts = [];
    const walker = document.createTreeWalker(th, NodeFilter.SHOW_TEXT);
    for (let n = walker.nextNode(); n; n = walker.nextNode()) {
      if (!n.textContent.trim()) continue;
      const r = document.createRange();
      r.selectNodeContents(n);
      texts.push({
        text: n.textContent,
        parent: n.parentElement.localName + "." + String(n.parentElement.className).slice(0, 80),
        rects: [...r.getClientRects()].map(rect),
        parentRect: rect(n.parentElement.getBoundingClientRect()),
        parentStyle: style(n.parentElement, ["text-align", "justify-content", "display", "padding-left", "padding-right", "letter-spacing"]),
      });
    }
    return {
      label: th.textContent.trim(),
      rect: rect(th.getBoundingClientRect()),
      style: style(th, [
        "text-align", "padding-left", "padding-right", "padding-top", "padding-bottom", "font-size", "font-weight",
        "line-height", "letter-spacing", "color", "position", "left", "right", "width", "box-sizing",
        "background-color", "backdrop-filter", "z-index", "opacity", "box-shadow", "border-left-width", "border-bottom-width",
      ]),
      // 吸附列的表头单元格里还有没有别的盒（背景垫、阴影）：只读直接子元素的类名与背景
      kids: [...th.children].map((k) => ({
        tag: k.localName, cls: String(k.className).slice(0, 140), rect: rect(k.getBoundingClientRect()),
        ...style(k, ["background-color", "backdrop-filter", "opacity", "z-index", "position"]),
      })),
      texts,
    };
  });
  const scroller = table.closest('[class*="overflow"]') ?? table;
  return {
    dpr: window.devicePixelRatio,
    root: getComputedStyle(document.documentElement).fontSize,
    table: rect(table.getBoundingClientRect()),
    scroller: { cls: String(scroller.className).slice(0, 120), rect: rect(scroller.getBoundingClientRect()), scrollLeft: scroller.scrollLeft, clientWidth: scroller.clientWidth, scrollWidth: scroller.scrollWidth },
    cells,
  };
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
    report.variants.push({ variant: name, header: await win.evaluate(collectHeader) });
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
