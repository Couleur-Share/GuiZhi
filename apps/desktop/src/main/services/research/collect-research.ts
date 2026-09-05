import { withinResearchBudget } from "./budget";
import { randomUUID } from "node:crypto";
import type { ResearchDB, ResearchWorkflowDB } from "@guizhi/db";
import type { ResearchRun, ResearchSource, ResearchQueryAttempt, ResearchPage } from "@guizhi/shared/types";
import type { ResearchCollector } from "./collectors";
import { normalizeUrl } from "../import/url-normalize";

export function classifyResearchError(error: unknown): string {
  const text = error instanceof Error ? error.message : String(error);
  const bracket = /^\[([^\]]+)]/.exec(text)?.[1];
  if (bracket) return bracket;
  if (/登录/.test(text)) return "login_required";
  if (/验证|访问保护|-352|-412/.test(text)) return "verification_required";
  if (/结构.*变|platform.changed/i.test(text)) return "platform_changed";
  if (/429|限流/.test(text)) return "rate_limited";
  if (/timeout|超时/i.test(text)) return "timeout";
  if (/依赖|not.found/i.test(text)) return "dependency_unavailable";
  return "collector_failed";
}

export async function collectResearchSource(input: {
  run: ResearchRun; source: ResearchSource; collector: ResearchCollector; store: ResearchDB; workflow: ResearchWorkflowDB;
  signal: AbortSignal; changed: () => void; progress: (message?: string) => void;
  verify?: (topic: string, signal: AbortSignal) => Promise<void>;
}): Promise<void> {
  const { run, source, collector, store, workflow, signal, changed, progress, verify } = input;
  const maxPages = run.depth === "quick" ? 1 : 3;
  const queries = workflow.context(run.id)?.plan?.queries.slice(0, maxPages) ?? [run.topic];
  const previous = workflow.attempts(run.id).filter((a) => a.source === source && a.finishedAt != null && !a.errorCode);
  let pages = previous.length;
  let count = store.listCandidates(run.id).filter((c) => c.source === source).length;
  const state = queries.map((query) => {
    const done = previous.filter((a) => a.query === query);
    const last = done.at(-1);
    return { restarted: false, query, cursor: last?.nextCursor ?? null, done: Boolean(last?.finished), seen: new Set(done.map((a) => a.cursor ?? "__first__")) };
  });
  store.updateSource(run.id, source, { status: "running", collectedCount: count, startedAt: Date.now(), finishedAt: null, error: null, errorCode: null });
  changed();
  let active: ResearchQueryAttempt | undefined;
  try {
    if (verify) { progress("等待完成平台验证，通过后将自动补采"); await withinResearchBudget(signal, 180_000, (child) => verify(run.topic, child)); signal.throwIfAborted(); }
    let turn = 0;
    while (pages < maxPages && count < maxPages * 20 && state.some((s) => !s.done)) {
      signal.throwIfAborted();
      const task = state[turn++ % state.length];
      if (task.done) continue;
      const key = task.cursor ?? "__first__";
      if (task.seen.has(key)) throw new Error("采集器返回了重复游标");
      task.seen.add(key);
      active = { id: randomUUID(), runId: run.id, source, query: task.query, cursor: task.cursor, nextCursor: null, finished: false, method: source === "bilibili" ? "public-api" : "authenticated-browser", startedAt: Date.now(), finishedAt: null, returnedCount: 0, inWindowCount: 0, unknownDateCount: 0, capped: false };
      workflow.putAttempt(active);
      progress(`第 ${pages + 1}/${maxPages} 页 · 正在搜索`);
      let page: ResearchPage;
      try { page = await withinResearchBudget(signal, 90_000, (child) => collector.search({ topic: task.query, rangeFrom: run.rangeFrom, rangeTo: run.rangeTo, cursor: task.cursor, limit: 20, signal: child, onProgress: (message) => { if (!signal.aborted && !child.aborted) progress(`第 ${pages + 1}/${maxPages} 页 · ${message}`); } })); }
      catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        if (!signal.aborted && task.cursor && !task.restarted && /游标.*(?:失效|无效)|cursor.*(?:invalid|expired)|invalid.cursor/i.test(message)) {
          workflow.putAttempt({ ...active, finishedAt: Date.now(), errorCode: "cursor_expired", failureStage: "query", error: message });
          active = undefined; task.cursor = null; task.restarted = true; task.seen.clear(); continue;
        }
        throw error;
      }
      signal.throwIfAborted();
      const before = count;
      for (const item of page.items.slice(0, 20)) {
        const normalized = normalizeUrl(item.url);
        if (count >= maxPages * 20) break;
        if (normalized && store.upsertCandidate(run.id, item, normalized)) count += 1;
      }
      if ((count > before || page.items.length > 0) && run.reportMarkdown) { workflow.patchContext(run.id, { reportOutdated: true }); store.markReportOutdated(run.id); }
      pages += 1;
      task.cursor = page.cursor;
      task.done = !page.hasMore || !page.cursor;
      workflow.putAttempt({ ...active, nextCursor: page.cursor, finished: task.done, finishedAt: Date.now(), method: page.items[0]?.discoveryMethod ?? active.method,
        returnedCount: page.returnedCount ?? page.items.length,
        inWindowCount: page.inWindowCount ?? page.items.filter((c) => c.publishedAt != null && c.publishedAt >= run.rangeFrom && c.publishedAt <= run.rangeTo && c.dateConfidence !== "low").length,
        unknownDateCount: page.unknownDateCount ?? page.items.filter((c) => c.publishedAt == null || c.dateConfidence === "low").length,
        capped: (pages >= maxPages || count >= maxPages * 20) && state.some((s) => !s.done),
      });
      active = undefined;
      store.updateSource(run.id, source, { collectedCount: count }); changed();
    }
    signal.throwIfAborted();
    store.updateSource(run.id, source, { status: "succeeded", collectedCount: count, finishedAt: Date.now() });
  } catch (error) {
    if (!store.get(run.id)) return;
    const code = signal.aborted ? "canceled" : classifyResearchError(error);
    const message = error instanceof Error ? error.message : String(error);
    if (active) workflow.putAttempt({ ...active, finishedAt: Date.now(), failureStage: /login|verification/.test(code) ? "verification" : /navigation/.test(code) ? "navigation" : /platform.changed/.test(code) ? "parse" : /dependency/.test(code) ? "dependency" : "query", errorCode: code, error: message });
    if (!signal.aborted) store.updateSource(run.id, source, { status: code === "login_required" ? "login_required" : count ? "partial" : "failed", errorCode: code, error: message, collectedCount: count, finishedAt: Date.now() });
  } finally { progress(); }
  changed();
}
