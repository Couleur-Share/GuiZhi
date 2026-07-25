/**
 * 知识库问答引擎（移植自 .NET 版 KnowledgeQaService）：
 * Agent 工具循环（search / read / answer 动作协议，检索即推理）优先，
 * 协议解析失败或纪律违规超限时自动回退单发检索管线（弱模型兜底）。
 *
 * 依赖以接口注入，便于单测用假实现驱动循环逻辑。
 * Wiki 页面检索随 M4 加入（当前 search 仅覆盖知识条目）。
 */
import type { WikiCatalogEntry, WikiPageDetail } from "@guizhi/shared/types";
import {
  QA_AGENT_SYSTEM_PROMPT,
  QA_SYSTEM_PROMPT,
  buildQaAgentPrompt,
  buildQaUserPrompt,
  truncateText,
  type QaTurn,
} from "./prompts";
import {
  normalizeWikiTitle,
  parseAliases,
  wikiKindLabel,
} from "./wiki-compile";

export type { QaTurn };

/** 引用资料的类别：知识条目原文 / AI 编译的 Wiki 页面。 */
export type QaSourceKind = "item" | "wiki";

export interface QaSourceRef {
  ordinal: number;
  kind: QaSourceKind;
  /** 条目 id 或 Wiki 页面 id（按 kind 区分） */
  refId: string;
  title: string;
}

export interface QaAnswer {
  text: string;
  sources: QaSourceRef[];
  model: string;
  /** 走了兜底管线（Agent 协议失败） */
  usedFallback: boolean;
}

export interface QaSearchHit {
  id: string;
  title: string;
  snippet: string;
}

export interface QaItemContent {
  title: string;
  content: string;
  transcript?: string | null;
}

export interface QaDeps {
  /** 已绑定 qa 场景配置的对话调用 */
  chat: (
    messages: { role: "system" | "user"; content: string }[],
    options: { temperature: number; maxTokens: number },
  ) => Promise<{ content: string; model: string }>;
  /** 知识库全文检索（含归档，排除回收站） */
  searchItems: (query: string, limit: number) => Promise<QaSearchHit[]>;
  /** 读取条目全文 */
  readItem: (id: string) => Promise<QaItemContent | null>;
  /** Wiki 目录（Agent 循环的页面匹配与出链解析；未提供时循环退化为纯条目检索） */
  getWikiCatalog?: () => Promise<WikiCatalogEntry[]>;
  /** 读取 Wiki 页面详情（含来源条目） */
  readWikiPage?: (id: string) => Promise<WikiPageDetail | null>;
}

// ---- Agent 循环预算 ----
const MAX_TOOL_ROUNDS = 6;
const MAX_PROTOCOL_VIOLATIONS = 2;
const TRANSCRIPT_BUDGET = 8000;
const ITEM_READ_LIMIT = 1500;
const WIKI_READ_LIMIT = 2000;
const SEARCH_ITEM_LIMIT = 5;
const SEARCH_WIKI_LIMIT = 4;
const LINKED_RESOURCE_LIMIT = 4;
const MAX_CONSECUTIVE_EMPTY_SEARCHES = 2;

// ---- 单发兜底管线预算 ----
const RETRIEVAL_LIMIT = 5;
const PER_SOURCE_CONTENT_LIMIT = 1500;
const TOTAL_SOURCE_CONTEXT_BUDGET = 6000;
const MIN_SOURCE_CHARS = 200;
const HISTORY_TURN_LIMIT = 3;
const HISTORY_ANSWER_LIMIT = 400;

const QA_TEMPERATURE = 0.2;
const QA_MAX_TOKENS = 1200;

/** 知识库没有相关资料（区别于调用失败，UI 显示引导文案）。 */
export class QaNoSourceError extends Error {
  constructor() {
    super("QA_NO_RELEVANT_SOURCE");
    this.name = "QaNoSourceError";
  }
}

type AgentAction =
  | { kind: "search"; query: string }
  | { kind: "read"; target: number }
  | { kind: "answer"; text: string };

