import { WebSourceDB } from "@guizhi/db";
import { createHash } from "node:crypto";
import type { WebCaptureResult } from "@guizhi/shared/types";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import DatabaseAdapter from "@guizhi/db/adapter";
import { SCHEMA_INDEXES, SCHEMA_TABLES } from "@guizhi/db/schema";
import { BackupRepository } from "../../../src/main/services/backup-repository";
import { previewRepositoryRestore } from "../../../src/main/services/backup-repository-preview";
import {
  applyPreparedRepositoryRestore,
  prepareRepositoryRestore,
} from "../../../src/main/services/backup-repository-restore";
import {
  decryptBuffer,
  encryptBuffer,
  type RepositoryKeyProtector,
} from "../../../src/main/services/backup-repository-crypto";

const protector: RepositoryKeyProtector = {
  backend: "test-keychain",
  isAvailable: () => true,
  isSecure: () => true,
  wrap: (key) => Buffer.from(`safe:${key.toString("base64")}`),
  unwrap: (wrapped) =>
    Buffer.from(wrapped.toString("utf8").replace(/^safe:/, ""), "base64"),
};

let workDir: string;
let db: DatabaseAdapter.Database;

function createRepository(): BackupRepository {
  return new BackupRepository(
    {
      repositoryDir: path.join(workDir, "backups", "repository"),
      databasePath: path.join(workDir, "data", "knowledge.db"),
      configDir: path.join(workDir, "config"),
      imagesDir: path.join(workDir, "data", "assets", "images"),
      videosDir: path.join(workDir, "data", "assets", "videos"),
    },
    protector,
  );
}

beforeEach(() => {
  workDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-repository-test-"));
  fs.mkdirSync(path.join(workDir, "data"), { recursive: true });
  db = new DatabaseAdapter(path.join(workDir, "data", "knowledge.db"));
  db.exec(SCHEMA_TABLES);
  db.exec(SCHEMA_INDEXES);
  const now = Date.now();
  db.run(
    "INSERT INTO knowledge_items (id, title, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
    "item-1",
    "带媒体的条目",
    "![图](local-image://import-same.png)\n\n[视频](local-video://import-same.mp4)",
    now,
    now,
  );
  fs.mkdirSync(path.join(workDir, "data", "assets", "images"), { recursive: true });
  fs.mkdirSync(path.join(workDir, "data", "assets", "videos"), { recursive: true });
  fs.writeFileSync(path.join(workDir, "data", "assets", "images", "import-same.png"), "image");
  fs.writeFileSync(path.join(workDir, "data", "assets", "videos", "import-same.mp4"), "video");
  fs.mkdirSync(path.join(workDir, "config"), { recursive: true });
  fs.writeFileSync(
    path.join(workDir, "config", "ai-models.json"),
    JSON.stringify({ apiKey: "secret" }),
  );
});

afterEach(() => {
  try {
    db.close();
  } catch {
    // ignore
  }
  fs.rmSync(workDir, { recursive: true, force: true });
});

