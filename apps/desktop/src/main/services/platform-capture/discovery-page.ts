import type { ElectronCapturePage } from "./electron-capture-runtime";

/** 在平台页面上下文执行；只读取页面自身的数据，不调用私有接口。 */
export function readDiscoveryPage(options?: { searchOnly?: boolean }) {
  const anchors = options?.searchOnly ? [] : Array.from(document.querySelectorAll<HTMLAnchorElement>("a[href]"));
  const cards = anchors.slice(0, 800).map((anchor) => {
    const image = anchor.querySelector<HTMLImageElement>("img");
    const card = anchor.closest<HTMLElement>("article, li, [class*='card'], [class*='item']") ?? anchor.parentElement;
    const author = card?.querySelector<HTMLElement>("[class*='author'], [class*='user'], [class*='name']");
    const time = card?.querySelector<HTMLTimeElement>("time");
    const timestamp = time?.dateTime ? Date.parse(time.dateTime) : Number.NaN;
    return {
      href: anchor.href,
      title: anchor.getAttribute("title") || anchor.textContent || image?.alt || "",
      coverUrl: image?.currentSrc || image?.src || "",
      author: author?.textContent || "",
      publishedAt: Number.isFinite(timestamp) ? timestamp : undefined,
      hasVideo: Boolean(card?.querySelector("video")),
    };
  });
  const payloads: unknown[] = [];
  // SSR 初始数据不一定再次经 XHR 返回；它与后续响应共用同一套卡片解析。
  const globals = window as unknown as Record<string, unknown>;
  for (const key of options?.searchOnly ? [] : ["__INITIAL_STATE__", "_ROUTER_DATA"]) {
    try {
      const json = JSON.stringify(globals[key]);
      if (json && json.length <= 5_000_000) payloads.push(JSON.parse(json));
    } catch { /* 循环引用或暂未初始化，下一轮继续读取。 */ }
  }
  for (const id of options?.searchOnly ? [] : ["RENDER_DATA", "__NEXT_DATA__"]) {
    const text = document.getElementById(id)?.textContent;
    if (!text || text.length > 5_000_000) continue;
    try { payloads.push(JSON.parse(text.startsWith("%") ? decodeURIComponent(text) : text)); }
    catch { /* 尚未加载完整或不是 JSON。 */ }
  }
  const visible = (node: Element) => {
    const rect = node.getBoundingClientRect();
    const style = getComputedStyle(node);
    return rect.width > 0 && rect.height > 0 && style.display !== "none" && style.visibility !== "hidden";
  };
  // 只认独立可见提示，不能把正文中提到“没有结果”的作品误当成平台空态。
  const hints = Array.from(document.querySelectorAll("p, span, [class*='empty'], [role='alert']"))
    .filter(visible).map((node) => node.textContent?.trim() ?? "");
  return {
    cards,
    payloads,
    loginRequired: Array.from(document.querySelectorAll(".login-container, [class*='login-modal'], #login-panel-new, #login-panel")).some(visible),
    empty: hints.some((text) => /^(?:暂无(?:搜索)?结果|没有找到相关(?:内容|结果|作品)|未找到相关(?:内容|结果|作品)|没有搜索到相关(?:内容|结果)|没有更多了)[。！!]?$/u.test(text)),
    // 中间页正文可以全空，跨域验证码也读不到文本，必须识别页面本身。
    verification: document.title.trim() === "验证码中间页" || Array.from(document.querySelectorAll<HTMLIFrameElement>("iframe[src]")).some((frame) => {
      try { const url = new URL(frame.src); return url.protocol === "https:" && url.hostname === "rmc.bytedance.com" && url.pathname.startsWith("/verifycenter/captcha/") && visible(frame); }
      catch { return false; }
    }) || hints.some((text) => /^(?:请完成安全验证|请先完成验证|请完成下方验证|拖动滑块完成拼图|访问过于频繁)[。！!]?$/u.test(text)),
  };
}

/** 经官方搜索框触发站内路由；直接打开小红书搜索深链可能被重定向到 HTTP 首页。 */
export function submitXhsSearch(keyword: string): "submitted" | "loading" | "login_required" {
  const input = document.querySelector<HTMLInputElement>("#search-input");
  if (!input) return "loading";
  if (/登录/.test(input.placeholder)) return "login_required";
  const button = input.parentElement?.querySelector<HTMLElement>(".search-icon");
  if (!button) return "loading";
  const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
  if (!setter) return "loading";
  setter.call(input, keyword);
  input.dispatchEvent(new Event("input", { bubbles: true }));
  input.dispatchEvent(new Event("change", { bubbles: true }));
  button.click();
  return "submitted";
}

export async function waitForXhsSearch(page: ElectronCapturePage, keyword: string, checkCanceled: () => void) {
  const started = Date.now();
  let loginSince: number | null = null;
  while (Date.now() - started < 15_000) {
    checkCanceled();
    const state = await page.evaluate(submitXhsSearch, keyword);
    if (state === "submitted") return state;
    // 首页先渲染访客占位，再异步恢复账号；不能在第一帧判定用户登录失效。
    if (state === "login_required") {
      loginSince ??= Date.now();
      if (Date.now() - loginSince >= 2500) return state;
    } else loginSince = null;
    await page.waitForTimeout(500);
  }
  return "unavailable" as const;
}
