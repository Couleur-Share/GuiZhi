/**
 * MCP 接入信息与可访问范围。
 *
 * 不在这里启动任何东西——MCP server 是 AI IDE 自己 spawn 的独立进程，
 * 归知只负责告诉用户「拿什么命令启动它」以及「它能看见哪些知识库」。
 */
import fs from "fs";
import path from "path";
import { app, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { McpInstallResult, McpServerConfig } from "@guizhi/shared/types";
import { parseMcpScope, type McpScope } from "@guizhi/shared/utils/mcp-scope";
import { installIntoCursor } from "../services/mcp-install";
import { readMcpScope, writeMcpScope } from "../services/mcp-scope";

function resolveServerPath(): string {
  // 打包后走 extraResources 落在 resources/mcp/ 下；开发时用 vite 的构建产物，
  // 两边算出来的都是能直接用的配置，不必特判环境
  return app.isPackaged
    ? path.join(process.resourcesPath, "mcp", "guizhi-mcp.mjs")
    : path.join(app.getAppPath(), "out", "mcp", "guizhi-mcp.mjs");
}

function buildConfig(): McpServerConfig {
  const serverPath = resolveServerPath();
  return {
    available: fs.existsSync(serverPath),
    serverPath,
    // 用应用本体当 Node 运行时：用户机器上不必另装 Node
    command: process.execPath,
    args: [serverPath],
    env: { ELECTRON_RUN_AS_NODE: "1" },
  };
}

export function registerMcpIPC(): void {
  ipcMain.handle(IPC_CHANNELS.MCP_CONFIG, (): McpServerConfig => buildConfig());

  ipcMain.handle(IPC_CHANNELS.MCP_GET_SCOPE, (): McpScope => readMcpScope());

  ipcMain.handle(
    IPC_CHANNELS.MCP_SET_SCOPE,
    (_event, raw: unknown): McpScope => {
      // 入参来自渲染进程，过一遍同一个解析器：坏形状会被规整成安全的默认值，
      // 而不是原样写进一个 MCP server 也要读的文件
      const scope = parseMcpScope(raw);
      writeMcpScope(scope);
      return scope;
    },
  );

  /**
   * 一键安装：直接把归知合并进客户端的配置文件。
   *
   * 渲染进程只发一个客户端 id，路径与写入规则都在主进程——既不用把文件路径
   * 交给渲染进程，也不给它任何写任意文件的余地。
   */
  ipcMain.handle(
    IPC_CHANNELS.MCP_INSTALL,
    (_event, client: unknown): McpInstallResult => {
      if (client !== "cursor") {
        return { success: false, error: `不支持一键安装：${String(client)}` };
      }
      const config = buildConfig();
      if (!config.available) {
        return { success: false, error: "MCP server 组件不存在，请先完整构建一次" };
      }
      return installIntoCursor({
        command: config.command,
        args: config.args,
        env: config.env,
      });
    },
  );
}
