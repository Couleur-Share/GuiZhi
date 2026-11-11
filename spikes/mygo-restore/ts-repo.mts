// 备份仓库对拍的 TS 一侧：用产品真实的 BackupRepository / prepareRepositoryRestore / applyPreparedRepositoryRestore。
// 子命令（都在隔离的临时目录里操作，不碰真实用户数据）：
//   make <root> <password>                      生成一个仓库（root/src 是模拟 userData）与两个快照，写 root/make.json
//   mk-target <dir>                             生成一个“当前正在用”的 userData（有机器相关设置、旧资源、旧配置）
//   restore <srcRoot> <snapshotId> <password> <targetDir>   用 TS 恢复到 targetDir
//   dump <dir> <out.json>                       文件树哈希 + 数据库逻辑内容，用来比较恢复结果
//   read <srcRoot> <snapshotId> <password> <out.json>       读取快照：manifest、全部条目的哈希
//   portable <srcRoot> <snapshotId> <password> <out.zip>    导出便携包
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import crypto from "node:crypto";
import Module from "node:module";
import { pathToFileURL } from "node:url";

const isolated = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-restore-ts-"));
process.env.APPDATA = isolated;
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy({ app: { getPath: () => isolated, isPackaged: false } } as Record<string | symbol, unknown>, {
  get: (t, p) => (p in t ? t[p] : noop),
});
const loader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = loader._load;
loader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "electron") return electronStub;
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } };
  return originalLoad.call(this, request, ...rest);
};

const ROOT = path.resolve(import.meta.dirname, "../..");
const { BackupRepository } = await import(pathToFileURL(path.join(ROOT, "apps/desktop/src/main/services/backup-repository.ts")).href);
const { prepareRepositoryRestore, applyPreparedRepositoryRestore } = await import(pathToFileURL(path.join(ROOT, "apps/desktop/src/main/services/backup-repository-restore.ts")).href);
const { previewRepositoryRestore } = await import(pathToFileURL(path.join(ROOT, "apps/desktop/src/main/services/backup-repository-preview.ts")).href);
const { initDatabase, closeDatabase, KnowledgeItemDB, SCHEMA_VERSION } = await import(pathToFileURL(path.join(ROOT, "packages/db/src/index.ts")).href);

const fixtureDb = process.env.GUIZHI_FIXTURE_DB;
const [cmd, ...args] = process.argv.slice(2);
const sha = (b: Buffer) => crypto.createHash("sha256").update(b).digest("hex");
const rnd = (n: number, seed: number) => {
  const b = Buffer.alloc(n);
  let s = seed >>> 0;
  for (let i = 0; i < n; i++) { s = (Math.imul(s, 1664525) + 1013904223) >>> 0; b[i] = s >>> 24; }
  return b;
};
const protector = { backend: "test", isAvailable: () => false, isSecure: () => false, wrap: () => { throw new Error("n/a"); }, unwrap: () => { throw new Error("n/a"); } };
const paths = (userData: string) => ({
  repositoryDir: path.join(userData, "backups", "repository"),
  databasePath: path.join(userData, "data", "knowledge.db"),
  configDir: path.join(userData, "config"),
  imagesDir: path.join(userData, "data", "assets", "images"),
  videosDir: path.join(userData, "data", "assets", "videos"),
});
function layout(userData: string) {
  const p = paths(userData);
  for (const d of [p.imagesDir, p.videosDir, p.configDir, path.dirname(p.databasePath)]) fs.mkdirSync(d, { recursive: true });
  return p;
}
const setSetting = (db: any, k: string, v: unknown) => db.run("INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", k, JSON.stringify(v));

