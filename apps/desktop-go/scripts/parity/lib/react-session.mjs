/**
 * 拉起 React 参照（Go 壳 + WebView2）并经 CDP 连上页面，供对等采集器复用。
 *
 * 启动方式与 apps/desktop-go/scripts/shot.mjs 一致（该脚本是截图用的一次性入口，不导出可复用的启动函数；
 * apps/desktop-go/testdata/make-webview2-localstorage.mjs 也是同样的复刻）：
 *   - -bench：自动化实例，窗口在所有显示器之外，不占单实例锁、不建托盘、不抢快捷键；
 *   - 一次性 userData 与 WEBVIEW2_USER_DATA_FOLDER，绝不共用真实安装的数据目录与 WebView2 配置；
 *   - CDP 端口 port=0 随机，从 DevToolsActivePort 读（两行写全才算有效）。
 * 窗口保持「屏幕外 + showInactive」：隐藏窗口的合成器会停摆，截图拿不到新帧（AGENTS.md）。
 *
 * 只按 PID 结束自己启动的进程树，禁止按进程名批量结束。
 */
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { chromium } from "playwright";

const READY_TEST_ID = "topbar-search";
const START_TIMEOUT_MS = 90_000;
const READY_TIMEOUT_MS = 30_000;
const EXIT_TIMEOUT_MS = 10_000;

async function readDevToolsPort(userDataDir, isExited, deadline) {
  const candidates = ["webview2/EBWebView/DevToolsActivePort", "webview2/DevToolsActivePort"].map((p) => path.join(userDataDir, p));
  for (;;) {
    for (const file of candidates) {
      let text;
      try { text = fs.readFileSync(file, "utf8"); } catch { continue; }
      const lines = text.split(/\r?\n/).filter(Boolean);
      if (lines.length >= 2) return lines[0];
    }
    if (isExited()) throw new Error("Go 壳在打开调试端口前退出了");
    if (Date.now() > deadline) throw new Error("等待 WebView2 调试端口超时（DevToolsActivePort 未出现）");
    await new Promise((r) => setTimeout(r, 50));
  }
}

/**
 * 打开一次会话。
 * @param {{executable:string, dist:string, dataDb?:string|null, env?:object}} options
 * @returns 会话：page、cdp、userDataDir，以及 reload()、close()。
 */
export async function openReactSession({ executable, dist, dataDb = null, env = process.env }) {
  const userDataDir = fs.mkdtempSync(path.join(fs.realpathSync.native(os.tmpdir()), "guizhi-parity-react-"));
  const dbFile = path.join(userDataDir, "data", "knowledge.db");
  fs.mkdirSync(path.dirname(dbFile), { recursive: true });
  if (dataDb) {
    fs.copyFileSync(dataDb, dbFile);
    // WAL 模式下最近的写入可能还在 -wal 里，旁文件一起带上（源库须已关闭）。
    const wal = `${dataDb}-wal`;
    if (fs.existsSync(wal) && fs.statSync(wal).size > 0) fs.copyFileSync(wal, `${dbFile}-wal`);
  }

  let exited = false;
  let output = "";
  const child = spawn(executable, ["-dist", dist, "-userdata", userDataDir, "-db", dbFile, "-bench"], {
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
    env: {
      ...env,
      WEBVIEW2_USER_DATA_FOLDER: path.join(userDataDir, "webview2"),
      WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: "--remote-debugging-port=0",
    },
  });
  child.stdout.on("data", (d) => { output += d; });
  child.stderr.on("data", (d) => { output += d; });
  child.once("exit", () => { exited = true; });
  child.once("error", (e) => { output += `\n[spawn error] ${e.message}`; exited = true; });

  let browser = null;
  const close = async () => {
    try { await browser?.close(); } catch { /* 已断开 */ }
    if (!exited) {
      const done = new Promise((resolve) => child.once("exit", resolve));
      spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" });
      await Promise.race([done, new Promise((r) => setTimeout(r, EXIT_TIMEOUT_MS))]);
    }
    try {
      fs.rmSync(userDataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    } catch (error) {
      console.warn(`临时目录删除失败，请稍后手动清理：${userDataDir}（${error.message}）`);
    }
  };

  try {
    let port;
    try {
      port = await readDevToolsPort(userDataDir, () => exited, Date.now() + START_TIMEOUT_MS);
    } catch (error) {
      throw new Error(`${error.message}\n--- Go 壳输出（末尾）---\n${output.split(/\r?\n/).slice(-30).join("\n")}`);
    }
    browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
    const context = browser.contexts()[0];
    const pages = browser.contexts().flatMap((c) => c.pages());
    const page = pages.find((p) => p.url().includes("guizhi.localhost")) ?? pages[0];
    if (!page) throw new Error("已连接调试端口，但没有找到页面");
    await page.waitForLoadState("domcontentloaded");
    await page.getByTestId(READY_TEST_ID).waitFor({ state: "attached", timeout: READY_TIMEOUT_MS });
    const cdp = await context.newCDPSession(page);
    return {
      page,
      cdp,
      userDataDir,
      pid: child.pid,
      /** reload 后等回到就绪状态。 */
      async reload() {
        await page.reload();
        await page.waitForLoadState("domcontentloaded");
        await page.getByTestId(READY_TEST_ID).waitFor({ state: "attached", timeout: READY_TIMEOUT_MS });
      },
      close,
    };
  } catch (error) {
    await close();
    throw error;
  }
}