/** 解析模型输出的动作 JSON（容忍围栏与前后缀文字；target 容忍字符串数字）。 */
export function tryParseAction(text: string): AgentAction | null {
  const start = text.indexOf("{");
  const end = text.lastIndexOf("}");
  if (start < 0 || end <= start) {
    return null;
  }

  let root: unknown;
  try {
    root = JSON.parse(text.slice(start, end + 1));
  } catch {
    return null;
  }
  if (!root || typeof root !== "object" || Array.isArray(root)) {
    return null;
  }

  const record = root as Record<string, unknown>;
  switch (String(record.action ?? "").trim().toLowerCase()) {
    case "search": {
      const query =
        typeof record.query === "string" ? record.query.trim() : "";
      return query ? { kind: "search", query } : null;
    }
    case "read": {
      if (typeof record.target === "number" && Number.isFinite(record.target)) {
        return { kind: "read", target: Math.trunc(record.target) };
      }
      if (typeof record.target === "string") {
        const digits = record.target.replace(/\D/g, "");
        if (digits) {
          return { kind: "read", target: parseInt(digits, 10) };
        }
      }
      return null;
    }
    case "answer": {
      return typeof record.text === "string"
        ? { kind: "answer", text: record.text }
        : null;
    }
    default:
      return null;
  }
}

/**
 * 从回答文本提取被引用的资料编号：匹配 [1] / 【2】 及复合标注（[1、3]、[1,2]），
 * 不在有效集合内的编号视为模型幻觉忽略。
 */
export function extractCitedOrdinals(
  answerText: string,
  validOrdinals: Set<number>,
): Set<number> {
  const cited = new Set<number>();
  const markerRegex = /[[【]\s*(\d{1,3}(?:\s*[,，、]\s*\d{1,3})*)\s*[\]】]/g;
  for (const match of answerText.matchAll(markerRegex)) {
    for (const token of match[1].split(/[,，、]/)) {
      const ordinal = parseInt(token.trim(), 10);
      if (Number.isFinite(ordinal) && validOrdinals.has(ordinal)) {
        cited.add(ordinal);
      }
    }
  }
  return cited;
}

interface AgentResource {
  ordinal: number;
  kind: QaSourceKind;
  refId: string;
  title: string;
  read: boolean;
}

function getOrAddResource(
  resources: AgentResource[],
  kind: QaSourceKind,
  refId: string,
  title: string,
): AgentResource {
  const existing = resources.find(
    (candidate) => candidate.kind === kind && candidate.refId === refId,
  );
  if (existing) {
    return existing;
  }
  const resource: AgentResource = {
    ordinal: resources.length + 1,
    kind,
    refId,
    title,
    read: false,
  };
  resources.push(resource);
  return resource;
}

/** Wiki 页面匹配：标题 / 别名 / 摘要与检索词双向包含计分（个人库规模内存匹配足够）。 */
export function matchWikiPages(
  catalog: WikiCatalogEntry[],
  query: string,
): WikiCatalogEntry[] {
  const trimmed = query.trim();
  if (!trimmed) {
    return [];
  }
  const queryLower = trimmed.toLowerCase();
  const tokens = trimmed.split(/\s+/).filter(Boolean);

  return catalog
    .map((entry) => {
      let score = 0;
      const titleLower = entry.title.toLowerCase();
      if (queryLower.includes(titleLower)) {
        score += 10;
      }
      for (const token of tokens) {
        const tokenLower = token.toLowerCase();
        if (titleLower.includes(tokenLower)) {
          score += 6;
        }
        if (entry.summary.toLowerCase().includes(tokenLower)) {
          score += 2;
        }
      }
      for (const alias of parseAliases(entry.aliasesJson)) {
        const aliasLower = alias.toLowerCase();
        if (queryLower.includes(aliasLower)) {
          score += 5;
        }
        for (const token of tokens) {
          if (aliasLower.includes(token.toLowerCase())) {
            score += 4;
          }
        }
      }
      return { entry, score };
    })
    .filter((candidate) => candidate.score > 0)
    .sort((left, right) => right.score - left.score)
    .map((candidate) => candidate.entry);
}

/** 解析页面正文里的 [[链接]] 并按目录（标题）定位目标页。 */
export function resolveBodyLinks(
  body: string,
  catalog: WikiCatalogEntry[],
): WikiCatalogEntry[] {
  const byNormalized = new Map<string, WikiCatalogEntry>();
  for (const entry of catalog) {
    if (!byNormalized.has(entry.normalizedTitle)) {
      byNormalized.set(entry.normalizedTitle, entry);
    }
  }

  const resolved: WikiCatalogEntry[] = [];
  for (const match of body.matchAll(/\[\[([^[\]]+)\]\]/g)) {
    const inner = match[1];
    const separatorIndex = inner.indexOf("|");
    const target = (
      separatorIndex >= 0 ? inner.slice(0, separatorIndex) : inner
    ).trim();
    if (!target) {
      continue;
    }
    const entry = byNormalized.get(normalizeWikiTitle(target));
    if (entry && !resolved.includes(entry)) {
      resolved.push(entry);
    }
  }
  return resolved;
}

