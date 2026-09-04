import { beforeEach, describe, expect, it, vi } from "vitest";

const { runScenarioChat } = vi.hoisted(() => ({ runScenarioChat: vi.fn() }));
vi.mock("../../../src/renderer/services/knowledge-ai/ai-invoke", () => ({
  runScenarioChat,
}));

import {
  classifyInboxItems,
  parseCollectionClassificationResponse,
} from "../../../src/renderer/services/knowledge-ai/classify-collections";

beforeEach(() => runScenarioChat.mockReset());

describe("处理中心 AI 智能归类", () => {
  it("解析代码块污染的 JSON，并按请求顺序返回", () => {
    expect(
      parseCollectionClassificationResponse(
        '```json\n{"assignments":[{"id":"b","collection":" 健康养护 "},{"id":"a","collectionName":"编程开发"}]}\n```',
        ["a", "b"],
      ),
    ).toEqual([
      { itemId: "a", collectionName: "编程开发" },
      { itemId: "b", collectionName: "健康养护" },
    ]);
  });

  it("拒绝漏项、重复项与未知 ID，不让部分计划落库", () => {
    expect(() =>
      parseCollectionClassificationResponse(
        '{"assignments":[{"id":"a","collection":"编程开发"}]}',
        ["a", "b"],
      ),
    ).toThrow("漏掉 1 条");
    expect(() =>
      parseCollectionClassificationResponse(
        '{"assignments":[{"id":"a","collection":"编程开发"},{"id":"a","collection":"技术"}]}',
        ["a"],
      ),
    ).toThrow("重复分类");
    expect(() =>
      parseCollectionClassificationResponse(
        '{"assignments":[{"id":"x","collection":"编程开发"}]}',
        ["a"],
      ),
    ).toThrow("未知条目");
  });

  it("分批分类时把前一批新分类加入后续目录，避免重复造类", async () => {
    const items = Array.from({ length: 21 }, (_, index) => ({
      itemId: `item-${index + 1}`,
      title: `条目 ${index + 1}`,
      excerpt: "健康与日常管理",
    }));
    runScenarioChat
      .mockResolvedValueOnce({
        content: JSON.stringify({
          assignments: items.slice(0, 20).map((item) => ({
            id: item.itemId,
            collection: "健康养护",
          })),
        }),
        model: "fast-model",
      })
      .mockResolvedValueOnce({
        content: JSON.stringify({
          assignments: [{ id: "item-21", collection: "健康养护" }],
        }),
        model: "fast-model",
      });
    const progress = vi.fn();

    const result = await classifyInboxItems(items, ["编程开发"], {
      onProgress: progress,
    });

    expect(result).toHaveLength(21);
    expect(runScenarioChat).toHaveBeenCalledTimes(2);
    const secondPrompt = runScenarioChat.mock.calls[1][1][1].content;
    expect(secondPrompt).toContain("健康养护");
    expect(progress).toHaveBeenNthCalledWith(1, 1, 2);
    expect(progress).toHaveBeenNthCalledWith(2, 2, 2);
  });
});
