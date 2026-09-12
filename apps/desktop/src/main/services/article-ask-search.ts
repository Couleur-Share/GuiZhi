import type { ArticleSearchResult, ArticleSource } from "@guizhi/shared/types/article-ask";
import { searchReadingWeb } from "./themed-reading/search-service";
import { captureWebPage } from "./web-capture/web-capture";
import { selectArticleContext } from "@guizhi/shared/utils/article-context";

export async function searchArticleWeb(queries: string[], signal: AbortSignal): Promise<ArticleSearchResult> {
  const warnings: string[] = [], candidates = new Map<string, { title: string; url: string; text?: string }>();
  // 串行查询/补抓，避免一次提问挤满共享网页采集队列。
  for (const query of queries) {
    signal.throwIfAborted();
    try {
      for (const result of await searchReadingWeb(query, signal, "qa")) {
        const url = new URL(result.url); url.hash = "";
        if (!candidates.has(url.href)) candidates.set(url.href, { ...result, url: url.href });
      }
    } catch (error) { signal.throwIfAborted(); warnings.push(error instanceof Error ? error.message : "搜索失败"); }
  }
  const sources: ArticleSource[] = [];
  let captures = 0;
  for (const result of candidates.values()) {
    signal.throwIfAborted();
    if (sources.length >= 5) break;
    let text = result.text?.trim() ?? "";
    if (text.length < 200 && captures < 3) {
      captures++;
      try {
        const captured = await captureWebPage({ url: result.url, purpose: "research", taskId: `article-ask-${Date.now()}` }, AbortSignal.any([signal, AbortSignal.timeout(20000)]));
        if (captured.error || !captured.complete) throw new Error("网页正文未完整取得");
        text = captured.markdown;
      } catch { signal.throwIfAborted(); warnings.push("部分网页正文未能取得"); }
    }
    if (text.length < 200) { warnings.push("部分搜索结果缺少有效正文，未作为依据"); continue; }
    sources.push({ ordinal: 0, kind: "web", title: result.title, url: result.url, text: selectArticleContext(text, queries.join(" "), "", 2200).text, capturedAt: Date.now() });
  }
  if (!sources.length) warnings.push("未取得有效联网资料，本次仅结合本文解释，未完成联网查证");
  return { success: true, sources, warnings: [...new Set(warnings)] };
}