describe("BackupRepository", () => {
  it("恢复口令至少 12 字符，并为自动备份写入安全存储包装", () => {
    const repository = createRepository();
    expect(() => repository.initialize("short")).toThrow("至少需要 12 个字符");

    const status = repository.initialize("correct horse battery");
    expect(status).toMatchObject({
      initialized: true,
      automaticAccessAvailable: true,
      keyStorageBackend: "test-keychain",
    });
  });

  it("从数据库快照解析引用媒体，并加密保存数据库、配置和界面偏好", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const result = repository.createSnapshot({
      db,
      appVersion: "0.20.0",
      request: { rendererSettings: { themeMode: "dark" } },
    });

    expect(result.success).toBe(true);
    expect(result.snapshot).toMatchObject({
      format: "repository-snapshot",
      encrypted: true,
      summary: { itemCount: 1, assetCount: 2, appVersion: "0.20.0" },
    });
    const manifest = repository.readManifest(result.snapshot!.fileName);
    expect(manifest.entries.map((entry) => entry.logicalPath)).toEqual(
      expect.arrayContaining([
        "data/knowledge.db",
        "data/assets/images/import-same.png",
        "data/assets/videos/import-same.mp4",
        "config/ai-models.json",
        "config/renderer-settings.json",
      ]),
    );
    const aiConfig = manifest.entries.find(
      (entry) => entry.logicalPath === "config/ai-models.json",
    )!;
    expect(repository.readEntry(aiConfig).toString("utf8")).toContain("secret");
  });

  it("多快照复用同一媒体对象，删除最后一个快照后 GC 对象", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const first = repository.createSnapshot({ db, appVersion: "0.20.0" });
    const second = repository.createSnapshot({ db, appVersion: "0.20.0" });

    expect(first.success).toBe(true);
    expect(second.reusedObjects).toBeGreaterThanOrEqual(3);
    const objectDir = path.join(workDir, "backups", "repository", "objects");
    const objectCount = fs.readdirSync(objectDir).length;
    expect(repository.deleteSnapshot(first.snapshot!.fileName)).toBe(0);
    expect(fs.readdirSync(objectDir)).toHaveLength(objectCount);
    expect(repository.deleteSnapshot(second.snapshot!.fileName)).toBe(objectCount);
    expect(fs.readdirSync(objectDir)).toHaveLength(0);
  });

  it("修改恢复口令只重包仓库密钥，不重写对象", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const snapshot = repository.createSnapshot({ db, appVersion: "0.20.0" });
    const objectDir = path.join(workDir, "backups", "repository", "objects");
    const before = fs.readdirSync(objectDir).map((name) => ({
      name,
      mtime: fs.statSync(path.join(objectDir, name)).mtimeMs,
    }));

    repository.changePassword("correct horse battery", "another secure password");
    const reopened = createRepository();
    expect(() => reopened.unlockWithPassword("correct horse battery")).toThrow();
    reopened.unlockWithPassword("another secure password");
    expect(reopened.readManifest(snapshot.snapshot!.fileName).id).toBe(
      snapshot.snapshot!.fileName,
    );
    expect(
      fs.readdirSync(objectDir).map((name) => ({
        name,
        mtime: fs.statSync(path.join(objectDir, name)).mtimeMs,
      })),
    ).toEqual(before);
  });

  it("引用媒体缺失时拒绝产出冒充完整的快照", () => {
    fs.rmSync(path.join(workDir, "data", "assets", "images", "import-same.png"));
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const result = repository.createSnapshot({ db, appVersion: "0.20.0" });

    expect(result).toMatchObject({ success: false });
    expect(result.error).toContain("引用媒体不存在");
    expect(repository.listSnapshots()).toHaveLength(0);
  });

  it("恢复预检能识别错误口令与被篡改的加密对象", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const result = repository.createSnapshot({ db, appVersion: "0.20.0" });
    const snapshotId = result.snapshot!.fileName;

    const lockedRepository = createRepository();
    expect(
      previewRepositoryRestore(
        lockedRepository,
        snapshotId,
        "wrong password value",
      ),
    ).toMatchObject({ success: false, error: "恢复口令不正确或仓库头已损坏" });

    const manifest = repository.readManifest(snapshotId);
    const media = manifest.entries.find((entry) => entry.category === "media")!;
    const objectPath = path.join(
      workDir,
      "backups",
      "repository",
      "objects",
      media.sha256,
    );
    const encrypted = fs.readFileSync(objectPath);
    encrypted[encrypted.length - 1] ^= 1;
    fs.writeFileSync(objectPath, encrypted);
    const preview = previewRepositoryRestore(repository, snapshotId);
    expect(preview).toMatchObject({
      success: false,
      snapshot: { validation: "invalid" },
    });
    expect(preview.damagedFiles).toContain(media.logicalPath);
  });

  it("便携导出只带当前快照所需对象，并能以流式 Zip 完成", async () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const result = repository.createSnapshot({ db, appVersion: "0.20.0" });
    const destination = path.join(workDir, "portable.guizhi-backup");

    await repository.exportPortable(result.snapshot!.fileName, destination);
    expect(fs.statSync(destination).size).toBeGreaterThan(0);
    const { unzipSync } = await import("fflate");
    const entries = unzipSync(fs.readFileSync(destination));
    const names = Object.keys(entries);
    expect(names).toEqual(
      expect.arrayContaining([
        "repository-header.json",
        "snapshot/header.json",
        "snapshot/manifest.enc",
      ]),
    );
    expect(names.filter((name) => name.startsWith("objects/"))).toHaveLength(
      result.createdObjects!,
    );
    const portableHeader = JSON.parse(
      Buffer.from(entries["repository-header.json"]).toString("utf8"),
    );
    expect(portableHeader.safeStorageWrappedKey).toBeNull();
  });

  it("完整恢复替换数据库、媒体和配置，同时保留机器绑定设置", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const snapshot = repository.createSnapshot({
      db,
      appVersion: "0.20.0",
      request: {
        rendererSettings: {
          themeMode: "dark",
          dataPath: "D:/old-machine",
          ytDlpPath: "D:/old-machine/yt-dlp.exe",
        },
      },
    });
    db.run(
      "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)",
      "ytDlpPath",
      JSON.stringify("D:/current-machine/yt-dlp.exe"),
    );
    db.run("UPDATE knowledge_items SET title = ? WHERE id = ?", "恢复前改坏", "item-1");
    fs.writeFileSync(
      path.join(workDir, "data", "assets", "images", "import-same.png"),
      "changed-image",
    );
    fs.writeFileSync(
      path.join(workDir, "config", "ai-models.json"),
      JSON.stringify({ apiKey: "changed" }),
    );
    const targets = {
      databasePath: path.join(workDir, "data", "knowledge.db"),
      imagesDir: path.join(workDir, "data", "assets", "images"),
      videosDir: path.join(workDir, "data", "assets", "videos"),
      configDir: path.join(workDir, "config"),
    };
    const prepared = prepareRepositoryRestore({
      repository,
      snapshotId: snapshot.snapshot!.fileName,
      liveDb: db,
      targets,
      currentRendererSettings: {
        dataPath: "D:/current-machine",
        ytDlpPath: "D:/current-machine/yt-dlp.exe",
      },
    });
    db.close();
    applyPreparedRepositoryRestore(prepared, targets);

    db = new DatabaseAdapter(targets.databasePath);
    expect(
      (db.get("SELECT title FROM knowledge_items WHERE id = ?", "item-1") as {
        title: string;
      }).title,
    ).toBe("带媒体的条目");
    expect(
      JSON.parse(
        (
          db.get("SELECT value FROM settings WHERE key = ?", "ytDlpPath") as {
            value: string;
          }
        ).value,
      ),
    ).toBe("D:/current-machine/yt-dlp.exe");
    expect(fs.readFileSync(path.join(targets.imagesDir, "import-same.png"), "utf8")).toBe(
      "image",
    );
    expect(fs.readFileSync(path.join(targets.configDir, "ai-models.json"), "utf8")).toContain(
      "secret",
    );
    const pending = JSON.parse(
      fs.readFileSync(
        path.join(targets.configDir, "pending-renderer-settings.json"),
        "utf8",
      ),
    );
    expect(pending).toMatchObject({
      themeMode: "dark",
      dataPath: "D:/current-machine",
      ytDlpPath: "D:/current-machine/yt-dlp.exe",
    });
  });

  it("多资源交换中途失败会逆序恢复原数据库与媒体", () => {
    const repository = createRepository();
    repository.initialize("correct horse battery");
    const snapshot = repository.createSnapshot({ db, appVersion: "0.20.0" });
    db.run("UPDATE knowledge_items SET title = ? WHERE id = ?", "当前版本", "item-1");
    fs.writeFileSync(
      path.join(workDir, "data", "assets", "images", "import-same.png"),
      "current-image",
    );
    const targets = {
      databasePath: path.join(workDir, "data", "knowledge.db"),
      imagesDir: path.join(workDir, "data", "assets", "images"),
      videosDir: path.join(workDir, "data", "assets", "videos"),
      configDir: path.join(workDir, "config"),
    };
    const prepared = prepareRepositoryRestore({
      repository,
      snapshotId: snapshot.snapshot!.fileName,
      liveDb: db,
      targets,
    });
    fs.rmSync(prepared.videosDir, { recursive: true, force: true });
    db.close();
    expect(() => applyPreparedRepositoryRestore(prepared, targets)).toThrow();

    db = new DatabaseAdapter(targets.databasePath);
    expect(
      (db.get("SELECT title FROM knowledge_items WHERE id = ?", "item-1") as {
        title: string;
      }).title,
    ).toBe("当前版本");
    expect(fs.readFileSync(path.join(targets.imagesDir, "import-same.png"), "utf8")).toBe(
      "current-image",
    );
  });
});

