#!/usr/bin/env node
/**
 * 在 React 参照里量上下文菜单的子菜单（台账 B-023(1)）：
 * 侧栏标签行的右键菜单（重命名 / 颜色〔子菜单，10 个颜色〕/ 删除标签）是渲染层里带子菜单的菜单。
 *
 * 量三类事实：
 *   1. 几何：根面板、各项、子菜单的外层（含 pl-2 / pr-2 的悬停桥）、子菜单面板与各子项的外框；
 *      锚点在视口中部（向右展开）、靠右（向左展开）、靠下（顶部上移）、右下（两者）；三个字号档。
 *   2. 键盘：→ 开子菜单并聚焦第一个子项、← 回到父项、↑ ↓ Home End 在「按 DOM 顺序展开的全部项」里循环
 *      （子菜单开着时，它的子项紧跟在父项后面）、Esc 关闭。
 *   3. 时间与悬停：鼠标离开后子菜单多久消失（渲染层 140 ms）、进入子菜单后父项的悬停底色是否还在、
 *      鼠标在父项与子菜单之间的缝里（悬停桥）时子菜单是否保持。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-submenu.mjs [--out <文件>] [--compact <文件>]
 *       node apps/desktop-go/scripts/parity/tools/probe-submenu.mjs --compact-from <完整报告.json> --compact <文件>
 * 前提：已有 React 参照构建；会话用一次性 userData、空库（探针自己建一个标签）。
 *
 * --compact 写出 L4 测试用的精简事实（viva/kit/testdata/submenu-react.json）：几何只留测试要比的外框，
 * 键盘序列只留「按键 → 焦点标签、展开状态」，悬停事实原样保留。--compact-from 从已有的完整报告生成，不再开会话。
 *
 * 关闭延迟（140 ms）取自渲染层源码（ContextMenu.tsx 的 scheduleSubmenuClose）：探针里的 MutationObserver 没有捕到
 * 子菜单从 DOM 消失的时刻（timing.closeAfterLeaveMs 为 null），所以没有实测毫秒数；
 * 「离开后子菜单仍保持一小会儿、随后关闭」的行为由 hover.rootToRoot.afterParentClick 等项旁证。
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
const outFile = arg("--out", null);
const compactFile = arg("--compact", null);
const compactFrom = arg("--compact-from", null);

/** 取两位小数，免得浮点噪声进测试数据。 */
const r2 = (rect) => rect.map((v) => Math.round(v * 100) / 100);

/** 精简事实：几何只留测试要比的外框，键盘序列只留「按键 → 焦点标签与展开状态」，悬停事实原样保留。 */
function compact(report) {
  const fontOf = (variant) => (/-(small|medium|large)-/.exec(variant) ?? [])[1];
  return {
    note: "由 scripts/parity/tools/probe-submenu.mjs 生成：React 参照里侧栏标签行菜单（重命名 / 颜色〔子菜单 10 色〕/ 删除标签）的子菜单事实。",
    tag: report.tag,
    cases: report.cases.map((c) => {
      const open = c.open;
      const parent = open.items.find((i) => i.haspopup);
      return {
        id: c.id,
        font: fontOf(c.variant),
        anchor: c.anchor,
        viewport: open.viewport,
        root: r2(open.rect),
        items: open.items.map((i) => ({ label: i.label, rect: r2(i.rect), haspopup: i.haspopup, expanded: i.expanded })),
        sub: {
          parent: parent.label,
          left: /right-full/.test(parent.sub.cls),
          wrapper: r2(parent.sub.wrapper),
          panel: r2(parent.sub.panel),
          children: parent.sub.children.map((k) => ({ label: k.label, rect: r2(k.rect) })),
        },
      };
    }),
    keyboard: Object.fromEntries(
      Object.entries(report.keyboard).map(([name, seq]) => [
        name,
        Array.isArray(seq)
          ? seq.map((s) => ({ key: s.key, label: s.focus?.label ?? null, index: s.focus?.index ?? null, total: s.focus?.total ?? null, expanded: s.expanded }))
          : seq,
      ]),
    ),
    hover: report.hover,
  };
}

