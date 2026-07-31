import { useCallback, useEffect, useState } from "react";
import { DownloadIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import { IPC_CHANNELS } from "@guizhi/shared/constants";
import type { FunasrInstallProgress, FunasrStatus } from "@guizhi/shared/types";
import { loadSettingsFromMainProcess } from "../../../stores/settings.store";
import { useToast } from "../../ui/Toast";
import { CaptureEngineRow, type EngineState } from "./CaptureEngineRow";
import { useEngineStatus } from "./use-engine-status";

/**
 * 本地转写引擎设置行：一键安装（分阶段进度）、卸载。
 * 安装完成后主进程写入内置模型并接上「语音转写」路由。
 *
 * Windows = Python SenseVoice（约 3GB，含说话人分离）；
 * macOS Apple Silicon = FunASR GGUF（约 300MB，无分离）；
 * 其余平台隐藏安装入口。
 */
export function FunasrEngineRow() {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const [isInstalling, setIsInstalling] = useState(false);
  const [isUninstalling, setIsUninstalling] = useState(false);
  const [progress, setProgress] = useState<FunasrInstallProgress | null>(null);

  const load = useCallback((force: boolean) => window.api.funasr.status(force), []);
  const { status, isRefreshing, error, refresh } = useEngineStatus<FunasrStatus>(
    "funasr",
    load,
  );

  useEffect(() => {
    const handleProgress = (payload: FunasrInstallProgress) => {
      setProgress(payload);
    };
    window.api.on?.(IPC_CHANNELS.FUNASR_INSTALL_PROGRESS, handleProgress);
    return () => {
      window.api.off?.(IPC_CHANNELS.FUNASR_INSTALL_PROGRESS, handleProgress);
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
    if (isInstalling || isUninstalling) {
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
    progress?.phase === "runtime"
      ? t("settings.funasrPhaseRuntime", "下载运行时")
      : progress?.phase === "deps"
        ? t("settings.funasrPhaseDeps", "安装依赖（约 700MB，需要几分钟）")
        : progress?.phase === "models"
          ? status?.installFlavor === "gguf"
            ? t(
                "settings.funasrPhaseModelsGguf",
                "下载语音模型（约 250MB）",
              )
            : t("settings.funasrPhaseModels", "下载语音模型（约 1GB）")
          : "";

  const busyText = isInstalling
    ? [
        phaseLabel || t("settings.captureInstalling", "安装中…"),
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
          ? error ?? ""
          : "";

  const showInstallActions = installSupported;

  return (
    <CaptureEngineRow
      engineId="funasr"
      name={t("settings.funasrEngine", "本地转写引擎")}
      purpose={t("settings.funasrPurpose", "SenseVoice 模型")}
      state={state}
      optional
      detail={detail}
      busyText={busyText}
      progressPercent={isInstalling ? (progress?.percent ?? null) : null}
      // 只在「确认没装」且本平台支持安装时才给按钮：探测中不知道装没装，
      // 装好之后也没有「更新」这回事——上游是 pip / GGUF 装出来的运行时，重装只是
      // 修复手段，不该占着主操作位诱人误点，挪进高级面板。
      primary={
        !showInstallActions
          ? undefined
          : isInstalling
            ? {
                kind: "button",
                label: t("settings.captureInstalling", "安装中…"),
                icon: DownloadIcon,
                emphasized: false,
                busy: true,
                onClick: () => {},
              }
            : status && !status.installed
              ? {
                  kind: "button",
                  label: t("settings.captureInstall", "一键安装"),
                  icon: DownloadIcon,
                  emphasized: true,
                  busy: isUninstalling,
                  onClick: () => void install(),
                }
              : undefined
      }
      isRefreshing={isRefreshing}
      onRefresh={() => void refresh(true)}
      activePath={status?.installed ? status.dir : undefined}
      reinstall={
        showInstallActions && status?.installed
          ? {
              label: t("settings.funasrReinstall", "重新安装"),
              busy: isInstalling || isUninstalling,
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
