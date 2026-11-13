import { beforeEach, describe, expect, it, vi } from "vitest";
import { validateReadingReconstruction } from "@guizhi/shared/utils/reading-reconstruction";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { prepareResearchRetry } from "../services/themed-reading/research-pipeline";
import { runV3Research } from "../services/themed-reading/v3-research";

const mocks = vi.hoisted(() => ({ search: vi.fn(), capture: vi.fn() }));
vi.mock("../services/themed-reading/search-service", () => ({ searchReadingWeb: mocks.search }));
vi.mock("../services/web-capture/web-capture", () => ({ captureWebPage: mocks.capture }));
const url = (name: string) => `https://example.org/${name}`;
const result = (name: string) => ({ title: name, url: url(name), text: "搜索服务返回的入口介绍。".repeat(30) });
const captured = { complete: true, markdown: "完整原始资料，包含定义、适用条件和原始证据。".repeat(100) };
const adequate = { adequate: true, missing: "", followUpQueries: [], evidence: [{ id: "R1", quotes: [captured.markdown.slice(0, 40)] }] };
const insufficient = (queries: string[]) => ({ adequate: false, missing: "需要取得标准正文和适用条件", followUpQueries: queries });
const hooksFor = (page: ReturnType<typeof reconstructionFixture>) => ({ checkpoint: vi.fn(() => validateReadingReconstruction(page.reconstruction, 20)), stage: vi.fn(), request: vi.fn() });

function workingPage() {
  const page = reconstructionFixture(); page.role = "working"; page.design = null; page.options.research = true;
  page.reconstruction.outline.questions = ["查明原文关键概念"];
  page.reconstruction.queries = [{ query: "初次查询", done: false, results: [] }];
  page.reconstruction.draft = [];
  return page;
}
function v3Page() {
  const page = workingPage();
  page.formatVersion = 3; page.options.researchDepth = "standard";
  page.generation = { route: "research", sections: [], css: "", scripts: [], libraries: [], revision: 0, done: false, repairs: {}, issues: [] };
  return page;
}

describe("AI 阅读查证恢复", () => {
  beforeEach(() => { mocks.search.mockReset(); mocks.capture.mockReset().mockResolvedValue(captured); });
  it("只有标题与 URL 的搜索结果经正文采集进入查证", async () => {
    const page = v3Page();
    // 搜索服务只给出标题与 URL（原生搜索的摘要文本由 Electron 侧 native-reading-search 测试覆盖），正文必须经网页采集取得。
    mocks.search.mockResolvedValue([{ title: "直接来源", url: url("native") }]);
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("native")] }).mockResolvedValueOnce(adequate);
    await runV3Research(page, call, new AbortController().signal, hooksFor(page));
    expect(page.reconstruction.references[0]).toMatchObject({ url: url("native"), text: captured.markdown, status: "ready" });
    expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("搜索无结果后继续会重新查找，不能把空结果永久视为已完成", async () => {
    const page = v3Page(); mocks.search.mockResolvedValueOnce([]).mockResolvedValueOnce([result("found")]);
    const call = vi.fn().mockResolvedValueOnce({ urls: [url("found")] }).mockResolvedValueOnce(adequate);
    await expect(runV3Research(page, call, new AbortController().signal, hooksFor(page))).rejects.toThrow("未取得可查证来源");
    expect(page.reconstruction.queries[0]).toMatchObject({ done: true, results: [] });
    prepareResearchRetry(page.reconstruction);
    await runV3Research(page, call, new AbortController().signal, hooksFor(page));
    expect(mocks.search).toHaveBeenCalledTimes(2); expect(page.reconstruction.researchComplete).toBe(true);
  });

  it("继续时只重置未审核批次里的空结果查询，已有结果和已审核批次保持不变", () => {
    const page = workingPage(), state = page.reconstruction;
    state.queries = [{ query: "已审核的空结果", done: true, results: [] }];
    state.researchReview = insufficient(["补充问题"]);
    state.researchBatches = [{ queries: [{ query: "已有结果", done: true, results: [result("kept")] }, { query: "空结果", done: true, results: [] }] }];
    prepareResearchRetry(state);
    expect(state.queries[0].done).toBe(true);
    expect(state.researchBatches[0].queries.map(q => q.done)).toEqual([true, false]);
    expect(state.researchBatches[0].queries[0].results).toHaveLength(1);
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
