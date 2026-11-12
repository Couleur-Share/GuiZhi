import { inspectThemedReadingBackup, verifyThemedReadingBackupAssets } from "./backup-repository-themed-reading";
import { retainAssetFiles } from "./asset-cleanup";
import fs from "node:fs";
import path from "node:path";
import { createHash, randomUUID } from "node:crypto";
import { gzipSync, gunzipSync } from "node:zlib";
import Database from "../database/sqlite";
import { getSchemaVersion, SCHEMA_VERSION } from "@guizhi/db";
import { extractLocalAssetRefs } from "@guizhi/shared/utils/media-refs";
import type {
  BackupFileInfo,
  BackupRepositoryStatus,
  PortableBackupImportResult,
  RepositorySnapshotRequest,
  RepositorySnapshotResult,
} from "@guizhi/shared/types";
import {
  createRepositoryKey,
  decryptBuffer,
  encryptBuffer,
  type PasswordWrappedKey,
  type RepositoryKeyProtector,
  unwrapKeyWithPassword,
  wrapKeyWithPassword,
} from "./backup-repository-crypto";
import {
  writePortableBackup,
  type PortableSource,
} from "./backup-portable";
import { extractPortableArchive } from "./backup-portable-reader";

const REPOSITORY_VERSION = 1;
const HEADER_FILE = "repository-header.json";
const CONFIG_FILE_NAMES = [
  "ai-models.json",
  "illustration-styles.json",
  "shortcuts.json",
  "shortcut-mode.json",
  "mcp.json",
] as const;

interface RepositoryHeader {
  kind: "guizhi-backup-repository";
  version: number;
  createdAt: number;
  passwordWrappedKey: PasswordWrappedKey;
  safeStorageWrappedKey: string | null;
  keyStorageBackend: string | null;
}

export type SnapshotEntryCategory =
  | "database"
  | "config"
  | "renderer-settings"
  | "media";

export interface RepositorySnapshotEntry {
  logicalPath: string;
  category: SnapshotEntryCategory;
  sha256: string;
  sizeBytes: number;
  storedBytes: number;
  compression: "gzip" | "none";
}

export interface RepositorySnapshotManifest {
  kind: "guizhi-repository-snapshot";
  version: number;
  id: string;
  createdAt: number;
  appVersion: string;
  schemaVersion: number;
  entries: RepositorySnapshotEntry[];
  summary: {
    itemCount: number;
    assetCount: number;
    configDomains: string[];
  };
}

interface SnapshotHeader {
  kind: "guizhi-repository-snapshot-header";
  version: number;
  id: string;
  createdAt: number;
  appVersion: string;
  schemaVersion: number;
  itemCount: number;
  assetCount: number;
  configDomains: string[];
  manifestSha256: string;
  sizeBytes: number;
  backupKind?: "manual" | "auto";
}

export interface BackupRepositoryPaths {
  repositoryDir: string;
  databasePath: string;
  configDir: string;
  imagesDir: string;
  videosDir: string;
}

interface SnapshotInput {
  db: Database.Database;
  appVersion: string;
  request?: RepositorySnapshotRequest;
  kind?: "manual" | "auto";
}

function sha256(value: Buffer): string {
  return createHash("sha256").update(value).digest("hex");
}

function atomicWrite(filePath: string, value: Buffer | string): void {
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  const tempPath = `${filePath}.tmp-${randomUUID()}`;
  try {
    fs.writeFileSync(tempPath, value);
    fs.renameSync(tempPath, filePath);
  } finally {
    fs.rmSync(tempPath, { force: true });
  }
}

function readJson<T>(filePath: string): T {
  return JSON.parse(fs.readFileSync(filePath, "utf8")) as T;
}

function validateHeader(header: RepositoryHeader): void {
  if (
    header?.kind !== "guizhi-backup-repository" ||
    header.version !== REPOSITORY_VERSION ||
    !header.passwordWrappedKey?.salt ||
    !header.passwordWrappedKey?.data
  ) {
    throw new Error("备份仓库头不合法或版本不受支持");
  }
}

export class BackupRepository {
  private cachedKey: Buffer | null = null;

