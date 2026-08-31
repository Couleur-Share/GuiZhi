import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import Database from "../database/sqlite";
import { getSchemaVersion, SCHEMA_VERSION } from "@guizhi/db";
import type { BackupRestorePreview } from "@guizhi/shared/types";
import type { BackupRepository } from "./backup-repository";

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** 解密并逐对象校验；数据库另外执行 quick_check 与向前版本边界检查。 */
export function previewRepositoryRestore(
  repository: BackupRepository,
  snapshotId: string,
  password?: string,
): BackupRestorePreview {
  const missingFiles: string[] = [];
  const damagedFiles: string[] = [];
  const warnings: string[] = [];
  try {
    const manifest = repository.readManifest(snapshotId, password);
    const snapshot = repository
      .listSnapshots()
      .find((candidate) => candidate.fileName === snapshotId);
    if (!snapshot) throw new Error("找不到指定快照");

    let database: Buffer | null = null;
    for (const entry of manifest.entries) {
      try {
        const plain = repository.readEntry(entry);
        if (entry.category === "database") database = plain;
      } catch (error) {
        const message = errorText(error);
        if (/ENOENT|no such file/i.test(message)) missingFiles.push(entry.logicalPath);
        else damagedFiles.push(entry.logicalPath);
      }
    }
    if (!database) missingFiles.push("data/knowledge.db");

    if (database) {
      const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-restore-preview-"));
      const databasePath = path.join(tempDir, "knowledge.db");
      try {
        fs.writeFileSync(databasePath, database);
        const probe = new Database(databasePath, { readOnly: true });
        try {
          const quickCheck = probe.pragma("quick_check") as Array<Record<string, unknown>>;
          if (quickCheck.length !== 1 || Object.values(quickCheck[0])[0] !== "ok") {
            damagedFiles.push("data/knowledge.db");
          }
          const schemaVersion = getSchemaVersion(probe);
          if (schemaVersion > SCHEMA_VERSION) {
            damagedFiles.push("data/knowledge.db");
            warnings.push(
              `快照数据结构 v${schemaVersion} 高于当前应用支持的 v${SCHEMA_VERSION}`,
            );
          } else if (schemaVersion < SCHEMA_VERSION) {
            warnings.push(
              `恢复后将把数据结构从 v${schemaVersion} 迁移到 v${SCHEMA_VERSION}`,
            );
          }
        } finally {
          probe.close();
        }
      } finally {
        fs.rmSync(tempDir, { recursive: true, force: true });
      }
    }

    return {
      success: missingFiles.length === 0 && damagedFiles.length === 0,
      snapshot: {
        ...snapshot,
        validation:
          missingFiles.length === 0 && damagedFiles.length === 0
            ? "valid"
            : "invalid",
      },
      missingFiles: [...new Set(missingFiles)],
      damagedFiles: [...new Set(damagedFiles)],
      warnings,
      error:
        missingFiles.length || damagedFiles.length
          ? "快照存在缺失或损坏文件，不能恢复"
          : undefined,
    };
  } catch (error) {
    return {
      success: false,
      missingFiles,
      damagedFiles,
      warnings,
      error: errorText(error),
    };
  }
}
