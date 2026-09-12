import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db";
import type { ThemedReadingAsset, ThemedReadingTask } from "@guizhi/shared/types";

const mocks = vi.hoisted(() => ({ call: vi.fn(), chat:vi.fn(), search: vi.fn(), capture: vi.fn(), plan: vi.fn(), chapter: vi.fn(), generate: vi.fn(), original: vi.fn(), notify: vi.fn(), cleanup: vi.fn() }));
vi.mock("@guizhi/core",async(importOriginal)=>({...await importOriginal<typeof import("@guizhi/core")>(),chatCompletion:(...args:unknown[])=>mocks.chat(...args)}));
vi.mock("../../src/main/services/media/media-summary", () => ({ resolveMediaSummaryConfig: () => ({ model: "text-test" }) }));
vi.mock("../../src/main/services/illustration/image-gen", () => ({ resolveImageGenConfig: () => ({ model: "image-test" }) }));
vi.mock("../../src/main/services/asset-cleanup", () => ({ cleanupOrphanAssets: mocks.cleanup, retainAssetFiles: () => () => undefined }));
vi.mock("../../src/main/diagnostic-log", () => ({ logAppError: vi.fn() }));
vi.mock("../../src/main/services/themed-reading/design", async (importOriginal) => ({
  ...await importOriginal<typeof import("../../src/main/services/themed-reading/design")>(),
  callDesignModel: (...args: unknown[]) => mocks.call(...args),
  chunkDesignBlocks: (blocks: unknown[]) => [blocks], planTheme: mocks.plan, designChapter: mocks.chapter,
}));
vi.mock("../../src/main/services/themed-reading/assets", () => ({ collectOriginalThemeAssets: () => [], generateThemeAsset: mocks.generate, prepareOriginalAsset: mocks.original }));
vi.mock("../../src/main/services/themed-reading/search-service", () => ({ searchReadingWeb: mocks.search }));
vi.mock("../../src/main/services/web-capture/web-capture", () => ({ captureWebPage: mocks.capture }));
import { ThemedReadingRuntime } from "../../src/main/services/themed-reading/runtime";
import { sampleComposition } from "./themed-reading-composition-fixture";
import { reconstructionFixture } from "./reading-reconstruction-fixture";

const options = { style: "暖色手册", generateImages: false, maxImages: 3 };
const asset = (id: string): ThemedReadingAsset => ({ id, role: "generated", purpose: "主题插画", prompt: "麦穗", alt: id, aspectRatio: "16:9", status: "pending", blockId: "b0" });

