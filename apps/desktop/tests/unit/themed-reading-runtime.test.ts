import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db";
import type { ThemedReadingAsset, ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";

const mocks = vi.hoisted(() => ({ call: vi.fn(), chat:vi.fn(), search: vi.fn(), capture: vi.fn(), plan: vi.fn(), generate: vi.fn(), original: vi.fn(), notify: vi.fn(), cleanup: vi.fn() }));
vi.mock("@guizhi/core",async(importOriginal)=>({...await importOriginal<typeof import("@guizhi/core")>(),chatCompletion:(...args:unknown[])=>mocks.chat(...args)}));
vi.mock("../../src/main/services/media/media-summary", () => ({ resolveMediaSummaryConfig: () => ({ model: "text-test" }) }));
vi.mock("../../src/main/services/illustration/image-gen", () => ({ resolveImageGenConfig: () => ({ model: "image-test" }) }));
vi.mock("../../src/main/services/asset-cleanup", () => ({ cleanupOrphanAssets: mocks.cleanup, retainAssetFiles: () => () => undefined }));
vi.mock("../../src/main/diagnostic-log", () => ({ logAppError: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/design", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../src/main/services/themed-reading/design")>(),
  callDesignModel: (...args: unknown[]) => mocks.call(...args), planTheme: mocks.plan,
}));
vi.mock("../../src/main/services/themed-reading/assets", () => ({ collectOriginalThemeAssets: () => [], generateThemeAsset: mocks.generate, prepareOriginalAsset: mocks.original }));
// 图片文件校验另有独立测试；这里只验证任务编排，不读写真实素材目录。
vi.mock("../../src/main/services/themed-reading/image-health", () => ({ inspectThemedReadingImages: async (version: unknown) => version }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({ searchReadingWeb: mocks.search }));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({ captureWebPage: mocks.capture }));
import { LEGACY_PAGE_ERROR, LEGACY_TASK_ERROR, ThemedReadingRuntime } from "../../src/main/services/themed-reading/runtime";
import { reconstructionFixture } from "./reading-reconstruction-fixture";
import { themedVersion } from "./db/themed-reading-fixture";

const options = { style: "暖色手册", generateImages: false, maxImages: 3 };
const body = "保留 60–70 度、限定条件与全部原文。";
const asset = (id: string): ThemedReadingAsset => ({ id, role: "generated", purpose: "主题插画", prompt: "麦穗", alt: id, aspectRatio: "16:9", status: "pending", blockId: "b0" });
const editorResult = (direction = "琥珀色手册") => ({ outline: { title: "啤酒", direction, questions: [], sections: [{ title: "完整正文", brief: "保留条件" }] }, draft: [{ title: "完整正文", markdown: body, referenceIds: [] }] });
const pageHtml = (assetIds: string[] = []) => `<h1>啤酒</h1>${assetIds.map(id => `<img data-theme-asset="${id}" alt="${id}">`).join("")}<h2>完整正文</h2><p>${body.repeat(8)}</p>`;
const streamPage = (withImages = true) => async (_config: unknown, messages: { content: string }[], streamOptions: { onDelta: (delta: string) => void }) => {
  const input = JSON.parse(messages[1].content);
  const ids = withImages ? input.assets.map((a: { id: string }) => a.id) : [];
  for (const record of [{ type: "meta", css: "" }, { type: "section", id: "article", html: pageHtml(ids) }, { type: "done" }]) streamOptions.onDelta(JSON.stringify(record) + "\n");
  return { content: "", finishReason: "stop" };
};
const pendingEditor = () => {
  let resolve: (value: unknown) => void;
  mocks.call.mockImplementationOnce((_config, _prompt, _signal, _chat, onRequest?: () => void) => { onRequest?.(); return new Promise(done => { resolve = done; }); });
  return (value: unknown) => resolve(value);
};
const noModelCalls = () => { for (const mock of [mocks.call, mocks.chat, mocks.plan, mocks.generate]) expect(mock).not.toHaveBeenCalled(); };

