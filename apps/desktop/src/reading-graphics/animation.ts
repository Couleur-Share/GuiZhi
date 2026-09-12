import { createTimeline, svg } from "animejs";
import type { ReadingAnimation } from "@guizhi/shared/types/reading-visuals";

/** 所有参数已由主进程验证；这里只执行固定预设，无模型代码。 */
function startReadingMotion() {
  const root = document.documentElement, lifetime = new AbortController();
  const embedded = Boolean(root.dataset.instance), reduced = matchMedia("(prefers-reduced-motion: reduce)");
  const definitions: ReadingAnimation[] = JSON.parse(document.getElementById("gz-system-motion-data").textContent);
  let enabled = !reduced.matches, disposed = false, raf = 0;
  let viewport = { top: 0, bottom: innerHeight, visible: !embedded };
  type RecordState = { slot: HTMLElement; original: SVGElement; definitions: ReadingAnimation[]; seen: boolean; manual: boolean; generation: number; state: string; timeline?: ReturnType<typeof createTimeline>; css: Animation[]; button: HTMLButtonElement };
  const records: RecordState[] = [];
  const button = (label: string, action: () => void) => { const b = document.createElement("button"); b.type = "button"; b.textContent = label; b.addEventListener("click", action, { signal: lifetime.signal }); return b; };
  const toggle = button("", () => { enabled = !enabled; applyEnabled(); });
  toggle.className = "gz-system-motion-toggle";
  root.dataset.readingMotion = enabled ? "on" : "off";
  const setState = (r: RecordState, value: string) => { r.state = value; r.slot.dataset.motionState = value; r.button.textContent = value === "playing" ? "暂停动画" : value === "paused" ? "继续动画" : "播放动画"; };
  const restore = (r: RecordState) => {
    r.generation++; r.timeline?.revert(); r.timeline = undefined;
    r.css.forEach(a => a.cancel()); r.css = [];
    r.slot.querySelector("svg")?.replaceWith(r.original.cloneNode(true));
  };
  const complete = (r: RecordState) => { restore(r); setState(r, "done"); schedule(); };
  const fail = (r: RecordState) => {
    restore(r); r.seen = true; setState(r, "failed");
    r.button.textContent = "动画不可用，已显示静态图"; r.button.disabled = true;
    if (embedded) parent.postMessage({ id: root.dataset.instance, type: "reading-motion-error", value: { visualId: r.slot.dataset.readingVisual } }, "*");
  };
  const pause = (r: RecordState, manual = false) => {
    if (r.state !== "playing") return;
    r.timeline?.pause(); r.css.forEach(a => a.pause()); r.manual = manual; setState(r, "paused");
  };
  const visible = (r: RecordState) => {
    if (document.hidden || !viewport.visible || r.slot.closest("[hidden]") || !r.slot.getClientRects().length) return false;
    const box = r.slot.querySelector("svg").getBoundingClientRect();
    const top = embedded ? viewport.top : 0, bottom = embedded ? viewport.bottom : innerHeight;
    return Math.max(0, Math.min(box.bottom, bottom) - Math.max(box.top, top)) >= Math.min(box.height, bottom - top) * .25 && box.height > 0 && bottom > top;
  };
  const play = (r: RecordState, manual = false, replay = false) => {
    if (!enabled || disposed || r.state === "failed") return;
    for (const other of records) if (other !== r && other.state === "playing") pause(other, manual);
    try {
      if (!replay && r.state === "paused") {
        r.manual = false; setState(r, "playing"); r.timeline?.resume(); r.css.forEach(a => a.play()); return;
      }
      restore(r); r.seen = true; r.manual = false; setState(r, "playing");
      const generation = r.generation;
      const done = () => { if (generation === r.generation && !disposed) complete(r); };
      if (r.definitions.length) {
        const timeline = createTimeline({ autoplay: false, onComplete: done }); r.timeline = timeline;
        for (const a of [...r.definitions].sort((a, b) => (a.order ?? 0) - (b.order ?? 0))) {
          const target = document.getElementById(a.targetId), path = a.pathId && document.getElementById(a.pathId);
          const duration = a.duration ?? 1000;
          if (a.preset === "draw") timeline.add(svg.createDrawable(target), { draw: ["0 0", "0 1"], duration, ease: "inOutQuad" });
          if (a.preset === "reveal") timeline.add(target, { opacity: [0, Number(getComputedStyle(target).opacity)], duration, ease: "outQuad" });
          if (a.preset === "motion") timeline.add(target, { ...svg.createMotionPath(path), duration, ease: "linear" });
          if (a.preset === "morph") timeline.add(target, { d: svg.morphTo(path), duration, ease: "inOutQuad" });
        }
        timeline.play();
      } else {
        // 浏览器自身播放经过校验的 SVG CSS 关键帧，不重复实现图表动画。
        r.css = r.slot.getAnimations({ subtree: true });
        if (!r.css.length) { done(); return; }
        Promise.all(r.css.map(a => a.finished)).then(done).catch(() => { /* 取消和恢复会使 finished 拒绝。 */ });
      }
    } catch { fail(r); }
  };
  function tick() {
    raf = 0; if (disposed || !enabled) return;
    for (const r of records) if (r.state === "playing" && !visible(r)) pause(r);
    if (records.some(r => r.state === "playing")) return;
    const candidate = records.find((r, i) => visible(r) && !r.manual && (r.state === "paused" || (!r.seen && i < 3)));
    if (candidate) play(candidate);
  }
  function schedule() { if (!raf && !disposed) raf = requestAnimationFrame(tick); }
  function applyEnabled() {
    root.dataset.readingMotion = enabled ? "on" : "off";
    toggle.textContent = enabled ? "关闭动画" : "启用动画"; toggle.setAttribute("aria-pressed", String(enabled));
    if (!enabled) records.forEach(r => { restore(r); setState(r, "done"); });
    records.forEach(r => { r.slot.querySelectorAll<HTMLButtonElement>(".gz-system-motion-controls button").forEach(b => b.disabled = !enabled || r.state === "failed"); });
    schedule();
  }
  for (const slot of document.querySelectorAll<HTMLElement>("[data-reading-visual]")) {
    const items = definitions.filter(a => a.visualId === slot.dataset.readingVisual);
    if (!items.length && !slot.hasAttribute("data-css-motion")) continue;
    const original = slot.querySelector("svg").cloneNode(true) as SVGElement;
    const r: RecordState = { slot, original, definitions: items, seen: false, manual: false, generation: 0, state: "ready", css: [], button: undefined };
    const controls = document.createElement("div"); controls.className = "gz-system-controls gz-system-motion-controls"; controls.setAttribute("role", "group"); controls.setAttribute("aria-label", "图形动画控制");
    r.button = button("播放动画", () => { if (r.state === "playing") pause(r, true); else play(r, true); });
    controls.append(r.button, button("重播动画", () => play(r, true, true))); slot.append(controls); records.push(r);
  }
  if (!records.length) return;
  const toolbar = document.createElement("div"); toolbar.className = "gz-system-controls"; toolbar.append(toggle); records[0].slot.before(toolbar);
  addEventListener("scroll", schedule, { passive: true, signal: lifetime.signal });
  addEventListener("resize", schedule, { passive: true, signal: lifetime.signal });
  document.addEventListener("visibilitychange", schedule, { signal: lifetime.signal });
  document.addEventListener("toggle", schedule, { capture: true, signal: lifetime.signal });
  const observer = new ResizeObserver(schedule); records.forEach(r => observer.observe(r.slot));
  addEventListener("message", event => {
    if (!embedded || event.source !== parent || event.data?.id !== root.dataset.instance || event.data.type !== "reading-viewport") return;
    const v = event.data.value;
    if (!v || !Number.isFinite(v.top) || !Number.isFinite(v.bottom) || v.top < 0 || v.bottom < v.top || v.bottom > 2000000 || typeof v.visible !== "boolean") return;
    viewport = v; schedule();
  }, { signal: lifetime.signal });
  reduced.addEventListener("change", () => { enabled = !reduced.matches; applyEnabled(); }, { signal: lifetime.signal });
  addEventListener("beforeprint", () => { records.forEach(r => { restore(r); setState(r, "done"); }); }, { signal: lifetime.signal });
  addEventListener("pagehide", () => { disposed = true; lifetime.abort(); observer.disconnect(); cancelAnimationFrame(raf); records.forEach(restore); }, { once: true });
  applyEnabled();
}
try { startReadingMotion(); } catch { document.documentElement.dataset.readingMotion = "off"; }
