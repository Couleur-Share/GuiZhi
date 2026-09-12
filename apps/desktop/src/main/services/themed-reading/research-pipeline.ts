import type { ThemedReadingStage, ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type { ReadingReconstruction, ReadingResearchBatch } from "@guizhi/shared/types/reading-reconstruction";
import { searchReadingWeb } from "./search-service";
import { captureWebPage } from "../web-capture/web-capture";
import { runV3Research } from "./v3-research";

export const RESEARCH_LIMIT_ERROR = "补充查证仍未完成，已达到本次自动补查上限";
const MAX_SUPPLEMENT_BATCHES = 2;
type ResearchHooks = {
  checkpoint: () => void;
  stage: (stage: ThemedReadingStage, done?: number, total?: number) => void;
  request: (kind: "searchCalls" | "pagesRead") => void;
};
type ResearchModel = (prompt: unknown) => Promise<Record<string, any>>;
const canonicalUrl = (value: string) => { const url = new URL(value); url.hash = ""; return url.toString(); };
const queryKey = (value: string) => value.trim().replace(/\s+/g, " ").toLocaleLowerCase();
const batches = (state: ReadingReconstruction): ReadingResearchBatch[] => [state, ...(state.researchBatches ?? [])];
const followUpQueries = (value: unknown, used: Set<string>) => Array.isArray(value)
  ? value.filter((q): q is string => typeof q === "string" && Boolean(q.trim()) && q.length <= 400)
    .map(q => q.trim()).filter(q => { const key = queryKey(q); if (used.has(key)) return false; used.add(key); return true; }).slice(0, 3)
  : [];

/** 查证与撰稿使用相同资料范围；多轮补查也不无限扩张模型上下文。 */
export function researchReferenceContext(state: ReadingReconstruction) {
  const ready = state.references.filter(r => r.status === "ready");
  const size = Math.min(16000, Math.floor(96000 / Math.max(ready.length, 1)));
  return ready.map(r => ({ id: r.id, title: r.title, text: r.text.slice(0, size) }));
}

/** 空搜索结果可以在用户继续时重试；已有结果和已通过的检查点保持不变。 */
export function prepareResearchRetry(state: ReadingReconstruction) {
  for (const batch of batches(state)) if (!batch.researchReview) {
    for (const query of batch.queries) if (query.done && !query.results.length) query.done = false;
  }
}

async function collectBatch(version: ThemedReadingVersion, batch: ReadingResearchBatch, earlier: ReadingResearchBatch[], call: ResearchModel, signal: AbortSignal, hooks: ResearchHooks) {
  const state = version.reconstruction;
  for (const [index, query] of batch.queries.entries()) {
    hooks.stage("research", index, batch.queries.length);
    if (!query.done) {
      hooks.request("searchCalls"); query.results = await searchReadingWeb(query.query, signal);
      query.done = true; hooks.checkpoint();
    }
  }
  const seen = new Set(earlier.flatMap(b => b.selectedUrls ?? []).map(canonicalUrl));
  const candidates = batch.queries.flatMap(q => q.results).filter(result => {
    const key = canonicalUrl(result.url); if (seen.has(key)) return false; seen.add(key); return true;
  });
  if (!batch.selectedUrls && candidates.length) {
    const selection = await call({ task: "从候选网页中按相关性和来源直接程度排序选择最多8篇。优先官方、标准组织、原始研究及直接资料，减少重复转载；标题和URL仅供筛选，不代表事实已经核实。返回{urls:string[]}，只能使用候选URL。", questions: state.outline.questions, missing: earlier.at(-1)?.researchReview?.missing, candidates: candidates.map(r => ({ title: r.title, url: r.url })) });
    const urls = selection.urls;
    if (!Array.isArray(urls) || !urls.length || urls.length > 8 || new Set(urls).size !== urls.length || urls.some(url => !candidates.some(c => c.url === url))) throw new Error("研究资料选择无效，请重试联网");
    batch.selectedUrls = urls; hooks.checkpoint();
  }
  const selected = (batch.selectedUrls ?? []).map(url => batch.queries.flatMap(q => q.results).find(r => r.url === url)).filter(Boolean);
  for (const [index, result] of selected.entries()) {
    hooks.stage("research", index, selected.length);
    if (batch.readUrls?.includes(result.url)) continue;
    const previous = state.references.find(r => canonicalUrl(r.url) === canonicalUrl(result.url));
    let text = previous?.status === "ready" ? previous.text : result.text ?? "";
    let captureError: string | undefined;
    // 搜索服务提供的正文也可能只有入口简介；选中的来源始终尝试实际正文抓取。
    try {
      const captured = await captureWebPage({ url: result.url, purpose: "research", taskId: version.id }, signal);
      if (!captured.complete || captured.error || captured.markdown.trim().length < 200) throw new Error("正文未能完整取得");
      text = captured.markdown.slice(0, 60000);
    } catch { signal.throwIfAborted(); captureError = "网页完整正文获取失败，保留已取得资料供查证"; }
    const ready = text.trim().length >= 200;
    let id = previous?.id ?? `R${state.references.length + 1}`;
    for (let next = state.references.length + 2; !previous && state.references.some(r => r.id === id); next++) id = `R${next}`;
    const reference = { id, title: result.title, url: result.url, capturedAt: captureError && previous ? previous.capturedAt : Date.now(), text: ready ? text : "", publishedAt: result.publishedAt, status: ready ? "ready" as const : "failed" as const, error: ready ? captureError : "未取得可用于查证的网页正文，可继续重试" };
    if (previous) state.references[state.references.indexOf(previous)] = reference; else state.references.push(reference);
    if (ready) {
      (batch.readUrls ??= []).push(result.url);
      if (!previous || previous.status !== "ready" || !captureError) hooks.request("pagesRead");
    }
    hooks.checkpoint();
  }
  const references = researchReferenceContext(state);
  if (!references.length) throw new Error("联网查证未取得有效正文。请重试联网，或选择改为不联网生成");
  // 新一批没有取得新证据时，直接保留缺口，不能对旧资料反复做同一次付费判断。
  if (earlier.length && !batch.readUrls?.length) {
    batch.researchReview = { adequate: false, missing: earlier.at(-1).researchReview.missing, followUpQueries: [] };
  } else {
    const review = await call({ task: "判断现有正文是否足以覆盖原文主题的关键查证问题。返回{adequate:boolean,missing:string,followUpQueries:string[]}。不把搜索标题当证据；不要求解决所有学术分歧，明确呈现分歧也可以。缺少资料时说明具体缺口，并给出最多3个不同于已搜索问题的公开搜索词，优先直接来源；不把私有原文或笔记放入搜索词。", questions: state.outline.questions, searchedQueries: batches(state).flatMap(b => b.queries.map(q => q.query)), references });
    if (typeof review.adequate !== "boolean") throw new Error("资料查证结果格式无效，请继续重试");
    batch.researchReview = { adequate: review.adequate, missing: review.adequate ? "" : String(review.missing ?? "关键事实仍缺少直接资料").slice(0, 2000), followUpQueries: followUpQueries(review.followUpQueries, new Set(batches(state).flatMap(b => b.queries.map(q => queryKey(q.query))))) };
  }
  hooks.checkpoint();
}

export async function runReadingResearch(version: ThemedReadingVersion, call: ResearchModel, signal: AbortSignal, hooks: ResearchHooks) {
  if(version.formatVersion===3)return runV3Research(version,call,signal,hooks);
  const state = version.reconstruction;
  if (state.researchComplete) return;
  hooks.stage("research");
  if (!state.queries.length && !state.references.some(r => r.status === "ready")) {
    state.queries = [{ query: version.source.title.slice(0, 400), done: false, results: [] }]; hooks.checkpoint();
  }
  while (true) {
    signal.throwIfAborted();
    const all = batches(state), batch = all.at(-1);
    if (!batch.researchReview) await collectBatch(version, batch, all.slice(0, -1), call, signal, hooks);
    if (batch.researchReview.adequate) { state.researchComplete = true; hooks.checkpoint(); return; }
    if ((state.researchBatches?.length ?? 0) >= MAX_SUPPLEMENT_BATCHES) {
      throw new Error(`${RESEARCH_LIMIT_ERROR}（最多 2 轮、6 次补充搜索）。已完成内容已保留。缺少：${batch.researchReview.missing}`);
    }
    const used = new Set(all.flatMap(b => b.queries.map(q => queryKey(q.query))));
    let queries = followUpQueries(batch.researchReview.followUpQueries, used);
    if (!queries.length) {
      const plan = await call({ task: "根据缺失资料制定最多3个新的公开搜索词，返回{queries:string[]}。优先官方资料、标准正文或原始研究，不能重复已搜索词，不包含私有原文或笔记。", questions: state.outline.questions, missing: batch.researchReview.missing, searchedQueries: all.flatMap(b => b.queries.map(q => q.query)) });
      queries = followUpQueries(plan.queries, used);
      if (!queries.length) throw new Error(`补充查证搜索计划无效，请继续重试。缺少：${batch.researchReview.missing}`);
    }
    (state.researchBatches ??= []).push({ queries: queries.map(query => ({ query, done: false, results: [] })) });
    hooks.checkpoint();
  }
}
