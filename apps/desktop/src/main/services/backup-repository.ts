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
      const contents = snapshotDb.all("SELECT content FROM knowledge_items") as Array<{
        content: string;
      }>;
      const images = new Set<string>();
      const videos = new Set<string>();
      for (const row of contents) {
        extractLocalAssetRefs(row.content, "local-image").forEach((name) => images.add(name));
        extractLocalAssetRefs(row.content, "local-video").forEach((name) => videos.add(name));
      }
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
      for (const { name, folder, filePath } of assets) {
        if (!fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) {
          throw new Error(`引用媒体不存在，完整备份已中止: ${name}`);
        }
        add(`data/assets/${folder}/${name}`, "media", fs.readFileSync(filePath));
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
