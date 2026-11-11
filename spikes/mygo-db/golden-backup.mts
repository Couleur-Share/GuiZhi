// 本地备份服务（backup.ts）的差分基准：真实的 listBackups / deleteBackup / pruneBackupsOfKind / validateBackupFile / performRestoreSwap。
// 备份库文件由本脚本用 TS 的 SQLite 适配器造出，写进 <out>/backup-golden/validate 供 Go 侧读同一批文件。
import fs from "node:fs";
import path from "node:path";

export async function buildBackupGolden(ctx: { out: string }): Promise<unknown> {
  const svc = await import("../../apps/desktop/src/main/services/backup.ts");
  const Database = (await import("../../apps/desktop/src/main/database/sqlite.ts")).default as any;
  const root = path.join(ctx.out, "backup-golden");
  fs.rmSync(root, { recursive: true, force: true });
  fs.mkdirSync(root, { recursive: true });
  const norm = (s: string) => s.split(root).join("<ROOT>").replace(/\\/g, "/");
  const stamp = (n: string) => n.replace(/knowledge-pre-restore-\d{8}-\d{6}(-\d+)?\.db/, "knowledge-pre-restore-<TS>.db");

  // ── 列表 / 删除 / 清理 ──
  const listNames = [
    "knowledge-manual-20261101-120000.db",
    "knowledge-manual-20261101-120000-1.db",
    "knowledge-manual-20261101-120000-10.db",
    "knowledge-auto-20261101-130000.db",
    "knowledge-auto-20261102-130000.db",
    "knowledge-auto-20261031-130000.db",
    "knowledge-pre-update-20261031-235959.db",
    "knowledge-pre-restore-20250101-000000-12.db",
    "knowledge-manual-20261301-250000.db", // 月/时越界：JS Date 会进位而不是 NaN
    "knowledge-weird-20261101-120000.db",
    "knowledge-manual-20261101-120000.db.bak",
    "notes.txt",
    "knowledge-auto-2026110-120000.db",
  ];
  const seed = (dir: string) => {
    fs.rmSync(dir, { recursive: true, force: true });
    fs.mkdirSync(dir, { recursive: true });
    listNames.forEach((n, i) => fs.writeFileSync(path.join(dir, n), "x".repeat(i + 1)));
  };
  const listDir = path.join(root, "list");
  seed(listDir);
  const infoOf = (dir: string) =>
    svc.listBackups(dir).map((b: any) => ({ fileName: b.fileName, kind: b.kind, sizeBytes: b.sizeBytes, createdAt: b.createdAt, format: b.format, encrypted: b.encrypted, validation: b.validation, path: norm(b.path) }));
  const list = infoOf(listDir);

  const del = ["../x.db", "a/b.db", "knowledge-manual-20261101-120000.db", "knowledge-manual-20261101-120000.db", "notes.txt", "knowledge-weird-20261101-120000.db", ""].map((name) => {
    const dir = path.join(root, "del");
    if (name === "../x.db") seed(dir);
    return { name, result: svc.deleteBackup(name, dir) };
  });

  const prune = [0, 1, 2, 3, 10].flatMap((keep) =>
    ["manual", "auto", "pre-update", "pre-restore"].map((kind) => {
      const dir = path.join(root, `prune-${kind}-${keep}`);
      seed(dir);
      const removed = svc.pruneBackupsOfKind(kind, keep, dir);
      return { kind, keep, removed, remaining: fs.readdirSync(dir).sort() };
    }),
  );

  // ── 校验 ──
  const vdir = path.join(root, "validate");
  fs.mkdirSync(vdir, { recursive: true });
  const mk = (name: string, build: (db: any) => void, userVersion?: number) => {
    const p = path.join(vdir, name);
    const db = new Database(p);
    db.exec("CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT); CREATE TABLE knowledge_items (id TEXT PRIMARY KEY, content TEXT); CREATE TABLE collections (id TEXT PRIMARY KEY); CREATE TABLE tags (id TEXT PRIMARY KEY);");
    build(db);
    if (userVersion !== undefined) db.exec(`PRAGMA user_version = ${userVersion}`);
    db.close();
  };
  const withVersions = (payloads: string[]) => (db: any) => {
    db.exec("CREATE TABLE web_source_versions (payload TEXT)");
    for (const p of payloads) db.run("INSERT INTO web_source_versions (payload) VALUES (?)", p);
  };
  const goodSnapshot = { formatVersion: 1, html: "<p>x</p>", css: "", assets: [], failures: [], warnings: [], account: "", author: "" };
  mk("ok.db", () => {}, 39);
  mk("ok-old-version.db", () => {}, 3);
  mk("future-version.db", () => {}, 40);
  mk("future-version-far.db", () => {}, 999);
  mk("no-tags.db", (db) => db.exec("DROP TABLE tags"));
  mk("no-settings.db", (db) => db.exec("DROP TABLE settings"));
  mk("snap-null.db", withVersions([JSON.stringify({ snapshot: null })]), 39);
  mk("snap-missing.db", withVersions([JSON.stringify({ markdown: "x" })]), 39);
  mk("snap-good.db", withVersions([JSON.stringify({ snapshot: goodSnapshot })]), 39);
  mk("snap-bad-version.db", withVersions([JSON.stringify({ snapshot: { ...goodSnapshot, formatVersion: 2 } })]), 39);
  mk("snap-bad-meta.db", withVersions([JSON.stringify({ snapshot: { ...goodSnapshot, account: 5 } })]), 39);
  mk("snap-bad-asset.db", withVersions([JSON.stringify({ snapshot: { ...goodSnapshot, assets: [{ fileName: "x.png", sha256: "z" }] } })]), 39);
  mk("snap-good-asset.db", withVersions([JSON.stringify({ snapshot: { ...goodSnapshot, assets: [{ fileName: `wechat-${"a".repeat(64)}.png`, sha256: "b".repeat(64) }] } })]), 39);
  mk("snap-falsy.db", withVersions([JSON.stringify({ snapshot: 0 }), JSON.stringify({ snapshot: "" }), JSON.stringify({ snapshot: false })]), 39);
  mk("snap-not-json.db", withVersions(["{not json"]), 39);
  fs.writeFileSync(path.join(vdir, "empty.db"), "");
  fs.writeFileSync(path.join(vdir, "garbage.db"), Buffer.alloc(4096, 7));
  const truncated = fs.readFileSync(path.join(vdir, "ok.db")).subarray(0, 100);
  fs.writeFileSync(path.join(vdir, "truncated.db"), truncated);
  const validateNames = [...fs.readdirSync(vdir).sort(), "missing.db"];
  const validate = validateNames.map((name) => ({ name, result: svc.validateBackupFile(path.join(vdir, name)) }));

  // ── 换库 ──
  const swapCase = (name: string, setup: (d: { dbPath: string; backupFile: string; backups: string }) => void) => {
    const d = path.join(root, `swap-${name}`);
    fs.rmSync(d, { recursive: true, force: true });
    const dbPath = path.join(d, "data", "knowledge.db");
    const backups = path.join(d, "backups");
    const backupFile = path.join(d, "incoming-source.db");
    fs.mkdirSync(path.dirname(dbPath), { recursive: true });
    setup({ dbPath, backupFile, backups });
    let threw = false;
    let preRestore: string | null = null;
    try {
      preRestore = svc.performRestoreSwap({ databasePath: dbPath, backupFilePath: backupFile, backupsDir: backups }).preRestoreFileName;
    } catch {
      threw = true;
    }
    const exists = (p: string) => fs.existsSync(p);
    return {
      name,
      threw,
      hasPreRestoreName: preRestore !== null,
      db: exists(dbPath) ? fs.readFileSync(dbPath, "utf8") : null,
      sidecars: ["-wal", "-shm", "-journal", ".clean", ".incoming"].map((s) => exists(dbPath + s)),
      backups: exists(backups) ? fs.readdirSync(backups).map(stamp).sort() : null,
      preRestoreContents: exists(backups) ? fs.readdirSync(backups).filter((n) => n.startsWith("knowledge-pre-restore-")).map((n) => fs.readFileSync(path.join(backups, n), "utf8")).sort() : null,
    };
  };
  const writeSide = (dbPath: string) => {
    for (const s of ["-wal", "-shm", "-journal", ".clean"]) fs.writeFileSync(dbPath + s, "side");
  };
  const swap = [
    swapCase("normal", ({ dbPath, backupFile }) => {
      fs.writeFileSync(dbPath, "OLD");
      writeSide(dbPath);
      fs.writeFileSync(backupFile, "NEW");
    }),
    swapCase("no-existing-db", ({ backupFile }) => fs.writeFileSync(backupFile, "NEW")),
    swapCase("missing-backup", ({ dbPath }) => {
      fs.writeFileSync(dbPath, "OLD");
      writeSide(dbPath);
    }),
    swapCase("prune-old-pre-restore", ({ dbPath, backupFile, backups }) => {
      fs.writeFileSync(dbPath, "OLD");
      fs.writeFileSync(backupFile, "NEW");
      fs.mkdirSync(backups, { recursive: true });
      for (const [i, n] of ["20250101-000000", "20250102-000000", "20250103-000000", "20250104-000000"].entries()) fs.writeFileSync(path.join(backups, `knowledge-pre-restore-${n}.db`), `P${i}`);
    }),
  ];

  return { list, del, prune, validate, validateNames, swap };
}
