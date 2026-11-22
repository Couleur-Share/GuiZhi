#!/usr/bin/env node
/**
 * 阅读岛的内容与渲染层阅读区的对照（台账 C-011 的岛内容对照工具，M2-5）。
 *
 * 为什么要有它：阅读区在 Viva 里是一个 WebView 岛（真子窗口），无头的 vivaparity 画不出它，场景对照在两侧都把阅读区隐去。
 * 岛里跑的是渲染层自己的阅读组件与同一份样式表，理应与渲染层的阅读区逐节点相同——这一点要量，不能只信「同一套组件」。
 *
 * 做法：同一个 WebView2 会话、同一个窗口、同一套启动参数（--force-device-scale-factor=1）里先后做两件事——
 *   1) React 参照：进入知识库、选一条目、取 `[data-article-reader]` 的盒子、逐节点的计算样式与 CDP 截图；
 *   2) 岛：同一个窗口导航到本机的一个小 http 服务（提供 viva/reader/assets 里的入口页，并注入 window.viva 假桥），
 *      视口钉成第 1 步量得的阅读区尺寸，按宿主协议重放 init 与 view（外观取自第 1 步读回的实际外观），取同样的数据与截图。
 * 逐节点比：标签、class、属性、自有文字、盒子（相对阅读区左上角）、一组计算样式；整体比：outerHTML 与像素。
 *   - display:none 的子树不进逐节点与 outerHTML 的比较（看不见、不占位），只报数量；可见子节点的序号只数可见的。
 *   - 岛的外观里带窗口宽度（viewportWidth）：岛的视口只有阅读区那么宽，sm: / xl: 这些断点要按窗口宽度判定（见 reader/breakpoints.ts）。
 *   - 小数起点（台账 B-068）：阅读区在大字号档位的起点/高度带小数（222.75 / 577.25），原生子窗口只能落在整数物理像素上。
 *     **默认**按真实宿主的做法重放：子窗口（视口）取 Viva 的取整框（Left=round(X·s)、Right=round((X+W)·s)，不向外扩大），
 *     React 一侧的参照也截同一个整数设备框，宿主协议里多一条 `layout`（真实起点减去取整起点、精确逻辑尺寸；整数像素上不发），
 *     页面把根节点挪过去并按精确尺寸设宽高（viva/reader/layout.go、reader/layout.ts；JS 一侧的 layoutFor 与 Go 的 LayoutFor 同一公式，
 *     测试向量见 layout_test.go）。
 *   - `--no-layout`：改造前的基线。同样的整数视口与整数设备框参照，但不发 layout 消息，用来量「小数起点」本身带来的差异。
 *   - 对照实验 --frac-shift：岛的视口按向外取整的整数框设置、页面再按起点的小数部分平移，用来验证「像素差只来自小数起点」（B-068 的对照实验，保留）。
 *
 * 用法：node apps/desktop-go/scripts/parity/tools/probe-reader-island.mjs [--out <目录>] [--variants a,b] [--scenes x,y] [--no-layout | --frac-shift]
 * 前提：已有 React 参照构建（readBuildRecord）与 viva/reader/assets（pnpm --filter @guizhi/desktop-go gen 的产物）。
 * 会话用一次性 userData，不含用户数据；本机 http 服务只绑 127.0.0.1、随机端口、用完即关。
 */
import fs from "node:fs";
import http from "node:http";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { readBuildRecord } from "../build-react-ref.mjs";
import { loadMatrix, parseVariantKey } from "../lib/matrix.mjs";
import { openReactSession } from "../lib/react-session.mjs";
import { captureRegionPng } from "../lib/react-facts.mjs";
import { SETTLE_CSS, applyVariant, readAppliedAppearance, settle } from "../lib/react-state.mjs";
import { LIBRARY_DETAIL_FIXTURE, enterLibrary, leaveLibrary } from "../lib/react-library.mjs";
import { CARD_ANCHOR, card, pressKeys } from "../scenes/react-library-list-scenes.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const ASSETS = path.resolve(here, "../../../viva/reader/assets");

