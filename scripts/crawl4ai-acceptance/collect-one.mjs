import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";

export default async function ({ win, outDir, shot }) {
  const server = http.createServer((request, response) => {
    if (!request.url.startsWith("http://guizhi-acceptance.example/")) {
      response.writeHead(403).end();
      return;
    }
    response.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    response.end(
      "<article><h1>退出前采集验收</h1><p>独立 Python 与浏览器采集完成，退出应用应回收本次进程。</p></article>",
    );
  });
  server.on("connect", (_request, socket) =>
    socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"),
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  try {
    await win.evaluate(
      (port) =>
        window.api.settings.set({
          networkProxy: {
            mode: "manual",
            protocol: "http",
            host: "127.0.0.1",
            port,
            username: "",
            password: "",
            bypass: "localhost,127.0.0.1",
          },
        }),
      server.address().port,
    );
    const [task] = await win.evaluate(() =>
      window.api.import.enqueue([
        { kind: "url", input: "http://guizhi-acceptance.example/exit" },
      ]),
    );
    let completed;
    for (const deadline = Date.now() + 90000; Date.now() < deadline;) {
      const tasks = await win.evaluate(() => window.api.import.list());
      completed = tasks.find((item) => item.id === task.id);
      if (["completed", "failed"].includes(completed?.status)) break;
      await new Promise((resolve) => setTimeout(resolve, 300));
    }
    assert.equal(completed?.status, "completed", completed?.error);
    const versions = await win.evaluate(
      (id) => window.api.webCapture.versions(id),
      completed.resultItemId,
    );
    assert.equal(versions.data.versions[0].engineVersion, "crawl4ai/0.9.3");
    assert.ok(versions.data.content.includes("退出应用应回收本次进程"));
    fs.writeFileSync(
      path.join(outDir, "collect-one.json"),
      JSON.stringify({ passed: true, task: completed, versions }, null, 2),
    );
    await shot("before-normal-exit");
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
