import { useCallback, useEffect, useState } from "react";
import {
  ArchiveRestoreIcon,
  DatabaseBackupIcon,
  DownloadIcon,
  KeyRoundIcon,
  Loader2Icon,
  ShieldCheckIcon,
  Trash2Icon,
} from "lucide-react";
import type {
  BackupFileInfo,
  BackupRepositoryStatus,
  BackupRestorePreview,
} from "@guizhi/shared/types";
import { SettingSection } from "./shared";
import { Input } from "../ui/Input";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Spinner } from "../ui/Spinner";
import { useToast } from "../ui/Toast";

function formatBytes(bytes: number): string {
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function formatDate(timestamp: number): string {
  return new Date(timestamp).toLocaleString();
}

type Busy = "init" | "create" | "preview" | "restore" | "export" | "password" | null;

export function RepositoryBackupSection() {
  const { showToast } = useToast();
  const [status, setStatus] = useState<BackupRepositoryStatus | null>(null);
  const [snapshots, setSnapshots] = useState<BackupFileInfo[]>([]);
  const [busy, setBusy] = useState<Busy>(null);
  const [password, setPassword] = useState("");
  const [passwordConfirm, setPasswordConfirm] = useState("");
  const [unlockPassword, setUnlockPassword] = useState("");
  const [currentPassword, setCurrentPassword] = useState("");
  const [nextPassword, setNextPassword] = useState("");
  const [preview, setPreview] = useState<BackupRestorePreview | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<BackupFileInfo | null>(null);

  const reload = useCallback(async () => {
    const [nextStatus, nextSnapshots] = await Promise.all([
      window.api.backup.repositoryStatus(),
      window.api.backup.listRepositorySnapshots(),
    ]);
    setStatus(nextStatus);
    setSnapshots(nextSnapshots);
  }, []);

  useEffect(() => {
    void reload().catch((error) =>
      showToast("读取完整备份仓库失败", "error", {
        detail: error instanceof Error ? error.message : String(error),
      }),
    );
  }, [reload, showToast]);

  const initialize = async () => {
    if (password !== passwordConfirm) {
      showToast("两次输入的恢复口令不一致", "error");
      return;
    }
    setBusy("init");
    const result = await window.api.backup.initializeRepository(password);
    setBusy(null);
    if (!result.success) {
      showToast(result.error || "初始化备份仓库失败", "error");
      return;
    }
    setPassword("");
    setPasswordConfirm("");
    showToast("完整备份仓库已启用", "success");
    await reload();
  };

  const createSnapshot = async () => {
    setBusy("create");
    const result = await window.api.backup.createRepositorySnapshot({
      recoveryPassword: unlockPassword || undefined,
    });
    setBusy(null);
    if (!result.success) {
      showToast(result.error || "完整备份失败", "error");
      return;
    }
    showToast(
      `完整备份已创建，新增 ${result.createdObjects ?? 0} 个对象，复用 ${result.reusedObjects ?? 0} 个对象`,
      "success",
    );
    await reload();
  };

  const openPreview = async (snapshot: BackupFileInfo) => {
    setBusy("preview");
    const result = await window.api.backup.previewRepositoryRestore({
      snapshotId: snapshot.fileName,
      recoveryPassword: unlockPassword || undefined,
    });
    setBusy(null);
    if (!result.success) {
      showToast(result.error || "恢复预检失败", "error", {
        detail: [
          ...result.missingFiles.map((name) => `缺失：${name}`),
          ...result.damagedFiles.map((name) => `损坏：${name}`),
        ].join("\n"),
      });
      return;
    }
    setPreview(result);
  };

  const restore = async () => {
    if (!preview?.snapshot) return;
    setBusy("restore");
    const result = await window.api.backup.restoreRepositorySnapshot({
      snapshotId: preview.snapshot.fileName,
      recoveryPassword: unlockPassword || undefined,
    });
    if (!result.success) {
      setBusy(null);
      showToast(result.error || "恢复失败", "error");
      return;
    }
    showToast("完整恢复完成，应用即将重启…", "success");
  };

  const exportPortable = async (snapshot: BackupFileInfo) => {
    setBusy("export");
    const result = await window.api.backup.exportPortable({
      snapshotId: snapshot.fileName,
      recoveryPassword: unlockPassword || undefined,
    });
    setBusy(null);
    if (!result.success && !result.canceled) {
      showToast(result.error || "便携备份导出失败", "error");
    } else if (result.success) {
      showToast("便携备份已导出", "success");
    }
  };

  const changePassword = async () => {
    setBusy("password");
    const result = await window.api.backup.changeRepositoryPassword({
      currentPassword,
      nextPassword,
    });
    setBusy(null);
    if (!result.success) {
      showToast(result.error || "修改恢复口令失败", "error");
      return;
    }
    setCurrentPassword("");
    setNextPassword("");
    showToast("恢复口令已修改；媒体对象无需重写", "success");
  };

  if (!status) {
    return (
      <SettingSection title="自动完整备份">
        <div className="flex h-24 items-center justify-center"><Spinner size="sm" tone="muted" /></div>
      </SettingSection>
    );
  }

  if (!status.initialized) {
    return (
      <SettingSection title="自动完整备份">
        <div className="space-y-4 p-4">
          <div className="flex gap-3 rounded-xl border border-primary/20 bg-primary/5 p-4">
            <ShieldCheckIcon className="mt-0.5 h-5 w-5 shrink-0 text-primary" aria-hidden="true" />
            <div>
              <p className="text-sm font-medium text-foreground">知识、媒体和配置一起备份</p>
              <p className="mt-1 text-sm text-muted-foreground">
                媒体按内容去重并全部加密。恢复口令不会保存在设备上；丢失口令且系统密钥环不可用时，备份无法恢复。
              </p>
            </div>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            <Input label="设置恢复口令" type="password" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" />
            <Input label="再次输入" type="password" value={passwordConfirm} onChange={(event) => setPasswordConfirm(event.target.value)} autoComplete="new-password" />
          </div>
          <button type="button" disabled={busy !== null} onClick={() => void initialize()} className="inline-flex h-9 items-center gap-2 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50">
            {busy === "init" ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <KeyRoundIcon className="h-4 w-4" />}
            启用完整备份
          </button>
        </div>
      </SettingSection>
    );
  }

  return (
    <>
      <SettingSection title="自动完整备份">
        <div className="space-y-3 p-4">
          <div className={`rounded-lg border px-3 py-2 text-sm ${status.automaticAccessAvailable ? "border-emerald-500/20 bg-emerald-500/5 text-emerald-700 dark:text-emerald-300" : "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"}`}>
            {status.automaticAccessAvailable
              ? `系统密钥环已就绪（${status.keyStorageBackend ?? "secure storage"}），可无人值守备份。`
              : status.warning}
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <Input className="w-56" aria-label="恢复口令（需要时）" placeholder="恢复口令（换机或密钥环不可用时）" type="password" value={unlockPassword} onChange={(event) => setUnlockPassword(event.target.value)} />
            <button type="button" disabled={busy !== null} onClick={() => void createSnapshot()} className="inline-flex h-10 items-center gap-2 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground disabled:opacity-50">
              {busy === "create" ? <Loader2Icon className="h-4 w-4 animate-spin" /> : <DatabaseBackupIcon className="h-4 w-4" />}
              立即完整备份
            </button>
          </div>
        </div>
        {snapshots.length === 0 ? (
          <p className="border-t border-border/60 px-4 py-6 text-center text-sm text-muted-foreground">还没有完整快照。</p>
        ) : (
          <ul className="max-h-80 overflow-y-auto border-t border-border/60">
            {snapshots.map((snapshot) => (
              <li key={snapshot.fileName} className="flex items-center gap-3 border-b border-border/50 px-4 py-3 last:border-0">
                <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs text-primary">{snapshot.kind === "auto" ? "自动" : "手动"}</span>
                <div className="min-w-0 flex-1">
                  <p className="text-sm text-foreground">{formatDate(snapshot.createdAt)}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {snapshot.summary?.itemCount ?? 0} 条 · {snapshot.summary?.assetCount ?? 0} 个媒体 · {formatBytes(snapshot.sizeBytes)} · schema v{snapshot.summary?.schemaVersion ?? 0}
                  </p>
                </div>
                <button type="button" disabled={busy !== null} onClick={() => void exportPortable(snapshot)} title="导出便携备份" aria-label="导出便携备份" className="rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground"><DownloadIcon className="h-4 w-4" /></button>
                <button type="button" disabled={busy !== null} onClick={() => void openPreview(snapshot)} className="inline-flex h-8 items-center gap-1 rounded-lg border border-border px-2.5 text-xs hover:bg-muted"><ArchiveRestoreIcon className="h-3.5 w-3.5" />预检并恢复</button>
                <button type="button" disabled={busy !== null} onClick={() => setDeleteTarget(snapshot)} title="删除快照" aria-label="删除快照" className="rounded-lg p-2 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"><Trash2Icon className="h-4 w-4" /></button>
              </li>
            ))}
          </ul>
        )}
        <details className="border-t border-border/60 p-4">
          <summary className="cursor-pointer text-sm font-medium text-foreground">修改恢复口令</summary>
          <div className="mt-3 flex flex-wrap items-end gap-2">
            <Input className="w-52" label="当前口令" type="password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} />
            <Input className="w-52" label="新口令（至少 12 字符）" type="password" value={nextPassword} onChange={(event) => setNextPassword(event.target.value)} />
            <button type="button" disabled={busy !== null} onClick={() => void changePassword()} className="h-10 rounded-lg border border-border px-3 text-sm hover:bg-muted disabled:opacity-50">确认修改</button>
          </div>
        </details>
      </SettingSection>

      <ConfirmDialog
        isOpen={preview !== null}
        onClose={() => setPreview(null)}
        onConfirm={() => void restore()}
        title="确认完整恢复"
        message={preview?.snapshot ? (
          <span>
            将恢复 {preview.snapshot.summary?.itemCount ?? 0} 条知识、{preview.snapshot.summary?.assetCount ?? 0} 个媒体和 {preview.snapshot.summary?.configDomains.length ?? 0} 个配置域。预检已通过；当前机器路径与启动偏好会保留。
          </span>
        ) : ""}
        confirmText="恢复并重启"
        cancelText="取消"
        variant="destructive"
        isLoading={busy === "restore"}
      />
      <ConfirmDialog
        isOpen={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => {
          if (!deleteTarget) return;
          void window.api.backup.deleteRepositorySnapshot(deleteTarget.fileName).then(async (result) => {
            if (!result.success) showToast(result.error || "删除失败", "error");
            setDeleteTarget(null);
            await reload();
          });
        }}
        title="删除完整快照"
        message="将删除该快照的清单，并回收没有被其他快照引用的加密对象。此操作无法撤销。"
        confirmText="删除"
        cancelText="取消"
        variant="destructive"
      />
    </>
  );
}
