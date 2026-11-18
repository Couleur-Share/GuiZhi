#!/usr/bin/env node
/**
 * 在 React 参照里量「键盘 Tab 能走到哪些控件」（台账 B-030）：
 * 在显示设置页（800x600，内容区放不下全部控件）里连续按 Tab，逐次记录聚焦的元素、它的外框与内容区的滚动位置。
 * 结论要回答两件事：
 *   1. Tab 环是否包含滚出可视区的控件（浏览器：包含，Viva：不包含，见 viva/kit/focus_reach_test.go）；
 *   2. 走到可视区之外的控件时，页面是否自己把它滚进视野。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-tab-reach.mjs [--out <文件>] [--presses 40]
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
const presses = Number(arg("--presses", "40"));

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: "light-royal-blue-medium-800x600", presses: [] };
try {
  const { page: win } = session;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);
  await getScene("settings.appearance").enter({ win, session });
  await win.mouse.move(2, 2);
  await win.waitForTimeout(300);
  const scroller = win.locator('div:has(> [data-testid="settings-content-shell"])');
  report.viewport = await scroller.evaluate((el) => ({ clientHeight: el.clientHeight, scrollHeight: el.scrollHeight }));
  for (let i = 1; i <= presses; i += 1) {
    await win.keyboard.press("Tab");
    const row = await win.evaluate(() => {
      const el = document.activeElement;
      if (!el || el === document.body) return { tag: "body" };
      const r = el.getBoundingClientRect();
      const box = document.querySelector('[data-testid="settings-content-shell"]')?.parentElement;
      const area = box?.getBoundingClientRect();
      return {
        tag: el.tagName.toLowerCase(),
        name: el.getAttribute("aria-label") || (el.textContent || "").trim().slice(0, 20),
        top: Math.round(r.top * 10) / 10,
        bottom: Math.round(r.bottom * 10) / 10,
        areaTop: area ? Math.round(area.top * 10) / 10 : null,
        areaBottom: area ? Math.round(area.bottom * 10) / 10 : null,
        scrollTop: box ? Math.round(box.scrollTop * 10) / 10 : null,
      };
    });
    report.presses.push({ n: i, ...row });
  }
  // 摘要：走到过的不同控件个数、其中走到时本来在可视区之外的个数、最大滚动位置。
  const named = report.presses.filter((r) => r.tag !== "body");
  report.distinct = new Set(named.map((r) => `${r.tag}:${r.name}`)).size;
  report.maxScrollTop = Math.max(0, ...named.map((r) => r.scrollTop ?? 0));
  report.allVisibleWhenFocused = named.every((r) => r.areaTop == null || (r.top >= r.areaTop - 1 && r.bottom <= r.areaBottom + 1));
} finally {
  await session.close();
}
const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(outFile, text);
}
console.log(
  `800x600 显示设置页：内容区高 ${report.viewport.clientHeight}、内容总高 ${report.viewport.scrollHeight}；按 ${presses} 次 Tab，走到 ${report.distinct} 个不同控件，` +
    `最大滚动位置 ${report.maxScrollTop}px，聚焦时控件都在可视区内：${report.allVisibleWhenFocused}`,
);
console.log(named_summary(report));
function named_summary(r) {
  return r.presses
    .slice(0, 24)
    .map((p) => `#${p.n} ${p.tag}${p.name ? `「${p.name}」` : ""} top=${p.top} scrollTop=${p.scrollTop}`)
    .join("\n");
}
