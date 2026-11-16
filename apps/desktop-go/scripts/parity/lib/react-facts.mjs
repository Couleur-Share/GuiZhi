/**
 * React 侧事实采集：对当前页面取一份「可比较的事实」（schema 1）。
 *
 * 事实来源（都经 CDP，口径是 CSS 像素、设备像素比 1，见 react-state.mjs 顶部说明）：
 *   - 可访问树：Accessibility.getFullAXTree（Chromium 自己算的角色与可访问名，是 L1 的机器比较来源）；
 *   - 外框与计算样式：对每个可访问节点对应的 DOM 节点，页内 getBoundingClientRect / getComputedStyle；
 *   - 文字行矩形：遍历文本节点的 Range.getClientRects，供 L3 像素比较遮掉文字区域；
 *   - 平台字体：CSS.getPlatformFontsForNode，按（字体族、字号、字重、字形）去重取证；
 *   - 人读的同一份结构：Playwright 的 ariaSnapshot() YAML。
 * 采集只读页面：不改 DOM、不改样式。
 *
 * 归一化（角色映射、静态文本折叠、名称空白折叠）不在这里做——事实保留原样，比较端按 §2.2 的规则归一，
 * 这样规则调整不需要重采 React。
 */

/** 计算样式清单：覆盖颜色、背景、边框、圆角、阴影、字体、间距与布局决定项。 */
export const STYLE_PROPS = [
  "display", "position", "visibility", "opacity", "overflow-x", "overflow-y", "z-index",
  "color", "background-color", "background-image",
  "border-top-width", "border-right-width", "border-bottom-width", "border-left-width",
  "border-top-style", "border-right-style", "border-bottom-style", "border-left-style",
  "border-top-color", "border-right-color", "border-bottom-color", "border-left-color",
  "border-top-left-radius", "border-top-right-radius", "border-bottom-right-radius", "border-bottom-left-radius",
  "box-shadow", "outline-width", "outline-style", "outline-color", "outline-offset",
  "padding-top", "padding-right", "padding-bottom", "padding-left",
  "font-family", "font-size", "font-weight", "font-style", "line-height", "letter-spacing",
  "text-align", "text-transform", "text-decoration-line", "text-overflow", "white-space",
  "backdrop-filter", "filter", "transform", "cursor",
];

const r3 = (n) => Math.round(n * 1000) / 1000;

/** 在页内对一批节点（Element 或 Text）取外框与样式；通过 Runtime.callFunctionOn 的多个参数传入。 */
const MEASURE_FN = `function (...nodes) {
  const PROPS = ${JSON.stringify(STYLE_PROPS)};
  const round = (n) => Math.round(n * 1000) / 1000;
  return nodes.map((n) => {
    if (n.nodeType === 3) {
      const range = document.createRange();
      range.selectNodeContents(n);
      const b = range.getBoundingClientRect();
      return { k: "t", box: [round(b.x), round(b.y), round(b.width), round(b.height)], text: n.data };
    }
    if (n.nodeType !== 1) return { k: "o" };
    const b = n.getBoundingClientRect();
    const cs = getComputedStyle(n);
    const style = {};
    for (const p of PROPS) style[p] = cs.getPropertyValue(p);
    return {
      k: "e",
      tag: n.localName,
      testId: n.getAttribute("data-testid"),
      box: [round(b.x), round(b.y), round(b.width), round(b.height)],
      scroll: [n.scrollLeft, n.scrollTop, n.scrollWidth, n.scrollHeight],
      style,
    };
  });
}`;

const TEXT_RECTS_EXPR = `(() => {
  const out = [];
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let node;
  while ((node = walker.nextNode())) {
    if (!node.data.trim()) continue;
    const el = node.parentElement;
    if (!el) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    for (const b of range.getClientRects()) {
      if (b.width > 0 && b.height > 0) out.push([Math.round(b.x * 1000) / 1000, Math.round(b.y * 1000) / 1000, Math.round(b.width * 1000) / 1000, Math.round(b.height * 1000) / 1000]);
    }
  }
  return out;
})()`;

const FONT_KEYS_EXPR = `(() => {
  const reps = [];
  const keys = [];
  const seen = new Set();
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  let node;
  while ((node = walker.nextNode())) {
    if (!node.data.trim()) continue;
    const el = node.parentElement;
    if (!el) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const key = [cs.fontFamily, cs.fontSize, cs.fontWeight, cs.fontStyle].join("|");
    if (seen.has(key)) continue;
    seen.add(key);
    reps.push(el);
    keys.push(key);
  }
  window.__parityFontReps = reps;
  return keys;
})()`;