const arg = (name, fallback) => {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : fallback;
};
const outDir = arg("--out", null);
const VARIANTS = (arg("--variants", "light-royal-blue-medium-1280x800,dark-royal-blue-medium-1280x800,light-teal-large-1280x800,light-royal-blue-medium-800x600")).split(",");
const ONLY = arg("--scenes", null)?.split(",") ?? null;
// --frac-shift：对照实验。React 阅读区的位置/尺寸可能带小数（大字号档位 222.75 / 577.25），原生子窗口只能落在整数像素上；
// 开了它，岛的视口按「向外取整」的整数框（floor 起点、ceil 终点）设置，页面内容再按起点的小数部分平移，看内容的像素是否因此与 React 一致。
const FRAC_SHIFT = process.argv.includes("--frac-shift");
// --no-layout：基线，整数视口 + 整数设备框参照，但不发 layout 消息（改造前的行为）。
const NO_LAYOUT = process.argv.includes("--no-layout");
if (FRAC_SHIFT && NO_LAYOUT) throw new Error("--frac-shift 与 --no-layout 不能同时使用");
// 宿主协议版本：与 viva/reader/protocol.go 的 ProtocolVersion、reader/protocol.ts 的 READER_PROTOCOL 一致（页面不校验 init 的 v，这里只求如实）。
const PROTOCOL_VERSION = 4;

// ---- 小数像素版面：与 viva/reader/layout.go 的 LayoutFor 同一公式（Viva Windows 平台的取整，float32 的先加后乘） ----
// 本探针的 WebView2 会话固定 --force-device-scale-factor=1，所以缩放是 1；Math.fround 复刻宿主的 float32 运算。
const SCALE = 1;
const vivaRound = (v) => (v < 0 ? -vivaRound(-v) : Math.floor(v + 0.5));
const ALIGNED_EPSILON = 1e-3;
/** 阅读区的整数设备框：Viva 把子窗口放在这上面（Left/Top/Right/Bottom 各自取整，宽高是差）。 */
function deviceRectOf(rect) {
  const x = Math.fround(rect.x), y = Math.fround(rect.y), w = Math.fround(rect.width), h = Math.fround(rect.height);
  const left = vivaRound(Math.fround(x * SCALE)), top = vivaRound(Math.fround(y * SCALE));
  const right = vivaRound(Math.fround(Math.fround(x + w) * SCALE)), bottom = vivaRound(Math.fround(Math.fround(y + h) * SCALE));
  return { x: left, y: top, width: right - left, height: bottom - top, right, bottom };
}
/** 宿主发给页面的 layout（落在设备像素上返回 null：宿主不发消息）。 */
function layoutFor(rect) {
  const x = Math.fround(rect.x), y = Math.fround(rect.y), w = Math.fround(rect.width), h = Math.fround(rect.height);
  const device = deviceRectOf(rect);
  const off = (logical, dev) => logical * SCALE - dev;
  const dx = off(x, device.x), dy = off(y, device.y);
  const dr = off(Math.fround(x + w), device.right), db = off(Math.fround(y + h), device.bottom);
  const aligned = (v) => Math.abs(v) <= ALIGNED_EPSILON;
  if (aligned(dx) && aligned(dy) && aligned(dr) && aligned(db)) return null;
  return { dx: aligned(dx) ? 0 : dx / SCALE, dy: aligned(dy) ? 0 : dy / SCALE, width: w, height: h };
}

// ---- 场景：怎么把 React 带到该条目的阅读区，以及对应的宿主 view 消息（与 viva/screens/library/detailreader.go 的 readerViewFor 一致） ----
const detail = (id) => LIBRARY_DETAIL_FIXTURE.details[id];
const afterHeading = (content, heading) => {
  const lines = content.split("\n");
  const at = lines.findIndex((l) => l.trim().startsWith(heading));
  if (at < 0) return "";
  return lines.slice(at + 1).join("\n").trim();
};
// 图中文字小节含小节标题（splitImageNoteSections 的 recognized；Go 侧 notes.SplitImageSections().Recognized 同）。
const fromHeading = (content, heading) => {
  const at = content.indexOf(heading);
  return at < 0 ? "" : content.slice(at).trim();
};
const clickCard = (title) => async (win) => {
  const box = await card(win, title).boundingBox();
  await win.mouse.click(box.x + CARD_ANCHOR, box.y + box.height / 2);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};
