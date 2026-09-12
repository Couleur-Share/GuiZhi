import type { ThemedReadingVersion } from "@guizhi/shared/types";
import { readThemeAsset } from "./assets";
import { themedReadingDocument } from "./document";
import { retainAssetFiles } from "../asset-cleanup";
import { parseHTML } from "linkedom";
import { v3InnerDocument, v3WrapperDocument } from "./v3-document";

export const THEMED_EXPORT_LIMIT = 100 * 1024 * 1024;

/** 图片内嵌后仍以最终 UTF-8 字节数核验上限，不把宿主桥接脚本带出。 */
export async function exportThemedReadingHtml(version: ThemedReadingVersion, withoutImages = false, staticOnly = false): Promise<string> {
  const release = retainAssetFiles(version.assets.flatMap(a => a.fileName ? [a.fileName] : []));
  try {
    const urls: Record<string, string> = {};
    let bytes = 0;
    if (!withoutImages) for (const asset of version.assets) {
      if (asset.status !== "ready") throw new Error(`素材不完整：${asset.alt}。请补生成，或选择无图导出。`);
      const { data, mime } = await readThemeAsset(asset);
      bytes += Math.ceil(data.length / 3) * 4;
      if (bytes > THEMED_EXPORT_LIMIT) throw new Error("单文件 HTML 超过 100 MiB，请使用无图导出");
      urls[asset.id] = `data:${mime};base64,${data.toString("base64")}`;
    }
    let html = version.formatVersion === 3 ? v3InnerDocument(version, urls, !staticOnly,false,false) : themedReadingDocument(version, undefined, urls);
    if (version.formatVersion === 3) {
      if (staticOnly) {
        const doc = parseHTML(html).document;
        for (const script of doc.querySelectorAll("script")) script.remove();
        const meta = doc.querySelector('meta[http-equiv="Content-Security-Policy"]');
        meta?.setAttribute("content", "default-src 'none';script-src 'none';style-src 'unsafe-inline';img-src data:;base-uri 'none';form-action 'none'");
        html = "<!doctype html>" + doc.documentElement.outerHTML;
      } else html = v3WrapperDocument(html);
    }
    if (Buffer.byteLength(html) > THEMED_EXPORT_LIMIT) throw new Error("单文件 HTML 超过 100 MiB，请使用无图导出");
    const { document } = parseHTML(html);
    if ([...document.querySelectorAll("[src],[href]")].some(node => /^local-image:/i.test(node.getAttribute("src") ?? node.getAttribute("href") ?? ""))) throw new Error("导出仍含本地图片引用，未写入文件");
    return html;
  } finally { release(); }
}
