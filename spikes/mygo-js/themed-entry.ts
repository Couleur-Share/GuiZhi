// 打进 goja 的主题阅读渲染层入口：直接引用 TS 原文件（正文分块、净化、v3 页面规范化与校验、文档拼装、导出文档），
// 保证与 Electron 版逐字节一致。只含纯函数；文件读写、数据库与模型请求都留在 Go 侧。
import "./text-polyfill";
import "./url-polyfill";
import { parseHTML } from "linkedom";
import { themedReadingDocument } from "../../apps/desktop/src/main/services/themed-reading/document";
import { validateThemedReadingVersion, validateThemedReadingTask } from "../../packages/db/src/themed-reading-validation";
import { renderThemedReadingBlocks } from "../../apps/desktop/src/main/services/themed-reading/content";
import { ensureRequestedReadingImage } from "../../apps/desktop/src/main/services/themed-reading/required-image";
import { prepareResearchRetry } from "../../apps/desktop/src/main/services/themed-reading/research-pipeline";
import { validateReadingVisuals } from "../../packages/shared/utils/reading-visuals";
import { isSafeAssetFileName } from "../../packages/shared/utils/media-refs";
import { v3InnerDocument, v3WrapperDocument, validateV3Page, normalizeV3Section, cleanV3Html, checkReadingScript, ReadingPageError } from "../../apps/desktop/src/main/services/themed-reading/v3-document";

/** ReadingPageError 是可修复的页面问题（回传给模型修复），其余异常原样抛出。 */
function guarded<T>(fn: () => T): { ok: T } | { issue: unknown } {
  try { return { ok: fn() }; }
  catch (error) { if (error instanceof ReadingPageError) return { issue: error.issue }; throw error; }
}

(globalThis as Record<string, unknown>).GZT = {
  document: (version: string, instanceId: string | null, assetUrls: string | null) =>
    themedReadingDocument(JSON.parse(version), instanceId ?? undefined, assetUrls ? JSON.parse(assetUrls) : undefined),
  /** 数据库与备份均非可信输入：与 packages/db 同一份结构校验，失败时抛出原文案。 */
  validateVersion: (json: string) => { validateThemedReadingVersion(JSON.parse(json)); return true; },
  validateTask: (json: string) => { validateThemedReadingTask(JSON.parse(json)); return true; },
  renderBlocks: (content: string) => JSON.stringify(renderThemedReadingBlocks(content)),
  v3Inner: (version: string, assets: string, interactive: boolean, partial: boolean, host: boolean) =>
    JSON.stringify(guarded(() => v3InnerDocument(JSON.parse(version), JSON.parse(assets), interactive, partial, host))),
  v3Wrapper: (inner: string) => v3WrapperDocument(inner),
  validateV3: (version: string, partial: boolean) => JSON.stringify(guarded(() => { validateV3Page(JSON.parse(version), partial); return null; })),
  normalizeSection: (html: string, unit: string, enhanced: boolean) => JSON.stringify(guarded(() => normalizeV3Section(html, unit, enhanced))),
  cleanV3Html: (html: string, assets: string) => cleanV3Html(html, JSON.parse(assets)),
  checkScript: (module: string) => JSON.stringify(checkReadingScript(JSON.parse(module)) ?? null),
  /** export.ts 的 HTML 组装部分（图片字节与大小核验在 Go 侧）。 */
  exportDocument: (version: string, urls: string, staticOnly: boolean) => {
    const page = JSON.parse(version), map = JSON.parse(urls);
    let html = page.formatVersion === 3 ? v3InnerDocument(page, map, !staticOnly, false, false) : themedReadingDocument(page, undefined, map);
    if (page.formatVersion === 3) {
      if (staticOnly) {
        const doc = parseHTML(html).document;
        for (const script of doc.querySelectorAll("script")) script.remove();
        doc.querySelector('meta[http-equiv="Content-Security-Policy"]')?.setAttribute("content", "default-src 'none';script-src 'none';style-src 'unsafe-inline';img-src data:;base-uri 'none';form-action 'none'");
        html = "<!doctype html>" + doc.documentElement.outerHTML;
      } else html = v3WrapperDocument(html);
    }
    return html;
  },
  /** assets.ts 的 collectOriginalThemeAssets（assets.ts 本身依赖文件系统，这里只带出纯函数部分）。 */
  collectOriginalAssets: (version: string) => {
    const page = JSON.parse(version);
    const { document } = parseHTML(`<html><body>${page.source.blocks.map((b: { html: string }) => b.html).join("")}</body></html>`);
    const seen = new Set<string>();
    return JSON.stringify([...document.querySelectorAll("img")].flatMap((node, i) => {
      const url = node.getAttribute("src") ?? "";
      if (!url || seen.has(url)) return [];
      seen.add(url);
      const local = url.startsWith("local-image://") ? url.slice("local-image://".length) : undefined;
      if (local && (!isSafeAssetFileName(local) || !/\.(png|jpe?g|gif|webp)$/i.test(local))) throw new Error("主题图片路径不合法");
      return [{ id: `original-${i}`, role: "original", purpose: "原文图片", prompt: "", alt: node.getAttribute("alt") ?? "原文图片", aspectRatio: "4:3", originalUrl: url, fileName: local, status: "pending" }];
    }));
  },
  /** required-image.ts：就地补一个真实生图槽位，返回修改后的版本。 */
  ensureRequestedImage: (version: string) => { const page = JSON.parse(version); ensureRequestedReadingImage(page); return JSON.stringify(page); },
  /** runtime.ts execute：设计里实际用到的素材 ID。 */
  usedAssetIds: (html: string) => {
    const { document } = parseHTML(`<html><body>${html}</body></html>`);
    return JSON.stringify([...document.querySelectorAll("img[data-theme-asset]")].map(node => node.getAttribute("data-theme-asset")));
  },
  prepareResearchRetry: (reconstruction: string) => { const state = JSON.parse(reconstruction); prepareResearchRetry(state); return JSON.stringify(state); },
  validateVisuals: (reconstruction: string) => { const r = JSON.parse(reconstruction); validateReadingVisuals(r.visuals, r.animations, r.draft); return true; },
  hasLocalImageRef: (html: string) => {
    const { document } = parseHTML(html);
    return [...document.querySelectorAll("[src],[href]")].some(node => /^local-image:/i.test(node.getAttribute("src") ?? node.getAttribute("href") ?? ""));
  },
};