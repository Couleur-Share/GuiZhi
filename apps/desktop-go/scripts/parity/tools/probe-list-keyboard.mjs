#!/usr/bin/env node
/**
 * 在 React 参照里量知识库卡片列表的窗口级键盘与「程序化滚动之后的悬停」（台账 B-045、B-037）：
 * 夹具 data（44 条，每页 20 条）、卡片视图、1280×800 中档，指针与焦点都按用例摆好后用真键盘按键（Playwright keyboard，isTrusted）。
 *
 * 量什么（每个按键之后各读一次快照：选中行序号与上下沿、滚动位置、多选数、持焦点的元素、DOM 里挂着的行范围）：
 *   A 方向键：没有选中时 ↓ 选第一条、↑ 选最后一条；连按 ↓ 的选中与滚动（就近对齐，tanstack-virtual scrollToIndex align:"auto"）；
 *     到头不循环；Home / End；Shift / Ctrl 修饰键；焦点在按钮上时方向键照样动。
 *   B 空格：切换选中条目的多选勾选；焦点在卡片按钮上时让给按钮（不切换）。
 *   C Tab：焦点序列（卡片按钮之后去哪、虚拟列表缓冲区里的行是不是停靠点、Tab 到窗口外的行时怎么滚）。
 *   D 悬停（B-037）：指针停住不动，按键让列表程序化滚动、换页让内容整体替换、Tab 让焦点滚进视野之后，
 *     逐帧读「悬停的行」与「指针下的行」，看多久重新一致，以及有没有合成的鼠标事件。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-list-keyboard.mjs [--out <文件>] [--only A,B,C,D,E]
 * 前提：已有 React 参照构建；会话用一次性 userData，不含用户数据；知识库数据来自夹具（lib/react-library.mjs）。
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
const only = new Set(String(arg("--only", "A,B,C,D,E")).split(","));

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variant: "light-royal-blue-medium-1280x800", cases: [] };
const round = (n) => Math.round(n * 100) / 100;

/** 页内记录器：快照（选中、滚动、多选、焦点）与逐帧的悬停采样。 */
function installRecorder() {
  const scroller = () => document.querySelector('[data-testid="item-list"]');
  const rowOf = (el) => el?.closest?.('[data-testid="item-list"] [data-index]') ?? null;
  const indexOf = (row) => (row ? Number(row.dataset.index) : null);
  const hoveredRow = () => {
    const chain = [...document.querySelectorAll(":hover")];
    for (let i = chain.length - 1; i >= 0; i -= 1) {
      const row = rowOf(chain[i]);
      if (row) return row;
    }
    return null;
  };
  const describeActive = () => {
    const ae = document.activeElement;
    if (!ae || ae === document.body) return "body";
    const row = rowOf(ae);
    const label = ae.getAttribute("aria-label") || (ae.textContent || "").trim().replace(/\s+/g, " ").slice(0, 16);
    return `${ae.tagName.toLowerCase()}${row ? `@row${indexOf(row)}` : ""}:${label}`;
  };
  const state = { events: [], frames: [], t0: 0, x: 0, y: 0, running: false };
  const now = () => Math.round((performance.now() - state.t0) * 10) / 10;
  for (const type of [
    "wheel", "scroll",
    "pointerover", "pointerenter", "pointermove", "pointerout", "pointerleave",
    "mouseover", "mouseenter", "mousemove", "mouseout", "mouseleave",
  ]) {
    document.addEventListener(
      type,
      (e) => {
        if (!state.running) return;
        const target = e.target instanceof Element ? e.target : null;
        state.events.push({ t: now(), type, trusted: e.isTrusted, x: e.clientX ?? null, y: e.clientY ?? null, row: indexOf(rowOf(target)) });
      },
      true,
    );
  }
  window.__kbProbe = {
    snapshot() {
      const sc = scroller();
      const sr = sc.getBoundingClientRect();
      const rows = [...sc.querySelectorAll("[data-index]")].map((r) => ({ index: indexOf(r), btn: r.querySelector("button"), rect: r.getBoundingClientRect(), card: r.querySelector("button")?.getBoundingClientRect() }));
      const classes = (b) => (b ? b.className.split(/\s+/) : []);
      const sel = rows.find((r) => classes(r.btn).includes("bg-primary"));
      const checked = rows.filter((r) => classes(r.btn).includes("bg-primary/20")).map((r) => r.index);
      const bulk = (document.body.innerText.match(/已选 (\d+) 项/) || [])[1] ?? null;
      const idx = rows.map((r) => r.index);
      return {
        scrollTop: sc.scrollTop, clientHeight: sc.clientHeight, scrollHeight: sc.scrollHeight,
        selectedIndex: sel ? sel.index : null,
        // 选中行（含 pt-1.5 的虚拟行盒）与卡片按钮相对滚动区上沿的上下沿
        row: sel ? [Math.round((sel.rect.top - sr.top) * 100) / 100, Math.round((sel.rect.bottom - sr.top) * 100) / 100] : null,
        card: sel ? [Math.round((sel.card.top - sr.top) * 100) / 100, Math.round((sel.card.bottom - sr.top) * 100) / 100] : null,
        checked, bulkCount: bulk,
        mounted: idx.length ? [Math.min(...idx), Math.max(...idx)] : null,
        active: describeActive(),
        page: (document.body.innerText.match(/第 (\d+) \/ (\d+) 页|(\d+) \/ (\d+)/) || [])[0] ?? null,
      };
    },
    blur() { document.activeElement?.blur?.(); },
    start(x, y) {
      state.events = []; state.frames = []; state.t0 = performance.now(); state.x = x; state.y = y; state.running = true;
      const tick = () => {
        if (!state.running) return;
        const atPoint = rowOf(document.elementFromPoint(x, y));
        state.frames.push({ t: now(), hovered: indexOf(hoveredRow()), atPoint: indexOf(atPoint), scrollTop: scroller().scrollTop });
        requestAnimationFrame(tick);
      };
      tick();
    },
    now,
    stop() { state.running = false; return { events: state.events, frames: state.frames }; },
    scrollTo(top) { scroller().scrollTop = top; return scroller().scrollTop; },
    /** 第 k 个「卡片完整落在滚动区可视范围内」的行的中心（避开行内可能的操作区，x 取卡片左部）。 */
    visibleCardSpot(k) {
      const sc = scroller();
      const sr = sc.getBoundingClientRect();
      const cards = [...sc.querySelectorAll("[data-index]")]
        .map((r) => ({ index: indexOf(r), b: r.querySelector("button")?.getBoundingClientRect() }))
        .filter((c) => c.b && c.b.top >= sr.top + 2 && c.b.bottom <= sr.bottom - 2)
        .sort((a, b) => a.index - b.index);
      const c = cards[k];
      return c ? { index: c.index, x: c.b.x + 60, y: c.b.y + c.b.height / 2 } : null;
    },
    clickNextPage() {
      const btn = document.querySelector('button[aria-label="下一页"]');
      if (!btn || btn.disabled) return false;
      btn.click();
      return true;
    },
  };
}