function propsToObject(properties = []) {
  const out = {};
  for (const p of properties) out[p.name] = p.value?.value;
  return out;
}

/** 把 AX 平坦节点按 DFS 顺序整理成带 parent/depth 的列表；ignored 节点不输出，其子节点挂到最近的非 ignored 祖先。 */
export function flattenAxTree(axNodes) {
  const byId = new Map(axNodes.map((n) => [n.nodeId, n]));
  const root = axNodes.find((n) => !n.parentId) ?? axNodes[0];
  const out = [];
  const walk = (id, parentIdx, depth) => {
    const n = byId.get(id);
    if (!n) return;
    let myIdx = parentIdx;
    let childDepth = depth;
    // InlineTextBox 是 Chromium 内部的断行片段，没有对应 DOM 节点、取不到外框，文字外框由 textRects 单独采集。
    if (!n.ignored && n.role?.value !== "InlineTextBox") {
      myIdx = out.length;
      out.push({
        role: n.role?.value ?? "",
        name: n.name?.value ?? "",
        description: n.description?.value ?? undefined,
        value: n.value?.value ?? undefined,
        props: propsToObject(n.properties),
        backendId: n.backendDOMNodeId,
        parent: parentIdx,
        depth,
      });
      childDepth = depth + 1;
    }
    for (const c of n.childIds ?? []) walk(c, myIdx, childDepth);
  };
  if (root) walk(root.nodeId, -1, 0);
  return out;
}

/** 区域外的节点不要：外框与区域相交才保留；没有外框（未渲染）的保留（它们仍是可访问树的一部分）。 */
function intersects(box, region) {
  const [x, y, w, h] = box;
  return x < region.x + region.width && x + w > region.x && y < region.y + region.height && y + h > region.y;
}

async function resolveRegion(page, regionSelector) {
  return page.evaluate((sel) => {
    const vw = innerWidth, vh = innerHeight;
    if (!sel) return { x: 0, y: 0, width: vw, height: vh, selector: null };
    const el = document.querySelector(sel);
    if (!el) throw new Error("找不到区域元素：" + sel);
    const b = el.getBoundingClientRect();
    // 取整并裁到视口内：截图 clip 与事实坐标用同一个整数原点。
    const x = Math.max(0, Math.floor(b.x));
    const y = Math.max(0, Math.floor(b.y));
    const right = Math.min(vw, Math.ceil(b.x + b.width));
    const bottom = Math.min(vh, Math.ceil(b.y + b.height));
    return { x, y, width: right - x, height: bottom - y, selector: sel };
  }, regionSelector);
}

async function measureNodes(cdp, backendIds) {
  const resolved = await Promise.all(
    backendIds.map((backendNodeId) => cdp.send("DOM.resolveNode", { backendNodeId, objectGroup: "parity" }).catch(() => null)),
  );
  const slots = [];
  const args = [];
  resolved.forEach((r, i) => {
    if (r?.object?.objectId) {
      slots.push(i);
      args.push({ objectId: r.object.objectId });
    }
  });
  const measured = new Array(backendIds.length).fill(null);
  if (args.length > 0) {
    const { result, exceptionDetails } = await cdp.send("Runtime.callFunctionOn", {
      objectId: args[0].objectId,
      functionDeclaration: MEASURE_FN,
      arguments: args,
      returnByValue: true,
      objectGroup: "parity",
    });
    if (exceptionDetails) throw new Error(`页内测量失败：${exceptionDetails.exception?.description ?? exceptionDetails.text}`);
    result.value.forEach((m, j) => { measured[slots[j]] = m; });
  }
  await cdp.send("Runtime.releaseObjectGroup", { objectGroup: "parity" });
  return measured;
}