  constructor(
    private readonly paths: BackupRepositoryPaths,
    private readonly protector: RepositoryKeyProtector,
  ) {}

  status(): BackupRepositoryStatus {
    const initialized = fs.existsSync(this.headerPath());
    if (!initialized) {
      return {
        initialized: false,
        automaticAccessAvailable: false,
        keyStorageBackend: null,
      };
    }
    try {
      const header = this.readHeader();
      const automaticAccessAvailable =
        Boolean(header.safeStorageWrappedKey) &&
        this.protector.isAvailable() &&
        this.protector.isSecure();
      return {
        initialized: true,
        automaticAccessAvailable,
        keyStorageBackend: header.keyStorageBackend,
        warning: automaticAccessAvailable
          ? undefined
          : "系统安全密钥环不可用，自动完整备份已暂停；请配置系统密钥环后重新授权",
      };
    } catch (error) {
      return {
        initialized: true,
        automaticAccessAvailable: false,
        keyStorageBackend: null,
        warning: error instanceof Error ? error.message : String(error),
      };
    }
  }

  initialize(password: string): BackupRepositoryStatus {
    if (fs.existsSync(this.headerPath())) {
      throw new Error("备份仓库已经初始化");
    }
    const key = createRepositoryKey();
    const canUseAutomaticAccess =
      this.protector.isAvailable() && this.protector.isSecure();
    const header: RepositoryHeader = {
      kind: "guizhi-backup-repository",
      version: REPOSITORY_VERSION,
      createdAt: Date.now(),
      passwordWrappedKey: wrapKeyWithPassword(key, password),
      safeStorageWrappedKey: canUseAutomaticAccess
        ? this.protector.wrap(key).toString("base64")
        : null,
      keyStorageBackend: canUseAutomaticAccess ? this.protector.backend : null,
    };
    fs.mkdirSync(this.objectsDir(), { recursive: true });
    fs.mkdirSync(this.snapshotsDir(), { recursive: true });
    atomicWrite(this.headerPath(), JSON.stringify(header, null, 2));
    this.cachedKey = key;
    return this.status();
  }

  unlockWithPassword(password: string): void {
    this.cachedKey = unwrapKeyWithPassword(
      this.readHeader().passwordWrappedKey,
      password,
    );
  }

  unlockAutomatically(): void {
    const header = this.readHeader();
    if (
      !header.safeStorageWrappedKey ||
      !this.protector.isAvailable() ||
      !this.protector.isSecure()
    ) {
      throw new Error("系统安全密钥环不可用，无法执行无人值守完整备份");
    }
    this.cachedKey = this.protector.unwrap(
      Buffer.from(header.safeStorageWrappedKey, "base64"),
    );
  }

  changePassword(currentPassword: string, nextPassword: string): void {
    const header = this.readHeader();
    const key = unwrapKeyWithPassword(header.passwordWrappedKey, currentPassword);
    const next: RepositoryHeader = {
      ...header,
      passwordWrappedKey: wrapKeyWithPassword(key, nextPassword),
    };
    atomicWrite(this.headerPath(), JSON.stringify(next, null, 2));
    this.cachedKey = key;
  }

