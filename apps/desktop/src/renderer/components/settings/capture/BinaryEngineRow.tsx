import { useCallback, useEffect, useState } from "react";
import {
  ArrowUpCircleIcon,
  ClipboardCopyIcon,
  DownloadIcon,
  RefreshCwIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  FfmpegStatus,
  ToolUpdateCheck,
  YtDlpDownloadProgress,
  YtDlpStatus,
} from "@guizhi/shared/types";
import { useToast } from "../../ui/Toast";
import {
  CaptureEngineRow,
  type CaptureEngineRowProps,
  type EngineState,
} from "./CaptureEngineRow";
import { formatEngineVersion } from "./engine-version";
import { useEngineStatus } from "./use-engine-status";

/** YtDlpStatus 与 FfmpegStatus 结构一致（source 取值也相同），共用一套渲染逻辑 */
type BinaryEngineStatus = YtDlpStatus | FfmpegStatus;

interface BinaryEngineApi {
  status: (force?: boolean) => Promise<BinaryEngineStatus>;
  checkUpdate: () => Promise<ToolUpdateCheck>;
  install: () => Promise<{
    success: boolean;
    version?: string;
    error?: string;
  }>;
  remove: () => Promise<boolean>;
  pickBinary: () => Promise<string | null>;
}

export interface BinaryEngineTexts {
  name: string;
  /** 极短的一句用途；装好之后状态行主要让位给版本与来源 */
  purpose: string;
  /** 只在未安装时追加：说明为什么值得装。装好后这句就是噪音 */
  missingHint: string;
  customPathHint: string;
  customPathPlaceholder: string;
  installedToast: (version: string) => string;
  removedToast: string;
  removeConfirmMessage: string;
}

export interface BinaryEngineRowProps {
  engineId: "ytdlp" | "ffmpeg";
  api: BinaryEngineApi;
  /** 主进程推送下载进度的频道（须在 preload 监听白名单内） */
  progressChannel: string;
  customPath: string;
  onCustomPathChange: (value: string) => void;
  /** 可选引擎缺失时不报警（ffmpeg 未安装只是少了音频压缩） */
  optional?: boolean;
  texts: BinaryEngineTexts;
}

/** 与「关于应用」里应用自身更新入口同构的四态机 */
type UpdateCheckState =
  | { phase: "idle" }
  | { phase: "checking" }
  | { phase: "latest" }
  | { phase: "available"; version: string };

