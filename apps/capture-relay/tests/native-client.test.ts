import { test } from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createServer } from "../src/server";
import { hash, secret, id } from "../src/database";

test("原生配对、待确认隔离、重放、回执与自助撤销完整链路", async () => {
  const dir = mkdtempSync(join(tmpdir(), "guizhi-native-"));
  const { app, db } = await createServer({ database: join(dir, "relay.db"), origin: "https://capture.example.com" });
  const headers = { "content-type": "application/json", "x-guizhi-protocol": "1" };
  const desktop = secret(), phone = secret(), nonce = secret(), invite = secret();
  const desktopHeaders = { ...headers, authorization: `Bearer ${desktop}` };
  const phoneHeaders = { ...headers, authorization: `Bearer ${phone}` };
  try {
    assert.equal((await app.inject({ url: "/v1/meta" })).json().nativePairing, true);
    db.run("INSERT INTO invites(hash) VALUES(?)", hash(invite));
    await app.inject({ method: "POST", url: "/v1/mailboxes", headers, payload: { invite, credential: desktop, requestId: id() } });
    const pair = (await app.inject({ method: "POST", url: "/v1/pairings", headers: desktopHeaders, payload: { nonce } })).json();
    const payload = { pairingId: pair.id, nonce, credential: phone, name: "鸿蒙测试手机" };
    const claim = await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload });
    assert.equal(claim.statusCode, 200);
    assert.equal(claim.headers["set-cookie"], undefined);
    const deviceId = claim.json().id;
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload })).json().id, deviceId);
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload: { ...payload, credential: secret() } })).statusCode, 409);
    const body = { requestId: id(), input: "https://example.com/article?keep=1", mode: "auto" };
    assert.equal((await app.inject({ url: "/v1/session", headers: phoneHeaders })).json().paired, false);
    assert.equal((await app.inject({ method: "POST", url: "/v1/captures", headers: phoneHeaders, payload: body })).statusCode, 401);
    await app.inject({ method: "POST", url: `/v1/pairings/${pair.id}/confirm`, headers: desktopHeaders, payload: { deviceId } });
    assert.equal((await app.inject({ url: "/v1/session", headers: phoneHeaders })).json().paired, true);
    const capture = await app.inject({ method: "POST", url: "/v1/captures", headers: phoneHeaders, payload: body });
    assert.equal(capture.statusCode, 201);
    const receipt = capture.json();
    assert.equal((await app.inject({ method: "POST", url: "/v1/captures", headers: phoneHeaders, payload: body })).json().id, receipt.id);
    assert.equal((await app.inject({ url: "/v1/deliveries", headers: desktopHeaders })).json().length, 1);
    await app.inject({ method: "POST", url: `/v1/deliveries/${receipt.id}/ack`, headers: desktopHeaders, payload: {} });
    await app.inject({ method: "PUT", url: `/v1/deliveries/${receipt.id}/progress`, headers: desktopHeaders,
      payload: { version: 1, items: [{ index: 0, status: "completed" }] } });
    assert.equal((await app.inject({ url: "/v1/history", headers: phoneHeaders })).json()[0].progress.items[0].status, "completed");
    assert.equal((await app.inject({ method: "DELETE", url: "/v1/session", headers: desktopHeaders, payload: {} })).statusCode, 403);
    assert.equal((await app.inject({ method: "DELETE", url: "/v1/session", headers: phoneHeaders, payload: {} })).statusCode, 200);
    assert.equal((await app.inject({ url: "/v1/history", headers: phoneHeaders })).statusCode, 401);
  } finally { await app.close(); rmSync(dir, { recursive: true, force: true }); }
});

test("原生入口不降低浏览器 CSRF、nonce、协议与过期校验", async () => {
  const dir = mkdtempSync(join(tmpdir(), "guizhi-native-boundary-"));
  const { app } = await createServer({ database: join(dir, "relay.db"), origin: "https://capture.example.com" });
  const headers = { "content-type": "application/json", "x-guizhi-protocol": "1" };
  const payload = { pairingId: id(), nonce: secret(), credential: secret(), name: "手机" };
  try {
    for (const extra of [{ origin: "https://evil.example" }, { origin: "https://capture.example.com" },
      { cookie: "other=1" }, { "sec-fetch-site": "same-origin" }]) {
      assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers: { ...headers, ...extra }, payload })).statusCode, 403);
    }
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload })).statusCode, 410);
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers, payload: { ...payload, nonce: "bad" } })).statusCode, 400);
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/native-claim", headers: { ...headers, "x-guizhi-protocol": "0" }, payload })).statusCode, 426);
    assert.equal((await app.inject({ method: "POST", url: "/v1/pairings/claim", headers, payload })).statusCode, 403);
  } finally { await app.close(); rmSync(dir, { recursive: true, force: true }); }
});
