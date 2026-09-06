import assert from "node:assert/strict";

// 仅在 pnpm shot 的临时库中创建合成条目，评论接口使用桩，不访问真实平台。
export default async ({ win, app, shot }) => {
  await app.evaluate(({ BrowserWindow }) => BrowserWindow.getAllWindows()[0].setSize(1440, 960));
  const rows = await win.evaluate(async () => {
    const stored = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(stored.state, { language: "zh", themeMode: "dark", isDarkMode: true });
    localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    return Promise.all([
      window.api.knowledge.create({ title: "生啤、熟啤、鲜啤：三个判断维度", itemType: "video", content: "# 视频总结\n\n判断啤酒可以分别看杀菌方式、新鲜程度和酿造方式。\n\n## 生啤与熟啤\n\n熟啤经过杀菌，生啤通常保留更多新鲜风味。\n\n## 阅读重点\n\n先理解概念，再结合生产日期和保存方式判断。" }),
      window.api.knowledge.create({ title: "本地知识库使用体验与补充建议", itemType: "webpage", content: "# 使用体验\n\n正文整理了本地知识库的导入和搜索流程。\n\n评论中的补充经验可按需展开查看。" }),
    ]);
  });
  await app.evaluate(({ ipcMain }, rows) => {
    const [video, note] = rows;
    video.sourceUri = "https://www.douyin.com/video/123";
    note.sourceUri = "https://www.xiaohongshu.com/explore/123";
    const comment = { id: "c1", externalId: "c1", platform: "xiaohongshu", itemId: note.id, authorName: "使用者", content: "补充一个经验：先按主题整理少量材料，再逐步导入历史笔记，检索效果更容易验证。", likeCount: 12, publishedAt: Date.now(), capturedAt: Date.now() };
    globalThis.commentPreview = { calls: [], comments: { [video.id]: [], [note.id]: [comment] } };
    for (const channel of ["knowledge:get", "platformCapture:listComments", "platformCapture:refreshComments"]) ipcMain.removeHandler(channel);
    ipcMain.handle("knowledge:get", (_event, id) => rows.find(row => row.id === id));
    ipcMain.handle("platformCapture:listComments", (_event, id) => globalThis.commentPreview.comments[id] ?? []);
    ipcMain.handle("platformCapture:refreshComments", (_event, input) => {
      globalThis.commentPreview.calls.push(input);
      const captured = [{ ...comment, itemId: input.itemId, platform: "douyin" }];
      globalThis.commentPreview.comments[input.itemId] = captured;
      return captured;
    });
  }, rows);
  await win.reload();
  const list = win.getByTestId("item-list");
  await list.getByText(rows[0].title, { exact: true }).click();
  await win.getByTestId("item-title-input").waitFor();
  assert.equal(await win.getByRole("button", { name: /来源评论/ }).count(), 0);
  await shot("source-comments-empty-hidden");
  await win.getByRole("button", { name: "更多操作", exact: true }).click();
  await win.getByText("采集评论", { exact: true }).waitFor();
  await shot("source-comments-menu");
  await win.getByText("采集评论", { exact: true }).click();
  await win.getByRole("button", { name: "评论采集数量", exact: true }).waitFor();
  assert.equal(await app.evaluate(() => globalThis.commentPreview.calls.length), 0);
  await win.getByRole("button", { name: "采集评论", exact: true }).click();
  await win.getByText(/补充一个经验/).waitFor();
  assert.deepEqual(await app.evaluate(() => globalThis.commentPreview.calls), [{ itemId: rows[0].id, limit: 20 }]);
  await list.getByText(rows[1].title, { exact: true }).click();
  const toggle = win.getByRole("button", { name: /来源评论/ });
  await toggle.waitFor();
  assert.equal(await toggle.getAttribute("aria-expanded"), "false");
  assert.equal(await win.getByRole("button", { name: "评论采集数量", exact: true }).count(), 0);
  await shot("source-comments-existing-collapsed");
  await toggle.click();
  await win.getByText(/补充一个经验/).waitFor();
  await shot("source-comments-existing-expanded");
  await win.getByRole("button", { name: "研究", exact: true }).click();
  const checkbox = win.getByRole("checkbox", { name: "精读时采集评论", exact: true });
  await checkbox.waitFor({ state: "attached" });
  assert.equal(await checkbox.isChecked(), false);
  await shot("research-comments-default-off");
  await win.getByText("精读时采集评论", { exact: true }).click();
  assert.equal(await checkbox.isChecked(), true);
  console.log("空卡片、更多入口、手动采集、已有评论折叠和研究默认关闭验证通过");
};
