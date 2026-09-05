import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import Database from "@guizhi/db/adapter";
import { SCHEMA_TABLES, SCHEMA_INDEXES } from "@guizhi/db/schema";
import { runMigrations } from "@guizhi/db/migrations";
import { initDatabase, closeDatabase } from "@guizhi/db/init";
import { ResearchDB } from "@guizhi/db";
import { createBackup } from "../../../src/main/services/backup";

const dirs: string[] = [];
afterEach(() => { closeDatabase(); dirs.splice(0).forEach((dir) => fs.rmSync(dir, { recursive: true, force: true })); });
function legacy() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-research-migration-")); dirs.push(dir);
  const file = path.join(dir, "knowledge.db"), db = new Database(file);
  db.exec(SCHEMA_TABLES); db.exec(SCHEMA_INDEXES); runMigrations(db);
  const store = new ResearchDB(db);
  const run = store.create({ topic: "旧版研究", dayRange: 7, depth: "quick", sources: ["bilibili"] }, 0, 1);
  store.saveReport(run.id, "旧版原始报告，不重新评分", "legacy"); store.finishRun(run.id, "ready");
  for (const table of ["research_contexts", "research_attempts", "research_snapshots", "research_authors", "research_documents", "research_series", "research_saved_reports"]) db.exec(`DROP TABLE ${table}`);
  db.run("DELETE FROM schema_migrations WHERE name >= '0025'"); db.pragma("user_version=24"); db.close();
  return { dir, file, runId: run.id };
}
describe("研究迁移的升级前备份", () => {
  it("备份仍是旧 schema，升级保留旧报告且不创建新策略上下文", () => {
    const { dir, file, runId } = legacy(); let backupPath = "";
    const db = initDatabase(file, { beforeSchemaUpgrade: (opened) => {
      expect(opened.get("SELECT name FROM sqlite_master WHERE name='research_documents'")).toBeFalsy();
      backupPath = createBackup(opened, "pre-update", path.join(dir, "backups")).path;
    } });
    expect(new ResearchDB(db).get(runId)?.reportMarkdown).toBe("旧版原始报告，不重新评分");
    expect(new ResearchDB(db).get(runId)?.context).toBeUndefined();
    expect(runMigrations(db)).toEqual([]);
    const backup = new Database(backupPath);
    try {
      expect(backup.get("SELECT name FROM sqlite_master WHERE name='research_documents'")).toBeFalsy();
      expect(backup.get("SELECT report_markdown FROM research_runs WHERE id=?", runId)).toEqual({ report_markdown: "旧版原始报告，不重新评分" });
    } finally { backup.close(); }
  });
  it("备份失败时在任何 schema 变更之前中止，可再次启动", () => {
    const { file } = legacy();
    expect(() => initDatabase(file, { beforeSchemaUpgrade: () => { throw new Error("备份失败"); } })).toThrow("备份失败");
    const probe = new Database(file);
    expect(probe.get("SELECT name FROM sqlite_master WHERE name='research_documents'")).toBeFalsy(); probe.close();
    const reopened = initDatabase(file);
    expect(reopened.get("SELECT name FROM sqlite_master WHERE name='research_documents'")).toBeTruthy();
  });
});
