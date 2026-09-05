import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync, readdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";
import { DatabaseSync } from "node:sqlite";
test("备份加密、认证恢复、错误密钥拒绝与禁止覆盖原库", () => {
  const dir = mkdtempSync(join(tmpdir(), "guizhi-backup-test-"));
  try {
    const database = join(dir, "source.db"), key = join(dir, "key"), backups = join(dir, "backups"), restored = join(dir, "restored.db");
    const db = new DatabaseSync(database); db.exec("CREATE TABLE sample(value TEXT); INSERT INTO sample VALUES('synthetic-private-note')"); db.close();
    writeFileSync(key, randomBytes(32));
    const env = { ...process.env, CAPTURE_DATABASE: database, CAPTURE_BACKUPS: backups, CAPTURE_BACKUP_KEY_FILE: key, TMPDIR: dir };
    const run = (...args: string[]) => execFileSync(process.execPath, ["--import", "tsx", resolve("src/backup.ts"), ...args], { env, stdio: "pipe" });
    run("backup"); const file = join(backups, readdirSync(backups)[0]);
    assert.ok(!readFileSync(file).includes(Buffer.from("synthetic-private-note")));
    run("restore", file, restored);
    const probe = new DatabaseSync(restored); assert.equal(probe.prepare("SELECT value FROM sample").get()?.value, "synthetic-private-note"); probe.close();
    assert.throws(() => run("restore", file, database));
    writeFileSync(key, randomBytes(32)); assert.throws(() => run("restore", file, join(dir, "wrong-key.db")));
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
