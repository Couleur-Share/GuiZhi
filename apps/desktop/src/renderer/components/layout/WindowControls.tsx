import { MinusIcon, SquareIcon, XIcon } from "lucide-react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";

/**
 * Windows 无边框窗口的最小化 / 最大化 / 关闭按钮。
 * 挂在 TopBar 右侧，代替原先独立的 32px TitleBar，释放纵向空间。
 */
export function WindowControls() {
  const { t } = useTranslation();
  const [isMaximized, setIsMaximized] = useState(false);
  const [isWindows, setIsWindows] = useState(false);

  useEffect(() => {
    setIsWindows(navigator.userAgent.toLowerCase().includes("win"));
  }, []);

  if (!isWindows) {
    return null;
  }

  return (
    <div className="ml-1 flex h-12 shrink-0 select-none items-stretch titlebar-no-drag">
      <button
        type="button"
        onClick={() => window.electron?.minimize?.()}
        className="flex w-11 items-center justify-center text-foreground/70 transition-colors hover:bg-muted hover:text-foreground focus:outline-none focus-visible:bg-muted"
        aria-label={t("common.minimize", "Minimize")}
      >
        <MinusIcon className="h-4 w-4" aria-hidden="true" />
      </button>
      <button
        type="button"
        onClick={() => {
          window.electron?.maximize?.();
          setIsMaximized((current) => !current);
        }}
        className="flex w-11 items-center justify-center text-foreground/70 transition-colors hover:bg-muted hover:text-foreground focus:outline-none focus-visible:bg-muted"
        aria-label={
          isMaximized
            ? t("common.restore", "Restore")
            : t("common.maximize", "Maximize")
        }
      >
        <SquareIcon className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
      <button
        type="button"
        onClick={() => window.electron?.close?.()}
        className="flex w-11 items-center justify-center text-foreground/70 transition-colors hover:bg-red-500 hover:text-white focus:outline-none focus-visible:bg-red-500/20"
        aria-label={t("common.close", "Close")}
      >
        <XIcon className="h-4 w-4" aria-hidden="true" />
      </button>
    </div>
  );
}