const keysTo = (n) => async (win) => {
  await pressKeys("ArrowDown", n)(win);
  await win.getByTestId("article-header").waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};
const clickTab = (name) => async (win) => {
  await win.getByTestId("article-toolbar").getByRole("button", { name, exact: true }).click();
  await win.waitForTimeout(300);
};
// 点读者工具条里的「编辑原文」并等懒加载的 CodeMirror 挂好（M2-6a：编辑态阅读区，C-012 的外观对照）。
const clickEditOriginal = async (win) => {
  await win.getByTestId("article-toolbar").getByRole("button", { name: "编辑原文", exact: true }).click();
  await win.locator('[data-testid="markdown-editor"] .cm-editor').waitFor({ state: "visible" });
  await win.waitForTimeout(300);
};
// 点进编辑器、光标移到文首：和岛里宿主发 focus 之后的状态一致（聚焦后光标在文首）。
const focusEditorAtStart = async (win) => {
  await win.locator('[data-testid="markdown-editor"] .cm-content').click();
  await win.keyboard.press("Control+Home");
  await win.waitForTimeout(200);
};
// 全选：选区底色（渲染层编辑器主题的选区色）只有选中了文字才看得见；岛一侧在聚焦之后发同一个 Ctrl+A。
const selectAllInEditor = async (win) => {
  await win.keyboard.press("Control+A");
  await win.waitForTimeout(200);
};
// G-032 补充的三类步骤：目录浮层、查找标记、滚动（M2-6b 收口）。
const clickToc = async (win) => {
  const btn = win.getByTestId("article-toolbar").getByRole("button", { name: "目录", exact: true });
  if ((await btn.getAttribute("aria-expanded")) !== "true") {
    await btn.click();
  }
  await win.waitForTimeout(300);
};
const findInReader = (query) => async (win) => {
  await win.keyboard.press("Control+f");
  await win.waitForTimeout(200);
  await win.keyboard.type(query);
  await win.locator("[data-article-reader] mark").first().waitFor({ state: "visible", timeout: 5000 });
  // 查找条属于外层外壳（已在 detail-find 整窗场景里对照），它悬浮压在阅读区右上角；
  // 截阅读区内部像素前把它隐去，只比对阅读区里的 <mark> 高亮本身。
  await win.evaluate(() => {
    const input = document.querySelector('input[placeholder*="查找"], input[placeholder*="Find"]');
    const bar = input?.closest("div.absolute");
    if (bar) bar.style.visibility = "hidden";
  });
  await win.waitForTimeout(200);
};
const LONG_MARKDOWN = Array.from({ length: 24 }, (_, i) => `## 第 ${i + 1} 节：本地优先架构\n\n本地优先的个人知识库把数据保存在用户自己的设备上，离线可读写，同步只是附加能力。这是第 ${i + 1} 段用于验证滚动位置与内容对等的长文段落。`).join("\n\n");
const scrollReaderTo = (top) => async (win) => {
  const actual = await win.evaluate((target) => {
    const root = document.querySelector("[data-article-reader]");
    const scroller = root?.querySelector(".overflow-y-auto, .overflow-auto") ?? root;
    if (!scroller) return -1;
    scroller.scrollTop = target;
    return scroller.scrollTop;
  }, top);
  if (actual < top - 1) throw new Error(`React 阅读区未能滚动到 ${top}（实际 ${actual}）`);
  await win.waitForTimeout(250);
};
const SCENARIOS = [
  { id: "web-original", itemId: "item-003", tab: "body", steps: [clickCard("如何构建本地优先的个人知识库")], view: (d) => ({ kind: "original", content: d.content, sourceKind: "body" }) },
  { id: "web-find-mark", itemId: "item-003", tab: "body", steps: [clickCard("如何构建本地优先的个人知识库"), findInReader("同步")], find: { query: "同步", index: 0 }, view: (d) => ({ kind: "original", content: d.content, sourceKind: "body" }) },
  { id: "web-scrolled", itemId: "item-003", tab: "body", detailCfg: { contentOverrides: { "item-003": LONG_MARKDOWN } }, steps: [clickCard("如何构建本地优先的个人知识库"), scrollReaderTo(240)], restoreTop: 240, view: () => ({ kind: "original", content: LONG_MARKDOWN, sourceKind: "body" }) },
  { id: "note-original", itemId: "item-001", tab: "body", steps: [clickCard("学习笔记：Go 并发模式")], view: (d) => ({ kind: "original", content: d.content, sourceKind: "body" }) },
  { id: "note-toc-open", itemId: "item-001", tab: "body", steps: [clickCard("学习笔记：Go 并发模式"), clickToc], tocOpen: true, view: (d) => ({ kind: "original", content: d.content, sourceKind: "body" }) },
  { id: "note-editing", itemId: "item-001", tab: "body", steps: [clickCard("学习笔记：Go 并发模式"), clickEditOriginal], view: (d) => ({ kind: "editor", content: d.content, sourceKind: "body" }) },
  // 聚焦态：活动行底色、光标、聚焦时的外框；岛一侧用宿主的 focus 消息（Host.FocusEditor 的页面一半）。
  { id: "note-editing-focus", itemId: "item-001", tab: "body", steps: [clickCard("学习笔记：Go 并发模式"), clickEditOriginal, focusEditorAtStart], focusEditor: true, view: (d) => ({ kind: "editor", content: d.content, sourceKind: "body" }) },
  { id: "note-editing-select", itemId: "item-001", tab: "body", steps: [clickCard("学习笔记：Go 并发模式"), clickEditOriginal, focusEditorAtStart, selectAllInEditor], focusEditor: true, selectAll: true, view: (d) => ({ kind: "editor", content: d.content, sourceKind: "body" }) },
  { id: "forum-replies", itemId: "item-012", tab: "replies", steps: [keysTo(12), clickTab("讨论")], view: (d) => ({ kind: "replies", content: afterHeading(d.content, "## 讨论") }) },
  { id: "image-recognized", itemId: "item-009", tab: "recognized", steps: [keysTo(9), clickTab("图中文字")], view: (d) => ({ kind: "markdown", content: fromHeading(d.content, "## 图中文字"), tab: "recognized" }) },
].filter((s) => !ONLY || ONLY.includes(s.id));

