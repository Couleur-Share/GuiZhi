/**
 * 配置迁移文件（一键导入 / 导出全部软件设置）。
 *
 * 与备份的分工：备份是整库 `VACUUM INTO`，装的是知识条目；这里装的是设置，
 * 两者互不覆盖。换新设备的完整路径是「恢复一份备份 + 导入一份配置」。
 *
 * 机密（API Key、代理密码）用传输密码加密后落进 `ENC::` 字段，其余部分保持
 * 明文——文件因此仍然可读：打开就能看到里面有哪些服务商、哪些模型、什么路由。
 * 忘了密码不是灾难，其余配置照常导入，只有 Key 需要手填。
 */

import type { McpScope } from "../utils/mcp-scope";
import type { IllustrationStyle } from "./illustration";

export const CONFIG_TRANSFER_KIND = "guizhi-config";
export const CONFIG_TRANSFER_VERSION = 1;

/**
 * 加密块里那段固定明文的密文。
 *
 * 导入时先解它：密码错了当场说清楚，而不是让一堆解不开的密文被当成 Key
 * 写进配置——那种失败要到用户下一次调模型时才暴露，且看不出根因。
 */
export const CONFIG_TRANSFER_CANARY = "guizhi-config-transfer";

export interface ConfigTransferEncryption {
  algo: "aes-256-gcm";
  kdf: "scrypt";
  /** base64，16 字节 */
  salt: string;
  /** scrypt 代价参数，随文件走，导入端不必猜导出端用的是哪版实现 */
  n: number;
  r: number;
  p: number;
  canary: string;
}

export interface ConfigTransferShortcuts {
  accelerators: Record<string, string>;
  modes: Record<string, "global" | "local">;
}

export interface ConfigTransferFile {
  kind: string;
  version: number;
  exportedAt: string;
  /** 导出方的应用版本，只用于界面展示与排查 */
  appVersion: string;
  /** 缺省表示文件里没有任何机密字段 */
  encryption?: ConfigTransferEncryption;
  /** guizhi-settings 的值快照，已剔除机器绑定字段 */
  settings: Record<string, unknown>;
  /** 导出方 settings store 的持久化版本号，回写 localStorage 时原样带上 */
  settingsVersion?: number;
  /** 界面偏好类 localStorage，key → 解析后的原始值（按白名单收敛） */
  uiLayout?: Record<string, unknown>;
  illustrationStyles?: IllustrationStyle[];
  shortcuts?: ConfigTransferShortcuts;
  /**
   * MCP 可访问范围。
   *
   * 缺省与「明确写了全部可见」必须分开：前者是旧版本导出的文件压根没有这个
   * 字段，导入时不该动本机的设置——把它按默认值处理，等于一份旧配置就能把
   * 用户收紧过的范围悄悄放开，那正是这个字段要防的事。
   */
  mcpScope?: McpScope;
}

/** 应用前给用户看的摘要：这份文件里到底有什么 */
export interface ConfigTransferPreview {
  exportedAt: string;
  appVersion: string;
  encrypted: boolean;
  providerCount: number;
  modelCount: number;
  routeCount: number;
  styleCount: number;
  shortcutCount: number;
  uiLayoutKeyCount: number;
  /** 缺省表示这份文件没带 MCP 范围，导入后本机现有范围保持不变 */
  mcpScope?: {
    mode: McpScope["mode"];
    collectionCount: number;
    allowUncategorized: boolean;
  };
}

export interface ConfigExportResult {
  success: boolean;
  canceled?: boolean;
  filePath?: string;
  error?: string;
}

export interface ConfigReadResult {
  success: boolean;
  canceled?: boolean;
  filePath?: string;
  preview?: ConfigTransferPreview;
  error?: string;
}

export interface ConfigApplyResult {
  success: boolean;
  /** 密码错误单独标记：用户改一下就能过，与「文件坏了」不是一回事 */
  wrongPassword?: boolean;
  /** 渲染进程要写回 localStorage 的最终设置（AI 列表已与本机内置引擎对账） */
  settings?: Record<string, unknown>;
  settingsVersion?: number;
  uiLayout?: Record<string, unknown>;
  /** 转交 settings:set 的白名单子集（不含 AI 键，避免二次触发整份替换） */
  mainSyncSettings?: Record<string, unknown>;
  /** 逐条说明哪些部分没能完整还原 */
  warnings?: string[];
  /** 应用前留下的配置快照目录 */
  snapshotDir?: string;
  error?: string;
}