function formatBytes(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function hasInstallHint(
  status: BinaryEngineStatus,
): status is FfmpegStatus & { installHintCommand: string } {
  return (
    "installHintCommand" in status &&
    typeof status.installHintCommand === "string" &&
    status.installHintCommand.length > 0
  );
}

/**
 * 二进制引擎（yt-dlp / ffmpeg）设置行：状态探测、一键安装 / 更新内置版、
 * 移除内置版、自定义路径。两者除文案与 API 命名空间外完全同构。
 *
 * 当前平台若不支持应用内安装（ffmpeg 在 Mac / Linux），主操作改为复制
 * 推荐命令或不给安装按钮，避免点了必失败。
 */
export function BinaryEngineRow({
  engineId,
  api,
  progressChannel,
  customPath,
  onCustomPathChange,
  optional = false,
  texts,
}: BinaryEngineRowProps) {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const [isInstalling, setIsInstalling] = useState(false);
  const [isRemoving, setIsRemoving] = useState(false);
  const [progress, setProgress] = useState<YtDlpDownloadProgress | null>(null);
  // 刻意不进 useEngineStatus 的跨挂载缓存：检查结果有时效性，
  // 每次进设置页重新回到 idle，用户想知道就再点一次。
  const [updateCheck, setUpdateCheck] = useState<UpdateCheckState>({
    phase: "idle",
  });

  const load = useCallback((force: boolean) => api.status(force), [api]);
  // 自定义路径进缓存键：换了探测目标就不该继续展示上一个目标的结果
  const { status, isRefreshing, error, refresh } =
    useEngineStatus<BinaryEngineStatus>(`${engineId}:${customPath}`, load);

  useEffect(() => {
    const handleProgress = (payload: YtDlpDownloadProgress) => {
      setProgress(payload);
    };
    window.api.on?.(progressChannel, handleProgress);
    return () => {
      window.api.off?.(progressChannel, handleProgress);
    };
  }, [progressChannel]);

  const checkUpdate = async () => {
    if (updateCheck.phase === "checking") {
      return;
    }
    setUpdateCheck({ phase: "checking" });
    try {
      const result = await api.checkUpdate();
      if (!result.latest) {
        setUpdateCheck({ phase: "idle" });
        showToast(
          t("settings.captureCheckFailed", "检查更新失败，请稍后重试"),
          "error",
        );
        return;
      }
      setUpdateCheck(
        result.updateAvailable
          ? { phase: "available", version: result.latest }
          : { phase: "latest" },
      );
    } catch (cause) {
      console.error(`检查 ${engineId} 更新失败：`, cause);
      setUpdateCheck({ phase: "idle" });
      showToast(
        t("settings.captureCheckFailed", "检查更新失败，请稍后重试"),
        "error",
        // 这里几乎总是网络或代理问题，说清楚用户才知道该去配代理
        { detail: cause instanceof Error ? cause.message : String(cause) },
      );
    }
  };

  const install = async () => {
    if (isInstalling) {
      return;
    }
    setIsInstalling(true);
    setProgress(null);
    try {
      const result = await api.install();
      if (result.success) {
        showToast(texts.installedToast(result.version ?? ""), "success");
        // 版本变了，之前的检查结论作废
        setUpdateCheck({ phase: "idle" });
      } else {
        showToast(
          t("settings.captureInstallFailed", "安装失败：{{message}}", {
            message: result.error ?? "",
          }),
          "error",
        );
      }
      await refresh(true);
    } catch (cause) {
      // 主进程 handler 内部已兜住业务异常，走到这里说明 IPC 本身出了问题
      console.error(`安装 ${engineId} 失败：`, cause);
      showToast(
        t("settings.captureInstallFailed", "安装失败：{{message}}", {
          message: cause instanceof Error ? cause.message : String(cause),
        }),
        "error",
      );
    } finally {
      setIsInstalling(false);
      setProgress(null);
    }
  };

  const copyInstallHint = async (command: string) => {
    try {
      await navigator.clipboard.writeText(command);
      showToast(
        t("settings.captureCommandCopied", "已复制：{{command}}", { command }),
        "success",
      );
    } catch (cause) {
      showToast(t("settings.captureCopyFailed", "复制失败"), "error", {
        detail: cause instanceof Error ? cause.message : String(cause),
      });
    }
  };

  const removeManaged = async () => {
    if (isRemoving) {
      return;
    }
    setIsRemoving(true);
    try {
      await api.remove();
      showToast(texts.removedToast, "success");
      await refresh(true);
    } catch (cause) {
      console.error(`移除内置版 ${engineId} 失败：`, cause);
      showToast(
        t("settings.captureRemoveFailed", "移除失败：{{message}}", {
          message: cause instanceof Error ? cause.message : String(cause),
        }),
        "error",
      );
    } finally {
      setIsRemoving(false);
    }
  };

  const pickBinary = async () => {
    const picked = await api.pickBinary();
    if (picked) {
      onCustomPathChange(picked);
    }
  };

  const hasCustomPath = customPath.trim().length > 0;
  const state: EngineState = error
    ? "error"
    : status === null
      ? "probing"
      : status.installed
        ? "ready"
        : hasCustomPath
          ? "invalid"
          : "missing";

  const sourceLabel =
    status?.source === "managed"
      ? t("settings.captureSourceManaged", "内置版")
      : status?.source === "path"
        ? t("settings.captureSourcePath", "系统 PATH")
        : status?.source === "custom"
          ? t("settings.captureSourceCustom", "自定义路径")
          : "";

  // 缺字段按「支持」处理：同版本主进程总会带上；旧 mock / 热更新瞬时缺省不应藏按钮
  const installUnsupported = status?.installSupported === false;

  const unsupportedMissingHint =
    status && installUnsupported && state === "missing"
      ? hasInstallHint(status)
        ? t(
            "settings.ffmpegBrewHint",
            "本平台不提供应用内安装；终端执行 {{command}}，或在高级选项里指定路径",
            { command: status.installHintCommand },
          )
        : t(
            "settings.ffmpegPackageManagerHint",
            "本平台不提供应用内安装；请用系统包管理器安装，或在高级选项里指定路径",
          )
      : null;

  const detail =
    state === "ready"
      ? [
          status?.version ? formatEngineVersion(status.version) : "",
          sourceLabel,
        ]
          .filter(Boolean)
          .join(" · ")
      : state === "invalid"
        ? t(
            "settings.captureCustomInvalid",
            "自定义路径无法运行，请在高级选项中检查或清除",
          )
        : state === "error"
          ? error ?? ""
          : state === "missing"
            ? (unsupportedMissingHint ?? texts.missingHint)
            : "";

  const percent =
    progress && progress.total
      ? Math.min(100, Math.round((progress.transferred / progress.total) * 100))
      : null;

  const isManaged = status?.source === "managed";
  const busyText = isInstalling
    ? percent !== null
      ? t("settings.captureDownloading", "下载中 {{percent}}%", { percent })
      : progress
        ? t("settings.captureDownloadingBytes", "已下载 {{size}}", {
            size: formatBytes(progress.transferred),
          })
        : t("settings.captureInstalling", "安装中…")
    : isRemoving
      ? t("settings.captureRemoving", "移除中…")
      : updateCheck.phase === "checking"
        ? t("settings.captureChecking", "检查更新中…")
        : undefined;

  const isBusy = isInstalling || isRemoving;

  /**
   * 内置版的主操作遵循「先检查、有更新才给更新按钮」：
   * 未检查 → 检查更新 / 检查中 → 检查中… / 有更新 → 更新到 X / 已是最新 → 不给按钮。
   * 平台不支持应用内安装时：未装给「复制 brew 命令」；已装（PATH/自定义）不诱人装内置版。
   */
  const primary = ((): CaptureEngineRowProps["primary"] => {
    if (isInstalling) {
      return {
        kind: "button",
        label: isManaged
          ? t("settings.captureUpdating", "更新中…")
          : t("settings.captureInstalling", "安装中…"),
        icon: DownloadIcon,
        emphasized: false,
        busy: true,
        onClick: () => {},
      };
    }
    // 还没探测出结果（或探测失败）时不知道装没装，任何动作都是瞎猜。
    // 状态徽章已经在说「检测中 / 检测失败」，主操作位留空即可。
    if (!status) {
      return undefined;
    }
    if (status.installSupported === false) {
      if (status.installed) {
        return undefined;
      }
      if (hasInstallHint(status)) {
        const command = status.installHintCommand;
        return {
          kind: "button",
          label: t("settings.captureCopyBrewCommand", "复制 brew 命令"),
          icon: ClipboardCopyIcon,
          emphasized: true,
          busy: false,
          onClick: () => void copyInstallHint(command),
        };
      }
      return undefined;
    }
    if (!status.installed) {
      return {
        kind: "button",
        label: t("settings.captureInstall", "一键安装"),
        icon: DownloadIcon,
        emphasized: true,
        busy: isBusy,
        onClick: () => void install(),
      };
    }
    if (status.source !== "managed") {
      return {
        kind: "button",
        label: t("settings.captureInstallManaged", "安装内置版"),
        icon: DownloadIcon,
        emphasized: false,
        busy: isBusy,
        onClick: () => void install(),
      };
    }
    if (updateCheck.phase === "available") {
      return {
        kind: "button",
        label: t("settings.captureUpdateTo", "更新到 {{version}}", {
          version: formatEngineVersion(updateCheck.version),
        }),
        icon: ArrowUpCircleIcon,
        emphasized: true,
        busy: isBusy,
        onClick: () => void install(),
      };
    }
    if (updateCheck.phase === "latest") {
      return { kind: "status", label: t("settings.captureUpToDate", "已是最新") };
    }
    return {
      kind: "button",
      label:
        updateCheck.phase === "checking"
          ? t("settings.captureChecking", "检查更新中…")
          : t("settings.captureCheckUpdate", "检查更新"),
      icon: RefreshCwIcon,
      emphasized: false,
      busy: isBusy || updateCheck.phase === "checking",
      onClick: () => void checkUpdate(),
    };
  })();

  return (
    <CaptureEngineRow
      engineId={engineId}
      name={texts.name}
      purpose={texts.purpose}
      state={state}
      optional={optional}
      detail={detail}
      busyText={busyText}
      progressPercent={isInstalling ? percent : null}
      primary={primary}
      isRefreshing={isRefreshing}
      // 重新探测本地状态时，之前那次远端检查的结论也一并作废
      onRefresh={() => {
        setUpdateCheck({ phase: "idle" });
        void refresh(true);
      }}
      // 系统 PATH 来源时 path 只是命令名，展示成「生效路径」反而误导
      activePath={
        status?.source === "managed" || status?.source === "custom"
          ? status.path
          : undefined
      }
      customPath={{
        value: customPath,
        placeholder: texts.customPathPlaceholder,
        hint: texts.customPathHint,
        onPick: () => void pickBinary(),
        onClear: () => onCustomPathChange(""),
      }}
      remove={
        isManaged
          ? {
              label: t("settings.captureRemoveManaged", "移除内置版"),
              confirmTitle: t("settings.captureRemoveManaged", "移除内置版"),
              confirmMessage: texts.removeConfirmMessage,
              busy: isRemoving,
              onConfirm: () => void removeManaged(),
            }
          : undefined
      }
    />
  );
}
