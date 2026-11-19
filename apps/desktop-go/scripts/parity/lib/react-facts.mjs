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
 *
 * 遮挡（场景选用）：页面里可以放 window.__parityOccluders（元素数组，场景在页内设置，重载即清）。
 * 吸附表头（sticky thead）盖在行上面，渲染层的节点与文字照旧在 DOM 里，IntersectionObserver 不认「盖住」；
 * Viva 的套件没有 z-index，只能把行区域裁在表头下沿（台账 A-027），被盖住的部分不出节点与文字。
 * 这里把遮挡元素盖住的上缘一段从可见部分里扣掉（只处理整宽盖住可见区上缘的情形，吸附表头就是这种），两边才是同一批「呈现出来」的内容。
 */
const VISIBLE_RECTS_SRC = `// 自 start（含）起沿祖先链，会裁剪内容的块（overflow 非 visible）的内边距盒（扣掉滚动条）与视口的交集。
  const clipRegionOf = (start) => {
    let left = 0;
    let top = 0;
    let right = window.innerWidth;
    let bottom = window.innerHeight;
    for (let a = start; a && a !== document.documentElement; a = a.parentElement) {
      const acs = getComputedStyle(a);
      if ((acs.overflowX === "visible" && acs.overflowY === "visible") || acs.display === "inline") continue;
      const ar = a.getBoundingClientRect();
      const al = ar.left + (parseFloat(acs.borderLeftWidth) || 0);
      const at = ar.top + (parseFloat(acs.borderTopWidth) || 0);
      left = Math.max(left, al);
      top = Math.max(top, at);
      right = Math.min(right, al + a.clientWidth);
      bottom = Math.min(bottom, at + a.clientHeight);
    }
    return { left, top, right, bottom };
  };
  const visibleRects = (targets) => new Promise((resolve) => {
  const out = new Array(targets.length).fill(null);
  const occlude = (target, r) => {
    const occluders = window.__parityOccluders;
    if (!occluders || occluders.length === 0 || r[2] <= 0 || r[3] <= 0) return r;
    let [x, y, w, h] = r;
    for (const o of occluders) {
      if (!o.isConnected || o.contains(target)) continue;
      // 遮挡元素自己看得见的部分：滚出视口的那一截（横向滚动后表头的左段在滚动容器之外）不盖住任何东西。
      const ob = o.getBoundingClientRect();
      const oc = clipRegionOf(o.parentElement);
      const b = { left: Math.max(ob.left, oc.left), top: Math.max(ob.top, oc.top), right: Math.min(ob.right, oc.right), bottom: Math.min(ob.bottom, oc.bottom) };
      if (b.left <= x + 0.5 && b.right >= x + w - 0.5 && b.top <= y + 0.5 && b.bottom > y) {
        const cut = Math.min(b.bottom - y, h);
        y += cut;
        h -= cut;
        if (h <= 0.001) return [Math.round(x * 1000) / 1000, Math.round(y * 1000) / 1000, 0, 0];
      }
    }
    return [Math.round(x * 1000) / 1000, Math.round(y * 1000) / 1000, Math.round(w * 1000) / 1000, Math.round(h * 1000) / 1000];
  };
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
      for (const i of index.get(e.target) ?? []) out[i] = occlude(e.target, [Math.round(r.x * 1000) / 1000, Math.round(r.y * 1000) / 1000, Math.round(r.width * 1000) / 1000, Math.round(r.height * 1000) / 1000]);
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
      // aria-current 不在 CDP 的 AX 属性表里（getFullAXTree 看不到），页码按钮的「当前页」靠它表达：顺带读 DOM 属性。
      ...(n.getAttribute("aria-current") ? { current: n.getAttribute("aria-current") } : {}),
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
  // -webkit-line-clamp 截断（line-clamp-N）：DOM 文本仍是全文，Chromium 只留前 N 行，最后一行重排成「放得下的前缀 + …」，
  // Range 给出的矩形里混着重排前后的片段与被隐藏的行，不能直接用。同样采集「看得见的文字」：在原位置旁放一份不截断的副本
  // （同宽、同样式，只去掉截断），按字符矩形的纵坐标分行，取前 N 行；最后一行取最长前缀，使「前缀宽度 + 省略号宽度」不超过内容盒宽度
  // （与 ellipsize 同一条规则）。只处理文字节点直接位于截断元素之下的写法；其余写法不复算，对照时文字不一致会直接报出来。
  const clampLines = (node, el, cs, rects) => {
    const n = parseInt(cs.webkitLineClamp, 10);
    if (!(n > 0) || cs.overflowY === "visible" || el.scrollHeight <= el.clientHeight + 0.5) return null;
    if (node.parentElement !== el) return null;
    const index = [...el.childNodes].indexOf(node);
    const clone = el.cloneNode(true);
    clone.removeAttribute("id");
    clone.removeAttribute("data-testid");
    const box = el.getBoundingClientRect();
    Object.assign(clone.style, {
      webkitLineClamp: "unset", display: "block", overflow: "visible", position: "absolute", visibility: "hidden",
      pointerEvents: "none", height: "auto", maxHeight: "none", boxSizing: "border-box", width: box.width + "px",
    });
    el.parentElement.insertBefore(clone, el);
    try {
      const text = clone.childNodes[index];
      const probe = document.createRange();
      const data = text.data;
      const charRect = (k) => {
        probe.setStart(text, k);
        probe.setEnd(text, k + 1);
        const r = probe.getBoundingClientRect();
        return r.height > 0 ? r : null;
      };
      // 行首下标：纵坐标变了就是新的一行。
      const starts = [];
      let lastY = null;
      for (let k = 0; k < data.length; k++) {
        const r = charRect(k);
        if (!r) continue;
        if (lastY === null || r.y > lastY + 1) { starts.push(k); lastY = r.y; }
      }
      if (starts.length <= n) return null;
      const lineRect = (from, to) => {
        probe.setStart(text, from);
        probe.setEnd(text, to);
        const bs = [...probe.getClientRects()].filter((b) => b.width > 0 && b.height > 0);
        const x = Math.min(...bs.map((b) => b.x));
        const right = Math.max(...bs.map((b) => b.x + b.width));
        return { x, y: bs[0].y, width: right - x, height: bs[0].height };
      };
      const first = lineRect(0, starts[1]);
      const dx = rects[0][0] - first.x;
      const dy = rects[0][1] - first.y;
      const out = [];
      for (let i = 0; i < n - 1; i++) {
        const b = lineRect(starts[i], starts[i + 1]);
        out.push([round(b.x + dx), round(b.y + dy), round(b.width), round(b.height)]);
      }
      measure.font = [cs.fontStyle, cs.fontWeight, cs.fontSize, cs.fontFamily].join(" ");
      const ellipsisWidth = measure.measureText("…").width;
      const px = (name) => parseFloat(cs.getPropertyValue(name)) || 0;
      const avail = clone.clientWidth - px("padding-left") - px("padding-right");
      const from = starts[n - 1];
      const widthOf = (k) => {
        probe.setStart(text, from);
        probe.setEnd(text, k);
        return probe.getBoundingClientRect().width;
      };
      let lo = from;
      let hi = starts[n];
      while (lo < hi) {
        const mid = (lo + hi + 1) >> 1;
        if (widthOf(mid) + ellipsisWidth <= avail + 1e-6) lo = mid;
        else hi = mid - 1;
      }
      const code = data.charCodeAt(lo - 1);
      if (lo > from && code >= 0xd800 && code <= 0xdbff) lo -= 1; // 不把代理对劈开
      const last = lineRect(from, Math.max(lo, from + 1));
      out.push([round(last.x + dx), round(last.y + dy), round(widthOf(lo) + ellipsisWidth), round(last.height)]);
      return { shown: data.slice(0, lo) + "…", rects: out };
    } finally {
      clone.remove();
    }
  };
  // text-overflow: ellipsis 截断，而宿主里不止一个文字节点（搜索命中词：文字、<mark>、文字……；台账 B-041）：
  // Chromium 按整行内容取最长的前缀，使「前缀宽度 + 省略号宽度」放得下；命中词（行内盒）整段收在前缀里时，它右侧的内边距也算进前缀宽度。
  // 省略号接在被截掉的第一个字符所在的那段里：截断点在某个节点之内就是「该节点的前缀 + …」；恰好落在节点之间就是后一个节点只剩「…」。
  // 一个宿主只算一次：截断点之前的节点原样，截断点所在的节点只留前缀加省略号，之后的节点没有可见部分。
  // 只处理单行（white-space: nowrap 的 truncate 类）；宿主里只有一个文字节点的写法走上面的 ellipsize，这里返回 undefined。
  const lineCuts = new Map();
  const ellipsisHostOf = (el) => {
    for (let e = el; e && e !== document.body; e = e.parentElement) {
      const cs = getComputedStyle(e);
      if (cs.textOverflow === "ellipsis" && cs.overflowX !== "visible") return e;
      if (cs.display !== "inline") return null;
    }
    return null;
  };
  const lineCutOf = (host) => {
    if (lineCuts.has(host)) return lineCuts.get(host);
    let result;
    const cs = getComputedStyle(host);
    const texts = [];
    const tw = document.createTreeWalker(host, NodeFilter.SHOW_TEXT);
    for (let t; (t = tw.nextNode()); ) if (t.data.length > 0) texts.push(t);
    if (texts.length > 1 && host.scrollWidth > host.clientWidth) {
      const px = (style, name) => parseFloat(style.getPropertyValue(name)) || 0;
      const hb = host.getBoundingClientRect();
      const left = hb.x + px(cs, "border-left-width") + px(cs, "padding-left");
      const avail = host.clientWidth - px(cs, "padding-left") - px(cs, "padding-right");
      measure.font = [cs.fontStyle, cs.fontWeight, cs.fontSize, cs.fontFamily].join(" ");
      const ell = measure.measureText("…").width;
      const probe = document.createRange();
      const total = texts.reduce((n, t) => n + t.data.length, 0);
      const locate = (k) => {
        for (let i = 0; i < texts.length; i++) {
          if (k <= texts[i].data.length) return [i, k];
          k -= texts[i].data.length;
        }
        return [texts.length - 1, texts[texts.length - 1].data.length];
      };
      const prefixWidth = (k) => {
        if (k === 0) return 0;
        const [i, off] = locate(k);
        probe.setStart(texts[0], 0);
        probe.setEnd(texts[i], off);
        let w = probe.getBoundingClientRect().right - left;
        if (off === texts[i].data.length) {
          for (let n = texts[i], e = n.parentElement; e && e !== host && e.lastChild === n; n = e, e = e.parentElement) {
            const ecs = getComputedStyle(e);
            w += px(ecs, "padding-right") + px(ecs, "border-right-width");
          }
        }
        return w;
      };
      if (prefixWidth(total) > avail + 1e-6) {
        let lo = 0;
        let hi = total;
        while (lo < hi) {
          const mid = (lo + hi + 1) >> 1;
          if (prefixWidth(mid) + ell <= avail + 1e-6) lo = mid;
          else hi = mid - 1;
        }
        let [i, off] = locate(lo);
        if (off > 0) {
          const code = texts[i].data.charCodeAt(off - 1);
          if (code >= 0xd800 && code <= 0xdbff) off -= 1; // 不把代理对劈开
        }
        if (off === texts[i].data.length && i < texts.length - 1) { i += 1; off = 0; }
        result = { nodes: texts, index: i, offset: off, ell };
      }
    }
    lineCuts.set(host, result);
    return result;
  };
  // -webkit-line-clamp 截断，而宿主里不止一个文字节点（表格的标题按钮：文字、<mark>、文字……；台账 B-041）。
  // 做法同 clampLines：在原位置旁放一份不截断的副本，按字符矩形的纵坐标分行，取前 N 行；最后一行取最长前缀，
  // 使「前缀宽度（命中词整段收在前缀里时含它右侧的内边距）+ 省略号宽度」不超过内容盒宽度，省略号接在被截掉的第一个字符所在的那段里。
  // 结果记截断点所在的节点与偏移，以及这个节点在各个可见行上的矩形；截断点之前的节点原样，之后的节点没有可见部分。
  const clampCuts = new Map();
  const clampHostOf = (el) => {
    for (let e = el; e && e !== document.body; e = e.parentElement) {
      const cs = getComputedStyle(e);
      if (parseInt(cs.webkitLineClamp, 10) > 0 && cs.overflowY !== "visible") return e;
      if (cs.display !== "inline") return null;
    }
    return null;
  };
  const textNodesOf = (root) => {
    const list = [];
    const tw = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    for (let t; (t = tw.nextNode()); ) if (t.data.length > 0) list.push(t);
    return list;
  };
  const clampCutOf = (host) => {
    if (clampCuts.has(host)) return clampCuts.get(host);
    let result;
    const cs = getComputedStyle(host);
    const n = parseInt(cs.webkitLineClamp, 10);
    const live = textNodesOf(host);
    if (live.length > 1 && host.scrollHeight > host.clientHeight + 0.5) {
      const clone = host.cloneNode(true);
      clone.removeAttribute("id");
      clone.removeAttribute("data-testid");
      const box = host.getBoundingClientRect();
      Object.assign(clone.style, {
        webkitLineClamp: "unset", display: "block", overflow: "visible", position: "absolute", visibility: "hidden",
        pointerEvents: "none", height: "auto", maxHeight: "none", boxSizing: "border-box", width: box.width + "px",
      });
      host.parentElement.insertBefore(clone, host);
      try {
        result = (() => {
          const texts = textNodesOf(clone);
          const px = (style, name) => parseFloat(style.getPropertyValue(name)) || 0;
          const probe = document.createRange();
          const offsets = []; // 各节点第一个字符的全局下标
          let total = 0;
          for (const t of texts) { offsets.push(total); total += t.data.length; }
          // 字符 k 所在的 [节点, 偏移]；前缀 [0,k) 的结尾落在节点边界上时算前一个节点的末尾。
          const locate = (k) => {
            let i = texts.length - 1;
            while (i > 0 && offsets[i] > k) i--;
            return [i, k - offsets[i]];
          };
          const locateEnd = (k) => {
            let i = texts.length - 1;
            while (i > 0 && offsets[i] >= k) i--;
            return [i, k - offsets[i]];
          };
          const charRect = (k) => {
            const [i, off] = locate(k);
            probe.setStart(texts[i], off);
            probe.setEnd(texts[i], off + 1);
            const r = probe.getBoundingClientRect();
            return r.height > 0 ? r : null;
          };
          // 行首下标：纵坐标变了就是新的一行。
          const starts = [];
          let lastY = null;
          for (let k = 0; k < total; k++) {
            const r = charRect(k);
            if (!r) continue;
            if (lastY === null || r.y > lastY + 1) { starts.push(k); lastY = r.y; }
          }
          if (starts.length <= n) return undefined;
          const from = starts[n - 1];
          const ccs = getComputedStyle(clone);
          const avail = clone.clientWidth - px(ccs, "padding-left") - px(ccs, "padding-right");
          measure.font = [cs.fontStyle, cs.fontWeight, cs.fontSize, cs.fontFamily].join(" ");
          const ell = measure.measureText("…").width;
          // 前缀收在命中词（行内盒）末尾时，它右侧的内边距也算进宽度。
          const pads = (i, off) => {
            let w = 0;
            if (off === texts[i].data.length) {
              for (let m = texts[i], e = m.parentElement; e && e !== clone && e.lastChild === m; m = e, e = e.parentElement) {
                const ecs = getComputedStyle(e);
                w += px(ecs, "padding-right") + px(ecs, "border-right-width");
              }
            }
            return w;
          };
          const prefixRange = (k) => {
            const [ai, aoff] = locate(from);
            const [bi, boff] = locateEnd(k);
            probe.setStart(texts[ai], aoff);
            probe.setEnd(texts[bi], boff);
            return [probe.getBoundingClientRect(), pads(bi, boff)];
          };
          const prefixWidth = (k) => {
            if (k <= from) return 0;
            const [r, pad] = prefixRange(k);
            return r.width + pad;
          };
          let lo = from;
          let hi = starts[n];
          while (lo < hi) {
            const mid = (lo + hi + 1) >> 1;
            if (prefixWidth(mid) + ell <= avail + 1e-6) lo = mid;
            else hi = mid - 1;
          }
          let [i, off] = locateEnd(lo);
          if (off > 0) {
            const code = texts[i].data.charCodeAt(off - 1);
            if (code >= 0xd800 && code <= 0xdbff) off -= 1; // 不把代理对劈开
          }
          if (off === texts[i].data.length && i < texts.length - 1) { i += 1; off = 0; }
          // 副本与原件的位置差：用第一个字符对齐（它一定在第一行，永远看得见）。
          probe.setStart(live[0], 0);
          probe.setEnd(live[0], 1);
          const l0 = probe.getBoundingClientRect();
          const c0 = charRect(0);
          const dx = l0.x - c0.x;
          const dy = l0.y - c0.y;
          // 截断点所在的节点在各个可见行上的片段 [x, y, 宽, 高]（副本坐标）。
          const s = offsets[i];
          const e = s + off;
          const pieces = [];
          for (let li = 0; li < n; li++) {
            const a = Math.max(starts[li], s);
            const b = Math.min(li < n - 1 ? starts[li + 1] : lo, e);
            if (b <= a) continue;
            probe.setStart(texts[i], a - s);
            probe.setEnd(texts[i], b - s);
            const bs = [...probe.getClientRects()].filter((r) => r.width > 0 && r.height > 0);
            const x = Math.min(...bs.map((r) => r.x));
            const right = Math.max(...bs.map((r) => r.x + r.width));
            pieces.push({ x, y: bs[0].y, w: right - x, h: bs[0].height, last: li === n - 1 });
          }
          if (pieces.length === 0) {
            // 截断点恰在节点之间（或最后一行连一个字符也放不下）：这个节点只剩「…」，接在前一段内容的右边。
            if (lo > from) {
              const [r, pad] = prefixRange(lo);
              pieces.push({ x: r.right + pad, y: r.y, w: 0, h: r.height, last: true });
            } else {
              const r = charRect(from);
              pieces.push({ x: r.x, y: r.y, w: 0, h: r.height, last: true });
            }
          }
          const rects = pieces.map((p) => [round(p.x + dx), round(p.y + dy), round(p.last ? p.w + ell : p.w), round(p.h)]);
          return { nodes: live, index: i, offset: off, rects };
        })();
      } finally {
        clone.remove();
      }
    }
    clampCuts.set(host, result);
    return result;
  };
  // 返回 undefined：这个节点不属于「多节点的截断宿主」，沿用 ellipsize / clampLines；"hidden"：被截掉，没有可见部分；
  // null：在截断点之前、完整可见；对象：截断点所在的节点，{ shown, rect } 或 { shown, rects }。
  const multiNodeCut = (node, el, rects) => {
    const clampHost = clampHostOf(el);
    if (clampHost) {
      const cut = clampCutOf(clampHost);
      if (!cut) return undefined;
      const at = cut.nodes.indexOf(node);
      if (at < 0 || at < cut.index) return null;
      if (at > cut.index) return "hidden";
      return { shown: node.data.slice(0, cut.offset) + "…", rects: cut.rects };
    }
    const host = ellipsisHostOf(el);
    if (!host) return undefined;
    const cut = lineCutOf(host);
    if (!cut) return undefined;
    const at = cut.nodes.indexOf(node);
    if (at < 0 || at < cut.index) return null;
    if (at > cut.index) return "hidden";
    const probe = document.createRange();
    probe.setStart(node, 0);
    probe.setEnd(node, cut.offset);
    const inside = cut.offset > 0 ? probe.getBoundingClientRect().width : 0;
    const [x, y, , h] = rects[0];
    return { shown: node.data.slice(0, cut.offset) + "…", rect: [x, y, round(inside + cut.ell), h] };
  };
  // 文字实际被裁掉的右缘：自身与祖先里会裁剪的块的内边距盒右缘（扣掉滚动条），再加视口右缘。
  // 不能拿所属元素自己的外框当裁剪线：文字溢出自己的盒子、但没有祖先裁它时（侧栏图标位里 19px 宽的表情），整个字形都画得出来。
  const clipRightOf = (el) => clipRegionOf(el).right;
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
    const multi = multiNodeCut(node, el, rects);
    if (multi === "hidden") continue;
    const cut = multi === undefined ? ellipsize(node, el, cs, rects) ?? clampLines(node, el, cs, rects) : multi;
    out.push({
      // 被省略号截断时，text 是看得见的「前缀 + …」，full 才是 DOM 原文；rects 取截断后的宽度。
      text: cut ? cut.shown : node.data,
      ...(cut ? { full: node.data } : {}),
      rects: cut ? (cut.rects ?? [cut.rect]) : rects,
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
      if (cut) {
        t.clip = v;
        // 右半段被祖先裁掉的文字只记看得见的宽度：Viva 一侧的绘制宽度同样按裁剪矩形的右缘截断（viva/parity/render.go），两边口径一致。
        // 只认真正的裁剪（clipRightOf）：文字只是溢出所属元素的盒子、没有祖先裁它时，字形整个都画出来，宽度照记。
        const right = clipRightOf(owners[i]);
        t.rects = t.rects.map(([x, y, w, h]) => (x + w > right + 0.5 ? [x, y, round(Math.max(right - x, 0)), h] : [x, y, w, h]));
      }
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
      props: m?.current ? { ...n.props, current: m.current } : n.props,
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
