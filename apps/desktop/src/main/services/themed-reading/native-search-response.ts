import type { ReadingQuery } from "@guizhi/shared/types/reading-reconstruction";
import { isPublicSearchUrl } from "./search-result-url";

/** 兼容 Responses JSON 与 SSE；只接受完整终态，不能把中途输出当成功。 */
export function decodeNativeSearchResponse(body: string): any {
  try {
    if (body.trimStart().startsWith("{")) return JSON.parse(body);
    let response: unknown;
    for (const frame of body.replace(/\r\n/g, "\n").split("\n\n")) {
      const data = frame.split("\n").filter(line => line.startsWith("data:"))
        .map(line => line.slice(5).trimStart()).join("\n");
      if (!data || data === "[DONE]") continue;
      const event = JSON.parse(data);
      if (["error", "response.failed", "response.incomplete"].includes(event.type)) throw new Error();
      if (event.type === "response.completed") response = event.response;
    }
    if (!response) throw new Error();
    return response;
  } catch { throw new Error("原生搜索响应不完整或格式无效，请重试"); }
}

/** 来源只取工具元数据与结构化引用；生成的回答和 Markdown 链接不是网页正文。 */
export function nativeSearchResults(response: any): ReadingQuery["results"] {
  if (response?.status !== "completed" || response.error || !Array.isArray(response.output)) {
    throw new Error("原生搜索未完成，请重试或切换搜索服务");
  }
  const searches = response.output.filter((item: any) => item?.type === "web_search_call");
  if (!searches.some((item: any) => item.status === "completed" && item.action?.type === "search") ||
      searches.some((item: any) => item.status !== "completed")) {
    throw new Error("模型服务未完成联网搜索，请确认支持原生搜索，或切换 AnySearch / Tavily");
  }
  const candidates: { url?: unknown; title?: unknown }[] = [];
  // 优先采用实际引用的网页；其余已检索来源作为补充候选。
  for (const item of response.output) {
    if (item?.type !== "message" || !Array.isArray(item.content)) continue;
    for (const part of item.content) if (part?.type === "output_text" && Array.isArray(part.annotations)) {
      for (const citation of part.annotations) if (citation?.type === "url_citation") candidates.push(citation);
    }
  }
  for (const item of searches) {
    if (Array.isArray(item.action?.sources)) candidates.push(...item.action.sources.filter((s: any) => s?.type === "url"));
    if (item.action?.type === "open_page") candidates.push({ url: item.action.url });
  }
  const results = new Map<string, ReadingQuery["results"][number]>();
  for (const candidate of candidates) {
    if (!isPublicSearchUrl(candidate?.url)) continue;
    const url = new URL(candidate.url); url.hash = "";
    if (!results.has(url.href)) results.set(url.href, { url: url.href,
      title: typeof candidate.title === "string" && candidate.title.trim() ? candidate.title.trim().slice(0, 500) : url.hostname });
    if (results.size >= 5) break;
  }
  if (!results.size) throw new Error("原生搜索未返回有效的公开网页来源，请重试或切换搜索服务");
  return [...results.values()];
}
