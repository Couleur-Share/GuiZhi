import { useEffect, useRef, useState } from "react";
import { Loader2Icon, ShieldCheckIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportTask } from "@guizhi/shared/types";
import { nodeseekVerificationTarget } from "@guizhi/shared/utils/forum-platforms";
import { useImportStore } from "../../stores/import.store";
import { useToast } from "../ui/Toast";

export function NodeseekRetryButton({
  task,
  onComplete,
}: {
  task: ImportTask;
  onComplete: () => void;
}) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [busy, setBusy] = useState(false);
  const pending = useRef(false);
  const canceled = useRef(false);
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      canceled.current = true;
      if (pending.current)
        void window.api.platformCapture
          .cancelLogin("nodeseek")
          .catch(() => undefined);
    };
  }, []);

  const run = async () => {
    if (pending.current) return;
    pending.current = true;
    canceled.current = false;
    setBusy(true);
    try {
      // 旧的“已验证”标记可能已过期，每次都以目标帖子实际可读为准。
      await window.api.platformCapture.login(
        "nodeseek",
        false,
        undefined,
        nodeseekVerificationTarget(task.sourceInput, task.error),
      );
      if (canceled.current) return;
      if (
        await useImportStore
          .getState()
          .retryTask(task.id, { captureStrategy: "authenticated" })
      )
        onComplete();
    } catch (error) {
      if (!canceled.current)
        showToast(
          t(
            "imports.nodeseekVerificationFailed",
            "NodeSeek 验证未完成，可再次尝试",
          ),
          "error",
          { detail: error instanceof Error ? error.message : String(error) },
        );
    } finally {
      pending.current = false;
      if (mounted.current) setBusy(false);
    }
  };
  const cancel = async () => {
    canceled.current = true;
    try {
      await window.api.platformCapture.cancelLogin("nodeseek");
    } catch (error) {
      showToast(
        t("imports.nodeseekCancelFailed", "取消验证失败，请关闭验证窗口"),
        "error",
        { detail: String(error) },
      );
    }
  };
  return (
    <div className="flex items-center gap-2">
      <button
        type="button"
        disabled={busy}
        onClick={() => void run()}
        className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-xs text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
      >
        {busy ? (
          <Loader2Icon
            className="h-3.5 w-3.5 animate-spin"
            aria-hidden="true"
          />
        ) : (
          <ShieldCheckIcon className="h-3.5 w-3.5" aria-hidden="true" />
        )}
        {busy
          ? t("imports.nodeseekVerifying", "等待网页验证…")
          : t("imports.nodeseekVerifyRetry", "验证并自动采集")}
      </button>
      {busy && (
        <button
          type="button"
          onClick={() => void cancel()}
          className="h-8 rounded-lg border border-border px-3 text-xs hover:bg-accent"
        >
          {t("common.cancel", "取消")}
        </button>
      )}
    </div>
  );
}
