// 已安装应用的真实 IPC 验收；只使用沙盒内的合成数据与受控 HTTP 代理。
import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";

async function poll(action, ready, timeout = 180000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await action();
    if (ready(value)) return value;
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error("已安装应用验收超时");
}

export default async function ({ win, app, outDir, shot }) {
  const phase = process.env.GUIZHI_INSTALLED_PHASE;
  assert.ok(["previous", "upgrade", "clean"].includes(phase));
  const runtime = await app.evaluate(({ app }) => ({
    packaged: app.isPackaged, version: app.getVersion(), userData: app.getPath("userData"),
  }));
  assert.equal(runtime.packaged, true);
  const expectedVersion = process.env.GUIZHI_INSTALLED_EXPECTED_VERSION;
  assert.ok(expectedVersion, '验收必须明确指定安装版本');
  assert.equal(runtime.version, expectedVersion);
  let readingAssets;
  if (phase !== "previous") {
    readingAssets = await app.evaluate(({ app }) => {
      const fs = process.getBuiltinModule("fs"), path = process.getBuiltinModule("path");
      const root = app.getAppPath();
      const require = process.getBuiltinModule("module").createRequire(path.join(root, "package.json"));
      const { Resvg } = require("@resvg/resvg-js");
      const png = new Resvg('<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12"><rect width="12" height="12" fill="red"/></svg>').render().asPng();
      const assets = ["animation.js", "compiler.js", "runtime-animation.js", "runtime-echarts.js", "runtime-mermaid.js"];
      return { pngSignature: png.subarray(0, 8).toString("hex"), assets: assets.map(name => ({
        name, bytes: fs.readFileSync(path.join(root, "out/main/reading-libraries", name)).length,
      })) };
    });
    assert.equal(readingAssets.pngSignature, "89504e470d0a1a0a");
    assert.ok(readingAssets.assets.every(asset => asset.bytes > 1000));
  }
  let preserved;
  if (phase === "upgrade") {
    const before = JSON.parse(fs.readFileSync(process.env.GUIZHI_INSTALLED_PREVIOUS, "utf8"));
    preserved = await win.evaluate(async (ids) => ({
      manual: await window.api.knowledge.get(ids.manual),
      captured: await window.api.webCapture.versions(ids.captured),
    }), { manual: before.manual.id, captured: before.captures[0].itemId });
    for (const key of ["id", "title", "content", "summary", "isFavorite", "collectionId"])
      assert.deepEqual(preserved.manual[key], before.manual[key], `升级改变人工数据：${key}`);
    assert.deepEqual(preserved.manual.tags, before.manual.tags);
    assert.equal(preserved.captured.ok, true);
    assert.deepEqual(preserved.captured.data.versions, before.captures[0].versions);
    assert.equal(preserved.captured.data.content, before.captures[0].content);
  }
  const origin = "http://guizhi-acceptance.example";
  const server = http.createServer((request, response) => {
    const url = new URL(request.url, origin);
    if (url.origin !== origin) return response.writeHead(403).end();
    const dynamic = url.pathname.includes("dynamic");
    response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    response.end(`<!doctype html><html><head><meta charset="utf-8"><title>安装验收 ${url.pathname}</title></head><body><article><h1>归知安装验收</h1><p>验收地址：${url.pathname}</p><p>中文正文与人工编辑必须保留。</p><pre><code>print("归知")</code></pre>${dynamic ? '<p id="dynamic">加载中</p>' : '<p>静态正文已就绪</p>'}</article>${dynamic ? '<script>setTimeout(()=>document.querySelector("#dynamic").textContent="动态正文已加载",100)</script>' : ''}</body></html>`);
  });
  server.on("connect", (_request, socket) => socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"));
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    await win.evaluate((port) => window.api.settings.set({ networkProxy: {
      mode: "manual", protocol: "http", host: "127.0.0.1", port,
      username: "", password: "", bypass: "localhost,127.0.0.1",
    }}), server.address().port);
    let manual;
    if (phase === "previous") {
      manual = await win.evaluate(async () => {
        const collection = await window.api.collection.create({ name: "升级保留集合" });
        const item = await window.api.knowledge.create({ title: "人工标题", content: "# 原始正文\n\n保留人工编辑。",
          itemType: "webpage", collectionId: collection.id, tagNames: ["升级保留标签"] });
        await window.api.knowledge.update(item.id, { isFavorite: true, summary: "保留人工摘要" });
        return window.api.knowledge.get(item.id);
      });
    }
    const captures = [];
    for (const mode of phase === "previous" ? ["dynamic"] : ["static", "dynamic"]) {
      const [task] = await win.evaluate((url) => window.api.import.enqueue([{ kind: "url", input: url }]), `${origin}/${phase}-${mode}`);
      const completed = await poll(async () => (await win.evaluate(() => window.api.import.list())).find((r) => r.id === task.id),
        (r) => ["completed", "failed", "duplicate", "cancelled"].includes(r?.status));
      assert.equal(completed.status, "completed", completed.error);
      const result = await win.evaluate((id) => window.api.webCapture.versions(id), completed.resultItemId);
      assert.equal(result.ok, true, result.error);
      const { versions, content } = result.data;
      assert.equal(versions[0].engineVersion, phase === "previous" ? "crawl4ai/0.9.3" : `crawl4ai/0.9.3-${mode === "static" ? "static" : "electron"}`);
      assert.ok(content.includes(mode === "static" ? "静态正文已就绪" : "动态正文已加载"));
      assert.ok(content.includes('print("归知")'));
      captures.push({ mode, itemId: completed.resultItemId, versions, content });
    }
    fs.writeFileSync(path.join(outDir, "capture-complete.json"), JSON.stringify({ phase, captures }, null, 2));
    const idle = await poll(async () => {
      const status = await win.evaluate(() => window.api.webCapture.status());
      assert.equal(status.ok, true, status.error);
      return status.data;
    }, (r) => r.available && !r.running, 85000);
    await shot("installed-capture");
    fs.writeFileSync(path.join(outDir, "installed.json"), JSON.stringify({
      passed: true, phase, runtime, readingAssets, manual, preserved, captures, idle,
    }, null, 2));
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
