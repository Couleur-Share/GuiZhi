import { useEffect, useState } from "react";
import { CheckIcon, Loader2Icon, RotateCcwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { FunasrInstallProgress } from "@guizhi/shared/types";

/** 更新阶段是实际事件；没有总量的阶段只报耗时，不推算完成百分比。 */
export function FunasrUpdateProgress({
  progress,
}: {
  progress: FunasrInstallProgress | null;
}) {
  const { t } = useTranslation();
  const [startedAt] = useState(Date.now);
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const timer = window.setInterval(
      () => setElapsed(Math.floor((Date.now() - startedAt) / 1000)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, [startedAt]);

  const steps = [
    {
      id: "prepare",
      label: t("settings.funasrStepPrepare", "准备"),
      title:
        progress?.phase === "prepare"
          ? t("settings.funasrUpdateStorage", "正在检查空间与历史备份")
          : t("settings.funasrUpdatePreparing", "正在确认更新版本"),
      hint: t(
        "settings.funasrUpdatePreparingHint",
        "正在确认版本、核对历史备份并检查可用空间；此时尚未停止原引擎。",
      ),
    },
    {
      id: "backup",
      label: t("settings.funasrStepBackup", "备份"),
      title: t("settings.funasrPhaseBackup", "备份当前引擎"),
      hint: t(
        "settings.funasrUpdateBackupHint",
        "正在保留旧版本，以便更新失败时恢复。文件较多，这一步可能需要几分钟。",
      ),
    },
    {
      id: "deps",
      label: t("settings.funasrStepUpdate", "更新"),
      title: t("settings.funasrUpdateDependencies", "正在下载并更新引擎"),
      hint: t(
        "settings.funasrUpdateDependenciesHint",
        "下载耗时取决于网络与需要更新的内容，已下载的语音模型会保留。",
      ),
    },
    {
      id: "verify",
      label: t("settings.funasrStepVerify", "验证"),
      title: t("settings.funasrPhaseVerify", "验证更新后的引擎"),
      hint: t(
        "settings.funasrUpdateVerifyHint",
        "正在检查依赖并启动引擎，确认更新后可以正常使用。",
      ),
    },
  ];
  const rollback = progress?.phase === "rollback";
  const index = steps.findIndex(
    (step) => step.id === (progress?.phase ?? "prepare"),
  );
  const current = steps[index < 0 ? 0 : index];
  const title = rollback
    ? t("settings.funasrPhaseRollback", "恢复原版本")
    : current.title;
  const hint = rollback
    ? t(
        "settings.funasrUpdateRollbackHint",
        "此次更新未完成，正在恢复旧引擎；恢复结果会在完成后显示。",
      )
    : current.hint;
  const percent =
    progress?.percent == null
      ? null
      : Math.max(0, Math.min(100, progress.percent));
  return (
    <div
      className="mx-4 mb-3 space-y-3 rounded-xl border border-primary/20 bg-primary/5 p-4"
      data-testid="funasr-update-progress"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div
          className="flex items-center gap-2 text-sm font-medium text-foreground"
          role="status"
        >
          {rollback ? (
            <RotateCcwIcon
              className="h-4 w-4 text-primary"
              aria-hidden="true"
            />
          ) : (
            <Loader2Icon
              className="h-4 w-4 animate-spin text-primary motion-reduce:animate-none"
              aria-hidden="true"
            />
          )}
          {title}
        </div>
        <span className="text-xs tabular-nums text-muted-foreground">
          {t("settings.funasrUpdateElapsed", "已用 {{time}}", {
            time: `${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, "0")}`,
          })}
        </span>
      </div>
      {!rollback ? (
        <ol
          className="grid grid-cols-2 gap-2 sm:grid-cols-4"
          aria-label={t("settings.funasrUpdateSteps", "更新步骤")}
        >
          {steps.map((step, i) => (
            <li
              key={step.id}
              aria-current={i === index ? "step" : undefined}
              className={`flex items-center gap-2 rounded-lg px-2.5 py-2 text-xs ${i === index ? "bg-primary/10 font-medium text-primary" : i < index ? "text-foreground" : "text-muted-foreground"}`}
            >
              <span
                className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-full ${i <= index ? "bg-primary/15 text-primary" : "bg-muted"}`}
              >
                {i < index ? (
                  <CheckIcon className="h-3 w-3" aria-hidden="true" />
                ) : (
                  i + 1
                )}
              </span>
              {step.label}
            </li>
          ))}
        </ol>
      ) : null}
      {percent !== null ? (
        <div className="flex items-center gap-3">
          <div
            role="progressbar"
            aria-label={title}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={percent}
            className="h-1.5 flex-1 overflow-hidden rounded-full bg-primary/10"
          >
            <div
              className="h-full rounded-full bg-primary transition-[width] motion-reduce:transition-none"
              style={{ width: `${percent}%` }}
            />
          </div>
          <span className="text-xs tabular-nums text-muted-foreground">
            {percent}%
          </span>
        </div>
      ) : null}
      <p className="text-xs leading-relaxed text-muted-foreground">{hint}</p>
      {progress?.detail ? (
        <details className="text-xs text-muted-foreground">
          <summary className="w-fit cursor-pointer hover:text-foreground">
            {t("settings.funasrUpdateDetails", "查看运行详情")}
          </summary>
          <pre className="mt-2 max-h-28 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted/50 p-2 font-mono">
            {progress.detail}
          </pre>
        </details>
      ) : null}
    </div>
  );
}
