/**
 * 配置迁移 IPC：导出到文件、读取预览、应用导入。
 *
 * 读与应用刻意拆成两步：导入会覆盖本机全部设置，用户得先看清文件里有什么
 * （几个服务商、几个模型、带没带 Key）再决定，而不是选完文件就已经改完了。
 */
import fs from "fs";
import path from "path";
import { app, BrowserWindow, dialog, ipcMain } from "electron";
import type { IpcMainInvokeEvent } from "electron";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type {
  ConfigApplyResult,
  ConfigExportResult,
  ConfigReadResult,
  ConfigTransferFile,
} from "@guizhi/shared/types";
import {
  CONFIG_TRANSFER_KIND,
  CONFIG_TRANSFER_VERSION,
} from "@guizhi/shared/types";
import {
  buildConfigPreview,
  mapConfigSecrets,
  parseConfigTransferFile,
  pickTransferableLocalStorage,
  stripNonPortableSettings,
} from "@guizhi/shared/utils/config-transfer";
import {
  applyMainConfigParts,
  collectMainConfigParts,
  decryptConfigSecrets,
  encryptConfigSecrets,
  snapshotConfigDir,
} from "../services/config-transfer";
import { isPersistedSettingKey } from "./settings.ipc";
import { logAppError } from "../diagnostic-log";

export interface ConfigExportRequest {
  /** 渲染进程 guizhi-settings 的值快照 */
  settings: Record<string, unknown>;
  settingsVersion?: number;
  uiLayout?: Record<string, unknown>;
  includeSecrets: boolean;
  password?: string;
}

/**
 * 上一次经 `config:read` 选中的文件。
 *
 * apply 只接受这个路径：路径由渲染进程传回来，不校验的话就是一个可以读任意
 * 文件的口子。同一条理由下 backup.ipc.ts 的恢复也只认列表里查得到的文件名。
 */
let lastReadFilePath: string | null = null;

function ownerWindow(event: IpcMainInvokeEvent): BrowserWindow | undefined {
  return BrowserWindow.fromWebContents(event.sender) ?? undefined;
}

function formatFileStamp(date = new Date()): string {
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    `${date.getFullYear()}${pad(date.getMonth() + 1)}${pad(date.getDate())}` +
    `-${pad(date.getHours())}${pad(date.getMinutes())}${pad(date.getSeconds())}`
  );
}