async function platformFonts(cdp) {
  await cdp.send("DOM.getDocument", { depth: 0 });
  const { result } = await cdp.send("Runtime.evaluate", { expression: FONT_KEYS_EXPR, returnByValue: true });
  const keys = result.value ?? [];
  const out = [];
  for (let i = 0; i < keys.length; i++) {
    const obj = await cdp.send("Runtime.evaluate", { expression: `window.__parityFontReps[${i}]`, objectGroup: "parity-fonts" });
    const { nodeId } = await cdp.send("DOM.requestNode", { objectId: obj.result.objectId });
    const { fonts } = await cdp.send("CSS.getPlatformFontsForNode", { nodeId });
    const [family, size, weight, style] = keys[i].split("|");
    out.push({
      key: keys[i], family, size, weight, style,
      platform: fonts.map((f) => ({ family: f.familyName, postScript: f.postScriptName, custom: f.isCustomFont, glyphs: f.glyphCount })),
    });
  }
  await cdp.send("Runtime.evaluate", { expression: "delete window.__parityFontReps" });
  await cdp.send("Runtime.releaseObjectGroup", { objectGroup: "parity-fonts" });
  return out;
}

/**
 * 采集当前页面的事实。
 * @param session openReactSession 的返回值
 * @param {{regionSelector?: string|null, scene: string, variant: object}} options
 */
export async function collectFacts(session, { regionSelector = null, scene, variant }) {
  const { page, cdp } = session;
  await cdp.send("DOM.enable");
  await cdp.send("CSS.enable");
  await cdp.send("Accessibility.enable");
  const region = await resolveRegion(page, regionSelector);
  const { nodes: axNodes } = await cdp.send("Accessibility.getFullAXTree");
  const ax = flattenAxTree(axNodes);

  const withIds = ax.map((n, i) => ({ n, i })).filter(({ n }) => n.backendId !== undefined);
  const measured = await measureNodes(cdp, withIds.map(({ n }) => n.backendId));
  const measuredByIndex = new Map(withIds.map(({ i }, k) => [i, measured[k]]));

  const nodes = [];
  const indexMap = new Map(); // 原 AX 序号 → 过滤后序号
  ax.forEach((n, i) => {
    const m = measuredByIndex.get(i);
    const box = m?.box ?? null;
    if (box && !intersects(box, region)) return;
    indexMap.set(i, nodes.length);
    nodes.push({
      role: n.role,
      name: n.name,
      ...(n.description !== undefined ? { description: n.description } : {}),
      ...(n.value !== undefined ? { value: n.value } : {}),
      props: n.props,
      // 位置换算到区域坐标系；尺寸不变
      box: box ? [r3(box[0] - region.x), r3(box[1] - region.y), box[2], box[3]] : null,
      kind: m?.k ?? null,
      ...(m?.tag ? { tag: m.tag } : {}),
      ...(m?.testId ? { testId: m.testId } : {}),
      ...(m?.k === "e" ? { style: m.style, scroll: m.scroll } : {}),
      ...(m?.k === "t" ? { text: m.text } : {}),
      parentOrig: n.parent,
      depth: n.depth,
    });
  });
  // 父节点序号重映射（被过滤掉的父节点沿祖先链往上找）
  const keptParent = (orig) => {
    let p = orig;
    while (p >= 0 && !indexMap.has(p)) p = ax[p].parent;
    return p >= 0 ? indexMap.get(p) : -1;
  };
  for (const n of nodes) {
    n.parent = keptParent(n.parentOrig);
    delete n.parentOrig;
  }

  const textRects = (await cdp.send("Runtime.evaluate", { expression: TEXT_RECTS_EXPR, returnByValue: true })).result.value
    .filter((b) => intersects(b, region))
    .map((b) => [r3(b[0] - region.x), r3(b[1] - region.y), b[2], b[3]]);
  const fonts = await platformFonts(cdp);
  const aria = await page.locator(regionSelector ?? "body").ariaSnapshot();
  const chromium = (await cdp.send("Browser.getVersion")).product;

  return {
    schema: 1,
    scene,
    variant,
    region: { x: region.x, y: region.y, width: region.width, height: region.height, selector: region.selector },
    capture: { dpr: 1, chromium },
    nodes,
    textRects,
    fonts,
    aria,
  };
}

/** CDP 直连截图：区域 clip，scale 1（得到的像素数 = 区域的 CSS 像素数）。 */
export async function captureRegionPng(session, region) {
  const { data } = await session.cdp.send("Page.captureScreenshot", {
    format: "png",
    fromSurface: true,
    captureBeyondViewport: false,
    clip: { x: region.x, y: region.y, width: region.width, height: region.height, scale: 1 },
  });
  return Buffer.from(data, "base64");
}
