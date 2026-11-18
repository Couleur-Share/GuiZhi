#!/usr/bin/env node
/**
 * 在 React 参照里量 Chromium 弹性布局产生的分数位置（台账 B-025）：
 * Chromium 的布局坐标是 1/64 像素的定点数（LayoutUnit），Viva 是 float32 的精确运算；恰好落在半个像素上的元素
 * 会差 1 个设备像素。这里把几类会产生分数位置的容器搭在页面上，读出 getBoundingClientRect，作为 kit 里
 * 按 Chromium 规则摆放的容器（kit.EvenCells 等）L4 用例的期望值。
 *
 * 搭的容器（都是 display:flex，宽度取一组整数与小数）：
 *   even    n 个 `flex: 1 1 0%; min-width: 0; justify-content: center` 的格子，每格居中放一个定宽孩子（显示设置页的色带），可带 gap；
 *   between justify-content: space-between 的定宽孩子；
 *   center  justify-content: center 的一个定宽孩子；
 *   grow    定宽（flex-basis）与 flex: 1 的孩子混排，可带 gap（工具栏的搜索框加按钮）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-flex-units.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { openReactSession } from "../lib/react-session.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);

// 容器宽度：整数一串（含显示设置页大字号档的 802）、小数几个。
const WIDTHS = [];
for (let w = 640; w <= 1000; w += 29) WIDTHS.push(w);
WIDTHS.push(802, 801, 803, 799, 563, 331.5, 802.25, 802.5, 802.75, 700.125, 913.34375);
const COUNTS = [2, 3, 4, 5, 6, 7, 8, 9];
const GAPS = [0, 8, 9, 13.5];
const CHILD_WIDTHS = [45, 40, 36.5, 0];

const cases = [];
const spec = { even: [], between: [], center: [], grow: [] };
for (const w of WIDTHS) {
  for (const n of COUNTS) {
    for (const gap of [0, 9]) spec.even.push({ w, n, gap, childW: 45 });
  }
}
// 交叉：只在几个宽度上扫缝隙与孩子宽度，避免用例爆炸
for (const w of [802, 563, 913.34375, 331.5]) {
  for (const n of [3, 5, 7]) {
    for (const gap of GAPS) for (const childW of CHILD_WIDTHS) spec.even.push({ w, n, gap, childW });
  }
}
for (const w of WIDTHS) {
  for (const n of [2, 3, 4, 5, 6, 7]) {
    spec.between.push({ w, n, childW: 45 });
    spec.between.push({ w, n, childW: 36.5 });
  }
}
for (const w of WIDTHS) {
  for (const childW of [45, 36.5, 0.34375, 101.25]) spec.center.push({ w, childW });
}
// grow：每项 {basis, grow}，basis 为 null 表示 flex: 1 1 0%
const GROW_SHAPES = [
  [[null], [100, 0]],
  [[100, 0], [null], [60, 0]],
  [[null], [32, 0], [32, 0]],
  [[null], [null], [32, 0]],
  [[null], [null], [null]],
  [[40, 0], [null], [40, 0]],
  [[33.5, 0], [null], [null], [60.25, 0]],
];
for (const w of WIDTHS) {
  for (const gap of [0, 8, 9]) {
    for (const shape of GROW_SHAPES) spec.grow.push({ w, gap, items: shape.map(([basis, g]) => ({ basis: basis ?? null, grow: g ?? 1 })) });
  }
}

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const { page: win } = session;
  const result = await win.evaluate(({ spec: sp }) => {
    const host = document.createElement("div");
    host.style.cssText = "position:absolute;left:0;top:0;visibility:hidden;pointer-events:none;width:2000px;";
    document.body.appendChild(host);
    const px = (v) => `${v}px`;
    const rect = (el) => {
      const r = el.getBoundingClientRect();
      return [r.x, r.y, r.width, r.height];
    };
    const build = (css, children) => {
      const box = document.createElement("div");
      box.style.cssText = `display:flex;height:50px;${css}`;
      host.appendChild(box);
      for (const child of children) box.appendChild(child);
      return box;
    };
    const out = { even: [], between: [], center: [], grow: [] };
    for (const c of sp.even) {
      const cells = [];
      const items = [];
      for (let i = 0; i < c.n; i += 1) {
        const cell = document.createElement("div");
        cell.style.cssText = "flex:1 1 0%;min-width:0;display:flex;justify-content:center;";
        const item = document.createElement("div");
        item.style.cssText = `width:${px(c.childW)};height:45px;flex-shrink:0;`;
        cell.appendChild(item);
        cells.push(cell);
        items.push(item);
      }
      const box = build(`width:${px(c.w)};gap:${px(c.gap)};`, cells);
      const origin = box.getBoundingClientRect().x;
      out.even.push({ ...c, cells: cells.map((el) => { const r = rect(el); return [r[0] - origin, r[2]]; }), items: items.map((el) => rect(el)[0] - origin) });
      box.remove();
    }
    for (const c of sp.between) {
      const items = [];
      for (let i = 0; i < c.n; i += 1) {
        const item = document.createElement("div");
        item.style.cssText = `width:${px(c.childW)};height:20px;flex-shrink:0;`;
        items.push(item);
      }
      const box = build(`width:${px(c.w)};justify-content:space-between;`, items);
      const origin = box.getBoundingClientRect().x;
      out.between.push({ ...c, items: items.map((el) => rect(el)[0] - origin) });
      box.remove();
    }
    for (const c of sp.center) {
      const item = document.createElement("div");
      item.style.cssText = `width:${px(c.childW)};height:20px;flex-shrink:0;`;
      const box = build(`width:${px(c.w)};justify-content:center;`, [item]);
      const origin = box.getBoundingClientRect().x;
      out.center.push({ ...c, items: [rect(item)[0] - origin] });
      box.remove();
    }
    for (const c of sp.grow) {
      const items = c.items.map((spec2) => {
        const item = document.createElement("div");
        item.style.cssText = spec2.basis == null ? "flex:1 1 0%;min-width:0;height:20px;" : `flex:0 0 ${px(spec2.basis)};height:20px;`;
        return item;
      });
      const box = build(`width:${px(c.w)};gap:${px(c.gap)};`, items);
      const origin = box.getBoundingClientRect().x;
      out.grow.push({ w: c.w, gap: c.gap, shape: c.items, items: items.map((el) => { const r = rect(el); return [r[0] - origin, r[2]]; }) });
      box.remove();
    }
    host.remove();
    return out;
  }, { spec });
  // 全部换成 1/64 像素的整数（LayoutUnit）；不是 1/64 整数倍的读数说明测量有问题，直接报错
  const U = 64;
  const toU = (px) => {
    const v = px * U;
    if (Math.abs(v - Math.round(v)) > 1e-6) throw new Error(`读数 ${px} 不是 1/64 的整数倍`);
    return Math.round(v);
  };
  const report = {
    note: "Chromium 弹性布局的分数位置，单位 1/64 px（LayoutUnit），相对容器左缘；B-025 的 kit 摆放规则 L4 用例的期望值，由 probe-flex-units.mjs 量出",
    chromium: await win.evaluate(() => navigator.userAgent),
    unit: U,
    // [容器宽, 格数, 间距, 孩子宽, [各格宽], [各孩子左缘]]
    even: result.even.map((c) => [toU(c.w), c.n, toU(c.gap), toU(c.childW), c.cells.map(([, w]) => toU(w)), c.items.map(toU)]),
    // [容器宽, 孩子数, 孩子宽, [各孩子左缘]]
    between: result.between.map((c) => [toU(c.w), c.n, toU(c.childW), c.items.map(toU)]),
    // [容器宽, 孩子宽, 孩子左缘]
    center: result.center.map((c) => [toU(c.w), toU(c.childW), toU(c.items[0])]),
    // [容器宽, 间距, [各项 flex-basis，弹性项（flex: 1 1 0%）记 -1], [[各项左缘, 各项宽]]]
    grow: result.grow.map((c) => [toU(c.w), toU(c.gap), c.shape.map((s) => (s.basis == null ? -1 : toU(s.basis))), c.items.map(([x, w]) => [toU(x), toU(w)])]),
  };
  const text = JSON.stringify(report);
  if (outFile) {
    fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
    fs.writeFileSync(outFile, text);
  }
  console.log(`even ${report.even.length}，between ${report.between.length}，center ${report.center.length}，grow ${report.grow.length}；${(text.length / 1024).toFixed(0)} KiB`);
} finally {
  await session.close();
}
