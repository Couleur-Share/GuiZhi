import { SnapshotToc } from "./SnapshotToc";
import { useSnapshotReader } from "./use-snapshot-reader";
import { useUIStore } from "../../stores/ui.store";
import { createPortal } from "react-dom";
import { ContextMenu } from "../ui/ContextMenu";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import type { KnowledgeItem, WebSnapshotView } from "@guizhi/shared/types";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";
import { ImageLightbox } from "./ImageLightbox";
import { runGuardedMutation } from "../../stores/operation-error.store";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";

const button =
  "rounded-lg border border-border px-3 py-1.5 text-xs hover:bg-accent disabled:opacity-50";
export function WebSnapshotPane({
  item,
  forceSimple,
  children,
  versionId,
  toolbarTarget,
  findQuery = "", findIndex = 0, onFindCount, onOriginalChange, onFindOpen,
}: {
  item: KnowledgeItem;
  forceSimple: boolean;
  children: ReactNode;
  versionId?: string;
  toolbarTarget?: HTMLElement | null;
  findQuery?: string;
  findIndex?: number;
  onFindCount?: (count: number) => void;
  onOriginalChange?: (original: boolean) => void;
  onFindOpen?: () => void;
}) {
  const { showToast } = useToast();
  const [view, setView] = useState<WebSnapshotView | null>(null),
    [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<"original" | "simple">("original"),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0);
  const [width, setWidth] = useState(() => { const saved=localStorage.getItem("wechat-reader-width"); return saved === "original" || saved === "wide" ? saved : "fluid"; });
  const menuTrigger = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState<{x:number;y:number}|null>(null);
  const [imageIndex, setImageIndex] = useState<number | null>(null),
    [height, setHeight] = useState(400),
    [tasks, setTasks] = useState<string[]>([]);
  const frame = useRef<HTMLIFrameElement>(null),
    scroll = useRef<HTMLDivElement>(null);
  const completedTasks = useImportStore((state) =>
    state.tasks
      .filter(
        (task) =>
          task.refreshOfItemId === item.id && task.status === "completed",
      )
      .map((task) => task.id)
      .join(","),
  );
  const original = !forceSimple && mode === "original" && !!view?.document;
  const reader = useSnapshotReader(frame, scroll, view?.instanceId, `${item.id}:${view?.version?.id || versionId || "current"}`, original);
  const findRequest = useRef(0);
  const sendFind = useCallback(() => {
    if (!original || !frame.current?.contentWindow) return;
    frame.current.contentWindow.postMessage({id:view?.instanceId,type:"find",value:{query:findQuery.slice(0,1000),index:findIndex,request:++findRequest.current}}, "*");
  }, [original, view?.instanceId, findQuery, findIndex]);
  useEffect(() => { onOriginalChange?.(original); }, [original, onOriginalChange]);
  useEffect(sendFind, [sendFind]);
  const reload = useCallback(() => setRevision((n) => n + 1), []);
  useEffect(() => {
    let canceled = false;
    setError(null);
    setView(null);
    window.api.webCapture
      .snapshot(item.id, versionId)
      .then((result) => {
        if (canceled) return;
        if (!result.ok || result.data?.error) {
          setError(result.error || result.data?.error || "读取原文失败");
          return;
        }
        setView(result.data!);
        setMode(result.data?.edited && !versionId ? "simple" : "original");
      })
      .catch((e) => {
        if (!canceled) setError(String(e));
      });
    return () => {
      canceled = true;
    };
  }, [item.id, item.content, revision, versionId, completedTasks]);
  useEffect(() => {
    const receive = (event: MessageEvent) => {
      if (
        event.source !== frame.current?.contentWindow ||
        event.origin !== "null" ||
        event.data?.id !== view?.instanceId
      )
        return;
      const { type, value } = event.data;
      if (type === "find-open" && value === null) onFindOpen?.();
      if (type === "find-result" && original && value?.request === findRequest.current && Number.isSafeInteger(value.count) && value.count >= 0 && value.count <= 10000) {
        onFindCount?.(value.count);
        if (typeof value.top === "number" && Number.isFinite(value.top)) scroll.current?.scrollTo({top:Math.max(0,Math.min(value.top-60,2000000))});
      }
      if (type === "escape" && value === null) useUIStore.getState().setFocusReadingMode(false);
      if (
        type === "height" &&
        typeof value === "number" &&
        Number.isFinite(value)
      )
        setHeight(Math.max(100, Math.min(value, 2000000)));
      if (
        type === "anchor" &&
        typeof value === "number" &&
        Number.isFinite(value)
      )
        scroll.current?.scrollTo({
          top: Math.max(0, Math.min(value, 2000000)),
        });
      if (type === "link" && typeof value === "string") {
        try {
          const url = new URL(value);
          if (
            ["http:", "https:"].includes(url.protocol) &&
            !url.username &&
            !url.password
          )
            window.open(url.href, "_blank", "noopener,noreferrer");
        } catch {
          /* 无效外链不执行 */
        }
      }
      if (type === "image" && typeof value === "string") {
        const index =
          view?.version?.snapshot?.assets.findIndex(
            (a) => `local-image://${a.fileName}` === value,
          ) ?? -1;
        if (index >= 0) setImageIndex(index);
      }
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [view, original, onFindCount, onFindOpen]);
  useEffect(() => {
    if (!tasks.length) return;
    let canceled = false,
      running = false;
    const timer = setInterval(async () => {
      if (running) return;
      running = true;
      try {
        const all = await window.api.import.list();
        if (canceled) return;
        const matched = all.filter((task) => tasks.includes(task.id));
        if (
          matched.length === tasks.length &&
          matched.every((task) =>
            ["completed", "failed", "canceled"].includes(task.status),
          )
        ) {
          setTasks([]);
          setBusy(false);
          reload();
          const failures = matched.filter(
            (task) => task.status !== "completed",
          );
          showToast(
            failures.length ? "原文补采未完成" : "原文版本已保存",
            failures.length ? "error" : "success",
            {
              detail: matched
                .map((task) => task.error || task.warning)
                .filter(Boolean)
                .join("\n"),
            },
          );
        }
      } catch (e) {
        if (!canceled) {
          setTasks([]);
          setBusy(false);
          setError(String(e));
        }
      } finally {
        running = false;
      }
    }, 1000);
    return () => {
      canceled = true;
      clearInterval(timer);
    };
  }, [tasks, reload, showToast]);
  const supplement = async () => {
    setBusy(true);
    const ok = await runGuardedMutation(
      "webSnapshot.supplement",
      "补采公众号原文",
      async () => {
        const result = await window.api.webCapture.supplement([item.id]);
        if (!result.ok) throw new Error(result.error);
        setTasks(result.data!.map((task) => task.id));
      },
    );
    if (!ok) setBusy(false);
  };
  const exportHtml = async () => {
    let exported: { canceled?: boolean; path?: string; incomplete?: boolean };
    const ok = await runGuardedMutation(
      "webSnapshot.export",
      "导出原文 HTML",
      async () => {
        const result = await window.api.webCapture.exportHtml(
          item.id,
          view!.version!.id,
        );
        if (!result.ok) throw new Error(result.error);
        exported = result.data!;
      },
    );
    if (ok && !exported?.canceled)
      showToast(
        exported?.incomplete ? "已导出，部分资源有缺失" : "原文 HTML 已导出",
        exported?.incomplete ? "warning" : "success",
      );
  };
  const snapshot = view?.version?.snapshot;
  const toolbar = (
<div className="flex shrink-0 items-center gap-1.5">
        <span data-testid="snapshot-reading-mode" className="px-2 text-xs text-muted-foreground">
          {original ? "原文排版" : view?.edited && !versionId ? "正文已编辑" : "标准排版"}
        </span>
        {!original && view?.document ? (
          <button className={button} onClick={() => setMode("original")} disabled={forceSimple}>查看原文快照</button>
        ) : original && view?.edited && !versionId ? (
          <button className={button} onClick={() => setMode("simple")}>返回编辑后正文</button>
        ) : null}
        {original && !reader.wideRail ? <button data-snapshot-toc-trigger className={button} aria-expanded={reader.catalogOpen} onClick={()=>reader.setCatalogOpen(!reader.catalogOpen)}>目录</button> : null}
        {original ? (
          <Select
            className="w-32"
            triggerClassName="flex h-7 w-full items-center gap-1 rounded-lg bg-muted px-2 text-xs text-left focus-visible:ring-2 focus-visible:ring-primary"
            ariaLabel="原文阅读宽度"
            value={width}
            onChange={(value) => {
              setWidth(value);
              localStorage.setItem("wechat-reader-width", value);
            }}
            options={[
              { value: "original", label: "原文宽度" },
              { value: "fluid", label: "自适应宽度" },
              { value: "wide", label: "铺满宽度" },
            ]}
          />
        ) : null}
        <button ref={menuTrigger} className={button} type="button" aria-haspopup="menu" aria-expanded={!!menu} onClick={event=>{const rect=event.currentTarget.getBoundingClientRect();setMenu(menu ? null : {x:rect.left,y:rect.bottom+4});}}>更多</button>
        {busy ? <span className="whitespace-nowrap text-xs text-muted-foreground">正在补采…</span> : null}
      </div>
  );
  return (
    <div
      className="flex h-full min-h-0 flex-col"
      data-testid="web-snapshot-pane"
    >
      {toolbarTarget ? createPortal(toolbar, toolbarTarget) : <div className="border-b border-border p-2">{toolbar}</div>}
      {menu ? <ContextMenu ignoreRef={menuTrigger} x={menu.x} y={menu.y} onClose={()=>setMenu(null)} items={[
        ...(original ? [{label:view?.edited && !versionId ? "查看编辑后正文" : "切换到标准排版",onClick:()=>setMode("simple")}] : []),
        ...(!item.deletedAt ? [{label:snapshot?.failures.length ? "重试缺失资源" : "补采 / 更新原文",disabled:busy,onClick:()=>void supplement()}] : []),
        ...(snapshot ? [{label:"导出原文 HTML",onClick:()=>void exportHtml()}] : []),
        ...(busy ? [{label:"取消补采",onClick:()=>void runGuardedMutation("webSnapshot.cancel","取消补采",async()=>{for(const id of tasks)if(!await window.api.import.cancel(id))throw new Error("任务已结束或无法取消");})}] : []),
      ]}/> : null}
      {error ? (
        <div className="p-3 text-sm text-destructive" role="alert">
          {error}
          <button className={button} onClick={reload}>
            重试读取
          </button>
        </div>
      ) : null}
      {!view && !error ? (
        <p className="p-3 text-sm text-muted-foreground">正在读取原文…</p>
      ) : null}
      {view && !snapshot ? (
        <p className="p-3 text-sm text-muted-foreground">
          尚未保存原文排版，当前显示标准排版。
          {!item.deletedAt ? <button className={`${button} ml-2`} disabled={busy} onClick={() => void supplement()}>补采原文排版</button> : null}
        </p>
      ) : null}
      {view?.edited ? (
        <p className="px-3 py-1 text-xs text-muted-foreground">
          {original ? "当前查看采集时的原文快照，不包含你的正文修改。" : "当前显示编辑后的正文，原文快照仍保留采集时内容。"}
        </p>
      ) : null}
      {view?.pending ? (
        <p className="px-3 py-1 text-xs text-muted-foreground">
          有新来源版本，请在“原文版本”中比较并采用。
        </p>
      ) : null}
      {snapshot?.failures.length || snapshot?.warnings.length ? (
        <div className="px-3 py-2 text-xs text-amber-600" role="status">
          {[
            ...snapshot.warnings,
            ...snapshot.failures.map((f) => f.reason),
          ].join("；")}
        </div>
      ) : null}
      {original ? (
        <div className="relative min-h-0 flex-1">
        <div
          ref={scroll}
          className="h-full min-h-0 overflow-auto bg-muted/30 p-3"
        >
          <iframe
            ref={frame}
            onLoad={sendFind}
            aria-label="微信公众号原文快照"
            sandbox="allow-scripts"
            referrerPolicy="no-referrer"
            srcDoc={view.document}
            style={{
              width: width === "original" ? 390 : width === "wide" ? "100%" : `min(100%, ${reader.comfortableWidth}px)`,
              maxWidth: "100%",
              height,
            }}
            className="mx-auto block border-0 bg-white"
          />
        </div>
        <SnapshotToc headings={reader.headings} active={reader.active} progress={reader.progress} wide={reader.wideRail} open={reader.catalogOpen} onOpen={reader.setCatalogOpen} onJump={reader.jump} onTop={reader.top}/>
        </div>
      ) : (
        <div className="min-h-0 flex-1">{children}</div>
      )}
      {imageIndex !== null && snapshot ? (
        <ImageLightbox
          images={snapshot.assets.map((a) => ({
            src: `local-image://${a.fileName}`,
          }))}
          startIndex={imageIndex}
          onClose={() => setImageIndex(null)}
        />
      ) : null}
    </div>
  );
}

export async function supplementWechatSelection(
  ids: string[],
): Promise<boolean> {
  return runGuardedMutation(
    "webSnapshot.supplement",
    "批量补采公众号原文",
    async () => {
      await useKnowledgeStore.getState().flushPendingSave();
      if (useKnowledgeStore.getState().hasUnsavedChanges)
        throw new Error("当前编辑尚未保存");
      for (let i = 0; i < ids.length; i += 50) {
        const result = await window.api.webCapture.supplement(
          ids.slice(i, i + 50),
        );
        if (!result.ok) throw new Error(result.error);
      }
    },
  );
}
