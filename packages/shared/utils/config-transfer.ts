/**
 * 配置迁移的协议层：机密字段遍历、不可移植字段剔除、信封校验与预览。
 *
 * 放在 shared 是因为主进程要用它加解密、渲染进程要用它收集与预览、单测要用它
 * 对着同一份定义验证——这三处对「哪些字段是机密」的理解必须一模一样。
 */

import type {
  ConfigTransferEncryption,
  ConfigTransferFile,
  ConfigTransferPreview,
} from "../types/config-transfer";
import {
  CONFIG_TRANSFER_KIND,
  CONFIG_TRANSFER_VERSION,
} from "../types/config-transfer";
import { parseMcpScope } from "./mcp-scope";

/**
 * 与本机绑定、导过去只会坏事的设置字段。
 *
 * 工具路径是绝对路径，而且主进程只接受文件选择器当场登记过的值
 * （`isAcceptableBinaryPath`），导进去必然被拒——留着它只会让人以为
 * yt-dlp 配好了。背景图字段存的是文件名，图片实体在 data/assets/images 下，
 * 不随配置走，导过去是一张破图。开机自启要写系统注册表，是逐台设备的决定。
 */
export const NON_PORTABLE_SETTINGS_KEYS = [
  "dataPath",
  "ytDlpPath",
  "ffmpegPath",
  "launchAtStartup",
  "backgroundImageFileName",
  "settingsUpdatedAt",
  "isDarkMode",
] as const;

/**
 * 随配置走的界面偏好 localStorage。
 *
 * 白名单而非全量拷贝：导入是照着文件往 localStorage 写，不收敛的话一份构造过的
 * 配置文件就能往里塞任意键。`guizhi-illustration-style-by-collection` 不在其中
 * ——它按知识库 id 记忆，换台机器指向的是不存在的库。
 */
export const TRANSFER_LOCAL_STORAGE_KEYS = [
  "ui-storage",
  "guizhi-library-table-config",
  "guizhi-table-config",
] as const;

/** 传入机密明文（或密文），返回转换结果；返回 null 视为失败 */
export type SecretTransform = (value: string) => string | null;

export interface SecretMapStats {
  processed: number;
  failed: number;
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function asRecordArray(value: unknown): Record<string, unknown>[] {
  return Array.isArray(value) ? value.filter(isPlainRecord) : [];
}

function transformField(
  holder: Record<string, unknown> | undefined,
  key: string,
  transform: SecretTransform,
  stats: SecretMapStats,
): void {
  if (!holder) {
    return;
  }
  const current = holder[key];
  if (typeof current !== "string" || current.length === 0) {
    return;
  }
  const next = transform(current);
  if (next === null) {
    stats.failed += 1;
    return;
  }
  holder[key] = next;
  stats.processed += 1;
}

/** 逐个访问文件里的机密字段（只读遍历，供计数与测试用） */
function forEachSecretHolder(
  file: ConfigTransferFile,
  visit: (holder: Record<string, unknown>, key: string) => void,
): void {
  const settings = isPlainRecord(file.settings) ? file.settings : undefined;
  if (!settings) {
    return;
  }
  visit(settings, "aiApiKey");
  for (const provider of asRecordArray(settings.aiProviders)) {
    visit(provider, "apiKey");
  }
  for (const model of asRecordArray(settings.aiModels)) {
    visit(model, "apiKey");
  }
  if (isPlainRecord(settings.networkProxy)) {
    visit(settings.networkProxy, "password");
  }
}

/**
 * 就地替换文件里的全部机密字段。
 *
 * 导出加密与导入解密共用这一个遍历器。漏掉一处，导出那边是 Key 明文躺在文件里，
 * 导入那边是解不开的密文被当成 Key 写进配置——两种都不报错，只会在很久以后
 * 表现为「这个模型怎么用不了」。
 */
export function mapConfigSecrets(
  file: ConfigTransferFile,
  transform: SecretTransform,
): SecretMapStats {
  const stats: SecretMapStats = { processed: 0, failed: 0 };
  forEachSecretHolder(file, (holder, key) => {
    transformField(holder, key, transform, stats);
  });
  return stats;
}

/** 文件里有几处非空机密字段 */
export function countConfigSecrets(file: ConfigTransferFile): number {
  let count = 0;
  forEachSecretHolder(file, (holder, key) => {
    if (typeof holder[key] === "string" && (holder[key] as string).length > 0) {
      count += 1;
    }
  });
  return count;
}

/** 剔除机器绑定字段，返回新对象 */
export function stripNonPortableSettings(
  values: Record<string, unknown>,
): Record<string, unknown> {
  const blocked = new Set<string>(NON_PORTABLE_SETTINGS_KEYS);
  const next: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(values)) {
    if (blocked.has(key) || typeof value === "function") {
      continue;
    }
    next[key] = value;
  }
  return next;
}

/** 按白名单收敛界面偏好，导出与导入两个方向都过一遍 */
export function pickTransferableLocalStorage(
  entries: Record<string, unknown> | undefined,
): Record<string, unknown> {
  const allowed = new Set<string>(TRANSFER_LOCAL_STORAGE_KEYS);
  const next: Record<string, unknown> = {};
  if (!isPlainRecord(entries)) {
    return next;
  }
  for (const [key, value] of Object.entries(entries)) {
    if (allowed.has(key) && value !== undefined) {
      next[key] = value;
    }
  }
  return next;
}

/**
 * 校验加密块。
 *
 * scrypt 的代价参数来自文件，等于让文件决定我们分配多少内存、算多久——
 * 所以要卡在合理区间内，构造过的文件不能靠一个巨大的 N 把主进程拖死。
 */
