/**
 * 各 AI 客户端的 MCP 接入片段。
 *
 * 格式差异比看上去大，而且踩错**一律静默失败**——这正是让归知代劳的理由：
 *
 * - Cursor 用 `mcpServers`（JSON），另有安装 deeplink，点一下就装。
 * - Codex 用 **TOML**，键是 snake_case 的 `mcp_servers`。写成 JSON 风格的
 *   `mcpServers` 整段被忽略，没有任何报错。
 * - `codex mcp add` 的 `--env` 排在服务器名**之后**、`--` 之前；而
 *   `claude mcp add` 恰好相反，flag 必须排在名字之前。两边抄串了都不报错。
 *
 * 纯函数、无 IO，两个进程都能用。
 */

/** 客户端里显示的服务器名。Codex 要求只含字母数字连字符下划线 */
export const MCP_SERVER_NAME = "guizhi";

export interface McpLaunchSpec {
  /** 可执行文件绝对路径（打包后是应用本体，以 ELECTRON_RUN_AS_NODE 跑） */
  command: string;
  args: string[];
  env: Record<string, string>;
}

/** 命令行参数：含空格就加引号，Windows 的 `C:\Program Files\...` 必然需要 */
function shellArg(value: string): string {
  return /[\s"]/.test(value) ? `"${value.replace(/"/g, '\\"')}"` : value;
}

/**
 * TOML 字符串。优先用单引号的字面量字符串——它不处理转义，
 * Windows 路径里的反斜杠可以原样写，比双引号里满屏的 `\\` 好读也不易错。
 * 值本身含单引号或换行时才回退到基本字符串。
 */
function tomlString(value: string): string {
  if (!value.includes("'") && !value.includes("\n")) {
    return `'${value}'`;
  }
  return JSON.stringify(value);
}

function tomlArray(values: string[]): string {
  return `[${values.map(tomlString).join(", ")}]`;
}

/** Cursor / Claude / Windsurf 共用的那个 server 配置对象 */
export function buildServerEntry(spec: McpLaunchSpec): Record<string, unknown> {
  return serverObject(spec);
}

function serverObject(spec: McpLaunchSpec): Record<string, unknown> {
  const value: Record<string, unknown> = {
    command: spec.command,
    args: spec.args,
  };
  if (Object.keys(spec.env).length > 0) {
    value.env = spec.env;
  }
  return value;
}

export function buildCursorConfig(spec: McpLaunchSpec): string {
  return `${JSON.stringify(
    { mcpServers: { [MCP_SERVER_NAME]: serverObject(spec) } },
    null,
    2,
  )}\n`;
}

export function buildCodexToml(spec: McpLaunchSpec): string {
  const lines = [
    `[mcp_servers.${MCP_SERVER_NAME}]`,
    `command = ${tomlString(spec.command)}`,
    `args = ${tomlArray(spec.args)}`,
  ];
  const envKeys = Object.keys(spec.env);
  if (envKeys.length > 0) {
    lines.push("", `[mcp_servers.${MCP_SERVER_NAME}.env]`);
    for (const key of envKeys) {
      lines.push(`${key} = ${tomlString(spec.env[key])}`);
    }
  }
  return `${lines.join("\n")}\n`;
}

export function buildCodexAddCommand(spec: McpLaunchSpec): string {
  const parts = ["codex", "mcp", "add", MCP_SERVER_NAME];
  for (const [key, value] of Object.entries(spec.env)) {
    parts.push("--env", `${key}=${value}`);
  }
  parts.push("--", shellArg(spec.command), ...spec.args.map(shellArg));
  return parts.join(" ");
}
