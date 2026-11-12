import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { zipSync } from "fflate";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { BackupRepository } from "../../../src/main/services/backup-repository";
import { previewRepositoryRestore } from "../../../src/main/services/backup-repository-preview";
import { crc32Update, isAllowedPortableEntry } from "../../../src/main/services/backup-portable-reader";
import type { RepositoryKeyProtector } from "../../../src/main/services/backup-repository-crypto";

const PASSWORD = "correct horse battery";

const protector: RepositoryKeyProtector = {
  backend: "test-keychain",
  isAvailable: () => true,
  isSecure: () => true,
  wrap: (key) => Buffer.from(`safe:${key.toString("base64")}`),
  unwrap: (wrapped) => Buffer.from(wrapped.toString("utf8").replace(/^safe:/, ""), "base64"),
};

let root: string;
let db: DatabaseAdapter.Database;

/** 在 root 下的某个“机器目录”里创建仓库；两台机器用不同的 machine 名模拟。 */
function repositoryOf(machine: string): BackupRepository {
  const base = path.join(root, machine);
  return new BackupRepository(
    {
      repositoryDir: path.join(base, "backups", "repository"),
      databasePath: path.join(base, "data", "knowledge.db"),
      configDir: path.join(base, "config"),
      imagesDir: path.join(base, "data", "assets", "images"),
      videosDir: path.join(base, "data", "assets", "videos"),
    },
    protector,
  );
}

const sourceRepository = () => repositoryOf("source");
const targetRepository = () => repositoryOf("target");
const targetDir = () => path.join(root, "target", "backups", "repository");

/** 源机器创建一条快照并导出便携包，返回包路径与快照 id。 */
async function exportFromSource(repository: BackupRepository, name: string) {
  const result = repository.createSnapshot({ db, appVersion: "0.29.0" });
  expect(result.success).toBe(true);
  const archive = path.join(root, `${name}.guizhi-backup`);
  await repository.exportPortable(result.snapshot!.fileName, archive);
  return { archive, id: result.snapshot!.fileName };
}

beforeEach(() => {
  root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-portable-import-"));
  const dataDir = path.join(root, "source", "data");
  fs.mkdirSync(path.join(dataDir, "assets", "images"), { recursive: true });
  fs.mkdirSync(path.join(dataDir, "assets", "videos"), { recursive: true });
  fs.mkdirSync(path.join(root, "source", "config"), { recursive: true });
  db = new DatabaseAdapter(path.join(dataDir, "knowledge.db"));
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  const now = Date.now();
  db.run(
    "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
    "item-1",
    "带媒体的条目",
    "![图](local-image://a.png)",
    now,
    now,
  );
  fs.writeFileSync(path.join(dataDir, "assets", "images", "a.png"), "image-bytes");
  fs.writeFileSync(path.join(root, "source", "config", "ai-models.json"), "{}");
});

afterEach(() => {
  try {
    db.close();
  } catch {
    // 已关闭
  }
  fs.rmSync(root, { recursive: true, force: true });
});

