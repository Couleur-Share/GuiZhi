#!/usr/bin/env node
/**
 * 在 React 参照里量批量条上「移动到合集」菜单与「批量打标签」浮层的锚点（M2-3，对应 L4 的键盘激活锚点测试）：
 *   - 鼠标点按钮：渲染层取 event.clientX / clientY，菜单与浮层开在光标处（夹进视口）；
 *   - 键盘（Enter、Space）激活：浏览器合成的 click 事件坐标是 (0, 0)（M2-1 的 probe-library-keymenu 已实测）。侧栏的 menuAnchor
 *     在坐标为 0 时会改取按钮的左下角，批量条的菜单与浮层没有这一步——读源码得出的结论是「开在原点再夹进视口」，
 *     这里实测它的外框，不能只读源码。
 * 每种方式记录：click 事件的 clientX / clientY、按钮外框、菜单或浮层外框。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-library-bulkmenu.mjs [--out <文件>]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据。
 */
import fs from "node:fs";
import path from "node:path";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { applyVariant } from "../lib/react-state.mjs";
import { enterLibrary, leaveLibrary } from "../lib/react-library.mjs";
import { CARD_ANCHOR, card } from "../scenes/react-library-list-scenes.mjs";

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
  await enterLibrary(session, { list: { fault: "", failMessage: "" } });
  // 捕获阶段记下每个 click 事件的坐标。
  await win.evaluate(() => {
    window.__events = [];
    document.addEventListener("click", (e) => window.__events.push({ x: e.clientX, y: e.clientY, detail: e.detail }), true);
  });
  // 选两张卡片：批量条出现。Esc 关菜单的同时也会清掉多选（批量条的 window keydown 监听），所以每个案例开始前按需重选。
  const selectTwo = async () => {
    for (const title of ["产品设计原则：极简与克制", "如何构建本地优先的个人知识库"]) {
      const box = await card(win, title).boundingBox();
      await win.keyboard.down("Control");
      await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
      await win.keyboard.up("Control");
      await win.waitForTimeout(250);
    }
  };

  const snapshot = async (label, buttonName, role, how) => {
    const button = win.getByRole("button", { name: buttonName, exact: true });
    if ((await button.count()) === 0) await selectTwo();
    await win.evaluate(() => { window.__events.length = 0; });
    await how(button);
    await win.getByRole(role).waitFor({ state: "visible" });
    await win.waitForTimeout(300); // 入场动画结束
    const data = await win.evaluate((r) => {
      const el = document.querySelector(`[role="${r}"]`);
      const b = el.getBoundingClientRect();
      return { events: window.__events.slice(), panel: [b.x, b.y, b.width, b.height] };
    }, role);
    const b = await button.boundingBox();
    report.cases.push({
      label,
      events: data.events,
      button: [round(b.x), round(b.y), round(b.width), round(b.height)],
      buttonBottomLeft: [round(b.x), round(b.y + b.height)],
      buttonCenter: [round(b.x + b.width / 2), round(b.y + b.height / 2)],
      panel: data.panel.map(round),
    });
    await win.keyboard.press("Escape");
    await win.getByRole(role).waitFor({ state: "detached" });
    await win.waitForTimeout(200);
    // Esc 同时清掉多选吗：读 Esc 之后批量条还在不在（渲染层的事实，Viva 一侧要对得上）。
    report.cases[report.cases.length - 1].selectionAfterEsc = (await button.count()) > 0;
  };

  for (const [name, role, tag] of [["移动到合集", "menu", "移到合集菜单"], ["批量打标签", "dialog", "批量标签浮层"]]) {
    await snapshot(`${tag}：键盘 Enter`, name, role, async (button) => {
      await win.keyboard.press("Tab");
      await button.focus();
      await win.keyboard.press("Enter");
    });
    await snapshot(`${tag}：键盘 Space`, name, role, async (button) => {
      await button.focus();
      await win.keyboard.press("Space");
    });
    await snapshot(`${tag}：鼠标点击`, name, role, async (button) => {
      await button.click();
    });
  }
} finally {
  try { await leaveLibrary(session); } catch { /* 会话关闭前的清理失败不影响读数 */ }
  await session.close();
}

const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(outFile, `${text}\n`);
  console.log(`已写入 ${outFile}`);
} else {
  console.log(text);
}
