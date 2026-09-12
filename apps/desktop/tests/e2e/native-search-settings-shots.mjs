import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import http from "node:http";

/** 真实隔离 Electron + 本机 Responses 测试服务；不读取用户配置、不访问第三方。 */
export default async ({ win, shot, outDir }) => {
  let mode = "success";
  const calls = [];
  const server = http.createServer((req, res) => {
    let body = ""; req.on("data", chunk => body += chunk);
    req.on("end", () => {
      calls.push({ path: req.url, body: JSON.parse(body), authenticated: req.headers.authorization === "Bearer native-fixture-key" });
      res.setHeader("Content-Type", "text/event-stream");
      res.write(': heartbeat\n\n');
      setTimeout(() => {
        const output = mode === "success" ? [
          { type: "web_search_call", status: "completed", action: { type: "search", sources: [{ type: "url", url: "https://example.org/docs" }] } },
          { type: "message", content: [{ type: "output_text", text: "测试来源", annotations: [{ type: "url_citation", title: "资料", url: "https://example.org/docs" }] }] },
        ] : [{ type: "message", content: [{ type: "output_text", text: "没有实际搜索的普通回答" }] }];
        res.end(`data: ${JSON.stringify({ type: "response.completed", response: { status: "completed", output, usage: { input_tokens: 10, output_tokens: 20 } } })}\n\ndata: [DONE]\n\n`);
      }, 1200);
    });
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  try {
    await win.evaluate(() => {
      const settings = JSON.parse(localStorage.getItem("guizhi-settings") || '{"state":{}}');
      Object.assign(settings.state, { language: "zh", themeMode: "light", isDarkMode: false });
      localStorage.setItem("guizhi-settings", JSON.stringify(settings));
      localStorage.setItem("guizhi-setup-dismissed", "1"); localStorage.setItem("guizhi-migration-dismissed", "1");
    });
    const open = async () => {
      await win.reload(); await win.getByTestId("rail-settings").click(); await win.getByTestId("settings-nav-ai").click();
    };
    await open();
    const section = win.getByRole("region", { name: "联网搜索", exact: true });
    const select = async name => {
      await section.getByRole("button", { name: "搜索服务" }).click();
      await win.getByRole("option", { name, exact: true }).click();
    };
    await select("模型服务原生搜索");
    assert.equal(await section.getByLabel(/API Key/).count(), 0);
    assert.equal(await section.getByRole("button", { name: "测试连接" }).isDisabled(), true);
    await section.scrollIntoViewIfNeeded(); await shot("native-search-no-model");
    await win.evaluate(async apiUrl => {
      await window.api.settings.set({ aiProvider: "custom", aiApiProtocol: "openai", aiApiKey: "native-fixture-key", aiApiUrl: apiUrl, aiModel: "gpt-6-astra" });
      await window.api.themedReading.searchConfig({ provider: "tavily", apiKey: "retained-fixture-key" });
    }, `http://127.0.0.1:${server.address().port}`);
    await open(); await select("模型服务原生搜索");
    await section.getByText("使用主文本模型：gpt-6-astra").waitFor();
    assert.equal(await section.getByRole("button", { name: "清除密钥" }).count(), 0);
    await section.getByRole("button", { name: "测试连接" }).click();
    await section.getByRole("status").waitFor(); await shot("native-search-testing");
    await section.getByText(/当前使用 模型服务原生搜索/).waitFor();
    await win.waitForFunction(async () => (await window.api.themedReading.searchConfig()).search.provider === "native");
    await section.scrollIntoViewIfNeeded(); await shot("native-search-configured");
    const status = await win.evaluate(() => window.api.themedReading.searchConfig());
    assert.deepEqual(status.search.configuredProviders, ["tavily", "native"]);
    assert.equal(status.search.defaultEnabled, false);
    assert(!JSON.stringify(status).includes("fixture-key"));
    await open(); await section.getByText("使用主文本模型：gpt-6-astra").waitFor();
    await select("Tavily"); await section.getByRole("button", { name: "保存", exact: true }).click();
    await section.getByText(/当前使用 Tavily/).waitFor();
    await select("模型服务原生搜索"); mode = "no-search";
    await section.getByRole("button", { name: "测试连接" }).click();
    await section.getByRole("alert").waitFor();
    assert.match(await section.getByRole("alert").innerText(), /未完成联网搜索/);
    assert.equal((await win.evaluate(() => window.api.themedReading.searchConfig())).search.provider, "tavily");
    await shot("native-search-retryable-error");
    mode = "success";
    await section.getByRole("button", { name: "测试连接" }).click();
    await section.getByText(/当前使用 模型服务原生搜索/).waitFor();
    await win.evaluate(() => document.documentElement.classList.add("dark"));
    await section.scrollIntoViewIfNeeded(); await shot("native-search-dark");
    await win.setViewportSize({ width: 1120, height: 900 });
    await section.scrollIntoViewIfNeeded(); await shot("native-search-narrow");
    assert.equal(calls.length, 3);
    for (const call of calls) {
      assert.equal(call.path, "/v1/responses"); assert(call.authenticated);
      assert.equal(call.body.tool_choice, "required"); assert.deepEqual(call.body.tools, [{ type: "web_search" }]);
      assert.equal(call.body.input, "OpenAI web search documentation");
    }
    await fs.writeFile(path.join(outDir, "native-search-evidence.json"), JSON.stringify({ success: true, isolatedApp: true, localResponsesFixture: true, liveCPA: false, actualSearchRequests: calls.length, requiresCompletedSearch: true, failedTestRetainsProvider: true, encryptedKeysRetained: true, defaultRemainsOff: true }, null, 2));
  } finally { server.closeAllConnections(); await new Promise(resolve => server.close(resolve)); }
};
