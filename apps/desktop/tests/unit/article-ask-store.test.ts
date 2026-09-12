import { beforeEach, expect, it, vi } from "vitest";
const engine = vi.hoisted(() => vi.fn());
vi.mock("../../src/renderer/services/knowledge-ai/article-qa", () => ({ askArticle: engine }));
import { useArticleAskStore, parseArticleMessages } from "../../src/renderer/stores/article-ask.store";
const target = { itemId: "a", view: "body" as const };
let records: Map<string, any>;
beforeEach(() => {
  records = new Map(); engine.mockReset();
  useArticleAskStore.setState({ sessionId: null, itemId: null, target: null, messages: [], sessions: [], running: false, loading: false, loadError: null, saveError: null, webEnabled: true });
  Object.defineProperty(window, "api", { configurable: true, value: {
    askSession: { list: vi.fn().mockImplementation(async ({ itemId }) => [...records.values()].filter(r => r.itemId === itemId)), get: vi.fn().mockImplementation(async id => records.get(id)), save: vi.fn().mockImplementation(async value => { records.set(value.id, value); return value; }) },
    knowledge: { get: vi.fn().mockResolvedValue({ title: "文章", deletedAt: null }) }, log: { appError: vi.fn() },
  } });
});
it("切换文章后迟到的回答不能覆盖新会话", async () => {
  let resolve: () => void, oldPatch: (p: any) => void;
  engine.mockImplementation(input => { oldPatch = input.patch; return new Promise<void>(r => { resolve = r; }); });
  await useArticleAskStore.getState().open(target);
  const first = useArticleAskStore.getState().sessionId!;
  const task = useArticleAskStore.getState().ask("问题", target);
  await vi.waitFor(() => expect(engine).toHaveBeenCalled());
  oldPatch!({ answer: "已生成部分" });
  await useArticleAskStore.getState().open({ itemId: "b", view: "body" });
  oldPatch!({ answer: "迟到的错误覆盖", status: "done" }); resolve!(); await task;
  expect(useArticleAskStore.getState().itemId).toBe("b");
  expect(useArticleAskStore.getState().messages).toEqual([]);
  const saved = JSON.parse(records.get(first).messagesJson);
  expect(saved[0].answer).toBe("已生成部分"); expect(saved[0].status).toBe("error");
});
it("空会话不落盘，联网开关在已有会话中保存和恢复", async () => {
  await useArticleAskStore.getState().open(target);
  await useArticleAskStore.getState().persist(); expect(records.size).toBe(0);
  engine.mockImplementation(async input => input.patch({ answer: "回答", status: "done" }));
  useArticleAskStore.getState().setWeb(false);
  await useArticleAskStore.getState().ask("问题", target);
  const id = useArticleAskStore.getState().sessionId!;
  await useArticleAskStore.getState().open({ itemId: "b", view: "body" });
  await useArticleAskStore.getState().open(target, id);
  expect(useArticleAskStore.getState().webEnabled).toBe(false);
  expect(useArticleAskStore.getState().messages[0].answer).toBe("回答");
});
it("会话加载失败显示错误，不渲染成新会话", async () => {
  vi.mocked(window.api.askSession.list).mockRejectedValueOnce(new Error("数据库不可读"));
  await useArticleAskStore.getState().open(target);
  expect(useArticleAskStore.getState().loadError).toBe("数据库不可读");
  expect(useArticleAskStore.getState().sessionId).toBeNull();
});
it("保存失败保留待保存消息并阻止切换丢失", async () => {
  await useArticleAskStore.getState().open(target);
  useArticleAskStore.setState({ messages: [{ id: "m", question: "q", answer: "a", status: "done", sources: [], warnings: [], webEnabled: false, webStatus: "off" }] });
  vi.mocked(window.api.askSession.save).mockRejectedValue(new Error("磁盘已满"));
  await useArticleAskStore.getState().open({ itemId: "b", view: "body" });
  expect(useArticleAskStore.getState().itemId).toBe("a");
  expect(useArticleAskStore.getState().messages[0].answer).toBe("a");
  expect(useArticleAskStore.getState().saveError).toBeTruthy();
});
it("恢复中断输出，损坏记录不静默丢弃", () => {
  expect(parseArticleMessages('[{"id":"m","question":"q","answer":"partial","status":"running"}]')[0]).toMatchObject({ answer: "partial", status: "error" });
  expect(() => parseArticleMessages("{}" )).toThrow("损坏");
});
