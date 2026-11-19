#!/usr/bin/env node
/**
 * 在 React 参照里量知识库列表的搜索命中词高亮（台账 B-041）：
 * 卡片标题 / 摘要、表格标题 / 摘要里 `<mark class="rounded-sm bg-amber-400/90 px-0.5 text-amber-950">`（highlight-text.tsx）的
 * 几何、折行与可访问树表现。夹具 data（44 条，每页 20 条）、1280×800，按 --variants 选字号档与明暗。
 *
 * 量什么：
 *   H1 每个含 mark 的宿主元素（卡片标题 span、卡片摘要 span、表格标题按钮、表格摘要 span）的类名、外框、行高与字体，
 *      以及它下面每个直接子节点（文字或 mark）的内容区矩形（Range，每个行片段一条）；mark 另给自己的行盒片段
 *      （Element.getClientRects，含左右内边距）、计算样式（底色、字色、圆角、内边距、box-decoration-break）；
 *   H2 可访问树：getFullAXTree 里有没有 role 为 mark 的节点、它的父节点角色（L1 会不会报「未映射」）；
 *   H3 选中的卡片（bg-primary）里的 mark 样式（底色在主色上的叠色由 L3 看，这里只读样式）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-highlight.mjs [--out <文件>] [--variants light-royal-blue-medium-1280x800,…]
 *       [--queries 设计,design,知识库] [--only cards,table,selected]
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
const variants = String(arg("--variants", "light-royal-blue-medium-1280x800")).split(",");
const queries = String(arg("--queries", "设计")).split(",");
const only = new Set(String(arg("--only", "cards,table,selected")).split(","));
/** 给每个宿主元素截一张放大 3 倍的图（目录），文件名 <变体>-<查询>-<范围>-<序号>.png；不给就不截。 */
const shotsDir = arg("--shots", null);

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const report = { variants: [] };

/** 页内：收集一个范围里所有含 mark 的宿主元素。 */
function collectHosts(scopeSelector) {
  const round = (n) => Math.round(n * 1000) / 1000;
  const rect = (r) => [round(r.x), round(r.y), round(r.width), round(r.height)];
  const scope = document.querySelector(scopeSelector);
  if (!scope) return { error: `没有 ${scopeSelector}` };
  const hosts = new Set();
  for (const m of scope.querySelectorAll("mark")) hosts.add(m.parentElement);
  const style = (el, props) => {
    const cs = getComputedStyle(el);
    return Object.fromEntries(props.map((p) => [p, cs.getPropertyValue(p)]));
  };
  return [...hosts].map((host) => {
    const card = host.closest("button, tr");
    return {
      host: { tag: host.localName, cls: host.className, rect: rect(host.getBoundingClientRect()), ...style(host, ["font-size", "font-weight", "line-height", "font-family", "color", "display"]) },
      cardCls: card ? String(card.className) : null,
      kids: [...host.childNodes].map((n) => {
        const r = document.createRange();
        r.selectNodeContents(n);
        const text = n.textContent;
        const rects = [...r.getClientRects()].filter((b) => b.width > 0 && b.height > 0).map(rect);
        if (n.nodeType === 3) return { kind: "text", text, rects };
        return {
          kind: n.localName,
          text,
          rects,
          // 行盒片段：含左右内边距（inline 盒的 padding 在片段的首尾）
          boxes: [...n.getClientRects()].map(rect),
          style: style(n, [
            "background-color", "color", "border-radius", "padding-left", "padding-right", "padding-top", "padding-bottom",
            "box-decoration-break", "font-size", "font-weight", "line-height", "display",
          ]),
        };
      }),
    };
  });
}

/** 页内：把选区外的每个 mark 的祖先角色读出来，不在这里做，改走 CDP。 */
async function axMarks(win) {
  const cdp = await win.context().newCDPSession(win);
  try {
    const { nodes } = await cdp.send("Accessibility.getFullAXTree");
    const byId = new Map(nodes.map((n) => [n.nodeId, n]));
    const roleOf = (n) => n?.role?.value ?? null;
    const marks = nodes.filter((n) => roleOf(n) === "mark");
    return {
      total: nodes.length,
      marks: marks.map((n) => {
        const parent = byId.get(n.parentId);
        return {
          ignored: !!n.ignored,
          name: n.name?.value ?? null,
          parentRole: roleOf(parent),
          parentName: parent?.name?.value?.slice(0, 40) ?? null,
          children: (n.childIds ?? []).map((id) => roleOf(byId.get(id))),
        };
      }),
    };
  } finally {
    await cdp.detach();
  }
}

/** 按宿主元素的外框截图（外扩 6px，放大 3 倍：用 deviceScaleFactor 太重，直接截 1 倍再由看图的人放大）。 */
async function shootHosts(win, hosts, tag) {
  if (!shotsDir) return;
  fs.mkdirSync(shotsDir, { recursive: true });
  let i = 0;
  for (const h of hosts) {
    const [x, y, w, hgt] = h.host.rect;
    const pad = 6;
    await win.screenshot({
      path: path.join(shotsDir, `${tag}-${String(i++).padStart(2, "0")}.png`),
      clip: { x: Math.max(0, x - pad), y: Math.max(0, y - pad), width: w + 2 * pad, height: hgt + 2 * pad },
    });
  }
}

async function searchFor(win, query) {
  await win.getByTestId("topbar-search").click();
  await win.keyboard.press("Control+a");
  await win.keyboard.type(query);
  await win.waitForTimeout(800);
  await win.mouse.click(200, 20);
  await win.waitForTimeout(250);
}

try {
  const win = session.page;
  for (const name of variants) {
    const variant = parseVariantKey(name, matrix);
    await applyVariant(session, variant, matrix);
    const entry = { variant: name, queries: [] };
    for (const query of queries) {
      const item = { query };
      await enterLibrary(session, { variant: "data", list: { fault: "" } });
      await win.waitForFunction(() => document.querySelectorAll('[data-testid="item-list"] [data-index]').length > 3);
      await searchFor(win, query);
      if (only.has("cards")) {
        item.cards = await win.evaluate(collectHosts, '[data-testid="item-list"]');
        await shootHosts(win, item.cards, `${name}-${query}-cards`);
        item.ax = await axMarks(win);
        item.listRect = await win.evaluate(() => {
          const r = document.querySelector('[data-testid="item-list"]').getBoundingClientRect();
          return [r.x, r.y, r.width, r.height];
        });
      }
      if (only.has("selected")) {
        const box = await win.getByTestId("item-list").locator("button").first().boundingBox();
        await win.mouse.click(box.x + 24, box.y + box.height / 2);
        await win.waitForTimeout(300);
        const hosts = await win.evaluate(collectHosts, '[data-testid="item-list"]');
        item.selected = hosts.filter((h) => /\bbg-primary\b/.test(h.cardCls ?? ""));
      }
      if (only.has("table")) {
        await win.getByRole("button", { name: "列表视图", exact: true }).click();
        await win.getByTestId("item-table").waitFor({ state: "visible" });
        await win.waitForTimeout(300); // 摘要列默认显示（item-table-config.ts），不用再切
        item.table = await win.evaluate(collectHosts, '[data-testid="item-table"]');
        await shootHosts(win, item.table, `${name}-${query}-table`);
        item.tableAx = await axMarks(win);
      }
      await leaveLibrary(session);
      entry.queries.push(item);
    }
    report.variants.push(entry);
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
} else {
  console.log(text);
}