// ---- 页内采集：阅读区盒子与逐节点数据 ----
const STYLE_PROPS = [
  "display", "position", "color", "background-color", "font-family", "font-size", "font-weight", "font-style", "line-height", "letter-spacing",
  "text-align", "text-decoration-line", "text-transform", "white-space", "word-break", "overflow-x", "overflow-y", "opacity", "visibility",
  "margin-top", "margin-right", "margin-bottom", "margin-left", "padding-top", "padding-right", "padding-bottom", "padding-left",
  "border-top-width", "border-top-color", "border-top-style", "border-right-width", "border-bottom-width", "border-left-width",
  "border-top-left-radius", "box-shadow", "list-style-type", "width", "height", "cursor",
];

function collectInPage(props) {
  const root = document.querySelector("[data-article-reader]");
  if (!root) return null;
  const rr = root.getBoundingClientRect();
  const round = (v) => Math.round(v * 100) / 100;
  const nodes = [];
  const hidden = [];
  const hiddenEls = [];
  const idLike = /^:r[0-9a-z]+:$/;
  const walk = (el, at) => {
    const cs = getComputedStyle(el);
    const r = el.getBoundingClientRect();
    const attrs = {};
    for (const a of el.attributes) {
      if (a.name === "class") continue;
      attrs[a.name] = idLike.test(a.value) ? ":rN:" : a.value;
    }
    const own = Array.from(el.childNodes).filter((n) => n.nodeType === 3).map((n) => n.nodeValue.replace(/\s+/g, " ").trim()).filter(Boolean).join("|");
    const style = {};
    for (const p of props) style[p] = cs.getPropertyValue(p);
    // position:fixed 的 top 相对视口（CodeMirror 的 .cm-announced 是 fixed、top:-10000px）：根在视口里的位置两侧不同
    // （React 里阅读区在窗口中部、岛里在 0,0），按根相对量会凭空差一个根的偏移，所以这类节点的 y 按视口量；x 仍按根相对（left:auto 取静态位置，跟着阅读区走）。
    const ry = cs.position === "fixed" ? r.y : r.y - rr.y;
    nodes.push({ at, tag: el.tagName.toLowerCase(), cls: el.getAttribute("class") || "", attrs, own, rect: [round(r.x - rr.x), round(ry), round(r.width), round(r.height)], style });
    // display:none 的子树看不见、不占位：不进逐节点比较（两侧对不齐的隐藏窗格不该冲掉可见内容的比较），单独记下来；
    // 可见子节点的序号只数可见的。
    let visibleIndex = 0;
    for (const c of el.children) {
      const tag = c.tagName.toLowerCase();
      if (getComputedStyle(c).display === "none") {
        hidden.push({ at: `${at}/${tag}`, cls: (c.getAttribute("class") || "").slice(0, 60), aria: c.getAttribute("aria-hidden"), text: (c.textContent || "").replace(/\s+/g, " ").trim().slice(0, 30) });
        hiddenEls.push(c);
        continue;
      }
      walk(c, `${at}/${tag}[${visibleIndex}]`);
      visibleIndex += 1;
    }
  };
  walk(root, "root");
  // outerHTML 也不含隐藏子树：先给它们打个临时标记，克隆之后从克隆里删掉，再把原节点上的标记还原。
  for (const c of hiddenEls) c.setAttribute("data-probe-hidden", "");
  const clone = root.cloneNode(true);
  for (const c of hiddenEls) c.removeAttribute("data-probe-hidden");
  clone.querySelectorAll("[data-probe-hidden]").forEach((n) => n.remove());
  return {
    rect: { x: rr.x, y: rr.y, width: rr.width, height: rr.height },
    html: clone.outerHTML.replace(/:r[0-9a-z]+:/g, ":rN:"),
    nodes,
    hidden,
  };
}

