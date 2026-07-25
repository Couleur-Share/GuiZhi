import { useEffect, useState } from "react";

/** 与主进程 shortcuts.ts 的 DEFAULT_SHORTCUTS 保持一致 */
const DEFAULT_SHORTCUTS: Record<ShortcutAction, string> = {
  showApp: "Alt+Shift+P",
  newItem: "Alt+Shift+N",
  search: "Alt+Shift+F",
  settings: "Alt+Shift+S",
};

export type ShortcutAction = "showApp" | "newItem" | "search" | "settings";

function formatShortcut(combo: string): string {
  const isMac = navigator.platform.includes("Mac");
  return combo
    .replace("CommandOrControl", isMac ? "⌘" : "Ctrl")
    .replace("Control", isMac ? "⌃" : "Ctrl")
    .replace("Alt", isMac ? "⌥" : "Alt")
    .replace("Shift", isMac ? "⇧" : "Shift");
}

/**
 * 读取主进程实际生效的快捷键并格式化为界面提示文案。
 * 用户在设置里改过之后经 shortcuts:updated 同步，避免界面写死组合键。
 */
export function useShortcutLabel(action: ShortcutAction): string {
  const [combo, setCombo] = useState(DEFAULT_SHORTCUTS[action]);

  useEffect(() => {
    let active = true;

    void window.electron?.getShortcuts?.().then((shortcuts) => {
      if (active && shortcuts?.[action]) {
        setCombo(shortcuts[action]);
      }
    });

    const offUpdated = window.electron?.onShortcutsUpdated?.((shortcuts) => {
      if (shortcuts?.[action]) {
        setCombo(shortcuts[action]);
      }
    });

    return () => {
      active = false;
      if (typeof offUpdated === "function") {
        offUpdated();
      }
    };
  }, [action]);

  return formatShortcut(combo);
}
