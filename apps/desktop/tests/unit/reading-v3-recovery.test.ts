import { beforeEach, describe, it, expect, vi } from "vitest";
const mocks = vi.hoisted(() => ({ call: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/design", () => ({
  callDesignModel: mocks.call,
}));
vi.mock("virtual:reading-libraries", () => ({
  default: { runtime: {}, components: {} },
}));
import { repairReadingRuntime } from "../../src/main/services/themed-reading/v3-runtime-repair";
import { applyReadingDraftUpdates } from "../../src/main/services/themed-reading/v3-editor";
import { reduceReadingNotes } from "../../src/main/services/themed-reading/v3-notes";
import { completeReadingCopy } from "../../src/main/services/themed-reading/v3-copy";
import { createReadingGeneration } from "../../src/main/services/themed-reading/v3-pipeline";
import { themeTestPage } from "./themed-reading-test-fixtures";

beforeEach(() => vi.clearAllMocks());
describe("v3 局部恢复", () => {
  it("页面遗漏的正文自动补齐，已有完整正文不重复", () => {
    const g = createReadingGeneration(themeTestPage());
    g.sections = [
      { id: "first", html: "<h1>标题</h1><h2>第一章</h2><p>已有内容</p>" },
    ];
    const draft = [
      { title: "第一章", markdown: "已有内容", referenceIds: [] },
      { title: "第二章", markdown: "关键适用条件不能丢失", referenceIds: [] },
    ];
    completeReadingCopy(draft, g);
    expect(g.sections).toHaveLength(2);
    expect(g.sections[1].html).toContain("关键适用条件不能丢失");
    completeReadingCopy(draft, g);
    expect(g.sections).toHaveLength(2);
  });
  it("只替换指定章节，保留未修改章节及引用", () => {
    const original = [
      { title: "甲", markdown: "原内容", referenceIds: ["R1"] },
      { title: "乙", markdown: "原条件", referenceIds: [] },
    ];
    const next = applyReadingDraftUpdates(original, [
      { index: 1, title: "乙", markdown: "新说明", referenceIds: [] },
    ]);
    expect(next[0]).toEqual(original[0]);
    expect(next[1].markdown).toBe("新说明");
    expect(original[1].markdown).toBe("原条件");
    expect(() => applyReadingDraftUpdates(original, [{ index: 3 }])).toThrow(
      "索引",
    );
  });
  it("超长笔记分组汇总，继续时不重复已完成请求", async () => {
    const g = createReadingGeneration(themeTestPage()),
      notes = Array.from({ length: 12 }, (_, i) => String(i).repeat(9000));
    const call = vi.fn().mockResolvedValue({ notes: "汇总正文" }),
      save = vi.fn();
    const text = await reduceReadingNotes(notes, g, call, save),
      calls = call.mock.calls.length;
    expect(calls).toBeGreaterThan(1);
    expect(text).toContain("汇总");
    expect(await reduceReadingNotes(notes, g, call, save)).toBe(text);
    expect(call).toHaveBeenCalledTimes(calls);
    expect(notes).toHaveLength(12);
  });
  it("运行错误只修复故障模块一次，持久化候选与检查点", async () => {
    const p = themeTestPage({
      formatVersion: 3,
      assets: [],
      options: {
        style: "",
        maxImages: 0,
        generateImages: false,
        enhancedInteraction: true,
      },
    });
    p.design = {
      html: `<h1>标题</h1><h2>说明</h2><p>${"完整静态正文".repeat(30)}</p>`,
      css: "",
      direction: "",
      assets: [],
      scripts: [
        { id: "broken", code: 'throw new Error("bad")', status: "ready" },
        { id: "good", code: "const x=1", status: "ready" },
      ],
    };
    p.generation = createReadingGeneration(p);
    const verify = vi
      .fn()
      .mockResolvedValueOnce("交互 broken 运行失败：bad")
      .mockResolvedValue(undefined);
    mocks.call.mockResolvedValue({ code: "const x=2" });
    await repairReadingRuntime(
      p,
      { model: "test" } as any,
      new AbortController().signal,
      verify,
      vi.fn(),
      vi.fn(),
    );
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(p.design.scripts[1].code).toBe("const x=1");
    expect(p.generation.scripts[0].code).toBe("const x=2");
    verify.mockResolvedValue("交互 broken 运行失败：bad");
    await repairReadingRuntime(
      p,
      { model: "test" } as any,
      new AbortController().signal,
      verify,
      vi.fn(),
      vi.fn(),
    );
    expect(mocks.call).toHaveBeenCalledTimes(1);
    expect(p.design.scripts.every((s) => s.status === "failed")).toBe(true);
  });
});