function transcriptLength(transcript: string[]): number {
  return transcript.reduce((sum, entry) => sum + entry.length, 0);
}

function throwIfAborted(signal?: AbortSignal): void {
  if (signal?.aborted) {
    throw new DOMException("已取消", "AbortError");
  }
}

function buildItemText(item: QaItemContent): string {
  return item.transcript
    ? `${item.content}\n【口播转写稿】\n${item.transcript}`
    : item.content;
}

/** 组装检索文本：新问题 + 最近一轮的问题（追问上下文），提高弱问题的召回。 */
function buildRetrievalText(question: string, history?: QaTurn[]): string {
  const lastQuestion =
    history && history.length > 0 ? history[history.length - 1].question : "";
  return lastQuestion ? `${question} ${lastQuestion}` : question;
}

export async function askKnowledgeBase(
  question: string,
  history: QaTurn[] | undefined,
  deps: QaDeps,
  onStep?: (description: string) => void,
  signal?: AbortSignal,
): Promise<QaAnswer> {
  const trimmedQuestion = question.trim();
  if (!trimmedQuestion) {
    throw new Error("请输入问题");
  }

  // 历史回答截断：完整回答可达千余 token，挤占资料预算
  const trimmedHistory = history
    ?.slice(-HISTORY_TURN_LIMIT)
    .map((turn) => ({
      question: turn.question,
      answer: truncateText(turn.answer, HISTORY_ANSWER_LIMIT),
    }));

  const agentAnswer = await askByAgentLoop(
    trimmedQuestion,
    trimmedHistory,
    deps,
    onStep,
    signal,
  );
  if (agentAnswer) {
    return agentAnswer;
  }

  onStep?.("智能检索未成功，改用单次检索…");
  return askSingleShot(trimmedQuestion, trimmedHistory, deps, signal);
}