const wait = (ms) => session.page.waitForTimeout(ms);

/** 重新进入知识库（reload 清掉选中与多选），把焦点放回 body，再等列表就绪。 */
async function freshList() {
  const win = session.page;
  await enterLibrary(session, { variant: "data", list: { fault: "" } });
  await win.evaluate(installRecorder);
  await win.waitForFunction(() => document.querySelectorAll('[data-testid="item-list"] [data-index]').length > 3);
  await win.mouse.move(2, 2);
  await win.evaluate(() => window.__kbProbe.blur());
  await wait(300);
}

/** 按键并读快照：每次按键后等 ms（滚动与虚拟器重排落定）。 */
async function pressAndSnap(label, key, { ms = 160 } = {}) {
  const win = session.page;
  await win.keyboard.press(key);
  await wait(ms);
  const snap = await win.evaluate(() => window.__kbProbe.snapshot());
  return { label, key, ...snap };
}

async function sequence(title, keys, opts) {
  const rows = [];
  for (const key of keys) rows.push(await pressAndSnap(title, key, opts));
  return rows;
}

function brief(rows) {
  return rows
    .map((r) => `${r.key}→选中${r.selectedIndex ?? "-"} 滚动${round(r.scrollTop)} 卡片${r.card ? `[${r.card[0]},${r.card[1]}]` : "-"} 勾选${r.bulkCount ?? "无"} 焦点${r.active}`)
    .join("\n    ");
}

