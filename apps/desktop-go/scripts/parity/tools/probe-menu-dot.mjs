#!/usr/bin/env node
/**
 * 在 React 参照里量「标签颜色」子菜单里那 10 个颜色圆点（M2-1）：
 * 渲染层 SidebarLibraryPanel 给每个颜色项的 icon 是 `<span class="h-3 w-3 rounded-full bg-…">`，
 * 而 ContextMenu 把 icon 放进 `<span aria-hidden class="w-4 h-4 shrink-0">`（行内元素的子 span）。
 * 行内元素的 width / height 不生效，所以圆点是不是真的画出来了、画多大，要实测，不能凭读源码判断。
 *
 * 量：每个子项里图标外壳与圆点的 display、外框、背景色；父项「颜色」的图标外壳；子项标签文字的左缘。
 * 同时把子菜单区域截成 PNG 放在 --out 旁边，便于肉眼核对。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-menu-dot.mjs [--out <文件.json>] [--variant <变体键>]
 * 前提：已有 React 参照构建；会话用一次性 userData、空库（探针自己建一个标签）。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", "D:/Project/GuiZhi-bench/parity/probes/menu-dot.json");
const variantKey = arg("--variant", "light-royal-blue-medium-1280x800");
const TAG_NAME = "Alpha";

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: variantKey, rows: [] };

try {
  const { page: win } = session;
  await win.evaluate(async (name) => window.api.tag.create({ name, colorKey: "blue" }), TAG_NAME);
  await applyVariant(session, parseVariantKey(variantKey, matrix), matrix);
  await win.getByRole("button", { name: /未使用的标签/ }).click();
  await win.waitForTimeout(150);
  await win.evaluate(({ name, x, y }) => {
    const btn = [...document.querySelectorAll("button[aria-pressed]")].find((b) => b.textContent.trim() === name);
    btn.parentElement.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: x, clientY: y }));
  }, { name: TAG_NAME, x: 150, y: 300 });
  await win.getByRole("menu").waitFor({ state: "visible" });
  const parent = win.getByRole("menuitem", { name: "颜色", exact: true });
  const box = await parent.boundingBox();
  await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await win.waitForTimeout(200);

  report.rows = await win.evaluate(() => {
    const rect = (el) => {
      const r = el.getBoundingClientRect();
      return [r.x, r.y, r.width, r.height].map((v) => Math.round(v * 100) / 100);
    };
    const rows = [];
    for (const btn of document.querySelectorAll('[role="menuitem"]')) {
      const wrapper = btn.querySelector(":scope > span[aria-hidden]");
      const dot = wrapper?.firstElementChild ?? null;
      const label = btn.querySelector("span.truncate");
      const dotStyle = dot ? getComputedStyle(dot) : null;
      rows.push({
        text: label?.textContent ?? btn.textContent,
        row: rect(btn),
        wrapper: wrapper ? { rect: rect(wrapper), display: getComputedStyle(wrapper).display } : null,
        child: dot ? {
          tag: dot.tagName, cls: dot.className, rect: rect(dot), display: dotStyle.display,
          width: dotStyle.width, height: dotStyle.height, bg: dotStyle.backgroundColor, radius: dotStyle.borderRadius,
        } : null,
        labelLeft: label ? rect(label)[0] : null,
      });
    }
    return rows;
  });

  // 子菜单区域截图：取「颜色」父项的右侧到最后一个子项的包围盒。
  const kids = report.rows.filter((r) => r.child);
  if (kids.length) {
    const xs = kids.map((k) => k.row[0]);
    const ys = kids.map((k) => k.row[1]);
    const right = Math.max(...kids.map((k) => k.row[0] + k.row[2]));
    const bottom = Math.max(...kids.map((k) => k.row[1] + k.row[3]));
    const clip = { x: Math.min(...xs) - 8, y: Math.min(...ys) - 8, width: right - Math.min(...xs) + 16, height: bottom - Math.min(...ys) + 16 };
    fs.mkdirSync(path.dirname(outFile), { recursive: true });
    await win.screenshot({ path: outFile.replace(/\.json$/, ".png"), clip });
  }
  fs.mkdirSync(path.dirname(outFile), { recursive: true });
  fs.writeFileSync(outFile, `${JSON.stringify(report, null, 1)}\n`);
  console.log(`已写出 ${outFile}`);
  for (const r of report.rows) {
    console.log(`${r.text.padEnd(8)} wrapper=${JSON.stringify(r.wrapper)} child=${r.child ? `${r.child.tag}.${r.child.display} ${r.child.width}x${r.child.height} ${r.child.bg}` : "无"}`);
  }
} finally {
  await session.close();
}
