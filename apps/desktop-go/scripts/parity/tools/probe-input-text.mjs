#!/usr/bin/env node
/**
 * 在 React 参照（设备缩放钉死为 1）里量 <input> 里文字的栅格行位置，对照同一行盒顶的普通 div 文字。
 *
 * 动机：Chromium 排版里的文字（div 里的行）基线 = 行盒顶 + 取整的 ascent + 半行距，落到设备像素时对「行盒顶」四舍五入
 * （viva/kit/baseline.go 的模型）。<input> 的内部编辑器是单独的滚动容器，偏移是否按同一规则取整没有依据——
 * 2026-11-16 在 app.window.page-settings 的大字号档量到搜索占位符比同基线的普通文字低 1 行，所以逐组实测。
 *
 * 做法：在页面上盖一层白底浮层（fixed，不碰真实内容，测完即删），摆若干 <input>（有值 / 只有占位符）与一个对照 div：
 * div 的行盒顶 = input 的 (顶 + 边框 + (高 − 2·边框 − 行高)/2)，即几何上编辑器行盒该在的位置。
 * 截图后在页面里数每个元素带内第一行有墨迹的行，输出两者之差。差为 0 说明 input 与普通文字同规则。
 *
 * 用法：
 *   node apps/desktop-go/scripts/parity/tools/probe-input-text.mjs [--grid tiers|mixed|all] [--out <文件>]
 *
 * --grid tiers（缺省）：三档根字号的 text-sm；mixed：字号 × 行高独立交叉；all：两者合并（夹具用这个）。
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

// 默认网格：三档根字号下渲染层 Input 的取值——text-sm（0.875rem / 行高 1.25rem），高度 2rem、2.25rem、2.5rem，边框 1px 或 0。
// --grid mixed：字号与行高独立交叉（含整数字号配小数行高、小数字号配整数行高），用来分清取整规则取决于哪一个。
const GRID = (() => {
  const i = process.argv.indexOf("--grid");
  return i >= 0 ? process.argv[i + 1] : "tiers";
})();
const TIERS = [14, 16, 18];
const HEIGHT_REM = [2, 2.25, 2.5];
const BORDERS = [0, 1];
const FRACS = [0, 0.25, 0.5, 0.75];
const KINDS = ["value", "placeholder"];
const MIXED_SIZES = [10.5, 12, 12.25, 13, 13.5, 14, 15.75];
const MIXED_LINES = [14, 16, 17.5, 18, 20, 21, 22.5, 24];
const MIXED_HEIGHTS = [28, 31.5, 36, 40.5];

const record = readBuildRecord();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
try {
  const cases = [];
  if (GRID === "mixed" || GRID === "all") {
    for (const size of MIXED_SIZES)
      for (const line of MIXED_LINES)
        for (const height of MIXED_HEIGHTS)
          for (const frac of FRACS) cases.push({ root: 0, size, line, height, border: 1, frac, kind: "value" });
  }
  if (GRID !== "mixed") {
    for (const root of TIERS)
      for (const hr of HEIGHT_REM)
        for (const border of BORDERS)
          for (const frac of FRACS)
            for (const kind of KINDS)
              cases.push({ root, size: 0.875 * root, line: 1.25 * root, height: hr * root, border, frac, kind });
  }

  // 浮层：一批一批摆（视口放得下多少摆多少），摆完截图、在页面里数墨迹行。屏幕外的格子读不到像素，必须分批，否则「读不到」会被当成「没墨迹」。
  const viewport = await session.page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }));
  const CELL_W = 130;
  const CELL_H = 60;
  const COLS = Math.floor(viewport.w / CELL_W);
  const ROWS = Math.floor(viewport.h / CELL_H);
  await session.page.evaluate(() => {
    const overlay = document.createElement("div");
    overlay.id = "probe-overlay";
    overlay.style.cssText = "position:fixed;left:0;top:0;width:100vw;height:100vh;background:#fff;z-index:2147483647;overflow:hidden";
    const style = document.createElement("style");
    style.textContent = ".probe-in::placeholder{color:#000;opacity:1}.probe-in{outline:none;appearance:none;-webkit-appearance:none;margin:0}";
    overlay.appendChild(style);
    document.body.appendChild(overlay);
  });

  const all = [];
  let dprSeen = 1;
  for (let at = 0; at < cases.length; at += COLS * ROWS) {
    const batch = cases.slice(at, at + COLS * ROWS);
    const placed = await session.page.evaluate(async ({ cs, COLS, CELL_W, CELL_H }) => {
      const family = getComputedStyle(document.body).fontFamily;
      const overlay = document.getElementById("probe-overlay");
      for (const el of [...overlay.children]) if (el.tagName !== "STYLE") el.remove();
      const placed = cs.map((c, i) => {
        const col = i % COLS;
        const row = Math.floor(i / COLS);
        const left = 4 + col * CELL_W;
        const top = 4 + row * CELL_H + c.frac;
        const input = document.createElement("input");
        input.className = "probe-in";
        input.style.cssText = `position:absolute;left:${left}px;top:${top}px;width:56px;height:${c.height}px;box-sizing:border-box;border:${c.border}px solid #fff;padding:0 0 0 2px;background:#fff;color:#000;font:400 ${c.size}px/${c.line}px ${family}`;
        if (c.kind === "value") input.value = "Hx";
        else input.placeholder = "Hx";
        overlay.appendChild(input);
        const ref = document.createElement("div");
        const lineTop = top + c.border + (c.height - 2 * c.border - c.line) / 2;
        ref.style.cssText = `position:absolute;left:${left + 60}px;top:${lineTop}px;width:56px;height:${c.line}px;line-height:${c.line}px;white-space:nowrap;padding-left:${2 + c.border}px;box-sizing:border-box;color:#000;font:400 ${c.size}px/${c.line}px ${family}`;
        ref.textContent = "Hx";
        overlay.appendChild(ref);
        return { left, top, lineTop };
      });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      return placed;
    }, { cs: batch, COLS, CELL_W, CELL_H });

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
      const scan = (x, y, w, h, fromBottom) => {
        if (x < 0 || y < 0 || x + w > img.width || y + h > img.height) throw new Error(`读取区域越界 ${x},${y},${w},${h}（画面 ${img.width}x${img.height}）`);
        const data = ctx.getImageData(x, y, w, h).data;
        for (let k = 0; k < h; k += 1) {
          const j = fromBottom ? h - 1 - k : k;
          for (let i = 0; i < w; i += 1) {
            const o = (j * w + i) * 4;
            if (data[o] < 250 || data[o + 1] < 250 || data[o + 2] < 250) return y + j;
          }
        }
        return -1;
      };
      const dpr = img.width / window.innerWidth;
      const res = cases.map((c, i) => {
        const p = placed[i];
        const bandTop = Math.floor(p.top) - 2;
        const bandH = Math.ceil(c.height) + 6;
        const x = Math.round(p.left);
        const inputFirst = scan(x, bandTop, 58, bandH, false);
        const refFirst = scan(x + 60, bandTop, 58, bandH, false);
        const inputLast = scan(x, bandTop, 58, bandH, true);
        const refLast = scan(x + 60, bandTop, 58, bandH, true);
        return { ...c, top: p.top, lineTop: p.lineTop, inputFirst, refFirst, inputLast, refLast, delta: inputFirst - refFirst, deltaLast: inputLast - refLast };
      });
      return { dpr, res };
    }, { base64, cases: batch, placed });
    dprSeen = ink.dpr;
    all.push(...ink.res);
  }
  const ink = { dpr: dprSeen, res: all };

  if (ink.dpr !== 1) throw new Error(`截图缩放 ${ink.dpr} 不是 1，参照被宿主缩放污染`);
  const res = ink.res;
  const dist = {};
  for (const r of res) {
    const key = `${r.kind} delta=${r.delta}`;
    dist[key] = (dist[key] ?? 0) + 1;
  }
  console.log("按（种类，input 比对照低的行数）统计：", dist);
  const bad = res.filter((r) => r.delta !== 0 || r.inputFirst < 0);
  console.log(`delta≠0：${bad.length}/${res.length}`);
  for (const r of bad.slice(0, 60))
    console.log(`  root=${r.root} h=${r.height} border=${r.border} frac=${r.frac} ${r.kind}: 行盒顶=${r.lineTop} input 首墨行=${r.inputFirst} 对照=${r.refFirst} delta=${r.delta}`);
  if (out) {
    fs.mkdirSync(path.dirname(out), { recursive: true });
    const keep = ({ size, line, height, border, frac, kind, top, lineTop, inputFirst, refFirst, delta }) => ({ size, line, height, border, frac, kind, top, lineTop, inputFirst, refFirst, delta });
    const note = "由 scripts/parity/tools/probe-input-text.mjs --grid all 生成；设备缩放钉死为 1；字重 400；delta = input 首墨行 − 同行盒顶普通文字首墨行。";
    const lines = res.map((r) => `  ${JSON.stringify(keep(r))}`).join(",\n");
    fs.writeFileSync(out, `{\n "note": ${JSON.stringify(note)},\n "res": [\n${lines}\n ]\n}\n`);
    console.log(`已写 ${res.length} 组到 ${out}`);
  }
} finally {
  await session.close();
}
