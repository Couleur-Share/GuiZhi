import assert from "node:assert/strict";
export default async ({ win, shot }) => {
  const rows = await win.evaluate(async () => {
    const stored = JSON.parse(
      localStorage.getItem("guizhi-settings") || '{"state":{}}',
    );
    Object.assign(stored.state, {
      language: "zh",
      themeMode: "dark",
      isDarkMode: true,
      editorMarkdownPreview: true,
    });
    localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
    return Promise.all([
      window.api.knowledge.create({
        title: "视频条目切换验证",
        itemType: "video",
        content: "# 视频总结\n\n当前视频的正文。",
      }),
      window.api.knowledge.create({
        title: "网配蔡司智锐个化版的过程分享（合成验证）",
        itemType: "forum",
        content:
          "## 讨论总结\n\n这是用于切换验证的讨论总结。\n\n## 正文\n\n这是用于切换验证的主楼正文。",
      }),
    ]);
  });
  await win.reload();
  const list = win.getByTestId("item-list");
  await list.getByText(rows[0].title, { exact: true }).click();
  await win.getByRole("button", { name: "正文", exact: true }).waitFor();
  await list.getByText(rows[1].title, { exact: true }).click();
  const summary = win.getByRole("button", { name: "讨论总结", exact: true });
  await summary.waitFor();
  assert.equal(await summary.getAttribute("aria-pressed"), "true");
  await shot("forum-summary-direct");
  await win.getByRole("button", { name: "正文", exact: true }).click();
  await list.getByText(rows[0].title, { exact: true }).click();
  await win
    .getByRole("button", { name: "讨论总结", exact: true })
    .waitFor({ state: "detached" });
  await list.getByText(rows[1].title, { exact: true }).click();
  await summary.waitFor();
  assert.equal(
    await win
      .getByRole("button", { name: "正文", exact: true })
      .getAttribute("aria-pressed"),
    "true",
  );
  await shot("forum-body-remembered");
};
