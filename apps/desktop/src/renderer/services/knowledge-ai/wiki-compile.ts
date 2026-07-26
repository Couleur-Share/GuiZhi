/**
 * Wiki 编译引擎（移植自 .NET 版 WikiCompilerService，ADR 0023）：
 * 把知识条目增量编译进 LLM 维护的 Wiki 页面网络。
 * 指纹 = 素材哈希 + 提示词版本（模型仅作出处不触发重编）；
 * 逐条目编译、逐条目落库，中断后下轮续跑。
 * 派生数据纪律：只写 Wiki 四表，绝不改动条目本身。
 */
import type {
  WikiApplyCompilationInput,
  WikiCatalogEntry,
  WikiCompilableItem,
  WikiPageKind,
} from "@guizhi/shared/types";
import {
  WIKI_COMPILE_CATALOG_LIMIT,
  WIKI_COMPILE_CONTEXT_PAGES_LIMIT,
  WIKI_COMPILE_ITEM_CONTENT_LIMIT,
  WIKI_COMPILE_PROMPT_VERSION,
  WIKI_COMPILE_SYSTEM_PROMPT,
  buildWikiCompilePrompt,
  truncatePlain,
} from "./prompts";
import { runScenarioChat } from "./ai-invoke";

const MAX_PAGES_PER_ITEM = 4;
const MAX_ALIAS_COUNT = 5;
const MAX_ALIAS_LENGTH = 60;
const TITLE_MAX_LENGTH = 120;
const SUMMARY_MAX_LENGTH = 300;
const BODY_MAX_LENGTH = 8000;
const WIKI_LINK_REGEX = /\[\[([^[\]]+)\]\]/g;
/** 连续失败到这个次数后不再自动重试，等素材变化或手动全量重建 */
const WIKI_COMPILE_MAX_FAILURES = 3;
/**
 * 输出上限。一次最多 4 个页面、每页正文上限 1200 字（约 1200 token），
 * 光正文就能顶到 5000 token，思考类模型还要另算推理消耗——预算给小了，
 * JSON 会在中途被截断，解析不出页面，整轮编译白跑。
 */
const WIKI_COMPILE_MAX_TOKENS = 8192;
/**
 * 单条目编译的请求超时。全应用最重的一次调用：条目正文 3000 字 + 目录 30 行 +
 * 最多 5 个上下文页（每页正文上限 8000 字），还要生成整份 JSON。
 */
const WIKI_COMPILE_TIMEOUT_MS = 180_000;
/** 首次重试的退避时长，之后每次 ×4（30 分钟 → 2 小时 → 8 小时） */
const WIKI_COMPILE_RETRY_BASE_MS = 30 * 60 * 1000;

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

    const body = truncatePlain(
      typeof dto.body === "string" ? dto.body.trim() : "",
      BODY_MAX_LENGTH,
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

/** 候选页排序：标题/别名在条目素材中出现即计分，分高在前。 */
export function rankCandidates(
  catalog: WikiCatalogEntry[],
  material: string,
): { entry: WikiCatalogEntry; score: number }[] {
  const materialLower = material.toLowerCase();
  return catalog
    .map((entry) => {
      let score = 0;
      if (materialLower.includes(entry.title.toLowerCase())) {
        score += 10;
      }
      for (const alias of parseAliases(entry.aliasesJson)) {
        if (materialLower.includes(alias.toLowerCase())) {
          score += 5;
        }
      }
      return { entry, score };
    })
    .sort((left, right) => right.score - left.score);
}

/** 编译素材（哈希与提示词共用同一截断口径：截断范围外的正文变化不触发重编）。 */
export function buildMaterial(item: WikiCompilableItem): string {
  return `${item.title}\n${truncatePlain(item.content, WIKI_COMPILE_ITEM_CONTENT_LIMIT)}`;
}

export async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(text),
  );
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

