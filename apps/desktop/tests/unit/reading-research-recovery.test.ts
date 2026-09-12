import { beforeEach, describe, expect, it, vi } from "vitest";
import { validateReadingReconstruction } from "@guizhi/shared/utils/reading-reconstruction";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { nativeSearchResults } from "../../src/main/services/themed-reading/native-search-response";
import { prepareResearchRetry, RESEARCH_LIMIT_ERROR, researchReferenceContext, runReadingResearch } from "../../src/main/services/themed-reading/research-pipeline";

const mocks = vi.hoisted(() => ({ search: vi.fn(), capture: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({ searchReadingWeb: mocks.search }));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({ captureWebPage: mocks.capture }));
const url = (name: string) => `https://example.org/${name}`;
const result = (name: string) => ({ title: name, url: url(name), text: "搜索服务返回的入口介绍。".repeat(30) });
const captured = { complete: true, markdown: "完整原始资料，包含定义、适用条件和原始证据。".repeat(100) };
const insufficient = (queries: string[]) => ({ adequate: false, missing: "需要取得标准正文和适用条件", followUpQueries: queries });
const hooksFor = (page: ReturnType<typeof reconstructionFixture>) => ({ checkpoint: vi.fn(() => validateReadingReconstruction(page.reconstruction)), stage: vi.fn(), request: vi.fn() });

function workingPage() {
  const page = reconstructionFixture(); page.role = "working"; page.design = null; page.options.research = true;
  page.reconstruction.outline.questions = ["查明原文关键概念"];
  page.reconstruction.queries = [{ query: "初次查询", done: false, results: [] }];
  page.reconstruction.draft = [];
  return page;
}
function legacyPage() {
  const page = workingPage();
  page.reconstruction.queries = [{ query: "初次查询", done: true, results: [result("old")] }];
  page.reconstruction.selectedUrls = [url("old")];
  page.reconstruction.references = [{ id: "R1", title: "旧资料", url: url("old"), capturedAt: 1, text: result("old").text, status: "ready" }];
  return page;
}

describe("AI 阅读查证恢复", () => {
  beforeEach(() => { mocks.search.mockReset(); mocks.capture.mockReset().mockResolvedValue(captured); });
  it("原生工具引用经正文采集进入查证，生成摘要不能替代网页正文", async () => {
    const page = workingPage();
    mocks.search.mockResolvedValue(nativeSearchResults({ status: "completed", output: [
      { type: "web_search_call", status: "completed", action: { type: "search" } },
      { type: "message", content: [{ type: "output_text", text: "模型生成的摘要".repeat(100), annotations: [{ type: "url_citation", title: "直接来源", url: url("native") }] }] },
    ] }));
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("native")] }).mockResolvedValueOnce({ adequate: true });
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(page.reconstruction.references[0]).toMatchObject({ url: url("native"), text: captured.markdown, status: "ready" });
    expect(call.mock.calls[1][0].references[0].text).not.toContain("模型生成的摘要");
    expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("超过 200 字的服务正文仍尝试原页，充分性审核使用实际取得的全文", async () => {
    const page = workingPage(); mocks.search.mockResolvedValue([result("source")]);
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("source")] }).mockResolvedValueOnce({ adequate: true });
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.capture).toHaveBeenCalledWith(expect.objectContaining({ url: url("source"), purpose: "research" }), expect.anything());
    expect(call.mock.calls[1][0].references[0].text).toBe(captured.markdown);
    expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("旧失败检查点保留完成工作，按具体缺口补查不同资料，不重搜已完成查询", async () => {
    const page = legacyPage(), originalNotes = [...page.reconstruction.notes];
    mocks.capture.mockRejectedValueOnce(new Error("旧网页正文无法取得"));
    mocks.search.mockResolvedValue([result("standard")]);
    const call = vi.fn().mockResolvedValueOnce(insufficient(["标准全文适用条件"]))
      .mockResolvedValueOnce({ urls: [url("standard")] }).mockResolvedValueOnce({ adequate: true });
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.search.mock.calls.map(c => c[0])).toEqual(["标准全文适用条件"]);
    expect(mocks.capture.mock.calls.map(c => c[0].url)).toEqual([url("old"), url("standard")]);
    expect(page.reconstruction.notes).toEqual(originalNotes);
    expect(page.reconstruction.references.map(r => r.id)).toEqual(["R1", "R2"]);
    expect(page.reconstruction.references[0].text).toBe(result("old").text);
    expect(page.reconstruction.researchBatches).toHaveLength(1);
    expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("补查取消后从最后的正文检查点恢复，不重复搜索、筛选和已经成功的抓取", async () => {
    let page = legacyPage(), saved = structuredClone(page);
    page.reconstruction.researchReview = insufficient(["标准直接来源"]);
    const controller = new AbortController();
    mocks.search.mockResolvedValue([result("one"), result("two")]);
    mocks.capture.mockResolvedValueOnce(captured).mockImplementationOnce(async () => { controller.abort(); throw new Error("已取消"); });
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("one"), url("two")] }).mockResolvedValueOnce({ adequate: true });
    const hooks = { ...hooksFor(page), checkpoint: vi.fn(() => { validateReadingReconstruction(page.reconstruction); saved = structuredClone(page); }) };
    await expect(runReadingResearch(page, call, controller.signal, hooks)).rejects.toThrow();
    page = structuredClone(saved);
    expect(page.reconstruction.researchBatches[0].readUrls).toEqual([url("one")]);
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.search).toHaveBeenCalledTimes(1);
    expect(mocks.capture.mock.calls.map(c => c[0].url)).toEqual([url("one"), url("two"), url("two")]);
    expect(call).toHaveBeenCalledTimes(2);
    expect(page.reconstruction.researchComplete).toBe(true);
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(call).toHaveBeenCalledTimes(2);
  });

  it("两批补查最多六次新搜索；达上限后不降级、不反复请求模型，检查点仍可读取", async () => {
    const page = legacyPage(); page.reconstruction.researchReview = insufficient(["问题一", "问题二", "问题三"]);
    let searches = 0, reviews = 0;
    mocks.search.mockImplementation(async () => [result(`new-${++searches}`)]);
    const call = vi.fn(async (input: any) => input.candidates
      ? { urls: input.candidates.map(c => c.url) }
      : insufficient(++reviews === 1 ? ["补充问题一", "补充问题二", "补充问题三"] : ["不应再搜索"]));
    const hooks = hooksFor(page);
    await expect(runReadingResearch(page, call, new AbortController().signal, hooks)).rejects.toThrow(RESEARCH_LIMIT_ERROR);
    expect(mocks.search).toHaveBeenCalledTimes(6); expect(mocks.capture).toHaveBeenCalledTimes(6); expect(call).toHaveBeenCalledTimes(4);
    expect(page.options.research).toBe(true); expect(page.reconstruction.researchComplete).not.toBe(true);
    expect(page.reconstruction.researchBatches).toHaveLength(2); expect(page.reconstruction.references).toHaveLength(7);
    validateReadingReconstruction(page.reconstruction);
    prepareResearchRetry(page.reconstruction);
    await expect(runReadingResearch(page, call, new AbortController().signal, hooks)).rejects.toThrow(RESEARCH_LIMIT_ERROR);
    expect(mocks.search).toHaveBeenCalledTimes(6); expect(call).toHaveBeenCalledTimes(4);
  });

  it("首次未取得正文时，继续只重试失败抓取，搜索和选择不会重复", async () => {
    const page = workingPage(); mocks.search.mockResolvedValue([{ title: "仅标题", url: url("retry") }]);
    mocks.capture.mockRejectedValueOnce(new Error("读取超时"));
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("retry")] }).mockResolvedValueOnce({ adequate: true });
    await expect(runReadingResearch(page, call, new AbortController().signal, hooksFor(page))).rejects.toThrow("未取得有效正文");
    expect(page.reconstruction.references[0].status).toBe("failed");
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.search).toHaveBeenCalledTimes(1); expect(mocks.capture).toHaveBeenCalledTimes(2); expect(call).toHaveBeenCalledTimes(2);
    expect(page.reconstruction.references).toHaveLength(1); expect(page.reconstruction.references[0].status).toBe("ready");
  });

  it("搜索无结果后用户可以重新查找，不能把空结果永久视为已完成", async () => {
    const page = workingPage(); mocks.search.mockResolvedValueOnce([]).mockResolvedValueOnce([result("found")]);
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("found")] }).mockResolvedValueOnce({ adequate: true });
    await expect(runReadingResearch(page, call, new AbortController().signal, hooksFor(page))).rejects.toThrow("未取得有效正文");
    prepareResearchRetry(page.reconstruction);
    await runReadingResearch(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.search).toHaveBeenCalledTimes(2); expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("多轮资料有统一文本预算，查证与撰稿都能看到早先的有效来源", () => {
    const page = workingPage();
    page.reconstruction.references = Array.from({ length: 24 }, (_, i) => ({ id: `R${i + 1}`, title: `资料${i}`, url: url(`${i}`), capturedAt: 1, status: "ready" as const, text: "正文".repeat(20000) }));
    const context = researchReferenceContext(page.reconstruction);
    expect(context).toHaveLength(24); expect(context[0].id).toBe("R1");
    expect(context.reduce((total, ref) => total + ref.text.length, 0)).toBeLessThanOrEqual(96000);
  });

  it.each([
    (page: ReturnType<typeof workingPage>) => { page.reconstruction.researchBatches = Array.from({ length: 3 }, () => ({ queries: [] })); },
    (page: ReturnType<typeof workingPage>) => { page.reconstruction.researchBatches = [{ queries: [], selectedUrls: [url("forged")] }]; },
    (page: ReturnType<typeof workingPage>) => { page.reconstruction.readUrls = [url("forged")]; },
    (page: ReturnType<typeof workingPage>) => { page.reconstruction.researchReview = { ...insufficient([]), adequate: "true" as any }; },
  ])("恢复数据拒绝超预算或伪造检查点 %#", corrupt => {
    const page = workingPage(); corrupt(page); expect(() => validateReadingReconstruction(page.reconstruction)).toThrow();
  });
});
