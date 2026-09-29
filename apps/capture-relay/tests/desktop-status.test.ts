import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { createServer } from "../src/server";
import { DESKTOP_STATUS_THROTTLE, RelayDatabase, hash, id, secret } from "../src/database";

// 电脑在线状态：中转记录每个收件箱最近一次通过认证的取件时间，已配对的手机在 GET /v1/session 里得到“距今多少毫秒”。
// 数据库层（时间戳、节流、清理、加法表兼容）与真实 Fastify + SQLite 层（谁能看到、什么算取件、失败隔离）分开验证。

// ---------- 数据库层 ----------

function tempDatabase() {
  const dir = mkdtempSync(join(tmpdir(), "guizhi-desktop-status-"));
  return { file: join(dir, "relay.db"), dispose: () => rmSync(dir, { recursive: true, force: true }) };
}
const polledAt = (db: RelayDatabase, mailboxId: string) => db.get<{ polled_at: number }>("SELECT polled_at FROM desktop_status WHERE mailbox_id=?", mailboxId)?.polled_at;

test("从未取件为 null；取件后按传入的服务端时间计算距今毫秒，时钟回拨不产生负数", () => {
  const t = tempDatabase(), db = new RelayDatabase(t.file);
  try {
    assert.equal(db.desktopSeenAgo("m1", 5_000), null);
    db.touchDesktop("m1", 1_000);
    assert.equal(db.desktopSeenAgo("m1", 1_000), 0);
    assert.equal(db.desktopSeenAgo("m1", 91_000), 90_000);
    assert.equal(db.desktopSeenAgo("m1", 500), 0, "服务端时钟回拨时不返回负数");
  } finally { db.close(); t.dispose(); }
});

test("写库节流 10 秒：窗口内不改写，满 10 秒更新；时钟回拨时立即改写，不停在未来", () => {
  const t = tempDatabase(), db = new RelayDatabase(t.file);
  try {
    assert.equal(DESKTOP_STATUS_THROTTLE, 10_000, "桌面约每 30 秒取件，10 秒足以让在线判断的误差远小于 90 秒阈值");
    db.touchDesktop("m1", 100_000);
    db.touchDesktop("m1", 100_000 + 9_999);
    assert.equal(polledAt(db, "m1"), 100_000, "窗口内不改写");
    db.touchDesktop("m1", 100_000 + 10_000);
    assert.equal(polledAt(db, "m1"), 110_000, "恰好满 10 秒就更新");
    db.touchDesktop("m1", 50_000);
    assert.equal(polledAt(db, "m1"), 50_000, "时钟回拨后立即改写");
    assert.equal(db.desktopSeenAgo("m1", 50_000 + 3_000), 3_000);
  } finally { db.close(); t.dispose(); }
});

test("不同收件箱的状态互相独立", () => {
  const t = tempDatabase(), db = new RelayDatabase(t.file);
  try {
    db.touchDesktop("a", 1_000);
    db.touchDesktop("b", 50_000);
    assert.equal(db.desktopSeenAgo("a", 61_000), 60_000);
    assert.equal(db.desktopSeenAgo("b", 61_000), 11_000);
    assert.equal(db.desktopSeenAgo("c", 61_000), null);
  } finally { db.close(); t.dispose(); }
});

test("清理：已停用或不存在的收件箱不保留在线状态，正常收件箱保留", () => {
  const t = tempDatabase(), db = new RelayDatabase(t.file);
  try {
    const mailbox = (mailboxId: string, active: number) => db.run("INSERT INTO mailboxes(id,request_id,credential_hash,active) VALUES(?,?,?,?)", mailboxId, id(), hash(secret()), active);
    mailbox("on", 1); mailbox("off", 0);
    for (const mailboxId of ["on", "off", "ghost"]) db.touchDesktop(mailboxId, 1_000);
    db.cleanup(2_000);
    assert.equal(polledAt(db, "on"), 1_000);
    assert.equal(polledAt(db, "off"), undefined);
    assert.equal(polledAt(db, "ghost"), undefined);
  } finally { db.close(); t.dispose(); }
});