  createSnapshot(input: SnapshotInput): RepositorySnapshotResult {
    try {
      if (input.request?.recoveryPassword) {
        this.unlockWithPassword(input.request.recoveryPassword);
      }
      const key = this.requireKey();
      const snapshotId = `${Date.now()}-${randomUUID()}`;
      const workDir = fs.mkdtempSync(
        path.join(this.paths.repositoryDir, ".snapshot-work-"),
      );
      try {
        const snapshotDbPath = path.join(workDir, "knowledge.db");
        input.db.run("VACUUM INTO ?", snapshotDbPath);
        const collected = this.collectSnapshotEntries(
          snapshotDbPath,
          input.request?.rendererSettings,
          key,
        );
        const manifest: RepositorySnapshotManifest = {
          kind: "guizhi-repository-snapshot",
          version: REPOSITORY_VERSION,
          id: snapshotId,
          createdAt: Date.now(),
          appVersion: input.appVersion,
          schemaVersion: collected.schemaVersion,
          entries: collected.entries,
          summary: {
            itemCount: collected.itemCount,
            assetCount: collected.entries.filter((entry) => entry.category === "media").length,
            configDomains: collected.configDomains,
          },
        };
        const manifestPlain = Buffer.from(JSON.stringify(manifest), "utf8");
        const manifestEncrypted = encryptBuffer(gzipSync(manifestPlain), key);
        const snapshotDir = path.join(this.snapshotsDir(), snapshotId);
        fs.mkdirSync(snapshotDir, { recursive: true });
        atomicWrite(path.join(snapshotDir, "manifest.enc"), manifestEncrypted);
        const sizeBytes =
          manifestEncrypted.length +
          collected.entries.reduce((total, entry) => total + entry.storedBytes, 0);
        const header: SnapshotHeader = {
          kind: "guizhi-repository-snapshot-header",
          version: REPOSITORY_VERSION,
          id: snapshotId,
          createdAt: manifest.createdAt,
          appVersion: input.appVersion,
          schemaVersion: manifest.schemaVersion,
          itemCount: manifest.summary.itemCount,
          assetCount: manifest.summary.assetCount,
          configDomains: manifest.summary.configDomains,
          manifestSha256: sha256(manifestEncrypted),
          sizeBytes,
          backupKind: input.kind ?? "manual",
        };
        atomicWrite(
          path.join(snapshotDir, "header.json"),
          JSON.stringify(header, null, 2),
        );
        return {
          success: true,
          snapshot: this.snapshotHeaderToInfo(header),
          reusedObjects: collected.reusedObjects,
          createdObjects: collected.createdObjects,
        };
      } finally {
        fs.rmSync(workDir, { recursive: true, force: true });
      }
    } catch (error) {
      return {
        success: false,
        error: error instanceof Error ? error.message : String(error),
      };
    }
  }

  listSnapshots(): BackupFileInfo[] {
    if (!fs.existsSync(this.snapshotsDir())) return [];
    const snapshots: BackupFileInfo[] = [];
    for (const entry of fs.readdirSync(this.snapshotsDir(), { withFileTypes: true })) {
      if (!entry.isDirectory()) continue;
      try {
        const header = readJson<SnapshotHeader>(
          path.join(this.snapshotsDir(), entry.name, "header.json"),
        );
        if (header.kind === "guizhi-repository-snapshot-header") {
          snapshots.push(this.snapshotHeaderToInfo(header));
        }
      } catch {
        // 半成品/损坏目录不冒充可恢复快照；预览诊断由显式文件入口处理。
      }
    }
    return snapshots.sort((left, right) => right.createdAt - left.createdAt);
  }

  readManifest(snapshotId: string, password?: string): RepositorySnapshotManifest {
    if (password) this.unlockWithPassword(password);
    const key = this.requireKey();
    const snapshotDir = this.resolveSnapshotDir(snapshotId);
    const header = readJson<SnapshotHeader>(path.join(snapshotDir, "header.json"));
    const encrypted = fs.readFileSync(path.join(snapshotDir, "manifest.enc"));
    if (sha256(encrypted) !== header.manifestSha256) {
      throw new Error("快照 manifest 校验失败");
    }
    const manifest = JSON.parse(
      gunzipSync(decryptBuffer(encrypted, key)).toString("utf8"),
    ) as RepositorySnapshotManifest;
    if (manifest.id !== snapshotId || manifest.kind !== "guizhi-repository-snapshot") {
      throw new Error("快照 manifest 与目录不匹配");
    }
    return manifest;
  }

  readEntry(entry: RepositorySnapshotEntry): Buffer {
    const key = this.requireKey();
    const objectPath = path.join(this.objectsDir(), entry.sha256);
    const decrypted = decryptBuffer(fs.readFileSync(objectPath), key);
    const plain = entry.compression === "gzip" ? gunzipSync(decrypted) : decrypted;
    if (plain.length !== entry.sizeBytes || sha256(plain) !== entry.sha256) {
      throw new Error(`对象内容校验失败: ${entry.logicalPath}`);
    }
    return plain;
  }

