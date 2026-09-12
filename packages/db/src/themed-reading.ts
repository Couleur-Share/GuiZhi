import type Database from "./adapter";
import type { ThemedReadingSourceKind, ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";
import { validateThemedReadingTask, validateThemedReadingVersion } from "./themed-reading-validation";

export { isThemedReadingAssetFileName, validateThemedReadingTask, validateThemedReadingVersion } from "./themed-reading-validation";

interface VersionRow { id: string; item_id: string; source_kind: string; role: string; payload: string; }
interface TaskRow { id: string; item_id: string; source_kind: string; version_id: string; state: string; payload: string; }

export function hasThemedReadingTables(db: Database): boolean {
  return !!db.get("SELECT name FROM sqlite_master WHERE type='table' AND name='themed_reading_versions'");
}

/** 新旧数据库共用的资产枚举；包含当前、上一版及可继续的工作版本。 */
export function listThemedReadingAssetFiles(db: Database, itemIds?: string[]): string[] {
  if (!hasThemedReadingTables(db) || itemIds?.length === 0) return [];
  const where = itemIds ? ` WHERE v.item_id IN (${itemIds.map(() => "?").join(",")})` : "";
  return (db.all(`SELECT DISTINCT a.file_name FROM themed_reading_assets a
    JOIN themed_reading_versions v ON v.id=a.version_id${where} ORDER BY a.file_name`, ...(itemIds ?? [])) as {file_name: string}[])
    .map(row => row.file_name);
}

function versionFromRow(row: VersionRow | undefined): ThemedReadingVersion | null {
  if (!row) return null;
  const value = JSON.parse(row.payload);
  validateThemedReadingVersion(value);
  if (value.id !== row.id || value.itemId !== row.item_id || value.sourceKind !== row.source_kind || value.role !== row.role) {
    throw new Error("主题阅读页记录与索引不一致");
  }
  return value;
}

function taskFromRow(row: TaskRow | undefined): ThemedReadingTask | null {
  if (!row) return null;
  const value = JSON.parse(row.payload);
  validateThemedReadingTask(value);
  if (value.id !== row.id || value.itemId !== row.item_id || value.sourceKind !== row.source_kind ||
      value.versionId !== row.version_id || value.state !== row.state) throw new Error("主题阅读页任务与索引不一致");
  return value;
}

export class ThemedReadingDB {
  constructor(private db: Database) {}

  get(itemId: string, sourceKind: ThemedReadingSourceKind, role: ThemedReadingVersion["role"] = "current"): ThemedReadingVersion | null {
    return versionFromRow(this.db.get("SELECT * FROM themed_reading_versions WHERE item_id=? AND source_kind=? AND role=?", itemId, sourceKind, role) as VersionRow | undefined);
  }

  getVersion(id: string): ThemedReadingVersion | null {
    return versionFromRow(this.db.get("SELECT * FROM themed_reading_versions WHERE id=?", id) as VersionRow | undefined);
  }

  private assertWritable(itemId: string): void {
    const item = this.db.get("SELECT deleted_at FROM knowledge_items WHERE id=?", itemId) as {deleted_at: number | null} | undefined;
    if (!item || item.deleted_at !== null) throw new Error("条目不存在或已在回收站，不能继续生成主题阅读页");
  }

  private assertIdle(itemId: string, sourceKind: ThemedReadingSourceKind): void {
    if (this.db.get("SELECT id FROM themed_reading_tasks WHERE item_id=? AND source_kind=? AND state IN ('queued','running') LIMIT 1", itemId, sourceKind)) {
      throw new Error("主题阅读页任务仍在执行，请先取消任务");
    }
  }

  saveVersion(version: ThemedReadingVersion, task?: ThemedReadingTask): ThemedReadingVersion {
    validateThemedReadingVersion(version);
    if (task) {
      validateThemedReadingTask(task);
      if (task.versionId !== version.id || task.itemId !== version.itemId || task.sourceKind !== version.sourceKind) throw new Error("主题检查点与任务归属不一致");
    }
    return this.db.transaction(() => {
      this.assertWritable(version.itemId);
      const existing = this.getVersion(version.id);
      if (existing && (existing.itemId !== version.itemId || existing.sourceKind !== version.sourceKind || existing.role !== version.role)) {
        throw new Error("不能更改主题阅读页的归属或直接切换版本角色");
      }
      const replaced = this.get(version.itemId, version.sourceKind, version.role);
      if (replaced && replaced.id !== version.id) {
        if (version.role !== "working") throw new Error("已发布版本必须通过发布操作替换");
        this.assertIdle(version.itemId, version.sourceKind);
        this.db.run("DELETE FROM themed_reading_versions WHERE id=?", replaced.id);
      }
      this.db.run(`INSERT INTO themed_reading_versions (id,item_id,source_kind,role,payload,created_at,updated_at)
        VALUES (?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`,
      version.id, version.itemId, version.sourceKind, version.role, JSON.stringify(version), version.createdAt, version.updatedAt);
      this.db.run("DELETE FROM themed_reading_assets WHERE version_id=?", version.id);
      const assets = new Map<string, {sha256: string; bytes: number}>();
      for (const asset of version.assets) {
        if (!asset.fileName) continue;
        const previous = assets.get(asset.fileName);
        if (previous?.sha256 && asset.sha256 && (previous.sha256 !== asset.sha256 || previous.bytes !== asset.bytes)) throw new Error("主题阅读页同名资源校验信息不一致");
        // 尚未读取字节的原图也要保留引用，空摘要表示待校验而非零字节文件。
        if (!previous?.sha256) assets.set(asset.fileName, {sha256: asset.sha256 ?? "", bytes: asset.bytes ?? 0});
      }
      for (const [name, asset] of assets) this.db.run("INSERT INTO themed_reading_assets VALUES (?,?,?,?)", version.id, name, asset.sha256, asset.bytes);
      // 图片引用与成功落盘计数必须同时提交，不能留下成功计数但资源检查点丢失。
      if (task) this.saveTask(task);
      return this.getVersion(version.id)!;
    })();
  }

  listTasks(): ThemedReadingTask[] {
    return (this.db.all("SELECT * FROM themed_reading_tasks ORDER BY updated_at DESC,id DESC") as TaskRow[]).map(row => taskFromRow(row)!);
  }

  getTask(id: string): ThemedReadingTask | null {
    return taskFromRow(this.db.get("SELECT * FROM themed_reading_tasks WHERE id=?", id) as TaskRow | undefined);
  }

  saveTask(task: ThemedReadingTask): ThemedReadingTask {
    validateThemedReadingTask(task);
    const existing = this.getTask(task.id);
    if (existing && (existing.itemId !== task.itemId || existing.sourceKind !== task.sourceKind || existing.versionId !== task.versionId)) {
      throw new Error("不能更改主题阅读页任务的归属");
    }
    if (["queued", "running"].includes(task.state)) this.assertWritable(task.itemId);
    this.db.run(`INSERT INTO themed_reading_tasks (id,item_id,source_kind,version_id,state,payload,created_at,updated_at)
      VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET state=excluded.state,payload=excluded.payload,updated_at=excluded.updated_at`,
    task.id, task.itemId, task.sourceKind, task.versionId, task.state, JSON.stringify(task), task.createdAt, task.updatedAt);
    return this.getTask(task.id)!;
  }

  private setRole(version: ThemedReadingVersion, role: ThemedReadingVersion["role"]): void {
    const next = {...version, role, updatedAt: Date.now()};
    this.db.run("UPDATE themed_reading_versions SET role=?,payload=?,updated_at=? WHERE id=?", role, JSON.stringify(next), next.updatedAt, version.id);
  }

  publish(versionId: string, task?: ThemedReadingTask): ThemedReadingVersion | null {
    return this.db.transaction(() => {
      const version = this.getVersion(versionId);
      if (!version) return null;
      if(task&&(task.versionId!==version.id||task.itemId!==version.itemId||task.sourceKind!==version.sourceKind||!["completed","partial"].includes(task.state)))throw new Error("发布任务与页面不一致");
      this.assertWritable(version.itemId);
      if (version.role !== "working" || !version.design) throw new Error("只能发布已完成设计的工作版本");
      const latest = this.db.get("SELECT state FROM themed_reading_tasks WHERE version_id=? ORDER BY updated_at DESC,rowid DESC LIMIT 1", version.id) as {state: string} | undefined;
      if (latest && !["running", "completed", "partial"].includes(latest.state)) throw new Error("任务已停止，不能发布迟到的主题阅读页");
      const current = this.get(version.itemId, version.sourceKind);
      this.db.run("DELETE FROM themed_reading_versions WHERE item_id=? AND source_kind=? AND role='previous'", version.itemId, version.sourceKind);
      if (current) this.setRole(current, "previous");
      this.setRole(version, "current");
      if(task)this.saveTask(task);
      return this.getVersion(version.id);
    })();
  }

  restorePrevious(itemId: string, sourceKind: ThemedReadingSourceKind): ThemedReadingVersion | null {
    return this.db.transaction(() => {
      this.assertWritable(itemId);
      this.assertIdle(itemId, sourceKind);
      const previous = this.get(itemId, sourceKind, "previous"), current = this.get(itemId, sourceKind);
      if (!previous) return null;
      // SQLite 唯一约束逐行检查；暂时清空角色，事务提交前完整恢复。
      if (current) this.db.run("UPDATE themed_reading_versions SET role=NULL WHERE id=?", current.id);
      this.setRole(previous, "current");
      if (current) this.setRole(current, "previous");
      return this.getVersion(previous.id);
    })();
  }

  remove(itemId: string, sourceKind: ThemedReadingSourceKind): string[] {
    return this.db.transaction(() => {
      this.assertIdle(itemId, sourceKind);
      const files = (this.db.all(`SELECT DISTINCT a.file_name FROM themed_reading_assets a JOIN themed_reading_versions v
        ON v.id=a.version_id WHERE v.item_id=? AND v.source_kind=?`, itemId, sourceKind) as {file_name: string}[]).map(row => row.file_name);
      this.db.run("DELETE FROM themed_reading_versions WHERE item_id=? AND source_kind=?", itemId, sourceKind);
      return files;
    })();
  }

  interruptRunning(): ThemedReadingTask[] {
    return this.db.transaction(() => this.listTasks().filter(task => ["queued", "running"].includes(task.state)).map(task => this.saveTask({
      ...task, state: "interrupted", error: "应用运行已中断；已完成的图片已保留，请手动继续", updatedAt: Date.now(),
    })))();
  }

  hasActiveTasks(): boolean {
    return !!this.db.get("SELECT id FROM themed_reading_tasks WHERE state IN ('queued','running') LIMIT 1");
  }

  listAssetFiles(): string[] { return listThemedReadingAssetFiles(this.db); }
}
