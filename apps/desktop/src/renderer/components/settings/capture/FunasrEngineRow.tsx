import { useCallback, useEffect, useState } from "react";
import { ArrowUpCircleIcon, DownloadIcon, RefreshCwIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { FunasrInstallProgress, FunasrStatus } from "@guizhi/shared/types";
import { loadSettingsFromMainProcess } from "../../../stores/settings.store";
import { useToast } from "../../ui/Toast";
import {
  CaptureEngineRow,
  type CaptureEngineRowProps,
  type EngineState,
} from "./CaptureEngineRow";
import { useEngineStatus } from "./use-engine-status";
import { FunasrUpdateProgress } from "./FunasrUpdateProgress";

/**
 * 本地转写引擎设置行：安装、检查更新、升级（分阶段进度）与卸载。
 * 安装完成后主进程写入内置模型并接上「语音转写」路由。
 *
 * Windows = Python SenseVoice（约 3GB，含说话人分离）；
 * macOS Apple Silicon = FunASR GGUF（约 300MB，无分离）；
 * 其余平台隐藏安装入口。
 */
export function FunasrEngineRow() {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const [isUpdating, setIsUpdating] = useState(false);
  const [updateCheck, setUpdateCheck] = useState<{
    phase: "idle" | "checking" | "available" | "latest";
    version?: string;
  }>({ phase: "idle" });
  const [isInstalling, setIsInstalling] = useState(false);
  const [isUninstalling, setIsUninstalling] = useState(false);
  const [progress, setProgress] = useState<FunasrInstallProgress | null>(null);

  const load = useCallback(
    (force: boolean) => window.api.funasr.status(force),
    [],
  );
  const { status, isRefreshing, error, refresh } =
    useEngineStatus<FunasrStatus>("funasr", load);

  useEffect(() => {
    const handleProgress = (payload: FunasrInstallProgress) => {
      setProgress(payload);
    };
    window.api.on?.(IPC_CHANNELS.FUNASR_INSTALL_PROGRESS, handleProgress);
    return () => {
      window.api.off?.(IPC_CHANNELS.FUNASR_INSTALL_PROGRESS, handleProgress);
    };
  }, []);

  const checkUpdate = async () => {
    if (
      isInstalling ||
      isUninstalling ||
      isUpdating ||
      updateCheck.phase === "checking"
    )
      return;
    setUpdateCheck({ phase: "checking" });
    try {
      const result = await window.api.funasr.checkUpdate();
      if (!result.latest)
        throw new Error(
          t("settings.captureCheckFailed", "检查更新失败，请稍后重试"),
        );
      setUpdateCheck({
        phase: result.updateAvailable ? "available" : "latest",
        version: result.latest,
      });
    } catch (cause) {
      setUpdateCheck({ phase: "idle" });
      showToast(
        t("settings.captureCheckFailed", "检查更新失败，请稍后重试"),
        "error",
        {
          detail: cause instanceof Error ? cause.message : String(cause),
        },
      );
    }
  };

  const update = async () => {
    if (!updateCheck.version || isInstalling || isUninstalling || isUpdating)
      return;
    setIsUpdating(true);
    setProgress(null);
    try {
      const result = await window.api.funasr.update(updateCheck.version);
      if (!result.success) throw new Error(result.error ?? "");
      showToast(
        t("settings.funasrUpdateDone", "本地转写引擎已更新到 v{{version}}", {
          version: result.version,
        }),
        result.warning ? "warning" : "success",
        result.warning ? { detail: result.warning } : undefined,
      );
    } catch (cause) {
      showToast(
        t("settings.funasrUpdateFailed", "本地转写引擎更新失败"),
        "error",
        {
          detail: cause instanceof Error ? cause.message : String(cause),
        },
      );
    } finally {
      await refresh(true);
      setUpdateCheck({ phase: "idle" });
      setIsUpdating(false);
      setProgress(null);
    }
  };

  const install = async () => {
    if (
      isInstalling ||
      isUninstalling ||
      isUpdating ||
      updateCheck.phase === "checking"
    ) {
      return;
    }
    setIsInstalling(true);
    setProgress(null);
    try {
      setUpdateCheck({ phase: "idle" });
      const result = await window.api.funasr.install();
      if (result.success) {
        showToast(
          t(
            "settings.funasrInstallDone",
            "本地转写引擎安装完成，已接入「语音转写」路由",
          ),
          "success",
        );
      } else {
        showToast(
          t("settings.captureInstallFailed", "安装失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
      // 主进程改写了 ai-models.json，把内置模型同步进渲染层配置
      await loadSettingsFromMainProcess();
      await refresh(true);
    } finally {
      setIsInstalling(false);
      setProgress(null);
    }
  };

  const uninstall = async () => {
    if (
      isInstalling ||
      isUninstalling ||
      isUpdating ||
      updateCheck.phase === "checking"
    ) {
      return;
    }
    setIsUninstalling(true);
    try {
      setUpdateCheck({ phase: "idle" });
      const result = await window.api.funasr.uninstall();
      if (result.success) {
        showToast(
          t("settings.funasrUninstalled", "已卸载本地转写引擎"),
          "success",
        );
      } else {
        showToast(
          t("settings.funasrUninstallFailed", "卸载失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
      await loadSettingsFromMainProcess();
      await refresh(true);
    } finally {
      setIsUninstalling(false);
    }
  };

  const installSupported = status?.installSupported !== false;
  const state: EngineState = error
    ? "error"
    : status === null
      ? "probing"
      : status.installed
        ? "ready"
        : "missing";

  const phaseLabel =
    progress?.phase === "backup"
      ? t("settings.funasrPhaseBackup", "备份当前引擎")
      : progress?.phase === "verify"
        ? t("settings.funasrPhaseVerify", "验证更新后的引擎")
        : progress?.phase === "rollback"
          ? t("settings.funasrPhaseRollback", "恢复原版本")
          : progress?.phase === "runtime"
            ? t("settings.funasrPhaseRuntime", "下载运行时")
            : progress?.phase === "deps"
              ? t(
                  "settings.funasrPhaseDeps",
                  "安装依赖（约 700MB，需要几分钟）",
                )
              : progress?.phase === "models"
                ? status?.installFlavor === "gguf"
                  ? t(
                      "settings.funasrPhaseModelsGguf",
                      "下载语音模型（约 250MB）",
                    )
                  : t("settings.funasrPhaseModels", "下载语音模型（约 1GB）")
                : "";

  const busyText =
    isInstalling || isUpdating
      ? [
          phaseLabel ||
            (isUpdating
              ? t("settings.captureUpdating", "更新中…")
              : t("settings.captureInstalling", "安装中…")),
          progress?.percent != null ? `${progress.percent}%` : "",
          progress?.detail ?? "",
        ]
          .filter(Boolean)
          .join(" · ")
      : isUninstalling
        ? t("settings.funasrUninstalling", "卸载中…")
        : undefined;

  const isGguf = status?.installFlavor === "gguf";
  const detail =
    state === "ready"
      ? // 「待命」是常态，说了等于没说——徽章的「已就绪」已经覆盖；
        // 只有真的在跑（占着内存）才值得单独标一下
        [
          `v${status?.version ?? "?"}`,
          status?.running ? t("settings.funasrRunning", "运行中") : "",
        ]
          .filter(Boolean)
          .join(" · ")
      : state === "missing"
        ? installSupported
          ? isGguf
            ? t(
                "settings.funasrDiskHintGguf",
                "音频不出本机、不按时长计费；Apple Silicon，约需 300MB 磁盘",
              )
            : t(
                "settings.funasrDiskHint",
                "音频不出本机、不按时长计费，约需 3GB 磁盘",
              )
          : t(
              "settings.funasrUnsupportedHint",
              "本平台未提供本地引擎；请在「模型服务」配置语音转写（audioText）路由",
            )
        : state === "error"
          ? (error ?? "")
          : "";

  const isRollingBack = isUpdating && progress?.phase === "rollback";
  const showInstallActions = installSupported;
  const busy =
    isInstalling ||
    isUpdating ||
    isUninstalling ||
    updateCheck.phase === "checking";
  const updateSupported =
    status?.updateSupported ?? (installSupported && !isGguf);
  const primary = ((): CaptureEngineRowProps["primary"] => {
    if (!showInstallActions || !status) return undefined;
    if (isInstalling || isUpdating)
      return {
        kind: "button",
        label: isRollingBack
          ? t("settings.funasrRestoring", "恢复中…")
          : isUpdating
            ? t("settings.captureUpdating", "更新中…")
            : t("settings.captureInstalling", "安装中…"),
        icon: DownloadIcon,
        emphasized: false,
        busy: true,
        onClick: () => {},
      };
    if (!status.installed)
      return {
        kind: "button",
        label: t("settings.captureInstall", "一键安装"),
        icon: DownloadIcon,
        emphasized: true,
        busy,
        onClick: () => void install(),
      };
    if (!updateSupported)
      return {
        kind: "status",
        label: t("settings.funasrUpdateWithApp", "随应用适配更新"),
      };
    if (updateCheck.phase === "available")
      return {
        kind: "button",
        label: t("settings.captureUpdateTo", "更新到 {{version}}", {
          version: `v${updateCheck.version}`,
        }),
        icon: ArrowUpCircleIcon,
        emphasized: true,
        busy,
        onClick: () => void update(),
      };
    if (updateCheck.phase === "latest")
      return {
        kind: "status",
        label: t("settings.captureUpToDate", "已是最新"),
      };
    return {
      kind: "button",
      label:
        updateCheck.phase === "checking"
          ? t("settings.captureChecking", "检查更新中…")
          : t("settings.captureCheckUpdate", "检查更新"),
      icon: RefreshCwIcon,
      emphasized: false,
      busy,
      onClick: () => void checkUpdate(),
    };
  })();

  return (
    <CaptureEngineRow
      engineId="funasr"
      name={t("settings.funasrEngine", "本地转写引擎")}
      purpose={t("settings.funasrPurpose", "SenseVoice 模型")}
      state={state}
      optional
      detail={
        isUpdating && !isRollingBack && status?.version && updateCheck.version
          ? `v${status.version} → v${updateCheck.version}`
          : detail
      }
      activityLabel={
        isRollingBack
          ? t("settings.funasrRestoringBadge", "恢复中")
          : isUpdating
            ? t("settings.funasrUpdatingBadge", "更新中")
            : undefined
      }
      activityContent={
        isUpdating ? <FunasrUpdateProgress progress={progress} /> : undefined
      }
      actionsDisabled={busy}
      busyText={isUpdating ? undefined : busyText}
      progressPercent={isInstalling ? (progress?.percent ?? null) : null}
      primary={primary}
      isRefreshing={isRefreshing && !isUpdating}
      onRefresh={() => {
        if (busy) return;
        setUpdateCheck({ phase: "idle" });
        void refresh(true);
      }}
      activePath={status?.installed ? status.dir : undefined}
      reinstall={
        showInstallActions && status?.installed
          ? {
              label: t("settings.funasrReinstall", "重新安装"),
              busy: isInstalling,
              onClick: () => void install(),
            }
          : undefined
      }
      remove={
        showInstallActions && status?.installed
          ? {
              label: t("settings.funasrUninstall", "卸载引擎"),
              confirmTitle: t("settings.funasrUninstall", "卸载引擎"),
              confirmMessage: isGguf
                ? t(
                    "settings.funasrUninstallConfirmGguf",
                    "确定卸载本地转写引擎？将删除运行时与模型文件（约 300MB），并移除对应的语音转写路由。",
                  )
                : t(
                    "settings.funasrUninstallConfirm",
                    "确定卸载本地转写引擎？将删除运行时与模型文件（约 3GB），并移除对应的语音转写路由。",
                  ),
              busy: isUninstalling,
              onConfirm: () => void uninstall(),
            }
          : undefined
      }
    />
  );
}
