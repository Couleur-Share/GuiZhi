import { useCallback, useEffect, useState } from "react";
import {
  CheckCircle2Icon,
  DownloadIcon,
  FolderOpenIcon,
  Loader2Icon,
  RefreshCwIcon,
  Trash2Icon,
  TriangleAlertIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { YtDlpDownloadProgress, YtDlpStatus } from "@guizhi/shared/types";
import { useSettingsStore } from "../../stores/settings.store";
import { SettingItem } from "./shared";
import { Input } from "../ui/Input";
import { useToast } from "../ui/Toast";

const STATUS_PROBE_DEBOUNCE_MS = 600;

function formatBytes(bytes: number): string {
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * yt-dlp 引擎管理：状态展示（来源 / 版本）、一键安装（带下载进度）、
 * 更新 / 移除托管版、自定义路径（高级选项）。
 */
export function YtDlpSettings() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const ytDlpPath = useSettingsStore((state) => state.ytDlpPath);
  const setYtDlpPath = useSettingsStore((state) => state.setYtDlpPath);

  const [status, setStatus] = useState<YtDlpStatus | null>(null);
  const [isProbing, setIsProbing] = useState(true);
  const [isInstalling, setIsInstalling] = useState(false);
  const [progress, setProgress] = useState<YtDlpDownloadProgress | null>(null);

  const refreshStatus = useCallback(async () => {
    setIsProbing(true);
    try {
      const next = await window.api.ytdlp.status();
      setStatus(next);
    } catch (error) {
      console.error("获取 yt-dlp 状态失败:", error);
    } finally {
      setIsProbing(false);
    }
  }, []);

  // 自定义路径变化后防抖重新探测（探测会真实运行 --version）
  useEffect(() => {
    const timer = setTimeout(() => {
      void refreshStatus();
    }, STATUS_PROBE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [refreshStatus, ytDlpPath]);

  // 下载进度事件
  useEffect(() => {
    const handleProgress = (payload: YtDlpDownloadProgress) => {
      setProgress(payload);
    };
    window.api.on?.("ytdlp:downloadProgress", handleProgress);
    return () => {
      window.api.off?.("ytdlp:downloadProgress", handleProgress);
    };
  }, []);

  const install = async () => {
    if (isInstalling) {
      return;
    }
    setIsInstalling(true);
    setProgress(null);
    try {
      const result = await window.api.ytdlp.install();
      if (result.success) {
        showToast(
          t("settings.ytDlpInstallDone", "yt-dlp 安装完成（v{{version}}），可在导入任务中重试解析", {
            version: result.version ?? "",
          }),
          "success",
        );
        await refreshStatus();
      } else {
        showToast(
          t("settings.ytDlpInstallFailed", "安装失败：{{message}}", {
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
    await window.api.ytdlp.remove();
    showToast(t("settings.ytDlpRemoved", "已移除内置版 yt-dlp"), "success");
    await refreshStatus();
  };

  const pickBinary = async () => {
    const picked = await window.api.ytdlp.pickBinary();
    if (picked) {
      setYtDlpPath(picked);
    }
  };

  const sourceLabel =
    status?.source === "managed"
      ? t("settings.ytDlpSourceManaged", "内置版")
      : status?.source === "path"
        ? t("settings.ytDlpSourcePath", "系统 PATH")
        : status?.source === "custom"
          ? t("settings.ytDlpSourceCustom", "自定义路径")
          : "";

  const statusText = isProbing
    ? t("settings.ytDlpProbing", "检测中…")
    : status?.installed
      ? t("settings.ytDlpStatusReady", "已就绪 · v{{version}} · {{source}}", {
          version: status.version ?? "?",
          source: sourceLabel,
        })
      : ytDlpPath.trim()
        ? t("settings.ytDlpCustomInvalid", "自定义路径无法运行，请检查或改用一键安装")
        : t("settings.ytDlpStatusMissing", "未安装——在线视频（B 站 / YouTube 等）解析需要它");

  const percent =
    progress && progress.total
      ? Math.min(100, Math.round((progress.transferred / progress.total) * 100))
      : null;
  const installLabel = isInstalling
    ? percent !== null
      ? t("settings.ytDlpDownloading", "下载中 {{percent}}%", { percent })
      : progress
        ? t("settings.ytDlpDownloadingBytes", "已下载 {{size}}", {
            size: formatBytes(progress.transferred),
          })
        : t("settings.ytDlpInstalling", "安装中…")
    : status?.source === "managed"
      ? t("settings.ytDlpReinstall", "更新内置版")
      : status?.installed
        ? t("settings.ytDlpInstallManaged", "安装内置版")
        : t("settings.ytDlpInstall", "一键安装");

  return (
    <>
      <SettingItem
        label={t("settings.ytDlpEngine", "yt-dlp 引擎")}
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
            <TriangleAlertIcon
              className="h-4 w-4 text-amber-500"
              aria-hidden="true"
            />
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
              title={t("settings.ytDlpRemove", "移除内置版")}
              aria-label={t("settings.ytDlpRemove", "移除内置版")}
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
        label={t("settings.ytDlpCustomPath", "自定义路径（高级）")}
        description={t(
          "settings.ytDlpCustomPathDesc",
          "指定后优先于内置版与系统 PATH；留空使用内置版",
        )}
      >
        <div className="flex shrink-0 items-center gap-2">
          <Input
            value={ytDlpPath}
            onChange={(event) => setYtDlpPath(event.target.value)}
            placeholder={t("settings.ytDlpCustomPathPlaceholder", "留空使用内置版")}
            className="w-56"
          />
          <button
            type="button"
            onClick={() => void pickBinary()}
            title={t("settings.ytDlpBrowse", "选择文件")}
            aria-label={t("settings.ytDlpBrowse", "选择文件")}
            className="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-border text-muted-foreground transition-colors hover:bg-muted/60 hover:text-foreground"
          >
            <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
      </SettingItem>
    </>
  );
}