describe("AI 阅读主进程持久任务", () => {
  let db: Database, runtime: ThemedReadingRuntime, itemId: string;
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.search.mockReset(); mocks.capture.mockReset();
    mocks.call.mockImplementation(async (_config, prompt: string, _signal, _chat, onRequest?: () => void) => {
      onRequest?.(); const input = JSON.parse(prompt);
      if (input.task.startsWith("一次完成编辑稿")) return editorResult();
      if (input.task.startsWith("规划专题")) return { title: "啤酒", direction: "琥珀色手册", questions: ["啤酒分类"], searchQueries: [], sections: [{ title: "完整正文", brief: "保留条件" }] };
      if (input.task.startsWith("撰写指定章节")) return { sections: input.sections.map(() => ({ title: "完整正文", markdown: body, referenceIds: [] })) };
      throw new Error(`未预期的模型请求：${input.task}`);
    });
    mocks.chat.mockImplementation(streamPage());
    mocks.plan.mockImplementation(async (_v, _c, _s, onRequest) => { onRequest?.(); return { direction: "琥珀色手册", assets: [] }; });
    mocks.generate.mockImplementation(async (a, _direction, _config, signal, checkpoint, _cleanup, onRequest) => {
      signal.throwIfAborted(); onRequest?.(); Object.assign(a, { status: "ready", fileName: `${a.id}.png`, sha256: "a".repeat(64), bytes: 100, error: undefined }); checkpoint();
    });
    db = new Database(":memory:"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    itemId = new KnowledgeItemDB(db).create({ title: "啤酒", content: `# 原文标题\n\n${body}` }).id;
    runtime = new ThemedReadingRuntime(db, mocks.notify);
  });
  afterEach(async () => { await runtime.shutdown(); db.close(); });
  const terminal = async (id: string) => {
    await vi.waitFor(() => expect(["completed", "partial", "failed", "cancelled", "interrupted"]).toContain(runtime.pages.getTask(id)?.state));
    return runtime.pages.getTask(id)!;
  };
  const v3Work = (item: string, patch: Partial<ThemedReadingVersion> = {}): ThemedReadingVersion =>
    ({ ...reconstructionFixture(), id: `v3-work-${item}`, itemId: item, role: "working", formatVersion: 3, design: null, ...patch });
  const v3Design = (assetIds: string[]) => ({ direction: "啤酒", html: pageHtml(assetIds), css: "", assets: [], scripts: [], libraries: [] });
  const legacyPage = (format: 1 | 2, item: string): ThemedReadingVersion => format === 1
    ? themedVersion(item, { id: `legacy-v1-${item}`, assets: [asset("g0")] })
    : { ...reconstructionFixture(), id: `legacy-v2-${item}`, itemId: item, role: "working", assets: [asset("g0")] };
  const publishLegacy = (format: 1 | 2, item = itemId) => {
    const page = legacyPage(format, item);
    runtime.pages.saveVersion(page); runtime.pages.publish(page.id);
    return runtime.pages.get(item, "body");
  };
  const legacyWork = (format: 1 | 2, item = itemId): ThemedReadingVersion =>
    ({ ...legacyPage(format, item), id: `work-v${format}-${item}`, design: null, options: { style: "", generateImages: false, maxImages: 0, research: true } });
  const taskFor = (version: ThemedReadingVersion, state: ThemedReadingTask["state"], id: string): ThemedReadingTask =>
    ({ id, itemId: version.itemId, sourceKind: "body", versionId: version.id, title: "啤酒", state, stage: "research", completed: 0, total: 1, error: "关键资料不足", createdAt: 1, updatedAt: 1 });

  it.each([
    ["未带动作的内部调用", {}],
    ["只带动作的旧调用", { action: "create" as const }],
    ["界面默认调用", { action: "create" as const, enhancedInteraction: true, researchDepth: "standard" as const }],
  ])("%s也一律生成 v3", async (_name, extra) => {
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, ...extra } });
    expect((await terminal(task.id)).state).toBe("completed");
    const page = runtime.pages.get(itemId, "body");
    expect(page.formatVersion).toBe(3); expect(page.designParts).toBeUndefined(); expect(page.design.composition).toBeUndefined();
    expect(page.reconstruction.draft).toHaveLength(1);
    expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.chat).toHaveBeenCalledTimes(1);
  });

  it("纯排版保存独立产物，正文不变，也不调用生图", async () => {
    const original = runtime.items.get(itemId).content;
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    expect((await terminal(task.id)).state).toBe("completed");
    expect(runtime.pages.get(itemId, "body").source.content).toBe(original);
    expect(runtime.items.get(itemId).content).toBe(original);
    expect(mocks.generate).not.toHaveBeenCalled(); expect(mocks.plan).not.toHaveBeenCalled();
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 2, imageCalls: 0, imagesSaved: 0, searchCalls: 0, pagesRead: 0 });
    expect(runtime.pages.get(itemId, "body").imageModel).toBeUndefined();
  });

  it("勾选生图但设计漏图时补一个槽位，未采用的计划图片不生成也不进入清单", async () => {
    mocks.chat.mockImplementation(streamPage(false));
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("required"), asset("unused")] });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true, maxImages: 1 } });
    const done = await terminal(task.id), page = runtime.pages.get(itemId, "body");
    expect(done.state).toBe("completed"); expect(done.usage.imageCalls).toBe(1); expect(done.plannedImages).toBe(1);
    expect(page.assets.map(a => a.id)).toEqual(["required"]);
    expect(page.design.html).toContain('data-theme-asset="required"');
    expect(mocks.generate).toHaveBeenCalledTimes(1);
  });

  it("勾选生图但规划没有可用图片时明确失败，不发布无图的成功页", async () => {
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    const done = await terminal(task.id);
    expect(done.state).toBe("failed"); expect(done.error).toContain("没有有效配图方案");
    expect(runtime.pages.get(itemId, "body")).toBeNull(); expect(mocks.generate).not.toHaveBeenCalled();
  });

  it("生图失败显示部分完成，继续只补图不重新设计", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("required")] });
    mocks.generate.mockRejectedValueOnce(new Error("生图服务暂不可用"));
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("partial");
    expect(runtime.pages.getTask(task.id).error).toContain("生图服务暂不可用");
    const next = await runtime.resume(task.id);
    expect((await terminal(next.id)).state).toBe("completed");
    expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.chat).toHaveBeenCalledTimes(1);
    expect(mocks.plan).toHaveBeenCalledTimes(1); expect(mocks.generate).toHaveBeenCalledTimes(2);
  });

  it("部分图片失败仍发布全文，继续仅补失败图", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("g0"), asset("g1")] });
    mocks.generate.mockImplementationOnce(async (a, _d, _c, _s, checkpoint) => { Object.assign(a, { status: "ready", fileName: "g0.png", sha256: "a".repeat(64), bytes: 100 }); checkpoint(); }).mockRejectedValueOnce(new Error("上游超时"));
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("partial");
    expect(runtime.pages.getTask(task.id).plannedImages).toBe(2);
    const next = await runtime.resume(task.id);
    expect((await terminal(next.id)).state).toBe("completed");
    expect(runtime.pages.getTask(next.id).plannedImages).toBe(1);
    expect(mocks.plan).toHaveBeenCalledTimes(1); expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.chat).toHaveBeenCalledTimes(1);
    expect(mocks.generate.mock.calls.map(call => call[0].id)).toEqual(["g0", "g1", "g1"]);
  });

  it("勾选生图时不能把上限设为零", async () => {
    await expect(runtime.generate({itemId,sourceKind:"body",options:{...options,generateImages:true,maxImages:0}})).rejects.toThrow("至少一张");
  });

  it("取消后的迟到文本结果不能写入或发布", async () => {
    const resolve = pendingEditor();
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.call).toHaveBeenCalled());
    runtime.cancel(task.id); resolve(editorResult("迟到"));
    await vi.waitFor(() => expect(runtime.hasActive()).toBe(false));
    expect(runtime.pages.get(itemId, "body")).toBeNull();
    expect(runtime.pages.getTask(task.id).state).toBe("cancelled");
    expect(mocks.chat).not.toHaveBeenCalled();
  });

  it("执行层再次约束图片上限，超额槽位不会发起付费请求", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("g0"), asset("g1")] });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true, maxImages: 1 } });
    expect((await terminal(task.id)).state).toBe("partial");
    expect(mocks.generate).toHaveBeenCalledTimes(1);
    expect(runtime.pages.getTask(task.id).plannedImages).toBe(1);
  });

  it("同步换库撤销闲置runner后不再接收新任务", async () => {
    runtime.retireIdle();
    await expect(runtime.generate({ itemId, sourceKind: "body", options })).rejects.toThrow("数据服务正在切换");
    noModelCalls();
  });

  it("单图替换失败保留当前版本和原图", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("g0")] });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } }); await terminal(task.id);
    const current = runtime.pages.get(itemId, "body");
    mocks.generate.mockRejectedValueOnce(new Error("替换失败"));
    const next = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } }, "g0");
    expect(await terminal(next.id)).toMatchObject({ state: "failed", error: expect.stringContaining("指定图片未能替换") });
    expect(runtime.pages.get(itemId, "body").id).toBe(current.id);
    expect(runtime.pages.get(itemId, "body").assets[0].fileName).toBe("g0.png");
    expect(runtime.pages.getVersion(next.versionId)).toMatchObject({ formatVersion: 3, textModel: "text-test", imageModel: "image-test" });
  });

  it("已提交的生图请求取消后仍计一次，迟到图片不计成功；继续只累计新请求", async () => {
    mocks.plan.mockImplementationOnce(async (_v, _c, _s, onRequest) => { onRequest(); return { direction: "啤酒", assets: [asset("g0")] }; });
    let finish: () => void;
    mocks.generate.mockImplementationOnce(async (a, _d, _c, signal, checkpoint, _cleanup, onRequest) => {
      onRequest(); await new Promise<void>(resolve => { finish = resolve; }); signal.throwIfAborted();
      Object.assign(a, { status: "ready", fileName: "g0.png", sha256: "a".repeat(64), bytes: 100 }); checkpoint();
    });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    await vi.waitFor(() => expect(mocks.generate).toHaveBeenCalledTimes(1));
    runtime.cancel(task.id); finish();
    await vi.waitFor(() => expect(runtime.hasActive()).toBe(false));
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 3, imageCalls: 1, imagesSaved: 0, searchCalls: 0, pagesRead: 0 });
    await runtime.resume(task.id); await terminal(task.id);
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 3, imageCalls: 2, imagesSaved: 1, searchCalls: 0, pagesRead: 0 });
    expect(mocks.plan).toHaveBeenCalledTimes(1); expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.chat).toHaveBeenCalledTimes(1);
  });

  it("新图检查点事务失败后，失败状态不能把内存中的成功计数写回", async () => {
    mocks.plan.mockImplementationOnce(async (_v, _c, _s, onRequest) => { onRequest(); return { direction: "啤酒", assets: [asset("g0")] }; });
    db.exec("CREATE TRIGGER reject_saved_count BEFORE UPDATE ON themed_reading_tasks WHEN json_extract(NEW.payload, '$.usage.imagesSaved') > 0 BEGIN SELECT RAISE(ABORT, 'image checkpoint rejected'); END;");
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("partial");
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 3, imageCalls: 1, imagesSaved: 0, searchCalls: 0, pagesRead: 0 });
    expect(runtime.pages.get(itemId, "body").assets[0].status).toBe("failed");
  });

  it("缺少用量记录的任务继续时不伪造历史统计，复用图不计作新图保存", async () => {
    const version = v3Work(itemId, { assets: [asset("g0")], design: v3Design(["g0"]), options: { ...options, generateImages: true } });
    runtime.pages.saveVersion(version);
    runtime.pages.saveTask({ id: "no-usage", itemId, sourceKind: "body", versionId: version.id, title: "啤酒", state: "interrupted", stage: "images", completed: 0, total: 1, createdAt: 1, updatedAt: 1 });
    await runtime.resume("no-usage");
    expect((await terminal("no-usage")).state).toBe("completed");
    expect(runtime.pages.getTask("no-usage").usage).toBeUndefined();
    expect(mocks.generate).toHaveBeenCalledTimes(1);
    const previous = runtime.pages.get(itemId, "body");
    previous.imageModel = "previous-image-model"; runtime.pages.saveVersion(previous);
    const next = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } });
    await terminal(next.id);
    expect(runtime.pages.getTask(next.id).usage).toEqual({ textCalls: 2, imageCalls: 0, imagesSaved: 0, searchCalls: 0, pagesRead: 0 });
    expect(runtime.pages.get(itemId, "body").assets[0].fileName).toBe(previous.assets[0].fileName);
    expect(runtime.pages.get(itemId, "body").imageModel).toBe("previous-image-model");
  });

  it("停止后立即继续不会被旧请求的取消回执覆盖", async () => {
    const resolve = pendingEditor();
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.call).toHaveBeenCalledTimes(1));
    runtime.cancel(task.id);
    await runtime.resume(task.id);
    resolve(editorResult("旧请求"));
    expect((await terminal(task.id)).state).toBe("completed");
    expect(mocks.call).toHaveBeenCalledTimes(2);
    expect(runtime.pages.get(itemId, "body").designDirection).toBe("琥珀色手册");
  });

  it("正文编辑后仍关联旧快照，删除条目后的结果不发布", async () => {
    const resolve = pendingEditor();
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.call).toHaveBeenCalled());
    runtime.items.update(itemId, { content: "编辑后的正文" });
    expect(runtime.pages.getVersion(task.versionId).source.content).toContain("60–70");
    runtime.items.moveToTrash([itemId]); runtime.items.deleteForever([itemId]);
    resolve(editorResult("迟到"));
    await vi.waitFor(() => expect(runtime.hasActive()).toBe(false));
    expect(runtime.pages.get(itemId, "body")).toBeNull();
    expect(runtime.pages.getTask(task.id)).toBeNull();
  });

  it("联网失败不发布；明确改为不联网后复用规划检查点", async () => {
    mocks.search.mockRejectedValue(new Error("搜索认证失败"));
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, action: "create", enhancedInteraction: true, research: true } });
    expect(await terminal(task.id)).toMatchObject({ state: "failed", stage: "research" });
    expect(runtime.pages.get(itemId, "body")).toBeNull();
    expect(runtime.pages.getVersion(task.versionId).reconstruction.outline.title).toBe("啤酒");
    await runtime.continueOffline(task.id);
    expect((await terminal(task.id)).state).toBe("completed");
    expect(runtime.pages.get(itemId, "body")).toMatchObject({ formatVersion: 3, options: { research: false } });
    expect(mocks.search).toHaveBeenCalledTimes(1);
    expect(mocks.call.mock.calls.map(c => JSON.parse(c[1]).task.slice(0, 4))).toEqual(["规划专题", "撰写指定"]);
    expect(JSON.parse(mocks.call.mock.calls[1][1]).references).toEqual([]);
    expect(mocks.chat).toHaveBeenCalledTimes(1);
  });

  it("修订页单图重试复用正文与研究稿，不请求文本模型", async () => {
    const page = v3Work(itemId, { assets: [asset("g0")], design: v3Design(["g0"]), options: { ...options, action: "revise", enhancedInteraction: true } });
    runtime.pages.saveVersion(page); runtime.pages.publish(page.id);
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...page.options, fromCurrent: true } }, "g0");
    expect((await terminal(task.id)).state).toBe("completed");
    expect(runtime.pages.get(itemId, "body").reconstruction).toEqual(page.reconstruction);
    expect(mocks.call).not.toHaveBeenCalled(); expect(mocks.chat).not.toHaveBeenCalled(); expect(mocks.generate).toHaveBeenCalledOnce();
  });

  it.each([1, 2] as const)("v%s 旧页只读：单图重做、补素材与基于当前页的调整都被拒绝，不请求模型", async (format) => {
    const page = publishLegacy(format);
    const attempts = [
      () => runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } }, "g0"),
      () => runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } }, undefined, true),
      () => runtime.generate({ itemId, sourceKind: "body", options: { ...options, action: "refresh", fromCurrent: true } }, undefined, true),
      ...(["revise", "redesign"] as const).map(action => () => runtime.generate({ itemId, sourceKind: "body", options: { ...options, action, enhancedInteraction: true, fromCurrent: true } })),
      () => runtime.generate({ itemId, sourceKind: "body", options: { ...options, enhancedInteraction: true, fromCurrent: true } }),
    ];
    for (const attempt of attempts) await expect(attempt()).rejects.toThrow(LEGACY_PAGE_ERROR);
    expect(runtime.pages.listTasks()).toEqual([]);
    expect(runtime.pages.get(itemId, "body")).toEqual(page);
    expect(runtime.pages.get(itemId, "body", "working")).toBeNull();
    noModelCalls();
  });

  it("旧页可以按最新原文重新生成 v3，旧页保留为上一版", async () => {
    const legacy = publishLegacy(2);
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, action: "refresh", enhancedInteraction: true } });
    expect((await terminal(task.id)).state).toBe("completed");
    const page = runtime.pages.get(itemId, "body");
    expect(page.formatVersion).toBe(3);
    expect(page.source.content).toBe(runtime.items.get(itemId).content);
    expect(runtime.pages.get(itemId, "body", "previous")).toMatchObject({ id: legacy.id, formatVersion: 2 });
  });

  it.each([1, 2] as const)("v%s 未完成任务不能继续或改为不联网继续，不请求模型", async (format) => {
    const work = legacyWork(format);
    runtime.pages.saveVersion(work);
    for (const state of ["failed", "interrupted", "cancelled"] as const) runtime.pages.saveTask(taskFor(work, state, `legacy-${state}`));
    for (const state of ["failed", "interrupted", "cancelled"]) await expect(runtime.resume(`legacy-${state}`)).rejects.toThrow(LEGACY_TASK_ERROR);
    await expect(runtime.continueOffline("legacy-failed")).rejects.toThrow(LEGACY_TASK_ERROR);
    expect(runtime.pages.getVersion(work.id).options).toEqual(work.options);
    expect(runtime.pages.getTask("legacy-failed")).toMatchObject({ state: "failed", error: "关键资料不足" });
    noModelCalls();
  });

  it("已排队的旧格式任务在执行阶段直接失败，不请求模型", async () => {
    const items = new KnowledgeItemDB(db);
    const legacy = ([1, 2] as const).map(format => legacyWork(format, items.create({ title: `v${format}`, content: body }).id));
    legacy.forEach((version, index) => { runtime.pages.saveVersion(version); runtime.pages.saveTask(taskFor(version, "queued", `queued-${index}`)); });
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    expect((await terminal(task.id)).state).toBe("completed");
    for (const id of ["queued-0", "queued-1"]) expect(runtime.pages.getTask(id)).toMatchObject({ state: "failed", error: LEGACY_TASK_ERROR });
    expect(mocks.call).toHaveBeenCalledTimes(1); expect(mocks.chat).toHaveBeenCalledTimes(1);
    expect(legacy.map(version => runtime.pages.getVersion(version.id).formatVersion)).toEqual([1, 2]);
  });

  it("启动时把未完成的旧格式任务标记为失败并写明原因，不改动页面也不请求模型", async () => {
    const items = new KnowledgeItemDB(db);
    const [v1Item, v2Item, pageItem, v3Item] = ["v1", "v2", "旧页", "v3"].map(title => items.create({ title, content: body }).id);
    const v1 = legacyWork(1, v1Item), v2 = legacyWork(2, v2Item), v3 = v3Work(v3Item);
    for (const version of [v1, v2, v3]) runtime.pages.saveVersion(version);
    const page = publishLegacy(2, pageItem);
    for (const task of [
      taskFor(v1, "queued", "v1-queued"),
      taskFor(v2, "interrupted", "v2-interrupted"), taskFor(v2, "failed", "v2-failed"), taskFor(v2, "cancelled", "v2-cancelled"),
      { ...taskFor(page, "partial", "page-partial"), stage: "done" as const, completed: 1, error: "主题插画：上游超时" },
      taskFor(v3, "queued", "v3-queued"),
    ]) runtime.pages.saveTask(task);
    const restarted = new ThemedReadingRuntime(db, mocks.notify);
    for (const id of ["v1-queued", "v2-interrupted", "v2-failed", "v2-cancelled"]) expect(restarted.pages.getTask(id)).toMatchObject({ state: "failed", error: LEGACY_TASK_ERROR, legacyFormat: true });
    expect(restarted.pages.getTask("v2-failed").updatedAt).toBe(1);
    expect(restarted.pages.getTask("page-partial")).toMatchObject({ state: "partial", error: "主题插画：上游超时", legacyFormat: true });
    expect(restarted.pages.getTask("v3-queued")).toMatchObject({ state: "interrupted" });
    expect(restarted.pages.getTask("v3-queued").legacyFormat).toBeUndefined();
    expect(restarted.pages.get(pageItem, "body")).toEqual(page);
    expect(restarted.pages.getVersion(v2.id)).toMatchObject({ formatVersion: 2, role: "working" });
    await expect(restarted.resume("v1-queued")).rejects.toThrow(LEGACY_TASK_ERROR);
    await expect(restarted.resume("page-partial")).rejects.toThrow(LEGACY_PAGE_ERROR);
    noModelCalls();
    await restarted.shutdown();
  });
});
