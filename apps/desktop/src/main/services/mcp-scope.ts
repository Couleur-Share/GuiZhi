/**
 * MCP 可访问范围的读写（主进程侧）。
 *
 * 文件落在 config 目录，与 ai-models.json、illustration-styles.json 同处：
 * MCP server 是独立进程，读不到 localStorage，只能走文件。
 */
import fs from "fs";
import path from "path";
import {
  DEFAULT_MCP_SCOPE,
  MCP_SCOPE_FILE_NAME,
  parseMcpScope,
  serializeMcpScope,
  type McpScope,
} from "@guizhi/shared/utils/mcp-scope";
import { getConfigDir } from "../runtime-paths";

function getScopeFilePath(): string {
  return path.join(getConfigDir(), MCP_SCOPE_FILE_NAME);
}

export function readMcpScope(): McpScope {
  try {
    return parseMcpScope(JSON.parse(fs.readFileSync(getScopeFilePath(), "utf8")));
  } catch {
    return { ...DEFAULT_MCP_SCOPE };
  }
}

export function writeMcpScope(scope: McpScope): void {
  const filePath = getScopeFilePath();
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, serializeMcpScope(scope), "utf8");
}