if (compactFrom) {
  const full = JSON.parse(fs.readFileSync(compactFrom, "utf8"));
  if (!compactFile) throw new Error("--compact-from 要同时给 --compact <输出文件>");
  fs.mkdirSync(path.dirname(path.resolve(compactFile)), { recursive: true });
  fs.writeFileSync(compactFile, `${JSON.stringify(compact(full), null, 1)}\n`);
  console.log(`已写出 ${compactFile}`);
  process.exit(0);
}

const TAG_NAME = "Alpha";
const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { tag: TAG_NAME, cases: [], keyboard: {}, timing: {}, hover: {} };

/** 页内的取数函数：把一个元素的外框取成 [x, y, w, h]（视口坐标，CSS 像素，保留原始小数）。 */
const PAGE_HELPERS = `
  window.__rect = (el) => { const r = el.getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; };
  window.__menu = () => {
    const menu = document.querySelector('[role="menu"]');
    if (!menu) return null;
    const rootItems = [...menu.querySelectorAll(':scope > div')].map((wrap) => {
      const btn = wrap.querySelector(':scope > button[role="menuitem"]');
      const out = { label: btn.querySelector('span.truncate')?.textContent ?? btn.textContent, rect: __rect(btn), wrap: __rect(wrap),
        haspopup: btn.getAttribute('aria-haspopup'), expanded: btn.getAttribute('aria-expanded') };
      const sub = btn.nextElementSibling;
      if (sub) {
        const panel = sub.firstElementChild;
        out.sub = { wrapper: __rect(sub), panel: __rect(panel), cls: sub.className,
          children: [...panel.querySelectorAll('button[role="menuitem"]')].map((b) => ({ label: b.querySelector('span.truncate')?.textContent ?? b.textContent, rect: __rect(b) })) };
      }
      return out;
    });
    return { rect: __rect(menu), items: rootItems, viewport: [innerWidth, innerHeight] };
  };
  window.__focus = () => {
    const el = document.activeElement;
    if (!el || el === document.body) return null;
    const all = [...document.querySelectorAll('[role="menuitem"]')];
    const label = el.querySelector?.('span.truncate')?.textContent ?? el.textContent;
    return { index: all.indexOf(el), label, parentIndex: el.dataset.parentIndex ?? null, menuIndex: el.dataset.menuIndex ?? null, total: all.length };
  };
`;

/** 在标签行上派发 contextmenu 事件，菜单的锚点取 (x, y)。 */
async function openMenuAt(win, x, y) {
  await win.evaluate(({ name, x, y }) => {
    const btn = [...document.querySelectorAll("button[aria-pressed]")].find((b) => b.textContent.trim() === name);
    if (!btn) throw new Error(`侧栏里没有标签行「${name}」`);
    btn.parentElement.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true, clientX: x, clientY: y }));
  }, { name: TAG_NAME, x, y });
  await win.getByRole("menu").waitFor({ state: "visible" });
  await win.waitForTimeout(80);
}

async function closeMenu(win) {
  await win.keyboard.press("Escape");
  await win.waitForTimeout(60);
  await win.mouse.move(2, 2);
}

/** 悬停到「颜色」项中心，等子菜单出现，返回菜单几何。 */
async function hoverParent(win) {
  const parent = win.getByRole("menuitem", { name: "颜色", exact: true });
  const box = await parent.boundingBox();
  await win.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await win.waitForTimeout(120);
  return win.evaluate(() => __menu());
}