  deleteSnapshot(snapshotId: string, password?: string): number {
    if (password) this.unlockWithPassword(password);
    const snapshotDir = this.resolveSnapshotDir(snapshotId);
    fs.rmSync(snapshotDir, { recursive: true, force: true });
    return this.garbageCollect();
  }

  async exportPortable(
    snapshotId: string,
    destinationPath: string,
    password?: string,
  ): Promise<void> {
    const manifest = this.readManifest(snapshotId, password);
    const snapshotDir = this.resolveSnapshotDir(snapshotId);
    const header = this.readHeader();
    // safeStorage 包装与当前操作系统账户绑定，便携包只携带恢复口令包装。
    const portableRepositoryHeader: RepositoryHeader = {
      ...header,
      safeStorageWrappedKey: null,
      keyStorageBackend: null,
    };
    const sources: PortableSource[] = [
      {
        archivePath: "repository-header.json",
        data: Buffer.from(JSON.stringify(portableRepositoryHeader, null, 2)),
      },
      {
        archivePath: "snapshot/header.json",
        filePath: path.join(snapshotDir, "header.json"),
      },
      {
        archivePath: "snapshot/manifest.enc",
        filePath: path.join(snapshotDir, "manifest.enc"),
      },
      ...[...new Set(manifest.entries.map((entry) => entry.sha256))].map(
        (hash): PortableSource => ({
          archivePath: `objects/${hash}`,
          filePath: path.join(this.objectsDir(), hash),
        }),
      ),
    ];
    await writePortableBackup(destinationPath, sources);
  }

