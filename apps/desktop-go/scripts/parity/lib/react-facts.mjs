/**
 * React 侧事实采集：对当前页面取一份「可比较的事实」（schema 2：在 schema 1 之上增加 texts 与 paints）。
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

/**
 * 页内工具：对一批元素取「在视口里看得见的部分」（IntersectionObserver 的 intersectionRect）。
 * 浏览器按包含块链裁掉祖先 overflow、fixed 与变换之后给出结果，所以滚出滚动容器、被裁掉的内容得到零面积。
 * 没有元素（null）的位置返回 null。
 *
 * 为什么要这个：渲染层把滚出可视区的节点仍留在 DOM 与可访问树里，Viva 只对看得见的内容出无障碍节点与文字
 * （台账 A-010）。对照只比「呈现出来」的部分，两边都按看得见的内容采，才是同口径。
 */
const VISIBLE_RECTS_SRC = `const visibleRects = (targets) => new Promise((resolve) => {
  const out = new Array(targets.length).fill(null);
  const index = new Map();
  targets.forEach((t, i) => {
    if (!t) return;
    if (!index.has(t)) index.set(t, []);
    index.get(t).push(i);
  });
  if (index.size === 0) { resolve(out); return; }
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) {
      const r = e.intersectionRect;
      for (const i of index.get(e.target) ?? []) out[i] = [Math.round(r.x * 1000) / 1000, Math.round(r.y * 1000) / 1000, Math.round(r.width * 1000) / 1000, Math.round(r.height * 1000) / 1000];
    }
    io.disconnect();
    resolve(out);
  }, { threshold: 0 });
  for (const t of index.keys()) io.observe(t);
});`;

/** 在页内对一批节点（Element 或 Text）取外框与样式；通过 Runtime.callFunctionOn 的多个参数传入。 */
const MEASURE_FN = `async function (...nodes) {
  const PROPS = ${JSON.stringify(STYLE_PROPS)};
  const round = (n) => Math.round(n * 1000) / 1000;
  ${VISIBLE_RECTS_SRC}
  // 视觉隐藏（sr-only，外框不超过 1px）的原生勾选/单选框，由同一容器里 aria-hidden 的兄弟元素代画（Checkbox.tsx：input.sr-only 加 span[aria-hidden]）。
  // 它在可访问树里是一个正常的 checkbox / radio，但视觉上就是那个方框；Viva 的复选框节点也是方框本身。
  // 所以外框与可见部分都取代画元素的，不然这个节点会被「可见部分为零」的规则整个丢掉，L1 就会报 Viva 多了一个节点。
  const proxyOf = (n) => {
    if (n.nodeType !== 1 || n.localName !== "input" || (n.type !== "checkbox" && n.type !== "radio")) return null;
    const r = n.getBoundingClientRect();
    if (r.width > 1 || r.height > 1) return null;
    return n.parentElement?.querySelector(":scope > [aria-hidden='true']") ?? null;
  };
  const measured = nodes.map((n) => {
    if (n.nodeType === 3) {
      const range = document.createRange();
      range.selectNodeContents(n);
      const b = range.getBoundingClientRect();
      return { k: "t", box: [round(b.x), round(b.y), round(b.width), round(b.height)], text: n.data };
    }
    if (n.nodeType !== 1) return { k: "o" };
    const proxy = proxyOf(n);
    const b = (proxy ?? n).getBoundingClientRect();
    const cs = getComputedStyle(n);
    const style = {};
    for (const p of PROPS) style[p] = cs.getPropertyValue(p);
    return {
      k: "e",
      tag: n.localName,
      testId: n.getAttribute("data-testid"),
      box: [round(b.x), round(b.y), round(b.width), round(b.height)],
      ...(proxy ? { proxy: proxy.localName } : {}),
      scroll: [n.scrollLeft, n.scrollTop, n.scrollWidth, n.scrollHeight],
      style,
    };
  });
  // 文字节点取父元素的可见部分；非元素、非文字的节点没有。
  const visible = await visibleRects(nodes.map((n) => (n.nodeType === 1 ? (proxyOf(n) ?? n) : n.nodeType === 3 ? n.parentElement : null)));
  measured.forEach((m, i) => { if (visible[i]) m.vis = visible[i]; });
  return measured;
}`;

