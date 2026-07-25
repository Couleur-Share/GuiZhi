import { useCallback, useEffect, useState } from "react";
import {
  CheckCircle2Icon,
  DownloadIcon,
  FolderOpenIcon,
  InfoIcon,
  Loader2Icon,
  RefreshCwIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type {
  FfmpegDownloadProgress,
  FfmpegStatus,
} from "@guizhi/shared/types";
import { useSettingsStore } from "../../stores/settings.store";
import { SettingItem } from "./shared";
import { Input } from "../ui/Input";
import { useToast } from "../ui/Toast";

const STATUS_PROBE_DEBOUNCE_MS = 600;

function formatBytes(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * ffmpeg 引擎管理：状态展示（来源 / 版本）、一键安装（带下载进度）、
 * 更新 / 移除托管版、自定义路径（高级选项）。
 *
 * 用途：转写前把音频转码为 16kHz 单声道 mp3（上传更小、格式兼容更好）；
 * 未安装时转写仍可用，原文件直传。
 */
export function FfmpegSettings() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const ffmpegPath = useSettingsStore((state) => state.ffmpegPath);
  const setFfmpegPath = useSettingsStore((state) => state.setFfmpegPath);

  const [status, setStatus] = useState<FfmpegStatus | null>(null);
  const [isProbing, setIsProbing] = useState(true);
  const [isInstalling, setIsInstalling] = useState(false);
  const [progress, setProgress] = useState<FfmpegDownloadProgress | null>(
    null,
  );

  const refreshStatus = useCallback(async () => {
    setIsProbing(true);
    try {
      const next = await window.api.ffmpeg.status();
      setStatus(next);
    } catch (error) {
      console.error("获取 ffmpeg 状态失败:", error);
    } finally {
      setIsProbing(false);
    }
  }, []);

  // 自定义路径变化后防抖重新探测（探测会真实运行 -version）
  useEffect(() => {
    const timer = setTimeout(() => {
      void refreshStatus();
    }, STATUS_PROBE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [refreshStatus, ffmpegPath]);

  // 下载进度事件
  useEffect(() => {
    const handleProgress = (payload: FfmpegDownloadProgress) => {
      setProgress(payload);
    };
    window.api.on?.("ffmpeg:downloadProgress", handleProgress);
    return () => {
      window.api.off?.("ffmpeg:downloadProgress", handleProgress);
    };
  }, []);

  const install = async () => {
    if (isInstalling) {
      return;
    }
    setIsInstalling(true);
    setProgress(null);
    try {
      const result = await window.api.ffmpeg.install();
      if (result.success) {
        showToast(
          t(
            "settings.ffmpegInstallDone",
            "ffmpeg 安装完成（v{{version}}），之后转写会自动压缩音频",
            { version: result.version ?? "" },
          ),
          "success",
        );
        await refreshStatus();
      } else {
        showToast(
          t("settings.ffmpegInstallFailed", "安装失败：{{message}}", {
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

  const removeManaged = async () => {
    await window.api.ffmpeg.remove();
    showToast(t("settings.ffmpegRemoved", "已移除内置版 ffmpeg"), "success");
    await refreshStatus();
  };

  const pickBinary = async () => {
    const picked = await window.api.ffmpeg.pickBinary();
    if (picked) {
      setFfmpegPath(picked);
    }
  };

  const sourceLabel =
    status?.source === "managed"
      ? t("settings.ffmpegSourceManaged", "内置版")
      : status?.source === "path"
        ? t("settings.ffmpegSourcePath", "系统 PATH")
        : status?.source === "custom"
          ? t("settings.ffmpegSourceCustom", "自定义路径")
          : "";

  const statusText = isProbing
    ? t("settings.ffmpegProbing", "检测中…")
    : status?.installed
      ? t("settings.ffmpegStatusReady", "已就绪 · v{{version}} · {{source}}", {
          version: status.version ?? "?",
          source: sourceLabel,
        })
      : ffmpegPath.trim()
        ? t(
            "settings.ffmpegCustomInvalid",
            "自定义路径无法运行，请检查或改用一键安装",
          )
        : t(
            "settings.ffmpegStatusMissing",
            "未安装（可选）——安装后转写前自动压缩音频，上传更快、格式兼容更好",
          );

  const percent =
    progress && progress.total
      ? Math.min(100, Math.round((progress.transferred / progress.total) * 100))
      : null;
  const installLabel = isInstalling
    ? percent !== null
      ? t("settings.ffmpegDownloading", "下载中 {{percent}}%", { percent })
      : progress
        ? t("settings.ffmpegDownloadingBytes", "已下载 {{size}}", {
            size: formatBytes(progress.transferred),
          })
        : t("settings.ffmpegInstalling", "安装中…")
    : status?.source === "managed"
      ? t("settings.ffmpegReinstall", "更新内置版")
      : status?.installed
        ? t("settings.ffmpegInstallManaged", "安装内置版")
        : t("settings.ffmpegInstall", "一键安装");

  return (
    <>
      <SettingItem
        label={t("settings.ffmpegEngine", "ffmpeg 引擎")}
        description={statusText}
      >
        <div className="flex shrink-0 items-center gap-2">
          {isProbing ? (
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
            <InfoIcon className="h-4 w-4 text-muted-foreground" aria-hidden="true" />
          )}
          <button
            type="button"
            onClick={() => void install()}
            disabled={isInstalling}
            className={`inline-flex h-9 items-center gap-1.5 rounded-lg px-3 text-sm font-medium transition-colors disabled:opacity-60 ${
              status?.installed
                ? "border border-border text-foreground hover:bg-muted/60"
                : "bg-primary text-primary-foreground hover:bg-primary/90"
            }`}
          >
            {isInstalling ? (
              <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : status?.source === "managed" ? (
              <RefreshCwIcon className="h-4 w-4" aria-hidden="true" />
            ) : (
              <DownloadIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {installLabel}
          </button>
          {status?.source === "managed" && !isInstalling ? (
            <button
              type="button"
              onClick={() => void removeManaged()}
              title={t("settings.ffmpegRemove", "移除内置版")}
              aria-label={t("settings.ffmpegRemove", "移除内置版")}
              className="inline-flex h-9 w-9 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive"
            >
              <Trash2Icon className="h-4 w-4" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      </SettingItem>

      {isInstalling && percent !== null ? (
        <div className="px-4 pb-3">
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
            <div
              className="h-full rounded-full bg-primary transition-all duration-200"
              style={{ width: `${percent}%` }}
            />
          </div>
        </div>
      ) : null}

      <SettingItem
        label={t("settings.ffmpegCustomPath", "ffmpeg 自定义路径（高级）")}
        description={t(
          "settings.ffmpegCustomPathDesc",
          "指定后优先于内置版与系统 PATH；请通过右侧按钮选择文件，留空自动选择",
        )}
      >
        <div className="flex shrink-0 items-center gap-2">
          {/* 只读：该路径会被主进程 spawn，只接受文件选择器返回的值 */}
          <Input
            value={ffmpegPath}
            readOnly
            title={ffmpegPath}
            placeholder={t(
              "settings.ffmpegCustomPathPlaceholder",
              "留空自动选择",
            )}
            className="w-56"
          />
          <button
            type="button"
            onClick={() => void pickBinary()}
            title={t("settings.ffmpegBrowse", "选择文件")}
            aria-label={t("settings.ffmpegBrowse", "选择文件")}
            className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
          >
            <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
          </button>
          {ffmpegPath ? (
            <button
              type="button"
              onClick={() => setFfmpegPath("")}
              title={t("settings.ffmpegClearPath", "清除自定义路径")}
              aria-label={t("settings.ffmpegClearPath", "清除自定义路径")}
              className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
            >
              <XIcon className="h-4 w-4" aria-hidden="true" />
            </button>
          ) : null}
        </div>
      </SettingItem>
    </>
  );
}