  /**
   * 导入便携备份（`exportPortable` 的逆操作）：把包里的快照与加密对象并入本机备份仓库，不直接改动数据库。
   * 导入后在列表里出现一条快照，仍要走「预检并恢复」才会替换当前数据。
   *
   * - 必须提供包对应的恢复口令：用它解开包内仓库密钥，并校验快照清单可解密、引用的对象齐全。
   * - 本机还没有仓库，或仓库里没有任何快照：改用包里的仓库头（换机的主要场景）。恢复口令随之变成包的口令；
   *   能用系统密钥环时重新包装，使无人值守备份继续可用。本机原有的孤立对象用旧密钥加密，会先清掉，
   *   否则同哈希对象会与导入对象混在一起。
   * - 本机已有快照：只有口令能解开本机仓库头且密钥相同（同一仓库的另一份拷贝）时才并入，其余情况拒绝——
   *   对象按明文哈希命名，不同密钥的对象无法安全共存。
   * - 提交顺序：先换仓库头，再放对象，最后一步把快照目录整体改名；快照出现之前，仓库不会列出半成品。
   *   从状态检查到提交之间没有 await，不会与同进程的其它备份操作交错。
   * - 导入的快照一律记为手动：自动快照有保留数量上限，导入的唯一副本不能被下一轮自动备份清掉。
   */
  async importPortable(
    archivePath: string,
    password: string,
  ): Promise<PortableBackupImportResult> {
    fs.mkdirSync(this.paths.repositoryDir, { recursive: true });
    const stage = fs.mkdtempSync(path.join(this.paths.repositoryDir, ".import-work-"));
    try {
      await extractPortableArchive(archivePath, stage);
      for (const required of ["repository-header.json", "snapshot/header.json", "snapshot/manifest.enc"]) {
        if (!fs.existsSync(path.join(stage, ...required.split("/")))) {
          throw new Error(`便携备份缺少必要文件：${required}`);
        }
      }
      const header = readJson<RepositoryHeader>(path.join(stage, "repository-header.json"));
      validateHeader(header);
      const key = unwrapKeyWithPassword(header.passwordWrappedKey, password);
      const snapshotHeader = readJson<SnapshotHeader>(path.join(stage, "snapshot", "header.json"));
      if (
        snapshotHeader?.kind !== "guizhi-repository-snapshot-header" ||
        typeof snapshotHeader.id !== "string" ||
        !/^[A-Za-z0-9-]+$/.test(snapshotHeader.id)
      ) {
        throw new Error("便携备份的快照头不合法");
      }
      const manifestEncrypted = fs.readFileSync(path.join(stage, "snapshot", "manifest.enc"));
      if (sha256(manifestEncrypted) !== snapshotHeader.manifestSha256) {
        throw new Error("便携备份的快照清单校验失败");
      }
      let manifest: RepositorySnapshotManifest;
      try {
        manifest = JSON.parse(
          gunzipSync(decryptBuffer(manifestEncrypted, key)).toString("utf8"),
        ) as RepositorySnapshotManifest;
      } catch {
        throw new Error("便携备份的快照清单无法解密，文件可能已损坏");
      }
      if (manifest.kind !== "guizhi-repository-snapshot" || manifest.id !== snapshotHeader.id) {
        throw new Error("便携备份的快照清单与快照头不匹配");
      }
      const needed = [...new Set(manifest.entries.map((entry) => entry.sha256))];
      const missing = needed.filter((hash) => !fs.existsSync(path.join(stage, "objects", hash)));
      if (missing.length > 0) {
        throw new Error(`便携备份缺少 ${missing.length} 个数据对象，文件可能不完整`);
      }

      // ── 以下到提交完成全部同步 ──
      const hasLocalRepository = fs.existsSync(this.headerPath());
      let sameRepository = false;
      if (hasLocalRepository) {
        const local = this.readHeader();
        try {
          sameRepository = unwrapKeyWithPassword(local.passwordWrappedKey, password).equals(key);
        } catch {
          sameRepository = false;
        }
        if (!sameRepository && this.listSnapshots().length > 0) {
          throw new Error(
            "本机备份仓库已有完整快照，且与这份便携备份不属于同一个仓库（恢复口令或密钥不同），无法合并。" +
              "请先在本机删除这些快照，或换一台没有完整快照的机器导入。",
          );
        }
      }
      const snapshotDir = path.join(this.snapshotsDir(), snapshotHeader.id);
      if (fs.existsSync(snapshotDir)) {
        if (!sameRepository) throw new Error("本机已存在同名快照目录，无法导入");
        this.cachedKey = key;
        return {
          success: true,
          alreadyExisted: true,
          snapshot: this.snapshotHeaderToInfo(snapshotHeader),
        };
      }

      if (!sameRepository) {
        fs.rmSync(this.objectsDir(), { recursive: true, force: true });
        const canUseAutomaticAccess = this.protector.isAvailable() && this.protector.isSecure();
        const nextHeader: RepositoryHeader = {
          ...header,
          safeStorageWrappedKey: canUseAutomaticAccess
            ? this.protector.wrap(key).toString("base64")
            : null,
          keyStorageBackend: canUseAutomaticAccess ? this.protector.backend : null,
        };
        fs.mkdirSync(this.objectsDir(), { recursive: true });
        fs.mkdirSync(this.snapshotsDir(), { recursive: true });
        atomicWrite(this.headerPath(), JSON.stringify(nextHeader, null, 2));
      }
      this.cachedKey = key;
      fs.mkdirSync(this.objectsDir(), { recursive: true });
      fs.mkdirSync(this.snapshotsDir(), { recursive: true });
      for (const hash of needed) {
        const target = path.join(this.objectsDir(), hash);
        if (!fs.existsSync(target)) fs.renameSync(path.join(stage, "objects", hash), target);
      }
      const importedHeader: SnapshotHeader = { ...snapshotHeader, backupKind: "manual" };
      fs.writeFileSync(
        path.join(stage, "snapshot", "header.json"),
        JSON.stringify(importedHeader, null, 2),
      );
      fs.renameSync(path.join(stage, "snapshot"), snapshotDir);
      return {
        success: true,
        replacedRepository: hasLocalRepository && !sameRepository,
        snapshot: this.snapshotHeaderToInfo(importedHeader),
      };
    } catch (error) {
      return { success: false, error: error instanceof Error ? error.message : String(error) };
    } finally {
      fs.rmSync(stage, { recursive: true, force: true });
    }
  }
  garbageCollect(): number {
    const referenced = new Set<string>();
    for (const snapshot of this.listSnapshots()) {
      for (const entry of this.readManifest(snapshot.fileName).entries) {
        referenced.add(entry.sha256);
      }
    }
    if (!fs.existsSync(this.objectsDir())) return 0;
    let removed = 0;
    for (const name of fs.readdirSync(this.objectsDir())) {
      if (/^[a-f0-9]{64}$/.test(name) && !referenced.has(name)) {
        fs.rmSync(path.join(this.objectsDir(), name), { force: true });
        removed += 1;
      }
    }
    return removed;
  }

