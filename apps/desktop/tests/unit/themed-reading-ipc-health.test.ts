import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ThemedReadingDB } from "@guizhi/db/themed-reading";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { configureRuntimePaths, getImagesDir, resetRuntimePaths } from "../../src/main/runtime-paths";
import { registerThemedReadingIPC } from "../../src/main/ipc/themed-reading.ipc";
import { resetThemedReadingImageHealthCache } from "../../src/main/services/themed-reading/image-health";
import { themedAsset, themedVersion } from "./db/themed-reading-fixture";

const mocks = vi.hoisted(() => ({handlers: new Map<string, (...args: any[]) => Promise<any>>() }));
vi.mock("electron", () => ({
  safeStorage: { isEncryptionAvailable: () => false },
  BrowserWindow: {getAllWindows: () => [], fromWebContents: () => null}, dialog: {},
  ipcMain: {handle: (channel: string, fn: (...args: any[]) => Promise<any>) => mocks.handlers.set(channel, fn)},
}));
vi.mock("../../src/main/services/media/media-summary", () => ({resolveMediaSummaryConfig: () => null}));
vi.mock("../../src/main/services/illustration/image-gen", () => ({resolveImageGenConfig: () => null, generateImage: vi.fn()}));
vi.mock("../../src/main/diagnostic-log", () => ({logAppError: vi.fn()}));

let root: string;
let db: Database;
let pages: ThemedReadingDB;
let items: KnowledgeItemDB;
let itemId: string;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-theme-ipc-health-"));
  configureRuntimePaths({userDataPath: root});
  fs.mkdirSync(getImagesDir(), {recursive: true});
  resetThemedReadingImageHealthCache();
  db = new Database(":memory:");
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES);
  items = new KnowledgeItemDB(db); pages = new ThemedReadingDB(db);
  itemId = items.create({title: "啤酒", content: "保持原文全部内容"}).id;
  registerThemedReadingIPC(db);
});
afterEach(() => {
  db.close();
  mocks.handlers.clear();
  resetThemedReadingImageHealthCache(); resetRuntimePaths();
  fs.rmSync(root, {recursive: true, force: true});
});

function call(channel: string) { return mocks.handlers.get(channel)!({}, {itemId, sourceKind: "body", instanceId: "health-test"}); }

describe("主题页 IPC 读取与恢复保护", () => {
  it("GET 坏图降级为可读页面，不改写数据库中的成功资源", async () => {
    const asset = themedAsset("missing.png");
    const version = themedVersion(itemId, {assets: [asset]});
    version.design!.html = `<main><img data-theme-asset="${asset.id}"><div data-source-block="b0"></div></main>`;
    pages.saveVersion(version); pages.publish(version.id);
    const result = await call(IPC_CHANNELS.THEMED_READING_GET);
    expect(result.success).toBe(true);
    expect(result.page.assets[0].status).toBe("failed");
    expect(result.page.warnings[0]).toContain("已丢失");
    expect(result.document).toContain("保持原文全部内容");
    expect(result.document).not.toContain("local-image://missing.png");
    expect(pages.get(itemId, "body")?.assets[0].status).toBe("ready");
  });

  it("缺图的上一版不能替换现有可读页", async () => {
    pages.saveVersion(themedVersion(itemId, {id: "previous", assets: [themedAsset("lost.png")]}));
    pages.publish("previous");
    pages.saveVersion(themedVersion(itemId, {id: "current"})); pages.publish("current");
    const result = await call(IPC_CHANNELS.THEMED_READING_RESTORE_PREVIOUS);
    expect(result.success).toBe(false);
    expect(result.error).toContain("上一版图片不完整");
    expect(pages.get(itemId, "body")?.id).toBe("current");
    expect(pages.get(itemId, "body", "previous")?.id).toBe("previous");
  });

  it("回收站可读取主题页，拒绝移除主题版本", async () => {
    pages.saveVersion(themedVersion(itemId)); pages.publish("theme-version-1");
    items.moveToTrash([itemId]);
    expect((await call(IPC_CHANNELS.THEMED_READING_GET)).success).toBe(true);
    const removed = await call(IPC_CHANNELS.THEMED_READING_REMOVE);
    expect(removed.success).toBe(false);
    expect(removed.error).toContain("只读");
    expect(pages.get(itemId, "body")?.id).toBe("theme-version-1");
  });

  it("已知装饰图失败的部分成功页仍可恢复，保留原有警告", async () => {
    const asset = { ...themedAsset("failed.png"), fileName: undefined, sha256: undefined, bytes: undefined, status: "failed" as const, error: "服务端超时" };
    pages.saveVersion(themedVersion(itemId, { id: "partial-previous", assets: [asset], warnings: ["服务端超时"] }));
    pages.publish("partial-previous");
    pages.saveVersion(themedVersion(itemId, { id: "current" })); pages.publish("current");
    const result = await call(IPC_CHANNELS.THEMED_READING_RESTORE_PREVIOUS);
    expect(result.success).toBe(true);
    expect(pages.get(itemId, "body")?.id).toBe("partial-previous");
    expect(pages.get(itemId, "body")?.warnings).toContain("服务端超时");
  });
});
