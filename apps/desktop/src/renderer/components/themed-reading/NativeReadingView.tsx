import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type { ReadingViewCommand } from "@guizhi/shared/types/reading-page-v3";
import { ImageLightbox } from "../library/ImageLightbox";
import { SnapshotToc } from "../library/SnapshotToc";
import { themeButton } from "./ThemedReadingSetup";
import { useUIStore } from "../../stores/ui.store";
import { MarkdownPreview } from "../library/MarkdownPreview";

/** 原生子视图只接收主进程保存的版本 ID；页面字符串不会从 React 送入执行环境。 */
export function NativeReadingView({
  page,
  preview = false,
  revision = 0,
  toolbar,
  findQuery,
  findIndex,
  onFindCount,
  onFindOpen,
}: {
  page: ThemedReadingVersion;
  preview?: boolean;
  revision?: number;
  toolbar?: HTMLElement | null;
  findQuery: string;
  findIndex: number;
  onFindCount: (n: number) => void;
  onFindOpen: () => void;
}) {
  const host = useRef<HTMLDivElement>(null),
    viewId = useRef<string>(),
    top = useRef(0);
  const [fault, setFault] = useState<string>(),
    [retry, setRetry] = useState(0),
    [enabled, setEnabled] = useState(true);
  const [headings, setHeadings] = useState<{ id: string; text: string }[]>([]),
    [catalog, setCatalog] = useState(false),
    [progress, setProgress] = useState(0);
  const [image, setImage] = useState<number | null>(null);
  const [obscured, setObscured] = useState(false);
  const storageKey = `reading-v3:${page.itemId}:${page.sourceKind}:${page.id}`;
  useEffect(() => {
    if ((fault || obscured) && host.current)
      host.current.scrollTop = top.current;
  }, [fault, obscured]);
  const target = {
    itemId: page.itemId,
    sourceKind: page.sourceKind,
    versionId: page.id,
    view: "themed",
  };
  const images = page.assets
    .filter((a) => a.status === "ready" && a.fileName)
    .map((a) => ({ id: a.id, src: `local-image://${a.fileName}`, alt: a.alt }));
  const send = useCallback((command: ReadingViewCommand) => {
    if (viewId.current)
      void window.api.themedReading
        .commandView({ viewId: viewId.current, command })
        .then((r) => {
          if (!r.success) setFault(r.error);
        })
        .catch((e) => setFault(String(e)));
  }, []);
  const appearance = useCallback(
    () =>
      send({
        type: "appearance",
        fontFamily: getComputedStyle(document.body).fontFamily,
        theme: document.documentElement.classList.contains("dark")
          ? "dark"
          : "light",
        fontSize: Math.max(
          12,
          Math.min(
            32,
            parseFloat(getComputedStyle(document.documentElement).fontSize) ||
              16,
          ),
        ),
      }),
    [send],
  );
  useEffect(() => {
    try {
      top.current = Number(sessionStorage.getItem(storageKey)) || 0;
    } catch {
      /* 阅读位置可选 */
    }
  }, [storageKey]);
  useEffect(() => {
    let disposed = false,
      id: string;
    setFault(undefined);
    const unsubscribe = window.api.themedReading.onViewEvent((event) => {
      if (event.viewId !== id) return;
      const v = event.value as any;
      if (event.type === "fault") setFault(v?.message || "交互运行失败");
      if (event.type === "key" && v === "find") onFindOpen();
      if (event.type === "key" && v === "escape")
        useUIStore.getState().setFocusReadingMode(false);
      if (
        event.type === "layout" &&
        Array.isArray(v?.headings) &&
        Number.isFinite(v.top) &&
        Number.isFinite(v.height)
      ) {
        setHeadings(v.headings);
        top.current = v.top;
        setProgress(
          Math.min(
            100,
            Math.round(
              (v.top /
                Math.max(1, v.height - (host.current?.clientHeight || 0))) *
                100,
            ),
          ),
        );
        try {
          sessionStorage.setItem(storageKey, String(v.top));
        } catch {
          /* 阅读位置可选 */
        }
      }
      if (event.type === "find" && Number.isSafeInteger(v?.count))
        onFindCount(v.count);
      if (event.type === "image") {
        const i = images.findIndex((a) => a.id === v?.id);
        if (i >= 0) setImage(i);
      }
      if (
        event.type === "selection" &&
        host.current &&
        typeof v?.text === "string" &&
        v.text.length <= 4000 &&
        Number.isFinite(v.x) &&
        Number.isFinite(v.y)
      ) {
        const b = host.current.getBoundingClientRect();
        host.current.dispatchEvent(
          new CustomEvent("reading-native-selection", {
            bubbles: true,
            detail: { text: v.text, x: b.left + v.x, y: b.top + v.y },
          }),
        );
      }
    });
    void window.api.themedReading
      .createView({
        itemId: page.itemId,
        sourceKind: page.sourceKind,
        versionId: page.id,
        preview,
        scriptsEnabled: enabled,
      })
      .then(async (r) => {
        if (!r.success || !r.viewId) {
          if (!disposed) setFault(r.error || "阅读视图创建失败");
          return;
        }
        id = r.viewId;
        if (disposed) {
          await window.api.themedReading.destroyView(id);
          return;
        }
        viewId.current = id;
        if (host.current) host.current.dataset.readingViewId = id;
        appearance();
        send({ type: "scroll", top: top.current });
        send({ type: "find", query: findQuery, index: findIndex });
        update();
      })
      .catch((e) => {
        if (!disposed) setFault(String(e));
      });
    let scheduled = 0,
      last = "";
    const update = () => {
      cancelAnimationFrame(scheduled);
      scheduled = requestAnimationFrame(() => {
        if (!id || disposed || !host.current) return;
        const b = host.current.getBoundingClientRect();
        const covered = [
          ...document.querySelectorAll(
            '[role="dialog"],[role="alertdialog"],[role="menu"],[data-article-selection-tools],[data-testid="snapshot-toc"] nav,[data-testid="article-ask-panel"]',
          ),
        ].some((e) => {
          const r = e.getBoundingClientRect();
          return (
            r.width > 0 &&
            r.height > 0 &&
            r.left < b.right &&
            r.right > b.left &&
            r.top < b.bottom &&
            r.bottom > b.top
          );
        });
        setObscured(covered);
        const bounds = {
          x: b.x,
          y: b.y,
          width: b.width,
          height: b.height,
          visible: !covered && document.visibilityState === "visible",
        };
        const key = JSON.stringify(bounds);
        if (key !== last) {
          last = key;
          void window.api.themedReading
            .updateView({ viewId: id, bounds })
            .then((r) => {
              if (!r.success && !disposed) setFault(r.error);
            })
            .catch((e) => {
              if (!disposed) setFault(String(e));
            });
        }
      });
    };
    const resize = new ResizeObserver(update);
    if (host.current) resize.observe(host.current);
    const mutations = new MutationObserver(() => {
      update();
      appearance();
    });
    mutations.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["class", "style", "open"],
    });
    mutations.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class", "style"],
    });
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    document.addEventListener("visibilitychange", update);
    return () => {
      disposed = true;
      unsubscribe();
      resize.disconnect();
      mutations.disconnect();
      cancelAnimationFrame(scheduled);
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
      document.removeEventListener("visibilitychange", update);
      viewId.current = undefined;
      if (id) void window.api.themedReading.destroyView(id);
    };
    // 预览修订重新装载静态完整单元，使用同一阅读位置。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page.id, preview, revision, retry, enabled]);
  useEffect(() => {
    send({ type: "find", query: findQuery, index: findIndex });
  }, [send, findQuery, findIndex]);
  const navigation = (
    <>
      <button className={themeButton} onClick={() => setCatalog(!catalog)}>
        目录
      </button>
      {!preview && page.design?.scripts?.length ? (
        <button className={themeButton} onClick={() => setEnabled(!enabled)}>
          {enabled ? "暂停交互" : "启用交互"}
        </button>
      ) : null}
    </>
  );
  return (
    <div
      className="relative flex min-h-0 flex-1 flex-col overflow-hidden"
      data-testid="native-reading-view"
    >
      {toolbar ? createPortal(navigation, toolbar) : navigation}
      {fault ? (
        <div className="shrink-0 bg-muted px-4 py-2 text-xs" role="status">
          {fault}{" "}
          <button
            className={themeButton}
            onClick={() => {
              setEnabled(true);
              setRetry((n) => n + 1);
            }}
          >
            重新启用交互
          </button>
        </div>
      ) : null}
      <div
        ref={host}
        data-article-reader
        data-article-target={JSON.stringify(target)}
        className="min-h-0 flex-1 overflow-auto"
      >
        {fault || obscured ? (
          <article className="mx-auto max-w-4xl p-6 text-base leading-relaxed">
            <h1 className="mb-6 text-2xl font-semibold">
              {page.reconstruction?.outline?.title || page.source.title}
            </h1>
            {page.reconstruction?.draft.map((s, i) => (
              <section key={i}>
                <h2 className="my-4 text-xl font-semibold">{s.title}</h2>
                <MarkdownPreview content={s.markdown} />
              </section>
            ))}
          </article>
        ) : null}
      </div>
      <SnapshotToc
        headings={headings.map((h, index) => ({ text: h.text, index }))}
        active={null}
        progress={progress}
        wide={false}
        open={catalog}
        onOpen={setCatalog}
        onJump={(i) => send({ type: "anchor", id: headings[i].id })}
        onTop={() => send({ type: "scroll", top: 0 })}
      />
      {image !== null ? (
        <ImageLightbox
          images={images}
          startIndex={image}
          onClose={() => setImage(null)}
        />
      ) : null}
    </div>
  );
}
