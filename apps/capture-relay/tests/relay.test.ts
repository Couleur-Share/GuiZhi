import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "../src/server";
import { hash, secret, id, DAY } from "../src/database";
import { DEFAULT_LIMITS } from "../src/captures";
const origin = "https://capture.example.com";
async function fixture(limits = DEFAULT_LIMITS) {
  const directory = mkdtempSync(join(tmpdir(), "guizhi-relay-test-")), database = join(directory, "relay.db");
  let server = await createServer({ database, origin, limits });
  const headers = (credential?: string) => ({ "content-type": "application/json", "x-guizhi-protocol": "1", ...(credential ? { authorization: `Bearer ${credential}` } : {}) });
  const request = (method: "POST" | "GET" | "PUT" | "DELETE", url: string, credential?: string, payload?: unknown) => server.app.inject({ method, url, headers: headers(credential), payload: payload === undefined ? undefined : JSON.stringify(payload) });
  async function account() {
    const invite = secret(), credential = secret(), requestId = id(); server.db.run("INSERT INTO invites(hash) VALUES(?)", hash(invite));
    const created = await request("POST", "/v1/mailboxes", undefined, { invite, credential, requestId }); assert.equal(created.statusCode, 200);
    return { credential, invite, requestId, mailboxId: created.json().mailboxId as string };
  }
  async function phone(desktop: string) {
    const nonce = secret(), credential = secret();
    const pairing = (await request("POST", "/v1/pairings", desktop, { nonce })).json();
    const claimBody = { pairingId: pairing.id, nonce, credential, name: "合成测试手机" };
    const claim = await server.app.inject({ method: "POST", url: "/v1/pairings/claim", payload: claimBody, headers: { ...headers(), origin, "x-guizhi-csrf": "1" } });
    assert.equal(claim.statusCode, 200);
    const deviceId = claim.json().id as string;
    assert.equal((await request("POST", `/v1/pairings/${pairing.id}/confirm`, desktop, { deviceId })).statusCode, 200);
    return { credential, deviceId, pairing, claimBody, cookie: claim.cookies[0] };
  }
  return { request, account, phone, headers, get server() { return server; },
    restart: async () => { await server.app.close(); server = await createServer({ database, origin, limits }); },
    close: async () => { await server.app.close(); rmSync(directory, { recursive: true, force: true }); } };
}
test("创建响应丢失后只建立一个收件箱，邀请码只能消费一次", async () => {
  const f = await fixture(); try {
    const a = await f.account();
    const retry = await f.request("POST", "/v1/mailboxes", undefined, a); assert.equal(retry.json().mailboxId, a.mailboxId);
    assert.equal((await f.request("POST", "/v1/mailboxes", undefined, { ...a, requestId: id(), credential: secret() })).statusCode, 403);
    assert.equal((await f.request("POST", "/v1/mailboxes", undefined, { ...a, credential: secret() })).statusCode, 409);
    assert.equal(f.server.db.all("SELECT * FROM mailboxes").length, 1);
  } finally { await f.close(); }
});
test("丢失提交回执、重启与 ACK 重放：不重复投递且正文在确认后清除", async () => {
  const f = await fixture(); try {
    const desktop = await f.account(), phone = await f.phone(desktop.credential);
    const body = { requestId: id(), input: "https://example.com/a?token=private&x=2\nhttps://example.com/b", mode: "auto" };
    const accepted = (await f.request("POST", "/v1/captures", phone.credential, body)).json();
    await f.restart();
    const retry = (await f.request("POST", "/v1/captures", phone.credential, body)).json(); assert.equal(retry.id, accepted.id);
    assert.equal((await f.request("POST", "/v1/captures", phone.credential, { ...body, input: "变更" })).statusCode, 409);
    const deliveries = (await f.request("GET", "/v1/deliveries", desktop.credential)).json(); assert.equal(deliveries.length, 1); assert.equal(deliveries[0].input, body.input);
    for (let n = 0; n < 2; n++) assert.equal((await f.request("POST", `/v1/deliveries/${accepted.id}/ack`, desktop.credential, {})).statusCode, 200);
    assert.equal(f.server.db.get<{ input: string | null }>("SELECT input FROM captures WHERE id=?", accepted.id)?.input, null);
    assert.equal((await f.request("GET", "/v1/deliveries", desktop.credential)).json().length, 0);
    const progress = { version: 2, items: [{ index: 0, status: "completed" }, { index: 1, status: "failed", error: "login_required" }] };
    assert.equal((await f.request("PUT", `/v1/deliveries/${accepted.id}/progress`, desktop.credential, progress)).statusCode, 200);
    await f.request("PUT", `/v1/deliveries/${accepted.id}/progress`, desktop.credential, { ...progress, version: 1 });
    const history = (await f.request("GET", "/v1/history", phone.credential)).json(); assert.equal(history[0].progress.version, 2); assert.ok(!JSON.stringify(history).includes("private"));
    assert.equal((await f.request("PUT", `/v1/deliveries/${accepted.id}/progress`, desktop.credential, { ...progress, items: [{ index: 0, status: "pending" }, progress.items[1]] })).statusCode, 409);
  } finally { await f.close(); }
});
test("收件箱隔离、手机越权和快捷指令撤销", async () => {
  const f = await fixture(); try {
    const a = await f.account(), b = await f.account(), p = await f.phone(a.credential), q = await f.phone(a.credential);
    const capture = (await f.request("POST", "/v1/captures", p.credential, { requestId: id(), input: "合成文字", mode: "text" })).json();
    assert.equal((await f.request("GET", "/v1/deliveries", p.credential)).statusCode, 403);
    assert.equal((await f.request("GET", "/v1/devices", p.credential)).statusCode, 403);
    assert.equal((await f.request("POST", `/v1/deliveries/${capture.id}/ack`, b.credential, {})).statusCode, 404);
    assert.equal((await f.request("GET", "/v1/history", q.credential)).json().length, 0);
    const shortcut = secret(); await f.request("POST", "/v1/shortcut", p.credential, { credential: shortcut });
    assert.equal((await f.request("POST", "/v1/captures", shortcut, { requestId: id(), input: "快捷指令文字", mode: "text" })).statusCode, 201);
    assert.equal((await f.request("GET", "/v1/history", p.credential)).json().length, 2);
    assert.equal((await f.request("GET", "/v1/history", shortcut)).json().length, 1);
    await f.request("DELETE", `/v1/devices/${p.deviceId}`, a.credential, {});
    assert.equal((await f.request("GET", "/v1/history", shortcut)).statusCode, 401);
    assert.equal((await f.request("GET", "/v1/history", p.credential)).statusCode, 401);
  } finally { await f.close(); }
});
test("配对过期/重放和 Cookie 同源 CSRF", async () => {
  const f = await fixture(); try {
    const a = await f.account(), p = await f.phone(a.credential);
    assert.ok(p.cookie.httpOnly && p.cookie.secure); assert.equal(p.cookie.sameSite, "Strict");
    const payload = { requestId: id(), input: "文字", mode: "text" };
    const headers = { ...f.headers(), cookie: `${p.cookie.name}=${p.cookie.value}` };
    assert.equal((await f.server.app.inject({ method: "POST", url: "/v1/captures", headers, payload })).statusCode, 403);
    assert.equal((await f.server.app.inject({ method: "POST", url: "/v1/captures", headers: { ...headers, origin, "x-guizhi-csrf": "1" }, payload })).statusCode, 201);
    assert.equal((await f.server.app.inject({ method: "POST", url: "/v1/pairings/claim", headers: { ...f.headers(), origin, "x-guizhi-csrf": "1" }, payload: p.claimBody })).statusCode, 410);
    const nonce = secret(), pair = (await f.request("POST", "/v1/pairings", a.credential, { nonce })).json();
    f.server.db.run("UPDATE pairings SET expires_at=0 WHERE id=?", pair.id);
    assert.equal((await f.server.app.inject({ method: "POST", url: "/v1/pairings/claim", headers: { ...f.headers(), origin, "x-guizhi-csrf": "1" }, payload: { ...p.claimBody, pairingId: pair.id, nonce } })).statusCode, 410);
  } finally { await f.close(); }
});
test("容量上限拒绝新投递，保留原投递；过期清除原文", async () => {
  const f = await fixture({ ...DEFAULT_LIMITS, pendingItems: 1 }); try {
    const a = await f.account(), p = await f.phone(a.credential);
    await f.request("POST", "/v1/captures", p.credential, { requestId: id(), input: "原投递", mode: "text" });
    const rejected = await f.request("POST", "/v1/captures", p.credential, { requestId: id(), input: "新投递", mode: "text" });
    assert.equal(rejected.statusCode, 429); assert.equal(rejected.json().error, "inbox_full");
    assert.equal(f.server.db.all("SELECT * FROM captures").length, 1);
    f.server.db.cleanup(Date.now() + 31 * DAY);
    assert.equal(f.server.db.get<{ input: string | null }>("SELECT input FROM captures")?.input, null);
  } finally { await f.close(); }
});
test("大小、项目数、协议校验和代理信任不能绕过 IP 限制", async () => {
  const f = await fixture(); try {
    const a = await f.account(), p = await f.phone(a.credential);
    assert.equal((await f.request("POST", "/v1/captures", p.credential, { requestId: id(), input: "中".repeat(11000), mode: "text" })).statusCode, 400);
    assert.equal((await f.request("POST", "/v1/captures", p.credential, { requestId: id(), input: Array.from({ length: 21 }, (_, i) => `https://example.com/${i}`).join("\n"), mode: "urls" })).statusCode, 400);
    assert.equal((await f.server.app.inject({ method: "POST", url: "/v1/captures", headers: { ...f.headers(p.credential), "x-guizhi-protocol": "2" }, payload: {} })).statusCode, 426);
    for (let i = 0; i < 6; i++) await f.server.app.inject({ method: "POST", url: "/v1/mailboxes", headers: { ...f.headers(), "x-forwarded-for": `1.1.1.${i}` }, payload: { requestId: id(), credential: secret(), invite: "invalid" } });
    assert.equal((await f.request("POST", "/v1/mailboxes", undefined, {})).statusCode, 429);
  } finally { await f.close(); }
});
