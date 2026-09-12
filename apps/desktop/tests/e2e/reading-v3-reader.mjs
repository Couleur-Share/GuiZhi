import assert from "node:assert/strict";
export default async ({ win, app, shot }) => {
  await win.evaluate(() => {
    const saved = JSON.parse(
      localStorage.getItem("guizhi-settings") || '{"state":{}}',
    );
    Object.assign(saved.state, {
      language: "zh",
      themeMode: "dark",
      isDarkMode: true,
    });
    localStorage.setItem("guizhi-settings", JSON.stringify(saved));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.waitForTimeout(1500);
  const item = await app.evaluate(() => globalThis.readingV3Fixture("seed"));
  await win.reload();
  await win
    .getByTestId("item-list")
    .getByText(item.title, { exact: true })
    .click();
  await win.getByRole("button", { name: "AI 阅读", exact: true }).click();
  const host = win.locator("[data-reading-view-id]");
  await host.waitFor();
  const nativeCount = () =>
    app.evaluate(
      ({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()
          .find((w) => w.webContents.getURL().startsWith("http"))
          .contentView.children.filter((c) =>
            c.webContents?.getURL().startsWith("guizhi-reading:"),
          ).length,
    );
  await win.waitForTimeout(600);
  assert.equal(await nativeCount(), 1);
  const inner = (code) =>
    app.evaluate(async ({ BrowserWindow }, code) => {
      const child = BrowserWindow.getAllWindows()
        .find((w) => w.webContents.getURL().startsWith("http"))
        .contentView.children.find((c) =>
          c.webContents?.getURL().startsWith("guizhi-reading:"),
        );
      return child.webContents.mainFrame.frames[0].executeJavaScript(code);
    }, code);
  assert.equal(
    await inner(
      "document.getElementById('add').click();document.getElementById('value').textContent",
    ),
    "1",
  );
  await shot("reader-v3");
  await win.getByRole("button", { name: "目录", exact: true }).last().click();
  await win.getByRole("navigation", { name: "文章章节目录" }).waitFor();
  await win.waitForTimeout(200);
  assert.equal(await nativeCount(), 0);
  await shot("reader-v3-toc");
  await win
    .getByRole("navigation", { name: "文章章节目录" })
    .getByRole("button", { name: "完整正文", exact: true })
    .click();
  await win.waitForTimeout(200);
  assert.equal(await nativeCount(), 1);
  await win.getByRole("button", { name: "阅读页设置", exact: true }).click();
  await win.getByRole("menu").waitFor();
  await win.waitForTimeout(200);
  assert.equal(await nativeCount(), 0);
  await shot("reader-v3-menu");
  await win.keyboard.press("Escape");
  const task = await app.evaluate(
    (_electron, itemId) => globalThis.readingV3Fixture("startPreview", { itemId }),
    item.id,
  );
  await win.getByRole("button", { name: "预览新稿", exact: true }).waitFor();
  assert.equal(
    await inner("document.body.textContent.includes('新稿预览第一部分')"),
    false,
  );
  await win.getByRole("button", { name: "预览新稿", exact: true }).click();
  await win.waitForTimeout(900);
  assert.equal(
    await inner("document.body.textContent.includes('新稿预览第一部分')"),
    true,
  );
  assert.equal(
    await inner(
      "document.getElementById('add').click();document.getElementById('value').textContent",
    ),
    "0",
  );
  await app.evaluate(
    (_electron, taskId) => globalThis.readingV3Fixture("advancePreview", { taskId }),
    task.id,
  );
  await win.waitForTimeout(900);
  assert.equal(
    await inner("document.body.textContent.includes('新稿第二部分')"),
    true,
  );
  await shot("reader-v3-progressive-preview");
  await win.getByRole("button", { name: "返回当前版", exact: true }).click();
  await win.waitForTimeout(400);
  await app.evaluate(
    (_electron, taskId) =>
      globalThis.readingV3Fixture("advancePreview", { taskId, publish: true }),
    task.id,
  );
  await win
    .getByRole("button", { name: "新版已完成 · 切换", exact: true })
    .waitFor();
  assert.equal(
    await inner("document.body.textContent.includes('新稿预览第一部分')"),
    false,
  );
  await shot("reader-v3-completed-keeps-current");
  await win
    .getByRole("button", { name: "新版已完成 · 切换", exact: true })
    .click();
  await win.waitForTimeout(400);
  assert.equal(
    await inner("document.body.textContent.includes('新稿第二部分')"),
    true,
  );
  const first = await app.evaluate(() =>
    globalThis.readingV3Fixture("seed", { preview: true }),
  );
  await win.reload();
  await win
    .getByTestId("item-list")
    .getByText(first.title, { exact: true })
    .click();
  await win.getByRole("button", { name: "AI 阅读", exact: true }).click();
  await host.waitFor();
  await win.waitForTimeout(600);
  assert.equal(await nativeCount(), 1);
  assert.equal(
    await inner(
      "document.getElementById('add').click();document.getElementById('value').textContent",
    ),
    "0",
  );
  await shot("reader-v3-first-preview");
};
