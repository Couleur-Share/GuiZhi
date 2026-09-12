import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { ArticleTarget } from "@guizhi/shared/types/article-ask";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useArticleAskStore } from "../../stores/article-ask.store";
import { useToast } from "../ui/Toast";
import { ArticleConversation } from "./ArticleConversation";
import { locateArticleSource, matchArticleFrame, readerTarget } from "./article-reader-target";

export function ArticleAskReader({ itemId, open, onOpen, onClose, children }: { itemId: string; open: boolean; onOpen: () => void; onClose: () => void; children: ReactNode }) {
  const { t } = useTranslation(), { showToast } = useToast();
  const root = useRef<HTMLDivElement>(null), closeButton = useRef<HTMLElement | null>(null);
  const [wide, setWide] = useState(false), [target, setTarget] = useState<ArticleTarget>({ itemId, view: "body" });
  const [selection, setSelection] = useState<{ target: ArticleTarget; x: number; y: number } | null>(null);
  const [prompt, setPrompt] = useState({ text: "", key: 0 });
  const resolveTarget = useCallback(async () => {
    if (!root.current) throw new Error("阅读区尚未就绪");
    if (root.current.querySelector('[data-article-editing="true"]')) throw new Error("请先完成编辑并保存，再围绕本文提问");
    const knowledge = useKnowledgeStore.getState();
    if (!(await knowledge.flushPendingSave())) throw new Error("正文尚未保存，请重试保存");
    if (useKnowledgeStore.getState().hasUnsavedChanges) throw new Error("正文尚未保存，请重试保存");
    return readerTarget(root.current, itemId);
  }, [itemId]);
  useEffect(() => {
    const node = root.current;
    if (!node) return;
    const resize = new ResizeObserver(() => setWide(node.clientWidth >= 960)); resize.observe(node);
    let previous = "";
    const update = () => { const next = readerTarget(node, itemId); const key = JSON.stringify(next); if (key !== previous) { previous = key; setTarget(next); setSelection(null); } };
    const observer = new MutationObserver(update); observer.observe(node.querySelector("[data-testid=article-content]") ?? node, { childList: true, subtree: true, attributes: true, attributeFilter: ["data-article-target"] }); update();
    return () => { resize.disconnect(); observer.disconnect(); useArticleAskStore.getState().stop(); };
  }, [itemId]);
  useEffect(() => {
    if (!open) return;
    closeButton.current = document.activeElement as HTMLElement;
    requestAnimationFrame(() => root.current?.querySelector<HTMLTextAreaElement>("[data-testid='article-ask-panel'] textarea")?.focus({ preventScroll: true }));
    return () => { if (closeButton.current?.isConnected) closeButton.current.focus({ preventScroll: true }); };
  }, [open]);
  useEffect(() => {
    const node = root.current;
    if (!node) return;
    const show = (value: ArticleTarget, x: number, y: number) => {
      const b = node.getBoundingClientRect();
      setSelection({ target: value, x: Math.max(8, Math.min(x - b.left, b.width - 240)), y: Math.max(8, Math.min(y - b.top + 5, b.height - 40)) });
    };
    const native = (event: Event) => {
      // iframe 的选择不在父窗口 Selection 中；点选段菜单时不能先清空并卸载按钮。
      if (event.target instanceof Element && event.target.closest("[data-article-selection-tools]")) return;
      const selected = window.getSelection();
      if (!selected?.rangeCount || selected.isCollapsed) { setSelection(null); return; }
      const range = selected.getRangeAt(0), element = range.commonAncestorContainer.nodeType === 1 ? range.commonAncestorContainer as Element : range.commonAncestorContainer.parentElement;
      if (!element || !node.contains(element) || !element.closest("[data-article-reader]") || element.closest("input,textarea,button,[contenteditable],[data-testid='article-toolbar']")) return;
      const text = selected.toString().trim();
      if (!text || text.length > 4000) { setSelection(null); return; }
      const b = range.getBoundingClientRect(), floor = element.closest<HTMLElement>("[data-floor]")?.dataset.floor;
      show({ ...readerTarget(node, itemId), selection: text, floor: floor ? Number(floor) : undefined }, b.left, b.bottom);
    };
    const message = (event: MessageEvent) => {
      const frame = matchArticleFrame(node, event);
      if (!frame || event.data.type !== "article-selection") return;
      const value = event.data.value;
      if (value === null) { setSelection(null); return; }
      if (typeof value?.text !== "string" || !value.text.trim() || value.text.length > 4000 || !Number.isFinite(value.x) || !Number.isFinite(value.y)) return;
      const b = frame.getBoundingClientRect();
      show({ ...readerTarget(node, itemId), selection: value.text }, b.left + value.x, b.top + value.y);
    };
    const readingSelection=(event:Event)=>{const e=event as CustomEvent;if(!(e.target instanceof HTMLElement)||!e.target.dataset.readingViewId||!node.contains(e.target))return;const v=e.detail;if(typeof v?.text!=="string"||v.text.length>4000||!Number.isFinite(v.x)||!Number.isFinite(v.y))return;show({...readerTarget(node,itemId),selection:v.text},v.x,v.y);};
    node.addEventListener("reading-native-selection",readingSelection);
    node.addEventListener("mouseup", native); node.addEventListener("keyup", native); window.addEventListener("message", message);
    return () => { node.removeEventListener("reading-native-selection",readingSelection);node.removeEventListener("mouseup", native); node.removeEventListener("keyup", native); window.removeEventListener("message", message); };
  }, [itemId]);
  const askSelection = async (explain: boolean) => {
    if (!selection) return;
    try {
      await resolveTarget();
      setTarget(selection.target); setPrompt(p => ({ text: explain ? t("articleAsk.explainPrompt", "请结合本文上下文解释这段内容，并举一个容易理解的例子。") : "", key: p.key + 1 }));
      setSelection(null); onOpen();
    } catch (e) { showToast(t("articleAsk.sendFailed", "提问失败"), "error", { detail: String(e) }); }
  };
  return <div ref={root} className="relative flex min-h-0 flex-1" data-testid="article-ask-reader">
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">{children}</div>
    {selection ? <div data-article-selection-tools role="toolbar" aria-label={t("articleAsk.selectionTools", "选段提问")} onMouseDown={e => e.preventDefault()} className="absolute z-40 flex gap-2 rounded-lg border border-border bg-popover p-2 text-xs shadow-lg" style={{ left: selection.x, top: selection.y }}>
      <button type="button" onClick={() => void askSelection(true)}>{t("articleAsk.explain", "解释这段")}</button><button type="button" onClick={() => void askSelection(false)}>{t("articleAsk.askSelection", "围绕这段提问")}</button>
    </div> : null}
    {open ? <aside className={wide ? "h-full w-[400px] shrink-0 border-l border-border" : "absolute inset-y-0 right-0 z-30 w-[400px] max-w-full border-l border-border shadow-xl"}>
      <ArticleConversation target={target} prompt={prompt.text} promptKey={prompt.key} resolveTarget={resolveTarget} onSubmitted={() => { if (root.current) setTarget(readerTarget(root.current, itemId)); setPrompt(p => ({ text: "", key: p.key + 1 })); }} onLocate={source => root.current ? locateArticleSource(root.current, source) : Promise.resolve(false)} onClose={onClose} />
    </aside> : null}
  </div>;
}
