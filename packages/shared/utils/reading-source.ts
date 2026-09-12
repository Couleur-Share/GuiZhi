import type { KnowledgeItem } from "../types";
import type { ThemedReadingSourceKind } from "../types/themed-reading";
import { splitForumNoteSections } from "./forum-note";
import { splitImageNoteSections } from "./image-note";
import { parseVideoMetaBlock } from "./video-meta";

export function readingSourceText(item: Pick<KnowledgeItem, "content" | "itemType">, kind: ThemedReadingSourceKind): string {
  if (item.itemType === "forum") return splitForumNoteSections(item.content)[kind === "summary" ? "summary" : "body"];
  if (kind === "summary") throw new Error("此条目没有讨论总结");
  if (item.itemType === "image") return splitImageNoteSections(parseVideoMetaBlock(item.content)?.body ?? item.content).caption;
  return parseVideoMetaBlock(item.content)?.body ?? item.content;
}
/** 替换选中来源，其他段落、元数据和换行形式保留。 */
export function replaceReadingSource(item: Pick<KnowledgeItem, "content" | "itemType">, kind: ThemedReadingSourceKind, replacement: string): string {
  const eol = item.content.includes("\r\n") ? "\r\n" : "\n";
  const content = item.content.replace(/\r\n/g, "\n"), next = replacement.replace(/\r\n/g, "\n");
  if (readingSourceText(item, kind) === replacement) return item.content;
  const meta = parseVideoMetaBlock(content), body = meta?.body ?? content;
  const prefix = meta ? content.slice(0, content.length - body.length) : "";
  let result: string;
  if (item.itemType === "forum") {
    const matches = [...body.matchAll(/^##\s*(讨论总结|正文|讨论(?:（[^\n]*）)?)\s*$/gm)];
    const ranges: { start: number; end: number }[] = [];
    if (kind === "body" && (matches[0]?.index ?? body.length) > 0) ranges.push({ start: 0, end: matches[0]?.index ?? body.length });
    matches.forEach((m, i) => { if ((kind === "summary" && m[1] === "讨论总结") || (kind === "body" && m[1] === "正文")) ranges.push({ start: m.index + m[0].length, end: matches[i + 1]?.index ?? body.length }); });
    if (!ranges.length) result = `${body}\n\n## ${kind === "summary" ? "讨论总结" : "正文"}\n\n${next}\n`;
    else {
      result = body;
      for (let i = ranges.length - 1; i >= 0; i--) result = result.slice(0, ranges[i].start) + (i === 0 ? `\n\n${next}\n\n` : "\n\n") + result.slice(ranges[i].end);
    }
  } else if (item.itemType === "image") {
    const parts = splitImageNoteSections(body);
    const end = parts.recognized ? body.indexOf(parts.recognized) : body.length;
    const pictures = body.slice(0, end).split(/\n{2,}/).filter(block => /^(?:!\[[^\]]*\]\([^)]+\)\s*)+$/.test(block.trim()));
    result = [next, ...pictures, parts.recognized].filter(Boolean).join("\n\n");
  } else { if (kind !== "body") throw new Error("此条目没有讨论总结"); result = next; }
  return (prefix + result).replace(/\n/g, eol);
}
