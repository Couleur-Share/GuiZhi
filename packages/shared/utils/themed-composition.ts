import type { ThemedComposition } from "../types/themed-composition";
import type { ThemedReadingSource } from "../types/themed-reading";

const normalized = (text: string) => text.replace(/\s+/gu, " ").trim();
function fail(message: string): never { throw new Error(`专题重构：${message}`); }
function record(value: unknown, keys: string[]): Record<string, any> {
  if (!value || typeof value !== "object" || Array.isArray(value)) fail("对象格式无效");
  if (Object.keys(value).some(key => !keys.includes(key))) fail("包含未知字段");
  return value as Record<string, any>;
}
function text(value: unknown, max = 160): asserts value is string {
  if (typeof value !== "string" || !value.trim() || value.length > max) fail("文字为空或超过长度限制");
}
function list(value: unknown, min: number, max: number): asserts value is any[] {
  if (!Array.isArray(value) || value.length < min || value.length > max) fail("列表数量无效");
}

/** 同时用于生成、数据库/备份读入和渲染；严格验证引用原句，禁止可执行表达式。 */
export function validateThemedComposition(value: unknown, source: ThemedReadingSource, assetIds: string[]): asserts value is ThemedComposition {
  const root = record(value, ["version", "chapters"]);
  if (root.version !== 1 || JSON.stringify(value).length > 4_000_000) fail("版本无效或数据过大");
  list(root.chapters, 1, 1000);
  const allIds: string[] = [];
  const sources = new Map(source.blocks.map(block => [block.id, normalized(block.text)]));
  const image = (id: unknown) => { if (id !== undefined && (typeof id !== "string" || !assetIds.includes(id))) fail("图片素材不存在"); };
  for (const raw of root.chapters) {
    const chapter = record(raw, ["blockIds", "palette", "category", "displayTitle", "subtitle", "lead", "imageId", "sections"]);
    if (chapter.displayTitle !== undefined) { text(chapter.displayTitle, 100); if (!source.title.includes(chapter.displayTitle)) fail("短标题必须截取原文标题"); }
    list(chapter.blockIds, 1, 10000);
    if (chapter.blockIds.some(id => typeof id !== "string" || !sources.has(id))) fail("章节引用未知原文");
    allIds.push(...chapter.blockIds);
    if (!["marine", "amber", "forest", "ink"].includes(chapter.palette)) fail("主题配色无效");
    text(chapter.category, 40); image(chapter.imageId);
    const cited = new Set<string>();
    const statement = (value: unknown) => {
      const entry = record(value, ["text", "kind", "evidence", "emphasis"]);
      text(entry.text, 2400);
      if (!["quote", "summary", "inference"].includes(entry.kind)) fail("未区分摘录、归纳与推导");
      if (entry.emphasis !== undefined) { list(entry.emphasis, 1, 6); for (const value of entry.emphasis) { text(value, 80); if (!entry.text.includes(value)) fail("强调文字不在陈述中"); } }
      list(entry.evidence, 1, 12);
      for (const raw of entry.evidence) {
        const ref = record(raw, ["blockId", "quote"]);
        if (!chapter.blockIds.includes(ref.blockId)) fail("引用不在当前章节原文范围内");
        text(ref.quote, 600);
        const sourceText = sources.get(ref.blockId) ?? "", quote = normalized(ref.quote);
        if (quote.length < Math.min(4, sourceText.length) || !sourceText.includes(quote)) fail(`引用原句不匹配 ${ref.blockId}`);
        cited.add(ref.blockId);
      }
      if (entry.kind === "quote" && !entry.evidence.some(ref => normalized(ref.quote) === normalized(entry.text))) fail("原文摘录与引用原句不一致");
    };
    statement(chapter.subtitle); statement(chapter.lead);
    list(chapter.sections, 1, 30);
    for (const raw of chapter.sections) {
      const section = record(raw, ["title", "label", "navLabel", "layout", "intro", "imageId", "items", "columns", "rows", "calculator"]);
      text(section.title, 100); if (section.label !== undefined) text(section.label, 40);
      if (section.navLabel !== undefined) text(section.navLabel, 24);
      if (!["cards", "comparison", "illustrated", "prose", "checklist", "matrix", "explorer", "calculator"].includes(section.layout)) fail("未知呈现组件");
      if (section.intro !== undefined) statement(section.intro);
      image(section.imageId);
      if (section.imageId && ["matrix", "calculator", "explorer"].includes(section.layout)) fail("该组件没有图片位置，请使用图文组件");
      if (section.layout === "matrix") {
        list(section.columns, 1, 6); section.columns.forEach(column => text(column, 40));
        list(section.rows, 1, 40);
        for (const raw of section.rows) {
          const row = record(raw, ["label", "cells"]); text(row.label, 60);
          list(row.cells, section.columns.length, section.columns.length); row.cells.forEach(statement);
        }
        if (section.items !== undefined || section.calculator !== undefined) fail("矩阵包含不适用的字段");
      } else if (section.layout === "calculator") {
        if (!["unit-cost", "daily-total"].includes(section.calculator) || !section.intro) fail("计算工具须使用内置公式并说明原文关联");
        if (section.items !== undefined || section.columns !== undefined || section.rows !== undefined) fail("计算工具包含不适用的字段");
      } else {
        list(section.items, 1, section.layout === "explorer" ? 6 : 12);
        if (section.columns !== undefined || section.rows !== undefined || section.calculator !== undefined) fail("内容组件包含不适用的字段");
        for (const raw of section.items) {
          const item = record(raw, ["title", "badge", "tone", "body", "metrics", "takeaway"]);
          text(item.title, 100); if (item.badge !== undefined) text(item.badge, 40);
          if (item.tone !== undefined && !["neutral", "positive", "caution"].includes(item.tone)) fail("状态配色无效");
          statement(item.body);
          if (item.takeaway !== undefined) statement(item.takeaway);
          if (item.metrics !== undefined) {
            list(item.metrics, 1, 6);
            for (const raw of item.metrics) { const metric = record(raw, ["label", "value"]); text(metric.label, 40); statement(metric.value); }
          }
        }
      }
    }
    // 标题和纯图片仍在原文索引完整呈现；有文字的正文必须参与重构或作为证据。
    for (const id of chapter.blockIds) {
      const block = source.blocks.find(block => block.id === id);
      if (!block) fail("原文块不存在");
      if (normalized(block.text) && !/^<h[1-6][ >]/u.test(block.html) && !cited.has(id)) fail(`正文尚未纳入重构依据 ${id}`);
    }
  }
  if (allIds.length !== source.blocks.length || allIds.some((id, index) => id !== source.blocks[index].id)) fail("章节原文范围存在遗漏、重复或顺序错误");
}

export function compositionAssetIds(value: ThemedComposition): string[] {
  return value.chapters.flatMap(chapter => [chapter.imageId, ...chapter.sections.map(section => section.imageId)]).filter((id): id is string => Boolean(id));
}
