import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { ThemedReadingDB } from "@guizhi/db/themed-reading";
import { BackupRepository } from "../../../src/main/services/backup-repository";
import { prepareRepositoryRestore } from "../../../src/main/services/backup-repository-restore";
import { previewRepositoryRestore } from "../../../src/main/services/backup-repository-preview";
import { inspectThemedReadingBackup } from "../../../src/main/services/backup-repository-themed-reading";
import { themedReadingDocument } from "../../../src/main/services/themed-reading/document";
import { themedAsset, themedTask, themedVersion } from "./themed-reading-fixture";
import { reconstructionFixture } from "../reading-reconstruction-fixture";

let root: string;
let db: Database;
let itemId: string;
let pages: ThemedReadingDB;
let repository: BackupRepository;
let targets: {databasePath: string; imagesDir: string; videosDir: string; configDir: string};
beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-theme-backup-"));
  targets = {databasePath: path.join(root, "data/knowledge.db"), imagesDir: path.join(root, "data/assets/images"),
    videosDir: path.join(root, "data/assets/videos"), configDir: path.join(root, "config")};
  for (const dir of [targets.imagesDir, targets.videosDir, targets.configDir]) fs.mkdirSync(dir, {recursive: true});
  db = new Database(targets.databasePath);
  db.pragma("foreign_keys=ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  itemId = new KnowledgeItemDB(db).create({title: "已编辑正文", content: "正文中没有图片引用"}).id;
  pages = new ThemedReadingDB(db);
  repository = new BackupRepository({...targets, repositoryDir: path.join(root, "repository")}, {
    backend: "isolated-test", isAvailable: () => true, isSecure: () => true, wrap: key => key, unwrap: key => key,
  });
  repository.initialize("isolated testing recovery password");
});
afterEach(() => {
  vi.restoreAllMocks();
  db.close();
  fs.rmSync(root, {recursive: true, force: true});
});

function savePage(id: string, fileName: string, publish = true) {
  const asset = themedAsset(fileName);
  fs.writeFileSync(path.join(targets.imagesDir, fileName), "theme image");
  const version = pages.saveVersion(themedVersion(itemId, {id, assets: [asset]}));
  if (publish) pages.publish(id);
  return version;
}