// 0.5.1 之前的镜像使用的建表语句，原样冻结：新表只能是加法，旧镜像必须仍能执行它并打开同一数据库。
const LEGACY_V1_DDL = `PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA secure_delete=ON;
      CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY);
      CREATE TABLE IF NOT EXISTS invites(hash TEXT PRIMARY KEY, used_by TEXT);
      CREATE TABLE IF NOT EXISTS mailboxes(id TEXT PRIMARY KEY, request_id TEXT UNIQUE NOT NULL, credential_hash TEXT NOT NULL UNIQUE, active INTEGER NOT NULL DEFAULT 1);
      CREATE TABLE IF NOT EXISTS devices(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), name TEXT NOT NULL, kind TEXT NOT NULL,
        credential_hash TEXT UNIQUE NOT NULL, parent_id TEXT REFERENCES devices(id), active INTEGER NOT NULL DEFAULT 0);
      CREATE TABLE IF NOT EXISTS pairings(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), nonce_hash TEXT UNIQUE NOT NULL,
        expires_at INTEGER NOT NULL, device_id TEXT REFERENCES devices(id), confirmed INTEGER NOT NULL DEFAULT 0);
      CREATE TABLE IF NOT EXISTS captures(id TEXT PRIMARY KEY, mailbox_id TEXT NOT NULL REFERENCES mailboxes(id), device_id TEXT NOT NULL REFERENCES devices(id),
        request_id TEXT NOT NULL, fingerprint TEXT NOT NULL, input TEXT, mode TEXT NOT NULL, item_count INTEGER NOT NULL, bytes INTEGER NOT NULL,
        created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, received_at INTEGER, state TEXT NOT NULL DEFAULT 'accepted', version INTEGER NOT NULL DEFAULT 0, progress TEXT,
        UNIQUE(device_id,request_id));
      CREATE INDEX IF NOT EXISTS captures_delivery ON captures(mailbox_id,state,created_at,id);
      CREATE INDEX IF NOT EXISTS captures_history ON captures(device_id,created_at);
      CREATE TABLE IF NOT EXISTS daily_usage(mailbox_id TEXT NOT NULL, day INTEGER NOT NULL, items INTEGER NOT NULL, PRIMARY KEY(mailbox_id,day));
      CREATE TABLE IF NOT EXISTS rate_limits(key TEXT PRIMARY KEY, reset_at INTEGER NOT NULL, count INTEGER NOT NULL);
      INSERT OR IGNORE INTO schema_migrations VALUES(1); PRAGMA user_version=1;`;

test("加法表不升数据库版本：旧镜像建出的库可直接打开，新库也能被旧建表语句打开（回滚安全）", () => {
  const t = tempDatabase();
  try {
    const legacy = new DatabaseSync(t.file);
    legacy.exec(LEGACY_V1_DDL);
    legacy.prepare("INSERT INTO mailboxes(id,request_id,credential_hash) VALUES(?,?,?)").run("m1", id(), hash(secret()));
    legacy.close();

    const db = new RelayDatabase(t.file);
    assert.equal(db.get<{ user_version: number }>("PRAGMA user_version")?.user_version, 1, "版本号不变");
    assert.deepEqual(db.all<{ version: number }>("SELECT version FROM schema_migrations").map(row => row.version), [1]);
    assert.ok(db.get("SELECT name FROM sqlite_master WHERE type='table' AND name='desktop_status'"), "新表按需创建");
    assert.equal(db.get<{ n: number }>("SELECT count(*) AS n FROM mailboxes")?.n, 1, "旧数据保持");
    db.touchDesktop("m1", 7_000);
    db.close();

    // 回滚到旧镜像：旧建表语句在含新表的库上照常执行，版本检查（> 1 才拒绝）也不会挡住它。
    const rollback = new DatabaseSync(t.file);
    rollback.exec(LEGACY_V1_DDL);
    assert.equal(rollback.prepare("PRAGMA user_version").get()?.user_version, 1);
    assert.equal(rollback.prepare("SELECT polled_at FROM desktop_status WHERE mailbox_id='m1'").get()?.polled_at, 7_000, "新表内容不受旧语句影响");
    rollback.close();

    // 再升级回来：不重复创建也不丢数据。
    const again = new RelayDatabase(t.file);
    assert.equal(polledAt(again, "m1"), 7_000);
    again.close();
  } finally { t.dispose(); }
});

// ---------- 真实 Fastify + SQLite ----------

