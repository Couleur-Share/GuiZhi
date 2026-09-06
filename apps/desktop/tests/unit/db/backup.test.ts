import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import { CrawlJobDB } from "@guizhi/db/crawl-job";
import { MIGRATIONS } from "@guizhi/db/migrations";
import {
  countActiveImportTasks,
  backupPendingSchemaUpgrade,
  createBackup,
  deleteBackup,
  listBackups,
  maybeRunAutoBackup,
  performRestoreSwap,
  pruneAutoBackups,
  pruneBackupsOfKind,
  validateBackupFile,
} from "../../../src/main/services/backup";

let workDir: string;
let backupsDir: string;

function createFileDb(fileName: string): DatabaseAdapter.Database {
  const db = new DatabaseAdapter(path.join(workDir, fileName));
  db.pragma("foreign_keys = ON");
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  return db;
}

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-backup-test-"));
  backupsDir = path.join(workDir, "backups");
});

afterEach(() => {
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("createBackup（VACUUM INTO 在线备份）", () => {
  it("已含 0027 的旧库仍在 0030 前保存快照；全部迁移完成后不重复备份", () => {
    const db = createFileDb("schema-upgrade.db");
    try {
      db.exec("CREATE TABLE schema_migrations(name TEXT PRIMARY KEY, applied_at INTEGER NOT NULL); PRAGMA user_version=29");
      for (const migration of MIGRATIONS.slice(0, -1)) db.run("INSERT INTO schema_migrations VALUES (?,?)", migration.name, 1);
      const item = new KnowledgeItemDB(db).create({ title: "升级保护", content: "人工编辑" });
      const backup = backupPendingSchemaUpgrade(db, backupsDir);
      expect(backup?.kind).toBe("pre-update");
      const probe = new DatabaseAdapter(backup!.path, { readOnly: true });
      try {
        expect(probe.get("PRAGMA user_version")).toEqual({ user_version: 29 });
        expect(probe.get("SELECT content FROM knowledge_items WHERE id=?", item.id)).toEqual({ content: "人工编辑" });
      } finally { probe.close(); }
      db.run("INSERT INTO schema_migrations VALUES (?,?)", MIGRATIONS.at(-1)!.name, 1);
      expect(backupPendingSchemaUpgrade(db, backupsDir)).toBeNull();
      expect(listBackups(backupsDir)).toHaveLength(1);
    } finally { db.close(); }
  });

  it("升级快照无法写入时抛错，不继续迁移", () => {
    const db = createFileDb("snapshot-failure.db");
    const blocked = path.join(workDir, "not-a-directory");
    fs.writeFileSync(blocked, "保留文件");
    try { expect(() => backupPendingSchemaUpgrade(db, blocked)).toThrow(); }
    finally { db.close(); }
  });

  it("全新空库不产生无意义的升级备份", () => {
    const db = new DatabaseAdapter(":memory:");
    try { expect(backupPendingSchemaUpgrade(db, backupsDir)).toBeNull(); }
    finally { db.close(); }
  });
  it("对打开中的库生成可读的一致副本", () => {
    const db = createFileDb("knowledge.db");
    const items = new KnowledgeItemDB(db);
    items.create({ title: "备份验证条目", content: "内容 A" });

    const backup = createBackup(db, "manual", backupsDir);
    expect(fs.existsSync(backup.path)).toBe(true);
    expect(backup.kind).toBe("manual");
    expect(backup.fileName).toMatch(/^knowledge-manual-\d{8}-\d{6}(?:-\d+)?\.db$/);
    expect(backup.sizeBytes).toBeGreaterThan(0);

    // 备份文件可独立打开且数据一致
    const probe = new DatabaseAdapter(backup.path, { readOnly: true });
    const row = probe.get(
      "SELECT COUNT(*) AS count FROM knowledge_items",
    ) as { count: number };
    expect(row.count).toBe(1);
    probe.close();
    db.close();
  });

  it("同一秒内多次备份文件名不冲突", () => {
    const db = createFileDb("knowledge.db");
    const first = createBackup(db, "auto", backupsDir);
    const second = createBackup(db, "auto", backupsDir);
    expect(first.fileName).not.toBe(second.fileName);
    db.close();
  });
});

describe("listBackups / deleteBackup", () => {
  it("列出规范命名的备份并按时间倒序", () => {
    const db = createFileDb("knowledge.db");
    createBackup(db, "manual", backupsDir);
    createBackup(db, "auto", backupsDir);
    // 非备份文件不应出现在列表里
    fs.writeFileSync(path.join(backupsDir, "random.txt"), "x");

    const backups = listBackups(backupsDir);
    expect(backups).toHaveLength(2);
    expect(backups.map((b) => b.kind).sort()).toEqual(["auto", "manual"]);
    db.close();
  });

  it("deleteBackup 拒绝路径穿越与非规范文件名，并说明拒绝的原因", () => {
    const db = createFileDb("knowledge.db");
    const backup = createBackup(db, "manual", backupsDir);

    // 只回一个 false 的话，界面上就只剩「删除失败」三个字
    expect(deleteBackup("../knowledge.db", backupsDir)).toEqual({
      success: false,
      error: "备份文件名不合法",
    });
    expect(deleteBackup("random.txt", backupsDir)).toEqual({
      success: false,
      error: "备份文件名不合法",
    });
    expect(deleteBackup(backup.fileName, backupsDir)).toEqual({
      success: true,
    });
    expect(fs.existsSync(backup.path)).toBe(false);
    // 同一个文件删第二次：文件已经不在了
    expect(deleteBackup(backup.fileName, backupsDir)).toEqual({
      success: false,
      error: "备份文件已不存在",
    });
    db.close();
  });
});

describe("pruneAutoBackups", () => {
  it("只清理超出保留数的自动备份，手动备份不受影响", () => {
    const db = createFileDb("knowledge.db");
    createBackup(db, "auto", backupsDir);
    createBackup(db, "auto", backupsDir);
    createBackup(db, "auto", backupsDir);
    createBackup(db, "manual", backupsDir);

    const pruned = pruneAutoBackups(1, backupsDir);
    expect(pruned).toBe(2);

    const remaining = listBackups(backupsDir);
    expect(remaining.filter((b) => b.kind === "auto")).toHaveLength(1);
    expect(remaining.filter((b) => b.kind === "manual")).toHaveLength(1);
    db.close();
  });

  it("快照类备份同样受保留数约束（每次更新都留一份整库副本）", () => {
    const db = createFileDb("knowledge.db");
    for (let i = 0; i < 5; i++) {
      createBackup(db, "pre-update", backupsDir);
    }
    createBackup(db, "manual", backupsDir);

    expect(pruneBackupsOfKind("pre-update", 3, backupsDir)).toBe(2);
    const remaining = listBackups(backupsDir);
    expect(remaining.filter((b) => b.kind === "pre-update")).toHaveLength(3);
    expect(remaining.filter((b) => b.kind === "manual")).toHaveLength(1);
    db.close();
  });

  it("清理顺序按文件名时间戳，不受 mtime 干扰", () => {
    const db = createFileDb("knowledge.db");
    fs.mkdirSync(backupsDir, { recursive: true });
    db.close();

    // 手工造三份不同时间的备份，再把 mtime 打乱成与时间戳相反的顺序
    // （整体复制/同步备份目录后就是这个状态）
    const names = [
      "knowledge-auto-20260101-000000.db",
      "knowledge-auto-20260102-000000.db",
      "knowledge-auto-20260103-000000.db",
    ];
    names.forEach((name, index) => {
      const fullPath = path.join(backupsDir, name);
      fs.writeFileSync(fullPath, "x");
      const mtime = new Date(2020, 0, names.length - index);
      fs.utimesSync(fullPath, mtime, mtime);
    });

    expect(pruneAutoBackups(1, backupsDir)).toBe(2);
    expect(fs.readdirSync(backupsDir)).toEqual([
      "knowledge-auto-20260103-000000.db",
    ]);
  });
});

describe("validateBackupFile", () => {
  it("合法备份通过校验", () => {
    const db = createFileDb("knowledge.db");
    const backup = createBackup(db, "manual", backupsDir);
    expect(validateBackupFile(backup.path).ok).toBe(true);
    db.close();
  });

  it("拒绝非 SQLite 文件与缺核心表的库", () => {
    const garbagePath = path.join(workDir, "garbage.db");
    fs.writeFileSync(garbagePath, "这不是一个 SQLite 文件");
    expect(validateBackupFile(garbagePath).ok).toBe(false);

    const bare = new DatabaseAdapter(path.join(workDir, "bare.db"));
    bare.exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)");
    bare.close();
    const result = validateBackupFile(path.join(workDir, "bare.db"));
    expect(result.ok).toBe(false);
    expect(result.error).toContain("knowledge_items");
  });
});

describe("performRestoreSwap", () => {
  it("备份覆盖主库前先保存 pre-restore 快照", () => {
    // 当前库有条目 A
    const current = createFileDb("knowledge.db");
    new KnowledgeItemDB(current).create({ title: "条目 A", content: "" });
    // 备份文件里是条目 B
    const other = createFileDb("other.db");
    new KnowledgeItemDB(other).create({ title: "条目 B", content: "" });
    const backup = createBackup(other, "manual", backupsDir);
    other.close();
    current.close();

    const databasePath = path.join(workDir, "knowledge.db");
    const { preRestoreFileName } = performRestoreSwap({
      databasePath,
      backupFilePath: backup.path,
      backupsDir,
    });

    // 主库现在是备份内容
    const restored = new DatabaseAdapter(databasePath);
    const row = restored.get(
      "SELECT title FROM knowledge_items",
    ) as { title: string };
    expect(row.title).toBe("条目 B");
    restored.close();

    // pre-restore 快照里是原内容
    expect(preRestoreFileName).toBeTruthy();
    const snapshot = new DatabaseAdapter(
      path.join(backupsDir, preRestoreFileName!),
    );
    const snapshotRow = snapshot.get(
      "SELECT title FROM knowledge_items",
    ) as { title: string };
    expect(snapshotRow.title).toBe("条目 A");
    snapshot.close();
  });

  it("换库失败时主库回滚到快照，不会留下半个文件", () => {
    const current = createFileDb("knowledge.db");
    new KnowledgeItemDB(current).create({ title: "条目 A", content: "" });
    current.close();

    const databasePath = path.join(workDir, "knowledge.db");
    const originalSize = fs.statSync(databasePath).size;

    // 备份文件不存在 → copyFileSync 抛错，模拟拷贝中途失败
    expect(() =>
      performRestoreSwap({
        databasePath,
        backupFilePath: path.join(workDir, "missing-backup.db"),
        backupsDir,
      }),
    ).toThrow();

    // 主库仍然可打开且内容完好，中间文件不残留
    expect(fs.existsSync(`${databasePath}.incoming`)).toBe(false);
    expect(fs.statSync(databasePath).size).toBe(originalSize);
    const reopened = new DatabaseAdapter(databasePath);
    const row = reopened.get("SELECT title FROM knowledge_items") as {
      title: string;
    };
    expect(row.title).toBe("条目 A");
    reopened.close();
  });
});

describe("自动备份调度与恢复守卫", () => {
  it("网页批次及暂停中的页面阻止换库，备份队列恢复后等待继续",()=>{
    const db=createFileDb("web-batch.db"),jobs=new CrawlJobDB(db);
    const job=jobs.create({purpose:"documents",seeds:[{url:"https://example.com/docs/",mode:"directory"}]});
    expect(countActiveImportTasks(db)).toBe(1);
    jobs.setStatus(job.id,"paused");jobs.save({...jobs.pages(job.id)[0],status:"running"});
    expect(countActiveImportTasks(db)).toBe(1);
    jobs.setStatus(job.id,"running");const backup=createBackup(db,"manual",backupsDir);
    const restored=new DatabaseAdapter(backup.path);const restoredJobs=new CrawlJobDB(restored);restoredJobs.recover();
    expect(restoredJobs.get(job.id)?.status).toBe("interrupted");expect(restoredJobs.pages(job.id)[0].status).toBe("pending");
    expect(restored.pragma("foreign_key_check")).toEqual([]);restored.close();db.close();
  });
  it("maybeRunAutoBackup 首次到期执行并记录时间，未到期不重复", () => {
    const db = createFileDb("knowledge.db");
    // 默认设置（未写 settings）：启用、24 小时间隔、从未备份 → 应执行
    const first = maybeRunAutoBackup(db, backupsDir);
    expect(first).toBe(true);
    expect(listBackups(backupsDir).filter((b) => b.kind === "auto")).toHaveLength(1);
    const lastAtRow = db.get(
      "SELECT value FROM settings WHERE key = 'backupLastAutoAt'",
    ) as { value: string };
    expect(Number(JSON.parse(lastAtRow.value))).toBeGreaterThan(0);

    const second = maybeRunAutoBackup(db, backupsDir);
    expect(second).toBe(false);
    db.close();
  });

  it("禁用后不执行自动备份", () => {
    const db = createFileDb("knowledge.db");
    db.run(
      "INSERT OR REPLACE INTO settings (key, value) VALUES ('backupAutoEnabled', 'false')",
    );
    expect(maybeRunAutoBackup(db, backupsDir)).toBe(false);
    db.close();
  });

  it("countActiveImportTasks 统计 pending/processing", () => {
    const db = createFileDb("knowledge.db");
    const now = Date.now();
    db.run(
      `INSERT INTO import_tasks (id, source_kind, source_input, display_name, status, created_at, updated_at)
       VALUES ('t1', 'text', 'a', 'a', 'pending', ?, ?),
              ('t2', 'text', 'b', 'b', 'completed', ?, ?)`,
      now,
      now,
      now,
      now,
    );
    expect(countActiveImportTasks(db)).toBe(1);
    db.close();
  });
});
