import { AlertTriangleIcon, RotateCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

/**
 * 列表读取失败的占位。
 *
 * 存在的理由是「空」和「读不出来」在界面上长得一模一样：加载失败时静默
 * 渲染成空态，用户看到的是「暂无条目」「Wiki 还是空的」，会以为东西真没了，
 * 而不是去重试。这里明确说明是读取失败，并给一个重试入口。
 */
export function LoadErrorState({
  message,
  onRetry,
}: {
  message: string;
  onRetry: () => void;
}) {
  const { t } = useTranslation();

  return (
    <div
      role="alert"
      className="flex flex-col items-center justify-center gap-2.5 px-6 py-10 text-center"
    >
      <AlertTriangleIcon
        className="h-7 w-7 text-destructive/60"
        aria-hidden="true"
      />
      <p className="text-sm font-medium text-foreground">
        {t("common.loadFailed", "读取失败")}
      </p>
      <p className="max-w-md break-words text-xs leading-relaxed text-muted-foreground">
        {message}
      </p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-1 inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-xs text-foreground transition-colors hover:bg-muted/60"
      >
        <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
        {t("common.retry", "重试")}
      </button>
    </div>
  );
}
