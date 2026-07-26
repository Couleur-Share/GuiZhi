/**
 * 启动完整性检查的跳过条件。
 *
 * quick_check 的耗时与库体积成正比（2 万条 / 364MB 实测 920ms），
 * 而它挡在窗口创建之前。这组用例锁住「什么时候可以不查」的边界：
 * 查漏了是白等一秒，跳错了是把坏库当好库打开。
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_TABLES } from "@guizhi/db/schema";
import { closeDatabase, initDatabase } from "@guizhi/db/init";

const WEEK_MS = 7 * 24 * 60 * 60 * 1000;

let workDir: string;
let dbPath: string;

/** 用真实文件建一个能打开的库，供 initDatabase 复用 */
function seedDatabase(): void {
  const db = new DatabaseAdapter(dbPath);
  db.exec(SCHEMA_TABLES);
  db.run(
    `INSERT INTO knowledge_items (id, title, content, created_at, updated_at)
     VALUES ('a', '标题', '正文', 1, 1)`,
  );
  db.close();
}

/** quick_check 是唯一能观测的信号：数它跑了几次 */
function countQuickChecks(): { calls: number; restore: () => void } {
  const original = DatabaseAdapter.prototype.pragma;
  const counter = { calls: 0, restore: () => undefined as void };
  const spy = vi
    .spyOn(DatabaseAdapter.prototype, "pragma")
    .mockImplementation(function (this: DatabaseAdapter.Database, sql: string) {
      if (sql.includes("quick_check")) {
        counter.calls++;
      }
      return original.call(this, sql);
    });
  counter.restore = () => spy.mockRestore();
  return counter;
}

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-integrity-"));
  dbPath = path.join(workDir, "knowledge.db");
  seedDatabase();
});

afterEach(() => {
  closeDatabase();
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("启动完整性检查", () => {
  it("首次启动（没有标记）会查", () => {
    const counter = countQuickChecks();
    initDatabase(dbPath);
    counter.restore();
    expect(counter.calls).toBe(1);
  });

  it("正常退出留下标记，下次启动跳过", () => {
    initDatabase(dbPath);
    closeDatabase();
    expect(fs.existsSync(`${dbPath}.clean`)).toBe(true);

    const counter = countQuickChecks();
    initDatabase(dbPath);
    counter.restore();
    expect(counter.calls).toBe(0);
  });

  // 崩溃后重启是跨进程的，同一个进程里 initDatabase 拿的是单例，
  // 模拟不出来。但那条路径由两件事构成，各自都能在这里验：
  // 启动时标记必被消费掉，且没有标记就会查（见上一条用例）。
  it("标记在启动时被消费掉：崩溃时没人补写，下次自然没得跳", () => {
    initDatabase(dbPath);
    closeDatabase();
    expect(fs.existsSync(`${dbPath}.clean`)).toBe(true);

    initDatabase(dbPath);
    expect(fs.existsSync(`${dbPath}.clean`)).toBe(false);
  });

  it("标记超过一周仍强制查一次：坏块不挑应用退没退干净", () => {
    initDatabase(dbPath);
    closeDatabase();
    fs.writeFileSync(`${dbPath}.clean`, String(Date.now() - WEEK_MS - 1000));

    const counter = countQuickChecks();
    initDatabase(dbPath);
    counter.restore();
    expect(counter.calls).toBe(1);
  });

  it("标记内容损坏时按「没有标记」处理", () => {
    initDatabase(dbPath);
    closeDatabase();
    fs.writeFileSync(`${dbPath}.clean`, "不是时间戳");

    const counter = countQuickChecks();
    initDatabase(dbPath);
    counter.restore();
    expect(counter.calls).toBe(1);
  });

  it("跳过的那次启动不会把校验时刻推后，一周兜底仍然到期", () => {
    // 一周内连开三次，标记里记的始终是最初那次真正校验的时刻
    initDatabase(dbPath);
    closeDatabase();
    const firstMarker = fs.readFileSync(`${dbPath}.clean`, "utf8");

    initDatabase(dbPath);
    closeDatabase();
    expect(fs.readFileSync(`${dbPath}.clean`, "utf8")).toBe(firstMarker);
  });
});