  pruneAutoSnapshots(keepCount: number): number {
    const excess = this.listSnapshots()
      .filter((snapshot) => snapshot.kind === "auto")
      .slice(Math.max(1, Math.round(keepCount)));
    for (const snapshot of excess) {
      fs.rmSync(this.resolveSnapshotDir(snapshot.fileName), {
        recursive: true,
        force: true,
      });
    }
    if (excess.length > 0) this.garbageCollect();
    return excess.length;
  }

  private collectSnapshotEntries(
    snapshotDbPath: string,
    rendererSettings: Record<string, unknown> | undefined,
    key: Buffer,
  ): {
    entries: RepositorySnapshotEntry[];
    itemCount: number;
    schemaVersion: number;
    configDomains: string[];
    reusedObjects: number;
    createdObjects: number;
  } {
    const entries: RepositorySnapshotEntry[] = [];
    let reusedObjects = 0;
    let createdObjects = 0;
    const add = (
      logicalPath: string,
      category: SnapshotEntryCategory,
      plain: Buffer,
    ) => {
      const result = this.storeObject(
        plain,
        category === "media" ? "none" : "gzip",
        key,
      );
      entries.push({ logicalPath, category, ...result.entry });
      if (result.reused) reusedObjects += 1;
      else createdObjects += 1;
    };

    const snapshotDb = new Database(snapshotDbPath, { readOnly: true });
    let itemCount: number;
    let schemaVersion: number;
    try {
      const quickCheck = snapshotDb.pragma("quick_check") as Array<Record<string, unknown>>;
      if (quickCheck.length !== 1 || Object.values(quickCheck[0])[0] !== "ok") {
        throw new Error("数据库快照 quick_check 未通过");
      }
      schemaVersion = getSchemaVersion(snapshotDb);
      if (schemaVersion > SCHEMA_VERSION) {
        throw new Error(`数据库结构 v${schemaVersion} 高于当前支持的 v${SCHEMA_VERSION}`);
      }
      const count = snapshotDb.get("SELECT COUNT(*) AS count FROM knowledge_items") as {
        count: number;
      };
      itemCount = count.count;
      const contents = snapshotDb.all("SELECT content FROM knowledge_items UNION ALL SELECT content FROM source_capture_revisions") as Array<{
        content: string;
      }>;
      const images = new Set<string>();
      const videos = new Set<string>();
      for (const row of contents) {
        extractLocalAssetRefs(row.content, "local-image").forEach((name) => images.add(name));
        extractLocalAssetRefs(row.content, "local-video").forEach((name) => videos.add(name));
      }
      for (const row of snapshotDb.all("SELECT payload FROM web_source_versions") as {payload:string}[]) {
        const version = JSON.parse(row.payload);
        extractLocalAssetRefs(version.markdown ?? "", "local-image").forEach(name => images.add(name));
        for (const asset of version.snapshot?.assets ?? []) {
          if (!/^wechat-[a-f0-9]{64}\.(png|jpg|gif|webp)$/.test(asset.fileName)) throw new Error("快照资源路径无效");
          images.add(asset.fileName);
        }
      }
      for (const row of snapshotDb.all("SELECT payload FROM crawl_pages") as {payload:string}[]) {
        for (const asset of JSON.parse(row.payload).result?.snapshot?.assets ?? []) {
          if (!/^wechat-[a-f0-9]{64}\.(png|jpg|gif|webp)$/.test(asset.fileName)) throw new Error("快照资源路径无效");
          images.add(asset.fileName);
        }
      }
      const themedAssets = inspectThemedReadingBackup(snapshotDb);
      for (const asset of themedAssets) images.add(asset.fileName);
      const assets = [
        ...[...images].map((name) => ({
          name,
          folder: "images" as const,
          filePath: path.join(this.paths.imagesDir, name),
        })),
        ...[...videos].map((name) => ({
          name,
          folder: "videos" as const,
          filePath: path.join(this.paths.videosDir, name),
        })),
      ];
      const releaseAssets = retainAssetFiles(assets.map(asset => asset.name));
      try {
        verifyThemedReadingBackupAssets(themedAssets, this.paths.imagesDir);
        for (const { name, folder, filePath } of assets) {
          if (!fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) {
            throw new Error(`引用媒体不存在，完整备份已中止: ${name}`);
          }
          add(`data/assets/${folder}/${name}`, "media", fs.readFileSync(filePath));
        }
      } finally {
        releaseAssets();
      }
    } finally {
      snapshotDb.close();
    }

    add("data/knowledge.db", "database", fs.readFileSync(snapshotDbPath));
    const configDomains: string[] = [];
    for (const name of CONFIG_FILE_NAMES) {
      const filePath = path.join(this.paths.configDir, name);
      if (!fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) continue;
      add(`config/${name}`, "config", fs.readFileSync(filePath));
      configDomains.push(name.replace(/\.json$/, ""));
    }
    if (rendererSettings) {
      add(
        "config/renderer-settings.json",
        "renderer-settings",
        Buffer.from(JSON.stringify(rendererSettings), "utf8"),
      );
      configDomains.push("interface-preferences");
    }
    return {
      entries,
      itemCount,
      schemaVersion,
      configDomains,
      reusedObjects,
      createdObjects,
    };
  }

