import { useEffect, type RefObject } from "react";

/** iframe 随全文伸高；可见区域必须以外层阅读器滚动与窗口交集计算。 */
export function useReadingViewport(frame: RefObject<HTMLIFrameElement>, scroll: RefObject<HTMLDivElement>, instanceId: string, html: string) {
  useEffect(() => {
    const host = scroll.current, iframe = frame.current;
    if (!host || !iframe) return;
    let raf = 0;
    const send = () => {
      raf = 0;
      const outer = host.getBoundingClientRect(), box = iframe.getBoundingClientRect();
      const visibleTop = Math.max(outer.top, 0, box.top), visibleBottom = Math.min(outer.bottom, innerHeight, box.bottom);
      const top = Math.max(0, visibleTop - box.top), bottom = Math.max(top, visibleBottom - box.top);
      iframe.contentWindow?.postMessage({ id: instanceId, type: "reading-viewport", value: { top: Math.min(top, 2000000), bottom: Math.min(bottom, 2000000), visible: !document.hidden && visibleBottom > visibleTop && outer.width > 0 } }, "*");
    };
    const schedule = () => { if (!raf) raf = requestAnimationFrame(send); };
    const observer = new ResizeObserver(schedule); observer.observe(host); observer.observe(iframe);
    host.addEventListener("scroll", schedule, { passive: true }); window.addEventListener("resize", schedule); document.addEventListener("visibilitychange", schedule); iframe.addEventListener("load", schedule);
    schedule();
    return () => { observer.disconnect(); cancelAnimationFrame(raf); host.removeEventListener("scroll", schedule); window.removeEventListener("resize", schedule); document.removeEventListener("visibilitychange", schedule); iframe.removeEventListener("load", schedule); };
  }, [frame, scroll, instanceId, html]);
}
