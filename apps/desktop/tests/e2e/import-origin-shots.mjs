import assert from "node:assert/strict";

// pnpm shot 的临时用户目录内使用合成任务，验证来源交互，不触碰真实库或手机投递。
export default async ({ win, app, shot }) => {
  await app.evaluate(({ ipcMain, BrowserWindow }) => {
    BrowserWindow.getAllWindows()[0].setSize(1440, 960);
    const now = Date.now();
    const rows = [
      ["phone-running", "mobile", "processing", "手机分享：构建个人知识库的实践"],
      ["phone-failed", "mobile", "failed", "手机分享：视频摘要与笔记整理"],
      ["desktop-failed", "desktop", "failed", "桌面采集：网页读取失败示例"],
      ["phone-done", "mobile", "completed", "手机分享：适合日常执行的运动计划"],
      ["desktop-done", "desktop", "completed", "桌面导入：本地 Markdown 学习笔记"],
    ].map(([id, origin, status, displayName], index) => ({
      id, origin, status, displayName, sourceKind: "url", itemType: "video",
      sourceInput: `https://example.com/${id}`, captureStrategy: "standard", commentLimit: 0,
      createdAt: now - index * 60_000, updatedAt: now - index * 30_000,
      receivedAt: origin === "mobile" ? now - index * 60_000 : null,
      stage: status === "processing" ? "transcribing" : null,
      error: status === "failed" ? "网络暂时不可用，请重试" : null,
    }));
    globalThis.importOriginPreview = { rows, calls: [] };
    const match = (row, query = {}) => {
      if (query.origin && query.origin !== "all" && query.origin !== row.origin) return false;
      if (query.query && !`${row.displayName} ${row.sourceInput} ${row.error ?? ""}`.includes(query.query)) return false;
      if (query.status === "active") return ["pending", "processing"].includes(row.status);
      if (query.status === "degraded") return row.status === "completed" && row.warning;
      return !query.status || query.status === "all" || row.status === query.status;
    };
    for (const channel of ["import:list", "import:queueState", "import:retry", "import:clearTerminalPreview", "import:clearTerminal"]) {
      ipcMain.removeHandler(channel);
    }
    ipcMain.handle("import:list", (_event, query = {}) => {
      const current = globalThis.importOriginPreview.rows;
      const entries = current.filter(row => match(row, query));
      const scoped = current.filter(row => match(row, { ...query, status: "all" }));
      return { entries, active: scoped.filter(row => ["pending", "processing"].includes(row.status)),
        nextCursor: null, total: entries.length, degradedCount: 0,
        counts: Object.fromEntries(["pending", "processing", "failed", "completed", "canceled", "duplicate"].map(status => [status, scoped.filter(row => row.status === status).length])) };
    });
    ipcMain.handle("import:queueState", () => ({ paused: false, runningCount: 1, pendingCount: 0, concurrency: 2 }));
    ipcMain.handle("import:retry", (_event, id) => {
      globalThis.importOriginPreview.calls.push({ action: "retry", id });
      return rows.find(row => row.id === id);
    });
    for (const action of ["clearTerminalPreview", "clearTerminal"]) {
      ipcMain.handle(`import:${action}`, (_event, query) => {
        const state = globalThis.importOriginPreview;
        state.calls.push({ action, query });
        const selected = state.rows.filter(row => !["pending", "processing"].includes(row.status) && match(row, query.scope === "all" ? {} : query));
        if (action === "clearTerminal") state.rows = state.rows.filter(row => !selected.includes(row));
        return { count: selected.length, preview: action === "clearTerminalPreview" };
      });
    }
  });
  await win.evaluate(() => {
    const stored = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(stored.state, { language: "zh", themeMode: "dark", isDarkMode: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  await win.getByRole("button", { name: "导入", exact: true }).click();
  await win.getByText("桌面导入：本地 Markdown 学习笔记", { exact: true }).waitFor();
  await shot("import-origin-all");
  await win.getByRole("button", { name: "手机端", exact: true }).click();
  await win.getByRole("button", { name: "重试当前失败 1", exact: true }).waitFor();
  assert.equal(await win.getByText("桌面采集：网页读取失败示例", { exact: true }).count(), 0);
  assert.equal(await win.getByText("手机提交", { exact: true }).count(), 3);
  await shot("import-origin-mobile");
  // 详情入口在行悬停操作栏中，使用键盘验证可访问入口。
  await win.getByRole("button", { name: "任务详情", exact: true }).first().focus();
  await win.keyboard.press("Enter");
  await win.getByText("桌面接收时间", { exact: true }).waitFor();
  await win.waitForFunction(() => {
    const dialog = document.querySelector('[role="dialog"], [role="alertdialog"]');
    return dialog && getComputedStyle(dialog).opacity === "1";
  });
  await shot("import-origin-detail");
  await win.keyboard.press("Escape");
  await win.getByRole("button", { name: "重试当前失败 1", exact: true }).click();
  await win.getByRole("button", { name: "清理当前筛选", exact: true }).click();
  await win.getByText(/范围：手机端/).waitFor();
  await win.waitForFunction(() => {
    const dialog = document.querySelector('[role="dialog"], [role="alertdialog"]');
    return dialog && getComputedStyle(dialog).opacity === "1";
  });
  await shot("import-origin-clear-confirm");
  await win.getByRole("button", { name: "删除 2 条", exact: true }).click();
  await win.getByText("手机分享：视频摘要与笔记整理", { exact: true }).waitFor({ state: "detached" });
  await win.getByRole("button", { name: "桌面端", exact: true }).click();
  await win.getByText("桌面采集：网页读取失败示例", { exact: true }).waitFor();
  const calls = await app.evaluate(() => globalThis.importOriginPreview.calls);
  assert.deepEqual(calls.filter(call => call.action === "retry").map(call => call.id), ["phone-failed"]);
  assert.deepEqual(calls.find(call => call.action === "clearTerminal").query,
    { scope: "filtered", origin: "mobile", status: "all", query: "" });
  const search = win.getByPlaceholder("搜索标题或链接");
  await search.fill("没有匹配的内容");
  await win.getByText("桌面采集：网页读取失败示例", { exact: true }).waitFor({ state: "detached" });
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1100, 800));
  await shot("import-origin-filtered-empty");
  console.log("来源切换、详情、重试范围和清理范围验证通过");
};
