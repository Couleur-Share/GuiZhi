#!/usr/bin/env node
/**
 * 在 React 参照里量「滚轮滚动之后，悬停状态怎么跟上」（台账 B-031）：
 * 知识库侧栏（many 夹具：28 个合集 + 36 个标签，放不下，会滚动）里把指针停在一行上，鼠标不再动，用滚轮滚几格，
 * 然后逐帧读两样东西——
 *   - 悬停的是哪一行（`:hover` 链上最内层的行）；
 *   - 指针底下此刻是哪一行（elementFromPoint）。
 * 两者重新一致所用的时间，就是 Chromium 补做悬停刷新的延迟；同时记下滚轮前后各个鼠标 / 指针事件
 * （类型、是否 isTrusted、坐标、目标行），看浏览器是不是真的补发了一次鼠标移动。
 * 另外测「滚不动的滚轮」（已到底再滚）：没有滚动，会不会也补发。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-wheel-hover.mjs [--out <文件>]
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
const round = (n) => Math.round(n * 10) / 10;

/**
 * 页内装一个记录器：capture 阶段监听滚轮、滚动与各类鼠标 / 指针事件，rAF 逐帧采样「悬停行」与「指针下的行」。
 * 行 = 侧栏里带 group/row 类的元素，名字取它的文字。
 */
function installRecorder() {
  const rowOf = (el) => el?.closest?.('aside [class*="group/row"]') ?? null;
  const textOf = (row) => (row ? (row.textContent || "").trim().replace(/\s+/g, " ").slice(0, 24) : null);
  const panel = () => [...document.querySelectorAll("aside")].find((a) => a.querySelector('[class*="group/row"]'));
  const hoveredRow = () => {
    const chain = [...document.querySelectorAll(":hover")];
    for (let i = chain.length - 1; i >= 0; i -= 1) {
      const row = rowOf(chain[i]);
      if (row) return row;
    }
    return null;
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
        state.events.push({
          t: now(), type, trusted: e.isTrusted,
          x: e.clientX ?? null, y: e.clientY ?? null,
          row: textOf(rowOf(target)),
        });
      },
      true,
    );
  }
  window.__wheelProbe = {
    /** 开始记录：x、y 是指针位置。 */
    start(x, y) {
      state.events = []; state.frames = []; state.t0 = performance.now(); state.x = x; state.y = y; state.running = true;
      const tick = () => {
        if (!state.running) return;
        const atPoint = rowOf(document.elementFromPoint(x, y));
        state.frames.push({ t: now(), hovered: textOf(hoveredRow()), atPoint: textOf(atPoint) });
        requestAnimationFrame(tick);
      };
      tick();
    },
    /** 页内当前时间（相对 start），用来给滚轮的发出时刻打点。 */
    now,
    stop() {
      state.running = false;
      return { events: state.events, frames: state.frames };
    },
    /** 侧栏滚动区：装着行的那个 aside（另一个 aside 是 rail）里第一个确实能滚的元素。 */
    scrollerInfo() {
      const aside = panel();
      const all = [aside, ...aside.querySelectorAll("*")];
      const el = all.find((n) => n.scrollHeight > n.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(n).overflowY));
      if (!el) return null;
      return { scrollTop: el.scrollTop, clientHeight: el.clientHeight, scrollHeight: el.scrollHeight };
    },
    scrollTo(top) {
      const aside = panel();
      const all = [aside, ...aside.querySelectorAll("*")];
      const el = all.find((n) => n.scrollHeight > n.clientHeight + 1 && /(auto|scroll)/.test(getComputedStyle(n).overflowY));
      el.scrollTop = top;
      return el.scrollTop;
    },
  };
}

/** 指针移到第 index 个「完整落在侧栏可视区内」的行的中心，不动，等悬停稳定，返回该行的中心坐标。 */
async function parkOnRow(win, index) {
  const spot = await win.evaluate((index) => {
    const aside = [...document.querySelectorAll("aside")].find((a) => a.querySelector('[class*="group/row"]'));
    const view = aside.getBoundingClientRect();
    const rows = [...aside.querySelectorAll('[class*="group/row"]')]
      .map((r) => r.getBoundingClientRect())
      .filter((b) => b.top >= view.top + 40 && b.bottom <= view.bottom - 4); // 40：顶部的范围页签与分组标题
    const b = rows[index];
    return b ? { x: b.x + 40, y: b.y + b.height / 2 } : null; // x 避开行末的操作钮
  }, index);
  if (!spot) throw new Error(`侧栏可视区里没有第 ${index} 个完整的行`);
  const { x, y } = spot;
  await win.mouse.move(2, 2);
  await win.waitForTimeout(150);
  await win.mouse.move(x, y);
  await win.waitForTimeout(400);
  return { x, y };
}