async function relayFixture() {
  const dir = mkdtempSync(join(tmpdir(), "guizhi-desktop-status-http-"));
  const { app, db } = await createServer({ database: join(dir, "relay.db"), origin: "https://capture.example.com" });
  const headers = { "content-type": "application/json", "x-guizhi-protocol": "1" };
  // 创建一个收件箱和一台手机；approve 为 false 时手机停留在“待确认”。
  async function account(name: string, approve = true) {
    const desktop = secret(), phone = secret(), nonce = secret(), invite = secret();
    const desktopHeaders = { ...headers, authorization: `Bearer ${desktop}` };
    const phoneHeaders = { ...headers, authorization: `Bearer ${phone}` };
    db.run("INSERT INTO invites(hash) VALUES(?)", hash(invite));
    assert.equal((await app.inject({ method: "POST", url: "/v1/mailboxes", headers, payload: { invite, credential: desktop, requestId: id() } })).statusCode, 200);
    const pair = (await app.inject({ method: "POST", url: "/v1/pairings", headers: desktopHeaders, payload: { nonce } })).json();
    const claim = await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload: { pairingId: pair.id, nonce, credential: phone, name } });
    assert.equal(claim.statusCode, 200);
    const deviceId = claim.json().id as string;
    const confirm = () => app.inject({ method: "POST", url: `/v1/pairings/${pair.id}/confirm`, headers: desktopHeaders, payload: { deviceId } });
    if (approve) assert.equal((await confirm()).statusCode, 200);
    const mailboxId = db.get<{ mailbox_id: string }>("SELECT mailbox_id FROM devices WHERE id=?", deviceId)!.mailbox_id;
    return {
      mailboxId, deviceId, desktopHeaders, phoneHeaders, confirm,
      poll: () => app.inject({ url: "/v1/deliveries", headers: desktopHeaders }),
      session: () => app.inject({ url: "/v1/session", headers: phoneHeaders }),
    };
  }
  return { app, db, account, close: async () => { await app.close(); rmSync(dir, { recursive: true, force: true }); } };
}
const rows = (db: RelayDatabase) => db.get<{ n: number }>("SELECT count(*) AS n FROM desktop_status")!.n;

test("/v1/meta 声明 desktopStatus 能力，旧字段保持", async () => {
  const relay = await relayFixture();
  try {
    const meta = (await relay.app.inject({ url: "/v1/meta" })).json();
    assert.equal(meta.desktopStatus, true);
    assert.equal(meta.nativePairing, true);
    assert.equal(meta.protocol, 1);
    assert.equal(typeof meta.serverTime, "number");
  } finally { await relay.close(); }
});

test("待确认的手机看不到电脑状态；被电脑确认后才出现，从未取件为 null", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("待确认手机", false);
    await a.poll();
    const pending = (await a.session()).json();
    assert.equal(pending.paired, false);
    assert.equal(pending.deviceId, a.deviceId);
    assert.equal("desktopSeenAgoMs" in pending, false, "待确认设备不应得知收件箱的活动情况");
    await a.confirm();
    const approved = (await a.session()).json();
    assert.equal(approved.paired, true);
    assert.equal(typeof approved.desktopSeenAgoMs, "number", "确认之前电脑已经取过件");
  } finally { await relay.close(); }
});

test("已确认的手机：未取件为 null；桌面取件后为很小的非负数；按服务端时间随时间增长", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("手机 A");
    const before = (await a.session()).json();
    assert.equal(before.paired, true);
    assert.equal(before.desktopSeenAgoMs, null, "从未取件");

    assert.equal((await a.poll()).statusCode, 200);
    const fresh = (await a.session()).json().desktopSeenAgoMs as number;
    assert.ok(Number.isInteger(fresh) && fresh >= 0 && fresh < 5_000, `刚取件后应接近 0：${fresh}`);

    // 把最近取件时间改成 2 分钟前，模拟电脑停止取件后的服务端读数。
    relay.db.run("UPDATE desktop_status SET polled_at=? WHERE mailbox_id=?", Date.now() - 120_000, a.mailboxId);
    const stale = (await a.session()).json().desktopSeenAgoMs as number;
    assert.ok(stale >= 120_000 && stale < 125_000, `约 2 分钟前：${stale}`);

    // 电脑再次取件（距上次写入已超过节流窗口）后恢复为很小的值。
    assert.equal((await a.poll()).statusCode, 200);
    const recovered = (await a.session()).json().desktopSeenAgoMs as number;
    assert.ok(recovered >= 0 && recovered < 5_000, `重新取件后恢复：${recovered}`);
  } finally { await relay.close(); }
});

