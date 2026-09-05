import { afterEach, describe, expect, it, vi } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import type { ResearchPage, ResearchSearchInput, ResearchSource } from "@guizhi/shared/types";
import { ResearchService } from "../../../src/main/services/research/research-service";

const databases: InstanceType<typeof DatabaseAdapter>[] = [];
afterEach(() => databases.splice(0).forEach((db) => db.close()));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

function page(source: ResearchSource, ids: string[] = [source]): ResearchPage {
  return {
    items: ids.map((id) => ({
      source, externalId: id, url: `https://www.${source}.com/video/${id}`,
      title: `本地 AI 知识库 ${id}`, author: "作者", publishedAt: Date.now() - 60_000, dateConfidence: "high", mediaType: "video", discoveryMethod: "fixture",
    })),
    cursor: null, hasMore: false,
  };
}

async function fixture(sources: ResearchSource[] = ["xiaohongshu", "douyin", "bilibili"]) {
  const db = new DatabaseAdapter(":memory:");
  databases.push(db);
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  const search = {
    douyin: vi.fn<(input: ResearchSearchInput) => Promise<ResearchPage>>()
      .mockRejectedValueOnce(new Error("[verification_required] 请完成搜索页验证"))
      .mockResolvedValue(page("douyin")),
    xiaohongshu: vi.fn().mockResolvedValue(page("xiaohongshu")),
    bilibili: vi.fn().mockResolvedValue(page("bilibili")),
  };
  const changed = vi.fn();
  const service = new ResearchService(db, {
    enqueueImports: (inputs) => inputs.map((_, index) => ({ id: `task-${index}` })) as never,
    onChanged: changed,
    collectors: {
      douyin: { source: "douyin", search: search.douyin },
      xiaohongshu: { source: "xiaohongshu", search: search.xiaohongshu },
      bilibili: { source: "bilibili", search: search.bilibili },
    },
  });
  const run = service.createAndRun({ topic: "本地 AI", dayRange: 7, depth: "quick", sources });
  const settled = () => vi.waitFor(() => expect(service.getDetail(run.id)?.run.status).not.toBe("collecting"));
  await settled();
  return { service, search, changed, run, settled };
}

