/** 仅由 pnpm shot 调用，使用工具创建的临时数据目录和屏幕外窗口。 */
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
  await win.getByRole("button", { name: "导入", exact: true }).click();
  await win.getByRole("button", { name: "导入文档站", exact: true }).click();
  await win
    .getByPlaceholder("https://docs.example.com/guide/")
    .fill("https://docs.python.org/zh-cn/3/tutorial/index.html");
  await win.getByText("实际范围：", { exact: false }).waitFor();
  await shot("documents-scope");
  await app.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(900, 740),
  );
  await shot("documents-narrow");
  await win.getByRole("button", { name: "研究", exact: true }).click();
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
  await win.getByText("内置网页采集", { exact: true }).waitFor();
  await shot("web-component-settings");
}
