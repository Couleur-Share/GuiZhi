import { useTranslation } from "react-i18next";
import { useCallback, useEffect, useState } from "react";
import type { WebRuntimeStatus } from "@guizhi/shared/types";
import { LoadErrorState } from "../ui/LoadErrorState";
import { runGuardedMutation } from "../../stores/operation-error.store";
export function WebCaptureSettings() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<WebRuntimeStatus | null>(null),
    [error, setError] = useState<string | null>(null);
  const load = useCallback(async () => {
    try {
      const r = await window.api.webCapture.status();
      if (!r.ok) throw new Error(r.error);
      setStatus(r.data!);
      setError(null);
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : t("webCapture.loadStatusError", "读取组件状态失败"),
      );
    }
  }, [t]);
  useEffect(() => {
    void load();
  }, [load]);
  return (
    <section className="rounded-xl border border-border p-4 space-y-3">
      <h3 className="text-sm font-medium">
        {t("webCapture.builtIn", "内置网页采集")}
      </h3>
      {error && <LoadErrorState message={error} onRetry={() => void load()} />}
      {status && (
        <>
          <p className="text-sm">
            Crawl4AI {status.version} · {status.runtimeTarget} ·{" "}
            {status.available
              ? status.running
                ? t("webCapture.running", "运行中")
                : t("webCapture.standby", "待命")
              : t("webCapture.unavailable", "不可用")}
          </p>
          {status.reason && (
            <p className="select-text text-sm text-muted-foreground">
              {status.reason}
            </p>
          )}
          <div className="flex gap-3 text-sm">
            <button onClick={() => void load()}>
              {t("webCapture.refresh", "刷新诊断")}
            </button>
            <button
              onClick={() =>
                void runGuardedMutation(
                  "webCapture.repair",
                  t("webCapture.openRepair", "打开组件修复"),
                  async () => {
                    const r = await window.api.webCapture.repair();
                    if (!r.ok) throw new Error(r.error);
                  },
                )
              }
            >
              {t("webCapture.repair", "修复组件")}
            </button>
          </div>
          <p className="text-xs text-muted-foreground">
            {t(
              "webCapture.repairHint",
              "修复将打开当前归知版本的安装包页面，重新安装恢复内置组件。无需安装 Python 或浏览器。",
            )}
          </p>
        </>
      )}
    </section>
  );
}