/** 把指针停在第 k 个完整可见的卡片中心，不动，等悬停稳定。 */
async function parkOnCard(k) {
  const win = session.page;
  const spot = await win.evaluate((k) => window.__kbProbe.visibleCardSpot(k), k);
  if (!spot) throw new Error(`没有第 ${k} 个完整可见的卡片`);
  await win.mouse.move(2, 2);
  await wait(150);
  await win.mouse.move(spot.x, spot.y);
  await wait(400);
  return spot;
}

/** 记录 act 之后 ms 毫秒内的帧与事件，返回悬停重新一致所用的时间。 */
async function hoverCase(label, spot, act, { ms = 700 } = {}) {
  const win = session.page;
  await win.evaluate(({ x, y }) => window.__kbProbe.start(x, y), spot);
  const t0 = await win.evaluate(() => window.__kbProbe.now());
  await act();
  await wait(ms);
  const data = await win.evaluate(() => window.__kbProbe.stop());
  const frames = data.frames.filter((f) => f.t >= t0);
  let settledAt = null;
  for (let i = 0; i < frames.length; i += 1) {
    if (frames.slice(i).every((f) => f.hovered === f.atPoint)) { settledAt = frames[i].t; break; }
  }
  const last = frames[frames.length - 1];
  const everDiverged = frames.some((f) => f.hovered !== f.atPoint);
  return {
    label,
    point: [round(spot.x), round(spot.y)],
    rowAtPointBefore: data.frames[0]?.atPoint ?? null,
    scroll: { first: frames[0]?.scrollTop, last: last?.scrollTop },
    atEnd: { hovered: last?.hovered, atPoint: last?.atPoint },
    everDiverged,
    // 从未分叉：指针下的行没变（没有滚动，或内容没动）；分叉之后一致：浏览器补了悬停刷新
    catchUpMs: !everDiverged ? null : settledAt == null ? "从未一致" : round(settledAt - t0),
    mouseEventsAfter: data.events.filter((e) => e.t >= t0 && !["wheel", "scroll"].includes(e.type)).map((e) => ({ type: e.type, trusted: e.trusted, t: round(e.t), row: e.row })),
    scrollEvents: data.events.filter((e) => e.type === "scroll").length,
  };
}

