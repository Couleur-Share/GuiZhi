import type { ThemedReadingVersion } from "@guizhi/shared/types/themed-reading";
import type { ReadingResearchBatch } from "@guizhi/shared/types/reading-reconstruction";
import { searchReadingWeb } from "./search-service";
import { captureWebPage } from "../web-capture/web-capture";
import { readingPool } from "./v3-stream";
import type { ReadingV3Hooks } from "./v3-pipeline";

const canonical = (s: string) => {
  const u = new URL(s);
  u.hash = "";
  return u.href;
};
export async function runV3Research(
  page: ThemedReadingVersion,
  call: (v: unknown) => Promise<Record<string, any>>,
  signal: AbortSignal,
  hooks: Pick<ReadingV3Hooks, "checkpoint" | "stage" | "request">,
) {
  const s = page.reconstruction,
    g = page.generation,
    deep = page.options.researchDepth === "deep";
  if (
    !s.queries.length &&
    s.revisionDraft?.length &&
    !s.outline.questions.length
  ) {
    s.researchComplete = true;
    hooks.checkpoint();
    return;
  }
  if (!s.queries.length)
    s.queries = [
      { query: page.source.title.slice(0, 400), done: false, results: [] },
    ];
  for (;;) {
    signal.throwIfAborted();
    const batches: ReadingResearchBatch[] = [s, ...(s.researchBatches ?? [])],
      round = batches.length - 1,
      batch = batches[round];
    const queryLimit = deep ? 3 : round ? 1 : 2,
      pageLimit = deep ? 8 : round ? 2 : 4;
    batch.queries = batch.queries.slice(0, queryLimit);
    if (!batch.researchReview) {
      try {
        await readingPool(
          batch.queries,
          g.limited ? 1 : 2,
          async (query, index) => {
            if (query.done) return;
            hooks.stage("research", index, batch.queries.length);
            hooks.request("searchCalls");
            query.results = await searchReadingWeb(query.query, signal);
            query.done = true;
            hooks.checkpoint();
          },
        );
      } catch (e) {
        if (/429|限流|频繁/.test(String(e))) {
          g.limited = true;
          hooks.checkpoint();
        }
        throw e;
      }
      const used = new Set(
        batches
          .slice(0, round)
          .flatMap((b) => b.selectedUrls ?? [])
          .map(canonical),
      );
      const candidates = batch.queries
        .flatMap((q) => q.results)
        .filter((r) => {
          const k = canonical(r.url);
          if (used.has(k)) return false;
          used.add(k);
          return true;
        });
      if (!batch.selectedUrls) {
        if (!candidates.length)
          throw new Error("未取得可查证来源，请继续联网或改为不联网生成");
        const selected = await call({
          task: `选择最多${pageLimit}篇相关且直接的资料，优先官方与原始来源，返回{urls:string[]}，只能选择候选URL。`,
          questions: s.outline.questions,
          candidates: candidates.map((r) => ({ title: r.title, url: r.url })),
        });
        if (Array.isArray(selected.urls) && !selected.urls.length)
          throw new Error("搜索结果与查证问题不相关，请调整检索词、切换搜索服务或改为不联网生成");
        if (
          !Array.isArray(selected.urls) ||
          !selected.urls.length ||
          selected.urls.length > pageLimit ||
          new Set(selected.urls).size !== selected.urls.length ||
          selected.urls.some((u) => !candidates.some((r) => r.url === u))
        )
          throw new Error("资料选择结果无效");
        batch.selectedUrls = selected.urls;
        hooks.checkpoint();
      }
      const selected = batch.selectedUrls
        .map((url) =>
          batch.queries.flatMap((q) => q.results).find((r) => r.url === url),
        )
        .filter(Boolean);
      // ID 在请求前分配，避免并发完成顺序改变来源身份。
      for (const result of selected)
        if (
          !s.references.some((r) => canonical(r.url) === canonical(result.url))
        )
          s.references.push({
            id: `R${s.references.length + 1}`,
            title: result.title,
            url: result.url,
            capturedAt: Date.now(),
            text: "",
            status: "failed",
          });
      hooks.checkpoint();
      await readingPool(selected, g.limited ? 1 : 3, async (result) => {
        if (batch.readUrls?.includes(result.url)) return;
        const ref = s.references.find(
          (r) => canonical(r.url) === canonical(result.url),
        );
        if (ref.status !== "ready") {
          try {
            const captured = await captureWebPage(
              { url: result.url, purpose: "research", taskId: page.id },
              signal,
            );
            if (
              !captured.complete ||
              captured.error ||
              captured.markdown.trim().length < 200
            )
              throw new Error("未取得完整网页正文");
            if (
              captured.markdown.length > 8 * 1024 * 1024 ||
              new TextEncoder().encode(JSON.stringify(page)).byteLength +
                new TextEncoder().encode(captured.markdown).byteLength >
                15 * 1024 * 1024
            )
              throw new Error("来源全文超过页面记录容量，已保留其他资料");
            Object.assign(ref, {
              text: captured.markdown,
              status: "ready",
              capturedAt: Date.now(),
              error: undefined,
            });
            hooks.request("pagesRead");
          } catch (e) {
            signal.throwIfAborted();
            ref.error =
              `完整正文抓取失败：${e instanceof Error ? e.message : String(e)}`.slice(
                0,
                1000,
              );
            if (/429|限流|频繁/.test(String(e))) {
              g.limited = true;
              hooks.checkpoint();
              throw e;
            }
          }
        }
        if (ref.status === "ready") (batch.readUrls ??= []).push(result.url);
        hooks.checkpoint();
      });
      const ready = s.references.filter((r) => r.status === "ready");
      if (!ready.length)
        throw new Error("联网查证未取得有效正文，请继续或改为不联网生成");
      const size = Math.min(16000, Math.floor(96000 / ready.length));
      const review = await call({
        task: "判断正文是否足以支持关键问题，返回{adequate:boolean,missing:string,followUpQueries:string[],evidence:[{id,quotes:string[],sections:number[]}]}。sections是与该摘录相关的提纲章节0基索引，只填相关章节。只要求关键事实有据，分歧可说明；每个来源提取相关原文摘录，quotes必须逐字存在于给定正文，每个来源合计最多6000字符。缺口给出新的公开检索词，最多3个。",
        questions: s.outline.questions,
        outline: s.outline.sections,
        references: ready.map((r) => ({
          id: r.id,
          title: r.title,
          text: r.text.slice(0, size),
        })),
      });
      if (typeof review.adequate !== "boolean")
        throw new Error("查证结果格式无效");
      const evidence = (
        Array.isArray(review.evidence) ? review.evidence : []
      ).flatMap((e) => {
        const ref = ready.find((r) => r.id === e?.id);
        if (!ref || !Array.isArray(e.quotes)) return [];
        const quotes = e.quotes.filter(
          (q) => typeof q === "string" && q.trim() && ref.text.includes(q),
        );
        return quotes.length
          ? [
              {
                id: ref.id,
                text: quotes.join("\n").slice(0, 6000),
                sections: Array.isArray(e.sections)
                  ? [
                      ...new Set<number>(
                        e.sections.filter(
                          (i) =>
                            Number.isSafeInteger(i) &&
                            i >= 0 &&
                            i < s.outline.sections.length,
                        ),
                      ),
                    ]
                  : s.outline.sections.map((_, i) => i),
              },
            ]
          : [];
      });
      if (review.adequate && !evidence.length)
        throw new Error("查证结果未提供可核对的正文证据，请继续查证");
      g.evidence = evidence;
      batch.researchReview = {
        adequate: review.adequate,
        missing: String(review.missing ?? "").slice(0, 2000),
        followUpQueries: (Array.isArray(review.followUpQueries)
          ? review.followUpQueries
          : []
        )
          .filter((q) => typeof q === "string" && q.trim() && q.length <= 400)
          .slice(0, 3),
      };
      hooks.checkpoint();
    }
    if (batch.researchReview.adequate) {
      s.researchComplete = true;
      hooks.checkpoint();
      return;
    }
    if (round >= (g.researchRoundLimit ?? (deep ? 2 : 1)))
      throw new Error(
        `已达到${deep ? "深度" : "标准"}查证上限，已保留资料。继续将增加一轮查证预算。关键缺口：${batch.researchReview.missing}`,
      );
    const searched = new Set(
      batches.flatMap((b) =>
        b.queries.map((q) => q.query.trim().toLowerCase()),
      ),
    );
    const queries = batch.researchReview.followUpQueries
      .filter((q) => !searched.has(q.trim().toLowerCase()))
      .slice(0, deep ? 3 : 1);
    if (!queries.length)
      throw new Error(`缺少新的补查问题：${batch.researchReview.missing}`);
    (s.researchBatches ??= []).push({
      queries: queries.map((query) => ({ query, done: false, results: [] })),
    });
    hooks.checkpoint();
  }
}
