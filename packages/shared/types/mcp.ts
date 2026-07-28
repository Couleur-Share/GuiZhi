/**
 * MCP server 接入信息。
 *
 * 归知随包发一个 stdio MCP server，让 Cursor / Codex 这类 AI IDE 直接检索
 * 并读取知识库。它由应用本体以 ELECTRON_RUN_AS_NODE 模式启动，所以用户机器
 * 上不需要另装 Node，代价是路径写死——应用换了安装位置就得重新复制一次配置。
 */
export interface McpInstallResult {
  success: boolean;
  /** 实际写入的客户端配置文件 */
  filePath?: string;
  /** 覆盖前留下的备份 */
  backupPath?: string;
  /** true = 之前就有一份同名配置，这次是更新（换了安装位置时的常态） */
  replaced?: boolean;
  error?: string;
}

export interface McpServerConfig {
  /** 产物是否真的在（构建产物缺失时界面要说清楚，而不是给一段跑不起来的配置） */
  available: boolean;
  /** MCP server 脚本的绝对路径 */
  serverPath: string;
  command: string;
  args: string[];
  env: Record<string, string>;
}