try {
  const win = session.page;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);

  if (only.has("A")) {
    await freshList();
    const intro = await win.evaluate(() => window.__kbProbe.snapshot());
    report.initial = intro;
    // A1 没有选中时 ↓ 起，连按 ↓ 越过视口再到页尾（20 条）并多按两次
    report.cases.push({ id: "A1", title: "↓ 连按 22 次（从没有选中开始，页内 20 条）", rows: await sequence("A1", Array(22).fill("ArrowDown")) });
    report.cases.push({ id: "A2", title: "End、Home、End、↑ ×3", rows: await sequence("A2", ["End", "Home", "End", "ArrowUp", "ArrowUp", "ArrowUp"]) });
    report.cases.push({ id: "A3", title: "Home 之后 ↑（到头不循环）", rows: await sequence("A3", ["Home", "ArrowUp"]) });
    // A4 修饰键
    report.cases.push({
      id: "A4", title: "修饰键：Shift+↓、Ctrl+↓、Alt+↓、Ctrl+Home（Home 之后各按一次）",
      rows: await sequence("A4", ["Home", "Shift+ArrowDown", "Control+ArrowDown", "Alt+ArrowDown", "Control+Home"]),
    });
    // A5 没有选中时 ↑ 选最后一条
    await freshList();
    report.cases.push({ id: "A5", title: "没有选中时 ↑", rows: await sequence("A5", ["ArrowUp"]) });
    // A6 焦点在工具条的按钮上时方向键
    await freshList();
    await win.getByRole("button", { name: "下一页", exact: true }).focus();
    await wait(80);
    report.cases.push({ id: "A6", title: "焦点在「下一页」按钮上按 ↓ ×2", rows: await sequence("A6", ["ArrowDown", "ArrowDown"]) });
    // A7 End 之后 ↑ 越过视口上沿：选中项在上方时怎么对齐
    await freshList();
    report.cases.push({ id: "A7", title: "End 之后 ↑ ×16（选中项越过视口上沿）", rows: await sequence("A7", ["End", ...Array(16).fill("ArrowUp")]) });
    // A8 滚动位置的时间线：连按 ↓ 到最后一条，逐帧记 scrollTop，看是一步到位还是分两步
    await freshList();
    await win.evaluate(() => {
      const sc = document.querySelector('[data-testid="item-list"]');
      window.__tl = { running: true, t0: performance.now(), samples: [] };
      const tick = () => {
        if (!window.__tl.running) return;
        window.__tl.samples.push([Math.round((performance.now() - window.__tl.t0) * 10) / 10, sc.scrollTop]);
        requestAnimationFrame(tick);
      };
      tick();
    });
    const keyTimes = [];
    for (let i = 0; i < 20; i += 1) {
      keyTimes.push(await win.evaluate(() => Math.round((performance.now() - window.__tl.t0) * 10) / 10));
      await win.keyboard.press("ArrowDown");
      await wait(80);
    }
    await wait(500);
    const samples = await win.evaluate(() => { window.__tl.running = false; return window.__tl.samples; });
    const changes = samples.filter((s, i) => i === 0 || s[1] !== samples[i - 1][1]);
    report.cases.push({ id: "A8", title: "↓ ×20（间隔 80 ms）的 scrollTop 变化时间线", keyTimes, changes });
  }

  if (only.has("B")) {
    await freshList();
    const rows = await sequence("B1", ["ArrowDown", "ArrowDown", " ", " ", " "]);
    report.cases.push({ id: "B1", title: "↓↓ 选中第二条后空格 ×3（切换勾选）", rows });
    // B2 焦点在卡片按钮上（点击卡片）时空格
    await freshList();
    const spot = await win.evaluate(() => window.__kbProbe.visibleCardSpot(2));
    await win.mouse.click(spot.x, spot.y);
    await wait(200);
    const afterClick = await win.evaluate(() => window.__kbProbe.snapshot());
    const rows2 = await sequence("B2", [" ", " "]);
    report.cases.push({ id: "B2", title: "点击第 3 张卡片（按钮持有焦点）后按空格 ×2", afterClick, rows: rows2 });
    // B3 Ctrl+点击进入多选后，空格仍作用于「选中的那条」
    await freshList();
    await win.keyboard.press("ArrowDown");
    await win.keyboard.press("ArrowDown");
    await win.evaluate(() => window.__kbProbe.blur());
    await wait(100);
    report.cases.push({ id: "B3", title: "↓↓ 后空格、↓、空格（勾选跟着选中项走）", rows: await sequence("B3", [" ", "ArrowDown", " ", "ArrowDown", "ArrowDown"]) });
  }

  if (only.has("C")) {
    await freshList();
    // 点击第一张卡片：选中并让按钮持有焦点，然后 Tab 一路走
    const spot = await win.evaluate(() => window.__kbProbe.visibleCardSpot(0));
    await win.mouse.click(spot.x, spot.y);
    await wait(250);
    const tabs = await sequence("C1", Array(30).fill("Tab"));
    report.cases.push({ id: "C1", title: "点击第 1 张卡片后 Tab ×30（焦点序列与滚动、挂载范围）", rows: tabs });
    const back = await sequence("C2", Array(8).fill("Shift+Tab"));
    report.cases.push({ id: "C2", title: "再 Shift+Tab ×8", rows: back });
  }

  if (only.has("D")) {
    // D1 指针停在第 3 张卡片上，↓ 连按直到选中项越过视口底部、列表程序化滚动
    await freshList();
    let spot = await parkOnCard(3);
    report.cases.push({
      id: "D1", title: "指针停在第 4 张可见卡片上，按 ↓ ×14（选中项越过视口底部，列表程序化滚动）",
      hover: await hoverCase("D1", spot, async () => { for (let i = 0; i < 14; i += 1) { await win.keyboard.press("ArrowDown"); await wait(60); } }),
    });
    // D2 内容整体替换（下一页），指针不动
    await freshList();
    spot = await parkOnCard(3);
    report.cases.push({
      id: "D2", title: "指针停在第 4 张可见卡片上，点「下一页」（内容整体替换，指针不动）",
      hover: await hoverCase("D2", spot, async () => { await win.evaluate(() => window.__kbProbe.clickNextPage()); }),
    });
    // D3 Tab 让焦点滚进视野
    await freshList();
    const first = await win.evaluate(() => window.__kbProbe.visibleCardSpot(0));
    await win.mouse.click(first.x, first.y);
    await wait(250);
    spot = await parkOnCard(1);
    report.cases.push({
      id: "D3", title: "指针停在第 2 张可见卡片上，Tab ×14（焦点走出视口，浏览器把它滚进视野）",
      hover: await hoverCase("D3", spot, async () => { for (let i = 0; i < 14; i += 1) { await win.keyboard.press("Tab"); await wait(60); } }),
    });
  }

  if (only.has("E")) {
    // E1 排序下拉打开后按 ↓：下拉自己没有方向键处理，看列表选中会不会动（窗口级钩子不看 defaultPrevented，也不看浮层）
    await freshList();
    await win.getByRole("button", { name: /^排序/ }).first().click();
    await wait(250);
    const afterOpen = await win.evaluate(() => window.__kbProbe.snapshot());
    report.cases.push({ id: "E1", title: "排序下拉打开后 ↓↓ Esc", afterClick: afterOpen, rows: await sequence("E1", ["ArrowDown", "ArrowDown", "Escape"]) });
    // E2 右键菜单打开后按 ↓：菜单自己用 document 监听移动焦点并 preventDefault，但不 stopPropagation
    await freshList();
    const sp = await win.evaluate(() => window.__kbProbe.visibleCardSpot(2));
    await win.mouse.click(sp.x, sp.y, { button: "right" });
    await wait(300);
    const afterMenu = await win.evaluate(() => window.__kbProbe.snapshot());
    report.cases.push({ id: "E2", title: "右键菜单打开后 ↓↓ Esc", afterClick: afterMenu, rows: await sequence("E2", ["ArrowDown", "ArrowDown", "Escape"]) });
  }
} finally {
  try { await leaveLibrary(session); } catch { /* 会话关闭前的清理失败不影响读数 */ }
  await session.close();
}

