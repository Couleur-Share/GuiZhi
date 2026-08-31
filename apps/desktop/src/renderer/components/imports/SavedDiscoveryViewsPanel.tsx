import { useCallback, useEffect, useState } from "react";
import {
  CalendarClockIcon,
  Loader2Icon,
  PlayIcon,
  SaveIcon,
  Trash2Icon,
  XIcon,
} from "lucide-react";
import type {
  DiscoveryIntervalMinutes,
  DiscoveryView,
  DiscoveryViewDetail,
  PlatformCapturePlatform,
} from "@guizhi/shared/types";
import { useImportStore } from "../../stores/import.store";
import { useInboxStore } from "../../stores/inbox.store";
import { useSettingsStore } from "../../stores/settings.store";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";

const INTERVAL_OPTIONS: Array<{ value: DiscoveryIntervalMinutes; label: string }> = [
  { value: 360, label: "每 6 小时" },
  { value: 720, label: "每 12 小时" },
  { value: 1440, label: "每天" },
  { value: 4320, label: "每 3 天" },
  { value: 10080, label: "每周" },
];

interface Props {
  platform: PlatformCapturePlatform;
  mode: "creator" | "search";
  query: string;
  initialViewId?: string | null;
}

export function SavedDiscoveryViewsPanel({
  platform,
  mode,
  query,
  initialViewId,
}: Props) {
  const { showToast } = useToast();
  const enqueue = useImportStore((state) => state.enqueue);
  const backgroundEnabled = useSettingsStore((state) => state.backgroundTasksEnabled);
  const setBackgroundEnabled = useSettingsStore((state) => state.setBackgroundTasksEnabled);
  const [views, setViews] = useState<DiscoveryView[]>([]);
  const [detail, setDetail] = useState<DiscoveryViewDetail | null>(null);
  const [name, setName] = useState("");
  const [interval, setIntervalValue] = useState<DiscoveryIntervalMinutes>(1440);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [authorizePending, setAuthorizePending] = useState(false);
  const [deletePending, setDeletePending] = useState<DiscoveryView | null>(null);

  const refresh = useCallback(async (selectedId?: string | null) => {
    const next = await window.api.platformCapture.listDiscoveryViews();
    setViews(next);
    const id = selectedId ?? initialViewId;
    if (id) setDetail(await window.api.platformCapture.getDiscoveryView(id));
  }, [initialViewId]);

  useEffect(() => {
    void refresh(initialViewId).catch((cause) =>
      setError(cause instanceof Error ? cause.message : String(cause)),
    );
  }, [initialViewId, refresh]);

  const save = async (enabled: boolean, authorizationGranted = false) => {
    const trimmed = query.trim();
    if (!trimmed) {
      setError("请先填写作者主页或关键词");
      return;
    }
    if (enabled && !backgroundEnabled && !authorizationGranted) {
      setAuthorizePending(true);
      return;
    }
    setBusy("save");
    setError(null);
    try {
      const saved = await window.api.platformCapture.saveDiscoveryView({
        name: name.trim() || trimmed.slice(0, 40),
        platform,
        mode: mode === "search" ? "keyword" : "creator",
        query: trimmed,
        intervalMinutes: interval,
        enabled,
      });
      setName("");
      await refresh(saved.id);
      showToast(enabled ? "定时发现已启用" : "手动发现视图已保存", "success");
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setBusy(null);
    }
  };

  const authorizeAndSave = async () => {
    setAuthorizePending(false);
    setBackgroundEnabled(true);
    // 主进程是权限判定方；等待设置落库后再创建任务，避免竞态留下半套状态。
    await window.api.settings.set({
      backgroundTasksEnabled: true,
      launchAtStartup: true,
      minimizeOnLaunch: true,
    });
    await save(true, true);
  };

  const run = async (view: DiscoveryView) => {
    setBusy(view.id);
    setError(null);
    try {
      if (view.state === "login_required") {
        await window.api.platformCapture.login(view.platform);
        await window.api.platformCapture.resumeDiscoveryAfterLogin(view.id);
      }
      const result = await window.api.platformCapture.runDiscoveryView(view.id);
      await refresh(view.id);
      showToast(`本轮新增 ${result.newCandidates} 条候选`, "success");
      void useInboxStore.getState().refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause));
      await refresh(view.id).catch(() => undefined);
    } finally {
      setBusy(null);
    }
  };

  const importCandidate = async (externalId: string) => {
    const candidate = detail?.candidates.find((entry) => entry.externalId === externalId);
    if (!candidate) return;
    setBusy(externalId);
    try {
      await enqueue([{
        kind: "url",
        input: candidate.item.url,
        captureStrategy: "authenticated",
        commentLimit: 0,
      }]);
      await window.api.platformCapture.setDiscoveryCandidateState(
        candidate.item.platform,
        externalId,
        "imported",
      );
      await refresh(detail.view.id);
      void useInboxStore.getState().refresh();
    } finally {
      setBusy(null);
    }
  };

  const dismissCandidate = async (externalId: string) => {
    const candidate = detail?.candidates.find((entry) => entry.externalId === externalId);
    if (!candidate) return;
    await window.api.platformCapture.setDiscoveryCandidateState(
      candidate.item.platform,
      externalId,
      "dismissed",
    );
    await refresh(detail.view.id);
    void useInboxStore.getState().refresh();
  };

  return (
    <section className="rounded-xl border border-border bg-background/55 p-4" aria-label="保存的采集视图">
      <div className="flex flex-wrap items-center gap-2">
        <CalendarClockIcon className="h-4 w-4 text-primary" aria-hidden="true" />
        <h3 className="text-sm font-semibold">保存的采集视图</h3>
        <span className="text-xs text-muted-foreground">定时任务只发现候选，绝不自动入库</span>
      </div>
      <div className="mt-3 flex flex-wrap gap-2">
        <input
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="视图名称（默认使用查询内容）"
          className="h-8 min-w-48 flex-1 rounded-lg border border-border bg-background px-2.5 text-xs"
        />
        <Select
          value={String(interval)}
          onChange={(value) => setIntervalValue(Number(value) as DiscoveryIntervalMinutes)}
          options={INTERVAL_OPTIONS.map((option) => ({
            value: String(option.value),
            label: option.label,
          }))}
          ariaLabel="发现周期"
          className="w-28"
          menuMinWidth={112}
          triggerClassName="flex h-8 w-full items-center justify-between gap-2 rounded-lg border border-border bg-background px-2 text-left text-xs"
        />
        <button type="button" disabled={busy === "save"} onClick={() => void save(false)} className="inline-flex h-8 items-center gap-1 rounded-lg border border-border px-3 text-xs">
          <SaveIcon className="h-3.5 w-3.5" aria-hidden="true" />保存为手动视图
        </button>
        <button type="button" disabled={busy === "save"} onClick={() => void save(true)} className="inline-flex h-8 items-center gap-1 rounded-lg bg-primary px-3 text-xs text-primary-foreground">
          <CalendarClockIcon className="h-3.5 w-3.5" aria-hidden="true" />启用定时发现
        </button>
      </div>
      {error ? <p className="mt-2 text-xs text-destructive">{error}</p> : null}

      {views.length > 0 ? (
        <div className="mt-4 grid gap-3 lg:grid-cols-[15rem_minmax(0,1fr)]">
          <div className="space-y-1">
            {views.map((view) => (
              <button
                key={view.id}
                type="button"
                onClick={() => void refresh(view.id)}
                className={`flex w-full items-center gap-2 rounded-lg px-2.5 py-2 text-left text-xs ${detail?.view.id === view.id ? "bg-accent" : "hover:bg-muted/60"}`}
              >
                <span className="min-w-0 flex-1 truncate font-medium">{view.name}</span>
                <span className={view.state === "login_required" ? "text-destructive" : "text-muted-foreground"}>
                  {view.state === "login_required" ? "需登录" : view.enabled ? "定时" : "手动"}
                </span>
              </button>
            ))}
          </div>
          {detail ? (
            <div className="min-w-0 rounded-lg border border-border p-3">
              <div className="flex items-center gap-2">
                <strong className="truncate text-sm">{detail.view.name}</strong>
                <span className="min-w-0 flex-1" />
                <button type="button" disabled={busy === detail.view.id} onClick={() => void run(detail.view)} className="inline-flex h-7 items-center gap-1 rounded-md border border-border px-2 text-xs">
                  {busy === detail.view.id ? <Loader2Icon className="h-3 w-3 animate-spin" /> : <PlayIcon className="h-3 w-3" />}
                  {detail.view.state === "login_required" ? "登录并恢复" : "立即运行"}
                </button>
                <button type="button" aria-label="删除视图" onClick={() => setDeletePending(detail.view)} className="rounded-md p-1.5 text-muted-foreground hover:text-destructive">
                  <Trash2Icon className="h-3.5 w-3.5" />
                </button>
              </div>
              <p className="mt-1 truncate text-xs text-muted-foreground">{detail.view.query}</p>
              <div className="mt-3 space-y-2">
                {detail.candidates.filter((entry) => entry.state === "new").length === 0 ? (
                  <p className="text-xs text-muted-foreground">当前没有待处理候选</p>
                ) : detail.candidates.filter((entry) => entry.state === "new").map((candidate) => (
                  <div key={candidate.externalId} className="flex items-center gap-2 rounded-md bg-muted/35 px-2.5 py-2">
                    <span className="min-w-0 flex-1 truncate text-xs">{candidate.item.title || candidate.item.url}</span>
                    <button type="button" disabled={busy === candidate.externalId} onClick={() => void importCandidate(candidate.externalId)} className="text-xs text-primary">导入</button>
                    <button type="button" aria-label="忽略候选" onClick={() => void dismissCandidate(candidate.externalId)} className="text-muted-foreground hover:text-foreground">
                      <XIcon className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
              {detail.runs[0] ? (
                <p className="mt-3 text-xs text-muted-foreground">
                  最近运行：{detail.runs[0].state} · {detail.runs[0].pagesScanned} 页 · 新增 {detail.runs[0].candidatesFound}
                </p>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}

      <ConfirmDialog
        isOpen={authorizePending}
        onClose={() => setAuthorizePending(false)}
        onConfirm={() => void authorizeAndSave()}
        title="允许归知在后台发现内容？"
        message="将同时启用开机启动、隐藏启动、最小化到托盘和关闭后驻留。你仍可从托盘真正退出，退出期间任务不会运行。"
        confirmText="授权并启用"
        cancelText="暂不授权"
      />
      <ConfirmDialog
        isOpen={deletePending !== null}
        onClose={() => setDeletePending(null)}
        onConfirm={() => {
          const view = deletePending;
          setDeletePending(null);
          if (!view) return;
          void window.api.platformCapture.deleteDiscoveryView(view.id).then(() => {
            setDetail(null);
            return refresh(null);
          });
        }}
        title="删除采集视图？"
        message="该视图的运行记录和候选会一并删除，已导入的知识不受影响。"
        confirmText="删除"
        cancelText="取消"
        variant="destructive"
      />
    </section>
  );
}