describe("主题排版主进程持久任务", () => {
  let db: Database, runtime: ThemedReadingRuntime, itemId: string;
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.chat.mockImplementation(async(_c,_m,options)=>{
      for(const record of [{type:"meta",css:""},{type:"section",id:"article",html:`<h1>恢复后的阅读页</h1><h2>完整正文</h2><p>${"正文保留与条件说明，模型脚本仍保持禁用。".repeat(8)}</p>`},{type:"done"}])options.onDelta(JSON.stringify(record)+'\n');
      return {content:"",finishReason:"stop"};
    });
    db = new Database(":memory:"); db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
    itemId = new KnowledgeItemDB(db).create({ title: "啤酒", content: "# 原文标题\n\n保留 60–70 度、限定条件与全部原文。" }).id;
    runtime = new ThemedReadingRuntime(db, mocks.notify);
    mocks.plan.mockImplementation(async (_v, _c, _s, onRequest) => { onRequest?.(); return { direction: "琥珀色手册", assets: [] }; });
    mocks.chapter.mockImplementation(async (version, blocks, _i, _c, _s, onRequest) => { onRequest?.(); return { direction: "琥珀色手册", html: blocks.map((b: { id: string }) => `<div data-source-block="${b.id}"></div>`).join("") + version.assets.map((a: ThemedReadingAsset) => `<img data-theme-asset="${a.id}">`).join(""), css: "", assets: [] }; });
    mocks.generate.mockImplementation(async (a, _direction, _config, signal, checkpoint, _cleanup, onRequest) => {
      signal.throwIfAborted(); onRequest?.(); Object.assign(a, { status: "ready", fileName: `${a.id}.png`, sha256: "a".repeat(64), bytes: 100, error: undefined }); checkpoint();
    });
  });
  afterEach(async () => { await runtime.shutdown(); db.close(); });
  const terminal = async (id: string) => {
    await vi.waitFor(() => expect(["completed", "partial", "failed", "cancelled", "interrupted"]).toContain(runtime.pages.getTask(id)?.state));
    return runtime.pages.getTask(id)!;
  };

  const prepareV2 = () => {
    mocks.call.mockImplementation(async (_config, prompt, _signal, _chat, onRequest) => {
      onRequest?.(); const input = JSON.parse(prompt);
      if (input.task.startsWith("完整理解")) return { notes: "理解后的信息与问题" };
      if (input.task.startsWith("规划全篇")) return { title: "新专题", direction: "蓝色", questions: ["啤酒分类"], sections: [{ title: "新章节", brief: "解释与例子" }] };
      if (input.task.startsWith("撰写该章节")) return { title: "新章节", markdown: "新文章内容", referenceIds: [] };
      if (input.task.startsWith("将编辑稿")) return { html: `<main><h1>新专题</h1><h2>新章节</h2><p>${"这是提炼扩写的新文章，原始正文仍独立保留。".repeat(8)}</p></main>`, css: "main{max-width:960px;margin:auto}", interactions: [] };
      throw new Error("未预期的模型阶段");
    });
  };
  it("v2 勾选生图但设计漏图时补一个槽位，生图成功才完成", async () => {
    prepareV2(); mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("required"),asset("unused")] });
    const task = await runtime.generate({itemId,sourceKind:"body",options:{...options,action:"create",generateImages:true,maxImages:1}});
    const done = await terminal(task.id), page = runtime.pages.get(itemId,"body");
    expect(done.state).toBe("completed"); expect(done.usage.imageCalls).toBe(1); expect(done.plannedImages).toBe(1);
    expect(page.assets.map(a => a.id)).toEqual(["required"]);
    expect(page.design.html).toContain('data-theme-asset="required"');
    expect(mocks.generate).toHaveBeenCalledTimes(1); expect(mocks.call).toHaveBeenCalledTimes(4);
  });
  it("v2 空配图方案明确失败，不发布没有图片的成功页", async () => {
    prepareV2();
    const task = await runtime.generate({itemId,sourceKind:"body",options:{...options,action:"create",generateImages:true}});
    const done = await terminal(task.id);
    expect(done.state).toBe("failed"); expect(done.error).toContain("没有有效配图方案");
    expect(runtime.pages.get(itemId,"body")).toBeNull(); expect(mocks.generate).not.toHaveBeenCalled();
  });
  it("v2 生图失败显示部分完成，继续只补图不重新设计", async () => {
    prepareV2(); mocks.plan.mockResolvedValue({direction:"啤酒",assets:[asset("required")]});
    mocks.generate.mockRejectedValueOnce(new Error("生图服务暂不可用"));
    const task = await runtime.generate({itemId,sourceKind:"body",options:{...options,action:"create",generateImages:true}});
    expect((await terminal(task.id)).state).toBe("partial");
    expect(runtime.pages.getTask(task.id).error).toContain("生图服务暂不可用");
    const next = await runtime.resume(task.id);
    expect((await terminal(next.id)).state).toBe("completed"); expect(mocks.call).toHaveBeenCalledTimes(4);
    expect(mocks.plan).toHaveBeenCalledTimes(1); expect(mocks.generate).toHaveBeenCalledTimes(2);
  });
  it("勾选生图时不能把上限设为零", async () => {
    await expect(runtime.generate({itemId,sourceKind:"body",options:{...options,generateImages:true,maxImages:0}})).rejects.toThrow("至少一张");
  });

  it("v2 完整生成原子发布自由 HTML，不覆盖 Markdown", async () => {
    prepareV2(); const before = runtime.items.get(itemId).content;
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, action: "create" } });
    const done = await terminal(task.id); expect(done.error).toBeUndefined(); expect(done.state).toBe("completed");
    const page = runtime.pages.get(itemId,"body"); expect(page.options.research).toBe(false); expect(mocks.search).not.toHaveBeenCalled(); expect(page.formatVersion).toBe(2); expect(page.reconstruction.draft).toHaveLength(1);
    expect(page.design.html).toContain("新专题"); expect(runtime.items.get(itemId).content).toBe(before);
    expect(done.usage).toEqual({textCalls:4,imageCalls:0,imagesSaved:0,searchCalls:0,pagesRead:0});
    expect(mocks.plan).not.toHaveBeenCalled(); expect(mocks.chapter).not.toHaveBeenCalled();
  });
  it("v2 联网失败不发布；明确转为离线后复用理解检查点", async () => {
    prepareV2(); mocks.search.mockRejectedValue(new Error("搜索认证失败"));
    const task = await runtime.generate({itemId,sourceKind:"body",options:{...options,action:"create",research:true}});
    expect((await terminal(task.id)).stage).toBe("research"); expect(runtime.pages.get(itemId,"body")).toBeNull();
    expect(runtime.pages.getVersion(task.versionId).reconstruction.notes).toHaveLength(1);
    await runtime.continueOffline(task.id); expect((await terminal(task.id)).state).toBe("completed");
    expect(mocks.search).toHaveBeenCalledTimes(1); expect(runtime.pages.get(itemId,"body").options.research).toBe(false);
    expect(mocks.call).toHaveBeenCalledTimes(3);expect(mocks.chat).toHaveBeenCalledTimes(1);
    expect(runtime.pages.get(itemId,"body")).toMatchObject({formatVersion:3,options:{enhancedInteraction:false}});
  });
  it("旧资料不足任务真正补查后发布，复用原有理解和搜索检查点", async () => {
    const page = reconstructionFixture(), design = page.design;
    page.itemId = itemId; page.role = "working"; page.design = null; page.options.research = true;
    page.reconstruction.draft = []; page.reconstruction.interactions = [];
    page.reconstruction.queries = [{ query: "旧查询", done: true, results: [{ title: "旧资料", url: "https://example.org/old", text: "旧资料简介".repeat(100) }] }];
    page.reconstruction.selectedUrls = ["https://example.org/old"];
    page.reconstruction.references = [{ id: "R1", title: "旧资料", url: "https://example.org/old", capturedAt: 1, text: "旧资料简介".repeat(100), status: "ready" }];
    runtime.pages.saveVersion(page);
    runtime.pages.saveTask({ id: "legacy-research", itemId, sourceKind: "body", versionId: page.id, title: "啤酒", state: "failed", stage: "research", completed: 0, total: 1, error: "关键资料不足", createdAt: 1, updatedAt: 1 });
    mocks.search.mockResolvedValue([{ title: "标准正文", url: "https://example.org/new" }]);
    mocks.capture.mockResolvedValue({ complete: true, markdown: "完整定义和条件".repeat(100) });
    let reviews = 0;
    mocks.call.mockImplementation(async (_config, prompt, _signal, _chat, onRequest) => {
      onRequest?.(); const input = JSON.parse(prompt);
      if (input.task.startsWith("判断现有")) return ++reviews === 1 ? { adequate: false, missing: "缺少标准正文", followUpQueries: ["啤酒标准适用条件"] } : { adequate: true };
      if (input.task.startsWith("从候选")) return { urls: ["https://example.org/new"] };
      if (input.task.startsWith("撰写该章节")) return { title: "新的结构", markdown: "新的正文", referenceIds: ["R2"] };
      if (input.task.startsWith("将编辑稿")) return { ...design, interactions: reconstructionFixture().reconstruction.interactions };
      throw new Error("不应重复原文理解和大纲");
    });
    await runtime.resume("legacy-research");
    expect((await terminal("legacy-research")).state).toBe("completed");
    expect(mocks.search).toHaveBeenCalledTimes(1);
    expect(mocks.search.mock.calls[0][0]).toBe("啤酒标准适用条件");
    const published = runtime.pages.get(itemId, "body");
    expect(published.reconstruction.notes).toEqual(page.reconstruction.notes);
    expect(published.reconstruction.researchComplete).toBe(true);
    expect(published.reconstruction.references.map(r => r.id)).toEqual(["R1", "R2"]);
  });

  it("明确改为按原文生成时，失败查证的外部资料不会进入新撰稿请求", async () => {
    const page = reconstructionFixture(); page.itemId = itemId; page.role = "working"; page.design = null;
    page.options.research = true; page.reconstruction.draft = []; page.reconstruction.interactions = [];
    page.reconstruction.references = [{ id: "R1", title: "尚未查证的资料", url: "https://example.org/failed", capturedAt: 1, text: "可能错误的信息", status: "ready" }];
    runtime.pages.saveVersion(page);
    runtime.pages.saveTask({ id: "offline-research", itemId, sourceKind: "body", versionId: page.id, title: "啤酒", state: "failed", stage: "research", completed: 0, total: 0, createdAt: 1, updatedAt: 1 });
    prepareV2();
    await runtime.continueOffline("offline-research");
    expect((await terminal("offline-research")).state).toBe("completed");
    const writeRequest = mocks.call.mock.calls.map(c => JSON.parse(c[1])).find(c => c.task.startsWith("撰写该章节"));
    expect(writeRequest).toMatchObject({ references: [], online: false });
    expect(mocks.search).not.toHaveBeenCalled(); expect(mocks.capture).not.toHaveBeenCalled();
  });
  it("v2 修订页单图重试复用正文和交互，不清空研究稿",async()=>{
    const page=reconstructionFixture();page.itemId=itemId;page.role="working";page.options.action="revise";
    page.assets=[asset("g0")];page.design.html+='<img data-theme-asset="g0">';
    runtime.pages.saveVersion(page);runtime.pages.publish(page.id);
    const task=await runtime.generate({itemId,sourceKind:"body",options:{...page.options,fromCurrent:true}},"g0");
    expect((await terminal(task.id)).state).toBe("completed");
    expect(runtime.pages.get(itemId,"body").reconstruction).toEqual(page.reconstruction);
    expect(mocks.call).not.toHaveBeenCalled();expect(mocks.generate).toHaveBeenCalledOnce();
  });

  it("纯排版保存独立产物，正文不变，也不调用生图", async () => {
    const original = runtime.items.get(itemId).content;
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    expect((await terminal(task.id)).state).toBe("completed");
    expect(runtime.pages.get(itemId, "body").source.content).toBe(original);
    expect(runtime.items.get(itemId).content).toBe(original);
    expect(mocks.generate).not.toHaveBeenCalled();
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 2, imageCalls: 0, imagesSaved: 0 });
    expect(runtime.pages.get(itemId, "body").imageModel).toBeUndefined();
  });

  it("专题结构进入真实保存与发布路径，只处理实际使用的素材", async () => {
    mocks.plan.mockResolvedValue({ direction: "专题", assets: [asset("used"), asset("unused")] });
    mocks.chapter.mockImplementation(async (version) => {
      const composition = sampleComposition(version.source); composition.chapters[0].imageId = "used";
      return { direction: "专题", html: "", css: "", assets: [], composition };
    });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("completed");
    const page = runtime.pages.get(itemId, "body");
    expect(page.design.composition.chapters[0].sections).toHaveLength(3);
    expect(page.assets.map(asset => asset.id)).toEqual(["used"]);
    expect(mocks.generate).toHaveBeenCalledTimes(1);
    expect(runtime.items.get(itemId).content).toBe(page.source.content);
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
    expect(mocks.plan).toHaveBeenCalledTimes(1);
    expect(mocks.chapter).toHaveBeenCalledTimes(1);
    expect(mocks.generate.mock.calls.map(call => call[0].id)).toEqual(["g0", "g1", "g1"]);
  });

  it("取消后的迟到文本结果不能写入或发布", async () => {
    let resolve: (value: unknown) => void;
    mocks.plan.mockImplementation(() => new Promise(done => { resolve = done; }));
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.plan).toHaveBeenCalled());
    runtime.cancel(task.id); resolve({ direction: "迟到", assets: [] });
    await vi.waitFor(() => expect(runtime.hasActive()).toBe(false));
    expect(runtime.pages.get(itemId, "body")).toBeNull();
    expect(runtime.pages.getTask(task.id).state).toBe("cancelled");
    expect(mocks.chapter).not.toHaveBeenCalled();
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
    expect(mocks.plan).not.toHaveBeenCalled();
  });

  it("设计未使用的计划图片不生成也不进入导出清单", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("unused")] });
    mocks.chapter.mockImplementationOnce(async (_v, blocks) => ({ direction: "啤酒", html: blocks.map((b: { id: string }) => `<div data-source-block="${b.id}"></div>`).join(""), css: "", assets: [] }));
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("completed");
    expect(mocks.generate).not.toHaveBeenCalled();
    expect(runtime.pages.get(itemId, "body").assets).toHaveLength(0);
    expect(runtime.pages.getTask(task.id).plannedImages).toBe(0);
  });

  it("单图替换失败保留当前版本和原图", async () => {
    mocks.plan.mockResolvedValue({ direction: "啤酒", assets: [asset("g0")] });
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } }); await terminal(task.id);
    const current = runtime.pages.get(itemId, "body");
    mocks.generate.mockRejectedValueOnce(new Error("替换失败"));
    const next = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } }, "g0");
    expect((await terminal(next.id)).state).toBe("failed");
    expect(runtime.pages.get(itemId, "body").id).toBe(current.id);
    expect(runtime.pages.get(itemId, "body").assets[0].fileName).toBe("g0.png");
    expect(runtime.pages.getVersion(next.versionId)).toMatchObject({ textModel: "text-test", imageModel: "image-test" });
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
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 2, imageCalls: 1, imagesSaved: 0 });
    await runtime.resume(task.id); await terminal(task.id);
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 2, imageCalls: 2, imagesSaved: 1 });
    expect(mocks.plan).toHaveBeenCalledTimes(1);
    expect(mocks.chapter).toHaveBeenCalledTimes(1);
  });

  it("新图检查点事务失败后，失败状态不能把内存中的成功计数写回", async () => {
    mocks.plan.mockImplementationOnce(async (_v, _c, _s, onRequest) => { onRequest(); return { direction: "啤酒", assets: [asset("g0")] }; });
    db.exec("CREATE TRIGGER reject_saved_count BEFORE UPDATE ON themed_reading_tasks WHEN json_extract(NEW.payload, '$.usage.imagesSaved') > 0 BEGIN SELECT RAISE(ABORT, 'image checkpoint rejected'); END;");
    const task = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, generateImages: true } });
    expect((await terminal(task.id)).state).toBe("partial");
    expect(runtime.pages.getTask(task.id).usage).toEqual({ textCalls: 2, imageCalls: 1, imagesSaved: 0 });
    expect(runtime.pages.get(itemId, "body").assets[0].status).toBe("failed");
  });

  it("旧任务继续不伪造历史统计，复用图不计作新图保存", async () => {
    const version = await import("./db/themed-reading-fixture").then(m => m.themedVersion(itemId));
    version.assets = [asset("g0")]; version.options.generateImages = true;
    runtime.pages.saveVersion(version);
    runtime.pages.saveTask({ id: "legacy", itemId, sourceKind: "body", versionId: version.id, title: "啤酒", state: "interrupted", stage: "images", completed: 0, total: 1, createdAt: 1, updatedAt: 1 });
    await runtime.resume("legacy"); await terminal("legacy");
    expect(runtime.pages.getTask("legacy").usage).toBeUndefined();
    expect(mocks.generate).toHaveBeenCalledTimes(1);
    const previous = runtime.pages.get(itemId, "body");
    previous.imageModel = "previous-image-model"; runtime.pages.saveVersion(previous);
    const next = await runtime.generate({ itemId, sourceKind: "body", options: { ...options, fromCurrent: true } });
    await terminal(next.id);
    expect(runtime.pages.getTask(next.id).usage).toEqual({ textCalls: 2, imageCalls: 0, imagesSaved: 0 });
    expect(runtime.pages.get(itemId, "body").assets[0].fileName).toBe(previous.assets[0].fileName);
    expect(runtime.pages.get(itemId, "body").imageModel).toBe("previous-image-model");
  });

  it("停止后立即继续不会被旧请求的取消回执覆盖", async () => {
    let resolve: (value: unknown) => void;
    mocks.plan.mockImplementationOnce(() => new Promise(done => { resolve = done; }));
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.plan).toHaveBeenCalledTimes(1));
    runtime.cancel(task.id);
    await runtime.resume(task.id);
    resolve({ direction: "旧请求", assets: [] });
    expect((await terminal(task.id)).state).toBe("completed");
    expect(mocks.plan).toHaveBeenCalledTimes(2);
    expect(runtime.pages.get(itemId, "body").designDirection).toBe("琥珀色手册");
  });

  it("正文编辑后仍关联旧快照，删除条目后的结果不发布", async () => {
    let resolve: (value: unknown) => void;
    mocks.plan.mockImplementation(() => new Promise(done => { resolve = done; }));
    const task = await runtime.generate({ itemId, sourceKind: "body", options });
    await vi.waitFor(() => expect(mocks.plan).toHaveBeenCalled());
    runtime.items.update(itemId, { content: "编辑后的正文" });
    expect(runtime.pages.getVersion(task.versionId).source.content).toContain("60–70");
    runtime.items.moveToTrash([itemId]); runtime.items.deleteForever([itemId]);
    resolve({ direction: "迟到", assets: [] });
    await vi.waitFor(() => expect(runtime.hasActive()).toBe(false));
    expect(runtime.pages.get(itemId, "body")).toBeNull();
    expect(runtime.pages.getTask(task.id)).toBeNull();
  });

  it("重启将排队任务置为中断，不自动请求模型", async () => {
    const version = await import("./db/themed-reading-fixture").then(m => m.themedVersion(itemId));
    runtime.pages.saveVersion(version);
    const task: ThemedReadingTask = { id: "restart", itemId, sourceKind: "body", versionId: version.id, title: "啤酒", state: "queued", stage: "images", completed: 0, total: 1, createdAt: 1, updatedAt: 1 };
    runtime.pages.saveTask(task);
    const restarted = new ThemedReadingRuntime(db, mocks.notify);
    expect(restarted.pages.getTask(task.id).state).toBe("interrupted");
    expect(mocks.generate).not.toHaveBeenCalled(); expect(mocks.plan).not.toHaveBeenCalled();
    await restarted.shutdown();
  });
});
