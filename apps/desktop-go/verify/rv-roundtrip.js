// 主题阅读视图 iframe 在真实 UI 里的往返（-shotjs 脚本，夹具由 make-reading-fixture.mts 生成）。
// 页面脚本在 iframe 内部定时合成：2.5s 选区(mouseup) → 3.5s Ctrl+F → 4.5s Esc → 5.5s 点击图片。
// 本脚本：点击条目 → 等视图创建 → 订阅 readingView 事件 → 验证渲染进程对这些事件的真实反应（选区工具、查找栏、图片放大）。
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const until = async (fn, what, ms = 20000) => {
  const t0 = Date.now();
  for (;;) {
    const v = fn();
    if (v) return v;
    if (Date.now() - t0 > ms) throw new Error("等待超时：" + what);
    await sleep(50);
  }
};
const tid = (id) => document.querySelector(`[data-testid="${id}"]`);
const out = { steps: [], events: [] };
const step = (name, data) => out.steps.push({ name, ...(data === undefined ? {} : { data }) });
const t0 = performance.now();
const at = () => Math.round(performance.now() - t0);

await until(() => tid("topbar-search"), "顶栏搜索框");
await sleep(500);
const later = [...document.querySelectorAll("button")].find((b) => /稍后再说|maybe later/i.test(b.textContent || ""));
if (later) later.click();

// 订阅 Go 推回渲染进程的视图事件（与 NativeReadingView 自己的订阅并存）
const summarize = (v) => (v && typeof v === "object" ? JSON.parse(JSON.stringify(v, (k, x) => (typeof x === "string" && x.length > 60 ? x.slice(0, 60) + "…" : Array.isArray(x) && x.length > 5 ? x.slice(0, 5).concat(["…" + x.length]) : x))) : v);
// 记录渲染进程发给视图的命令（排查 layout 高度变化与哪条命令相关）
out.commands = [];
for (const name of ["commandView", "updateView"]) {
  const orig = window.api.themedReading[name].bind(window.api.themedReading);
  window.api.themedReading[name] = (...a) => { out.commands.push({ at: at(), name, arg: summarize(a.length > 1 ? a.slice(1) : a[0]) }); return orig(...a); };
}
window.api.themedReading.onViewEvent((e) => out.events.push({ at: at(), viewId: e.viewId, type: e.type, value: summarize(e.value) }));

// 1) 真实 UI：点击条目（脚本里按标题找叶子节点）
const title = "阅读视图往返验证";
const row = await until(() => [...document.querySelectorAll("*")].reverse().find((x) => x.children.length === 0 && (x.textContent || "").trim() === title), "列表里的条目");
row.click();
step("clicked-item", { at: at() });
const host = await until(() => tid("native-reading-view"), "原生阅读视图容器", 30000);
const viewHost = await until(() => document.querySelector("[data-reading-view-id]"), "视图 ID 写入 DOM", 30000);
const viewId = viewHost.dataset.readingViewId;
step("view-created", { at: at(), viewId });

// 2) iframe 是否存在、位置是否与宿主区域一致（layout 精度）
const frame = await until(() => document.querySelector('iframe[title="AI 阅读页"]'), "iframe", 10000);
await sleep(800);
const hb = viewHost.getBoundingClientRect(), fb = frame.getBoundingClientRect();
step("geometry", {
  dpr: window.devicePixelRatio, inner: [innerWidth, innerHeight],
  host: [hb.x, hb.y, hb.width, hb.height].map((n) => Math.round(n * 100) / 100),
  frame: [fb.x, fb.y, fb.width, fb.height].map((n) => Math.round(n * 100) / 100),
  sandbox: frame.getAttribute("sandbox"), display: getComputedStyle(frame).display,
});

// 3) 等 iframe 内合成的各类事件到达
const seen = (type) => out.events.some((e) => e.type === type);
await until(() => seen("ready"), "ready 事件", 15000);
step("event-ready", { at: at() });
await until(() => seen("layout"), "layout 事件", 15000);
await until(() => seen("selection"), "selection 事件（2.5s 合成）", 15000);
step("event-selection", { at: at(), selectionToolsShown: /Explain passage|Ask about passage|解释这段|询问这段/.test(document.body.innerText) });
await until(() => out.events.some((e) => e.type === "key" && e.value === "find"), "key=find（Ctrl+F，3.5s 合成）", 15000);
const findInput = await until(() => document.querySelector(".absolute.right-2.top-11 input"), "查找栏输入框（Ctrl+F 应打开）", 5000);
step("event-ctrlF-opened-findbar", { at: at() });

