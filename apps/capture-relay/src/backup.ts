import { DatabaseSync, backup } from "node:sqlite";
import { createCipheriv, createDecipheriv, randomBytes } from "node:crypto";
import { createReadStream, createWriteStream, readFileSync, writeFileSync, mkdirSync, readdirSync, statSync, unlinkSync, openSync, readSync, closeSync, appendFileSync } from "node:fs";
import { pipeline } from "node:stream/promises";
import { join, resolve } from "node:path";
const database = resolve(process.env.CAPTURE_DATABASE ?? "data/capture.db");
const directory = resolve(process.env.CAPTURE_BACKUPS ?? "backups");
const keyPath = process.env.CAPTURE_BACKUP_KEY_FILE;
if (!keyPath) throw new Error("必须配置独立备份密钥文件");
const key = readFileSync(keyPath);
if (key.length !== 32) throw new Error("备份密钥必须为 32 字节");
mkdirSync(directory, { recursive: true, mode: 0o700 });
const mode = process.argv[2] ?? "backup";
if (mode === "restore") {
  const file = resolve(process.argv[3] ?? "");
  // 只恢复到显式的全新路径，避免覆盖仍在使用的数据库。
  const target = process.argv[4]; if (!target) throw new Error("请指定全新恢复路径");
  const length = statSync(file).size; if (length < 32) throw new Error("备份格式错误");
  const fd = openSync(file, "r"), header = Buffer.alloc(16), tag = Buffer.alloc(16);
  readSync(fd, header, 0, 16, 0); readSync(fd, tag, 0, 16, length - 16); closeSync(fd);
  if (header.subarray(0, 4).toString() !== "GZB1") throw new Error("备份版本不支持");
  const decipher = createDecipheriv("aes-256-gcm", key, header.subarray(4)); decipher.setAuthTag(tag);
  try {
    await pipeline(createReadStream(file, { start: 16, end: length - 17 }), decipher, createWriteStream(target, { flags: "wx", mode: 0o600 }));
    const probe = new DatabaseSync(target, { readOnly: true });
    try { if (probe.prepare("PRAGMA integrity_check").get()?.integrity_check !== "ok") throw new Error("恢复校验失败"); } finally { probe.close(); }
  } catch (error) { throw new Error("备份恢复失败，请保留原数据库并检查密钥和文件", { cause: error }); }
} else {
  const snapshot = join(process.env.TMPDIR ?? "/tmp", `capture-snapshot-${randomBytes(12).toString("hex")}.db`);
  const file = join(directory, `capture-${Date.now()}.gzb`), tmp = file + ".tmp";
  const db = new DatabaseSync(database, { readOnly: true });
  try {
    await backup(db, snapshot);
    const iv = randomBytes(12), cipher = createCipheriv("aes-256-gcm", key, iv);
    writeFileSync(tmp, Buffer.concat([Buffer.from("GZB1"), iv]), { flag: "wx", mode: 0o600 });
    await pipeline(createReadStream(snapshot), cipher, createWriteStream(tmp, { flags: "a" }));
    appendFileSync(tmp, cipher.getAuthTag());
    const { renameSync } = await import("node:fs"); renameSync(tmp, file);
    // 仅删除受本命名规则管理且超过七天的备份文件。
    for (const name of readdirSync(directory)) if (/^capture-\d+\.gzb$/.test(name)) {
      const candidate = join(directory, name); if (statSync(candidate).mtimeMs < Date.now() - 7 * 86400000) unlinkSync(candidate);
    }
    process.stdout.write("加密备份完成\n");
  } finally { db.close(); try { unlinkSync(snapshot); } catch { /* 快照可能尚未创建。 */ } }
}
