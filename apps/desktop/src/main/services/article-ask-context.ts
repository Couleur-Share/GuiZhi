import { themedReadingDocument } from "./themed-reading/document";
import { createHash } from "node:crypto";
import { parseHTML } from "linkedom";
import { KnowledgeItemDB, WebSourceDB, ThemedReadingDB } from "@guizhi/db";
import type Database from "../database/sqlite";
import type { ArticleContext, ArticleTarget, ArticleView } from "@guizhi/shared/types/article-ask";
import { readingSourceText } from "@guizhi/shared/utils/reading-source";
import { splitForumNoteSections } from "@guizhi/shared/utils/forum-note";
import { splitImageNoteSections } from "@guizhi/shared/utils/image-note";
import { normalizeArticleText, selectArticleContext } from "@guizhi/shared/utils/article-context";

const views: ArticleView[] = ["body", "summary", "transcript", "recognized", "replies", "snapshot", "themed"];
export function parseArticleTarget(raw: ArticleTarget): ArticleTarget {
  if (!raw || typeof raw.itemId !== "string" || !raw.itemId || raw.itemId.length > 200 || !views.includes(raw.view)) throw new Error("本文来源无效");
  if (raw.versionId !== undefined && (typeof raw.versionId !== "string" || raw.versionId.length > 200)) throw new Error("来源版本无效");
  if (raw.sourceKind !== undefined && !["body", "summary"].includes(raw.sourceKind)) throw new Error("阅读来源无效");
  if (raw.selection !== undefined && (typeof raw.selection !== "string" || raw.selection.length > 4000)) throw new Error("选段最多 4000 字，请缩小选择范围");
  return { itemId: raw.itemId, view: raw.view, sourceKind: raw.sourceKind, versionId: raw.versionId, selection: raw.selection, floor: Number.isSafeInteger(raw.floor) && raw.floor > 0 ? raw.floor : undefined };
}
function htmlText(html: string): string {
  const { document } = parseHTML(html.includes("<html") ? html : `<html><body>${html}</body></html>`);
  document.querySelectorAll("script,style,nav,button,input,textarea").forEach(n => n.remove());
  document.querySelectorAll("p,div,h1,h2,h3,li,tr,section").forEach(n => n.appendChild(document.createTextNode("\n\n")));
  return document.body.textContent ?? "";
}
export function articleContext(db: Database.Database, input: { target: ArticleTarget; question: string }): ArticleContext {
  const target = parseArticleTarget(input?.target);
  if (typeof input.question !== "string" || input.question.length > 8000) throw new Error("问题最多 8000 字");
  const item = new KnowledgeItemDB(db).get(target.itemId);
  if (!item || item.deletedAt != null) throw new Error("文章不存在或已删除，请先恢复文章");
  const labels: Record<ArticleView, string> = { body: "正文", summary: "AI 讨论总结", transcript: "文字稿", recognized: "OCR 文本", replies: "论坛讨论", snapshot: "网页快照", themed: "AI 阅读页" };
  let text: string, original = "";
  let priorReferences: ArticleContext["sources"] = [];
  switch (target.view) {
    case "snapshot": {
      if (!target.versionId) throw new Error("网页快照尚未载入，请稍后重试");
      const version = new WebSourceDB(db).versions(item.id).find(v => v.id === target.versionId);
      if (!version) throw new Error("正在查看的快照版本已不存在");
      text = version.markdown;
      break;
    }
    case "themed": {
      if (!target.versionId) throw new Error("AI 阅读页尚未载入，请稍后重试");
      const version = new ThemedReadingDB(db).getVersion(target.versionId);
      if (!version || version.itemId !== item.id || version.sourceKind !== (target.sourceKind ?? "body")) throw new Error("正在查看的 AI 阅读版本已不存在或不属于本文");
      text = htmlText(themedReadingDocument(version));
      original = version.source.content;
      const used = new Set(version.reconstruction?.draft.flatMap(section => section.referenceIds) ?? []);
      priorReferences = (version.reconstruction?.references ?? []).filter(reference => reference.status === "ready" && reference.text?.trim() && used.has(reference.id)).slice(0, 3).map(reference => ({
        ordinal: 0, kind: "web", title: `AI 阅读页已有资料 · ${reference.title}`, url: reference.url, capturedAt: reference.capturedAt,
        text: selectArticleContext(reference.text, input.question, "", 1200).text,
      }));
      break;
    }
    case "transcript": text = item.transcript ?? ""; break;
    case "recognized": text = splitImageNoteSections(item.content).recognized; break;
    case "replies": text = splitForumNoteSections(item.content).replies; break;
    default: text = readingSourceText(item, target.view); break;
  }
  if (!normalizeArticleText(text)) throw new Error("还没有可提问的文本，请先采集、识别或转写");
  if (target.selection && !normalizeArticleText(text).includes(normalizeArticleText(target.selection))) throw new Error("选段与当前保存的来源不一致，请重新选择或完成保存");
  const fingerprint = createHash("sha256").update(text).digest("hex");
  const selected = selectArticleContext(text, input.question, target.selection);
  const sources: ArticleContext["sources"] = [{ ordinal: 1, kind: "article", title: `${labels[target.view]}${target.floor ? ` · 第 ${target.floor} 楼` : ""} · ${item.title}`, text: selected.text, target, fingerprint }];
  if (!original && !["body", "snapshot"].includes(target.view)) original = readingSourceText(item, "body");
  if (original && original !== text) {
    const extra = selectArticleContext(original, input.question, "", 2500);
    sources.push({ ordinal: 2, kind: "article", title: `原文补充 · ${item.title}`, text: extra.text, target: { itemId: item.id, view: "body" }, fingerprint: createHash("sha256").update(original).digest("hex") });
  }
  for (const reference of priorReferences) sources.push({ ...reference, ordinal: sources.length + 1 });
  return { target, title: item.title, fingerprint, sources, clipped: selected.clipped };
}
