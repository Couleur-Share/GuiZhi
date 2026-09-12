import { afterEach, beforeEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { MIGRATIONS } from "@guizhi/db/migrations";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ThemedReadingDB, listThemedReadingAssetFiles } from "@guizhi/db/themed-reading";
import { themedAsset, themedTask, themedVersion } from "./themed-reading-fixture";

let db: Database;
let pages: ThemedReadingDB;
let items: KnowledgeItemDB;
let itemId: string;
beforeEach(() => {
  db = new Database(":memory:");
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  pages = new ThemedReadingDB(db);
  items = new KnowledgeItemDB(db);
  itemId = items.create({title: "啤酒", content: "人工编辑的正文"}).id;
});
afterEach(() => db.close());

describe("主题阅读页版本与任务", () => {
  it("发布与完成状态在同一事务提交，状态写入失败保留当前版",()=>{
    const old=pages.saveVersion(themedVersion(itemId));pages.publish(old.id);
    const next=pages.saveVersion(themedVersion(itemId,{id:"next"}));
    const task=pages.saveTask(themedTask(next));
    db.exec("CREATE TRIGGER reject_reading_done BEFORE UPDATE ON themed_reading_tasks WHEN NEW.state='completed' BEGIN SELECT RAISE(ABORT, 'completion rejected'); END;");
    expect(()=>pages.publish(next.id,{...task,state:"completed",stage:"done"})).toThrow("completion rejected");
    expect(pages.get(itemId,"body").id).toBe(old.id);expect(pages.getVersion(next.id).role).toBe("working");expect(pages.getTask(task.id).state).toBe("running");
  });
  it("迁移可从旧库幂等创建，不修改原文与现有来源", () => {
    db.exec("DROP TABLE themed_reading_tasks; DROP TABLE themed_reading_assets; DROP TABLE themed_reading_versions;");
    const original = items.get(itemId);
    expect(listThemedReadingAssetFiles(db)).toEqual([]);
    const migration = MIGRATIONS.find(value => value.name === "0032-themed-reading")!;
    migration.up(db);
    migration.up(db);
    expect(items.get(itemId)).toEqual(original);
    expect(pages.listTasks()).toEqual([]);
    expect(pages.get(itemId, "body")).toBeNull();
  });

  it("发布只保留当前、上一版和工作版，淘汰版本的任务与资产引用级联删除", () => {
    const first = pages.saveVersion(themedVersion(itemId, {assets: [themedAsset("oldest.png")]}));
    pages.saveTask(themedTask(first, {state: "completed", stage: "done"}));
    pages.publish(first.id);
    const second = pages.saveVersion(themedVersion(itemId, {id: "second", assets: [themedAsset("previous.png")]}));
    pages.publish(second.id);
    expect(pages.get(itemId, "body", "previous")?.id).toBe(first.id);
    const third = pages.saveVersion(themedVersion(itemId, {id: "third", assets: [themedAsset("current.png")]}));
    pages.publish(third.id);
    expect(pages.get(itemId, "body")?.id).toBe(third.id);
    expect(pages.get(itemId, "body", "previous")?.id).toBe(second.id);
    expect(pages.getVersion(first.id)).toBeNull();
    expect(pages.getTask(`task-${first.id}`)).toBeNull();
    expect(pages.listAssetFiles()).toEqual(["current.png", "previous.png"]);
    expect(pages.get(itemId, "body", "working")).toBeNull();
    expect(items.get(itemId)?.content).toBe("人工编辑的正文");
  });

  it("无设计、取消以及删除后的迟到发布不覆盖当前可读页", () => {
    pages.saveVersion(themedVersion(itemId));
    pages.publish("theme-version-1");
    const working = pages.saveVersion(themedVersion(itemId, {id: "next", design: null}));
    expect(() => pages.publish(working.id)).toThrow("完成设计");
    pages.saveVersion({...working, design: themedVersion(itemId).design});
    pages.saveTask(themedTask(working, {state: "cancelled"}));
    expect(() => pages.publish(working.id)).toThrow("任务已停止");
    expect(pages.get(itemId, "body")?.id).toBe("theme-version-1");
    expect(pages.get(itemId, "body", "previous")).toBeNull();
    items.moveToTrash([itemId]);
    expect(() => pages.publish(working.id)).toThrow("回收站");
    expect(pages.get(itemId, "body")?.id).toBe("theme-version-1");
  });

  it("发布引用失败整体回滚，原工作版本与资源保留", () => {
    const version = pages.saveVersion(themedVersion(itemId, {assets: [themedAsset("kept.png")]}));
    const duplicate = themedAsset("same.png");
    expect(() => pages.saveVersion({...version, assets: [duplicate, {...duplicate, id: "other", sha256: "f".repeat(64)}]})).toThrow("同名资源");
    expect(pages.listAssetFiles()).toEqual(["kept.png"]);
    expect(pages.getVersion(version.id)?.assets[0].fileName).toBe("kept.png");
  });

  it("新图引用与保存计数原子提交，任务写入失败时两者同时回滚", () => {
    const version = pages.saveVersion(themedVersion(itemId));
    const task = pages.saveTask(themedTask(version, {usage: {textCalls: 2, imageCalls: 1, imagesSaved: 0}}));
    const ready = {...version, assets: [themedAsset("generated.png")]};
    const counted = {...task, usage: {...task.usage, imagesSaved: 1}};
    db.exec("CREATE TRIGGER reject_theme_usage BEFORE UPDATE ON themed_reading_tasks BEGIN SELECT RAISE(ABORT, 'usage checkpoint rejected'); END;");
    expect(() => pages.saveVersion(ready, counted)).toThrow("usage checkpoint rejected");
    expect(pages.getVersion(version.id).assets).toEqual(version.assets);
    expect(pages.listAssetFiles()).toEqual([]);
    expect(pages.getTask(task.id).usage.imagesSaved).toBe(0);
    db.exec("DROP TRIGGER reject_theme_usage;");
    pages.saveVersion(ready, counted);
    expect(pages.listAssetFiles()).toEqual(["generated.png"]);
    expect(pages.getTask(task.id).usage.imagesSaved).toBe(1);
  });

  it("旧任务没有统计仍可读取，新统计拒绝负数和不完整值", () => {
    const version = pages.saveVersion(themedVersion(itemId));
    const task = pages.saveTask(themedTask(version));
    expect(pages.getTask(task.id).usage).toBeUndefined();
    expect(() => pages.saveTask({...task, usage: {textCalls: -1, imageCalls: 0, imagesSaved: 0}})).toThrow("任务格式");
    expect(() => pages.saveTask({...task, usage: {textCalls: 1} as typeof task.usage})).toThrow("任务格式");
  });

  it("上一版交换不破坏任务外键，正文与总结版本相互隔离", () => {
    const first = pages.saveVersion(themedVersion(itemId));
    const task = pages.saveTask(themedTask(first, {state: "completed"}));
    pages.publish(first.id);
    pages.saveVersion(themedVersion(itemId, {id: "second"}));
    pages.publish("second");
    pages.saveVersion(themedVersion(itemId, {id: "summary", sourceKind: "summary"}));
    pages.publish("summary");
    expect(pages.restorePrevious(itemId, "body")?.id).toBe(first.id);
    expect(pages.get(itemId, "body", "previous")?.id).toBe("second");
    expect(pages.get(itemId, "summary")?.id).toBe("summary");
    expect(pages.getTask(task.id)?.versionId).toBe(first.id);
    expect(db.pragma("foreign_key_check")).toEqual([]);
    expect(db.get("SELECT count(*) AS count FROM themed_reading_versions WHERE role IS NULL")).toEqual({count: 0});
    expect(pages.restorePrevious(itemId, "body")?.id).toBe("second");
  });

  it("活动任务不能被替换或移除；中断后保留每张图片检查点", () => {
    const pending = {...themedAsset("original.png"), role: "original" as const, status: "pending" as const, sha256: undefined, bytes: undefined};
    const version = pages.saveVersion(themedVersion(itemId, {assets: [pending, themedAsset("generated.png")]}));
    const task = pages.saveTask(themedTask(version, {completed: 1}));
    expect(pages.hasActiveTasks()).toBe(true);
    expect(() => pages.saveVersion(themedVersion(itemId, {id: "replacement"}))).toThrow("任务仍在执行");
    expect(() => pages.remove(itemId, "body")).toThrow("任务仍在执行");
    const interrupted = pages.interruptRunning();
    expect(interrupted[0]).toMatchObject({id: task.id, state: "interrupted", completed: 1});
    expect(pages.hasActiveTasks()).toBe(false);
    expect(pages.listAssetFiles()).toEqual(["generated.png", "original.png"]);
    expect(pages.getVersion(version.id)?.assets[0].status).toBe("pending");
    pages.saveTask({...interrupted[0], state: "running"});
    expect(pages.hasActiveTasks()).toBe(true);
    expect(pages.getVersion(version.id)?.assets[1].fileName).toBe("generated.png");
  });

  it("清除页面只影响所选来源，返回该来源全部版本候选资源", () => {
    pages.saveVersion(themedVersion(itemId, {assets: [themedAsset("current.png")]}));
    pages.publish("theme-version-1");
    pages.saveVersion(themedVersion(itemId, {id: "working", assets: [themedAsset("working.png")]}));
    pages.saveTask(themedTask(pages.getVersion("working")!, {state: "interrupted"}));
    pages.saveVersion(themedVersion(itemId, {id: "summary", sourceKind: "summary", assets: [themedAsset("summary.png")]}));
    expect(pages.remove(itemId, "body").sort()).toEqual(["current.png", "working.png"]);
    expect(pages.get(itemId, "body")).toBeNull();
    expect(pages.listTasks()).toEqual([]);
    expect(pages.get(itemId, "summary", "working")?.id).toBe("summary");
    expect(pages.listAssetFiles()).toEqual(["summary.png"]);
  });

  it("永久删除自动级联，迟到的任务保存不能重建数据", () => {
    const version = pages.saveVersion(themedVersion(itemId, {assets: [themedAsset()]}));
    const task = pages.saveTask(themedTask(version));
    expect(items.listAssetRefs([itemId])).toEqual(["theme-test.png"]);
    items.deleteForever([itemId]);
    expect(pages.getVersion(version.id)).toBeNull();
    expect(pages.getTask(task.id)).toBeNull();
    expect(pages.listAssetFiles()).toEqual([]);
    expect(() => pages.saveVersion(version)).toThrow("条目不存在");
    expect(() => pages.saveTask({...task, state: "completed"})).toThrow();
  });

  it("资源路径或元数据无效时拒绝持久化，支持最多五张配图", () => {
    expect(() => pages.saveVersion(themedVersion(itemId, {assets: [themedAsset("../escape.png")]}))).toThrow("资源清单");
    expect(() => pages.saveVersion(themedVersion(itemId, {assets: [{...themedAsset(), sha256: undefined}]}))).toThrow("资源清单");
    const version = themedVersion(itemId);
    expect(pages.saveVersion({...version, options: {...version.options, maxImages: 5}}).options.maxImages).toBe(5);
    expect(() => pages.saveVersion({...version, options: {...version.options, maxImages: 6}})).toThrow("生成选项");
  });

  it("任务不能跨条目关联版本，同一内容来源最多一个活动任务", () => {
    const version = pages.saveVersion(themedVersion(itemId));
    const otherId = items.create({title: "另一条目"}).id;
    expect(() => pages.saveTask(themedTask(version, {itemId: otherId}))).toThrow();
    pages.saveTask(themedTask(version));
    expect(() => pages.saveTask(themedTask(version, {id: "other-task"}))).toThrow();
    expect(pages.listTasks()).toHaveLength(1);
  });
});