const text = JSON.stringify(report, null, 2);
if (outFile) {
  fs.mkdirSync(path.dirname(path.resolve(outFile)), { recursive: true });
  fs.writeFileSync(path.resolve(outFile), text);
  console.log(`已写 ${path.resolve(outFile)}`);
}
console.log(`初始：${JSON.stringify(report.initial)}`);
for (const c of report.cases) {
  console.log(`\n[${c.id}] ${c.title}`);
  if (c.afterClick) console.log(`  点击之后：选中${c.afterClick.selectedIndex} 勾选${c.afterClick.bulkCount ?? "无"} 焦点${c.afterClick.active}`);
  if (c.rows) console.log(`    ${brief(c.rows)}`);
  if (c.changes) console.log(`  按键时刻 ${c.keyTimes.join(' ')}\n  scrollTop 变化 ${c.changes.map((s) => `${s[0]}ms→${s[1]}`).join('  ')}`);
  if (c.hover) {
    const h = c.hover;
    const moves = h.mouseEventsAfter.map((e) => `${e.type}${e.trusted ? "" : "(合成)"}@${e.t}→行${e.row ?? "-"}`).join("、");
    console.log(
      `  滚动 ${h.scroll.first}→${h.scroll.last}；指针下的行 ${h.rowAtPointBefore}→${h.atEnd.atPoint}，悬停行 ${h.atEnd.hovered}；` +
        `${h.everDiverged ? `曾分叉，重新一致 ${h.catchUpMs}` : "全程一致（指针下的行没换）"}；之后的鼠标事件：${moves || "无"}`,
    );
  }
}