describe("研究平台验证后自动补采", () => {
  it("验证成功自动只补采抖音，保留原研究范围、其他来源和已导入候选", async () => {
    const { service, search, changed, run, settled } = await fixture();
    const before = service.getDetail(run.id)!;
    service.enqueueCandidates(run.id, [before.candidates[0].id]);
    const verification = deferred<void>();
    const verify = vi.fn((_topic: string, _signal: AbortSignal) => verification.promise);
    expect(service.verifyAndRetrySource(run.id, "douyin", verify)).toMatchObject({ id: run.id, status: "collecting", completedAt: null });
    expect(verify).toHaveBeenCalledWith(run.topic, expect.any(AbortSignal));
    expect(service.getDetail(run.id)!.sources.find((s) => s.source === "douyin")).toMatchObject({
      status: "running", error: null, errorCode: null, finishedAt: null,
      progress: "等待完成平台验证，通过后将自动补采",
    });
    expect(search.douyin).toHaveBeenCalledTimes(1);
    expect(() => service.verifyAndRetrySource(run.id, "douyin", verify)).toThrow(/仍在执行/);
    verification.resolve();
    await settled();
    expect(search.douyin).toHaveBeenCalledTimes(2);
    expect(search.xiaohongshu).toHaveBeenCalledTimes(1);
    expect(search.bilibili).toHaveBeenCalledTimes(1);
    expect(search.douyin.mock.calls[1][0]).toMatchObject({ topic: run.topic, rangeFrom: run.rangeFrom, rangeTo: run.rangeTo });
    expect(service.list()).toHaveLength(1);
    const after = service.getDetail(run.id)!;
    expect(after.run).toMatchObject({ id: run.id, status: "ready", candidateCount: 3 });
    expect(after.sources.filter((s) => s.source !== "douyin")).toEqual(before.sources.filter((s) => s.source !== "douyin"));
    expect(after.candidates.find((c) => c.id === before.candidates[0].id)).toMatchObject({ state: "queued", importTaskId: "task-0" });
    for (const candidate of before.candidates) expect(after.candidates).toContainEqual(expect.objectContaining({ id: candidate.id, title: candidate.title, url: candidate.url }));
    expect(changed.mock.calls.at(-1)![0].run.status).toBe("ready");
  });

  it("已有抖音候选去重保留，累计数量和导入关联不被补采重置", async () => {
    const { service, search, run, settled } = await fixture();
    const existing = page("douyin").items[0];
    service.store.upsertCandidate(run.id, existing, existing.url);
    service.store.updateSource(run.id, "douyin", { collectedCount: 1, status: "partial" });
    const candidate = service.getDetail(run.id)!.candidates.find((c) => c.source === "douyin")!;
    service.enqueueCandidates(run.id, [candidate.id]);
    service.beginReport(run.id);
    service.saveReport(run.id, "旧结论 [R1]", "v1");
    const previousReport = service.getDetail(run.id)!.run.reportMarkdown;
    search.douyin.mockResolvedValue(page("douyin", ["douyin", "additional"]));
    service.verifyAndRetrySource(run.id, "douyin", async () => {});
    await settled();
    const detail = service.getDetail(run.id)!;
    expect(detail.sources.find((s) => s.source === "douyin")!.collectedCount).toBe(2);
    expect(detail.candidates.filter((c) => c.source === "douyin")).toHaveLength(2);
    expect(detail.candidates.find((c) => c.id === candidate.id)).toMatchObject({ state: "queued", importTaskId: "task-0" });
    expect(detail.run).toMatchObject({ reportStatus: "none", reportMarkdown: previousReport, reportError: "候选已补充，请重新生成报告以包含最新结果" });
  });

  it.each(["browser_closed", "login_required", "login_timeout"])("验证失败 %s 不启动采集，保留可重试的错误", async (code) => {
    const { service, search, run, settled } = await fixture();
    const before = service.getDetail(run.id)!;
    service.verifyAndRetrySource(run.id, "douyin", async () => { throw new Error(`[${code}] 验证未完成`); });
    await settled();
    expect(search.douyin).toHaveBeenCalledTimes(1);
    expect(service.getDetail(run.id)!.run).toMatchObject({ status: "partial", candidateCount: 2 });
    expect(service.getDetail(run.id)!.sources.find((s) => s.source === "douyin")).toMatchObject({ errorCode: code });
    expect(service.getDetail(run.id)!.sources.find((s) => s.source === "douyin")!.progress).toBeUndefined();
    expect(service.getDetail(run.id)!.sources.filter((s) => s.source !== "douyin")).toEqual(before.sources.filter((s) => s.source !== "douyin"));
    service.verifyAndRetrySource(run.id, "douyin", async () => {});
    await settled();
    expect(service.getDetail(run.id)!.run.status).toBe("ready");
  });

  it.each(["cancel", "delete"] as const)("等待验证时 %s，迟到的成功不会启动补采或恢复记录", async (action) => {
    const { service, search, run } = await fixture();
    const verification = deferred<void>();
    let signal!: AbortSignal;
    service.verifyAndRetrySource(run.id, "douyin", async (_topic, value) => { signal = value; await verification.promise; });
    service[action](run.id);
    expect(signal.aborted).toBe(true);
    verification.resolve();
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(search.douyin).toHaveBeenCalledTimes(1);
    if (action === "delete") expect(service.getDetail(run.id)).toBeNull();
    else expect(service.getDetail(run.id)!.run.status).toBe("canceled");
  });

  it("补采中取消时丢弃迟到候选，之后仍能重新验证恢复", async () => {
    const { service, search, run, settled } = await fixture();
    const response = deferred<ResearchPage>();
    search.douyin.mockReturnValueOnce(response.promise);
    service.verifyAndRetrySource(run.id, "douyin", async () => {});
    await vi.waitFor(() => expect(search.douyin).toHaveBeenCalledTimes(2));
    service.cancel(run.id);
    response.resolve(page("douyin", ["late"]));
    await new Promise((resolve) => setTimeout(resolve, 10));
    expect(service.getDetail(run.id)!.candidates.some((c) => c.source === "douyin")).toBe(false);
    service.verifyAndRetrySource(run.id, "douyin", async () => {});
    await settled();
    expect(service.getDetail(run.id)!.run.status).toBe("ready");
  });

  it("不接受不存在的研究、未选平台及报告生成期间的补采", async () => {
    const { service, run } = await fixture(["bilibili"]);
    const verify = vi.fn();
    expect(() => service.verifyAndRetrySource("missing", "douyin", verify)).toThrow(/不存在/);
    expect(() => service.verifyAndRetrySource(run.id, "douyin", verify)).toThrow(/不属于/);
    const next = await fixture();
    next.service.beginReport(next.run.id);
    expect(() => next.service.verifyAndRetrySource(next.run.id, "douyin", verify)).toThrow(/报告生成/);
    expect(verify).not.toHaveBeenCalled();
  });
});
