import { useCallback, useEffect, useState } from "react";
import {
  ArchiveRestoreIcon,
  DatabaseBackupIcon,
  FileDownIcon,
  FolderOpenIcon,
  Loader2Icon,
  ScrollTextIcon,
  Trash2Icon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { BackupFileInfo, BackupKind } from "@guizhi/shared/types";
import { useSettingsStore } from "../../stores/settings.store";
import { SettingSection, SettingItem, ToggleSwitch } from "./shared";
import { Select } from "../ui/Select";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Spinner } from "../ui/Spinner";
import { useToast } from "../ui/Toast";

type BusyAction = "create" | "restore" | "export" | null;

type ConfirmState =
  | { kind: "restore"; backup: BackupFileInfo }
  | { kind: "delete"; backup: BackupFileInfo }
  | null;

const BACKUP_KIND_STYLES: Record<BackupKind, string> = {
  manual: "bg-primary/10 text-primary",
  auto: "bg-muted text-muted-foreground",
  "pre-update": "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  "pre-restore": "bg-violet-500/15 text-violet-600 dark:text-violet-400",
};

function formatBytes(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  if (bytes < 1024 * 1024) {
    return `${(bytes / 1024).toFixed(1)} KB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDateTime(timestamp: number): string {
  return new Date(timestamp).toLocaleString(undefined, {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function BackupKindBadge({ kind }: { kind: BackupKind }) {
  const { t } = useTranslation();
  const labels: Record<BackupKind, string> = {
    manual: t("settings.backupKindManual", "手动"),
    auto: t("settings.backupKindAuto", "自动"),
    "pre-update": t("settings.backupKindPreUpdate", "升级前"),
    "pre-restore": t("settings.backupKindPreRestore", "恢复前"),
  };
  return (
    <span
      className={`shrink-0 rounded-full px-2 py-0.5 text-[11px] ${BACKUP_KIND_STYLES[kind]}`}
    >
      {labels[kind]}
    </span>
  );
}

/**
 * 数据设置：本地备份（手动 + 定时自动）、从备份恢复、Markdown 导出。
 */
export function DataSettings() {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const backupAutoEnabled = useSettingsStore((s) => s.backupAutoEnabled);
  const backupIntervalHours = useSettingsStore((s) => s.backupIntervalHours);
  const backupKeepCount = useSettingsStore((s) => s.backupKeepCount);
  const setBackupAutoEnabled = useSettingsStore((s) => s.setBackupAutoEnabled);
  const setBackupIntervalHours = useSettingsStore(
    (s) => s.setBackupIntervalHours,
  );
  const setBackupKeepCount = useSettingsStore((s) => s.setBackupKeepCount);

  const [backups, setBackups] = useState<BackupFileInfo[]>([]);
  const [isLoadingBackups, setIsLoadingBackups] = useState(true);
  const [busy, setBusy] = useState<BusyAction>(null);
  const [confirmState, setConfirmState] = useState<ConfirmState>(null);
  const [backupsDir, setBackupsDir] = useState<string | null>(null);
  const [logsDir, setLogsDir] = useState<string | null>(null);

  const reloadBackups = useCallback(async () => {
    setIsLoadingBackups(true);
    try {
      const list = await window.api.backup.list();
      setBackups(list);
    } catch (error) {
      // 读不出来会被渲染成「还没有备份」，用户可能因此以为定时备份没在跑
      showToast(
        t("settings.backupListFailed", "读取备份列表失败"),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    } finally {
      setIsLoadingBackups(false);
    }
  }, [showToast, t]);

  useEffect(() => {
    void reloadBackups();
    void window.electron
      ?.getRuntimePaths?.()
      .then((paths) => {
        setBackupsDir(paths.backupsDir);
        setLogsDir(paths.logsDir);
      })
      .catch(() => {});
  }, [reloadBackups]);

  const handleCreateBackup = async () => {
    setBusy("create");
    try {
      const result = await window.api.backup.create();
      if (result.success) {
        showToast(t("settings.backupCreated", "备份已创建"), "success");
        await reloadBackups();
      } else {
        showToast(
          result.error ?? t("settings.backupFailed", "备份失败"),
          "error",
        );
      }
    } finally {
      setBusy(null);
    }
  };

  const handleRestore = async (backup: BackupFileInfo) => {
    setBusy("restore");
    try {
      const result = await window.api.backup.restore(backup.fileName);
      if (result.success) {
        showToast(
          t("settings.backupRestoring", "恢复完成，应用即将重启…"),
          "success",
        );
        // 主进程随后 relaunch；保持 busy 状态直到重启
        return;
      }
      showToast(
        result.error ?? t("settings.backupRestoreFailed", "恢复失败"),
        "error",
      );
      setBusy(null);
    } catch (error) {
      showToast(error instanceof Error ? error.message : String(error), "error");
      setBusy(null);
    }
  };

  const handleDelete = async (backup: BackupFileInfo) => {
    const result = await window.api.backup.delete(backup.fileName);
    if (!result.success) {
      showToast(
        t("settings.backupDeleteFailed", "删除失败"),
        "error",
        result.error ? { detail: result.error } : undefined,
      );
    }
    await reloadBackups();
  };

  const handleExportMarkdown = async () => {
    setBusy("export");
    try {
      const result = await window.api.backup.exportMarkdown();
      if (result.canceled) {
        return;
      }
      if (result.success && result.dir) {
        showToast(
          t("settings.exportDone", "已导出 {{count}} 个条目", {
            count: result.count ?? 0,
          }),
          "success",
        );
        void window.electron?.openPath?.(result.dir);
      } else {
        showToast(
          result.error ?? t("settings.exportFailed", "导出失败"),
          "error",
        );
      }
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="space-y-6" data-testid="data-settings">
      {/* 自动备份 */}
      <SettingSection title={t("settings.backupAutoSection", "自动备份")}>
        <SettingItem
          label={t("settings.backupAuto", "定时自动备份")}
          description={t(
            "settings.backupAutoDesc",
            "按设定间隔自动备份知识库到本地备份目录",
          )}
        >
          <ToggleSwitch
            ariaLabel={t("settings.backupAuto", "定时自动备份")}
            checked={backupAutoEnabled}
            onChange={setBackupAutoEnabled}
          />
        </SettingItem>
        <SettingItem
          label={t("settings.backupInterval", "备份间隔")}
          description={t(
            "settings.backupIntervalDesc",
            "应用运行期间到期自动执行",
          )}
        >
          <Select
            ariaLabel={t("settings.backupInterval", "备份间隔")}
            value={String(backupIntervalHours)}
            onChange={(value) => setBackupIntervalHours(Number(value))}
            options={[
              { value: "12", label: t("settings.backupInterval12h", "12 小时") },
              { value: "24", label: t("settings.backupInterval24h", "每天") },
              { value: "72", label: t("settings.backupInterval72h", "每 3 天") },
              { value: "168", label: t("settings.backupInterval168h", "每周") },
            ]}
            className="w-32"
          />
        </SettingItem>
        <SettingItem
          label={t("settings.backupKeep", "保留数量")}
          description={t(
            "settings.backupKeepDesc",
            "超出数量的自动备份会被清理，手动备份不受影响",
          )}
        >
          <Select
            ariaLabel={t("settings.backupKeep", "保留数量")}
            value={String(backupKeepCount)}
            onChange={(value) => setBackupKeepCount(Number(value))}
            options={[
              { value: "5", label: "5" },
              { value: "10", label: "10" },
              { value: "20", label: "20" },
              { value: "30", label: "30" },
            ]}
            className="w-32"
          />
        </SettingItem>
      </SettingSection>

      {/* 备份列表 */}
      <SettingSection title={t("settings.backupSection", "本地备份")}>
        <div className="flex items-center gap-2 px-4 py-3 border-b border-border/70">
          <button
            type="button"
            onClick={() => void handleCreateBackup()}
            disabled={busy !== null}
            data-testid="backup-create"
            className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
          >
            {busy === "create" ? (
              <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <DatabaseBackupIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {t("settings.backupNow", "立即备份")}
          </button>
          <button
            type="button"
            onClick={() => backupsDir && void window.electron?.openPath?.(backupsDir)}
            disabled={!backupsDir}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60 disabled:opacity-50"
          >
            <FolderOpenIcon className="h-4 w-4" aria-hidden="true" />
            {t("settings.openBackupsDir", "打开备份目录")}
          </button>
          {/* 后台任务（定时备份、后台 Wiki 编译）失败时不弹窗，只记进 error.log；
              这个按钮是它们唯一的出口 */}
          <button
            type="button"
            onClick={() => logsDir && void window.electron?.openPath?.(logsDir)}
            disabled={!logsDir}
            title={t(
              "settings.openLogsHint",
              "后台任务失败会记在这里的 error.log",
            )}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60 disabled:opacity-50"
          >
            <ScrollTextIcon className="h-4 w-4" aria-hidden="true" />
            {t("settings.openLogs", "打开日志")}
          </button>
        </div>

        {isLoadingBackups ? (
          <div className="flex h-24 items-center justify-center">
            <Spinner size="sm" tone="muted" />
          </div>
        ) : backups.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm text-muted-foreground">
            {t("settings.backupEmpty", "还没有备份。点击「立即备份」创建第一份。")}
          </p>
        ) : (
          <ul className="max-h-72 overflow-y-auto">
            {backups.map((backup) => (
              <li
                key={backup.fileName}
                className="flex items-center gap-3 border-b border-border/50 px-4 py-2.5 last:border-0"
              >
                <BackupKindBadge kind={backup.kind} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm text-foreground">
                    {formatDateTime(backup.createdAt)}
                  </p>
                  <p className="truncate text-xs text-muted-foreground">
                    {backup.fileName} · {formatBytes(backup.sizeBytes)}
                  </p>
                </div>
                <button
                  type="button"
                  onClick={() => setConfirmState({ kind: "restore", backup })}
                  disabled={busy !== null}
                  className="inline-flex h-8 items-center gap-1 rounded-lg border border-border px-2.5 text-xs text-foreground transition-colors hover:bg-muted/60 disabled:opacity-50"
                >
                  <ArchiveRestoreIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  {t("settings.backupRestore", "恢复")}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmState({ kind: "delete", backup })}
                  disabled={busy !== null}
                  title={t("settings.backupDelete", "删除")}
                  aria-label={t("settings.backupDelete", "删除")}
                  className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
                >
                  <Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" />
                </button>
              </li>
            ))}
          </ul>
        )}
      </SettingSection>

      {/* 导出 */}
      <SettingSection title={t("settings.exportSection", "导出")}>
        <SettingItem
          label={t("settings.exportMarkdown", "导出为 Markdown")}
          description={t(
            "settings.exportMarkdownDesc",
            "全部条目导出为带元信息的 .md 文件，按知识库分文件夹，可导入 Obsidian 等工具",
          )}
        >
          <button
            type="button"
            onClick={() => void handleExportMarkdown()}
            disabled={busy !== null}
            className="inline-flex h-9 items-center gap-1.5 rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60 disabled:opacity-50"
          >
            {busy === "export" ? (
              <Loader2Icon className="h-4 w-4 animate-spin" aria-hidden="true" />
            ) : (
              <FileDownIcon className="h-4 w-4" aria-hidden="true" />
            )}
            {t("settings.exportStart", "选择目录并导出")}
          </button>
        </SettingItem>
      </SettingSection>

      <ConfirmDialog
        isOpen={confirmState !== null}
        onClose={() => setConfirmState(null)}
        onConfirm={() => {
          if (confirmState?.kind === "restore") {
            void handleRestore(confirmState.backup);
          } else if (confirmState?.kind === "delete") {
            void handleDelete(confirmState.backup);
          }
          setConfirmState(null);
        }}
        title={
          confirmState?.kind === "restore"
            ? t("settings.backupRestoreConfirmTitle", "从备份恢复")
            : t("settings.backupDelete", "删除")
        }
        message={
          confirmState?.kind === "restore"
            ? t(
                "settings.backupRestoreConfirmMessage",
                "将用 {{time}} 的备份替换当前全部数据，替换前会自动保存一份当前数据的快照。恢复完成后应用会自动重启。",
                {
                  time: confirmState
                    ? formatDateTime(confirmState.backup.createdAt)
                    : "",
                },
              )
            : t(
                "settings.backupDeleteConfirmMessage",
                "删除备份 {{name}}？此操作无法撤销。",
                { name: confirmState?.backup.fileName ?? "" },
              )
        }
        confirmText={t("common.confirm", "确认")}
        cancelText={t("common.cancel", "取消")}
        variant="destructive"
      />
    </div>
  );
}
