import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ThemedReadingTask } from "@guizhi/shared/types/themed-reading";
import { LoadErrorState } from "../ui/LoadErrorState";
import { useToast } from "../ui/Toast";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { patchContentReadingMemory } from "../library/reading-memory";
import { ThemedReadingStatus } from "./ThemedReadingStatus";
import { themeButton } from "./ThemedReadingSetup";

/** 按需生成任务的列表；不与周期后台作业共用重试调度。 */
export function ThemedReadingTasks({ onBack }: { onBack: () => void }) {
  const { t } = useTranslation(), { showToast } = useToast();
  const [tasks, setTasks] = useState<ThemedReadingTask[]>([]);
  const [loading, setLoading] = useState(true), [error, setError] = useState<string | null>(null), [busyId, setBusyId] = useState<string | null>(null);
  const reload = useCallback(async () => {
    setError(null);
    try {
      const result = await window.api.themedReading.listTasks();
      if (!result.success) throw new Error(result.error || "任务读取失败");
      setTasks(result.tasks ?? []);
    } catch (failure) { setError(failure instanceof Error ? failure.message : String(failure)); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void reload(); return window.api.themedReading.onProgress((task) => setTasks((prev) => [task, ...prev.filter((entry) => entry.id !== task.id)].sort((a, b) => b.updatedAt - a.updatedAt))); }, [reload]);
  const action = async (task: ThemedReadingTask, kind: "cancel" | "resume") => {
    setBusyId(task.id);
    try {
      const result = await window.api.themedReading[kind](task.id);
      if (!result.success) throw new Error(result.error || "任务操作失败");
      await reload();
    } catch (failure) { showToast(t("themedReading.taskFailed", "任务操作失败"), "error", { detail: failure instanceof Error ? failure.message : String(failure) }); }
    finally { setBusyId(null); }
  };
  const open = async (task: ThemedReadingTask) => {
    patchContentReadingMemory(task.itemId, { tab: task.sourceKind, themedBySource: { [task.sourceKind]: true } });
    useKnowledgeStore.getState().setScope("all");
    await useKnowledgeStore.getState().selectItem(task.itemId);
    useUIStore.getState().setAppModule("library");
  };
  return <div className="flex h-full min-h-0 flex-1 flex-col app-wallpaper-section">
    <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-5">
      <button className={themeButton} onClick={onBack}>{t("themedReading.backToInbox", "返回处理中心")}</button>
      <h2 className="text-sm font-semibold">{t("themedReading.tasks", "主题页任务")}</h2>
      <button className={`${themeButton} ml-auto`} onClick={() => void reload()}>{t("common.refresh", "刷新")}</button>
    </div>
    {error ? <LoadErrorState message={error} onRetry={() => void reload()} /> : loading ? <p className="p-6 text-sm text-muted-foreground">{t("themedReading.loadingTasks", "正在读取任务…")}</p> : <div className="min-h-0 flex-1 space-y-3 overflow-y-auto p-5">
      {!tasks.length ? <p className="py-12 text-center text-sm text-muted-foreground">{t("themedReading.emptyTasks", "还没有主题页任务。在文章正文或讨论总结中点击 AI 主题页即可开始。")}</p> : null}
      {tasks.map((task) => <article key={task.id} className="space-y-3 rounded-xl border border-border p-4">
        <div className="flex items-start gap-3"><div className="min-w-0 flex-1"><h3 className="text-sm font-medium">{task.title}</h3><p className="mt-1 text-xs text-muted-foreground">{task.sourceKind === "summary" ? t("library.forumSummarySection", "讨论总结") : t("library.bodySection", "正文")}</p></div>
          <button className={themeButton} onClick={() => void open(task)}>{t("themedReading.open", "查看主题页")}</button>
        </div>
        <ThemedReadingStatus task={task} busy={busyId === task.id} onCancel={() => void action(task, "cancel")} onResume={() => void action(task, "resume")} />
      </article>)}
    </div>}
  </div>;
}