const KIND_LABELS: Record<WikiPageKind, string> = {
  topic: "主题",
  entity: "实体",
  concept: "概念",
};

export function wikiKindLabel(kind: WikiPageKind): string {
  return KIND_LABELS[kind];
}

export interface WikiCompileRoundResult {
  compiled: number;
  pending: number;
  /** 条目级解析失败被跳过的数量（下轮重试） */
  skipped: number;
}

/**
 * 编译全部待处理条目。
 * Provider 级失败（未配置/网络）直接抛出中止本轮；
 * 条目级失败（响应无法解析）跳过继续。
 */
export async function compilePendingItems(
  onProgress?: (message: string, current: number, total: number) => void,
  signal?: AbortSignal,
): Promise<WikiCompileRoundResult> {
  const [items, ingestions] = await Promise.all([
    window.api.wiki.listCompilable(),
    window.api.wiki.listIngestions(),
  ]);
  const ingestionByItem = new Map(
    ingestions.map((ingestion) => [ingestion.itemId, ingestion]),
  );

  // 指纹失效集：无指纹 / 素材变化 / 提示词版本升级
  // 素材 trim 后哈希：与旧版 .NET ContentHasher 口径一致，迁移来的指纹保持有效
  const now = Date.now();
  const pending: { item: WikiCompilableItem; hash: string }[] = [];
  for (const item of items) {
    const hash = await sha256Hex(buildMaterial(item).trim());
    const ingestion = ingestionByItem.get(item.id);
    const isStale =
      !ingestion ||
      ingestion.contentHash !== hash ||
      ingestion.promptVersion !== WIKI_COMPILE_PROMPT_VERSION;
    if (!isStale) {
      continue;
    }
    // 素材变了就重新给机会，否则遵守退避窗口与失败上限——
    // 一条模型始终解析不出来的条目，此前会每轮白烧两次调用，永不停止
    if (ingestion && ingestion.contentHash === hash) {
      if (ingestion.failureCount >= WIKI_COMPILE_MAX_FAILURES) {
        continue;
      }
      if (ingestion.nextAttemptAt !== null && ingestion.nextAttemptAt > now) {
        continue;
      }
    }
    pending.push({ item, hash });
  }

  if (pending.length === 0) {
    return { compiled: 0, pending: 0, skipped: 0 };
  }

  let compiled = 0;
  let skipped = 0;
  for (const { item, hash } of pending) {
    if (signal?.aborted) {
      break;
    }
    onProgress?.(item.title, compiled + skipped + 1, pending.length);
    const success = await compileSingleItem(item, hash, signal);
    if (success) {
      compiled++;
    } else {
      skipped++;
      await recordFailure(item.id, hash, ingestionByItem.get(item.id));
    }
  }
  return { compiled, pending: pending.length, skipped };
}

/** 失败落库并排下次重试：退避按失败次数指数增长，到上限后不再自动重试 */
async function recordFailure(
  itemId: string,
  hash: string,
  previous: { contentHash: string; failureCount: number } | undefined,
): Promise<void> {
  const attempts =
    previous && previous.contentHash === hash ? previous.failureCount + 1 : 1;
  const nextAttemptAt =
    attempts >= WIKI_COMPILE_MAX_FAILURES
      ? null
      : Date.now() +
        WIKI_COMPILE_RETRY_BASE_MS * Math.pow(4, Math.min(attempts - 1, 4));
  await window.api.wiki.recordCompilationFailure(itemId, hash, nextAttemptAt);
}