/** 一次滚轮：记录从滚轮发出到 ms 毫秒后的帧与事件，返回摘要。 */
async function wheelCase(win, label, { index, deltaY, ms = 700, steps = 1, gapMs = 0, prepare }) {
  // 默认每个用例都从顶部开始，互不影响；到底的用例自己指定 prepare。
  await (prepare ?? (async () => { await win.evaluate(() => window.__wheelProbe.scrollTo(0)); await win.waitForTimeout(200); }))();
  const { x, y } = await parkOnRow(win, index);
  const before = await win.evaluate(() => window.__wheelProbe.scrollerInfo());
  await win.evaluate(({ x, y }) => window.__wheelProbe.start(x, y), { x, y });
  const wheelAt = [];
  for (let i = 0; i < steps; i += 1) {
    wheelAt.push(await win.evaluate(() => window.__wheelProbe.now()));
    await win.mouse.wheel(0, deltaY);
    if (gapMs) await win.waitForTimeout(gapMs);
  }
  await win.waitForTimeout(ms);
  const data = await win.evaluate(() => window.__wheelProbe.stop());
  const after = await win.evaluate(() => window.__wheelProbe.scrollerInfo());
  const lastWheel = wheelAt[wheelAt.length - 1];
  // 滚轮发出之后，悬停行与指针下的行第一次重新一致的时刻（且此后不再分叉）。
  const frames = data.frames.filter((f) => f.t >= lastWheel);
  let settledAt = null;
  for (let i = 0; i < frames.length; i += 1) {
    if (frames.slice(i).every((f) => f.hovered === f.atPoint)) { settledAt = frames[i].t; break; }
  }
  const first = frames[0];
  const last = frames[frames.length - 1];
  report.cases.push({
    label,
    point: [round(x), round(y)],
    scroll: { before: before?.scrollTop, after: after?.scrollTop, scrollHeight: after?.scrollHeight, clientHeight: after?.clientHeight },
    wheelAt: wheelAt.map(round),
    rowAtPointBefore: data.frames[0]?.atPoint ?? null,
    afterLastWheel: { hovered: last?.hovered, atPoint: last?.atPoint },
    firstFrameAfterWheel: first,
    settledAt,
    catchUpMs: settledAt == null ? null : round(settledAt - lastWheel),
    // 滚轮之后到达的鼠标 / 指针事件（排除滚轮与滚动本身）。
    mouseEventsAfterWheel: data.events.filter((e) => e.t >= wheelAt[0] && !["wheel", "scroll"].includes(e.type)),
    scrollEventTimes: data.events.filter((e) => e.type === "scroll").map((e) => round(e.t)).slice(0, 12),
  });
}

try {
  const { page: win } = session;
  const variant = parseVariantKey(report.variant, matrix);
  await applyVariant(session, variant, matrix);
  await enterLibrary(session, { variant: "many" });
  await win.evaluate(installRecorder);
  report.sidebar = await win.evaluate(() => window.__wheelProbe.scrollerInfo());
  if (!report.sidebar) throw new Error("侧栏没有可滚动区域（many 夹具应当放不下）");

  await wheelCase(win, "滚一格（120），指针停在第 3 行", { index: 3, deltaY: 120 });
  await wheelCase(win, "滚三格（360），指针停在第 3 行", { index: 3, deltaY: 360 });
  await wheelCase(win, "小步滚（40），指针停在第 3 行", { index: 3, deltaY: 40 });
  await wheelCase(win, "连滚 6 次（每次 120，间隔 30 ms）", { index: 3, deltaY: 120, steps: 6, gapMs: 30 });
  await wheelCase(win, "已经到底，再滚（没有滚动）", {
    index: 8, deltaY: 120,
    prepare: async () => { await win.evaluate(() => window.__wheelProbe.scrollTo(1e6)); await win.waitForTimeout(200); },
  });
  await wheelCase(win, "回到顶，向上滚（没有滚动）", {
    index: 3, deltaY: -120,
    prepare: async () => { await win.evaluate(() => window.__wheelProbe.scrollTo(0)); await win.waitForTimeout(200); },
  });
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
console.log(`侧栏滚动区：${JSON.stringify(report.sidebar)}`);
for (const c of report.cases) {
  const moves = c.mouseEventsAfterWheel.map((e) => `${e.type}${e.trusted ? "" : "(合成)"}@${e.t}→${e.row ?? "-"}`).join("、");
  console.log(
    `${c.label}：滚动 ${c.scroll.before}→${c.scroll.after}；指针下的行 ${c.rowAtPointBefore}→${c.afterLastWheel.atPoint}，悬停行 ${c.afterLastWheel.hovered}；` +
      `重新一致 ${c.catchUpMs == null ? "从未一致" : `${c.catchUpMs} ms（滚轮后）`}；滚轮之后的鼠标事件：${moves || "无"}`,
  );
}
