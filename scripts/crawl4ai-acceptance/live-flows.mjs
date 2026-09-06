import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

export default async function ({ win, app, shot, outDir, userDataDir }) {
  const results = {};
  const only = new Set(
    (process.env.GUIZHI_LIVE_ONLY || "").split(",").filter(Boolean),
  );
  const topic =
    process.env.GUIZHI_LIVE_TOPIC ||
    "Python asyncio 的任务创建、取消与超时处理应该如何使用？";
  const record = (name, value) =>
    fs.writeFileSync(
      path.join(outDir, `${name}.json`),
      JSON.stringify(value, null, 2),
    );
  const attempt = async (name, action) => {
    if (only.size && !only.has(name)) return;
    console.log(`联网验收 / ${name}`);
    try {
      results[name] = { passed: true, ...(await action()) };
    } catch (error) {
      results[name] = {
        passed: false,
        error: String(error.message).slice(0, 1200),
      };
    }
    record("progress", results);
    return results[name];
  };
  const poll = async (action, ready, timeout = 240000) => {
    const end = Date.now() + timeout;
    while (Date.now() < end) {
      const value = await action();
      if (ready(value)) return value;
      await new Promise((resolve) => setTimeout(resolve, 700));
    }
    throw new Error("联网流程等待超时");
  };
  const web = (operation, input) =>
    win.evaluate(
      async ({ operation, input }) => {
        const result = await window.api.webCapture[operation](input);
        if (!result.ok) throw new Error(result.error);
        return result.data;
      },
      { operation, input },
    );
  // 机密配置和会话副本只进入截图工具拥有的临时 profile，绝不写入验收产物。
  const target = await app.evaluate(({ app }) => app.getPath("userData"));
  assert.equal(path.resolve(target), path.resolve(userDataDir));
  assert.ok(
    path.basename(target).startsWith("guizhi-shot-") &&
      path.dirname(target) === os.tmpdir(),
  );
  const source = path.join(process.env.APPDATA, "GuiZhi");
  fs.mkdirSync(path.join(target, "config"), { recursive: true });
  fs.copyFileSync(
    path.join(source, "config/ai-models.json"),
    path.join(target, "config/ai-models.json"),
  );
  for (const platform of ["xiaohongshu", "douyin", "linuxdo"]) {
    const folder = path.join(
      "Partitions",
      `guizhi-platform-capture-${platform}`,
    );
    if (fs.existsSync(path.join(source, folder)))
      fs.cpSync(path.join(source, folder), path.join(target, folder), {
        recursive: true,
      });
  }
  fs.mkdirSync(path.join(target, "browser-capture"), { recursive: true });
  const state = path.join(source, "browser-capture/session-status.json");
  if (fs.existsSync(state))
    fs.copyFileSync(
      state,
      path.join(target, "browser-capture/session-status.json"),
    );
  const config = JSON.parse(
    fs.readFileSync(path.join(target, "config/ai-models.json"), "utf8"),
  );
  const configuration = {
    routes: Object.fromEntries(
      Object.entries(config.modelRouteDefaults).map(([route, id]) => [
        route,
        config.models.find((m) => m.id === id)?.model,
      ]),
    ),
    sessionsCopied: true,
    networkMode: "explicit-existing-local-proxy",
  };
  record("configuration-summary", configuration);
  await win.evaluate(() => {
    const settings = JSON.parse(
      localStorage.getItem("guizhi-settings") || '{"state":{}}',
    );
    settings.state.language = "zh";
    localStorage.setItem("guizhi-settings", JSON.stringify(settings));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  // 当前主模型位于已有的 Tailscale 服务，只在测试 profile 中保持该地址直连。
  const modelHost = new URL(
    config.models.find((m) => m.id === config.modelRouteDefaults.mainText)
      .apiUrl,
  ).hostname;
  await win.evaluate(
    (bypass) =>
      window.api.settings.set({
        networkProxy: {
          mode: "manual",
          protocol: "http",
          host: "127.0.0.1",
          port: 7897,
          bypass,
          username: "",
          password: "",
        },
      }),
    `<local>,localhost,127.0.0.1,::1,${modelHost}`,
  );
  await attempt("single-import", async () => {
    const [task] = await win.evaluate(() =>
      window.api.import.enqueue([
        {
          kind: "url",
          input: "https://docs.python.org/zh-cn/3/tutorial/introduction.html",
        },
      ]),
    );
    const tasks = await poll(
      () => win.evaluate(() => window.api.import.list()),
      (list) =>
        list.some(
          (t) => t.id === task.id && ["completed", "failed"].includes(t.status),
        ),
    );
    const done = tasks.find((t) => t.id === task.id);
    assert.equal(done.status, "completed", done.error);
    const versions = await web("versions", done.resultItemId);
    assert.equal(versions.versions[0].engineVersion, "crawl4ai/0.9.3");
    assert.ok(
      versions.content.includes("数字") && versions.content.includes("```"),
    );
    record("single-import-detail", { task: done, versions });
    return {
      title: versions.title,
      engine: versions.versions[0].engineVersion,
      characters: versions.content.length,
    };
  });
  await attempt("documentation-site", async () => {
    const collection = await win.evaluate(() =>
      window.api.collection.create({ name: "联网文档验收" }),
    );
    const id = await web("create", {
      purpose: "documents",
      seeds: [
        {
          url: "https://docs.python.org/3/tutorial/index.html",
          mode: "directory",
          directory: "/3/tutorial/",
        },
      ],
      maxPages: 3,
      maxDepth: 1,
      collectionId: collection.id,
      duplicatePolicy: "skip",
    });
    const done = await poll(
      () => web("get", id),
      (r) =>
        ["completed", "paused", "failed", "canceled"].includes(r.job.status),
    );
    record("documentation-detail", done);
    assert.equal(done.job.status, "completed", JSON.stringify(done.job));
    assert.equal(
      done.pages.filter((p) => p.status === "added").length,
      3,
      JSON.stringify(done.job.counts),
    );
    for (const page of done.pages)
      assert.ok(page.url.startsWith("https://docs.python.org/3/tutorial/"));
    return {
      jobId: id,
      counts: done.job.counts,
      pages: done.pages.map((p) => ({
        url: p.url,
        title: p.result?.title,
        status: p.status,
      })),
    };
  });
  const research = async (name, input, generate) =>
    attempt(name, async () => {
      const created = await win.evaluate(
        (input) => window.api.research.create(input),
        input,
      );
      let detail = await poll(
        () => win.evaluate((id) => window.api.research.get(id), created.id),
        (r) => r.run.status !== "collecting",
        360000,
      );
      record(`${name}-detail`, detail);
      assert.equal(detail.run.timeScope, "all");
      assert.ok(
        ["ready", "partial"].includes(detail.run.status),
        JSON.stringify(detail.sources),
      );
      assert.ok(
        detail.documents.some((d) => d.status === "ready"),
        "No complete research document",
      );
      assert.ok(detail.candidates.length <= 20);
      if (generate) {
        await win.evaluate(
          (id) => window.api.research.generateReport(id),
          created.id,
        );
        detail = await poll(
          () => win.evaluate((id) => window.api.research.get(id), created.id),
          (r) => r.run.reportStatus !== "generating",
          360000,
        );
        record(`${name}-detail`, detail);
        assert.equal(
          detail.run.reportStatus,
          "ready",
          detail.run.reportError || JSON.stringify(detail.run),
        );
        assert.ok(detail.run.reportMarkdown.includes("不限时间"));
        assert.ok(/\[R\d+\]/.test(detail.run.reportMarkdown));
        assert.ok(!detail.run.reportMarkdown.includes("最近 30 天"));
        fs.writeFileSync(
          path.join(outDir, `${name}-report.md`),
          detail.run.reportMarkdown,
        );
        const evidence = await win.evaluate(
          (id) => window.api.research.evidence(id),
          created.id,
        );
        record(`${name}-evidence`, evidence);
        const saved = await win.evaluate(
          (id) => window.api.research.saveToKnowledge(id),
          created.id,
        );
        assert.ok(saved.itemId);
        const item = await win.evaluate(
          (id) => window.api.knowledge.get(id),
          saved.itemId,
        );
        assert.ok(item.content.includes("引用来源"));
      }
      return {
        runId: created.id,
        status: detail.run.status,
        candidates: detail.candidates.length,
        documents: detail.documents.length,
        documentResults: detail.documents.map(({ source, status, error }) => ({
          source,
          status,
          error,
        })),
        sourceResults: detail.sources.map(
          ({ source, status, collectedCount, error }) => ({
            source,
            status,
            collectedCount,
            error,
          }),
        ),
        reportStatus: detail.run.reportStatus,
        plan: detail.run.context?.plan,
      };
    });
  await research(
    "web-research",
    {
      topic,
      sources: ["web"],
      timeScope: "all",
      dayRange: 30,
      depth: process.env.GUIZHI_LIVE_DEEP ? "deep" : "quick",
      webSeeds: [
        {
          url: "https://docs.python.org/zh-cn/3/library/asyncio-task.html",
          mode: "page",
        },
        {
          url: "https://docs.python.org/zh-cn/3/library/asyncio-sync.html",
          mode: "page",
        },
      ],
    },
    true,
  );
  await research(
    "directory-research",
    {
      topic: "Python 入门教程中的数字、字符串、列表与控制流",
      sources: ["web"],
      timeScope: "all",
      dayRange: 30,
      depth: "quick",
      webSeeds: [
        {
          url: "https://docs.python.org/zh-cn/3/tutorial/index.html",
          mode: "directory",
          directory: "/zh-cn/3/tutorial/",
        },
      ],
    },
    false,
  );
  await research(
    "mixed-research",
    {
      topic: "Python asyncio 入门",
      sources: ["web", "bilibili"],
      timeScope: "all",
      dayRange: 30,
      depth: "quick",
      webSeeds: [
        {
          url: "https://docs.python.org/zh-cn/3/library/asyncio-task.html",
          mode: "page",
        },
      ],
    },
    false,
  );
  for (const platform of ["douyin"]) {
    await attempt(`platform-${platform}`, async () => {
      const page = await Promise.race([
        win.evaluate(
          (platform) =>
            window.api.platformCapture.search({
              platform,
              keyword: "Python",
              limit: 3,
            }),
          platform,
        ),
        new Promise((_, reject) =>
          setTimeout(() => reject(new Error("平台查询超时")), 90000),
        ),
      ]);
      assert.ok(page.items.length, "平台未返回候选");
      return {
        count: page.items.length,
        items: page.items.slice(0, 3).map((i) => ({
          title: i.title,
          url: i.url.split("?")[0],
          mediaType: i.mediaType,
        })),
      };
    });
    await win
      .evaluate(() => window.api.platformCapture.cancelDiscovery())
      .catch(() => {});
  }
  await attempt("forum-public", async () => {
    const [task] = await win.evaluate(() =>
      window.api.import.enqueue([
        { kind: "url", input: "https://linux.do/t/topic/1" },
      ]),
    );
    const tasks = await poll(
      () => win.evaluate(() => window.api.import.list()),
      (list) =>
        list.some(
          (t) => t.id === task.id && ["completed", "failed"].includes(t.status),
        ),
      120000,
    );
    const done = tasks.find((t) => t.id === task.id);
    record("forum-detail", done);
    assert.equal(done.status, "completed", done.error);
    return { status: done.status };
  });
  await attempt("research-ui", async () => {
    await win.getByRole("button", { name: "研究", exact: true }).click();
    await win.getByText(topic, { exact: true }).first().click();
    await win.getByText("不限时间", { exact: false }).first().waitFor();
    await shot("web-research-live");
    await win.getByRole("button", { name: "研究报告", exact: true }).click();
    await shot("web-research-report-live");
    return {};
  });
  const backup = await win.evaluate(() => window.api.backup.create());
  assert.equal(backup.success, true, backup.error);
  const file = path.resolve(backup.backup.path);
  assert.ok(file.startsWith(path.resolve(userDataDir) + path.sep));
  fs.copyFileSync(file, path.join(outDir, "live-test-knowledge.db"));
  record("result", {
    scope: "Windows x64 live network flows",
    results,
    modelRoutes: configuration.routes,
    credentialsExported: false,
  });
}