/** Agent 工具循环。返回 null 表示应回退单发管线（协议失败）。 */
async function askByAgentLoop(
  question: string,
  history: QaTurn[] | undefined,
  deps: QaDeps,
  onStep?: (description: string) => void,
  signal?: AbortSignal,
): Promise<QaAnswer | null> {
  // Wiki 目录取一次：编译知识参与检索与出链遍历（Wiki 为空时自然退化为条目检索）
  const catalog = deps.getWikiCatalog ? await deps.getWikiCatalog() : [];

  const resources: AgentResource[] = [];
  const transcript: string[] = [];
  let toolRounds = 0;
  let violations = 0;
  let consecutiveEmptySearches = 0;
  let forceAnswer = false;

  while (true) {
    throwIfAborted(signal);

    if (
      !forceAnswer &&
      (toolRounds >= MAX_TOOL_ROUNDS ||
        transcriptLength(transcript) > TRANSCRIPT_BUDGET)
    ) {
      forceAnswer = true;
      transcript.push(
        "[系统] 检索预算已用尽，请立即基于已阅读的资料给出最终回答（action=answer），资料不足就说明缺少什么。",
      );
    }

    const generation = await deps.chat(
      [
        { role: "system", content: QA_AGENT_SYSTEM_PROMPT },
        {
          role: "user",
          content: buildQaAgentPrompt(question, history, transcript),
        },
      ],
      { temperature: QA_TEMPERATURE, maxTokens: QA_MAX_TOKENS },
    );
    throwIfAborted(signal);

    const action = tryParseAction(generation.content);
    if (!action) {
      if (++violations > MAX_PROTOCOL_VIOLATIONS) {
        return null;
      }
      transcript.push(
        "[系统] 上一步输出无法解析。请严格只输出一个符合协议的 JSON 对象。",
      );
      continue;
    }

    if (forceAnswer && action.kind !== "answer") {
      if (++violations > MAX_PROTOCOL_VIOLATIONS) {
        return null;
      }
      transcript.push("[系统] 预算已用尽，只能给出最终回答（action=answer）。");
      continue;
    }

    switch (action.kind) {
      case "answer": {
        const readResources = resources.filter((resource) => resource.read);
        const text = action.text.trim();
        if (readResources.length === 0 || text.length === 0) {
          if (++violations > MAX_PROTOCOL_VIOLATIONS) {
            return null;
          }
          transcript.push(
            readResources.length === 0
              ? "[系统] 给出回答前必须至少阅读一份资料（先 search 再 read）。"
              : "[系统] 回答内容为空，请重新给出回答。",
          );
          continue;
        }

        const readOrdinals = new Set(
          readResources.map((resource) => resource.ordinal),
        );
        const cited = extractCitedOrdinals(text, readOrdinals);
        const chosen =
          cited.size > 0
            ? readResources.filter((resource) => cited.has(resource.ordinal))
            : readResources;
        return {
          text,
          sources: chosen.map((resource) => ({
            ordinal: resource.ordinal,
            kind: resource.kind,
            refId: resource.refId,
            title: resource.title,
          })),
          model: generation.model,
          usedFallback: false,
        };
      }

      case "search": {
        toolRounds++;
        onStep?.(`检索：${action.query}`);
        const lines: string[] = [];

        // Wiki 页面匹配在前（编译知识优先），条目融合检索在后
        for (const entry of matchWikiPages(catalog, action.query).slice(
          0,
          SEARCH_WIKI_LIMIT,
        )) {
          const resource = getOrAddResource(
            resources,
            "wiki",
            entry.id,
            entry.title,
          );
          lines.push(
            `[${resource.ordinal}]（Wiki·${wikiKindLabel(entry.kind)}）《${entry.title}》：${entry.summary}`,
          );
        }

        const hits = await deps.searchItems(action.query, SEARCH_ITEM_LIMIT);
        for (const hit of hits) {
          const resource = getOrAddResource(
            resources,
            "item",
            hit.id,
            hit.title,
          );
          const excerpt = truncateText(hit.snippet.replace(/\r?\n/g, " "), 80);
          lines.push(
            `[${resource.ordinal}]（条目）《${hit.title}》：${excerpt}`,
          );
        }

        if (lines.length === 0) {
          consecutiveEmptySearches++;
          transcript.push(
            `[工具] 搜索「${action.query}」无命中，请换一组关键词。`,
          );
          if (consecutiveEmptySearches >= MAX_CONSECUTIVE_EMPTY_SEARCHES) {
            transcript.push(
              "[系统] 已连续多次无命中：换一个角度检索，或基于已读资料回答 / 说明资料不足。",
            );
          }
        } else {
          consecutiveEmptySearches = 0;
          transcript.push(
            `[工具] 搜索「${action.query}」结果：\n${lines.join("\n")}`,
          );
        }
        break;
      }

      case "read": {
        toolRounds++;
        const resource = resources.find(
          (candidate) => candidate.ordinal === action.target,
        );
        if (!resource) {
          transcript.push(
            `[工具] 编号 ${action.target} 不存在，请使用搜索结果中出现过的编号。`,
          );
          break;
        }

        onStep?.(`阅读：${resource.title}`);
        const entry =
          resource.kind === "item"
            ? await readItemResource(deps, resource)
            : await readWikiResource(deps, resource, catalog, resources);
        transcript.push(entry);
        break;
      }
    }
  }
}

/** 读条目全文（视频/音频附转写稿）。 */
async function readItemResource(
  deps: QaDeps,
  resource: AgentResource,
): Promise<string> {
  const item = await deps.readItem(resource.refId);
  if (!item || !item.content.trim()) {
    return `[工具] [${resource.ordinal}]《${resource.title}》不存在或内容为空。`;
  }
  resource.read = true;
  return `[工具] [${resource.ordinal}]《${item.title}》内容：\n${truncateText(buildItemText(item), ITEM_READ_LIMIT)}`;
}

/**
 * 读 Wiki 页面，并把它的关联页面与来源条目注册为新的可读资料
 * （编译期物化的链接在查询期成为遍历线索——检索即推理的核心收益）。
 */
