/**
 * 配置迁移的主进程侧：采集本进程持有的那部分设置、应用导入、以及应用前的快照。
 *
 * 分工：渲染进程的 localStorage 是 guizhi-settings（含 AI 服务商 / 模型 / 路由，
 * 且是 ai-models.json 的超集——多出 scenarioModelDefaults）的真相源，由它采集；
 * 配图风格与快捷键落在 config 目录下的 JSON 里，只有主进程读得到。
 */
import fs from "fs";
import path from "path";
import {
  coreAIConfigService,
  coreIllustrationStyleService,
  getConfigDir,
} from "@guizhi/core";
import type { CoreAIModelConfig } from "@guizhi/core";
import type {
  ConfigTransferFile,
  ConfigTransferShortcuts,
  IllustrationStyle,
} from "@guizhi/shared/types";
import {
  MCP_SCOPE_FILE_NAME,
  type McpScope,
} from "@guizhi/shared/utils/mcp-scope";
import {
  getCurrentShortcuts,
  getShortcutModes,
  persistImportedShortcuts,
} from "../../shortcuts";
import { readMcpScope, writeMcpScope } from "../mcp-scope";
import { reconcileImportedAiConfig } from "./ai-reconcile";

export { decryptConfigSecrets, encryptConfigSecrets } from "./config-crypto";
export type { ConfigDecryptResult } from "./config-crypto";
export { reconcileImportedAiConfig } from "./ai-reconcile";

/** 快照要拷的配置文件；不存在的跳过 */
const CONFIG_FILE_NAMES = [
  "ai-models.json",
  "illustration-styles.json",
  "shortcuts.json",
  "shortcut-mode.json",
  MCP_SCOPE_FILE_NAME,
];
const SNAPSHOT_DIR_PREFIX = "pre-import-";
const SNAPSHOT_KEEP_COUNT = 3;

function formatSnapshotStamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

function pruneSnapshots(configDir: string): void {
  try {
    const dirs = fs
      .readdirSync(configDir, { withFileTypes: true })
      .filter(
        (entry) => entry.isDirectory() && entry.name.startsWith(SNAPSHOT_DIR_PREFIX),
      )
      .map((entry) => entry.name)
      .sort()
      .reverse();
    for (const name of dirs.slice(SNAPSHOT_KEEP_COUNT)) {
      fs.rmSync(path.join(configDir, name), { recursive: true, force: true });
    }
  } catch (error) {
    console.warn("[config-transfer] 清理旧快照失败:", error);
  }
}

/**
 * 应用导入前把 config 目录下的配置文件拷一份。
 *
 * 导错一份文件能把 API Key 一次性弄没，而 Key 是这堆东西里唯一重配代价高的。
 * 快照就落在同一个 config 目录下：那些文件本来就是明文，拷一份不改变任何安全
 * 态势，却让「导错了」从不可逆变成可恢复。
 */
export function snapshotConfigDir(): string | null {
  const configDir = getConfigDir();
  const present = CONFIG_FILE_NAMES.filter((name) =>
    fs.existsSync(path.join(configDir, name)),
  );
  if (present.length === 0) {
    return null;
  }

  const targetDir = path.join(
    configDir,
    `${SNAPSHOT_DIR_PREFIX}${formatSnapshotStamp()}`,
  );
  fs.mkdirSync(targetDir, { recursive: true });
  for (const name of present) {
    fs.copyFileSync(path.join(configDir, name), path.join(targetDir, name));
  }
  pruneSnapshots(configDir);
  return targetDir;
}

export interface MainConfigParts {
  illustrationStyles: IllustrationStyle[];
  shortcuts: ConfigTransferShortcuts;
  mcpScope: McpScope;
}

/** 采集只有主进程读得到的那部分设置 */
export function collectMainConfigParts(): MainConfigParts {
  let illustrationStyles: IllustrationStyle[] = [];
  try {
    illustrationStyles = coreIllustrationStyleService.read();
  } catch (error) {
    console.warn("[config-transfer] 读取配图风格失败，导出将不含风格:", error);
  }

  return {
    illustrationStyles,
    shortcuts: {
      accelerators: { ...getCurrentShortcuts() },
      modes: { ...getShortcutModes() },
    },
    // 没有 mcp.json 时回默认的「全部可见」，那正是本机此刻的真实状态
    mcpScope: readMcpScope(),
  };
}

export interface ApplyMainConfigResult {
  /** 对账后的最终 AI 配置，渲染进程用它覆盖 localStorage，两侧才不会打架 */
  aiProviders: unknown[];
  aiModels: CoreAIModelConfig[];
  modelRouteDefaults: Record<string, string>;
  warnings: string[];
}

/**
 * 把导入文件里属于主进程的部分落盘。
 *
 * AI 配置失败视为整次导入失败（它是这个功能的全部价值），风格与快捷键失败只记
 * warning——少一套风格不影响用，为它整份作废不划算。
 */
export function applyMainConfigParts(
  file: ConfigTransferFile,
): ApplyMainConfigResult {
  const settings = (file.settings ?? {}) as Record<string, unknown>;
  const reconciled = reconcileImportedAiConfig(
    {
      providers: settings.aiProviders,
      models: settings.aiModels,
      routes: settings.modelRouteDefaults,
    },
    coreAIConfigService.read(),
  );

  coreAIConfigService.replace({
    providers: reconciled.providers,
    models: reconciled.models,
    modelRouteDefaults: reconciled.routes,
  });

  const warnings = [...reconciled.warnings];

  if (Array.isArray(file.illustrationStyles) && file.illustrationStyles.length > 0) {
    const result = coreIllustrationStyleService.write(file.illustrationStyles);
    if (!result.success) {
      warnings.push(`配图风格未能导入：${result.error ?? "未知原因"}`);
    }
  }

  if (file.shortcuts) {
    try {
      persistImportedShortcuts(
        file.shortcuts.accelerators,
        file.shortcuts.modes,
      );
    } catch (error) {
      warnings.push(
        `快捷键未能导入：${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  // 只在文件真带了范围时才写：旧版本导出的文件没有这个字段，本机现有范围保持原样
  if (file.mcpScope) {
    try {
      writeMcpScope(file.mcpScope);
      if (file.mcpScope.mode === "selected") {
        // 范围存的是知识库 id，这台机器若还没恢复备份，勾中的库可能一个都不存在，
        // 表现是 MCP 什么都搜不到——不说出来没人会想到去设置页核对
        warnings.push(
          `MCP 可访问范围已收紧到 ${file.mcpScope.allowedCollectionIds.length} 个知识库，请在「设置 → MCP 接入」确认这些知识库在本机存在`,
        );
      }
    } catch (error) {
      warnings.push(
        `MCP 可访问范围未能导入：${error instanceof Error ? error.message : String(error)}`,
      );
    }
  }

  return {
    aiProviders: reconciled.providers,
    aiModels: reconciled.models,
    modelRouteDefaults: reconciled.routes as Record<string, string>,
    warnings,
  };
}
