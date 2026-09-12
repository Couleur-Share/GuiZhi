import { beforeEach, expect, it, vi } from "vitest";
import type { ArticleContext, ArticleMessage } from "@guizhi/shared/types/article-ask";
const chat = vi.hoisted(() => vi.fn());
vi.mock("../../src/renderer/services/knowledge-ai/ai-invoke", () => ({ runScenarioChat: chat }));
import { askArticle } from "../../src/renderer/services/knowledge-ai/article-qa";
const context: ArticleContext = { target: { itemId: "a", view: "body" }, title: "文章", fingerprint: "hash", clipped: false, sources: [{ ordinal: 1, kind: "article", title: "原文", text: "证据" }] };
beforeEach(() => {
  chat.mockReset();
  Object.defineProperty(window, "api", { configurable: true, value: { articleAsk: { context: vi.fn().mockResolvedValue({ success: true, context }), search: vi.fn(), cancelSearch: vi.fn().mockResolvedValue({ success: true }) }, knowledge: { get: vi.fn().mockResolvedValue({ id: "a", deletedAt: null }) } } });
});
it("不联网只调用一次问答，流式增量正确累积", async () => {
  const patches: Partial<ArticleMessage>[] = [];
  chat.mockImplementation(async (_scenario, _messages, options) => { options.onDelta("第一段"); options.onDelta("第二段"); return { content: "第一段第二段", model: "test" }; });
  await askArticle({ target: context.target, question: "解释", history: [], webEnabled: false, requestId: "r", signal: new AbortController().signal, patch: p => patches.push(p) });
  expect(chat).toHaveBeenCalledTimes(1);
  expect(window.api.articleAsk.search).not.toHaveBeenCalled();
  expect(patches.some(p => p.answer === "第一段第二段")).toBe(true);
  expect(patches.at(-1)?.status).toBe("done");
});
it("搜索失败仍生成解释并明确保留未查证状态", async () => {
  chat.mockResolvedValueOnce({ content: '["概念"]', model: "test" }).mockResolvedValueOnce({ content: "结合本文解释[1]", model: "test", finishReason: "length" });
  vi.mocked(window.api.articleAsk.search).mockResolvedValue({ success: false, error: "HTTP 402" });
  const patches: Partial<ArticleMessage>[] = [];
  await askArticle({ target: context.target, question: "解释", history: [], webEnabled: true, requestId: "r", signal: new AbortController().signal, patch: p => patches.push(p) });
  expect(patches.some(p => p.webStatus === "failed")).toBe(true);
  expect(patches.some(p => p.warnings?.join().includes("402"))).toBe(true);
  expect(patches.at(-1)?.truncated).toBe(true);
});
it("重试使用保存的上下文，删除文章后不能继续", async () => {
  chat.mockResolvedValue({ content: "旧依据", model: "test" });
  const input = { target: context.target, context, question: "解释", history: [], webEnabled: false, requestId: "r", signal: new AbortController().signal, patch: vi.fn() };
  await askArticle(input); expect(window.api.articleAsk.context).not.toHaveBeenCalled();
  vi.mocked(window.api.knowledge.get).mockResolvedValue(null);
  await expect(askArticle(input)).rejects.toThrow("已删除");
});