// ---- 比较 ----
function compareNodes(a, b) {
  const diffs = { count: [], tag: [], cls: [], attrs: [], own: [], rect: [], style: [] };
  if (a.nodes.length !== b.nodes.length) diffs.count.push(`节点数 React ${a.nodes.length}，岛 ${b.nodes.length}`);
  const n = Math.min(a.nodes.length, b.nodes.length);
  for (let i = 0; i < n; i += 1) {
    const x = a.nodes[i];
    const y = b.nodes[i];
    const where = `#${i} ${x.at}`;
    if (x.tag !== y.tag) diffs.tag.push(`${where}：${x.tag} ≠ ${y.tag}`);
    if (x.cls !== y.cls) diffs.cls.push(`${where}：「${x.cls}」≠「${y.cls}」`);
    if (JSON.stringify(x.attrs) !== JSON.stringify(y.attrs)) diffs.attrs.push(`${where}：${JSON.stringify(x.attrs)} ≠ ${JSON.stringify(y.attrs)}`);
    if (x.own !== y.own) diffs.own.push(`${where}：「${x.own}」≠「${y.own}」`);
    if (x.rect.some((v, k) => Math.abs(v - y.rect[k]) > 0.01)) diffs.rect.push(`${where}：[${x.rect}] ≠ [${y.rect}]`);
    for (const p of STYLE_PROPS) {
      if (x.style[p] !== y.style[p]) diffs.style.push(`${where} ${p}：${x.style[p]} ≠ ${y.style[p]}`);
    }
  }
  return diffs;
}

async function pixelDiff(page, aPng, bPng) {
  return page.evaluate(async ({ a, b }) => {
    const decode = async (b64) => {
      const bmp = await createImageBitmap(await (await fetch(`data:image/png;base64,${b64}`)).blob());
      const canvas = new OffscreenCanvas(bmp.width, bmp.height);
      const ctx = canvas.getContext("2d", { willReadFrequently: true });
      ctx.drawImage(bmp, 0, 0);
      return { w: bmp.width, h: bmp.height, data: ctx.getImageData(0, 0, bmp.width, bmp.height).data };
    };
    const x = await decode(a);
    const y = await decode(b);
    if (x.w !== y.w || x.h !== y.h) return { sizeMismatch: [x.w, x.h, y.w, y.h] };
    let differing = 0, over16 = 0, maxDelta = 0;
    let minX = x.w, minY = x.h, maxX = -1, maxY = -1;
    for (let i = 0; i < x.data.length; i += 4) {
      const d = Math.max(Math.abs(x.data[i] - y.data[i]), Math.abs(x.data[i + 1] - y.data[i + 1]), Math.abs(x.data[i + 2] - y.data[i + 2]), Math.abs(x.data[i + 3] - y.data[i + 3]));
      if (d > 0) {
        differing += 1;
        if (d > 16) over16 += 1;
        if (d > maxDelta) maxDelta = d;
        const px = (i / 4) % x.w, py = Math.floor(i / 4 / x.w);
        if (px < minX) minX = px;
        if (px > maxX) maxX = px;
        if (py < minY) minY = py;
        if (py > maxY) maxY = py;
      }
    }
    return { total: x.w * x.h, differing, over16, maxDelta, box: differing ? [minX, minY, maxX - minX + 1, maxY - minY + 1] : null };
  }, { a: aPng.toString("base64"), b: bPng.toString("base64") });
}