/**
 * 文字节点（schema 2）：原文、每个行片段的矩形（Range 的内容区矩形，高 = ascent+descent）、字体键、
 * 字号/字重/行高的计算值（行高 normal 时为 null）、颜色。比较端用它核对文字的宽度、行盒与基线。
 *
 * 单行输入框（input）里的值与占位符不是 DOM 文本节点，但它们是看得见的文字，Viva 一侧也把它们当文字画：
 * 这里按输入框的内容盒补一条记录（值非空取值，否则取占位符；密码框不采）。
 * 位置的算法与普通文字一致：行盒在内容盒里垂直居中，内容区（高 = 取整的 ascent + descent）在行盒里
 * 上方留 floor((行高 − (A+D)) / 2)；宽度取 canvas 按同一字体量出的排版宽度（两者共用 Chromium 的字体匹配与整形）。
 */
const TEXTS_EXPR = `(async () => {
  ${VISIBLE_RECTS_SRC}
  const out = [];
  const owners = []; // 与 out 一一对应的所属元素，用来按可见部分裁掉滚出可视区的文字
  const round = (n) => Math.round(n * 1000) / 1000;
  const measure = document.createElement("canvas").getContext("2d");
  const skipTypes = new Set(["password", "hidden", "checkbox", "radio", "file", "range", "color", "button", "submit", "reset", "image"]);
  const fieldText = (el) => {
    if (el.localName !== "input" || skipTypes.has(el.type)) return null;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") return null;
    // 采「看得见的文字」：text-transform（取色器的十六进制框是 uppercase）改变的是画出来的字，不是 value。
    const raw = el.value || el.getAttribute("placeholder") || "";
    const text = cs.textTransform === "uppercase" ? raw.toUpperCase() : cs.textTransform === "lowercase" ? raw.toLowerCase() : raw;
    if (!text) return null;
    const b = el.getBoundingClientRect();
    if (b.width <= 0 || b.height <= 0) return null;
    const px = (name) => parseFloat(cs.getPropertyValue(name)) || 0;
    const left = b.x + px("border-left-width") + px("padding-left");
    const top = b.y + px("border-top-width") + px("padding-top");
    const contentW = b.width - px("border-left-width") - px("border-right-width") - px("padding-left") - px("padding-right");
    const contentH = b.height - px("border-top-width") - px("border-bottom-width") - px("padding-top") - px("padding-bottom");
    measure.font = [cs.fontStyle, cs.fontWeight, cs.fontSize, cs.fontFamily].join(" ");
    const m = measure.measureText(text);
    const ascent = Math.round(m.fontBoundingBoxAscent);
    const descent = Math.round(m.fontBoundingBoxDescent);
    const lh = parseFloat(cs.lineHeight);
    const lineHeight = Number.isFinite(lh) ? lh : ascent + descent;
    const lineTop = top + (contentH - lineHeight) / 2;
    const y = lineTop + Math.floor((lineHeight - (ascent + descent)) / 2);
    const width = Math.min(m.width, contentW);
    if (width <= 0) return null;
    return {
      text,
      rects: [[round(left), round(y), round(width), ascent + descent]],
      font: [cs.fontFamily, cs.fontSize, cs.fontWeight, cs.fontStyle].join("|"),
      size: parseFloat(cs.fontSize),
      weight: Number(cs.fontWeight),
      lineHeight: Number.isFinite(lh) ? lh : null,
      color: el.value ? cs.color : getComputedStyle(el, "::placeholder").color,
      tag: el.localName,
      field: true,
      testId: el.closest("[data-testid]")?.getAttribute("data-testid") ?? null,
    };
  };
  // 看得见的文字：自身与全部祖先 opacity 的乘积，再乘文字色 alpha。低于 0.01 的文字不是「呈现出来的文字」——
  // 悬停或焦点在行内时淡出的行尾计数（opacity-0）、text-transparent 的叠层。口径与 Viva 一侧 collectViva 一致
  // （kit.VisibleInkAlpha = 0.01：那边透明的文字不进采集），两边比的都是用户看得见的字（台账 G-010）。
  const inkAlpha = (el, cs) => {
    let o = 1;
    for (let e = el; e && e !== document.documentElement; e = e.parentElement) {
      o *= Number(getComputedStyle(e).opacity);
      if (o < 0.001) return 0;
    }
    const a = cs.color.startsWith("rgba(") ? parseFloat(cs.color.split(",")[3]) : 1;
    return o * (Number.isFinite(a) ? a : 1);
  };
  // text-overflow: ellipsis 截断：DOM 文本仍是全文，浏览器画出的是「放得下的前缀 + …」。
  // 采集的是「看得见的文字」（Viva 的 Ellipsis 同样只留可见部分），所以按 Chromium 的规则复算截断点：
  // 取最长的前缀，使「前缀宽度 + 省略号宽度」不超过内容盒宽度。只处理单行、文字节点直接位于截断元素之下的常见写法
  // （truncate 类）；其余写法不复算，对照时文字不一致会直接报出来，不会悄悄放过。
  const ellipsize = (node, el, cs, rects) => {
    if (cs.textOverflow !== "ellipsis" || cs.overflowX === "visible") return null;
    if (el.scrollWidth <= el.clientWidth) return null;
    // 单行才复算（折行的文字没有省略号）；截断的那一行 Chromium 会给出一个以上的矩形片段，同一 y。
    if (rects.some((r) => r[1] !== rects[0][1])) return null;
    const px = (name) => parseFloat(cs.getPropertyValue(name)) || 0;
    const avail = el.clientWidth - px("padding-left") - px("padding-right");
    measure.font = [cs.fontStyle, cs.fontWeight, cs.fontSize, cs.fontFamily].join(" ");
    const ellipsisWidth = measure.measureText("…").width;
    const text = node.data;
    const probe = document.createRange();
    const widthOf = (k) => {
      probe.setStart(node, 0);
      probe.setEnd(node, k);
      return probe.getBoundingClientRect().width;
    };
    let lo = 0;
    let hi = text.length;
    while (lo < hi) {
      const mid = (lo + hi + 1) >> 1;
      if (widthOf(mid) + ellipsisWidth <= avail + 1e-6) lo = mid;
      else hi = mid - 1;
    }
    const code = text.charCodeAt(lo - 1);
    if (lo > 0 && code >= 0xd800 && code <= 0xdbff) lo -= 1; // 不把代理对劈开
    const [x, y, , h] = rects[0];
    return { shown: text.slice(0, lo) + "…", rect: [x, y, round(widthOf(lo) + ellipsisWidth), h] };
  };
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT | NodeFilter.SHOW_ELEMENT);
  let node;
  while ((node = walker.nextNode())) {
    if (node.nodeType === 1) {
      const field = fieldText(node);
      if (field) { out.push(field); owners.push(node); }
      continue;
    }
    if (!node.data.trim()) continue;
    const el = node.parentElement;
    if (!el) continue;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    if (inkAlpha(el, cs) < 0.01) continue;
    const range = document.createRange();
    range.selectNodeContents(node);
    const rects = [];
    for (const b of range.getClientRects()) {
      if (b.width > 0 && b.height > 0) rects.push([round(b.x), round(b.y), round(b.width), round(b.height)]);
    }
    if (rects.length === 0) continue;
    const lh = parseFloat(cs.lineHeight);
    const cut = ellipsize(node, el, cs, rects);
    out.push({
      // 被省略号截断时，text 是看得见的「前缀 + …」，full 才是 DOM 原文；rects 取截断后的宽度。
      text: cut ? cut.shown : node.data,
      ...(cut ? { full: node.data } : {}),
      rects: cut ? [cut.rect] : rects,
      font: [cs.fontFamily, cs.fontSize, cs.fontWeight, cs.fontStyle].join("|"),
      size: parseFloat(cs.fontSize),
      weight: Number(cs.fontWeight),
      lineHeight: Number.isFinite(lh) ? lh : null,
      color: cs.color,
      tag: el.localName,
      testId: el.closest("[data-testid]")?.getAttribute("data-testid") ?? null,
    });
    owners.push(el);
  }
  // 只留与所属元素「看得见的部分」相交的行片段：滚出滚动容器的文字不算呈现出来的文字。
  const visible = await visibleRects(owners);
  const kept = [];
  out.forEach((t, i) => {
    const v = visible[i];
    if (v) {
      t.rects = t.rects.filter(([x, y, w, h]) => x < v[0] + v[2] && x + w > v[0] && y < v[1] + v[3] && y + h > v[1]);
      // 文字被祖先的 overflow 裁掉一部分（滚动区域的底边压住按钮文字之类）：记下看得见的区域，
      // 比较端量墨迹时只取这一块，免得把裁剪线以外的别的图形（页脚、边线）当成文字的墨迹。
      const cut = t.rects.some(([x, y, w, h]) => x < v[0] - 0.5 || y < v[1] - 0.5 || x + w > v[0] + v[2] + 0.5 || y + h > v[1] + v[3] + 0.5);
      if (cut) t.clip = v;
    }
    if (t.rects.length > 0) kept.push(t);
  });
  return kept;
})()`;

