import type { ArticleSource, ArticleTarget } from "@guizhi/shared/types/article-ask";
import { normalizeArticleText } from "@guizhi/shared/utils/article-context";

export function readerTarget(root: HTMLElement, itemId: string): ArticleTarget {
  const frames = [...root.querySelectorAll<HTMLIFrameElement>("iframe[data-article-target]")].filter(n => n.getClientRects().length && n.offsetHeight > 0);
  const element = frames[0] ?? root.querySelector<HTMLElement>("[data-article-reader]");
  const target = element?.dataset.articleTarget ? JSON.parse(element.dataset.articleTarget) as ArticleTarget : { itemId, view: "body" as const };
  if (target.itemId !== itemId) throw new Error("阅读内容已切换，请稍后重试");
  return target;
}
export function matchArticleFrame(root: HTMLElement, event: MessageEvent): HTMLIFrameElement | undefined {
  if (event.origin !== "null" || !event.data?.id) return undefined;
  return [...root.querySelectorAll<HTMLIFrameElement>("iframe[data-article-instance]")].find(frame => frame.getClientRects().length && frame.contentWindow === event.source && frame.dataset.articleInstance === event.data.id);
}
export async function locateArticleSource(root: HTMLElement, source: ArticleSource): Promise<boolean> {
  if (!source.target) return false;
  if (source.fingerprint) {
    const result = await window.api.articleAsk.context({ target: { ...source.target, selection: undefined }, question: "" });
    if (!result.success || result.context?.fingerprint !== source.fingerprint) return false;
  }
  window.dispatchEvent(new CustomEvent("article-ask-navigate", { detail: source.target }));
  const text = normalizeArticleText(source.target.selection || source.text).slice(0, 180);
  // 等阅读视图完成切换；只使用匹配版本，不把历史引用定位到新版。
  for (let attempt = 0; attempt < 15; attempt++) {
    await new Promise(resolve => setTimeout(resolve, 100));
    const target = readerTarget(root, source.target.itemId);
    if (target.view !== source.target.view || target.versionId !== source.target.versionId) continue;
    const native=root.querySelector<HTMLElement>("[data-reading-view-id]");
    if(native?.dataset.readingViewId){
      const viewId=native.dataset.readingViewId;
      const requestId=crypto.randomUUID();
      return new Promise(resolve=>{
        let unsubscribe=()=>{};
        const finish=(found:boolean)=>{clearTimeout(timer);unsubscribe();resolve(found);};
        const timer=setTimeout(()=>finish(false),2500);
        unsubscribe=window.api.themedReading.onViewEvent(event=>{const v=event.value as {count?:number;requestId?:string};if(event.viewId===viewId&&event.type==="find"&&v?.requestId===requestId)finish(Number(v.count)>0);});
        void window.api.themedReading.commandView({viewId,command:{type:"find",query:text,index:0,requestId}}).then(r=>{if(!r.success)finish(false);}).catch(()=>finish(false));
      });
    }
    const frame = [...root.querySelectorAll<HTMLIFrameElement>("iframe[data-article-instance]")].find(n => n.getClientRects().length);
    if (frame) {
      return new Promise(resolve => {
        const request = crypto.randomUUID();
        const finish = (found: boolean) => { clearTimeout(timer); window.removeEventListener("message", receive); resolve(found); };
        const receive = (event: MessageEvent) => {
          if (matchArticleFrame(root, event) !== frame || event.data.type !== "article-located" || event.data.value?.request !== request) return;
          const value = event.data.value;
          if (value.found === true && Number.isFinite(value.top)) frame.parentElement?.scrollTo({ top: Math.max(0, Math.min(value.top, 2000000)) });
          finish(value.found === true);
        };
        const timer = setTimeout(() => finish(false), 2500);
        window.addEventListener("message", receive);
        frame.contentWindow?.postMessage({ id: frame.dataset.articleInstance, type: "article-locate", value: { text, request } }, "*");
      });
    }
    const elements = [...root.querySelectorAll<HTMLElement>("[data-article-reader] p, [data-article-reader] li, [data-article-reader] h1, [data-article-reader] h2, [data-article-reader] h3, [data-article-reader] pre, [data-article-reader] td")];
    const found = elements.find(n => n.getClientRects().length && normalizeArticleText(n.textContent ?? "").includes(text));
    if (found) { found.scrollIntoView({ block: "center" }); found.classList.add("ring-2", "ring-primary"); setTimeout(() => found.classList.remove("ring-2", "ring-primary"), 2500); return true; }
  }
  return false;
}
