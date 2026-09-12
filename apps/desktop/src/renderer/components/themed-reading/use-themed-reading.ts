import { useCallback, useEffect, useRef, useState } from "react";
import type { ThemedReadingOptions, ThemedReadingResult, ThemedReadingSourceKind, ThemedReadingTask } from "@guizhi/shared/types/themed-reading";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useToast } from "../ui/Toast";
import { useTranslation } from "react-i18next";

export const isThemeTaskActive = (task?: ThemedReadingTask | null) => Boolean(task && ["queued", "running"].includes(task.state));
export const canResumeThemeTask = (task?: ThemedReadingTask | null) => Boolean(task && ["partial", "failed", "cancelled", "interrupted"].includes(task.state));

/** 主进程持有任务；关闭面板只退订，绝不隐式取消已付费的生成。 */
export function useThemedReading(itemId: string, sourceKind: ThemedReadingSourceKind, content: string, showPage = true) {
  const { showToast } = useToast();
  const { t } = useTranslation();
  const [instanceId] = useState(() => crypto.randomUUID());
  const [result, setResult] = useState<ThemedReadingResult | null>(null);
  const [latest, setLatest] = useState<ThemedReadingResult | null>(null);
  const pinned = useRef<ThemedReadingResult | null>(null);
  const displayed = useRef<ThemedReadingResult | null>(null);
  useEffect(()=>{displayed.current=result;},[result]);
  const [task, setTask] = useState<ThemedReadingTask | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const mounted = useRef(true);
  const revision = useRef(0);
  const taskRevision = useRef(0);
  useEffect(()=>{pinned.current=null;setLatest(null);setResult(null);setTask(null);revision.current+=1;},[itemId,sourceKind]);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; revision.current += 1; }; }, []);
  const reload = useCallback(async () => {
    const request = ++revision.current;
    const taskAtRequest = taskRevision.current;
    setLoadError(null);
    try {
      const next = await window.api.themedReading.get({ itemId, sourceKind, instanceId, ...(!showPage ? { metadataOnly: true } : {}) });
      if (!mounted.current || request !== revision.current) return;
      if (!next.success) throw new Error(next.error || "主题页读取失败");
      if(!pinned.current&&next.page&&isThemeTaskActive(next.task))pinned.current=next;
      if (pinned.current?.page && next.page && pinned.current.page.id !== next.page.id) {
        setLatest(next);
        setResult({ ...next, page: pinned.current.page, document: pinned.current.document });
      } else { setResult(next); setLatest(null); }
      if (taskAtRequest === taskRevision.current) setTask(next.task ?? null);
    } catch (error) {
      if (mounted.current && request === revision.current) setLoadError(error instanceof Error ? error.message : String(error));
    }
  }, [itemId, sourceKind, instanceId, showPage]);
  useEffect(() => { void reload(); }, [reload, content]);
  useEffect(() => window.api.themedReading.onProgress((next) => {
    if (next.itemId !== itemId || next.sourceKind !== sourceKind) return;
    if(isThemeTaskActive(next)&&!pinned.current&&displayed.current?.page)pinned.current=displayed.current;
    taskRevision.current += 1;
    setTask(next);
    if (!isThemeTaskActive(next)) void reload();
  }), [itemId, sourceKind, reload]);

  const run = async (label: string, action: () => Promise<ThemedReadingResult>, successText?: string) => {
    if (busy) return false;
    setBusy(true);
    try {
      const next = await action();
      if (!next.success) throw new Error(next.error || `${label}失败`);
      if (mounted.current) {
        if (next.task) { taskRevision.current += 1; setTask(next.task); }
        await reload();
      }
      if (successText && !next.cancelled) showToast(successText, "success");
      return true;
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      showToast(t("themedReading.actionFailed", "{{action}}失败", { action: label }), "error", { detail });
      window.api.log?.appError({ scope: "themedReading", action: label, message: detail });
      return false;
    } finally {
      if (mounted.current) setBusy(false);
    }
  };
  const generate = (options: ThemedReadingOptions) => run(t("themedReading.generate", "生成主题页"), async () => {
    if (!(await useKnowledgeStore.getState().flushPendingSave())) return;
    if (useKnowledgeStore.getState().hasUnsavedChanges) throw new Error("当前编辑尚未保存，请先保存后重试");
    pinned.current = result?.page ? result : null;
    return window.api.themedReading.generate({ itemId, sourceKind, options });
  });
  const request = { itemId, sourceKind };
  return { result, task, instanceId, busy, loadError, reload, generate, latest,
    adoptLatest: () => { pinned.current = null; if (latest) setResult(latest); setLatest(null); },
    cancel: () => task ? run(t("themedReading.stop", "停止生成"), () => window.api.themedReading.cancel(task.id)) : Promise.resolve(false),
    continueOffline: () => task ? run("改为不联网生成", () => window.api.themedReading.continueOffline(task.id)) : Promise.resolve(false),
    resume: () => task ? run(t("themedReading.resume", "继续未完成部分"), () => window.api.themedReading.resume(task.id)) : Promise.resolve(false),
    regenerateAsset: (assetId: string) => run(t("themedReading.redraw", "重做这张图"), () => window.api.themedReading.regenerateAsset({ ...request, assetId })),
    restore: () => run(t("themedReading.restore", "恢复上一版"), () => { pinned.current = null; return window.api.themedReading.restorePrevious(request); }, t("themedReading.restored", "已恢复上一版主题页")),
    remove: () => run(t("themedReading.delete", "删除主题页"), () => { pinned.current = null; return window.api.themedReading.remove(request); }, t("themedReading.removed", "主题页已删除，正文保留")),
    exportHtml: (withoutImages = false, staticOnly = false) => run(t("themedReading.export", "导出 HTML"), () => window.api.themedReading.exportHtml({ ...request, withoutImages, staticOnly, versionId:result?.page?.id }), t("themedReading.exported", "主题页 HTML 已导出")),
  };
}