test("只有通过认证的桌面取件请求才算：手机、错误凭证与桌面的其他接口都不写入", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("手机 A");
    const app = relay.app;
    assert.equal((await app.inject({ url: "/v1/deliveries", headers: a.phoneHeaders })).statusCode, 403, "手机不能取件");
    assert.equal((await app.inject({ url: "/v1/deliveries", headers: { ...a.desktopHeaders, authorization: `Bearer ${secret()}` } })).statusCode, 401, "错误凭证");
    assert.equal((await app.inject({ url: "/v1/deliveries" })).statusCode, 401, "无凭证");
    for (const request of [
      () => app.inject({ url: "/v1/devices", headers: a.desktopHeaders }),
      () => app.inject({ url: "/v1/pairings", headers: a.desktopHeaders }),
      () => app.inject({ url: "/v1/history", headers: a.phoneHeaders }),
      () => app.inject({ url: "/v1/session", headers: a.phoneHeaders }),
      () => app.inject({ url: "/v1/meta" }),
    ]) assert.equal((await request()).statusCode, 200);
    assert.equal(rows(relay.db), 0, "以上请求都不应产生在线记录");
    assert.equal((await a.poll()).statusCode, 200);
    assert.equal(rows(relay.db), 1);
  } finally { await relay.close(); }
});

test("同一收件箱的连续取件受节流：窗口内不重复写库", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("手机 A");
    await a.poll();
    const first = polledAt(relay.db, a.mailboxId)!;
    for (let i = 0; i < 3; i++) await a.poll();
    assert.equal(polledAt(relay.db, a.mailboxId), first, "10 秒内的重复取件不改写时间戳");
    assert.equal(rows(relay.db), 1);
  } finally { await relay.close(); }
});

test("跨收件箱隔离：一台电脑取件不会让另一个收件箱的手机看到在线", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("手机 A"), b = await relay.account("手机 B");
    assert.notEqual(a.mailboxId, b.mailboxId);
    await a.poll();
    assert.equal(typeof (await a.session()).json().desktopSeenAgoMs, "number");
    assert.equal((await b.session()).json().desktopSeenAgoMs, null);
    await b.poll();
    relay.db.run("UPDATE desktop_status SET polled_at=? WHERE mailbox_id=?", Date.now() - 400_000, a.mailboxId);
    assert.ok(((await a.session()).json().desktopSeenAgoMs as number) >= 400_000);
    assert.ok(((await b.session()).json().desktopSeenAgoMs as number) < 5_000, "B 的状态不受 A 影响");
  } finally { await relay.close(); }
});

test("被撤销的手机看不到电脑状态：session 退回“未配对”且不含该字段，其余接口 401（自助撤销与桌面撤销两条路径）", async () => {
  const relay = await relayFixture();
  try {
    const a = await relay.account("自助撤销"), b = await relay.account("桌面撤销");
    await a.poll(); await b.poll();
    assert.equal(typeof (await a.session()).json().desktopSeenAgoMs, "number");
    assert.equal(typeof (await b.session()).json().desktopSeenAgoMs, "number");

    assert.equal((await relay.app.inject({ method: "DELETE", url: "/v1/session", headers: a.phoneHeaders, payload: {} })).statusCode, 200);
    assert.equal((await relay.app.inject({ method: "DELETE", url: `/v1/devices/${b.deviceId}`, headers: b.desktopHeaders, payload: {} })).statusCode, 200);
    for (const revoked of [a, b]) {
      const session = await revoked.session();
      assert.equal(session.statusCode, 200, "session 允许待确认与已撤销的设备查询自己的配对状态");
      assert.equal(session.json().paired, false);
      assert.equal("desktopSeenAgoMs" in session.json(), false);
      assert.equal((await relay.app.inject({ url: "/v1/history", headers: revoked.phoneHeaders })).statusCode, 401);
    }
  } finally { await relay.close(); }
});

test("在线状态写入失败不影响取件：仍返回待取列表，只在标准错误留一行说明", async () => {
  const relay = await relayFixture();
  const original = process.stderr.write;
  const lines: string[] = [];
  try {
    const a = await relay.account("手机 A");
    const submit = await relay.app.inject({ method: "POST", url: "/v1/captures", headers: a.phoneHeaders,
      payload: { requestId: id(), input: "https://example.com/status", mode: "auto" } });
    assert.equal(submit.statusCode, 201);
    relay.db.run("DROP TABLE desktop_status");
    process.stderr.write = ((chunk: string | Uint8Array) => { lines.push(String(chunk)); return true; }) as typeof process.stderr.write;
    const polled = await a.poll();
    process.stderr.write = original;
    assert.equal(polled.statusCode, 200);
    assert.equal((polled.json() as unknown[]).length, 1, "待取内容照常返回");
    assert.ok(lines.some(line => line.includes("桌面在线状态写入失败")));
    assert.ok(lines.every(line => !line.includes("example.com")), "说明里不带任何内容");
  } finally { process.stderr.write = original; await relay.close(); }
});
