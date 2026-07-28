/**
 * 把归知写进 Cursor 的 MCP 配置文件。
 *
 * 一开始走的是 Cursor 的安装 deeplink
 * （`cursor://anysphere.cursor-deeplink/mcp/install?name=…&config=…`），
 * 实测点下去只把 Cursor 的 MCP 设置页调到前台、不弹安装确认框。格式对得上
 * 官方示例（config 是裸的 server 对象、base64、不 URL 转义），但那条路上叠了
 * 三个不受我们控制的变量：Cursor 的 deeplink 实现社区报过多次失效、
 * `shell.openExternal` 到协议 handler 的传参、以及新版正在把 MCP 迁进
 * 「Customize」。与其赌它，不如直接写文件——路径、格式、合并规则全在我们手里。
 *
 * Codex 那边保持给 `codex mcp add` 命令：那是官方 CLI，比我们代写 TOML
 * （还要保住注释与格式）可靠。
 */
import fs from "fs";
import os from "os";
import path from "path";
import { applyEdits, modify, parse, type ParseError } from "jsonc-parser";
import {
  buildServerEntry,
  MCP_SERVER_NAME,
  type McpLaunchSpec,
} from "@guizhi/shared/utils/mcp-clients";

export interface McpInstallOutcome {
  success: boolean;
  /** 实际写入的配置文件 */
  filePath?: string;
  /** 覆盖前留下的备份 */
  backupPath?: string;
  /** true = 之前就有一份同名配置，这次是更新（换了安装位置时的常态） */
  replaced?: boolean;
  error?: string;
}

const FORMATTING = { formattingOptions: { insertSpaces: true, tabSize: 2 } };

export function getCursorConfigPath(): string {
  return path.join(os.homedir(), ".cursor", "mcp.json");
}

/**
 * 合并进 Cursor 的 mcp.json。
 *
 * 用 jsonc 的 modify/applyEdits 而不是 parse → 改 → stringify：后者会把用户
 * 手写的注释和排版全部冲掉，而这是个人人都会手改的文件。
 */
export function installIntoCursor(spec: McpLaunchSpec): McpInstallOutcome {
  return installIntoCursorAt(getCursorConfigPath(), spec);
}

/** 供测试指定落点；对外只暴露不带路径的那个，渲染进程碰不到文件路径 */
export function installIntoCursorAt(
  filePath: string,
  spec: McpLaunchSpec,
): McpInstallOutcome {
  const entry = buildServerEntry(spec);

  let original = "";
  let exists: boolean;
  try {
    original = fs.readFileSync(filePath, "utf8");
    exists = true;
  } catch {
    exists = false;
  }

  // 空文件（客户端建了但还没写过）按空对象处理：拿它去 parse 会报「不是合法
  // JSON」，而那句提示对着一个空文件说出来只会让人一头雾水
  const hasContent = original.trim().length > 0;

  let replaced = false;
  if (exists && hasContent) {
    // 文件坏了就住手。这里面可能躺着用户配了很久的一堆 server，
    // 覆盖掉换来的是「一键安装顺便清空了我的 MCP」
    const errors: ParseError[] = [];
    const parsed = parse(original, errors, {
      allowTrailingComma: true,
      disallowComments: false,
    });
    if (errors.length > 0) {
      return {
        success: false,
        filePath,
        error: `${filePath} 不是合法的 JSON，为免弄丢已有配置没有改动它。请手工修好，或用下面的配置片段自行添加。`,
      };
    }
    replaced = Boolean(
      parsed && typeof parsed === "object" && parsed.mcpServers?.[MCP_SERVER_NAME],
    );
  }

  const source = hasContent ? original : "{}";
  const next = applyEdits(
    source,
    modify(source, ["mcpServers", MCP_SERVER_NAME], entry, FORMATTING),
  );

  let backupPath: string | undefined;
  try {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    if (exists) {
      // 单份、每次覆盖：这个操作是幂等的，留最近一次就够回退
      backupPath = `${filePath}.guizhi-backup`;
      fs.writeFileSync(backupPath, original, "utf8");
    }
    fs.writeFileSync(filePath, next.endsWith("\n") ? next : `${next}\n`, "utf8");
  } catch (error) {
    return {
      success: false,
      filePath,
      error: error instanceof Error ? error.message : String(error),
    };
  }

  return { success: true, filePath, backupPath, replaced };
}
