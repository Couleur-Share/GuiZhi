/**
 * 仅由 pnpm shot 调用，使用工具创建的临时数据目录和屏幕外窗口：文档站范围、网页研究入口与采集设置。
 * 不发起网页采集，可用于 --executable 的打包产物冒烟。
 */
export default async function ({ win, app, shot }) {
  await win.evaluate(() => {
    const stored = JSON.parse(
      localStorage.getItem("guizhi-settings") || '{"state":{}}',
    );
    stored.state.language = "zh";
    localStorage.setItem("guizhi-settings", JSON.stringify(stored));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  await win.reload();
  await win.getByTestId("topbar-search").waitFor();
  await win.getByRole("button", { name: "采集", exact: true }).click();
  await win.getByRole("button", { name: "采集文档站", exact: true }).click();
  await win
    .getByPlaceholder("https://docs.example.com/guide/")
    .fill("https://docs.python.org/zh-cn/3/tutorial/index.html");
  await win.getByText("实际范围：", { exact: false }).waitFor();
  await shot("documents-scope");
  await app.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(900, 740),
  );
  await shot("documents-narrow");
  await win.keyboard.press("Alt+Shift+F"); await win.getByPlaceholder("搜索知识、Wiki、任务、设置或动作…").fill("新建近期研究"); await win.keyboard.press("Enter");
  for (const label of ["小红书", "抖音", "哔哩哔哩"]) {
    const control = win.getByRole("checkbox", { name: label, exact: true });
    if (await control.isChecked())
      await win.getByText(label, { exact: true }).click();
  }
  await win.getByText("网页", { exact: true }).click();
  await win
    .getByRole("textbox", { name: "网页入口 1" })
    .fill("https://docs.python.org/zh-cn/3/tutorial/index.html");
  await win.getByText("不限时间", { exact: true }).waitFor();
  await shot("web-research-all-time");
  await win.getByTestId("rail-settings").click();
  await win.getByRole("button", { name: "采集与转写", exact: true }).click();
  await win.getByText("Cloudflare 后备采集", { exact: true }).waitFor();
  if (await win.getByText("内置网页采集", { exact: true }).count())
    throw new Error("采集设置里仍有已移除的「内置网页采集」分区");
  await shot("capture-settings");
}
