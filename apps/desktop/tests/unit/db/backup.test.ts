import { afterEach, beforeEach, describe, expect, it } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { KnowledgeItemDB } from "@guizhi/db/knowledge";
import {
  countActiveImportTasks,
  createBackup,
  deleteBackup,
  listBackups,
  maybeRunAutoBackup,
  performRestoreSwap,
  pruneAutoBackups,
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

  it("deleteBackup 拒绝路径穿越与非规范文件名", () => {
    const db = createFileDb("knowledge.db");
    const backup = createBackup(db, "manual", backupsDir);

    expect(deleteBackup("../knowledge.db", backupsDir)).toBe(false);
    expect(deleteBackup("random.txt", backupsDir)).toBe(false);
    expect(deleteBackup(backup.fileName, backupsDir)).toBe(true);
    expect(fs.existsSync(backup.path)).toBe(false);
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
});

describe("自动备份调度与恢复守卫", () => {
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
