/**
 * Wiki 页面草稿的解析与清洗：从模型输出提取页面 JSON、净化字段、
 * 按标题与别名解析 [[链接]]。全文编译器、问答与页面手动编辑共用。
 */
import type { WikiCatalogEntry, WikiPageKind } from "@guizhi/shared/types";
import { stripDuplicateTitleHeading } from "@guizhi/shared/utils/wiki-body";
import { truncatePlain } from "./prompts";

const MAX_PAGES_PER_ITEM = 4;
const MAX_ALIAS_COUNT = 5;
const MAX_ALIAS_LENGTH = 60;
const TITLE_MAX_LENGTH = 120;
const SUMMARY_MAX_LENGTH = 300;
const BODY_MAX_LENGTH = 8000;
const WIKI_LINK_REGEX = /\[\[([^[\]]+)\]\]/g;

/** 规范化标题：去首尾空白、内部连续空白折叠、不区分大小写（[[链接]] 锚点与唯一性判定）。 */
export function normalizeWikiTitle(title: string): string {
  return title.split(/\s+/).filter(Boolean).join(" ").toUpperCase();
}

export function parseAliases(aliasesJson: string | null | undefined): string[] {
  if (!aliasesJson) {
    return [];
  }
  try {
    const parsed = JSON.parse(aliasesJson);
    return Array.isArray(parsed)
      ? parsed.filter((alias): alias is string => typeof alias === "string")
      : [];
  } catch {
    return [];
  }
}

export interface WikiPageDraft {
  title: string;
  normalizedTitle: string;
  kind: WikiPageKind;
  summary: string;
  body: string;
  aliasesJson: string | null;
}

interface RawPageDto {
  title?: unknown;
  kind?: unknown;
  summary?: unknown;
  aliases?: unknown;
  body?: unknown;
}

/** 从模型输出提取页面 JSON（容忍代码块围栏与前后缀文字）。 */
export function parseWikiResponse(text: string): RawPageDto[] | null {
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start < 0 || end <= start) {
    return null;
  }
  try {
    const parsed = JSON.parse(text.slice(start, end + 1)) as {
      pages?: unknown;
    };
    return Array.isArray(parsed.pages) ? (parsed.pages as RawPageDto[]) : null;
  } catch {
    return null;
  }
}

function parseKind(kind: unknown): WikiPageKind {
  const normalized = typeof kind === "string" ? kind.trim().toLowerCase() : "";
  return normalized === "entity" || normalized === "concept"
    ? normalized
    : "topic";
}

function firstLine(text: unknown): string {
  const trimmed = typeof text === "string" ? text.trim() : "";
  const newlineIndex = trimmed.indexOf("\n");
  return newlineIndex >= 0 ? trimmed.slice(0, newlineIndex).trim() : trimmed;
}

/** 净化模型输出：字段截断、类别解析、批内按规范化标题去重，上限 4 页。 */
export function sanitizePages(pages: RawPageDto[]): WikiPageDraft[] {
  const drafts: WikiPageDraft[] = [];
  const seen = new Set<string>();
  for (const dto of pages) {
    if (drafts.length >= MAX_PAGES_PER_ITEM) {
      break;
    }
    const title = truncatePlain(
      typeof dto.title === "string" ? dto.title.trim() : "",
      TITLE_MAX_LENGTH,
    );
    if (!title) {
      continue;
    }
    const normalized = normalizeWikiTitle(title);
    if (seen.has(normalized)) {
      continue;
    }
    seen.add(normalized);

    // 详情页头部已经渲染了 title，模型还常在正文首行放一个同名一级标题，
    // 落库前剥掉；渲染侧另有一层兜底，负责已经入库的老页面
    const body = stripDuplicateTitleHeading(
      truncatePlain(
        typeof dto.body === "string" ? dto.body.trim() : "",
        BODY_MAX_LENGTH,
      ),
      title,
    );
    if (!body) {
      continue;
    }

    const aliases = (Array.isArray(dto.aliases) ? dto.aliases : [])
      .filter((alias): alias is string => typeof alias === "string")
      .map((alias) => alias.trim())
      .filter(
        (alias) =>
          alias.length > 0 &&
          alias.length <= MAX_ALIAS_LENGTH &&
          normalizeWikiTitle(alias) !== normalized,
      )
      .filter((alias, index, all) => all.indexOf(alias) === index)
      .slice(0, MAX_ALIAS_COUNT);

    drafts.push({
      title,
      normalizedTitle: normalized,
      kind: parseKind(dto.kind),
      summary: truncatePlain(firstLine(dto.summary), SUMMARY_MAX_LENGTH),
      body,
      aliasesJson: aliases.length > 0 ? JSON.stringify(aliases) : null,
    });
  }
  return drafts;
}

/**
 * 构建链接解析器：规范化的标题/别名 → 目标页规范显示标题。
 * 标题优先于别名；冲突先到先得（批内页先于目录页——本批是最新状态）。
 */
export function buildLinkResolver(
  catalog: WikiCatalogEntry[],
  batch: WikiPageDraft[],
): Map<string, string> {
  const resolver = new Map<string, string>();
  for (const draft of batch) {
    resolver.set(draft.normalizedTitle, draft.title);
  }
  for (const entry of catalog) {
    if (!resolver.has(entry.normalizedTitle)) {
      resolver.set(entry.normalizedTitle, entry.title);
    }
  }
  for (const draft of batch) {
    for (const alias of parseAliases(draft.aliasesJson)) {
      const key = normalizeWikiTitle(alias);
      if (!resolver.has(key)) {
        resolver.set(key, draft.title);
      }
    }
  }
  for (const entry of catalog) {
    for (const alias of parseAliases(entry.aliasesJson)) {
      const key = normalizeWikiTitle(alias);
      if (!resolver.has(key)) {
        resolver.set(key, entry.title);
      }
    }
  }
  return resolver;
}

/**
 * 清洗正文里的 [[链接]]：能按标题/别名解析的重写为 [[目标页标题]] 或 [[目标页标题|显示文字]]，
 * 解析失败的去掉括号降级为纯文本；返回清洗后的正文与去重的目标集合（规范化标题）。
 */
export function cleanWikiLinks(
  body: string,
  resolver: Map<string, string>,
): { body: string; targets: string[] } {
  const targets: string[] = [];
  const cleaned = body.replace(WIKI_LINK_REGEX, (_match, inner: string) => {
    const separatorIndex = inner.indexOf("|");
    const target = (
      separatorIndex >= 0 ? inner.slice(0, separatorIndex) : inner
    ).trim();
    const display =
      separatorIndex >= 0 ? inner.slice(separatorIndex + 1).trim() : target;
    if (!target) {
      return display;
    }

    const canonicalTitle = resolver.get(normalizeWikiTitle(target));
    if (canonicalTitle) {
      const canonicalNormalized = normalizeWikiTitle(canonicalTitle);
      if (!targets.includes(canonicalNormalized)) {
        targets.push(canonicalNormalized);
      }
      return !display || display === canonicalTitle
        ? `[[${canonicalTitle}]]`
        : `[[${canonicalTitle}|${display}]]`;
    }
    return display;
  });
  return { body: cleaned, targets };
}

const KIND_LABELS: Record<WikiPageKind, string> = {
  topic: "主题",
  entity: "实体",
  concept: "概念",
};

export function wikiKindLabel(kind: WikiPageKind): string {
  return KIND_LABELS[kind];
}