/**
 * 有绘制的元素（schema 2）：背景色/背景图、边框、圆角、阴影、轮廓，以及 SVG 图标（颜色取 currentColor）。
 * 按文档顺序（即默认的绘制先后）输出，外框是未裁剪的边框盒。比较端用它给 L3 的差异定位到具体元素与令牌。
 */
const PAINTS_EXPR = `(async () => {
  ${VISIBLE_RECTS_SRC}
  const out = [];
  const owners = [];
  const round = (n) => Math.round(n * 1000) / 1000;
  const clear = (c) => c === "rgba(0, 0, 0, 0)" || c === "transparent";
  // 有效不透明度：自身与全部祖先 opacity 的乘积。opacity-0 的容器里（悬停才显现的行内工具条）的图标自己的 opacity 仍是 1，
  // 但整体看不见；采集时记 invisible，比较端据此不比这类图标的墨迹、也不遮罩它们底下的内容。
  const effectiveOpacity = (node) => {
    let o = 1;
    for (let e = node; e && e !== document.documentElement; e = e.parentElement) {
      o *= Number(getComputedStyle(e).opacity);
      if (o < 0.001) return 0;
    }
    return o;
  };
  let order = 0;
  for (const el of document.body.querySelectorAll("*")) {
    order += 1;
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.visibility === "hidden") continue;
    const b = el.getBoundingClientRect();
    if (b.width <= 0 || b.height <= 0) continue;
    const sides = ["top", "right", "bottom", "left"];
    const border = sides.map((s) => ({
      w: parseFloat(cs.getPropertyValue("border-" + s + "-width")),
      style: cs.getPropertyValue("border-" + s + "-style"),
      color: cs.getPropertyValue("border-" + s + "-color"),
    }));
    const hasBorder = border.some((x) => x.w > 0 && x.style !== "none" && !clear(x.color));
    const bg = clear(cs.backgroundColor) ? null : cs.backgroundColor;
    const bgImage = cs.backgroundImage === "none" ? null : cs.backgroundImage;
    const shadow = cs.boxShadow === "none" ? null : cs.boxShadow;
    const outline = cs.outlineStyle !== "none" && parseFloat(cs.outlineWidth) > 0 ? { w: parseFloat(cs.outlineWidth), color: cs.outlineColor, offset: parseFloat(cs.outlineOffset) } : null;
    const svg = el instanceof SVGSVGElement;
    if (!(bg || bgImage || hasBorder || shadow || outline || svg)) continue;
    out.push({
      order,
      tag: el.localName,
      testId: el.getAttribute("data-testid"),
      box: [round(b.x), round(b.y), round(b.width), round(b.height)],
      bg, bgImage, shadow, outline,
      border: hasBorder ? border : null,
      radius: ["top-left", "top-right", "bottom-right", "bottom-left"].map((c) => cs.getPropertyValue("border-" + c + "-radius")),
      opacity: Number(cs.opacity),
      ...(svg && effectiveOpacity(el) === 0 ? { invisible: true } : {}),
      overflow: cs.overflowX + " " + cs.overflowY,
      color: svg ? cs.color : undefined,
    });
    owners.push(el);
  }
  // 被祖先裁得一点不剩的元素不算绘制出来的（可见部分零面积）。
  const visible = await visibleRects(owners);
  const kept = [];
  out.forEach((p, i) => {
    const v = visible[i];
    if (v && !(v[2] > 0 && v[3] > 0)) return;
    // SVG 图标被祖先的 overflow 裁掉一部分（滚动区域的底边只留下按钮图标的一条薄片）：记下看得见的区域，
    // 比较端量图标墨迹时只取这一块，免得把裁剪线以外的页脚、边线当成图标的墨迹（同文字的 clip，见 TEXTS_EXPR）。
    if (v && p.tag === "svg") {
      const [x, y, w, h] = p.box;
      if (x < v[0] - 0.5 || y < v[1] - 0.5 || x + w > v[0] + v[2] + 0.5 || y + h > v[1] + v[3] + 0.5) p.clip = v;
    }
    kept.push(p);
  });
  return kept;
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
      awaitPromise: true,
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
    // 被祖先裁得一点不剩（滚出滚动容器等）的节点不收：口径见 VISIBLE_RECTS_SRC。零面积的外框不受影响。
    if (box && box[2] > 0 && box[3] > 0 && m.vis && !(m.vis[2] > 0 && m.vis[3] > 0)) return;
    // 被祖先裁掉一部分的节点：Viva 报告的外框是裁剪之后的，所以比较口径取可见部分；原外框留在 fullBox 里备查。
    const clipped = m?.k === "e" && box && m.vis && box[2] > 0 && box[3] > 0 &&
      (Math.abs(m.vis[0] - box[0]) > 0.01 || Math.abs(m.vis[1] - box[1]) > 0.01 ||
       Math.abs(m.vis[2] - box[2]) > 0.01 || Math.abs(m.vis[3] - box[3]) > 0.01);
    const visibleBox = clipped ? m.vis : box;
    indexMap.set(i, nodes.length);
    nodes.push({
      role: n.role,
      name: n.name,
      ...(n.description !== undefined ? { description: n.description } : {}),
      ...(n.value !== undefined ? { value: n.value } : {}),
      props: n.props,
      // 位置换算到区域坐标系；未被裁剪时尺寸不变
      box: visibleBox ? [r3(visibleBox[0] - region.x), r3(visibleBox[1] - region.y), visibleBox[2], visibleBox[3]] : null,
      ...(clipped ? { fullBox: [r3(box[0] - region.x), r3(box[1] - region.y), box[2], box[3]] } : {}),
      kind: m?.k ?? null,
      ...(m?.tag ? { tag: m.tag } : {}),
      ...(m?.testId ? { testId: m.testId } : {}),
      ...(m?.proxy ? { proxy: m.proxy } : {}),
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

  // 文字：保留与区域相交的行片段，坐标换算到区域坐标系；整段文字都在区域外的丢弃。
  const rel = (b) => [r3(b[0] - region.x), r3(b[1] - region.y), b[2], b[3]];
  const texts = (await cdp.send("Runtime.evaluate", { expression: TEXTS_EXPR, returnByValue: true, awaitPromise: true })).result.value
    .map((t) => ({ ...t, rects: t.rects.filter((b) => intersects(b, region)).map(rel), ...(t.clip ? { clip: rel(t.clip) } : {}) }))
    .filter((t) => t.rects.length > 0);
  const textRects = texts.flatMap((t) => t.rects);
  const paints = (await cdp.send("Runtime.evaluate", { expression: PAINTS_EXPR, returnByValue: true, awaitPromise: true })).result.value
    .filter((p) => intersects(p.box, region))
    .map((p) => ({ ...p, box: rel(p.box), ...(p.clip ? { clip: rel(p.clip) } : {}) }));
  const fonts = await platformFonts(cdp);
  const aria = await page.locator(regionSelector ?? "body").ariaSnapshot();
  const chromium = (await cdp.send("Browser.getVersion")).product;

  return {
    schema: 2,
    scene,
    variant,
    region: { x: region.x, y: region.y, width: region.width, height: region.height, selector: region.selector },
    capture: { dpr: 1, forcedDsf: true, chromium },
    nodes,
    texts,
    textRects,
    paints,
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
