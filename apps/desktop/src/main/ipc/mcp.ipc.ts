/**
 * MCP server 接入信息。
 *
 * 只回一段配置数据，不启动任何东西——MCP server 是由 AI IDE 自己 spawn 的
 * 独立进程，归知这边只需要告诉用户「拿什么命令启动它」。
 */
import fs from "fs";
import path from "path";
import { app, ipcMain } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { McpServerConfig } from "@guizhi/shared/types";

function resolveServerPath(): string {
  // 打包后走 extraResources 落在 resources/mcp/ 下；开发时用 vite 的构建产物，
  // 两边算出来的都是能直接用的配置，不必特判环境
  return app.isPackaged
    ? path.join(process.resourcesPath, "mcp", "guizhi-mcp.mjs")
    : path.join(app.getAppPath(), "out", "mcp", "guizhi-mcp.mjs");
}

export function registerMcpIPC(): void {
  ipcMain.handle(IPC_CHANNELS.MCP_CONFIG, (): McpServerConfig => {
    const serverPath = resolveServerPath();
    return {
      available: fs.existsSync(serverPath),
      serverPath,
      // 用应用本体当 Node 运行时：用户机器上不必另装 Node
      command: process.execPath,
      args: [serverPath],
      env: { ELECTRON_RUN_AS_NODE: "1" },
    };
  });
}
