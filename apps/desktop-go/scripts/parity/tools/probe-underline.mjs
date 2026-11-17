#!/usr/bin/env node
/**
 * 在 React 参照（设备缩放钉死为 1）里量 `text-decoration: underline`（hover:underline）的栅格位置：
 * 下划线在行盒顶之下第几行、几行厚、横向从哪到哪，随字号、行高、行盒顶的小数部分怎么变。
 *
 * 动机：Viva 的文字没有下划线样式（台账 B-012）；要自己画一条线，位置必须与 Chromium 一致。
 * Chromium 的位置取自字体的下划线度量（`auto`），落到设备像素时四舍五入，逐组实测比推导可靠。
 *
 * 做法：在页面上盖一层白底浮层（fixed，不碰真实内容，测完即删），逐格摆一行带下划线的文字（黑字白底），
 * 截图后在页面里数「整行大半是黑色」的行——那就是下划线（字形很难占满一行宽度的 90%）。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/tools/probe-underline.mjs [--out <文件>]
 *
 * 缺省只打印汇总；给 --out 才写 JSON。前提：已有 React 参照构建（node apps/desktop-go/scripts/parity/build-react-ref.mjs）。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { openReactSession } from "../lib/react-session.mjs";

const out = (() => {
  const i = process.argv.indexOf("--out");
  return i >= 0 ? path.resolve(process.argv[i + 1]) : null;
})();

// 渲染层里带 hover:underline 的文字：任务标题（text-sm，三档根字号 14/16/18 → 12.25/14/15.75px，行高 1.25rem）、
// 阶段耗时摘要（text-[11px]，行高继承 1.5 倍）。再补几个常见字号，看规律是否随字号单调。
const SIZES = [
  { size: 11, line: 16.5 },
  { size: 12, line: 18 },
  { size: 12.25, line: 17.5 },
  { size: 13, line: 19.5 },
  { size: 14, line: 20 },
  { size: 15.75, line: 22.5 },
  { size: 16, line: 24 },
  { size: 18, line: 27 },
];
const FRACS = [0, 0.25, 0.5, 0.75];
const TEXT = "本地优先软件的七个理想 yt-dlp";

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const cases = [];
  for (const s of SIZES) for (const frac of FRACS) cases.push({ ...s, frac });

  const viewport = await session.page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }));
  const CELL_W = 360;
  const CELL_H = 50;
  const COLS = Math.floor(viewport.w / CELL_W);
  const ROWS = Math.floor(viewport.h / CELL_H);
  await session.page.evaluate(() => {
    const overlay = document.createElement("div");
    overlay.id = "probe-overlay";
    overlay.style.cssText = "position:fixed;left:0;top:0;width:100vw;height:100vh;background:#fff;z-index:2147483647;overflow:hidden";
    document.body.appendChild(overlay);
  });

  const all = [];
  let dprSeen = 1;
  for (let at = 0; at < cases.length; at += COLS * ROWS) {
    const batch = cases.slice(at, at + COLS * ROWS);
    const placed = await session.page.evaluate(async ({ cs, COLS, CELL_W, CELL_H, TEXT }) => {
      const family = getComputedStyle(document.body).fontFamily;
      const overlay = document.getElementById("probe-overlay");
      overlay.replaceChildren();
      const placed = cs.map((c, i) => {
        const col = i % COLS;
        const row = Math.floor(i / COLS);
        const left = 4 + col * CELL_W;
        const top = 4 + row * CELL_H + c.frac;
        const box = document.createElement("div");
        box.style.cssText = `position:absolute;left:${left}px;top:${top}px;white-space:nowrap;color:#000;font:400 ${c.size}px/${c.line}px ${family}`;
        const span = document.createElement("span");
        span.style.textDecoration = "underline";
        span.textContent = TEXT;
        box.appendChild(span);
        overlay.appendChild(box);
        return { left, top, box };
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      return placed.map((p) => {
        const r = p.box.firstChild.getBoundingClientRect();
        return { left: p.left, top: p.top, textLeft: r.left, textWidth: r.width };
      });
    }, { cs: batch, COLS, CELL_W, CELL_H, TEXT });

    const shot = await session.page.screenshot({ type: "png" });
    const base64 = shot.toString("base64");
    const ink = await session.page.evaluate(async ({ base64, cases, placed }) => {
      const img = new Image();
      img.src = `data:image/png;base64,${base64}`;
      await img.decode();
      const canvas = document.createElement("canvas");
      canvas.width = img.width;
      canvas.height = img.height;
      const ctx = canvas.getContext("2d", { willReadFrequently: true });
      ctx.drawImage(img, 0, 0);
      const dpr = img.width / window.innerWidth;
      const res = cases.map((c, i) => {
        const p = placed[i];
        const x0 = Math.floor(p.textLeft) - 2;
        const w = Math.ceil(p.textWidth) + 6;
        const y0 = Math.floor(p.top) - 2;
        const h = Math.ceil(c.line) + 8;
        const data = ctx.getImageData(x0, y0, w, h).data;
        const darkness = (o) => 255 - (data[o] + data[o + 1] + data[o + 2]) / 3;
        const rows = [];
        for (let j = 0; j < h; j += 1) {
          let dark = 0;
          let first = -1;
          let last = -1;
          let weight = 0;
          for (let k = 0; k < w; k += 1) {
            const d = darkness((j * w + k) * 4);
            weight += d / 255;
            if (d > 128) {
              dark += 1;
              if (first < 0) first = k;
              last = k;
            }
          }
          rows.push({ y: y0 + j, dark, first: first < 0 ? -1 : x0 + first, last: last < 0 ? -1 : x0 + last, weight });
        }
        // 下划线行：大半个文字宽度都是深色
        const ul = rows.filter((r) => r.dark >= p.textWidth * 0.9);
        // 边缘列的覆盖度：第一列、最后一列的灰度，看横向边缘是否取整
        const row = ul.length ? ul[0].y - y0 : -1;
        const edge = (col) => (row < 0 ? -1 : Math.round(darkness(((row) * w + (col - x0)) * 4)));
        return {
          ...c,
          top: p.top,
          textLeft: p.textLeft,
          textWidth: p.textWidth,
          ulRows: ul.map((r) => r.y),
          ulFirst: ul.length ? ul[0].first : -1,
          ulLast: ul.length ? ul[0].last : -1,
          edgeLeft: edge(Math.floor(p.textLeft)),
          edgeRight: edge(Math.floor(p.textLeft + p.textWidth)),
        };
      });
      return { dpr, res };
    }, { base64, cases: batch, placed });
    dprSeen = ink.dpr;
    all.push(...ink.res);
  }

  if (dprSeen !== 1) throw new Error(`截图缩放 ${dprSeen} 不是 1，参照被宿主缩放污染`);
  for (const r of all) {
    console.log(
      `size=${r.size} line=${r.line} frac=${r.frac}: 行盒顶=${r.top} 下划线行=${JSON.stringify(r.ulRows)}` +
        `（相对 round(行盒顶) ${r.ulRows.length ? r.ulRows[0] - Math.round(r.top) : "?"}）` +
        ` 横向 ${r.ulFirst}..${r.ulLast}（文字 ${r.textLeft.toFixed(2)} + ${r.textWidth.toFixed(2)}）边缘灰度 ${r.edgeLeft}/${r.edgeRight}`,
    );
  }
  if (out) {
    fs.mkdirSync(path.dirname(out), { recursive: true });
    fs.writeFileSync(out, `${JSON.stringify({ note: "由 scripts/parity/tools/probe-underline.mjs 生成；设备缩放钉死为 1；字重 400。", res: all }, null, 1)}\n`);
    console.log(`已写 ${all.length} 组到 ${out}`);
  }
} finally {
  await session.close();
}
