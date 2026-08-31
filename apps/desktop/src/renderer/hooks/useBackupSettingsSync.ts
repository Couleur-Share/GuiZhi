import { useEffect } from "react";
import type { SettingsState } from "../stores/settings/settings-types";
import { useSettingsStore } from "../stores/settings.store";
import { isWebRuntime } from "../runtime";

function portableSettings(state: SettingsState): Record<string, unknown> {
  return Object.fromEntries(
    Object.entries(state).filter(([, value]) => typeof value !== "function"),
  );
}

/** 把 Renderer 独有的界面偏好送到主进程内存，供无人值守完整备份取用。 */
export function useBackupSettingsSync(): void {
  useEffect(() => {
    if (isWebRuntime() || !window.api?.backup?.syncRendererSettings) return;
    const sync = (state: SettingsState) => {
      void window.api.backup.syncRendererSettings(portableSettings(state));
    };
    sync(useSettingsStore.getState());
    return useSettingsStore.subscribe(sync);
  }, []);
}