async function readWikiResource(
  deps: QaDeps,
  resource: AgentResource,
  catalog: WikiCatalogEntry[],
  resources: AgentResource[],
): Promise<string> {
  const detail = deps.readWikiPage
    ? await deps.readWikiPage(resource.refId)
    : null;
  if (!detail) {
    return `[工具] [${resource.ordinal}]《${resource.title}》页面已不存在。`;
  }

  resource.read = true;
  const parts: string[] = [
    `[工具] [${resource.ordinal}]《${detail.page.title}》页面内容：`,
    truncateText(detail.page.body, WIKI_READ_LIMIT),
  ];

  // 出链：从正文 [[目标|显示]] 解析并按目录定位
  const linkedEntries = resolveBodyLinks(detail.page.body, catalog)
    .filter((entry) => entry.id !== detail.page.id)
    .slice(0, LINKED_RESOURCE_LIMIT);
  if (linkedEntries.length > 0) {
    const labels = linkedEntries.map((entry) => {
      const linked = getOrAddResource(resources, "wiki", entry.id, entry.title);
      return `[${linked.ordinal}]《${entry.title}》`;
    });
    parts.push(`关联页面（可继续 read）：${labels.join("、")}`);
  }

  if (detail.sources.length > 0) {
    const labels = detail.sources
      .slice(0, LINKED_RESOURCE_LIMIT)
      .map((source) => {
        const linked = getOrAddResource(
          resources,
          "item",
          source.itemId,
          source.title,
        );
        return `[${linked.ordinal}]《${source.title}》`;
      });
    parts.push(`来源条目（原文，可继续 read）：${labels.join("、")}`);
  }

  return parts.join("\n").trimEnd();
}

/** 单发检索管线：宽召回 → 组装资料上下文 → 生成回答并按引用对齐来源。 */
async function askSingleShot(
  question: string,
  history: QaTurn[] | undefined,
  deps: QaDeps,
  signal?: AbortSignal,
): Promise<QaAnswer> {
  const hits = await deps.searchItems(
    buildRetrievalText(question, history),
    RETRIEVAL_LIMIT,
  );
  throwIfAborted(signal);
  if (hits.length === 0) {
    throw new QaNoSourceError();
  }

  // 取全文并组装资料上下文（打包到总预算：耗尽即停）
  const sources: QaSourceRef[] = [];
  const contextBlocks: string[] = [];
  let budgetRemaining = TOTAL_SOURCE_CONTEXT_BUDGET;
  let ordinal = 1;
  for (const hit of hits) {
    if (budgetRemaining < MIN_SOURCE_CHARS) {
      break;
    }
    const item = await deps.readItem(hit.id);
    if (!item || !item.content.trim()) {
      continue;
    }
    const content = truncateText(
      buildItemText(item),
      Math.min(PER_SOURCE_CONTENT_LIMIT, budgetRemaining),
    );
    budgetRemaining -= content.length;
    contextBlocks.push(`[${ordinal}] 《${item.title}》\n${content}`);
    sources.push({ ordinal, kind: "item", refId: hit.id, title: item.title });
    ordinal++;
  }
  throwIfAborted(signal);

  if (sources.length === 0) {
    throw new QaNoSourceError();
  }

  const generation = await deps.chat(
    [
      { role: "system", content: QA_SYSTEM_PROMPT },
      {
        role: "user",
        content: buildQaUserPrompt(question, contextBlocks, history),
      },
    ],
    { temperature: QA_TEMPERATURE, maxTokens: QA_MAX_TOKENS },
  );

  // 引用对齐：来源列表只保留回答实际标注引用的条目；未标注时退回完整列表保底可回溯
  const validOrdinals = new Set(sources.map((source) => source.ordinal));
  const citedOrdinals = extractCitedOrdinals(generation.content, validOrdinals);
  const citedSources =
    citedOrdinals.size > 0
      ? sources.filter((source) => citedOrdinals.has(source.ordinal))
      : sources;

  return {
    text: generation.content,
    sources: citedSources,
    model: generation.model,
    usedFallback: true,
  };
}

/** 生产环境依赖组装：检索走 FTS+语义混合，读取走 knowledge IPC，对话走 qa 场景模型。 */
export function createQaDeps(): QaDeps {
  return {
    chat: async (messages, options) => {
      const { runScenarioChat } = await import("./ai-invoke");
      return runScenarioChat("qa", messages, options);
    },
    searchItems: async (query, limit) => {
      const { hybridSearchItems } = await import("./hybrid-search");
      return hybridSearchItems(query, limit);
    },
    readItem: async (id) => {
      const item = await window.api.knowledge.get(id);
      if (!item) {
        return null;
      }
      return {
        title: item.title || "无标题",
        content: item.content,
        transcript: item.transcript,
      };
    },
    getWikiCatalog: () => window.api.wiki.catalog(),
    readWikiPage: (id) => window.api.wiki.getPage(id),
  };
}
