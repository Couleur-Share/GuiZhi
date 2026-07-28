/**
 * MCP server 侧读取可访问范围。
 *
 * 每次工具调用前重读一次文件，不缓存：用户在归知界面上改完范围，期望的是
 * 立刻生效，而 MCP server 是被 IDE 拉起来后一直驻留的进程，缓存住就要等到
 * 下次重启 IDE 才生效——那种「改了没反应」最难自查。文件只有几百字节。
 */
import fs from "node:fs";
import path from "node:path";
import {
  DEFAULT_MCP_SCOPE,
  MCP_SCOPE_FILE_NAME,
  parseMcpScope,
  type McpScope,
} from "@guizhi/shared/utils/mcp-scope";

/**
 * 范围文件与 knowledge.db 同属一个 userData 目录，从库路径反推 config 目录，
 * 免得把那套四级路径解析再走一遍（两份实现迟早对不上）。
 */
export function resolveScopeFilePath(dbPath: string): string {
  return path.join(path.dirname(path.dirname(dbPath)), "config", MCP_SCOPE_FILE_NAME);
}

export function readMcpScope(dbPath: string): McpScope {
  const filePath = resolveScopeFilePath(dbPath);
  try {
    return parseMcpScope(JSON.parse(fs.readFileSync(filePath, "utf8")));
  } catch {
    // 文件不存在是常态（从没设过范围），读坏了也按「全部可见」走：
    // 静默搜不到东西比多看见几条难查得多，收紧只要在界面上点两下
    return { ...DEFAULT_MCP_SCOPE };
  }
}