// 4) 在查找栏里输入关键词：渲染进程 → commandView(find) → iframe 内高亮 → find 事件带回命中数
const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value").set;
setter.call(findInput, "GZ-FIND");
findInput.dispatchEvent(new Event("input", { bubbles: true }));
await until(() => out.events.some((e) => e.type === "find" && e.value && e.value.count > 0), "find 事件带命中数", 10000);
const find = out.events.filter((e) => e.type === "find").pop();
step("find-count", { count: find.value.count, barText: (findInput.closest(".absolute")?.textContent || "").trim().slice(0, 40) });

await until(() => out.events.some((e) => e.type === "key" && e.value === "escape"), "key=escape（4.5s 合成）", 15000);
step("event-escape", { at: at() });

// 5) 图片点击 → 渲染进程图片放大
await until(() => seen("image"), "image 事件（5.5s 合成）", 15000);
const lightboxImg = await until(() => document.querySelector('img[src*="local-image"]'), "图片放大层", 5000).catch(() => null);
await sleep(800); // 给图片加载留时间
const imgLoaded = !!lightboxImg && lightboxImg.complete && lightboxImg.naturalWidth > 0;
step("event-image", { at: at(), lightboxOpened: !!lightboxImg, imageLoaded: imgLoaded, natural: lightboxImg ? [lightboxImg.naturalWidth, lightboxImg.naturalHeight] : null, src: lightboxImg && lightboxImg.currentSrc });

// 5b) 遮挡恢复：放大层（role=dialog）与选区工具关闭后，视图应重新可见（原生视图盖住 DOM 时的既有设计）
const hiddenBefore = getComputedStyle(frame).display === "none";
const cmdMark = out.commands.length;
// 放大层：点它自己的关闭按钮（yet-another-react-lightbox）
const lbButtons = [...document.querySelectorAll('.yarl__root button, .yarl__portal button, [class*="yarl"] button')].map((b) => ({ label: b.getAttribute("aria-label") || b.title || b.className.slice(0, 40) }));
const closeBtn = [...document.querySelectorAll('[class*="yarl"] button')].find((b) => /close|关闭/i.test((b.getAttribute("aria-label") || "") + b.title + b.className));
step("lightbox-buttons", lbButtons);
closeBtn?.click();
await sleep(600);
const lightboxGone = !document.querySelector('img[src*="local-image"]');
// 选区工具：读者根节点收到 mouseup 且窗口选区已折叠时收起（点在已隐藏视图的 DOM 垫底上等价于此）
getSelection()?.removeAllRanges();
viewHost.dispatchEvent(new MouseEvent("mouseup", { bubbles: true }));
await sleep(1500);
const layoutMark = out.events.length;
const toolsStill = /Explain passage|Ask about passage|解释这段|询问这段/.test(document.body.innerText);
const lastUpdate = out.commands.slice(cmdMark).filter((c) => c.name === "updateView").pop();
step("covered-restore", {
  hiddenBefore, lightboxGone, selectionToolsStillShown: toolsStill,
  updateAfterClose: lastUpdate ? lastUpdate.arg.bounds : null,
  frameDisplayNow: getComputedStyle(frame).display,
});

// 6) 心跳与故障：期间不应有 fault
out.faults = out.events.filter((e) => e.type === "fault");
step("faults", out.faults);
out.eventTypes = [...new Set(out.events.map((e) => e.type))];
out.ok = ["ready", "layout", "selection", "find", "image"].every(seen) && out.events.some((e) => e.type === "key" && e.value === "find") && out.events.some((e) => e.type === "key" && e.value === "escape") && out.faults.length === 0 && imgLoaded && out.steps.find((s) => s.name === "event-selection").data.selectionToolsShown;
out.eventCounts = out.events.reduce((m, e) => ((m[e.type] = (m[e.type] || 0) + 1), m), {});
out.events = out.events.filter((e) => e.type !== "layout"); // layout 事件很多，只留计数
return out;
