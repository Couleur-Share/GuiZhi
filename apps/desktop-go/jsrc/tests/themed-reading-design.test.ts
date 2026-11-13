// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AIClientConfig } from "@guizhi/core";
import { themeTestPage } from "./themed-reading-test-fixtures";

const mocks = vi.hoisted(() => ({ chat: vi.fn(), usage: vi.fn() }));
vi.mock("@guizhi/core", () => ({ chatCompletion: mocks.chat }));
vi.mock("../services/ai-usage", () => ({ recordMainAiUsage: mocks.usage }));
import { callDesignModel, parseDesignJson, planTheme } from "../services/themed-reading/design";

const config = { model: "fixture-text", apiKey: "fixture-only", apiUrl: "https://model.example" } as AIClientConfig;
const response = (value: unknown) => ({ content: JSON.stringify(value), usage: { promptTokens: 123, completionTokens: 456 } });
beforeEach(() => { vi.clearAllMocks(); });

describe("主题设计文本编排", () => {
  it.each([[], [{prompt:"   "}]])("勾选生图后拒绝空配图方案，不伪装成成功", async assets => {
    mocks.chat.mockResolvedValue(response({direction:"主题插画",assets}));
    const page = themeTestPage({assets:[]}); page.options.generateImages = true;
    await expect(planTheme(page, config, new AbortController().signal)).rejects.toThrow("未提供有效配图方案");
    expect(JSON.parse(mocks.chat.mock.calls[0][1][1].content).minImages).toBe(1);
    expect(mocks.chat).toHaveBeenCalledTimes(1);
  });

  it("解析JSON代码围栏并拒绝数组、无对象和过长响应", () => {
    expect(parseDesignJson('```json\n{"html":"<main></main>","css":""}\n```')).toEqual({ html: "<main></main>", css: "" });
    expect(() => parseDesignJson('[{"direction":"wrong root"}]')).toThrow();
    expect(() => parseDesignJson("not json")).toThrow();
    expect(() => parseDesignJson("{" + "x".repeat(1000000) + "}")).toThrow("大小上限");
  });

  it("最多接受请求数量的生图计划，关闭配图时忽略模型返回图片", async () => {
    mocks.chat.mockResolvedValue(response({ direction: "琥珀色、麦穗与清晰文字", assets: Array.from({ length: 8 }, (_, index) => ({ id: `unsafe-${index}`, prompt: `图${index}`, aspectRatio: "invalid", blockId: "unknown" })) }));
    const page = themeTestPage({ assets: [] });
    const result = await planTheme(page, config, new AbortController().signal);
    expect(result.assets).toHaveLength(3);
    expect(new Set(result.assets.map((asset) => asset.id)).size).toBe(3);
    expect(result.assets.every((asset) => asset.aspectRatio === "16:9" && asset.blockId === "b0" && asset.status === "pending")).toBe(true);
    page.options.generateImages = false;
    expect((await planTheme(page, config, new AbortController().signal)).assets).toHaveLength(0);
    expect(JSON.parse(mocks.chat.mock.calls[1][1][1].content).maxImages).toBe(0);
  });

  it("配图规划收到实际 HTML 结构，并保留原文与事实边界", async () => {
    const page = themeTestPage({ assets: [] }); page.options.generateImages = false;
    page.source.blocks = [
      { id: "b0", markdown: "## 比较条件", html: "<h2>比较条件</h2>", text: "比较条件" },
      { id: "b1", markdown: "- 甲\n  - 子项\n- 乙", html: "<ul><li>甲<ul><li>子项</li></ul></li><li>乙</li></ul>", text: "甲子项乙" },
      { id: "b2", markdown: "表格与正文图", html: '<table><tr><th>名称</th><th>条件</th></tr><tr><td>甲</td><td>乙</td></tr></table><p><img src="local-image://source.png"></p>', text: "名称条件甲乙" },
    ];
    const before = JSON.stringify(page.source);
    mocks.chat.mockResolvedValueOnce(response({ direction: "原有比较条件按章节分组，窄屏单列", assets: [] }));
    await planTheme(page, config, new AbortController().signal);
    const planning = JSON.parse(mocks.chat.mock.calls[0][1][1].content);
    expect(planning.outline[0].structure.heading).toEqual({ level: 2, text: "比较条件" });
    expect(planning.outline[1]).toMatchObject({ id: "b1", element: "ul", structure: { list: { ordered: false, items: 2, nestedItems: 1 } } });
    expect(planning.outline[2].structure).toMatchObject({ table: { rows: 2, columns: 2 }, images: 1 });
    expect(mocks.chat.mock.calls[0][1][0].content).toContain("不能编造事实、数字、品牌排名");
    expect(planning.task).toContain("窄屏变化");
    expect(JSON.stringify(page.source)).toBe(before);
  });

  it("文本用量归属主题场景，取消后不接受迟到结果", async () => {
    mocks.chat.mockResolvedValue(response({ direction: "fixture" }));
    const controller = new AbortController();
    await callDesignModel(config, "request", controller.signal);
    expect(mocks.usage).toHaveBeenLastCalledWith({ scenario: "themedReading", model: "fixture-text", promptTokens: 123, completionTokens: 456 });
    mocks.chat.mockImplementationOnce(async () => { controller.abort(); return response({ direction: "late" }); });
    await expect(callDesignModel(config, "request", controller.signal)).rejects.toThrow();
    const error = new Error("network failed");
    mocks.chat.mockRejectedValueOnce(error);
    await expect(callDesignModel(config, "request", new AbortController().signal)).rejects.toThrow(error);
    expect(mocks.usage).toHaveBeenLastCalledWith({ scenario: "themedReading", model: "fixture-text", failed: true });
  });

  it("计数持久化失败或发送前取消时，不能发送文本请求或记作已调用", async () => {
    const onRequest = vi.fn(() => { throw new Error("用量检查点写入失败"); });
    await expect(callDesignModel(config, "request", new AbortController().signal, undefined, onRequest)).rejects.toThrow("检查点");
    expect(onRequest).toHaveBeenCalledTimes(1);
    expect(mocks.chat).not.toHaveBeenCalled();
    expect(mocks.usage).not.toHaveBeenCalled();
    onRequest.mockClear(); const controller = new AbortController(); controller.abort();
    await expect(callDesignModel(config, "request", controller.signal, undefined, onRequest)).rejects.toThrow();
    expect(onRequest).not.toHaveBeenCalled();
    expect(mocks.chat).not.toHaveBeenCalled();
  });
});