describe("备份对象 AES-256-GCM", () => {
  it("密文篡改会在解密时失败", () => {
    const key = Buffer.alloc(32, 7);
    const encrypted = encryptBuffer(Buffer.from("secret"), key);
    encrypted[encrypted.length - 1] ^= 1;
    expect(() => decryptBuffer(encrypted, key)).toThrow();
  });
});


it("公众号历史快照资源随备份恢复，已编辑正文不再引用图片也不会丢失", () => {
  const data=Buffer.from("历史图片"), sha256=createHash("sha256").update(data).digest("hex"), fileName=`wechat-${sha256}.png`;
  const capture:WebCaptureResult={taskId:"capture",entryUrl:"https://mp.weixin.qq.com/s/test",finalUrl:"https://mp.weixin.qq.com/s/test",title:"历史",author:"",publishedAt:null,dateConfidence:"unknown",markdown:"原文",links:[],paragraphs:[],contentHash:"",capturedAt:1,engineVersion:"wechat-html/1",complete:true,truncated:false,warnings:[],snapshot:{formatVersion:1,policyVersion:1,adapterVersion:"wechat-html/1",html:`<img src="local-image://${fileName}">`,css:"p{color:red}",hash:"test",account:"",author:"",publishedAt:null,assets:[{fileName,sourceUrl:"https://mmbiz.qpic.cn/test",sha256,bytes:data.length}],failures:[],warnings:[]}};
  new WebSourceDB(db).initialize("item-1",capture);
  db.run("UPDATE knowledge_items SET content='人工编辑正文',deleted_at=123 WHERE id='item-1'");
  fs.writeFileSync(path.join(workDir,"data/assets/images",fileName),data);
  const repository=createRepository();repository.initialize("correct horse battery");
  const saved=repository.createSnapshot({db,appVersion:"test",request:{}});
  expect(saved.success).toBe(true);
  const targets={databasePath:path.join(workDir,"data/knowledge.db"),imagesDir:path.join(workDir,"data/assets/images"),videosDir:path.join(workDir,"data/assets/videos"),configDir:path.join(workDir,"config")};
  const prepared=prepareRepositoryRestore({repository,snapshotId:saved.snapshot.fileName,liveDb:db,targets});
  expect(fs.readFileSync(path.join(prepared.imagesDir,fileName))).toEqual(data);
  const restored=new DatabaseAdapter(prepared.databasePath);
  try {expect(new WebSourceDB(restored).versions("item-1")[0].snapshot.html).toContain(fileName);expect(restored.get("SELECT content,deleted_at FROM knowledge_items WHERE id='item-1'")).toEqual({content:"人工编辑正文",deleted_at:123});}finally{restored.close();}
});
