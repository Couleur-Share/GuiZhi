import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";

export default async ({ win, app, shot, outDir }) => {
  await win.evaluate(() => {
    const settings = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
    Object.assign(settings.state, { language: "zh", themeMode: "light", isDarkMode: false });
    localStorage.setItem("guizhi-settings", JSON.stringify(settings));
    localStorage.setItem("guizhi-setup-dismissed", "1");
    localStorage.setItem("guizhi-migration-dismissed", "1");
  });
  const open = async () => {
    await win.reload();
    await win.getByTestId("rail-settings").click();
    await win.getByTestId("settings-nav-ai").click();
  };
  await open();
  const section = win.getByRole("region", { name: "联网搜索", exact: true });
  const toggle = section.getByRole("switch", { name: "生成阅读页时默认联网搜索" });
  assert.equal(await toggle.getAttribute("aria-checked"), "false");
  await toggle.click();
  await win.waitForFunction(async () => (await window.api.themedReading.searchConfig()).search.defaultEnabled === true);
  await open();
  assert.equal(await toggle.getAttribute("aria-checked"), "true");
  await toggle.click();
  await win.waitForFunction(async () => (await window.api.themedReading.searchConfig()).search.defaultEnabled === false);
  await section.scrollIntoViewIfNeeded(); await shot("reading-search-default-off");
  await section.getByLabel("Tavily API Key").fill("fixture-tavily-key");
  await section.getByRole("button", { name: "保存", exact: true }).click();
  await section.getByText(/Tavily · 已配置/).waitFor();
  const select = async provider => {
    await section.getByRole("button", { name: "搜索服务" }).click();
    await win.getByRole("option", { name: provider, exact: true }).click();
  };
  await select("AnySearch");
  await section.getByLabel("AnySearch API Key").fill("fixture-anysearch-key");
  await section.getByRole("button", { name: "保存", exact: true }).click();
  await section.getByText(/AnySearch · 已配置 · 当前使用 AnySearch/).waitFor();
  await section.scrollIntoViewIfNeeded(); await shot("anysearch-configured");
  await open();
  assert.equal(await section.getByLabel("AnySearch API Key").inputValue(), "");
  await section.getByText(/AnySearch · 已配置/).waitFor();
  await select("Tavily");
  await section.getByRole("button", { name: "保存", exact: true }).click();
  await section.getByText(/当前使用 Tavily/).waitFor();
  await select("AnySearch");
  // 隔离实例模拟额度失败，不发送测试密钥，也不改正式配置。
  await app.evaluate(({ ipcMain }) => {
    const channel = "themedReading:searchConfig";
    const original = ipcMain._invokeHandlers.get(channel);
    ipcMain.removeHandler(channel);
    ipcMain.handle(channel, (event, input) => input?.test
      ? new Promise(resolve => setTimeout(() => resolve({ success: false, error: "AnySearch 搜索失败（HTTP 402）：搜索额度已用尽，请检查账户额度" }), 1200))
      : original(event, input));
  });
  await section.getByRole("button", { name: "测试连接" }).click();
  await section.getByRole("status").waitFor(); await shot("anysearch-testing");
  await section.getByRole("alert").waitFor();
  assert.match(await section.getByRole("alert").innerText(), /HTTP 402.*额度/);
  const status = await win.evaluate(() => window.api.themedReading.searchConfig());
  assert.equal(status.search.provider, "tavily");
  assert.deepEqual(status.search.configuredProviders, ["tavily", "anysearch"]);
  await shot("anysearch-retryable-error");
  await section.getByRole("button", { name: "清除密钥" }).click();
  await section.getByText(/AnySearch · 尚未配置 · 当前使用 Tavily/).waitFor();
  await win.setViewportSize({ width: 1120, height: 900 });
  await section.scrollIntoViewIfNeeded(); await shot("anysearch-cleared-narrow");
  await fs.writeFile(path.join(outDir, "evidence.json"), JSON.stringify({ success: true, isolatedApp: true, switching: true, persistence: true, failureKeepsPrevious: true, clearingKeepsOtherKey: true, liveService: false }, null, 2));
};