  private storeObject(
    plain: Buffer,
    compression: "gzip" | "none",
    key: Buffer,
  ): {
    entry: Omit<RepositorySnapshotEntry, "logicalPath" | "category">;
    reused: boolean;
  } {
    const objectHash = sha256(plain);
    const objectPath = path.join(this.objectsDir(), objectHash);
    if (fs.existsSync(objectPath)) {
      return {
        entry: {
          sha256: objectHash,
          sizeBytes: plain.length,
          storedBytes: fs.statSync(objectPath).size,
          compression,
        },
        reused: true,
      };
    }
    const prepared = compression === "gzip" ? gzipSync(plain) : plain;
    const encrypted = encryptBuffer(prepared, key);
    atomicWrite(objectPath, encrypted);
    return {
      entry: {
        sha256: objectHash,
        sizeBytes: plain.length,
        storedBytes: encrypted.length,
        compression,
      },
      reused: false,
    };
  }

  private requireKey(): Buffer {
    if (!this.cachedKey) this.unlockAutomatically();
    if (!this.cachedKey || this.cachedKey.length !== 32) {
      throw new Error("无法解锁备份仓库");
    }
    return this.cachedKey;
  }

  private readHeader(): RepositoryHeader {
    const header = readJson<RepositoryHeader>(this.headerPath());
    validateHeader(header);
    return header;
  }

  private snapshotHeaderToInfo(header: SnapshotHeader): BackupFileInfo {
    return {
      fileName: header.id,
      path: path.join(this.snapshotsDir(), header.id),
      kind: header.backupKind ?? "auto",
      format: "repository-snapshot",
      sizeBytes: header.sizeBytes,
      createdAt: header.createdAt,
      encrypted: true,
      validation: "unchecked",
      summary: {
        itemCount: header.itemCount,
        assetCount: header.assetCount,
        configDomains: header.configDomains,
        schemaVersion: header.schemaVersion,
        appVersion: header.appVersion,
      },
    };
  }

  private resolveSnapshotDir(snapshotId: string): string {
    if (!/^[A-Za-z0-9-]+$/.test(snapshotId)) {
      throw new Error("快照 id 不合法");
    }
    const snapshotDir = path.join(this.snapshotsDir(), snapshotId);
    if (!fs.existsSync(snapshotDir)) throw new Error("找不到指定快照");
    return snapshotDir;
  }

  private headerPath(): string {
    return path.join(this.paths.repositoryDir, HEADER_FILE);
  }

  private objectsDir(): string {
    return path.join(this.paths.repositoryDir, "objects");
  }

  private snapshotsDir(): string {
    return path.join(this.paths.repositoryDir, "snapshots");
  }
}