describe("主题阅读页完整备份", () => {
  it("v3 备份与恢复只检查数据，不执行脚本，保留完整研究正文和失败候选",()=>{
    const version=reconstructionFixture();version.itemId=itemId;version.role="working";version.formatVersion=3;
    version.options.enhancedInteraction=true;
    version.reconstruction.references=[{id:"R1",title:"完整公开资料",url:"https://example.org/full",capturedAt:1,text:"完整来源正文".repeat(12000),status:"ready"}];
    version.design={html:`<h1>标题</h1><h2>正文</h2><p>${"正文与原文独立保存。".repeat(20)}</p>`,css:"",direction:"",assets:[],scripts:[{id:"sideEffect",code:"globalThis.__readingBackupExecuted = true; while(true){}",status:"ready"}],libraries:[]};
    version.generation={route:"short",sections:[],css:"",scripts:[],libraries:[],revision:0,done:false,repairs:{},issues:[{kind:"script",unit:"candidate",message:"语法未完成"}],candidates:{candidate:"<button onclick=\"broken(\">尚未通过</button>"}};
    pages.saveVersion(version);pages.publish(version.id);
    const saved=repository.createSnapshot({db,appVersion:"test"});expect(saved.success).toBe(true);
    const prepared=prepareRepositoryRestore({repository,snapshotId:saved.snapshot!.fileName,liveDb:db,targets});
    const restored=new Database(prepared.databasePath);
    try{const page=new ThemedReadingDB(restored).get(itemId,"body");expect(page.formatVersion).toBe(3);expect(page.reconstruction.references[0].text).toBe(version.reconstruction.references[0].text);expect(page.generation).toEqual(version.generation);expect((globalThis as any).__readingBackupExecuted).toBeUndefined();expect(new KnowledgeItemDB(restored).get(itemId).content).toBe("正文中没有图片引用");}finally{restored.close();}
  });
  it("v2 编辑稿、研究资料和检查点随备份恢复，原文保持独立", () => {
    const version = reconstructionFixture(); version.itemId = itemId; version.role = "working";
    version.reconstruction.references = [{id:"R1",title:"直接资料",url:"https://example.org/r",capturedAt:1,text:"取得的研究正文",status:"ready"}];
    version.reconstruction.draft[0].referenceIds = ["R1"];
    pages.saveVersion(version); pages.publish(version.id);
    const saved = repository.createSnapshot({db, appVersion:"test"}); expect(saved.success).toBe(true);
    const prepared = prepareRepositoryRestore({repository,snapshotId:saved.snapshot!.fileName,liveDb:db,targets});
    const restored = new Database(prepared.databasePath);
    try {
      const page = new ThemedReadingDB(restored).get(itemId,"body");
      expect(page.formatVersion).toBe(2); expect(page.reconstruction).toEqual(version.reconstruction);
      expect(themedReadingDocument(page)).toContain("重新拟定的标题");
      expect(themedReadingDocument(page)).not.toContain("取得的研究正文");
      expect(new KnowledgeItemDB(restored).get(itemId).content).toBe("正文中没有图片引用");
    } finally {restored.close();}
  });
  it("当前、上一版、工作版及已完成图片离线恢复，正文保持编辑后内容", () => {
    savePage("previous", "previous.png");
    savePage("current", "current.png");
    const working = savePage("working", "working.png", false);
    pages.saveTask(themedTask(working, {state: "interrupted", completed: 1}));
    const saved = repository.createSnapshot({db, appVersion: "test"});
    expect(saved.success).toBe(true);
    expect(saved.snapshot?.summary?.assetCount).toBe(3);
    expect(previewRepositoryRestore(repository, saved.snapshot!.fileName).success).toBe(true);
    const prepared = prepareRepositoryRestore({repository, snapshotId: saved.snapshot!.fileName, liveDb: db, targets});
    const restored = new Database(prepared.databasePath);
    try {
      const restoredPages = new ThemedReadingDB(restored);
      expect(restoredPages.get(itemId, "body")?.id).toBe("current");
      expect(restoredPages.get(itemId, "body", "previous")?.id).toBe("previous");
      expect(restoredPages.get(itemId, "body", "working")?.id).toBe("working");
      expect(restoredPages.listTasks()[0]).toMatchObject({state: "interrupted", completed: 1});
      for (const name of restoredPages.listAssetFiles()) expect(fs.readFileSync(path.join(prepared.imagesDir, name), "utf8")).toBe("theme image");
      expect(themedReadingDocument(restoredPages.get(itemId, "body")!)).toContain("保持原文全部内容");
      expect(new KnowledgeItemDB(restored).get(itemId)?.content).toBe("正文中没有图片引用");
    } finally { restored.close(); }
  });

  it("工作版本尚未生成设计或验证原图字节时，引用图片仍随备份保存", () => {
    const pending = {...themedAsset("original.png"), status: "pending" as const, role: "original" as const, sha256: undefined, bytes: undefined};
    fs.writeFileSync(path.join(targets.imagesDir, pending.fileName!), "original picture");
    pages.saveVersion(themedVersion(itemId, {design: null, assets: [pending]}));
    const saved = repository.createSnapshot({db, appVersion: "test"});
    expect(saved.success).toBe(true);
    expect(saved.snapshot?.summary?.assetCount).toBe(1);
    expect(previewRepositoryRestore(repository, saved.snapshot!.fileName).success).toBe(true);
    const prepared = prepareRepositoryRestore({repository, snapshotId: saved.snapshot!.fileName, liveDb: db, targets});
    expect(fs.readFileSync(path.join(prepared.imagesDir, "original.png"), "utf8")).toBe("original picture");
  });

  it("资源缺失或校验不符时中止完整备份，不将丢图状态报告成功", () => {
    savePage("current", "current.png");
    fs.writeFileSync(path.join(targets.imagesDir, "current.png"), "tampered---");
    const damaged = repository.createSnapshot({db, appVersion: "test"});
    expect(damaged.success).toBe(false);
    expect(damaged.error).toMatch(/图片长度|图片校验/);
    fs.rmSync(path.join(targets.imagesDir, "current.png"));
    expect(repository.createSnapshot({db, appVersion: "test"}).success).toBe(false);
  });

  it("篡改正文占位符、版本索引和资产引用索引均不能通过预检", () => {
    const version = savePage("current", "current.png");
    db.run("DELETE FROM themed_reading_assets WHERE version_id=?", version.id);
    expect(() => inspectThemedReadingBackup(db)).toThrow("资源索引不完整");
    const current = pages.getVersion(version.id)!;
    pages.saveVersion({...current, design: {...current.design!, html: "<main></main>"}});
    const invalid = repository.createSnapshot({db, appVersion: "test"});
    expect(invalid.success).toBe(false);
    expect(invalid.error).toContain("正文内容块");
    db.run("UPDATE themed_reading_versions SET payload=? WHERE id=?", JSON.stringify({...current, itemId: "wrong-item"}), version.id);
    expect(() => inspectThemedReadingBackup(db)).toThrow("索引不一致");
  });

  it("备份清单未携带主题页引用的图片时，预检明确报告缺失并拒绝恢复", () => {
    savePage("current", "current.png");
    const saved = repository.createSnapshot({db, appVersion: "test"});
    const manifest = repository.readManifest(saved.snapshot!.fileName);
    vi.spyOn(repository, "readManifest").mockReturnValue({...manifest,
      entries: manifest.entries.filter(entry => entry.logicalPath !== "data/assets/images/current.png")});
    const preview = previewRepositoryRestore(repository, saved.snapshot!.fileName);
    expect(preview.success).toBe(false);
    expect(preview.missingFiles).toContain("data/assets/images/current.png");
    expect(() => prepareRepositoryRestore({repository, snapshotId: saved.snapshot!.fileName, liveDb: db, targets})).toThrow();
  });

  it("恢复旧备份时没有新表可直接通过，后续迁移负责创建空表", () => {
    db.exec("DROP TABLE themed_reading_tasks; DROP TABLE themed_reading_assets; DROP TABLE themed_reading_versions;");
    expect(inspectThemedReadingBackup(db)).toEqual([]);
    const saved = repository.createSnapshot({db, appVersion: "old-test"});
    expect(saved.success).toBe(true);
    expect(previewRepositoryRestore(repository, saved.snapshot!.fileName).success).toBe(true);
    const prepared = prepareRepositoryRestore({repository, snapshotId: saved.snapshot!.fileName, liveDb: db, targets});
    const restored = new Database(prepared.databasePath);
    try {
      expect(new KnowledgeItemDB(restored).get(itemId)?.content).toBe("正文中没有图片引用");
      expect(inspectThemedReadingBackup(restored)).toEqual([]);
    } finally { restored.close(); }
  });
});
