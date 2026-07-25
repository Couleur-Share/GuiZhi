/**
 * 端到端安全基线：CSP 实际生效、外部导航被拦截。
 *
 * 这两条只能在真实 Electron 里验证——CSP 走 session 的响应头注入，
 * 单测无法确认它对打包后的 file:// 页面是否真的落地。
 * 运行前需 `pnpm build`（test:e2e 脚本已包含）。
 */
import { test, expect, _electron as electron } from "@playwright/test";
import fs from "fs";
import os from "os";
import path from "path";

test("安全基线：CSP 阻断内联脚本，外部导航不带走主窗口", async () => {
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-e2e-sec-"));
  const app = await electron.launch({
    args: [path.join(__dirname, "../../out/main/index.js")],
    env: {
      ...process.env,
      GUIZHI_E2E: "1",
      GUIZHI_E2E_USER_DATA_DIR: userDataDir,
    },
  });

  try {
    const window = await app.firstWindow();
    await window.waitForLoadState("domcontentloaded");
    await expect(window.getByTestId("topbar-search")).toBeVisible({
      timeout: 20_000,
    });

    const readMainProcessUrl = () =>
      app.evaluate(({ BrowserWindow }) =>
        BrowserWindow.getAllWindows()[0].webContents.getURL(),
      );
    const originalUrl = await readMainProcessUrl();

    // 导入的网页正文是不可信内容；即使绕过 rehype-sanitize 插入了 script，
    // 生产 CSP 的 script-src 'self' 也必须让它不执行
    const inlineScriptRan = await window.evaluate(() => {
      const marker = "__guizhi_csp_probe__";
      const script = document.createElement("script");
      script.textContent = `window.${marker} = true;`;
      document.body.appendChild(script);
      const ran = (window as unknown as Record<string, unknown>)[marker] === true;
      script.remove();
      return ran;
    });
    expect(inlineScriptRan).toBe(false);

    // preload 的 window.api 绑在 window 上、不区分来源，
    // 主窗口一旦被导航到本地任意 HTML，对方就拿到了完整 IPC 能力。
    // 用 file:// 而非 http：外链会被转交系统浏览器，测试不该有那个副作用。
    // 被 preventDefault 的导航在 Playwright 侧不会 commit，因此从主进程读 URL。
    await window.evaluate(() => {
      window.location.href = "file:///C:/Windows/System32/drivers/etc/hosts";
    });
    await window.waitForTimeout(1500);
    expect(await readMainProcessUrl()).toBe(originalUrl);
  } finally {
    await app.close();
    fs.rmSync(userDataDir, { recursive: true, force: true });
  }
});
