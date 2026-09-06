import assert from "node:assert/strict";
import fs from "node:fs";
import http from "node:http";
import path from "node:path";

const origin = "http://guizhi-acceptance.example";
async function poll(action, ready, timeout = 180000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const result = await action();
    if (ready(result)) return result;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error("验收操作超时");
}
export default async function ({ win, app, shot, outDir }) {
  let revision = 1;
  const requested = [];
  const server = http.createServer((req, res) => {
    const url = new URL(req.url, origin);
    requested.push(url.href);
    if (url.origin !== origin) {
      res.writeHead(403).end("验收代理拒绝其他目标");
      return;
    }
    if (url.pathname === "/robots.txt") {
      res
        .writeHead(200, { "content-type": "text/plain" })
        .end("User-agent: *\nAllow: /\nDisallow: /blocked\n");
      return;
    }
    if (url.pathname === "/slow") {
      req.on("close", () => res.destroy());
      return;
    }
    res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
    res.end(
      `<title>验收网页 ${url.pathname} v${revision}</title><nav>导航广告</nav><article><h1>中文组件说明</h1><p>正文版本 ${revision}：归知保存网页与人工编辑。</p><p id="dynamic">加载中</p><table><tr><th>配置</th><th>值</th></tr><tr><td>并发</td><td>2</td></tr></table><pre><code>print("归知")</code></pre><a href="/docs/b">下一页</a><a href="/outside">目录外</a></article><script>setTimeout(()=>document.querySelector('#dynamic').textContent='动态正文已加载',100)</script>`,
    );
  });
  server.on("connect", (_req, socket) =>
    socket.end("HTTP/1.1 403 Forbidden\r\n\r\n"),
  );
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const call = (operation, argument) =>
    win.evaluate(
      async ({ operation, argument }) => {
        const result = await window.api.webCapture[operation](argument);
        if (!result.ok) throw new Error(result.error);
        return result.data;
      },
      { operation, argument },
    );
  const finish = (id) =>
    poll(
      () => call("get", id),
      (r) => r.job.status === "completed",
    );
  try {
    await win.evaluate(async (port) => {
      await window.api.settings.set({
        networkProxy: {
          mode: "manual",
          protocol: "http",
          host: "127.0.0.1",
          port,
          username: "",
          password: "",
          bypass: "localhost,127.0.0.1",
        },
      });
    }, server.address().port);
    // 通过真实导入队列验证默认单页入口，不替换任何 IPC 或网络服务。
    const enqueued = await win.evaluate(
      (url) => window.api.import.enqueue([{ kind: "url", input: url }]),
      `${origin}/single`,
    );
    const single = await poll(
      () => win.evaluate(() => window.api.import.list()),
      (tasks) =>
        tasks.some(
          (t) =>
            t.id === enqueued[0].id &&
            ["completed", "failed"].includes(t.status),
        ),
    );
    const singleTask = single.find((t) => t.id === enqueued[0].id);
    assert.equal(singleTask.status, "completed", singleTask.error);
    const singleVersions = await call("versions", singleTask.resultItemId);
    assert.equal(singleVersions.versions[0].engineVersion, "crawl4ai/0.9.3");
    assert.ok(singleVersions.content.includes("动态正文已加载"));
    const input = {
      purpose: "documents",
      seeds: [
        { url: `${origin}/docs/a`, mode: "directory", directory: "/docs/" },
      ],
      maxPages: 2,
      maxDepth: 2,
      duplicatePolicy: "skip",
    };
    const first = await finish(await call("create", input));
    assert.equal(first.job.counts.added, 2, JSON.stringify(first));
    assert.equal((await win.evaluate(() => window.api.knowledge.counts())).byPlatform.web, 3);
    assert.ok(!requested.includes(`${origin}/outside`));
    const duplicate = await finish(await call("create", input));
    assert.equal(duplicate.job.counts.duplicate, 2);
    const itemId = first.pages.find((p) => p.url.endsWith("/b")).itemId;
    await win.evaluate(
      (id) =>
        window.api.knowledge.update(id, {
          title: "人工标题",
          content: "人工正文",
          isFavorite: true,
          tagNames: ["保留标签"],
          summary: "保留摘要",
        }),
      itemId,
    );
    revision = 2;
    const update = await finish(
      await call("create", { ...input, duplicatePolicy: "update" }),
    );
    assert.equal(update.job.counts.updated, 1);
    assert.equal(update.job.counts["pending-version"], 1);
    const pending = await call("versions", itemId);
    assert.equal(pending.content, "人工正文");
    assert.equal(pending.pendingVersion, true);
    const versionId = pending.versions.find((v) => v.kind === "remote").id;
    await win.evaluate(
      (id) => window.api.knowledge.update(id, { content: "比较期间再次编辑" }),
      itemId,
    );
    const conflict = await win.evaluate(
      (input) => window.api.webCapture.adopt(input),
      {
        itemId,
        versionId,
        expectedContentHash: pending.contentHash,
        expectedTitle: pending.title,
      },
    );
    assert.equal(conflict.ok, false);
    const current = await call("versions", itemId);
    await call("adopt", {
      itemId,
      versionId,
      expectedContentHash: current.contentHash,
      expectedTitle: current.title,
    });
    const adopted = await win.evaluate(
      (id) => window.api.knowledge.get(id),
      itemId,
    );
    assert.equal(adopted.title, "人工标题");
    assert.equal(adopted.isFavorite, true);
    assert.ok(adopted.tags.some((tag) => tag.name === "保留标签"));
    assert.equal(adopted.summary, "保留摘要");
    assert.ok(adopted.content.includes("正文版本 2"));
    const history = await call("versions", itemId);
    assert.equal(history.summaryStale, true);
    assert.ok(
      history.versions.some(
        (v) => v.kind === "local" && v.markdown === "比较期间再次编辑",
      ),
    );
    // 运行中的真实队列写入快照，恢复后必须等待用户继续。
    const interruptedJobId = await call("create", {
      ...input,
      seeds: [{ url: `${origin}/slow`, mode: "page" }],
      maxPages: 1,
    });
    await poll(
      () => call("get", interruptedJobId),
      (r) => r.pages.some((p) => p.status === "running"),
    );
    const backup = await win.evaluate(() => window.api.backup.create());
    assert.equal(backup.success, true, backup.error);
    const refused = await win.evaluate(
      (name) => window.api.backup.restore(name),
      backup.backup.fileName,
    );
    assert.equal(refused.success, false);
    await call("pause", interruptedJobId);
    await poll(
      () => call("get", interruptedJobId),
      (r) => r.pages.every((p) => p.status !== "running"),
    );
    await win.evaluate(
      (id) =>
        window.api.knowledge.update(id, {
          content: "备份之后的改动，恢复时应撤回",
        }),
      itemId,
    );
    fs.writeFileSync(
      path.join(outDir, "capture.json"),
      JSON.stringify(
        {
          singleTask,
          first,
          duplicate,
          update,
          adopted,
          history,
          interruptedJobId,
          backup,
          requested,
          restoreGuard: refused,
        },
        null,
        2,
      ),
    );
    await shot("capture-before-restore");
    // 恢复业务路径照常执行；重启由下一次 pnpm shot 控制，避免自动化端口被继承。
    await app.evaluate(({ app }) => {
      app.relaunch = () => {};
    });
    const closed = app.waitForEvent("close", { timeout: 30000 });
    const restored = await win.evaluate(
      (name) => window.api.backup.restore(name),
      backup.backup.fileName,
    );
    assert.equal(restored.success, true, restored.error);
    await closed;
    fs.writeFileSync(
      path.join(outDir, "restore-result.json"),
      JSON.stringify(restored, null, 2),
    );
  } finally {
    server.closeAllConnections();
    await new Promise((resolve) => server.close(resolve));
  }
}
