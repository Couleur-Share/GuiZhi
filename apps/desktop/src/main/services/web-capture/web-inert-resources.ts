/**
 * 渲染窗口里的“惰性图片”：只决定页面算不算加载完，却不会进入采集结果的子请求。
 * Inert images in the render window: they only decide when the page counts as loaded.
 *
 * 采集结果来自渲染后主框架的 DOM → 抽取 → Markdown，图片在其中只以网址出现；
 * 图片字节由导入流程按网址另行下载，从不取自渲染窗口，窗口里加载的那一份没有任何下游使用。
 * 它们却会：拖住 load 事件（DOMContentLoaded 到 load 的间隔约占一次渲染采集的两成）、
 * 占用全局网络槽位（并发采集时挤占别的页面）、消耗页面的请求数与流量预算。
 *
 * 做法：Accept 以 image/ 开头的 GET 子请求（img、picture、CSS 背景图、图标）不出网，
 * 直接以一张有效的 1×1 透明 GIF 应答。不用取消或报错：onerror 处理器常把 src 改写成占位图，
 * 会污染 DOM 里的图片地址；返回有效图片则 onload 照常触发，src 保持作者写的原样。
 * 字体、媒体、脚本、XHR 在协议处理器里无法可靠区分（见 web-network-activity.ts），不在此列。
 *
 * 已知限制：页面脚本若按 naturalWidth 判断图片是否“真实”（例如宽度大于 1 才显示正文），
 * 会读到 1，行为与真实加载不同；这类写法罕见，且只影响渲染窗口里的显隐。
 * GUIZHI_WEB_INERT_RESOURCES=load 恢复真实加载。
 */
export const WEB_INERT_ENV = "GUIZHI_WEB_INERT_RESOURCES";

type Env = Record<string, string | undefined>;

/** 经典的 43 字节 GIF89a：1×1、透明。 */
const TRANSPARENT_GIF = Buffer.from(
  "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7",
  "base64",
);

export function inertResourceMode(env: Env = process.env): "stub" | "load" {
  return env[WEB_INERT_ENV] === "load" ? "load" : "stub";
}

/** 该请求是否应以占位图本地应答：只认 GET，且 Accept 属于图片类（见 classifyAccept）。 */
export function isInertImageRequest(method: string, kind: string): boolean {
  return method === "GET" && kind === "image";
}

/**
 * 占位图应答。带 Origin 的请求（<img crossorigin>、跨域图片检查）需要 CORS 头，
 * 开启 COEP 的页面还要求 CORP 头，缺了会被浏览器当作加载失败而触发 onerror。
 */
export function inertImageResponse(requestHeaders: Headers): Response {
  const headers = new Headers({
    "content-type": "image/gif",
    "content-length": String(TRANSPARENT_GIF.length),
    "cache-control": "no-store",
    "cross-origin-resource-policy": "cross-origin",
  });
  const origin = requestHeaders.get("origin");
  if (origin) {
    headers.set("access-control-allow-origin", origin);
    headers.set("access-control-allow-credentials", "true");
    headers.set("vary", "Origin");
  }
  return new Response(new Uint8Array(TRANSPARENT_GIF), { status: 200, headers });
}
