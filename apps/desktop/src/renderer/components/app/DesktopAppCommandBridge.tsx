import { useCallback, useEffect } from "react";
import type { AppCommand } from "@guizhi/shared/types";

import { dispatchNewItemCommand } from "./app-command-events";

type PageType = "home" | "settings";

interface DesktopAppCommandBridgeProps {
  onNavigate: (page: PageType) => void;
  onOpenUpdater: () => void;
}

/**
 * 主进程命令桥：托盘菜单 / 全局快捷键发来的 AppCommand 在这里落地。
 */
export function DesktopAppCommandBridge({
  onNavigate,
  onOpenUpdater,
}: DesktopAppCommandBridgeProps) {
  const handleCommand = useCallback(
    (command: AppCommand) => {
      switch (command.type) {
        case "settings:open":
          onNavigate("settings");
          return;
        case "updater:open":
          onOpenUpdater();
          return;
        case "item:new":
          onNavigate("home");
          dispatchNewItemCommand();
      }
    },
    [onNavigate, onOpenUpdater],
  );

  useEffect(() => {
    const unsubscribe = window.electron?.onAppCommand?.(handleCommand);
    return () => {
      if (typeof unsubscribe === "function") {
        unsubscribe();
      }
    };
  }, [handleCommand]);

  return null;
}
