import { useReadingViewport } from "./use-reading-viewport";
import { createPortal } from "react-dom";
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import { useSnapshotReader } from "../library/use-snapshot-reader";
import { SnapshotToc } from "../library/SnapshotToc";
import { ImageLightbox } from "../library/ImageLightbox";
import { useUIStore } from "../../stores/ui.store";
import { useTranslation } from "react-i18next";

export function ThemedReadingFrame({ document: html, page, instanceId, toolbar, findQuery = "", findIndex = 0, onFindCount, onFindOpen }: {
  toolbar?: HTMLElement | null; document: string; page: ThemedReadingVersion; instanceId: string; findQuery?: string; findIndex?: number;
  onFindCount?: (count: number) => void; onFindOpen?: () => void;
}) {
  const { t } = useTranslation();
  const frame = useRef<HTMLIFrameElement>(null), scroll = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(400), [imageIndex, setImageIndex] = useState<number | null>(null);
  const request = useRef(0);
  useReadingViewport(frame, scroll, instanceId, html);
  const reader = useSnapshotReader(frame, scroll, instanceId, `theme:${page.itemId}:${page.sourceKind}:${page.id}`, true);
  const setCatalogOpen = reader.setCatalogOpen;
  const catalogKey = `reading-ai-catalog:${page.itemId}:${page.sourceKind}`;
  useEffect(() => { try { setCatalogOpen(localStorage.getItem(catalogKey) === "true"); } catch { /* 不影响阅读 */ } }, [catalogKey, setCatalogOpen]);
  const toggleCatalog = () => { const open = !reader.catalogOpen; reader.setCatalogOpen(open); try { localStorage.setItem(catalogKey,String(open)); } catch { /* 不影响阅读 */ } };
  const images = page.assets.filter((asset) => asset.status === "ready" && asset.fileName).map((asset) => ({ src: `local-image://${asset.fileName}`, alt: asset.alt }));
  const sendFind = useCallback(() => frame.current?.contentWindow?.postMessage({ id: instanceId, type: "find", value: { query: findQuery.slice(0, 1000), index: findIndex, request: ++request.current } }, "*"), [instanceId, findQuery, findIndex]);
  const sendAppearance = useCallback(() => {
    frame.current?.contentWindow?.postMessage({ id: instanceId, type: "appearance", value: {
      theme: document.documentElement.classList.contains("dark") ? "dark" : "light",
      fontSize: Math.max(12, Math.min(32, parseFloat(getComputedStyle(document.documentElement).fontSize) || 16)),
      fontFamily: getComputedStyle(document.body).fontFamily,
    } }, "*");
  }, [instanceId]);
  useEffect(sendFind, [sendFind]);
  useEffect(() => {
    const observer = new MutationObserver(sendAppearance);
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["class", "style", "data-theme"] });
    observer.observe(document.body, { attributes: true, attributeFilter: ["class", "style"] });
    return () => observer.disconnect();
  }, [sendAppearance]);
  useLayoutEffect(() => {
    const receive = (event: MessageEvent) => {
      if (event.source !== frame.current?.contentWindow || event.origin !== "null" || event.data?.id !== instanceId) return;
      const { type, value } = event.data;
      if (type === "reading-motion-error" && typeof value?.visualId === "string" && page.reconstruction?.visuals?.some(v => v.id === value.visualId)) window.api.log.appError({ scope: "themedReading", action: "播放动画", message: `图形 ${value.visualId} 初始化失败，已恢复静态图` });
      if (type === "height" && Number.isFinite(value)) setHeight(Math.max(100, Math.min(value, 2_000_000)));
      if (type === "anchor" && Number.isFinite(value)) scroll.current?.scrollTo({ top: Math.max(0, Math.min(value, 2_000_000)) });
      if (type === "find-open" && value === null) onFindOpen?.();
      if (type === "escape" && value === null) useUIStore.getState().setFocusReadingMode(false);
      if (type === "find-result" && value?.request === request.current && Number.isSafeInteger(value.count) && value.count >= 0 && value.count <= 10000) {
        onFindCount?.(value.count);
        if (Number.isFinite(value.top)) scroll.current?.scrollTo({ top: Math.max(0, Math.min(value.top - 60, 2_000_000)) });
      }
      if (type === "image" && typeof value === "string") {
        const index = images.findIndex((image) => image.src === value);
        if (index >= 0) setImageIndex(index);
      }
      if (type === "link" && typeof value === "string") {
        try { const url = new URL(value); if (["https:", "http:"].includes(url.protocol) && !url.username && !url.password) window.open(url.href, "_blank", "noopener,noreferrer"); } catch { /* 无效链接不执行 */ }
      }
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [instanceId, images, onFindCount, onFindOpen, page.reconstruction?.visuals]);
  const navigation = <button data-snapshot-toc-trigger className="rounded-md px-2 py-1 text-xs text-muted-foreground hover:bg-accent focus-visible:ring-2 focus-visible:ring-primary" aria-expanded={reader.catalogOpen} onClick={toggleCatalog}>{t("library.catalog", "目录")}</button>;
  return <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden" data-testid="themed-reading-frame">
    <div ref={scroll} className="min-h-0 flex-1 overflow-auto bg-background p-3">
      <iframe data-article-instance={instanceId} data-article-target={JSON.stringify({ itemId: page.itemId, view: "themed", versionId: page.id, sourceKind: page.sourceKind })} ref={frame} aria-label={t("themedReading.title", "AI 主题阅读页")} sandbox="allow-scripts" scrolling="no" referrerPolicy="no-referrer" srcDoc={html}
        onLoad={() => { sendAppearance(); sendFind(); frame.current?.contentWindow?.postMessage({ id: instanceId, type: "reader-refresh" }, "*"); }} className="mx-auto block w-full border-0" style={{ height, maxWidth: 1200 }} />
    </div>
    {toolbar ? createPortal(navigation, toolbar) : null}
    <SnapshotToc headings={reader.headings} active={reader.active} progress={reader.progress} wide={false} open={reader.catalogOpen} onOpen={reader.setCatalogOpen} onJump={reader.jump} onTop={reader.top} />
    {imageIndex !== null ? <ImageLightbox images={images} startIndex={imageIndex} onClose={() => setImageIndex(null)} /> : null}
  </div>;
}
