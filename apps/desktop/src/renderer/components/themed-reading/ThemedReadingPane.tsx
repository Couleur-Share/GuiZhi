import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import type { ThemedReadingSourceKind } from "@guizhi/shared/types/themed-reading";
import { LoadErrorState } from "../ui/LoadErrorState";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { ThemedReadingFrame } from "./ThemedReadingFrame";
import { NativeReadingView } from "./NativeReadingView";
import type { ReadingPreview } from "@guizhi/shared/types/reading-page-v3";
import { ThemedReadingSetup, themeButton } from "./ThemedReadingSetup";
import { ThemedReadingStatus } from "./ThemedReadingStatus";
import { ThemedReadingPageTools } from "./ThemedReadingPageTools";
import { isThemeTaskActive, useThemedReading } from "./use-themed-reading";

export function ThemedReadingPane({
  item,
  sourceKind,
  readOnly = Boolean(item.deletedAt),
  findQuery,
  findIndex,
  onFindCount,
  onFindOpen,
  original,
  showPage = true,
  onPageRemoved,
  toolbarTarget,
}: {
  item: KnowledgeItem;
  sourceKind: ThemedReadingSourceKind;
  findQuery: string;
  findIndex: number;
  onFindCount: (count: number) => void;
  onFindOpen: () => void;
  original?: ReactNode;
  showPage?: boolean;
  onPageRemoved?: () => void;
  readOnly?: boolean;
  toolbarTarget?: HTMLElement | null;
}) {
  const { t } = useTranslation();
  const [readerToolbar, setReaderToolbar] = useState<HTMLDivElement | null>(
    null,
  );
  const view = useThemedReading(
    item.id,
    sourceKind,
    JSON.stringify([item.title, item.sourceUri, item.content]),
    showPage,
  );
  const [setup, setSetup] = useState<"new" | "adjust" | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const autoSetupAttempted = useRef(false);
  const page = view.result?.page;
  const active = isThemeTaskActive(view.task);
  const [preview,setPreview]=useState<ReadingPreview|null>(null);
  const [previewOpen,setPreviewOpen]=useState(false);
  const lastPreviewAt=useRef(0);
  useEffect(()=>{
    if(!view.task?.previewRevision || (!previewOpen&&page))return;
    let disposed=false;
    const timer=setTimeout(()=>{lastPreviewAt.current=Date.now();void window.api.themedReading.preview({itemId:item.id,sourceKind,taskId:view.task.id}).then(r=>{if(!disposed&&r.success&&r.preview)setPreview(p=>!p||r.preview.revision>=p.revision?r.preview:p);}).catch(error=>{window.api.log?.appError({scope:"themedReading",action:"preview",message:String(error)});});},Math.max(0,500-(Date.now()-lastPreviewAt.current)));
    return()=>{disposed=true;clearTimeout(timer);};
  },[view.task?.id,view.task?.previewRevision,previewOpen,page,item.id,sourceKind]);
  useEffect(()=>{setPreview(null);setPreviewOpen(false);},[view.task?.id]);
  const showingPreview=Boolean(preview&&(!page||previewOpen));
  const readingPage=showingPreview?preview.page:page;
  const disabled = view.busy || active || readOnly;
  useEffect(() => {
    if (!view.result || autoSetupAttempted.current) return;
    autoSetupAttempted.current = true;
    if (!original && !page && !view.task && !readOnly) setSetup("new");
  }, [view.result, page, view.task, readOnly, original]);
  const toolbar = (
    <div className="flex shrink-0 items-center gap-1">
      <div ref={setReaderToolbar} className="flex items-center" />
      {page&&view.task?.previewRevision&&view.task.versionId!==page.id&&!view.latest?<button className={themeButton} onClick={()=>setPreviewOpen(!previewOpen)}>{previewOpen?"返回当前版":"预览新稿"}</button>:null}
      {view.latest?<button className={themeButton} onClick={()=>{view.adoptLatest();setPreviewOpen(false);}}>新版已完成 · 切换</button>:null}
      {page ? (
        <ThemedReadingPageTools
          view={view}
          disabled={disabled}
          onSetup={setSetup}
          onDelete={() => setDeleteOpen(true)}
        />
      ) : view.task && !active ? (
        <button
          className={themeButton}
          disabled={disabled || !view.result}
          onClick={() => setSetup("new")}
        >
          {t("themedReading.readerStartOver", "重新生成")}
        </button>
      ) : null}
    </div>
  );
  const taskStatus =
    view.task && (view.task.state !== "completed" || page?.formatVersion===3) ? (
      <ThemedReadingStatus
        key={view.task.id}
        task={view.task}
        busy={view.busy}
        readOnly={readOnly}
        hasPage={Boolean(page)}
        expanded={!readingPage}
        onCancel={() => void view.cancel()}
        onResume={() => void view.resume()}
        onOffline={() => void view.continueOffline()}
      />
    ) : null;
  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      data-testid="themed-reading-pane"
    >
      {showPage && (page || view.task) ? (
        toolbarTarget ? (
          createPortal(toolbar, toolbarTarget)
        ) : (
          <div className="flex h-10 shrink-0 items-center justify-end border-b border-border/50 px-3">
            {toolbar}
          </div>
        )
      ) : null}
      {!showPage && original ? (
        <div className="min-h-0 flex-1">{original}</div>
      ) : (
        <>
          {readOnly ? (
            <p className="shrink-0 px-4 py-2 text-xs text-muted-foreground">
              {t(
                "themedReading.readerReadOnly",
                "此条目在回收站中，阅读页仍可查看与导出。",
              )}
            </p>
          ) : null}
          {view.loadError ? (
            <LoadErrorState
              message={view.loadError}
              onRetry={() => void view.reload()}
            />
          ) : !view.result ? (
            <p className="p-6 text-sm text-muted-foreground">
              {t("themedReading.loading", "正在读取主题页…")}
            </p>
          ) : null}
          {readingPage && taskStatus ? (
            <div className="max-h-[35%] shrink-0 overflow-y-auto">
              {taskStatus}
            </div>
          ) : null}
          {page && view.result?.stale ? (
            <p className="shrink-0 px-4 py-1.5 text-xs text-muted-foreground">
              {t(
                "themedReading.readerStale",
                "原文已有更新，当前阅读页保留生成时的内容。",
              )}
            </p>
          ) : null}
          {readingPage?.formatVersion===3 ? <NativeReadingView page={readingPage} preview={showingPreview} revision={showingPreview?preview.revision:0} toolbar={readerToolbar} findQuery={findQuery} findIndex={findIndex} onFindCount={onFindCount} onFindOpen={onFindOpen}/> : page && view.result?.document ? (
            <ThemedReadingFrame
              toolbar={readerToolbar}
              document={view.result.document}
              page={page}
              instanceId={view.instanceId}
              findQuery={findQuery}
              findIndex={findIndex}
              onFindCount={onFindCount}
              onFindOpen={onFindOpen}
            />
          ) : view.result && !view.loadError && taskStatus ? (
            <div className="flex min-h-0 flex-1 items-center justify-center overflow-y-auto p-6">
              {taskStatus}
            </div>
          ) : view.result &&
            !view.loadError &&
            view.task?.state === "completed" ? (
            <p className="p-6 text-sm text-muted-foreground" role="status">
              {t("themedReading.waitDone", "正在载入完成的阅读页…")}
            </p>
          ) : view.result && !view.loadError ? (
            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-8 text-center">
              <h3 className="text-base font-medium">
                {t("themedReading.readerEmpty", "换一种方式阅读这篇文章")}
              </h3>
              <p className="max-w-sm text-sm leading-relaxed text-muted-foreground">
                {t(
                  "themedReading.readerEmptyHint",
                  "AI 可以整理文章结构、解释重点并设计阅读版式。原文始终保留。",
                )}
              </p>
              <button
                className={`${themeButton} mt-1 bg-primary text-primary-foreground hover:bg-primary/90`}
                disabled={disabled}
                onClick={() => setSetup("new")}
              >
                {t("themedReading.readerCreate", "生成阅读页")}
              </button>
            </div>
          ) : null}
        </>
      )}
      {setup ? (
        <ThemedReadingSetup
          title={item.title}
          sourceKind={sourceKind}
          current={setup === "adjust" ? page?.options : undefined}
          models={view.result?.models}
          searchConfigured={view.result?.search?.configured}
          defaultResearch={view.result?.search?.defaultEnabled === true}
          busy={view.busy}
          onClose={() => setSetup(null)}
          onGenerate={view.generate}
        />
      ) : null}
      <ConfirmDialog
        isOpen={deleteOpen}
        onClose={() => setDeleteOpen(false)}
        onConfirm={async () => {
          if (await view.remove()) {
            setDeleteOpen(false);
            onPageRemoved?.();
          }
        }}
        title={t("themedReading.readerDeleteTitle", "删除 AI 阅读页？")}
        message={t(
          "themedReading.deleteHint",
          "当前页、上一版及专属生成素材将被移除，正文和原有图片保留。",
        )}
        confirmText={t("common.delete", "删除")}
      />
    </div>
  );
}