try {
  const { page: win } = session;
  // 建一个标签；侧栏默认不列没有条目的标签，所以之后要点「显示未使用的标签」。
  await win.evaluate(async (name) => window.api.tag.create({ name, colorKey: "blue" }), TAG_NAME);

  const caseDefs = [
    { id: "mid-small", variant: "light-royal-blue-small-1280x800", anchor: [150, 300] },
    { id: "mid-medium", variant: "light-royal-blue-medium-1280x800", anchor: [150, 300] },
    { id: "mid-large", variant: "light-royal-blue-large-1280x800", anchor: [150, 300] },
    { id: "right-medium", variant: "light-royal-blue-medium-1280x800", anchor: [1100, 300] },
    { id: "bottom-medium", variant: "light-royal-blue-medium-1280x800", anchor: [150, 700] },
    { id: "bottom-right-medium", variant: "light-royal-blue-medium-1280x800", anchor: [1100, 700] },
    { id: "edge-right-medium", variant: "light-royal-blue-medium-1280x800", anchor: [900, 300] },
    { id: "bottom-large", variant: "light-royal-blue-large-1280x800", anchor: [150, 640] },
  ];
  let currentVariant = null;
  for (const def of caseDefs) {
    if (def.variant !== currentVariant) {
      await applyVariant(session, parseVariantKey(def.variant, matrix), matrix);
      await win.evaluate(PAGE_HELPERS);
      // 点「显示 N 个未使用的标签」
      await win.getByRole("button", { name: /未使用的标签/ }).click();
      await win.waitForTimeout(150);
      currentVariant = def.variant;
    }
    await openMenuAt(win, def.anchor[0], def.anchor[1]);
    const closed = await win.evaluate(() => __menu());
    const open = await hoverParent(win);
    report.cases.push({ ...def, closed, open });
    await closeMenu(win);
  }

  // —— 键盘（中字号，锚点 (150, 300)）——
  await applyVariant(session, parseVariantKey("light-royal-blue-medium-1280x800", matrix), matrix);
  await win.evaluate(PAGE_HELPERS);
  await win.getByRole("button", { name: /未使用的标签/ }).click();
  await win.waitForTimeout(150);
  const keyLog = [];
  const press = async (key) => {
    await win.keyboard.press(key);
    await win.waitForTimeout(90);
    keyLog.push({ key, focus: await win.evaluate(() => __focus()), expanded: await win.evaluate(() => __menu()?.items.map((i) => i.expanded) ?? null) });
  };
  await openMenuAt(win, 150, 300);
  keyLog.push({ key: "(打开)", focus: await win.evaluate(() => __focus()), expanded: await win.evaluate(() => __menu()?.items.map((i) => i.expanded)) });
  for (const key of ["ArrowDown", "ArrowRight", "ArrowDown", "ArrowDown", "ArrowUp", "End", "ArrowDown", "Home", "ArrowLeft", "ArrowDown", "ArrowDown", "ArrowDown", "ArrowUp", "ArrowRight", "Home", "ArrowUp"]) {
    await press(key);
  }
  report.keyboard.sequenceA = keyLog.splice(0);
  await closeMenu(win);

  // 子菜单由悬停打开、焦点仍在根项上时的方向键
  await openMenuAt(win, 150, 300);
  await hoverParent(win);
  for (const key of ["ArrowDown", "ArrowDown", "ArrowDown", "ArrowUp", "ArrowUp", "ArrowUp", "ArrowUp"]) await press(key);
  report.keyboard.sequenceB = keyLog.splice(0);
  await closeMenu(win);

  // 子菜单里按 ←：关闭子菜单并聚焦父项；再按 → 重新打开并聚焦第一个子项；→ 在没有子菜单的项上没有作用
  await openMenuAt(win, 150, 300);
  await press("ArrowRight"); // 焦点在「重命名」：没有子菜单
  await press("ArrowDown");
  await press("ArrowRight");
  await press("ArrowDown");
  await press("ArrowDown");
  await press("ArrowLeft");
  await press("ArrowRight");
  await press("ArrowLeft");
  await press("ArrowLeft"); // 焦点在父项上（根项）：← 没有作用
  await press("ArrowDown"); // 子菜单已关闭：下一项是「删除标签」
  report.keyboard.sequenceC = keyLog.splice(0);
  await closeMenu(win);
  // Esc 在子菜单里
  await openMenuAt(win, 150, 300);
  await press("ArrowDown");
  await press("ArrowRight");
  await win.keyboard.press("Escape");
  await win.waitForTimeout(90);
  report.keyboard.escInSubmenu = { menuStillOpen: await win.evaluate(() => document.querySelector('[role="menu"]') !== null) };
  await closeMenu(win);

  // —— 时间与悬停 ——
  await openMenuAt(win, 150, 300);
  const open0 = await hoverParent(win);
  const parent = open0.items.find((i) => i.haspopup);
  const subPanel = parent.sub.panel;
  // 1) 进入子菜单后父项的悬停底色（CSS 动画被压掉，直接读计算样式）
  await win.mouse.move(subPanel[0] + 30, subPanel[1] + 30);
  await win.waitForTimeout(120);
  report.hover.parentBgInSubmenu = await win.evaluate(() => {
    const p = document.querySelector('[role="menu"] button[aria-haspopup="menu"]');
    return { bg: getComputedStyle(p).backgroundColor, expanded: p.getAttribute("aria-expanded") };
  });
  // 2) 悬停在子菜单的第一个子项上时，该子项的底色
  report.hover.childBg = await win.evaluate(() => {
    const kids = document.querySelectorAll('button[data-parent-index]');
    return [...kids].slice(0, 2).map((k) => getComputedStyle(k).backgroundColor);
  });
  // 3) 从父项移到「桥」（父项右缘外 2px，子菜单面板左缘之前）：子菜单是否保持
  await win.mouse.move(parent.rect[0] + parent.rect[2] / 2, parent.rect[1] + parent.rect[3] / 2);
  await win.waitForTimeout(60);
  const bridgeX = parent.sub.panel[0] - 2;
  await win.mouse.move(bridgeX, parent.rect[1] + parent.rect[3] / 2, { steps: 2 });
  await win.waitForTimeout(260);
  report.hover.bridgeKeepsOpen = { x: bridgeX, panelLeft: parent.sub.panel[0], wrapperLeft: parent.sub.wrapper[0], open: await win.evaluate(() => document.querySelector('[role="menu"] button[aria-haspopup="menu"]').nextElementSibling !== null) };
  // 4) 离开后多久关闭：页内记下「第一个 mousemove 到达」与「子菜单从 DOM 里消失」的时刻，两者之差就是关闭延迟
  await win.mouse.move(parent.rect[0] + parent.rect[2] / 2, parent.rect[1] + parent.rect[3] / 2);
  await win.waitForTimeout(150);
  await win.evaluate(() => {
    window.__ev = { move: 0, removed: 0 };
    document.addEventListener("mousemove", () => { if (!window.__ev.move) window.__ev.move = performance.now(); }, true);
    const menu = document.querySelector('[role="menu"]');
    new MutationObserver(() => {
      const p = document.querySelector('[role="menu"] button[aria-haspopup="menu"]');
      if (p && p.nextElementSibling == null && !window.__ev.removed) window.__ev.removed = performance.now();
    }).observe(menu, { childList: true, subtree: true });
  });
  await win.mouse.move(parent.rect[0] + parent.rect[2] / 2 + 20, 760);
  await win.waitForTimeout(500);
  const ev = await win.evaluate(() => window.__ev);
  report.timing.closeAfterLeaveMs = ev.removed && ev.move ? Math.round((ev.removed - ev.move) * 10) / 10 : null;
  report.timing.raw = ev;
  await closeMenu(win);
  // 4b) 鼠标从父项移到别的根项：立即关闭（渲染层 onMouseEnter 里 setOpenSubmenu(null)）；移到子菜单里再移到别的根项同理
  await openMenuAt(win, 150, 300);
  const o1 = await hoverParent(win);
  const par1 = o1.items.find((i) => i.haspopup);
  const rename = o1.items[0];
  const del = o1.items[2];
  const isOpen = () => win.evaluate(() => document.querySelector('[role="menu"] button[aria-haspopup="menu"]')?.nextElementSibling != null);
  await win.mouse.move(del.rect[0] + 40, del.rect[1] + del.rect[3] / 2);
  await win.waitForTimeout(30);
  const afterDel30 = await isOpen();
  await win.waitForTimeout(250);
  const afterDel280 = await isOpen();
  await hoverParent(win);
  await win.mouse.move(rename.rect[0] + 40, rename.rect[1] + rename.rect[3] / 2);
  await win.waitForTimeout(30);
  const afterRename30 = await isOpen();
  await hoverParent(win);
  // 悬停桥压在下面的根项右缘 4px 上：指针放在「删除标签」行的最右 2px（落在桥里），子菜单应当保持
  const bridgeOverRow = [par1.sub.wrapper[0] + 1, del.rect[1] + del.rect[3] / 2];
  await win.mouse.move(bridgeOverRow[0], bridgeOverRow[1]);
  await win.waitForTimeout(300);
  const bridgeOverRowOpen = await isOpen();
  // 点父项：不关闭菜单也不触发动作
  await hoverParent(win);
  const pb = par1.rect;
  await win.mouse.click(pb[0] + pb[2] / 2, pb[1] + pb[3] / 2);
  await win.waitForTimeout(120);
  const afterParentClick = await win.evaluate(() => ({ menu: document.querySelector('[role="menu"]') !== null, open: document.querySelector('[role="menu"] button[aria-haspopup="menu"]')?.nextElementSibling != null }));
  report.hover.rootToRoot = { afterDel30, afterDel280, afterRename30, bridgeOverRow, bridgeOverRowOpen, afterParentClick };
  await closeMenu(win);
  // 5) 悬停时间：鼠标进入父项到子菜单出现（没有延迟，渲染层是 onMouseEnter 里立即 setState）
  await openMenuAt(win, 150, 300);
  await win.evaluate(() => {
    window.__ev2 = { move: 0, shown: 0 };
    document.addEventListener("mousemove", () => { if (!window.__ev2.move) window.__ev2.move = performance.now(); }, true);
    const menu = document.querySelector('[role="menu"]');
    new MutationObserver(() => {
      const p = document.querySelector('[role="menu"] button[aria-haspopup="menu"]');
      if (p && p.nextElementSibling && !window.__ev2.shown) window.__ev2.shown = performance.now();
    }).observe(menu, { childList: true, subtree: true });
  });
  const parentBox = await win.getByRole("menuitem", { name: "颜色", exact: true }).boundingBox();
  await win.mouse.move(parentBox.x + parentBox.width / 2, parentBox.y + parentBox.height / 2);
  await win.waitForTimeout(300);
  const ev2 = await win.evaluate(() => window.__ev2);
  report.timing.openAfterEnterMs = ev2.shown && ev2.move ? Math.round((ev2.shown - ev2.move) * 10) / 10 : null;
  await closeMenu(win);} finally {
  await session.close();
}

const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(outFile, text);
}
if (compactFile) {
  fs.mkdirSync(path.dirname(path.resolve(compactFile)), { recursive: true });
  fs.writeFileSync(compactFile, `${JSON.stringify(compact(report), null, 1)}\n`);
}
console.log(`案例 ${report.cases.length} 个；键盘序列 A ${report.keyboard.sequenceA?.length}、B ${report.keyboard.sequenceB?.length} 步`);
for (const c of report.cases) {
  const parent = c.open?.items.find((i) => i.haspopup);
  console.log(
    `${c.id}\t锚点 ${c.anchor}\t根面板 ${c.open?.rect.map((v) => Math.round(v * 100) / 100)}\t子菜单外层 ${parent?.sub?.wrapper.map((v) => Math.round(v * 100) / 100)}\t子面板 ${parent?.sub?.panel.map((v) => Math.round(v * 100) / 100)}`,
  );
}
