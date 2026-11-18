#!/usr/bin/env node
/**
 * 在 React 参照里量原生 <input type="range"> 的指针取值（台账 B-019）：
 * 显示设置页有两枚滑块（背景可见度 0–100 步长 1，虚化强度 0–50 步长 0.5）。把输入框的宽度固定成几个已知值，
 * 在几个横向位置上点击、拖动，读出 el.value，作为 Viva 滑块（kit.RangeInput）L4 指针用例的期望值。
 *
 * 取值规则（Chromium 的 SliderThumbElement，本探针量的是结果，不依赖这条推断）：滑块头 14px，行程 = 宽度 − 14；
 * 点轨道时滑块头中心跳到指针处并开始拖动；点在滑块头上不跳，此后每次移动都让中心对齐到指针（没有抓取偏移）；
 * 值取最近的步长倍数。另外量了滑块头的命中范围（二维网格）、拖出输入框后的悬停放大，以及键盘（方向键、PageUp/PageDown、Home/End、修饰键）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-range-pointer.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";
import { getScene } from "../scenes/react-scenes.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);

// 固定的输入框宽度（CSS px）。奇数宽度让行程出现小数，能暴露取整方式。
const WIDTHS = [400, 313, 200];
// 点击位置：距输入框左缘的偏移，按宽度换算（负数表示距右缘）。
const CLICKS = [0, 1, 3, 6, 7, 8, 14, 21, 50, 0.25, 0.5, 0.75, -21, -14, -8, -7, -6, -3, -1, -0];
// 滑块头命中网格：相对滑块头中心的横向偏移与相对输入框上缘的纵向位置（输入框高 20）。
const HIT_DX = [-9, -8, -7.75, -7.5, -7.25, -7, -6.75, -6.5, -6, -5, -3, -2, 2, 3, 5, 6, 6.5, 6.75, 7, 7.25, 7.5, 7.75, 8, 9];
const HIT_DY = Array.from({ length: 21 }, (_, i) => i);

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: "light-royal-blue-medium-1280x800", widths: WIDTHS, sliders: [] };
try {
  const { page: win } = session;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);
  await getScene("settings.appearance").enter({ win, session });
  const inputs = win.locator('input[type="range"]');
  const count = await inputs.count();
  if (count < 2) throw new Error(`显示设置页只找到 ${count} 枚滑块，预期 2 枚`);
  for (let k = 0; k < count; k += 1) {
    const input = inputs.nth(k);
    const spec = await input.evaluate((el) => ({ min: el.min, max: el.max, step: el.step, id: el.id }));
    const slider = { ...spec, runs: [] };
    for (const width of WIDTHS) {
      await input.evaluate((el, w) => {
        el.style.width = `${w}px`;
        el.style.maxWidth = "none";
      }, width);
      await input.scrollIntoViewIfNeeded();
      await win.waitForTimeout(150);
      const box = await input.boundingBox();
      if (!box) throw new Error("滑块没有外框");
      const cy = box.y + box.height / 2;
      const readValue = () => input.evaluate((el) => el.value);
      const clicks = [];
      for (const raw of CLICKS) {
        // 偏移：整数（含 0）为距左缘 px，|x| < 1 的小数为宽度比例，负数或 -0 为距右缘 px。
        const fromRight = raw < 0 || Object.is(raw, -0);
        const dx = Math.abs(raw) > 0 && Math.abs(raw) < 1 ? raw * box.width : fromRight ? box.width + raw : raw;
        // 滑块头在哪一端就先把它放到对端，保证点击落在轨道上而不是滑块头上（点滑块头不跳）。
        await input.focus();
        await win.keyboard.press(dx < box.width / 2 ? "End" : "Home");
        await win.mouse.move(box.x + dx, cy);
        await win.mouse.down();
        await win.mouse.up();
        clicks.push({ dx: Math.round(dx * 1000) / 1000, value: await readValue() });
      }
      // 拖动：从滑块头中心按下（Home：中心在 左缘 + 7），移到若干位置，不松手时逐点读值，最后松手。
      const drags = [];
      await input.focus();
      await win.keyboard.press("Home");
      await win.mouse.move(box.x + 7, cy);
      await win.mouse.down();
      for (const to of [30, 100, box.width / 2, box.width - 30, box.width - 7, box.width + 40, -40]) {
        await win.mouse.move(box.x + to, cy, { steps: 4 });
        drags.push({ to: Math.round(to * 1000) / 1000, value: await readValue() });
      }
      await win.mouse.up();
      // 带偏移抓取：滑块头在 Home 位置，在它中心右侧 5px（仍在 14px 滑块头内）按下，向右拖 100px。
      await input.focus();
      await win.keyboard.press("Home");
      await win.mouse.move(box.x + 12, cy);
      await win.mouse.down();
      await win.mouse.move(box.x + 112, cy, { steps: 4 });
      const grabbed = await readValue();
      await win.mouse.up();
      // 点在滑块头上（Home 位置，滑块头占 0–14）：按下不动、松开，值是否变化（点轨道会跳，点滑块头按理不跳）。
      const thumbPress = [];
      for (const at of [8, 10, 13]) {
        await input.focus();
        await win.keyboard.press("Home");
        await win.mouse.move(box.x + at, cy);
        await win.mouse.down();
        const down = await readValue();
        await win.mouse.up();
        thumbPress.push({ at, afterDown: down, afterUp: await readValue() });
      }
      // 滑块头的命中范围：值放在正中，在滑块头中心附近的 (dx, dy) 网格上按下不动、读值——
      // 值不变说明按在滑块头上（不跳），变了说明按在输入框本身（跳到指针处）。|dx| < 2 时跳不跳读数相同，不判。
      // 只在第一个宽度上量（几何与宽度无关）。
      let thumbHit = null;
      if (width === WIDTHS[0]) {
        thumbHit = { dxs: HIT_DX, dys: HIT_DY, mid: null, boxX: box.x, boxY: box.y, inside: [] };
        for (const dy of HIT_DY) {
          const row = [];
          for (const dx of HIT_DX) {
            await input.evaluate((el) => {
              const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
              setter.call(el, String((Number(el.min) + Number(el.max)) / 2));
              el.dispatchEvent(new Event("input", { bubbles: true }));
            });
            const mid = await readValue();
            thumbHit.mid = mid;
            const center = 7 + 0.5 * (box.width - 14);
            await win.mouse.move(box.x + center + dx, box.y + dy);
            await win.mouse.down();
            const down = await readValue();
            await win.mouse.up();
            row.push(down === mid);
          }
          thumbHit.inside.push(row);
        }
      }
      // 点在滑块头上之后再动一点点（1px）：值是否立刻对齐到指针。
      await input.focus();
      await win.keyboard.press("Home");
      await win.mouse.move(box.x + 13, cy);
      await win.mouse.down();
      await win.mouse.move(box.x + 14, cy);
      const nudge = await readValue();
      await win.mouse.up();
      // 拖出输入框之外：按在滑块头上、拖到输入框上方很远，读输入框是否还匹配 :hover（globals.css 里滑块头的放大是
      // input[type=range]:hover::-webkit-slider-thumb { transform: scale(1.12) }），再看松开之后、指针移离之后有没有回到常态。
      // 只在第一个宽度上量。
      let dragHover = null;
      if (width === WIDTHS[0]) {
        const hovered = () => input.evaluate((el) => el.matches(":hover"));
        await input.focus();
        await win.keyboard.press("Home");
        await win.mouse.move(box.x + 7, box.y - 200); // 先在远处，让悬停归零
        await win.mouse.move(box.x + 7, cy);
        const overThumb = await hovered();
        await win.mouse.down();
        await win.mouse.move(box.x + 100, box.y - 120, { steps: 4 });
        const draggedOutside = await hovered();
        await win.mouse.up();
        const afterUp = await hovered();
        await win.mouse.move(box.x + 100, box.y - 200);
        dragHover = { overThumb, draggedOutside, afterUp, away: await hovered() };
      }
      // 键盘：值放在正中（或指定起点），聚焦后按键，读值。与 Viva 的 SliderBase 对照：它的 Shift+方向键与
      // PageUp/PageDown 按 10 个值单位走，原生不是。只在第一个宽度上量。
      let keys = null;
      if (width === WIDTHS[0]) {
        keys = [];
        const setValue = (v) => input.evaluate((el, val) => {
          const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
          setter.call(el, String(val));
          el.dispatchEvent(new Event("input", { bubbles: true }));
        }, v);
        const mid = (Number(spec.min) + Number(spec.max)) / 2;
        const cases = [
          ["ArrowRight", mid], ["ArrowLeft", mid], ["ArrowUp", mid], ["ArrowDown", mid],
          ["PageUp", mid], ["PageDown", mid], ["Home", mid], ["End", mid],
          ["Shift+ArrowRight", mid], ["Shift+ArrowLeft", mid], ["Control+ArrowRight", mid], ["Control+Shift+ArrowRight", mid],
          ["Shift+Home", mid], ["Shift+End", mid], ["Control+PageUp", mid], ["Shift+PageDown", mid], ["Alt+ArrowRight", mid],
          ["ArrowLeft", Number(spec.min)], ["ArrowRight", Number(spec.max)],
          ["PageUp", Number(spec.max) - 1], ["PageDown", Number(spec.min) + 1],
        ];
        for (const [key, start] of cases) {
          await setValue(start);
          await input.focus();
          await win.keyboard.press(key);
          keys.push({ key, from: await readValue(), start: String(start) });
        }
      }
      slider.runs.push({ width, boxWidth: box.width, clicks, drags, grab: { downAt: 12, moveTo: 112, value: grabbed }, thumbPress, thumbHit, dragHover, keys, nudge: { downAt: 13, moveTo: 14, value: nudge } });
    }
    report.sliders.push(slider);
  }
} finally {
  await session.close();
}
const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(outFile, text);
}
for (const s of report.sliders) {
  console.log(`滑块 ${s.id}：min=${s.min} max=${s.max} step=${s.step}`);
  for (const r of s.runs) {
    console.log(`  宽 ${r.width}：` + r.clicks.map((c) => `${c.dx}→${c.value}`).join("  "));
    console.log(`  拖动：` + r.drags.map((d) => `${d.to}→${d.value}`).join("  ") + `  抓取偏移 12→112：${r.grab.value}`);
    if (r.thumbHit) {
      console.log(`  滑块头命中网格（#=按在滑块头上不跳，.=跳；列 dx：${r.thumbHit.dxs.join(" ")}）：`);
      r.thumbHit.inside.forEach((row, i) => console.log(`    y${String(r.thumbHit.dys[i]).padStart(2)} ` + row.map((v) => (v ? "#" : ".")).join("")));
    }
    if (r.dragHover) console.log(`  输入框 :hover：滑块头上 ${r.dragHover.overThumb}，拖到输入框外 ${r.dragHover.draggedOutside}，松开后 ${r.dragHover.afterUp}，指针移离后 ${r.dragHover.away}`);
    if (r.keys) console.log(`  键盘：` + r.keys.map((k) => `${k.key}(${k.start})→${k.from}`).join("  "));
    console.log(`  点滑块头：` + r.thumbPress.map((p) => `${p.at}→按下${p.afterDown}/松开${p.afterUp}`).join("  ") + `  13 按下后移到 14：${r.nudge.value}`);
  }
}
