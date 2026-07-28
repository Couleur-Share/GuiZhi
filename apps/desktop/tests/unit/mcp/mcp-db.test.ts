import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_TABLES } from "@guizhi/db/schema";
import {
  openKnowledgeDbReadOnly,
  resolveKnowledgeDbPath,
} from "../../../src/mcp/db";

let dataRoot: string;
let dbPath: string;
const originalEnv = process.env.GUIZHI_DATA_DIR;

function seedDatabase(userVersion?: number): void {
  fs.mkdirSync(path.dirname(dbPath), { recursive: true });
  const db = new DatabaseAdapter(dbPath);
  db.exec(SCHEMA_TABLES);
  if (userVersion !== undefined) {
    db.pragma(`user_version = ${userVersion}`);
  }
  db.close();
}

beforeEach(() => {
  dataRoot = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-mcp-db-"));
  dbPath = path.join(dataRoot, "data", "knowledge.db");
  process.env.GUIZHI_DATA_DIR = dataRoot;
});

afterEach(() => {
  if (originalEnv === undefined) {
    delete process.env.GUIZHI_DATA_DIR;
  } else {
    process.env.GUIZHI_DATA_DIR = originalEnv;
  }
  fs.rmSync(dataRoot, { recursive: true, force: true });
});

describe("MCP 数据库接入", () => {
  it("GUIZHI_DATA_DIR 覆盖数据目录", () => {
    expect(resolveKnowledgeDbPath()).toBe(dbPath);
  });

  it("没设环境变量时落到应用的默认数据目录", () => {
    delete process.env.GUIZHI_DATA_DIR;
    const resolved = resolveKnowledgeDbPath();
    expect(resolved.endsWith(path.join("data", "knowledge.db"))).toBe(true);
    expect(resolved).toContain("GuiZhi");
  });

  it("库不存在时报的是可行动的错误，不是裸 ENOENT", () => {
    let message = "";
    try {
      openKnowledgeDbReadOnly();
    } catch (error) {
      message = error instanceof Error ? error.message : String(error);
    }
    expect(message).toContain("找不到归知的知识库文件");
    expect(message).toContain(dbPath);
    expect(message).toContain("GUIZHI_DATA_DIR");
  });

  it("正常打开后能查，且登记了客户端租约", () => {
    seedDatabase();
    const handle = openKnowledgeDbReadOnly();
    try {
      // 归知主进程带 recoverUnregisteredLock 启动，没登记的话它会把本进程
      // 正持有的 .lock 目录当孤儿删掉
      const leases = fs.readdirSync(`${dbPath}.clients`);
      expect(leases).toContain(`${process.pid}.json`);

      const row = handle.db.get(
        "SELECT COUNT(*) AS c FROM knowledge_items",
      ) as { c: number };
      expect(row.c).toBe(0);
    } finally {
      handle.close();
    }
    expect(fs.existsSync(path.join(`${dbPath}.clients`, `${process.pid}.json`))).toBe(
      false,
    );
  });

  it("只读打开：写入被拒绝", () => {
    seedDatabase();
    const handle = openKnowledgeDbReadOnly();
    try {
      expect(() =>
        handle.db.run(
          "INSERT INTO knowledge_items (id, title, content, item_type, status, created_at, updated_at) VALUES ('x','x','x','note','active',1,1)",
        ),
      ).toThrow();
    } finally {
      handle.close();
    }
  });

  it("库来自更新版本的归知时明确拒绝，而不是拿旧表结构硬查", () => {
    seedDatabase(9999);
    let message = "";
    try {
      openKnowledgeDbReadOnly();
    } catch (error) {
      message = error instanceof Error ? error.message : String(error);
    }
    expect(message).toContain("更新版本的归知");
    // 失败路径也要还锁，否则归知下次启动会撞上一个没人认领的 .lock
    expect(fs.existsSync(path.join(`${dbPath}.clients`, `${process.pid}.json`))).toBe(
      false,
    );
  });

  it("close 可重复调用", () => {
    seedDatabase();
    const handle = openKnowledgeDbReadOnly();
    handle.close();
    expect(() => handle.close()).not.toThrow();
  });
});
