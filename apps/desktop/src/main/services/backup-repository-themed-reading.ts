import fs from "node:fs";
import path from "node:path";
import { createHash } from "node:crypto";
import type Database from "@guizhi/db/adapter";
import { hasThemedReadingTables, ThemedReadingDB } from "@guizhi/db/themed-reading";
import { themedReadingDocument } from "./themed-reading/document";

export interface ThemedReadingBackupAsset { fileName: string; sha256?: string; bytes?: number; }

/** 同时核对页面、引用索引与任务归属；旧备份没有主题页数据时无需迁移即可预检。 */
export function inspectThemedReadingBackup(db: Database): ThemedReadingBackupAsset[] {
  if (!hasThemedReadingTables(db)) return [];
  const pages = new ThemedReadingDB(db);
  const expected = new Map<string, ThemedReadingBackupAsset>();
  const files = new Map<string, ThemedReadingBackupAsset>();
  for (const row of db.all("SELECT id FROM themed_reading_versions") as {id: string}[]) {
    const version = pages.getVersion(row.id)!;
    const item = db.get("SELECT id FROM knowledge_items WHERE id=?", version.itemId);
    if (!item) throw new Error("主题阅读页所属条目不存在");
    // 即使是备份中保存的 HTML，也必须通过当前策略清理及全文槽位校验。
    if (version.design) themedReadingDocument(version);
    else if (version.role !== "working") throw new Error("已发布的主题阅读页缺少设计");
    for (const asset of version.assets) {
      if (!asset.fileName) continue;
      const reference = {fileName: asset.fileName, sha256: asset.sha256, bytes: asset.bytes};
      const key = `${version.id}:${asset.fileName}`;
      if (!expected.get(key)?.sha256) expected.set(key, reference);
      const previous = files.get(asset.fileName);
      if (previous?.sha256 && asset.sha256 && (previous.sha256 !== asset.sha256 || previous.bytes !== asset.bytes)) {
        throw new Error("主题阅读页共享资源校验信息不一致");
      }
      if (!previous?.sha256) files.set(asset.fileName, reference);
    }
  }
  const stored = db.all("SELECT version_id,file_name,sha256,bytes FROM themed_reading_assets") as {
    version_id: string; file_name: string; sha256: string; bytes: number;
  }[];
  if (stored.length !== expected.size) throw new Error("主题阅读页资源索引不完整");
  for (const row of stored) {
    const asset = expected.get(`${row.version_id}:${row.file_name}`);
    if (!asset || (asset.sha256 ?? "") !== row.sha256 || (asset.bytes ?? 0) !== row.bytes) throw new Error("主题阅读页资源索引与内容不一致");
  }
  for (const task of pages.listTasks()) {
    const version = pages.getVersion(task.versionId);
    if (!version || version.itemId !== task.itemId || version.sourceKind !== task.sourceKind) {
      throw new Error("主题阅读页任务所属版本不一致");
    }
  }
  return [...files.values()];
}

/** 文件必须来自指定的已暂存图片目录，不能通过备份载荷提供磁盘路径。 */
export function verifyThemedReadingBackupAssets(assets: ThemedReadingBackupAsset[], imagesDir: string): void {
  for (const asset of assets) {
    const file = path.join(imagesDir, asset.fileName);
    const info = fs.lstatSync(file);
    if (!info.isFile() || info.isSymbolicLink() || (asset.bytes !== undefined && info.size !== asset.bytes)) throw new Error(`主题阅读页图片长度无效: ${asset.fileName}`);
    const hash = createHash("sha256").update(fs.readFileSync(file)).digest("hex");
    if (asset.sha256 && hash !== asset.sha256) throw new Error(`主题阅读页图片校验失败: ${asset.fileName}`);
  }
}
