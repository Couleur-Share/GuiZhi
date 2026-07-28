/**
 * MCP 两个工具的实现。
 *
 * 纯函数、只吃一个 db 句柄，不碰 stdio——这样单测可以直接调，
 * 不用把整个 JSON-RPC 通道搭起来。
 *
 * 输出是给模型读的紧凑文本而不是 JSON dump：条目正文本身就是自然语言，
 * 套一层 JSON 只是在浪费上下文，还得让模型自己反转义。
 */
import type DatabaseAdapter from "@guizhi/db/adapter";
import { CollectionDB, KnowledgeItemDB } from "@guizhi/db";
import { buildAiHandoff } from "@guizhi/shared/utils/ai-handoff";
import {
  isItemVisibleToMcp,
  isMcpScopeEmpty,
  type McpScope,
} from "@guizhi/shared/utils/mcp-scope";

const DEFAULT_LIMIT = 10;
const MAX_LIMIT = 50;
/** 列表里每条给多少字的正文预览。给多了几十条就能撑爆上下文 */
const SNIPPET_CHARS = 120;

export interface SearchInput {
  query: string;
  limit?: number;
  platform?: string;
  collection?: string;
}

function formatDate(timestamp: number): string {
  const date = new Date(timestamp);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

function collapseSnippet(text: string): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > SNIPPET_CHARS
    ? `${flat.slice(0, SNIPPET_CHARS)}…`
    : flat;
}

export function searchKnowledge(
  db: DatabaseAdapter.Database,
  input: SearchInput,
  scope: McpScope,
): string {
  const query = input.query?.trim();
  if (!query) {
    return "请给出检索关键词。";
  }
  // 「选了指定范围但一个都没勾」是有效配置，只是什么都搜不到。不点破的话
  // 用户看到的是「归知里明明有却搜不出来」，只会怀疑检索坏了
  if (isMcpScopeEmpty(scope)) {
    return "归知当前没有向 MCP 开放任何知识库。请在归知的「设置 → MCP 接入」里调整可访问范围。";
  }

  const allCollections = new CollectionDB(db).list();
  // 范围外的知识库连名字都不该出现在提示里
  const collections =
    scope.mode === "all"
      ? allCollections
      : allCollections.filter((collection) =>
          scope.allowedCollectionIds.includes(collection.id),
        );
  let collectionId: string | undefined;
  if (input.collection?.trim()) {
    const wanted = input.collection.trim().toLowerCase();
    const matched = collections.find(
      (collection) => collection.name.toLowerCase() === wanted,
    );
    if (!matched) {
      const names = collections.map((c) => c.name).join("、") || "（还没有知识库）";
      return `没有名为「${input.collection}」的知识库。现有：${names}`;
    }
    collectionId = matched.id;
  }

  const collectionNames = new Map(collections.map((c) => [c.id, c.name]));
  const limit = Math.max(1, Math.min(input.limit ?? DEFAULT_LIMIT, MAX_LIMIT));

  const result = new KnowledgeItemDB(db).list({
    scope: "all",
    search: query,
    // 调用方是模型，给进来的多半是词组甚至整句。默认的 phrase 会把中文长句
    // 编译成一个要求逐字连续出现的短语，必然零命中。
    searchMode: "recall",
    // 归档只是「处理完了」，不是移出知识库，检索时不该看不见
    includeArchived: true,
    collectionId,
    platform: input.platform?.trim() || undefined,
    collectionScope:
      scope.mode === "all"
        ? undefined
        : {
            ids: scope.allowedCollectionIds,
            includeUncategorized: scope.allowUncategorized,
          },
    limit,
  });

  if (result.entries.length === 0) {
    return `没有找到与「${query}」相关的条目。可以换个说法，或者把关键词拆得更短一些。`;
  }

  const lines = [
    `找到 ${result.total} 条与「${query}」相关的条目${
      result.total > result.entries.length ? `，下面列出前 ${result.entries.length} 条` : ""
    }：`,
    "",
  ];

  for (const [index, entry] of result.entries.entries()) {
    const meta = [
      `id=${entry.id}`,
      `type=${entry.itemType}`,
      entry.platform ? `platform=${entry.platform}` : "",
      entry.collectionId
        ? `collection=${collectionNames.get(entry.collectionId) ?? "?"}`
        : "",
      `updated=${formatDate(entry.updatedAt)}`,
    ]
      .filter(Boolean)
      .join("  ");

    lines.push(`${index + 1}. ${entry.title || "（无标题）"}`);
    lines.push(`   ${meta}`);
    const snippet = collapseSnippet(entry.snippet ?? "");
    if (snippet) {
      lines.push(`   ${snippet}`);
    }
    lines.push("");
  }

  lines.push(
    "用 read_item 工具加上面某条的 id，可以读到该条目的完整记录（视频/音频会附完整口播文字稿）。",
  );
  return lines.join("\n");
}

export interface ReadItemInput {
  id: string;
  includeFullText?: boolean;
}

export function readItem(
  db: DatabaseAdapter.Database,
  input: ReadItemInput,
  scope: McpScope,
): { text: string; found: boolean } {
  const id = input.id?.trim();
  if (!id) {
    return { text: "请给出条目 id。", found: false };
  }

  const item = new KnowledgeItemDB(db).get(id);
  if (!item) {
    return {
      text: `找不到 id 为 ${id} 的条目。它可能已被删除；用 search_knowledge 重新检索一次。`,
      found: false,
    };
  }
  // 明说「在范围外」而不是伪装成「不存在」：范围是用户自己在本机设的，
  // 含糊其辞只会让他以为条目丢了
  if (!isItemVisibleToMcp(scope, item.collectionId)) {
    return {
      text: `条目 ${id} 不在归知向 MCP 开放的范围内。可在归知的「设置 → MCP 接入」里调整可访问范围。`,
      found: false,
    };
  }

  const collectionName = item.collectionId
    ? (new CollectionDB(db).get(item.collectionId)?.name ?? null)
    : null;

  // 与详情页「复制给 AI」产出的是同一份东西：那段阅读须知（ASR 误差、
  // 无画面信息、素材与指令的边界）对这边的调用方同样必要
  const handoff = buildAiHandoff(
    { ...item, collectionName },
    { includeFullText: input.includeFullText !== false },
  );
  return { text: handoff.text, found: true };
}