/** 单条目编译：候选检索 → LLM 生成（1 次纠错重试）→ 净化 → 链接清洗 → 落库。失败返回 false（跳过）。 */
async function compileSingleItem(
  item: WikiCompilableItem,
  materialHash: string,
  signal?: AbortSignal,
): Promise<boolean> {
  // 每条目重取目录：同轮前序条目新建的页面要参与本条目的候选与链接解析
  const catalog = await window.api.wiki.catalog();
  const material = buildMaterial(item);
  const ranked = rankCandidates(catalog, material);

  const catalogLines = ranked
    .slice(0, WIKI_COMPILE_CATALOG_LIMIT)
    .map(
      (candidate) =>
        `- ${candidate.entry.title}（${wikiKindLabel(candidate.entry.kind)}）：${candidate.entry.summary}`,
    );
  const contextIds = ranked
    .filter((candidate) => candidate.score > 0)
    .slice(0, WIKI_COMPILE_CONTEXT_PAGES_LIMIT)
    .map((candidate) => candidate.entry.id);
  const contextPages: { title: string; body: string }[] = [];
  // 只有进了这里的页面才允许被整体覆盖：模型没看到原文就重写，等于凭空编一份
  const contextPageIds: string[] = [];
  for (const id of contextIds) {
    const detail = await window.api.wiki.getPage(id);
    if (detail) {
      contextPages.push({ title: detail.page.title, body: detail.page.body });
      contextPageIds.push(detail.page.id);
    }
  }

  const basePrompt = buildWikiCompilePrompt(
    item.title,
    item.content,
    catalogLines,
    contextPages,
  );

  // LLM 生成 + 一次纠错重试（AiNotConfiguredError 等 Provider 级错误向上抛，中止本轮）
  let pages: RawPageDto[] | null = null;
  let model = "";
  let truncated = false;
  for (let attempt = 0; attempt <= 1; attempt++) {
    // 截断和格式错误是两种失败，纠错话术不能混：上一次是被 max_tokens 切断的，
    // 再喊一遍「只输出 JSON」毫无作用。半截 JSON 也不能拼接续写——模型不会
    // 逐 token 接上，只会重写并漂移，把截断问题变成脏数据问题。
    // 两个杠杆一起上：抬上限（provider 顶不住会自己 clamp），同时压需求
    // （少要几个页面是确定生效的那个）。
    const prompt =
      attempt === 0
        ? basePrompt
        : truncated
          ? `${basePrompt}\n\n注意：你上一次的输出太长，被截断在半途，JSON 不完整。这次请只输出 1~2 个页面，正文写得更精简，务必让 JSON 完整闭合。`
          : `${basePrompt}\n\n注意：你上一次的输出无法解析。请严格只输出要求格式的 JSON 对象，不要包含任何其他文字或代码块标记。`;
    const generation = await runScenarioChat(
      "wiki",
      [
        { role: "system", content: WIKI_COMPILE_SYSTEM_PROMPT },
        { role: "user", content: prompt },
      ],
      {
        temperature: 0.2,
        maxTokens: truncated
          ? WIKI_COMPILE_MAX_TOKENS * 2
          : WIKI_COMPILE_MAX_TOKENS,
        signal,
        timeoutMs: WIKI_COMPILE_TIMEOUT_MS,
      },
    );
    model = generation.model;
    truncated = generation.finishReason === "length";
    pages = parseWikiResponse(generation.content);
    if (pages && pages.length > 0) {
      break;
    }
  }
  if (!pages || pages.length === 0) {
    return false;
  }

  const batch = sanitizePages(pages);
  if (batch.length === 0) {
    return false;
  }

  const resolver = buildLinkResolver(catalog, batch);
  const compiledPages: WikiApplyCompilationInput["pages"] = batch.map(
    (draft) => {
      const { body, targets } = cleanWikiLinks(draft.body, resolver);
      return {
        title: draft.title,
        normalizedTitle: draft.normalizedTitle,
        kind: draft.kind,
        summary: draft.summary,
        body,
        aliasesJson: draft.aliasesJson,
        linkTargets: targets,
      };
    },
  );

  await window.api.wiki.applyCompilation({
    itemId: item.id,
    contentHash: materialHash,
    provider: "guizhi",
    model,
    promptVersion: WIKI_COMPILE_PROMPT_VERSION,
    contextPageIds,
    pages: compiledPages,
  });
  return true;
}
