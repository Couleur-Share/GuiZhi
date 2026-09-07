import { parseHTML } from "linkedom";
import TurndownService from "turndown";
import { createHash } from "node:crypto";
import type { WebCaptureResult, WebSnapshot } from "@guizhi/shared/types";
import { fetchHtml } from "../import/safe-fetch";
import { cleanCss, cleanHtml, snapshotHash } from "./snapshot-sanitize";
import {
  collectSnapshotAssets,
  releaseSnapshotAssets,
} from "./snapshot-assets";

export function isWechatUrl(url: string): boolean {
  try {
    const u = new URL(url);
    return (
      ["https:", "http:"].includes(u.protocol) &&
      u.hostname === "mp.weixin.qq.com"
    );
  } catch {
    return false;
  }
}
/** 同一次 HTML 解析生成原文快照与可编辑正文；不运行微信脚本。 */
export async function captureWechat(
  url: string,
  taskId: string,
  signal?: AbortSignal,
): Promise<WebCaptureResult> {
  if (!isWechatUrl(url)) throw new Error("不是微信公众号地址");
  const deadline = AbortSignal.timeout(180000),
    combined = signal ? AbortSignal.any([signal, deadline]) : deadline;
  const fetched = await fetchHtml(url, combined);
  if (!isWechatUrl(fetched.finalUrl))
    throw new Error("公众号地址跳转到了其他平台");
  const { document } = parseHTML(fetched.html),
    body = document.querySelector("#js_content");
  if (!body || (!body.textContent.trim() && !body.querySelector("img")))
    throw new Error(
      "未获取到公众号正文，页面可能要求验证、已失效或受访问限制；请打开原文确认后重试",
    );
  const meta = (selector: string) =>
    document.querySelector(selector)?.getAttribute("content")?.trim() ?? "";
  const title =
    document.querySelector("#activity-name")?.textContent.trim() ||
    meta('meta[property="og:title"]') ||
    document.title ||
    url;
  const account = document.querySelector("#js_name")?.textContent.trim() ?? "";
  const author = meta('meta[name="author"]');
  const date = meta('meta[property="article:published_time"]');
  const publishedAt =
    /(?:Z|[+-]\d\d:\d\d)$/.test(date) && Number.isFinite(Date.parse(date))
      ? Date.parse(date)
      : null;
  const warnings: string[] = [];
  // 只移除已知入口容器的延迟显示属性，保留作者设置的其他样式。
  (body as unknown as HTMLElement).style.removeProperty("visibility");
  (body as unknown as HTMLElement).style.removeProperty("opacity");
  if ((body as unknown as HTMLElement).style.display === "none")
    (body as unknown as HTMLElement).style.removeProperty("display");
  for (const image of body.querySelectorAll("img")) {
    const src = image.getAttribute("data-src") || image.getAttribute("src");
    if (src) image.setAttribute("src", src);
  }
  for (const media of body.querySelectorAll(
    "iframe,video,audio,mpvoice,mpvideo,mp-miniprogram,wx-open-launch-weapp",
  )) {
    const placeholder = document.createElement("p");
    placeholder.textContent = "音视频或互动内容：请打开原文查看";
    const cover =
      media.getAttribute("poster") || media.getAttribute("data-cover");
    if (cover) {
      const image = document.createElement("img");
      image.setAttribute("src", cover);
      placeholder.appendChild(image);
    }
    media.replaceWith(placeholder);
    warnings.push("音视频或互动内容仅保留说明与原文入口");
  }
  if (body.querySelector("animate,animateTransform,set,foreignObject"))
    warnings.push("动态 SVG 装饰已降级为静态内容");
  const urls = new Set<string>();
  const unsupported = new Map<string,string>();
  const absolute = (value: string) => {
    try {
      const u = new URL(value, fetched.finalUrl);
      return ["https:", "http:"].includes(u.protocol) &&
        !u.username &&
        !u.password
        ? u.href
        : undefined;
    } catch {
      return undefined;
    }
  };
  const collect = (value: string) => {
    const u = absolute(value);
    if (u) urls.add(u);
    else if (value) unsupported.set(value.slice(0,200), "图片或样式资源地址不支持离线保存");
    return u;
  };
  for (const image of body.querySelectorAll("img[src]"))
    collect(image.getAttribute("src"));
  const coverUrl = meta('meta[property="og:image"]');
  if (coverUrl) collect(coverUrl);
  for (const anchor of body.querySelectorAll("a[href]")) {
    const href = anchor.getAttribute("href");
    if (!href.startsWith("#"))
      anchor.setAttribute("href", absolute(href) || "");
  }
  for (const node of body.querySelectorAll("[style]"))
    cleanCss(node.getAttribute("style"), collect);
  cleanCss(body.getAttribute("style") || "", collect);
  const styles = [...document.querySelectorAll("style")]
    .map((n) => n.textContent)
    .join("\n");
  // 保留页面静态类样式；隔离文档与 CSS AST 清理阻断全局样式泄漏及外部导入。
  const stylesheet = cleanCss(styles, collect, true);
  const { assets, failures } = await collectSnapshotAssets([...urls], combined);
  failures.push(...[...unsupported].map(([url,reason])=>({url,reason})));
  try {
    // 队列检查取消状态并释放本次资源，保留清单以完成清理。
    const mapping = new Map(
      assets.map((a) => [a.sourceUrl, `local-image://${a.fileName}`]),
    );
    const map = (value: string) => mapping.get(absolute(value) ?? "");
    const html = cleanHtml(body.outerHTML, map),
      css = cleanCss(stylesheet, map, true);
    const snapshot: WebSnapshot = {
      formatVersion: 1,
      policyVersion: 1,
      adapterVersion: "wechat-html/1",
      html,
      css,
      hash: "",
      account,
      author,
      publishedAt,
      assets,
      failures,
      warnings: [...new Set(warnings)],
    };
    snapshot.cover = map(meta('meta[property="og:image"]'));
    snapshot.hash = snapshotHash(snapshot);
    const turndown = new TurndownService({
      headingStyle: "atx",
      codeBlockStyle: "fenced",
    });
    turndown.addRule("missing-image", {
      filter: (node) => node.nodeName === "IMG" && !node.getAttribute("src"),
      replacement: () => "\n\n[图片未保存]\n\n",
    });
    const markdown =
      turndown.turndown(html).trim() + `\n\n---\n\n来源：<${fetched.finalUrl}>`;
    return {
      taskId,
      entryUrl: url,
      finalUrl: fetched.finalUrl,
      title,
      author,
      publishedAt,
      dateConfidence: publishedAt ? "exact" : "unknown",
      markdown,
      links: [],
      paragraphs: markdown
        .split(/\n\s*\n/)
        .map((text, i) => ({ id: `p${i + 1}`, text })),
      contentHash: createHash("sha256").update(markdown).digest("hex"),
      capturedAt: Date.now(),
      engineVersion: "wechat-html/1",
      complete: failures.length === 0 && !warnings.length,
      truncated: false,
      warnings: [
        ...snapshot.warnings,
        ...(failures.length
          ? [`${failures.length} 个图片资源未保存，可重试补采`]
          : []),
      ],
      snapshot,
    };
  } catch (error) {
    releaseSnapshotAssets(assets);
    throw error;
  }
}