if (cmd === "schema") {
  console.log(SCHEMA_VERSION);
} else if (cmd === "make") {
  const [root, password] = args;
  const src = path.join(root, "src");
  const p = layout(src);
  fs.copyFileSync(fixtureDb!, p.databasePath);
  const db = initDatabase(p.databasePath);
  const items = new KnowledgeItemDB(db);
  fs.writeFileSync(path.join(p.imagesDir, "a1.png"), rnd(5000, 1));
  fs.writeFileSync(path.join(p.imagesDir, "a2.png"), rnd(7000, 2));
  fs.writeFileSync(path.join(p.videosDir, "v1.mp4"), rnd(3 * 1024 * 1024, 3));
  items.create({ title: "带图片和视频的笔记", content: "正文\n\n![图](local-image://a1.png)\n![图2](local-image://a2.png)\n\nlocal-video://v1.mp4", tagNames: ["备份"] } as never);
  setSetting(db, "ytDlpPath", "C:\\snapshot\\yt.exe");
  setSetting(db, "launchAtStartup", false);
  setSetting(db, "theme", "dark");
  fs.writeFileSync(path.join(p.configDir, "ai-models.json"), JSON.stringify({ kind: "guizhi-ai-config", version: 1, note: "SNAPSHOT-CONFIG（非真实密钥）" }));
  fs.writeFileSync(path.join(p.configDir, "shortcuts.json"), JSON.stringify({ search: "Ctrl+K" }));
  fs.writeFileSync(path.join(p.configDir, "mcp.json"), JSON.stringify({ servers: [] }));
  const repo = new BackupRepository(p, protector);
  repo.initialize(password);
  const rendererSettings = { theme: "dark", fontSize: 15, ytDlpPath: "C:\\snapshot\\yt.exe", dataPath: "Z:\\old", closeAction: "tray", custom: { a: [1, 2, 3] } };
  const s1 = repo.createSnapshot({ db, appVersion: "0.28.0", request: { rendererSettings }, kind: "manual" });
  // 第二个快照：多一条笔记、一张新图，其余对象应被复用
  fs.writeFileSync(path.join(p.imagesDir, "a3.png"), rnd(4000, 4));
  items.create({ title: "第二个快照新增", content: "![x](local-image://a3.png)", tagNames: [] } as never);
  const s2 = repo.createSnapshot({ db, appVersion: "0.28.0", request: { rendererSettings }, kind: "auto" });
  closeDatabase();
  fs.writeFileSync(path.join(root, "make.json"), JSON.stringify({ s1, s2 }, null, 1));
  console.log("snapshots", s1.success, s1.reusedObjects, s1.createdObjects, "|", s2.success, s2.reusedObjects, s2.createdObjects);
} else if (cmd === "mk-target") {
  const [dir] = args;
  const p = layout(dir);
  fs.copyFileSync(fixtureDb!, p.databasePath);
  const db = initDatabase(p.databasePath);
  setSetting(db, "ytDlpPath", "D:\\live\\yt.exe");
  setSetting(db, "launchAtStartup", true);
  setSetting(db, "theme", "light");
  fs.writeFileSync(path.join(p.imagesDir, "old.png"), rnd(100, 9));
  fs.writeFileSync(path.join(p.configDir, "ai-models.json"), "LIVE-AI-CONFIG");
  fs.writeFileSync(path.join(p.configDir, "illustration-styles.json"), "LIVE-ILLUSTRATION");
  fs.writeFileSync(p.databasePath + ".clean", "x");
  closeDatabase();
} else if (cmd === "restore") {
  const [srcRoot, snapshotId, password, target] = args;
  const repo = new BackupRepository(paths(path.join(srcRoot, "src")), protector);
  const tp = paths(target);
  const live = initDatabase(tp.databasePath);
  const targets = { databasePath: tp.databasePath, imagesDir: tp.imagesDir, videosDir: tp.videosDir, configDir: tp.configDir };
  const prepared = prepareRepositoryRestore({
    repository: repo, snapshotId, recoveryPassword: password, liveDb: live, targets,
    currentRendererSettings: { ytDlpPath: "D:\\live\\yt.exe", theme: "light", launchAtStartup: true, closeAction: "exit" },
  });
  closeDatabase();
  applyPreparedRepositoryRestore(prepared, targets);
  console.log("TS 恢复完成");
} else if (cmd === "dump") {
  const [dir, out] = args;
  const files: Record<string, string> = {};
  const walk = (d: string) => {
    for (const e of fs.readdirSync(d, { withFileTypes: true })) {
      const full = path.join(d, e.name);
      if (e.isDirectory()) { files[path.relative(dir, full).replaceAll("\\", "/") + "/"] = "dir"; walk(full); }
      else if (e.name !== "knowledge.db") files[path.relative(dir, full).replaceAll("\\", "/")] = sha(fs.readFileSync(full));
      else files["data/knowledge.db"] = "present";
    }
  };
  walk(dir);
  const dbPath = path.join(dir, "data", "knowledge.db");
  fs.copyFileSync(dbPath, dbPath + ".probe");
  const probe = initDatabase(dbPath + ".probe") as any;
  const settings = probe.all("SELECT key, value FROM settings ORDER BY key");
  const titles = probe.all("SELECT title FROM knowledge_items ORDER BY title").map((r: any) => r.title);
  const userVersion = probe.get("PRAGMA user_version").user_version;
  closeDatabase();
  for (const s of ["", "-wal", "-shm", "-journal", ".clean"]) fs.rmSync(dbPath + ".probe" + s, { force: true });
  fs.writeFileSync(out, JSON.stringify({ files, settings, titles, userVersion }, null, 1));
} else if (cmd === "read") {
  const [srcRoot, snapshotId, password, out] = args;
  const repo = new BackupRepository(paths(path.join(srcRoot, "src")), protector);
  const manifest = repo.readManifest(snapshotId, password);
  const entries = manifest.entries.map((e: any) => ({ ...e, plainSha: sha(repo.readEntry(e)) }));
  fs.writeFileSync(out, JSON.stringify({ manifest: { ...manifest, entries }, list: repo.listSnapshots().map((s: any) => ({ ...s, path: undefined })) }, null, 1));
} else if (cmd === "preview") {
  const [srcRoot, snapshotId, password, out] = args;
  const repo = new BackupRepository(paths(path.join(srcRoot, "src")), protector);
  const p = previewRepositoryRestore(repo, snapshotId, password);
  if (p.snapshot) delete p.snapshot.path;
  fs.writeFileSync(out, JSON.stringify(p, null, 1));
} else if (cmd === "changepw") {
  const [srcRoot, oldPw, newPw] = args;
  new BackupRepository(paths(path.join(srcRoot, "src")), protector).changePassword(oldPw, newPw);
} else if (cmd === "delete") {
  const [srcRoot, snapshotId, password] = args;
  console.log("回收对象", new BackupRepository(paths(path.join(srcRoot, "src")), protector).deleteSnapshot(snapshotId, password));
} else if (cmd === "portable") {
  const [srcRoot, snapshotId, password, out] = args;
  const repo = new BackupRepository(paths(path.join(srcRoot, "src")), protector);
  await repo.exportPortable(snapshotId, out, password);
} else {
  throw new Error("未知子命令 " + cmd);
}
process.exit(0);