describe("导入便携备份", () => {
  it("空机器导入：快照入列、记为手动，预检可用便携口令通过", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive, id } = await exportFromSource(source, "p1");

    const target = targetRepository();
    const result = await target.importPortable(archive, PASSWORD);

    expect(result).toMatchObject({ success: true });
    expect(result.replacedRepository).toBeFalsy();
    expect(result.alreadyExisted).toBeFalsy();
    expect(target.status()).toMatchObject({ initialized: true, automaticAccessAvailable: true });
    const listed = target.listSnapshots();
    expect(listed.map((snapshot) => snapshot.fileName)).toEqual([id]);
    expect(listed[0].kind).toBe("manual");
    const preview = previewRepositoryRestore(targetRepository(), id, PASSWORD);
    expect(preview.success).toBe(true);
    expect(preview.damagedFiles ?? []).toEqual([]);
    expect(fs.readdirSync(targetDir()).filter((name) => name.startsWith(".import-work-"))).toEqual([]);
  });

  it("口令错误时拒绝，目标仓库保持不变", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive } = await exportFromSource(source, "p1");

    const result = await targetRepository().importPortable(archive, "wrong password value");

    expect(result.success).toBe(false);
    expect(fs.existsSync(path.join(targetDir(), "repository-header.json"))).toBe(false);
    expect(fs.existsSync(path.join(targetDir(), "snapshots"))).toBe(false);
  });

  it("同一仓库的另一份拷贝：重复导入不新增，后续快照可并入并复用对象", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const first = await exportFromSource(source, "p1");
    const target = targetRepository();
    expect((await target.importPortable(first.archive, PASSWORD)).success).toBe(true);

    const again = await target.importPortable(first.archive, PASSWORD);
    expect(again).toMatchObject({ success: true, alreadyExisted: true });
    expect(target.listSnapshots()).toHaveLength(1);

    const second = await exportFromSource(source, "p2");
    const merged = await target.importPortable(second.archive, PASSWORD);
    expect(merged.success).toBe(true);
    expect(target.listSnapshots().map((snapshot) => snapshot.fileName).sort()).toEqual(
      [first.id, second.id].sort(),
    );
    expect(previewRepositoryRestore(targetRepository(), second.id, PASSWORD).success).toBe(true);
  });

  it("本机已有无关仓库的快照时拒绝合并，原数据不动", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive } = await exportFromSource(source, "p1");

    const target = targetRepository();
    target.initialize("another long passphrase");
    // 本机要能自己产出快照：给目标机器准备同样结构的数据与配置
    fs.cpSync(path.join(root, "source", "data"), path.join(root, "target", "data"), { recursive: true });
    fs.cpSync(path.join(root, "source", "config"), path.join(root, "target", "config"), { recursive: true });
    const local = target.createSnapshot({ db, appVersion: "0.29.0" });
    expect(local.error ?? "").toBe("");
    expect(local.success).toBe(true);

    const result = await target.importPortable(archive, PASSWORD);

    expect(result.success).toBe(false);
    expect(result.error).toContain("无法合并");
    expect(target.listSnapshots().map((snapshot) => snapshot.fileName)).toEqual([local.snapshot!.fileName]);
    expect(previewRepositoryRestore(targetRepository(), local.snapshot!.fileName, "another long passphrase").success).toBe(true);
  });

  it("本机仓库没有快照时换用便携包的仓库，并清掉旧密钥的孤立对象", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive, id } = await exportFromSource(source, "p1");

    const target = targetRepository();
    target.initialize("another long passphrase");
    fs.mkdirSync(path.join(targetDir(), "objects"), { recursive: true });
    const stale = path.join(targetDir(), "objects", "f".repeat(64));
    fs.writeFileSync(stale, "orphan");

    const result = await target.importPortable(archive, PASSWORD);

    expect(result).toMatchObject({ success: true, replacedRepository: true });
    expect(fs.existsSync(stale)).toBe(false);
    expect(previewRepositoryRestore(targetRepository(), id, PASSWORD).success).toBe(true);
    expect(previewRepositoryRestore(targetRepository(), id, "another long passphrase").success).toBe(false);
  });

  it("对象被篡改：校验和不符，整个导入回滚且不留半成品", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive } = await exportFromSource(source, "p1");
    const bytes = fs.readFileSync(archive);
    // 存储模式的 Zip：对象密文原样位于包内，定位源仓库里的一个对象并翻转其中一个字节
    const objectsDir = path.join(root, "source", "backups", "repository", "objects");
    const sample = fs.readFileSync(path.join(objectsDir, fs.readdirSync(objectsDir)[0]));
    const at = bytes.indexOf(sample);
    expect(at).toBeGreaterThan(0);
    const tampered = Buffer.from(bytes);
    tampered[at + Math.floor(sample.length / 2)] ^= 0xff;
    const bad = path.join(root, "bad.guizhi-backup");
    fs.writeFileSync(bad, tampered);

    const target = targetRepository();
    const result = await target.importPortable(bad, PASSWORD);

    expect(result.success).toBe(false);
    expect(fs.existsSync(path.join(targetDir(), "repository-header.json"))).toBe(false);
    const leftovers = fs.existsSync(targetDir()) ? fs.readdirSync(targetDir()) : [];
    expect(leftovers.filter((name) => name.startsWith(".import-work-"))).toEqual([]);
  });

  it("拒绝越权路径、压缩条目与缺少对象的包", async () => {
    const source = sourceRepository();
    source.initialize(PASSWORD);
    const { archive } = await exportFromSource(source, "p1");
    const target = targetRepository();

    const traversal = path.join(root, "traversal.guizhi-backup");
    fs.writeFileSync(traversal, zipSync({ "../evil.txt": new Uint8Array([1]) }, { level: 0 }));
    expect((await target.importPortable(traversal, PASSWORD)).success).toBe(false);
    expect(fs.existsSync(path.join(root, "target", "backups", "evil.txt"))).toBe(false);

    const deflated = path.join(root, "deflated.guizhi-backup");
    fs.writeFileSync(deflated, zipSync({ "repository-header.json": new Uint8Array(500).fill(65) }));
    expect((await target.importPortable(deflated, PASSWORD)).success).toBe(false);

    const { unzipSync } = await import("fflate");
    const entries = unzipSync(fs.readFileSync(archive));
    const withoutObjects: Record<string, Uint8Array> = {};
    for (const [name, data] of Object.entries(entries)) {
      if (!name.startsWith("objects/")) withoutObjects[name] = data;
    }
    const incomplete = path.join(root, "incomplete.guizhi-backup");
    fs.writeFileSync(incomplete, zipSync(withoutObjects, { level: 0 }));
    const result = await target.importPortable(incomplete, PASSWORD);
    expect(result.success).toBe(false);
    expect(result.error).toContain("缺少");
    expect(fs.existsSync(path.join(targetDir(), "repository-header.json"))).toBe(false);
  });
});

describe("便携包读取辅助", () => {
  it("只允许约定的条目名", () => {
    expect(isAllowedPortableEntry("repository-header.json")).toBe(true);
    expect(isAllowedPortableEntry("snapshot/manifest.enc")).toBe(true);
    expect(isAllowedPortableEntry(`objects/${"a".repeat(64)}`)).toBe(true);
    expect(isAllowedPortableEntry("objects/../x")).toBe(false);
    expect(isAllowedPortableEntry("objects/ABC")).toBe(false);
    expect(isAllowedPortableEntry("/etc/passwd")).toBe(false);
  });

  it("CRC-32 与标准结果一致，且可分块累计", () => {
    const data = Buffer.from("123456789");
    expect(crc32Update(0, data)).toBe(0xcbf43926);
    expect(crc32Update(crc32Update(0, data.subarray(0, 4)), data.subarray(4))).toBe(0xcbf43926);
  });
});
