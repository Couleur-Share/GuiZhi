/** 静态正文判断：只凭静态 HTML 的正面证据接受正文，不把字数相近当成完整性。 */
import { parseHTML } from "linkedom";

// 某些错误页和短文只返回 HTML 片段；补齐文档外壳，避免丢失兄弟节点。
function parsePage(html: string) {
  return parseHTML(
    /<html(?:\s|>)/i.test(html) ? html : `<html><body>${html}</body></html>`,
  );
}

export interface StaticDecision {
  route: "static" | "render" | "terminal";
  reasons: string[];
  textChars: number;
  semanticRegions: number;
  hasScripts: boolean;
}

export function assessStaticPage(html: string, status: number): StaticDecision {
  const { document } = parsePage(html);
  const regions = Array.from(
    document.querySelectorAll('main, article, [role="main"]'),
  );
  const root = regions.length === 1 ? regions[0] : document.body;
  const executable = Array.from(document.querySelectorAll("script")).some(
    (node) => {
      const type = (node.getAttribute("type") ?? "").toLowerCase();
      return !type || type === "module" || /(?:java|ecma)script/.test(type);
    },
  );
  const hasScripts =
    executable || !!document.querySelector("[onload], [onclick], [onerror]");
  const clean = root.cloneNode(true) as unknown as Element;
  clean
    .querySelectorAll(
      "script, style, noscript, template, nav, header, footer, form",
    )
    .forEach((node) => node.remove());
  const textChars = (clean.textContent ?? "")
    .replace(/\s+/g, " ")
    .trim().length;
  const stats = { textChars, semanticRegions: regions.length, hasScripts };
  const title = document.querySelector("title")?.textContent?.trim() ?? "";
  if (
    status >= 400 ||
    /^(just a moment|verify you are human|人机验证|安全验证)/i.test(title) ||
    (!!document.querySelector('input[type="password"]') &&
      /登录|登入|sign in|log in|login/i.test(title))
  )
    return {
      ...stats,
      route: "terminal",
      reasons: ["HTTP 或登录验证错误保持原结果"],
    };

  const reasons: string[] = [];
  if (!textChars) reasons.push("静态页面无正文");
  if (document.querySelector('meta[http-equiv="refresh" i]'))
    reasons.push("存在延迟跳转");
  for (
    let node: Element | null = root as unknown as Element;
    node;
    node = node.parentElement
  ) {
    if (
      node.matches('[hidden], [aria-hidden="true"]') ||
      /display\s*:\s*none|visibility\s*:\s*hidden/i.test(
        node.getAttribute("style") ?? "",
      )
    ) {
      reasons.push("正文区域初始隐藏");
      break;
    }
  }
  const pendingSelector =
    '[aria-busy="true"], [data-loading="true"], [data-load-more], template[shadowrootmode]';
  if (root.matches(pendingSelector) || root.querySelector(pendingSelector))
    reasons.push("存在未加载内容标记");
  const loading = Array.from(
    clean.querySelectorAll('p, div, span, [role="status"]'),
  ).some((node) => {
    if (node.closest("pre, code")) return false;
    return /^(loading(?:\s+(?:content|article|data))?[.…!\s]*|正在加载[.…!\s]*|加载中[.…!\s]*|please enable javascript[.!\s]*)$/i.test(
      node.textContent?.trim() ?? "",
    );
  });
  if (loading) reasons.push("正文含加载占位");
  if (
    Array.from(root.querySelectorAll("*")).some((node) =>
      node.tagName.includes("-"),
    )
  )
    reasons.push("正文含自定义组件");
  if (
    Array.from(root.querySelectorAll("pre a[href]")).some(
      (node) =>
        !(node.textContent ?? "").trim() &&
        !node.querySelector("img, svg") &&
        (node.getAttribute("href") ?? "").includes("#"),
    )
  )
    reasons.push("代码含空锚点，需渲染后核对");
  if (hasScripts && regions.length !== 1)
    reasons.push("有脚本但正文区域不唯一");
  if (hasScripts && textChars < 400) reasons.push("有脚本且静态正文证据不足");
  return {
    ...stats,
    route: reasons.length ? "render" : "static",
    reasons: reasons.length
      ? reasons
      : [
          hasScripts
            ? "唯一正文区域且未发现动态占位"
            : "无可执行脚本的静态内容",
        ],
  };
}

export function staticPageLinks(html: string, url: string): string[] {
  const { document } = parsePage(html);
  return Array.from(document.querySelectorAll("a[href]"))
    .slice(0, 2000)
    .flatMap((node) => {
      try {
        const target = new URL(node.getAttribute("href") ?? "", url);
        return ["http:", "https:"].includes(target.protocol)
          ? [target.href]
          : [];
      } catch {
        return [];
      }
    });
}
