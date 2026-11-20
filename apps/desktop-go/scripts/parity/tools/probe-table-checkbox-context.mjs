#!/usr/bin/env node
/**
 * 在 React 参照里量表格行「复选框格」上点右键的行为（台账 B-057，M2-5 开头的携带项）：
 * 渲染层的 onContextMenu 挂在整个 <tr> 上（ItemTableRow.tsx），复选框所在的 <td> 在行里、复选框按钮不拦截 contextmenu，
 * 读源码得出的结论是「在复选框格上右键同样弹出这一行的菜单」；这个结论是读出来的，要在真 Chromium 里实测，不能只读源码。
 *
 * 每个点位记录：右键之后菜单有没有出现、菜单项名字、捕获阶段看到的 contextmenu 事件（目标元素的标签与所在单元格）。
 * 点位：1. 复选框的 16×16 可见方框中心；2. 复选框所在单元格里方框以外的空白（单元格左缘内 2 px）；
 *       3. 对照：「类型」列单元格中心（已知会出菜单）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-table-checkbox-context.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";
import { enterLibrary, leaveLibrary, navLabel } from "../lib/react-library.mjs";

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outFile = arg("--out", null);

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: "light-royal-blue-medium-1280x800", cases: [] };
const round = (n) => Math.round(n * 100) / 100;

try {
  const { page: win } = session;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);
  await enterLibrary(session, { list: { fault: "", failMessage: "probe" } });
  const toggle = win.getByRole("button", { name: navLabel("viewList"), exact: true });
  try {
    await toggle.click({ timeout: 8000 });
  } catch (err) {
    // 找不到视图切换钮时把页面上的按钮名列出来，便于判断是不是默认就在表格视图。
    const names = await win.getByRole("button").evaluateAll((els) => els.map((e) => e.getAttribute("aria-label") || (e.textContent || "").trim()).filter(Boolean));
    console.error("页面上的按钮：", JSON.stringify(names));
    throw err;
  }
  await win.getByTestId("item-table").waitFor({ state: "visible" });
  await win.waitForTimeout(300);

  // 捕获阶段记下 contextmenu 事件：落在哪个元素、哪个单元格里。
  await win.evaluate(() => {
    window.__ctx = [];
    document.addEventListener(
      "contextmenu",
      (e) => {
        const t = e.target;
        const cell = t instanceof Element ? t.closest("td") : null;
        const cells = cell ? Array.from(cell.parentElement.children) : [];
        window.__ctx.push({
          target: t instanceof Element ? t.tagName.toLowerCase() : String(t),
          cellIndex: cell ? cells.indexOf(cell) : -1,
          inRow: Boolean(t instanceof Element && t.closest("[data-item-row]")),
        });
      },
      true,
    );
  });

  const row = win.locator("[data-item-row]").nth(0);
  const box = await row.locator("label span[aria-hidden='true']").first().boundingBox();
  const checkCell = await row.locator("td").nth(0).boundingBox();
  const typeCell = await row.locator("td").nth(4).boundingBox();
  const points = [
    ["复选框方框中心", box.x + box.width / 2, box.y + box.height / 2],
    ["复选框单元格空白（左缘内 2 px）", checkCell.x + 2, checkCell.y + checkCell.height / 2],
    ["对照：「类型」列单元格中心", typeCell.x + typeCell.width / 2, box.y + box.height / 2],
  ];
  for (const [label, x, y] of points) {
    await win.evaluate(() => { window.__ctx.length = 0; });
    await win.mouse.click(Math.floor(x), Math.floor(y), { button: "right" });
    await win.waitForTimeout(500);
    const data = await win.evaluate(() => ({
      events: window.__ctx.slice(),
      menus: Array.from(document.querySelectorAll('[role="menu"]')).map((m) => ({
        items: Array.from(m.querySelectorAll('[role="menuitem"]')).map((i) => (i.textContent || "").trim()),
      })),
    }));
    report.cases.push({ label, point: [Math.floor(x), Math.floor(y)], ...data, menuOpen: data.menus.length > 0 });
    if (data.menus.length > 0) {
      await win.keyboard.press("Escape");
      await win.getByRole("menu").waitFor({ state: "detached" });
      await win.waitForTimeout(200);
    }
  }
  report.geometry = {
    checkbox: [round(box.x), round(box.y), round(box.width), round(box.height)],
    checkCell: [round(checkCell.x), round(checkCell.y), round(checkCell.width), round(checkCell.height)],
  };
} finally {
  try { await leaveLibrary(session); } catch { /* 会话关闭前的清理失败不影响读数 */ }
  await session.close();
}

const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(outFile, text);
}
for (const c of report.cases) {
  console.log(
    `${c.label}：点 ${JSON.stringify(c.point)}，菜单${c.menuOpen ? "出现" : "没有出现"}` +
      (c.menuOpen ? `（${c.menus[0].items.join("、")}）` : "") +
      `；contextmenu 事件 ${JSON.stringify(c.events)}`,
  );
}
console.log(`几何：复选框方框 ${JSON.stringify(report.geometry.checkbox)}，复选框单元格 ${JSON.stringify(report.geometry.checkCell)}`);