function parseEncryption(raw: unknown): {
  ok: boolean;
  encryption?: ConfigTransferEncryption;
  error?: string;
} {
  if (raw === undefined || raw === null) {
    return { ok: true };
  }
  if (!isPlainRecord(raw)) {
    return { ok: false, error: "加密信息格式不正确" };
  }
  const { algo, kdf, salt, n, r, p, canary } = raw as Record<string, unknown>;
  if (algo !== "aes-256-gcm" || kdf !== "scrypt") {
    return { ok: false, error: "不支持的加密方式" };
  }
  if (typeof salt !== "string" || !salt || typeof canary !== "string" || !canary) {
    return { ok: false, error: "加密信息不完整" };
  }
  const isPowerOfTwo = (value: unknown): value is number =>
    typeof value === "number" &&
    Number.isInteger(value) &&
    value > 0 &&
    (value & (value - 1)) === 0;
  if (!isPowerOfTwo(n) || n < 4096 || n > 65536) {
    return { ok: false, error: "加密参数超出支持范围" };
  }
  if (
    typeof r !== "number" ||
    !Number.isInteger(r) ||
    r < 1 ||
    r > 8 ||
    typeof p !== "number" ||
    !Number.isInteger(p) ||
    p < 1 ||
    p > 4
  ) {
    return { ok: false, error: "加密参数超出支持范围" };
  }
  return {
    ok: true,
    encryption: { algo, kdf, salt, n, r, p, canary },
  };
}

export interface ConfigTransferParseResult {
  ok: boolean;
  file?: ConfigTransferFile;
  error?: string;
}

/** 校验信封并收敛可选字段；更高版本一律拒绝（理由同备份恢复） */
export function parseConfigTransferFile(
  raw: unknown,
): ConfigTransferParseResult {
  if (!isPlainRecord(raw)) {
    return { ok: false, error: "这不是一份归知配置文件" };
  }
  if (raw.kind !== CONFIG_TRANSFER_KIND) {
    return { ok: false, error: "这不是一份归知配置文件" };
  }
  const version = raw.version;
  if (typeof version !== "number" || !Number.isInteger(version) || version < 1) {
    return { ok: false, error: "配置文件缺少有效的版本号" };
  }
  if (version > CONFIG_TRANSFER_VERSION) {
    return {
      ok: false,
      error: `配置文件来自更新版本的归知（格式 v${version}，当前支持 v${CONFIG_TRANSFER_VERSION}），请先升级应用`,
    };
  }
  if (!isPlainRecord(raw.settings)) {
    return { ok: false, error: "配置文件里没有可用的设置内容" };
  }

  const encryption = parseEncryption(raw.encryption);
  if (!encryption.ok) {
    return { ok: false, error: encryption.error };
  }

  const shortcuts = isPlainRecord(raw.shortcuts)
    ? {
        accelerators: isPlainRecord(raw.shortcuts.accelerators)
          ? (raw.shortcuts.accelerators as Record<string, string>)
          : {},
        modes: isPlainRecord(raw.shortcuts.modes)
          ? (raw.shortcuts.modes as Record<string, "global" | "local">)
          : {},
      }
    : undefined;

  return {
    ok: true,
    file: {
      kind: CONFIG_TRANSFER_KIND,
      version,
      exportedAt: typeof raw.exportedAt === "string" ? raw.exportedAt : "",
      appVersion: typeof raw.appVersion === "string" ? raw.appVersion : "",
      encryption: encryption.encryption,
      settings: raw.settings,
      settingsVersion:
        typeof raw.settingsVersion === "number" ? raw.settingsVersion : undefined,
      uiLayout: pickTransferableLocalStorage(
        raw.uiLayout as Record<string, unknown> | undefined,
      ),
      illustrationStyles: Array.isArray(raw.illustrationStyles)
        ? (raw.illustrationStyles as ConfigTransferFile["illustrationStyles"])
        : undefined,
      shortcuts,
      // parseMcpScope 对坏数据回「全部可见」，拿它处理缺省字段就会让一份旧配置
      // 把本机收紧过的范围放开，所以这里先判在不在，再谈内容对不对
      mcpScope: isPlainRecord(raw.mcpScope)
        ? parseMcpScope(raw.mcpScope)
        : undefined,
    },
  };
}

export function buildConfigPreview(
  file: ConfigTransferFile,
): ConfigTransferPreview {
  const settings = isPlainRecord(file.settings) ? file.settings : {};
  const routes = isPlainRecord(settings.modelRouteDefaults)
    ? settings.modelRouteDefaults
    : {};
  return {
    exportedAt: file.exportedAt,
    appVersion: file.appVersion,
    encrypted: !!file.encryption,
    providerCount: asRecordArray(settings.aiProviders).length,
    modelCount: asRecordArray(settings.aiModels).length,
    routeCount: Object.values(routes).filter(
      (value) => typeof value === "string" && value.trim(),
    ).length,
    styleCount: Array.isArray(file.illustrationStyles)
      ? file.illustrationStyles.length
      : 0,
    shortcutCount: file.shortcuts
      ? Object.values(file.shortcuts.accelerators).filter(Boolean).length
      : 0,
    uiLayoutKeyCount: Object.keys(file.uiLayout ?? {}).length,
    mcpScope: file.mcpScope
      ? {
          mode: file.mcpScope.mode,
          collectionCount: file.mcpScope.allowedCollectionIds.length,
          allowUncategorized: file.mcpScope.allowUncategorized,
        }
      : undefined,
  };
}
