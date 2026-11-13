// Go 壳 UI 烟雾检查（CI 用，也可本地跑：pnpm shot:go --steps apps/desktop-go/scripts/smoke-steps.mjs）。
// 覆盖：能启动、能渲染、渲染进程 → preload → Go IPC → SQLite 通、一次真实的列表/详情交互、页面导航。
// 只依赖 win，不依赖 Electron 主进程；选择器均取自已在 Go 壳上验证过的 content-panel-switch-shots.mjs。
import assert from "node:assert/strict";

export default async ({ win, shot }) => {
  // 压掉首次引导与迁移提示（与现有步骤脚本一致），再 reload 让设置生效
  await win.evaluate(() => {
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor({ state: "visible" });

  // IPC + SQLite：经 preload 暴露的 API 真实写入一条论坛条目
  const title = "烟雾检查条目";
  const created = await win.evaluate(
    (t) =>
      window.api.knowledge.create({
        title: t,
        itemType: "forum",
        content: "## 讨论总结\n\n烟雾检查的讨论总结。\n\n## 正文\n\n烟雾检查的主楼正文。",
      }),
    title,
  );
  assert.equal(created.title, title, "knowledge.create 应返回刚写入的条目");

  // 列表与详情：reload 后条目仍在（说明已落库），点开后显示「讨论总结」页签
  await win.reload();
  const list = win.getByTestId("item-list");
  await list.getByText(title, { exact: true }).click();
  const summary = win.getByRole("button", { name: "讨论总结", exact: true });
  await summary.waitFor();
  assert.equal(await summary.getAttribute("aria-pressed"), "true");
  await shot("smoke-detail");

  // 页面导航：进入设置页
  await win.getByTestId("rail-settings").click();
  await win.getByText("应用设置", { exact: true }).first().waitFor();
  await shot("smoke-settings");
};
