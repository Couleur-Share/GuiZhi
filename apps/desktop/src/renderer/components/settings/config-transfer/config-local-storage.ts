/**
 * 配置迁移在渲染进程这一侧的读写：localStorage 里的设置与界面偏好。
 *
 * 主进程读不到 localStorage，而 guizhi-settings 恰恰是 AI 配置的真相源
 * （比 ai-models.json 多出 chatParams 与 scenarioModelDefaults），所以采集
 * 必须由渲染进程发起。
 */
import {
  pickTransferableLocalStorage,
  TRANSFER_LOCAL_STORAGE_KEYS,
} from "@guizhi/shared/utils/config-transfer";
import { useSettingsStore } from "../../../stores/settings.store";

const SETTINGS_STORAGE_KEY = "guizhi-settings";

export interface SettingsSnapshot {
  settings: Record<string, unknown>;
  settingsVersion?: number;
}

/**
 * 取 zustand persist 写下的 `{ state, version }`。
 *
 * 优先读 localStorage 而不是内存状态：它已经过 partialize，天然不含 actions，
 * 而函数过不了 IPC 的结构化克隆。全新安装且一项都没改过时 localStorage 是空的，
 * 此时退回内存状态并做一次 JSON 往返把函数丢掉。
 */
export function readSettingsSnapshot(): SettingsSnapshot {
  try {
    const raw = window.localStorage.getItem(SETTINGS_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw) as { state?: unknown; version?: unknown };
      if (parsed?.state && typeof parsed.state === "object") {
        return {
          settings: parsed.state as Record<string, unknown>,
          settingsVersion:
            typeof parsed.version === "number" ? parsed.version : undefined,
        };
      }
    }
  } catch (error) {
    console.warn("[config] 读取已持久化的设置失败，改用内存状态:", error);
  }
  return {
    settings: JSON.parse(
      JSON.stringify(useSettingsStore.getState()),
    ) as Record<string, unknown>,
  };
}

export function readUiLayoutSnapshot(): Record<string, unknown> {
  const entries: Record<string, unknown> = {};
  for (const key of TRANSFER_LOCAL_STORAGE_KEYS) {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw) {
        entries[key] = JSON.parse(raw);
      }
    } catch (error) {
      console.warn(`[config] 界面偏好 ${key} 读取失败，导出将不含它:`, error);
    }
  }
  return entries;
}

/**
 * 把导入结果写回 localStorage。
 *
 * 是「盖在现有设置之上」而不是整份替换：文件里没有的字段保留本机原值。
 * 机器绑定的那几项（工具路径、数据目录、开机自启）故意不进导出文件，整份替换
 * 会让它们退回默认值——yt-dlp 路径在 SQLite 里还在、工具照样能跑，但设置页
 * 显示成空的，用户只会以为配置丢了。
 *
 * 写完立刻重启：settings store 只在创建时读一次 localStorage，而主进程侧的
 * 快捷键注册、代理、备份调度也都要按新配置重新起来。逐项热更新做得到，但漏掉
 * 一处的表现是「界面改了、行为没改」，排查成本远高于两秒钟。
 */
export function writeImportedLocalStorage(
  settings: Record<string, unknown>,
  settingsVersion: number | undefined,
  uiLayout: Record<string, unknown> | undefined,
): void {
  const current = readSettingsSnapshot();
  window.localStorage.setItem(
    SETTINGS_STORAGE_KEY,
    JSON.stringify({
      state: { ...current.settings, ...settings },
      // 版本号缺省时写 0：低于 store 当前版本会走 migrate，那条路径对任意历史
      // 快照都做全量归一，比直接 merge 更保险
      version: settingsVersion ?? current.settingsVersion ?? 0,
    }),
  );

  // 同理，只覆盖文件里带到的那几个界面偏好
  for (const [key, value] of Object.entries(
    pickTransferableLocalStorage(uiLayout),
  )) {
    window.localStorage.setItem(key, JSON.stringify(value));
  }
}
