/**
 * MCP 可访问范围：限制外部 AI 工具能看见哪些知识库。
 *
 * 知识库里可能有私人日记、体检报告这类并不想让 IDE 里的 AI 读到的东西，
 * 而 MCP 默认是整库可见。这份配置落在 `<userData>/config/mcp.json`——不能放
 * localStorage，MCP server 是独立进程，读不到。
 *
 * 纯函数、无 IO：主进程负责写，MCP server 每次工具调用前读一次
 * （用户在归知里改完范围应当立刻生效，而 MCP server 是长驻进程）。
 */

export const MCP_SCOPE_FILE_NAME = "mcp.json";
export const MCP_SCOPE_VERSION = 1;

export type McpCollectionScopeMode = "all" | "selected";

export interface McpScope {
  mode: McpCollectionScopeMode;
  /** mode = selected 时生效；存 id 而非名字，改名不该让范围失效 */
  allowedCollectionIds: string[];
  /**
   * 未分类条目（collection_id IS NULL）算不算在内。
   *
   * 单列一个开关而不是塞进 id 列表里用哨兵值：归知的「未分类」是待整理队列，
   * 数量往往比任何一个知识库都多，混进 id 数组会让「清空选择」这类操作
   * 顺手把它一起带走，而用户根本没在列表里看见过它。
   */
  allowUncategorized: boolean;
}

export const DEFAULT_MCP_SCOPE: McpScope = {
  mode: "all",
  allowedCollectionIds: [],
  allowUncategorized: true,
};

/**
 * 解析配置文件内容。坏数据一律退回「全部可见」而不是「全部不可见」：
 * 这个文件是用户手改得到的，改坏了让 MCP 静默搜不到任何东西，
 * 排查起来比多看见几条难得多。真要收紧，界面上点两下就行。
 */
export function parseMcpScope(raw: unknown): McpScope {
  if (!raw || typeof raw !== "object") {
    return { ...DEFAULT_MCP_SCOPE };
  }
  const source = raw as Record<string, unknown>;
  const mode: McpCollectionScopeMode =
    source.mode === "selected" ? "selected" : "all";
  const ids = Array.isArray(source.allowedCollectionIds)
    ? source.allowedCollectionIds.filter(
        (value): value is string => typeof value === "string" && value !== "",
      )
    : [];
  return {
    mode,
    allowedCollectionIds: [...new Set(ids)],
    allowUncategorized:
      typeof source.allowUncategorized === "boolean"
        ? source.allowUncategorized
        : true,
  };
}

export function serializeMcpScope(scope: McpScope): string {
  return `${JSON.stringify(
    {
      version: MCP_SCOPE_VERSION,
      mode: scope.mode,
      allowedCollectionIds: scope.allowedCollectionIds,
      allowUncategorized: scope.allowUncategorized,
    },
    null,
    2,
  )}\n`;
}

/** 单条目是否可见。mode = all 时一切放行，连未分类开关都不看 */
export function isItemVisibleToMcp(
  scope: McpScope,
  collectionId: string | null | undefined,
): boolean {
  if (scope.mode === "all") {
    return true;
  }
  if (!collectionId) {
    return scope.allowUncategorized;
  }
  return scope.allowedCollectionIds.includes(collectionId);
}

/**
 * mode = selected 且什么都没选中：这不是「没设置」，是「一条都不给看」。
 * 界面要据此提醒，否则用户只会看到 MCP 突然搜不到东西。
 */
export function isMcpScopeEmpty(scope: McpScope): boolean {
  return (
    scope.mode === "selected" &&
    scope.allowedCollectionIds.length === 0 &&
    !scope.allowUncategorized
  );
}
