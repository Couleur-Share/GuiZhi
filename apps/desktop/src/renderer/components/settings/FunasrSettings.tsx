import { useCallback, useEffect, useState } from "react";
import {
  AudioLinesIcon,
  CheckCircle2Icon,
  DownloadIcon,
  Loader2Icon,
  Trash2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  FunasrInstallProgress,
  FunasrStatus,
} from "@guizhi/shared/types";
import { loadSettingsFromMainProcess } from "../../stores/settings.store";
import { SettingItem } from "./shared";
import { useToast } from "../ui/Toast";

/**
 * 本地转写引擎（SenseVoiceSmall）管理：一键安装（分阶段进度）、卸载。
 * 安装完成后主进程自动写入内置模型并接上「语音转写」路由，
 * 普通用户无需接触 API 地址 / Key；高级用户可在「模型服务」里编辑。
 */
export function FunasrSettings() {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const [status, setStatus] = useState<FunasrStatus | null>(null);
  const [isProbing, setIsProbing] = useState(true);
  const [isInstalling, setIsInstalling] = useState(false);
  const [isUninstalling, setIsUninstalling] = useState(false);
  const [progress, setProgress] = useState<FunasrInstallProgress | null>(null);

  const refreshStatus = useCallback(async () => {
    setIsProbing(true);
    try {
      const next = await window.api.funasr.status();
      setStatus(next);
    } catch (error) {
      console.error("获取本地转写引擎状态失败:", error);
    } finally {
      setIsProbing(false);
    }
  }, []);

  useEffect(() => {
    void refreshStatus();
  }, [refreshStatus]);

  useEffect(() => {
    const handleProgress = (payload: FunasrInstallProgress) => {
      setProgress(payload);
    };
    window.api.on?.("funasr:installProgress", handleProgress);
    return () => {
      window.api.off?.("funasr:installProgress", handleProgress);
    };
  }, []);

  const install = async () => {
    if (isInstalling || isUninstalling) {
      return;
    }
    setIsInstalling(true);
    setProgress(null);
    try {
      const result = await window.api.funasr.install();
      if (result.success) {
        showToast(
          t(
            "settings.funasrInstallDone",
            "本地转写引擎安装完成，已接入「语音转写」路由",
          ),
          "success",
        );
        // 主进程改写了 ai-models.json，把内置模型同步进渲染层配置
        await loadSettingsFromMainProcess();
        await refreshStatus();
      } else {
        showToast(
          t("settings.funasrInstallFailed", "安装失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
    } finally {
      setIsInstalling(false);
      setProgress(null);
    }
  };

  const uninstall = async () => {
    if (isInstalling || isUninstalling) {
      return;
    }
    if (
      !confirm(
        t(
          "settings.funasrUninstallConfirm",
          "确定卸载本地转写引擎？将删除运行时与模型文件（约 3GB），并移除对应的语音转写路由。",
        ),
      )
    ) {
      return;
    }
    setIsUninstalling(true);
    try {
      const result = await window.api.funasr.uninstall();
      if (result.success) {
        showToast(
          t("settings.funasrUninstalled", "已卸载本地转写引擎"),
          "success",
        );
        await loadSettingsFromMainProcess();
        await refreshStatus();
      } else {
        showToast(
          t("settings.funasrUninstallFailed", "卸载失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
    } finally {
      setIsUninstalling(false);
    }
  };

  const phaseLabel =
    progress?.phase === "runtime"
      ? t("settings.funasrPhaseRuntime", "下载运行时")
      : progress?.phase === "deps"
        ? t("settings.funasrPhaseDeps", "安装依赖（约 700MB，需要几分钟）")
        : progress?.phase === "models"
          ? t("settings.funasrPhaseModels", "下载语音模型（约 1GB）")
          : "";

  const statusText = isProbing
    ? t("settings.funasrProbing", "检测中…")
    : isInstalling
      ? [
          phaseLabel,
          progress?.percent != null ? `${progress.percent}%` : "",
          progress?.detail ?? "",
        ]
          .filter(Boolean)
          .join(" · ")
      : status?.installed
        ? status.running
          ? t("settings.funasrStatusRunning", "已安装 · v{{version}} · 服务运行中", {
              version: status.version ?? "?",
            })
          : t(
              "settings.funasrStatusIdle",
              "已安装 · v{{version}} · 待命（转写时自动启动）",
              { version: status.version ?? "?" },
            )
        : t(
            "settings.funasrStatusMissing",
            "SenseVoice 中文转写模型，本地离线运行、免费无限流——安装后自动接入「语音转写」路由（约需 3GB 磁盘）",
          );

  const installLabel = isInstalling
    ? t("settings.funasrInstalling", "安装中…")
    : t("settings.funasrInstall", "一键安装");

  return (
    <>
      <SettingItem
        label={t("settings.funasrEngine", "本地转写引擎")}
        description={statusText}
      >
        <div className="flex shrink-0 items-center gap-2">
          {isProbing || isInstalling || isUninstalling ? (
            <Loader2Icon
              className="h-4 w-4 animate-spin text-muted-foreground"
              aria-hidden="true"
            />
          ) : status?.installed ? (
            <CheckCircle2Icon
              className="h-4 w-4 text-emerald-500"
              aria-hidden="true"
            />
          ) : (
            <AudioLinesIcon
              className="h-4 w-4 text-muted-foreground"
              aria-hidden="true"
            />
          )}
          {!status?.installed || isInstalling ? (
            <button
              type="button"
              onClick={() => void install()}
              disabled={isInstalling || isUninstalling}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-60"
            >
              {isInstalling ? (
                <Loader2Icon
                  className="h-4 w-4 animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <DownloadIcon className="h-4 w-4" aria-hidden="true" />
              )}
              {installLabel}
            </button>
          ) : (
            <button
              type="button"
              onClick={() => void uninstall()}
              disabled={isUninstalling}
              title={t("settings.funasrUninstall", "卸载")}
              className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-60"
            >
              {isUninstalling ? (
                <Loader2Icon
                  className="h-4 w-4 animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <Trash2Icon className="h-4 w-4" aria-hidden="true" />
              )}
              {t("settings.funasrUninstall", "卸载")}
            </button>
          )}
        </div>
      </SettingItem>

      {isInstalling && progress?.percent != null ? (
        <div className="px-4 pb-3">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-primary transition-all duration-200"
              style={{ width: `${progress.percent}%` }}
            />
          </div>
        </div>
      ) : null}
    </>
  );
}