function describeError(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function resolveDefaultExportPath(fileName: string): string {
  try {
    return path.join(app.getPath("downloads"), fileName);
  } catch {
    return fileName;
  }
}

function readTransferFile(filePath: string): {
  ok: boolean;
  file?: ConfigTransferFile;
  error?: string;
} {
  let raw: unknown;
  try {
    raw = JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch (error) {
    return { ok: false, error: `读取配置文件失败：${describeError(error)}` };
  }
  return parseConfigTransferFile(raw);
}

function buildExportFile(request: ConfigExportRequest): ConfigTransferFile {
  const mainParts = collectMainConfigParts();
  return {
    kind: CONFIG_TRANSFER_KIND,
    version: CONFIG_TRANSFER_VERSION,
    exportedAt: new Date().toISOString(),
    appVersion: app.getVersion(),
    settings: stripNonPortableSettings(request.settings ?? {}),
    settingsVersion: request.settingsVersion,
    uiLayout: pickTransferableLocalStorage(request.uiLayout),
    illustrationStyles: mainParts.illustrationStyles,
    shortcuts: mainParts.shortcuts,
  };
}

async function handleExport(
  event: IpcMainInvokeEvent,
  request: ConfigExportRequest,
): Promise<ConfigExportResult> {
  if (request?.includeSecrets && !request.password?.trim()) {
    return { success: false, error: "带 API Key 导出必须设置密码" };
  }

  let file: ConfigTransferFile;
  try {
    file = buildExportFile(request);
  } catch (error) {
    return { success: false, error: `采集配置失败：${describeError(error)}` };
  }

  if (request.includeSecrets) {
    encryptConfigSecrets(file, request.password!.trim());
  } else {
    // 清空而不是删除字段：结构保持完整，导入端照样按「有这个字段但是空的」处理，
    // 用户在设置页看到的就是一个等着填的空 Key 框
    mapConfigSecrets(file, () => "");
  }

  const defaultName = `GuiZhi-Config-${formatFileStamp()}.json`;
  const owner = ownerWindow(event);
  const options = {
    title: "导出归知配置",
    // 取不到下载目录时只给文件名，让系统自己决定落在哪儿——为了一个默认目录
    // 让整个导出功能不可用不值得
    defaultPath: resolveDefaultExportPath(defaultName),
    filters: [{ name: "归知配置", extensions: ["json"] }],
  };
  const result = owner
    ? await dialog.showSaveDialog(owner, options)
    : await dialog.showSaveDialog(options);
  if (result.canceled || !result.filePath) {
    return { success: false, canceled: true };
  }

  try {
    fs.writeFileSync(result.filePath, `${JSON.stringify(file, null, 2)}\n`, "utf8");
  } catch (error) {
    return { success: false, error: `写入配置文件失败：${describeError(error)}` };
  }
  return { success: true, filePath: result.filePath };
}

async function handleRead(
  event: IpcMainInvokeEvent,
): Promise<ConfigReadResult> {
  const owner = ownerWindow(event);
  const options = {
    title: "选择归知配置文件",
    properties: ["openFile" as const],
    filters: [{ name: "归知配置", extensions: ["json"] }],
  };
  const picked = owner
    ? await dialog.showOpenDialog(owner, options)
    : await dialog.showOpenDialog(options);
  if (picked.canceled || picked.filePaths.length === 0) {
    return { success: false, canceled: true };
  }

  const filePath = picked.filePaths[0];
  const parsed = readTransferFile(filePath);
  if (!parsed.ok || !parsed.file) {
    return { success: false, error: parsed.error };
  }

  lastReadFilePath = filePath;
  return {
    success: true,
    filePath,
    preview: buildConfigPreview(parsed.file),
  };
}

/** 从导入的设置里挑出主进程 settings 表也要存一份的键 */
function buildMainSyncSettings(
  settings: Record<string, unknown>,
): Record<string, unknown> {
  const next: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(settings)) {
    if (isPersistedSettingKey(key)) {
      next[key] = value;
    }
  }
  return next;
}

function handleApply(
  _event: IpcMainInvokeEvent,
  filePath: string,
  password?: string,
): ConfigApplyResult {
  if (!filePath || filePath !== lastReadFilePath) {
    return { success: false, error: "请重新选择要导入的配置文件" };
  }

  const parsed = readTransferFile(filePath);
  if (!parsed.ok || !parsed.file) {
    return { success: false, error: parsed.error };
  }
  const file = parsed.file;

  const decrypted = decryptConfigSecrets(file, password?.trim() ?? "");
  if (!decrypted.ok) {
    return {
      success: false,
      wrongPassword: decrypted.wrongPassword,
      error: decrypted.error,
    };
  }

  let snapshotDir: string | null = null;
  try {
    snapshotDir = snapshotConfigDir();
  } catch (error) {
    // 快照失败不该拦住导入，但要说出来——用户得知道这次没有后悔药
    console.warn("[config-transfer] 导入前快照失败:", error);
  }

  try {
    const applied = applyMainConfigParts(file);
    const settings = stripNonPortableSettings(file.settings ?? {});
    settings.aiProviders = applied.aiProviders;
    settings.aiModels = applied.aiModels;
    settings.modelRouteDefaults = applied.modelRouteDefaults;

    const warnings = [...applied.warnings];
    if (decrypted.failed) {
      warnings.push(`有 ${decrypted.failed} 处密钥解不开，需要手动重填`);
    }
    if (!snapshotDir) {
      warnings.push("导入前的配置快照没能保存");
    }

    return {
      success: true,
      settings,
      settingsVersion: file.settingsVersion,
      uiLayout: pickTransferableLocalStorage(file.uiLayout),
      mainSyncSettings: buildMainSyncSettings(settings),
      warnings,
      snapshotDir: snapshotDir ?? undefined,
    };
  } catch (error) {
    const reason = describeError(error);
    logAppError({
      scope: "config-transfer",
      action: "导入配置",
      message: reason,
      snapshotDir: snapshotDir ?? undefined,
    });
    return {
      success: false,
      error: snapshotDir
        ? `导入失败：${reason}。导入前的配置已备份在 ${snapshotDir}`
        : `导入失败：${reason}`,
      snapshotDir: snapshotDir ?? undefined,
    };
  }
}

export function registerConfigTransferIPC(): void {
  ipcMain.handle(IPC_CHANNELS.CONFIG_EXPORT, handleExport);
  ipcMain.handle(IPC_CHANNELS.CONFIG_READ, handleRead);
  ipcMain.handle(IPC_CHANNELS.CONFIG_APPLY, handleApply);
}
