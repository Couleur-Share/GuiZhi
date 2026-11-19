#!/usr/bin/env node
/**
 * 在 React 参照里量侧栏行「更多操作」菜单的锚点（M2-1，对应 L4 TestKeyboardOpenedMenuAnchorsAtButtonBottomLeft）：
 *   - 鼠标点按钮：菜单开在光标处（menuAnchor 取 clientX / clientY）；
 *   - 键盘（Enter、Space）激活按钮：渲染层的 menuAnchor 在 clientX、clientY 都为 0 时改取按钮的左下角——
 *     读源码得出的前提是「键盘激活的 click 事件坐标是 (0, 0)」，Chromium 是否真的这样给，要实测，不能只读源码。
 *   - 右键：菜单开在光标处。
 * 每种方式记录：click 事件的 clientX / clientY（页面里挂一个捕获阶段的监听器取读数）、按钮外框、菜单外框。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-library-keymenu.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
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

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: "light-royal-blue-medium-1280x800", cases: [] };
const round = (n) => Math.round(n * 100) / 100;

try {
  const { page: win } = session;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);
  await enterLibrary(session, {});
  // 捕获阶段记下每个 click / contextmenu 事件的坐标：键盘激活时事件坐标到底是多少，直接读出来。
  await win.evaluate(() => {
    window.__events = [];
    for (const type of ["click", "contextmenu"]) {
      document.addEventListener(
        type,
        (e) => window.__events.push({ type, x: e.clientX, y: e.clientY, detail: e.detail }),
        true,
      );
    }
  });
  const row = win.getByRole("button", { name: "学习笔记", exact: true }).first().locator("xpath=ancestor::*[contains(@class,'group/row')][1]");
  const more = row.getByRole("button", { name: "更多操作", exact: true });

  const snapshot = async (label, how) => {
    await win.evaluate(() => { window.__events.length = 0; });
    await how();
    await win.getByRole("menu").waitFor({ state: "visible" });
    await win.waitForTimeout(300); // 入场动画结束
    const data = await win.evaluate(() => {
      const menu = document.querySelector('[role="menu"]');
      const m = menu.getBoundingClientRect();
      return { events: window.__events.slice(), menu: [m.x, m.y, m.width, m.height] };
    });
    const b = await more.boundingBox();
    report.cases.push({
      label,
      events: data.events,
      button: [round(b.x), round(b.y), round(b.width), round(b.height)],
      buttonBottomLeft: [round(b.x), round(b.y + b.height)],
      buttonCenter: [round(b.x + b.width / 2), round(b.y + b.height / 2)],
      menu: data.menu.map(round),
    });
    await win.keyboard.press("Escape");
    await win.getByRole("menu").waitFor({ state: "detached" });
    await win.waitForTimeout(200);
  };

  // 1. 键盘：聚焦按钮后按 Enter
  await snapshot("键盘 Enter", async () => {
    await win.keyboard.press("Tab");
    await more.focus();
    await win.keyboard.press("Enter");
  });
  // 2. 键盘：聚焦按钮后按空格
  await snapshot("键盘 Space", async () => {
    await more.focus();
    await win.keyboard.press("Space");
  });
  // 3. 鼠标：点按钮中心
  await snapshot("鼠标点击按钮", async () => {
    await more.click();
  });
  // 4. 右键：在行内 (40, 16) 处
  await snapshot("右键行", async () => {
    await row.click({ button: "right", position: { x: 40, y: 16 } });
  });
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
    `${c.label}：click 事件坐标 ${JSON.stringify(c.events.map((e) => [e.x, e.y]))}，按钮左下角 ${JSON.stringify(c.buttonBottomLeft)}，` +
      `按钮中心 ${JSON.stringify(c.buttonCenter)}，菜单左上角 [${c.menu[0]}, ${c.menu[1]}]（${c.menu[2]}×${c.menu[3]}）`,
  );
}
