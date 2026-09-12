import { beforeEach, describe, it, expect, vi } from "vitest";
const mocks = vi.hoisted(() => ({
  call: vi.fn(),
  chat: vi.fn(),
  research: vi.fn(),
}));
vi.mock("@guizhi/core", () => ({ chatCompletion: mocks.chat }));
vi.mock("virtual:reading-libraries", () => ({
  default: { components: {}, runtime: {} },
}));
vi.mock("../../src/main/services/themed-reading/design", () => ({
  callDesignModel: mocks.call,
  planTheme: vi.fn(),
}));
vi.mock("../../src/main/services/themed-reading/v3-research", () => ({
  runV3Research: mocks.research,
}));
vi.mock("../../src/main/services/ai-usage", () => ({
  recordMainAiUsage: vi.fn(),
}));
import {
  runReadingV3,
  semanticReadingChunks,
} from "../../src/main/services/themed-reading/v3-pipeline";
import { themeTestPage } from "./themed-reading-test-fixtures";
const body = "正文包含完整的知识解释与条件，不依赖脚本执行。".repeat(10);
const records = [
  { type: "meta", css: "" },
  {
    type: "section",
    id: "intro",
    html: '<main><h1>标题</h1><h2>说明</h2><div data-reading-copy="s0b0"></div></main>',
  },
  { type: "done" },
];
const hooks = () => ({ checkpoint: vi.fn(), stage: vi.fn(), request: vi.fn() });
const fixture = () =>
  themeTestPage({
    formatVersion: 3,
    assets: [],
    design: null,
    options: {
      style: "",
      research: false,
      action: "create",
      enhancedInteraction: true,
      generateImages: false,
      maxImages: 0,
    },
    reconstruction: {
      notes: [],
      queries: [],
      references: [],
      draft: [],
      interactions: [],
    },
  });
beforeEach(() => {
  vi.clearAllMocks();
  mocks.call.mockResolvedValue({
    outline: {
      title: "标题",
      direction: "简洁",
      questions: [],
      sections: [{ title: "说明", brief: "完整解释" }],
    },
    draft: [{ title: "说明", markdown: body, referenceIds: [] }],
  });
  mocks.chat.mockImplementation(async (_config, _messages, options) => {
    for (const r of records) options.onDelta(JSON.stringify(r) + "\n");
    return { content: "", finishReason: "stop" };
  });
});
describe("v3 自适应生成", () => {
  it("离线规划中的多余研究问题不导致整稿失败", async () => {
    mocks.call.mockResolvedValue({
      outline: {
        title: "标题",
        direction: "简洁",
        questions: ["一", "二", "三", "四", "五"],
        sections: [{ title: "说明", brief: "完整解释" }],
      },
      draft: [{ title: "说明", markdown: body, referenceIds: [] }],
    });
    const page = fixture();
    await runReadingV3(
      page,
      { model: "test" } as any,
      new AbortController().signal,
      hooks(),
    );
    expect(page.reconstruction.outline.questions).toEqual([]);
    expect(page.design.html).toContain(body);
    expect(mocks.call).toHaveBeenCalledTimes(1);
  });
  it("短文正常流程仅两个文本请求并提前保存预览", async () => {
    const page = fixture(),
      h = hooks();
    const checkpoints = [];
    h.checkpoint.mockImplementation(() =>
      checkpoints.push(structuredClone(page)),
    );
    await runReadingV3(
      page,
      { model: "test" } as any,
      new AbortController().signal,
      h,
    );
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(mocks.chat).toHaveBeenCalledTimes(1);
    expect(mocks.research).not.toHaveBeenCalled();
    expect(page.design.html).toContain(body);
    expect(
      checkpoints.some((p) => p.generation?.sections.length && !p.design),
    ).toBe(true);
  });
  it("截断重试复用编辑稿和完整章节", async () => {
    const page = fixture();
    mocks.chat.mockImplementationOnce(async (_c, _m, o) => {
      o.onDelta(
        records
          .slice(0, 2)
          .map((r) => JSON.stringify(r))
          .join("\n") + "\n",
      );
      throw new Error("网络断开");
    });
    await expect(
      runReadingV3(
        page,
        { model: "test" } as any,
        new AbortController().signal,
        hooks(),
      ),
    ).rejects.toThrow("网络断开");
    expect(page.design).toBeNull();
    expect(page.generation.sections).toHaveLength(1);
    mocks.chat.mockImplementationOnce(async (_c, _m, o) => {
      o.onDelta(
        JSON.stringify(records[0]) + "\n" + JSON.stringify(records[2]) + "\n",
      );
      return { content: "", finishReason: "stop" };
    });
    await runReadingV3(
      page,
      { model: "test" } as any,
      new AbortController().signal,
      hooks(),
    );
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(page.design.html).toContain(body);
  });
  it("语义切块保留所有输入", () => {
    const raw = "甲".repeat(15900) + "\n\n中间条件" + "乙".repeat(40000);
    expect(semanticReadingChunks(raw).join("")).toBe(raw);
  });
  it("取消后的增量不能保存", async () => {
    const page = fixture(),
      c = new AbortController();
    mocks.chat.mockImplementationOnce(async (_a, _b, o) => {
      c.abort();
      o.onDelta(JSON.stringify(records[0]) + "\n");
    });
    await expect(
      runReadingV3(page, { model: "test" } as any, c.signal, hooks()),
    ).rejects.toThrow();
    expect(page.design).toBeNull();
    expect(page.generation.sections).toEqual([]);
  });
});
