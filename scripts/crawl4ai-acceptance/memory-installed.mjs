// 同一受控网页、真实导入队列；采样器是驱动的子进程，不计入应用内存。
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import http from "node:http";
import { spawn } from "node:child_process";
import { fileURLToPath } from "node:url";

const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
async function poll(action, ready, limit = 120000) {
  const deadline = Date.now() + limit;
  while (Date.now() < deadline) {
    const result = await action();
    if (ready(result)) return result;
    await delay(250);
  }
  throw new Error("内存验收等待超时");
}

export default async function ({ win, app, outDir, shot }) {
  const variant = process.env.GUIZHI_MEMORY_VARIANT;
  assert.ok(["previous", "candidate"].includes(variant));
  const runtime = await app.evaluate(({ app }) => ({
    pid: process.pid, packaged: app.isPackaged, version: app.getVersion(),
  }));
  assert.ok(runtime.packaged);
  const events = [], captures = [];
  function stage(name) {
    const state = { stage: name, time: Date.now() };
    // Windows 共享目录中的读句柄可能阻止替换文件；阶段标记只新增、不覆盖。
    const file = path.join(outDir, `memory-stage-${String(events.length).padStart(5, "0")}.json`);
    fs.writeFileSync(`${file}.tmp`, JSON.stringify(state));
    fs.renameSync(`${file}.tmp`, file);
    events.push(state);
    fs.writeFileSync(path.join(outDir, "events.json"), JSON.stringify(events, null, 2));
  }
  const origin = "http://guizhi-memory.example";
  let active = 0, maxActive = 0;
  const server = http.createServer(async (req, res) => {
    const url = new URL(req.url, origin);
    if (url.origin !== origin) return res.writeHead(403).end();
    active++; maxActive = Math.max(maxActive, active);
    // 延迟让两个真实导入任务重叠；正文包含唯一样本地址，避免内容去重。
    await delay(700);
    const dynamic = url.pathname.includes("dynamic");
    const paragraphs = Array.from({ length: 120 }, (_, i) => `<h2>章节 ${i}</h2><p>归知内存验收正文，记录知识采集与数据保留。${i} ${url.pathname}</p>`).join("");
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    res.end(`<!doctype html><html><head><title>归知内存验收 ${url.pathname}</title></head><body><article><h1>归知内存验收</h1>${paragraphs}<pre><code>print("归知")</code></pre><p id="result">${dynamic ? "加载中" : "静态正文已就绪"}</p></article>${dynamic ? '<script>setTimeout(()=>document.querySelector("#result").textContent="动态正文已加载",100)</script>' : ""}</body></html>`);
    active--;
  });
  server.on("connect", (_req, socket) => socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"));
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const source = path.dirname(fileURLToPath(import.meta.url));
  let sampler, samplerExit;
  try {
    await win.evaluate((port) => window.api.settings.set({ networkProxy: {
      mode: "manual", protocol: "http", host: "127.0.0.1", port,
      username: "", password: "", bypass: "localhost,127.0.0.1",
    }}), server.address().port);
    stage("startup-idle");
    const log = fs.openSync(path.join(outDir, "sampler.log"), "w");
    sampler = spawn(path.join(source, "tools/python/python.exe"),
      ["-B", path.join(source, "memory-sampler.py"), "--pid", String(runtime.pid), "--out", outDir],
      { windowsHide: true, stdio: ["ignore", log, log] });
    fs.closeSync(log);
    samplerExit = new Promise((resolve, reject) => {
      sampler.once("error", reject); sampler.once("exit", (code) => resolve(code));
    });
    await poll(() => fs.existsSync(path.join(outDir, "sampler.ready")), Boolean, 15000);
    await delay(10000);

    async function batch(name, count, dynamic) {
      stage(name);
      const started = Date.now();
      const tasks = await win.evaluate((urls) => window.api.import.enqueue(urls.map((input) => ({ kind: "url", input }))),
        Array.from({ length: count }, (_, i) => `${origin}/${name}-${dynamic ? "dynamic" : "static"}-${i}`));
      assert.equal(tasks.length, count);
      let maxRunning = 0;
      const results = await poll(async () => {
        const rows = await win.evaluate(() => window.api.import.list());
        const selected = tasks.map((task) => rows.find((row) => row.id === task.id));
        maxRunning = Math.max(maxRunning, selected.filter((row) => row?.status === "processing").length);
        return selected;
      }, (rows) => rows.every((row) => ["completed", "failed", "duplicate", "canceled"].includes(row?.status)));
      for (const row of results) {
        assert.equal(row.status, "completed", row.error);
        const result = await win.evaluate((id) => window.api.webCapture.versions(id), row.resultItemId);
        assert.equal(result.ok, true, result.error);
        const expected = variant === "previous" ? "crawl4ai/0.9.3" : `crawl4ai/0.9.3-${dynamic ? "electron" : "static"}`;
        assert.equal(result.data.versions[0].engineVersion, expected);
        assert.ok(result.data.content.includes(dynamic ? "动态正文已加载" : "静态正文已就绪"));
        assert.ok(result.data.content.includes('print("归知")'));
        captures.push({ name, itemId: row.resultItemId, engine: expected, content: result.data.content });
      }
      assert.ok(count === 1 || maxRunning === 2, `并发未达到 2：${maxRunning}`);
      events.push({ batch: name, durationMs: Date.now() - started, maxRunning });
    }
    async function cooldown(name) {
      stage(`${name}-cooldown`);
      const started = Date.now();
      await poll(async () => {
        const result = await win.evaluate(() => window.api.webCapture.status());
        assert.equal(result.ok, true, result.error);
        return result.data;
      }, (status) => status.available && !status.running, 85000);
      events.push({ cooldown: name, elapsedMs: Date.now() - started });
      stage(`${name}-idle`);
      await delay(8000);
    }
    await batch("static", 1, false);
    await cooldown("static");
    await batch("dynamic", 2, true);
    await cooldown("dynamic");
    for (let i = 1; i <= 5; i++) {
      await batch(`repeat-${i}`, 2, true);
      stage(`repeat-${i}-settled`);
      await delay(2000);
    }
    await cooldown("repeat");
    stage("complete");
    fs.writeFileSync(path.join(outDir, "memory-result.json"), JSON.stringify({
      passed: true, variant, runtime, captures, events, maxProxyRequests: maxActive,
    }, null, 2));
    await shot("memory-complete");
  } finally {
    fs.writeFileSync(path.join(outDir, "sampler.stop"), "");
    if (sampler) assert.equal(await samplerExit, 0, "内存采样器失败");
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
