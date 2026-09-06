/** 只在 pnpm shot 的临时实例里提供失败/版本展示夹具，不访问真实网站或用户库。 */
export default async function ({ win, app, shot }) {
  await app.evaluate(({ ipcMain, BrowserWindow }) => {
    BrowserWindow.getAllWindows()[0].setSize(1100, 850);
    const job = {
      id: "preview-job",
      input: {
        purpose: "documents",
        seeds: [{ url: "https://example.invalid/docs/", mode: "directory" }],
        maxPages: 50,
        maxDepth: 2,
      },
      status: "paused",
      createdAt: Date.now(),
      updatedAt: Date.now(),
      counts: { added: 1, failed: 1, pending: 1 },
      error: "robots.txt 暂时无法读取，来源已暂停，可重试。",
    };
    ipcMain.removeHandler("crawl:list");
    ipcMain.handle("crawl:list", () => ({ ok: true, data: [job] }));
    ipcMain.removeHandler("crawl:get");
    ipcMain.handle("crawl:get", () => ({
      ok: true,
      data: {
        job,
        pages: [
          {
            id: "p1",
            jobId: job.id,
            url: "https://example.invalid/docs/a",
            status: "added",
          },
          {
            id: "p2",
            jobId: job.id,
            url: "https://example.invalid/docs/b",
            status: "failed",
            error: "HTTP 503：网站暂时不可用",
          },
        ],
      },
    }));
    ipcMain.removeHandler("web:versions");
    ipcMain.handle("web:versions", (_e, itemId) => ({
      ok: true,
      data: {
        content: "# 我的知识笔记\n\n这是手动补充的正文，需要保留。",
        title: "版本比较样本",
        contentHash: "a".repeat(64),
        summaryStale: true,
        versions: [
          {
            id: "preview-version",
            itemId,
            sourceUrl: "https://example.invalid/docs/a",
            title: "网站更新标题",
            markdown:
              "# 网站更新标题\n\n网站增加了新的章节。\n\n| 参数 | 值 |\n| --- | --- |\n| 并发 | 2 |",
            contentHash: "b".repeat(64),
            capturedAt: Date.now(),
            engineVersion: "crawl4ai/0.9.3",
            complete: true,
            kind: "remote",
          },
        ],
      },
    }));
  });
  const language = async (lang) => {
    await win.evaluate((lang) => {
      const s = JSON.parse(
        localStorage.getItem("guizhi-settings") || '{"state":{}}',
      );
      s.state.language = lang;
      localStorage.setItem("guizhi-settings", JSON.stringify(s));
      localStorage.setItem("guizhi-setup-dismissed", "1");
      localStorage.setItem("guizhi-migration-dismissed", "1");
    }, lang);
    await win.reload();
    await win.getByTestId("topbar-search").waitFor();
  };
  await language("zh");
  await win.getByRole("button", { name: "导入", exact: true }).click();
  await win.getByRole("button", { name: "导入文档站", exact: true }).click();
  await win.getByRole("button", { name: /example.invalid\/docs\/ ·/ }).click();
  await win.getByText("HTTP 503：网站暂时不可用", { exact: true }).waitFor();
  await shot("documents-partial-failure");
  await win.evaluate(() =>
    window.api.knowledge.create({
      title: "版本比较样本",
      content: "# 我的知识笔记\n\n这是手动补充的正文，需要保留。",
      itemType: "webpage",
    }),
  );
  await win.getByRole("button", { name: "知识库", exact: true }).click();
  await win.getByText("版本比较样本", { exact: true }).first().click();
  await win.getByRole("button", { name: /原文版本/ }).click();
  await win.getByRole("button", { name: "原文版本", exact: true }).click();
  await win.getByRole("option", { name: /crawl4ai/ }).click();
  await win.getByText("所选版本 ·", { exact: false }).waitFor();
  await shot("source-version-compare");
  await language("en");
  await win.getByRole("button", { name: "Imports", exact: true }).click();
  await win
    .getByRole("button", { name: "Import documentation site", exact: true })
    .click();
  await shot("documents-en");
  await app.evaluate(({ ipcMain }) => {
    ipcMain.removeHandler("crawl:list");
    ipcMain.handle("crawl:list", () => ({
      ok: false,
      error: "Temporary database read failure; please retry.",
    }));
  });
  await win
    .getByText("Temporary database read failure; please retry.", {
      exact: true,
    })
    .waitFor();
  await shot("documents-load-error-en");
}
