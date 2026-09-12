import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ThemedReadingDB } from "@guizhi/db/themed-reading";
import { configureRuntimePaths, getImagesDir, resetRuntimePaths } from "../../../src/main/runtime-paths";
import { cleanupOrphanAssets, retainAssetFiles } from "../../../src/main/services/asset-cleanup";
import { themedAsset, themedVersion } from "./themed-reading-fixture";

let db: Database;
let root: string;
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-theme-assets-"));
  configureRuntimePaths({userDataPath: root});
  fs.mkdirSync(getImagesDir(), {recursive: true});
  db = new Database(":memory:");
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
});
afterEach(() => {
  db.close();
  resetRuntimePaths();
  fs.rmSync(root, {recursive: true, force: true});
});

describe("主题阅读页资产生命周期", () => {
  it("正文换图、回收站及共享引用保留主题页图片，最后引用删除才清理", () => {
    const items = new KnowledgeItemDB(db), pages = new ThemedReadingDB(db);
    const first = items.create({title: "正文", content: "![](local-image://shared.png)"});
    const second = items.create({title: "其他正文", content: "![](local-image://shared.png)"});
    const file = path.join(getImagesDir(), "shared.png");
    fs.writeFileSync(file, "theme image");
    pages.saveVersion(themedVersion(first.id, {assets: [themedAsset("shared.png")]}));
    items.update(first.id, {content: "已移除正文插图"});
    items.update(second.id, {content: "无图"});
    expect(cleanupOrphanAssets(items, ["shared.png"])).toBe(0);
    items.moveToTrash([first.id]);
    expect(items.listReferencedAssets().has("shared.png")).toBe(true);
    expect(cleanupOrphanAssets(items, ["shared.png"])).toBe(0);
    const candidates = items.listAssetRefs([first.id]);
    items.deleteForever([first.id]);
    expect(cleanupOrphanAssets(items, candidates)).toBe(1);
    expect(fs.existsSync(file)).toBe(false);
  });

  it("尚未校验字节的原图引用也受工作版本保护", () => {
    const items = new KnowledgeItemDB(db), pages = new ThemedReadingDB(db);
    const item = items.create({title: "正文"});
    fs.writeFileSync(path.join(getImagesDir(), "pending.png"), "original image");
    pages.saveVersion(themedVersion(item.id, {design: null, assets: [{...themedAsset("pending.png"), role: "original", status: "pending", sha256: undefined, bytes: undefined}]}));
    expect(items.listAssetRefs([item.id])).toEqual(["pending.png"]);
    expect(cleanupOrphanAssets(items, ["pending.png"])).toBe(0);
    expect(cleanupOrphanAssets(items, pages.remove(item.id, "body"))).toBe(1);
  });

  it("多个租约独立释放，落盘至引用提交窗口不被清理", () => {
    const items = new KnowledgeItemDB(db), name = "leased.png";
    fs.writeFileSync(path.join(getImagesDir(), name), "new image");
    const first = retainAssetFiles([name, name]);
    const second = retainAssetFiles([name]);
    try {
      expect(cleanupOrphanAssets(items, [name])).toBe(0);
      first();
      first();
      expect(cleanupOrphanAssets(items, [name])).toBe(0);
      second();
      expect(cleanupOrphanAssets(items, [name])).toBe(1);
    } finally { first(); second(); }
    expect(() => retainAssetFiles(["../escape.png"])).toThrow("无效文件名");
  });
});