// ---- 本机 http 服务：入口页（注入假桥）与资源 ----
const STUB = `<script>window.__out=[];window.viva={postMessage(t){window.__out.push(JSON.parse(t))},onmessage:null};</script>`;
function startServer() {
  const server = http.createServer((req, res) => {
    const name = decodeURIComponent((req.url || "/").split("?")[0]).replace(/^\//, "") || "index.html";
    // 只放行根目录与 chunks/ 一层（编辑器是懒加载的分块，见 viva/reader/assets/chunks）。
    if (name.includes("..") || name.includes("\\") || !/^(chunks\/)?[^/]+$/.test(name)) { res.writeHead(404).end(); return; }
    const file = path.join(ASSETS, name);
    if (!fs.existsSync(file) || !fs.statSync(file).isFile()) { res.writeHead(404).end(); return; }
    const ext = path.extname(name);
    const type = { ".html": "text/html; charset=utf-8", ".js": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8" }[ext] ?? "application/octet-stream";
    let body = fs.readFileSync(file);
    if (name === "index.html") body = Buffer.from(body.toString("utf8").replace("<script type=\"module\"", `${STUB}<script type="module"`), "utf8");
    res.writeHead(200, { "content-type": type, "cache-control": "no-store" });
    res.end(body);
  });
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

const record = readBuildRecord();
const matrix = loadMatrix();
const session = await openReactSession({ executable: record.exe, dist: record.dist });
const server = await startServer();
const base = `http://127.0.0.1:${server.address().port}`;
const results = [];
const refs = [];

try {
  const { page: win, cdp } = session;
  // 第一段：React 参照（每个变体 × 场景）。
  for (const key of VARIANTS) {
    const variant = parseVariantKey(key, matrix);
    const applied = await applyVariant(session, variant, matrix);
    for (const scenario of SCENARIOS) {
      await enterLibrary(session, { variant: "data", detail: scenario.detailCfg ?? {}, showReader: true, list: { fault: "", failMessage: "probe" } });
      for (const step of scenario.steps) await step(win, session);
      await win.waitForFunction(() => !!document.querySelector("[data-article-reader]"));
      await settle(win, { quietMs: 250 });
      const data = await win.evaluate(collectInPage, STYLE_PROPS);
      if (!data) throw new Error(`${key} ${scenario.id}：React 里没有 [data-article-reader]`);
      // 参照截图两份：整数设备框（Viva 子窗口真正盖住的那块，默认的判定依据）与阅读区的小数矩形（旧口径，--frac-shift 与对照读数用）。
      const device = deviceRectOf(data.rect);
      const png = await captureRegionPng(session, device);
      const pngRect = await captureRegionPng(session, data.rect);
      refs.push({ key, scenario, applied, data, png, pngRect, device });
      await leaveLibrary(session);
    }
  }

  // 第二段：岛。同一个窗口导航到本机服务，视口钉成阅读区尺寸，按协议重放 init 与 view。
  for (const ref of refs) {
    const { key, scenario, applied, data, png, pngRect, device } = ref;
    // 子窗口（岛的视口）：Viva 的取整框，宽高是取整后右下角减左上角（不是各自取整的宽高）。
    let width = device.width;
    let height = device.height;
    const layout = FRAC_SHIFT || NO_LAYOUT ? null : layoutFor(data.rect);
    const fractional = Math.abs(width - data.rect.width) > 0.001 || Math.abs(height - data.rect.height) > 0.001 || Math.abs(data.rect.x - Math.round(data.rect.x)) > 0.001 || Math.abs(data.rect.y - Math.round(data.rect.y)) > 0.001;
    const fracX = data.rect.x - Math.floor(data.rect.x);
    const fracY = data.rect.y - Math.floor(data.rect.y);
    const shift = FRAC_SHIFT && fractional;
    if (shift) {
      width = Math.ceil(data.rect.x + data.rect.width) - Math.floor(data.rect.x);
      height = Math.ceil(data.rect.y + data.rect.height) - Math.floor(data.rect.y);
    }
    await cdp.send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: false });
    await win.goto(`${base}/index.html`, { waitUntil: "load" });
    await win.addStyleTag({ content: SETTLE_CSS });
    if (shift) await win.addStyleTag({ content: `html{box-sizing:border-box;padding:${fracY}px 0 0 ${fracX}px}` });
    await win.waitForFunction(() => window.__out.some((e) => e.t === "ready"));
    // 渲染层的 data-motion 可以是 "system"（跟随系统「减少动态效果」）；宿主发给页面的是合并之后的实际档位，本会话的系统设置是标准动效。
    const motion = ["standard", "reduced", "off"].includes(applied.motion) ? applied.motion : "standard";
    // viewportWidth：岛的视口只有阅读区那么宽，响应式断点要按窗口宽度判定（宿主给，见 reader.Theme.ViewportWidth）。
    const theme = { dark: applied.dark, hue: Number(applied.hue), saturation: Number(applied.saturation), basePx: applied.rootFontPx, motion, viewportWidth: applied.viewport[0] };
    const send = (msg) => win.evaluate((text) => window.viva.onmessage({ data: text }), JSON.stringify(msg));
    await send({ t: "init", v: PROTOCOL_VERSION, theme, lang: "zh" });
    // 真实宿主在 init 之后、view 之前发 layout（host.go flush 的顺序）；整数设备像素上的阅读区不发。
    if (layout) await send({ t: "layout", layout });
    await send({
      t: "view", seq: 1, itemId: scenario.itemId, tab: scenario.tab, view: scenario.view(detail(scenario.itemId)),
      restoreTop: scenario.restoreTop ?? null, catalogOpen: false, tocOpen: scenario.tocOpen ?? false,
    });
    await win.waitForFunction(() => window.__out.some((e) => e.t === "rendered" && e.seq === 1), null, { timeout: 15000 });
    if (scenario.find) {
      await send({ t: "find", query: scenario.find.query, index: scenario.find.index });
      await win.locator("[data-article-reader] mark").first().waitFor({ state: "visible", timeout: 5000 });
    }
    if (scenario.restoreTop != null) {
      await win.waitForFunction((target) => {
        const root = document.querySelector("[data-article-reader]");
        const scroller = root?.querySelector(".overflow-y-auto, .overflow-auto") ?? root;
        return scroller && scroller.scrollTop >= target - 1;
      }, scenario.restoreTop, { timeout: 5000 });
    }
    if (scenario.focusEditor) {
      await send({ t: "focus" });
      await win.waitForFunction(() => document.activeElement?.classList.contains("cm-content"), null, { timeout: 5000 });
    }
    if (scenario.selectAll) await selectAllInEditor(win);
    await settle(win, { quietMs: 250 });
    const island = await win.evaluate(collectInPage, STYLE_PROPS);
    if (!island) throw new Error(`${key} ${scenario.id}：岛里没有 [data-article-reader]`);
    // 岛实际生效的外观必须与 React 一致，否则后面的差异都是外观没对上造成的，不是内容的差异。
    const islandApplied = await readAppliedAppearance(win);
    const appearanceMismatch = ["dark", "hue", "saturation", "rootFontPx"].filter((k) => String(islandApplied[k]) !== String(applied[k]));
    // 旧口径：React 的小数矩形与岛的根节点矩形（带版面偏移时根节点的矩形也带同样的小数部分）。
    const islandPngRect = await captureRegionPng(session, island.rect);
    // 默认口径：子窗口盖住的整个整数视口，对 React 的整数设备框。--frac-shift 保持旧口径。
    const islandPng = FRAC_SHIFT ? islandPngRect : await captureRegionPng(session, { x: 0, y: 0, width, height });
    const diffs = compareNodes(data, island);
    const htmlSame = data.html === island.html;
    const pixels = await pixelDiff(win, FRAC_SHIFT ? pngRect : png, islandPng);
    const pixelsRect = FRAC_SHIFT ? pixels : await pixelDiff(win, pngRect, islandPngRect);
    const row = {
      mode: FRAC_SHIFT ? "frac-shift" : NO_LAYOUT ? "no-layout" : "layout", layout, deviceRect: device, pixelsRect,
      variant: key, scene: scenario.id, region: [data.rect.width, data.rect.height], reactRect: [data.rect.x, data.rect.y, data.rect.width, data.rect.height], islandRegion: [island.rect.x, island.rect.y, island.rect.width, island.rect.height],
      fractionalRegion: fractional, appearanceMismatch, islandApplied, reactApplied: applied, nodes: [data.nodes.length, island.nodes.length], htmlSame,
      hidden: { react: data.hidden, island: island.hidden },
      counts: Object.fromEntries(Object.entries(diffs).map(([k, v]) => [k, v.length])), pixels, samples: Object.fromEntries(Object.entries(diffs).map(([k, v]) => [k, v.slice(0, 4)])),
    };
    results.push(row);
    if (outDir) {
      const dir = path.resolve(outDir, key);
      fs.mkdirSync(dir, { recursive: true });
      fs.writeFileSync(path.join(dir, `${scenario.id}.react.png`), FRAC_SHIFT ? pngRect : png);
      fs.writeFileSync(path.join(dir, `${scenario.id}.island.png`), islandPng);
      fs.writeFileSync(path.join(dir, `${scenario.id}.react.html`), data.html);
      fs.writeFileSync(path.join(dir, `${scenario.id}.island.html`), island.html);
    }
  }
} finally {
  server.close();
  await session.close();
}

if (outDir) {
  fs.mkdirSync(path.resolve(outDir), { recursive: true });
  fs.writeFileSync(path.resolve(outDir, "report.json"), JSON.stringify(results, null, 2));
}
let failed = 0;
for (const r of results) {
  const c = r.counts;
  const structural = c.count + c.tag + c.cls + c.attrs + c.own;
  const clean = structural === 0 && c.rect === 0 && c.style === 0 && r.htmlSame && r.pixels.differing === 0;
  if (!clean) failed += 1;
  if (r.appearanceMismatch.length) console.log(`外观没对上 ${r.variant} ${r.scene}：${r.appearanceMismatch.join("、")}；岛 ${JSON.stringify(r.islandApplied)}；React ${JSON.stringify(r.reactApplied)}`);
  const px = r.pixels.sizeMismatch ? `尺寸不同 ${r.pixels.sizeMismatch}` : `像素 ${r.pixels.differing}/${r.pixels.total}（>16：${r.pixels.over16}，最大差 ${r.pixels.maxDelta}${r.pixels.box ? `，范围 ${r.pixels.box}` : ""}）`;
  const legacy = r.mode === "frac-shift" ? "" : `，旧口径（小数矩形）像素 ${r.pixelsRect.sizeMismatch ? `尺寸不同 ${r.pixelsRect.sizeMismatch}` : r.pixelsRect.differing}`;
  const layoutText = r.mode === "layout" ? (r.layout ? `，layout ${JSON.stringify(r.layout)}` : "，无 layout（整数设备像素）") : "";
  console.log(`${clean ? "相同" : "有差"} ${r.variant} ${r.scene}：节点 ${r.nodes.join("/")}，结构 ${structural}，盒子 ${c.rect}，样式 ${c.style}，outerHTML ${r.htmlSame ? "相同" : "不同"}，${px}${legacy}${layoutText}，阅读区 ${r.region.join("x")}，岛阅读区 ${r.islandRegion.join(",")}${r.fractionalRegion ? `（阅读区尺寸非整数，React 阅读区 ${r.reactRect.join(",")}）` : ""}，隐藏子树 React ${r.hidden.react.length}/岛 ${r.hidden.island.length}`);
  if (!clean) {
    for (const [k, list] of Object.entries(r.samples)) for (const s of list) console.log(`    ${k}：${s}`);
  }
}
console.log(`共 ${results.length} 项，有差 ${failed} 项`);
process.exitCode = failed ? 1 : 0;
