import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";

const { chromium } = await import(
  process.env.GUIZHI_SHOT_PLAYWRIGHT
    ? pathToFileURL(path.resolve(process.env.GUIZHI_SHOT_PLAYWRIGHT)).href
    : "playwright"
);

export default async function ({ win, app, outDir, userDataDir }) {
  const beforePid = await app.evaluate(() => process.pid);
  const beforeUserData = await app.evaluate(({ app }) =>
    app.getPath("userData"),
  );
  assert.equal(path.resolve(beforeUserData), path.resolve(userDataDir));
  const fixture = await win.evaluate(async () => {
    const item = await window.api.knowledge.create({
      title: "自行重启验收",
      content: "恢复后必须保留的中文正文",
      itemType: "webpage",
      tagNames: ["重启验收"],
    });
    const backup = await window.api.backup.create();
    if (!backup.success) throw new Error(backup.error);
    await window.api.knowledge.update(item.id, {
      content: "恢复时应撤回的临时改动",
    });
    return { item, backup };
  });
  fs.writeFileSync(
    path.join(outDir, "relaunch-start.json"),
    JSON.stringify({ beforePid, beforeUserData, fixture }, null, 2),
  );
  const portFile = path.join(userDataDir, "DevToolsActivePort");
  assert.ok(
    fs.existsSync(portFile),
    "Electron CDP endpoint file is required for observing the relaunched instance",
  );
  // 不替换 app.relaunch，也不由测试工具启动第二个应用实例。
  const closed = app.waitForEvent("close", { timeout: 30000 });
  const restored = await win.evaluate(
    (name) => window.api.backup.restore(name),
    fixture.backup.backup.fileName,
  );
  assert.equal(restored.success, true, restored.error);
  await closed;
  let browser;
  let session;
  let afterPid;
  let lastError;
  const deadline = Date.now() + 45000;
  while (Date.now() < deadline) {
    try {
      const [port, endpoint] = fs
        .readFileSync(portFile, "utf8")
        .trim()
        .split(/\r?\n/);
      assert.match(port, /^\d+$/);
      assert.ok(endpoint.startsWith("/devtools/browser/"));
      browser = await chromium.connectOverCDP(
        `ws://127.0.0.1:${port}${endpoint}`,
        { timeout: 1500 },
      );
      session = await browser.newBrowserCDPSession();
      const processes = await session.send("SystemInfo.getProcessInfo");
      afterPid = processes.processInfo.find((p) => p.type === "browser")?.id;
      assert.ok(
        afterPid && afterPid !== beforePid,
        "The application must restart as a new process",
      );
      break;
    } catch (error) {
      lastError = error;
      await browser?.close().catch(() => {});
      browser = null;
      await new Promise((resolve) => setTimeout(resolve, 500));
    }
  }
  assert.ok(browser, `Cannot observe actual relaunch: ${lastError}`);
  try {
    const context = browser.contexts()[0];
    const next =
      context.pages().find((page) => page.url().startsWith("file:")) ??
      (await context.waitForEvent("page"));
    await next.getByTestId("topbar-search").waitFor({ timeout: 30000 });
    const item = await next.evaluate(
      (id) => window.api.knowledge.get(id),
      fixture.item.id,
    );
    assert.equal(item.content, fixture.item.content);
    assert.equal(item.title, fixture.item.title);
    assert.ok(item.tags.some((tag) => tag.name === "重启验收"));
    await next.evaluate(() => {
      const settings = JSON.parse(
        localStorage.getItem("guizhi-settings") || '{"state":{}}',
      );
      settings.state.language = "zh";
      localStorage.setItem("guizhi-settings", JSON.stringify(settings));
      localStorage.setItem("guizhi-setup-dismissed", "1");
      localStorage.setItem("guizhi-migration-dismissed", "1");
    });
    await next.reload();
    await next.getByTestId("topbar-search").waitFor();
    await next.getByText("自行重启验收", { exact: true }).first().click();
    await next.screenshot({
      path: path.join(outDir, "actual-relaunch.png"),
      animations: "disabled",
      caret: "hide",
    });
    fs.writeFileSync(
      path.join(outDir, "relaunch.json"),
      JSON.stringify(
        {
          passed: true,
          beforePid,
          afterPid,
          userDataDir,
          item,
          restored,
          launchMethod: "unmodified app.relaunch",
        },
        null,
        2,
      ),
    );
  } finally {
    // 通过新实例自己的浏览器退出路径关闭；不按进程名结束其他实例。
    await Promise.race([
      session.send("Browser.close").catch(() => {}),
      new Promise((resolve) => setTimeout(resolve, 2000)),
    ]);
    if (browser.isConnected()) await browser.close().catch(() => {});
  }
}
