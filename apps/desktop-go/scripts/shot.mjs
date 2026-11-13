/**
 * Go 壳界面截图：拉起真实的 Go 壳（WebView2），经 CDP 交给 Playwright，截图后退出；全程不抢焦点、不占屏幕。
 *
 * 通常不要直接运行：用 `pnpm shot:go`（scripts/isolated-go.mjs），它会先把当前源码复制到系统临时目录、构建，再调用本脚本。
 * 本脚本只负责"已有 exe 与 dist → 启动 → 截图"，所以必须显式给出 --executable 与 --dist。
 *
 *   pnpm shot:go                          # 首页截一张
 *   pnpm shot:go --steps my-steps.mjs     # 按脚本走到指定界面再截
 *   pnpm shot:go --data-db path/knowledge.db   # 用数据库副本截图
 *   pnpm shot:go --keep-profile           # 保留临时用户数据目录（路径写入 <out>/profile.json）
 *
 * 实现要点：
 *   - 以 -bench 方式启动：自动化实例，窗口放在所有显示器之外，不占单实例锁、不建托盘、不抢快捷键；
 *   - 环境变量 WEBVIEW2_USER_DATA_FOLDER 指向一次性目录，WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS 打开 CDP 端口（port=0，随机）；
 *   - 端口从 <userData>/webview2/EBWebView/DevToolsActivePort 读取，两行写全才算有效（Chromium 非原子写入该文件）；
 *   - 就绪判据与 Electron 版一致：顶栏搜索框（topbar-search）出现。
 *
 * steps 文件默认导出一个函数，参数与 Electron 版同名字段保持一致：
 *
 *   export default async ({ win, shot, outDir, userDataDir }) => {
 *     await win.getByTestId("rail-settings").click();
 *     await shot("settings");
 *   };
 *
 * 与 Electron 版的差异（必须知道）：
 *   - 没有 Electron 主进程，因此没有可用的 `app`：访问它的任何属性都会抛出说明性错误。
 *     依赖 app.evaluate（ipcMain / BrowserWindow / session 等）的旧步骤脚本需要改写，例如把"替换主进程 IPC 处理器"
 *     改为在页面里拦截 window.electron.ipcRenderer.invoke。
 *   - shot() 只截宿主页面：Go 壳的阅读子视图是独立的 WebView2，不会被合成进截图（Electron 版会合成）。
 *   - 退出时直接结束本次启动的进程树（不等待优雅关闭），临时数据库随目录一并删除。
 */
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { chromium } from "playwright";

/** 首屏就绪的判据：顶栏搜索框出现即说明渲染进程与数据库都通了。 */
const READY_TEST_ID = "topbar-search";
const START_TIMEOUT_MS = 90_000;
const READY_TIMEOUT_MS = 30_000;
const EXIT_TIMEOUT_MS = 10_000;

/** 关动画、藏光标：否则界面就绪后立刻截的第一张会抓到淡入过渡的中间态。调用方可在 shot() 第二个参数里覆盖。 */
const DEFAULT_SHOT_OPTIONS = { animations: "disabled", caret: "hide" };

/** 用法错误（参数写错、产物不存在）只打一句话，调用栈是噪音。 */
class UsageError extends Error {}

function parseArgs(argv) {
  const args = { out: null, steps: null, dataDb: null, executable: null, dist: null, keepProfile: false };
  const value = (i, name) => {
    if (!argv[i]) throw new UsageError(`${name} 缺少值`);
    return path.resolve(argv[i]);
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--out") args.out = value(++i, arg);
    else if (arg === "--steps") args.steps = value(++i, arg);
    else if (arg === "--data-db") args.dataDb = value(++i, arg);
    else if (arg === "--executable") args.executable = value(++i, arg);
    else if (arg === "--dist") args.dist = value(++i, arg);
    else if (arg === "--keep-profile") args.keepProfile = true;
    else throw new UsageError(`未知参数：${arg}`);
  }
  if (!args.executable || !args.dist) throw new UsageError("必须给出 --executable 与 --dist；日常请用 pnpm shot:go（会先隔离构建）");
  for (const [label, p] of [["--executable", args.executable], ["--dist", args.dist], ["--steps", args.steps], ["--data-db", args.dataDb]])
    if (p && !fs.existsSync(p)) throw new UsageError(`${label} 指向的路径不存在：${p}`);
  args.out ??= path.resolve("shots");
  return args;
}

/** 没有 Electron 主进程：任何访问都给出可操作的错误，而不是 "Cannot read properties of undefined"。 */
function unavailableApp() {
  const fail = (what) => {
    throw new Error(
      `Go 壳没有 Electron 主进程，无法使用 app${what ? `.${what}` : ""}。` +
        "该步骤脚本依赖 app.evaluate（ipcMain / BrowserWindow 等），需要改写：例如在页面里拦截 window.electron.ipcRenderer.invoke 代替替换主进程 IPC 处理器。",
    );
  };
  return new Proxy({}, { get: (_t, key) => (key === "then" ? undefined : fail(String(key))) });
}

/** 轮询读取 DevToolsActivePort；文件须有两行（端口 + ws 路径）才算写完整。 */
async function readDevToolsPort(userDataDir, child, isExited, deadline) {
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

const settlesWithin = (promise, ms) => {
  let timer;
  return Promise.race([promise.then(() => true, () => true), new Promise((r) => { timer = setTimeout(() => r(false), ms); })]).finally(() => clearTimeout(timer));
};

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const stepsFn = args.steps ? (await import(pathToFileURL(args.steps).href)).default : null;
  if (args.steps && typeof stepsFn !== "function") throw new UsageError(`${args.steps} 需要默认导出一个函数`);
  fs.mkdirSync(args.out, { recursive: true });

  const userDataDir = fs.mkdtempSync(path.join(args.keepProfile ? args.out : os.tmpdir(), "guizhi-go-shot-"));
  const dbFile = path.join(userDataDir, "data", "knowledge.db");
  fs.mkdirSync(path.dirname(dbFile), { recursive: true });
  if (args.dataDb) fs.copyFileSync(args.dataDb, dbFile);

  const taken = [];
  let child = null;
  let exited = false;
  let output = "";
  let browser = null;
  let page = null;
  let failure = null;

  try {
    child = spawn(args.executable, ["-dist", args.dist, "-userdata", userDataDir, "-db", dbFile, "-bench"], {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        // 一次性 WebView2 配置：绝不共用真实安装的 %LOCALAPPDATA%\GuiZhi\WebView2。
        WEBVIEW2_USER_DATA_FOLDER: path.join(userDataDir, "webview2"),
        WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: "--remote-debugging-port=0",
      },
    });
    child.stdout.on("data", (d) => { output += d; });
    child.stderr.on("data", (d) => { output += d; });
    child.once("exit", () => { exited = true; });
    child.once("error", (e) => { output += `\n[spawn error] ${e.message}`; exited = true; });

    let port;
    try {
      port = await readDevToolsPort(userDataDir, child, () => exited, Date.now() + START_TIMEOUT_MS);
    } catch (error) {
      throw new Error(`${error.message}\n--- Go 壳输出（末尾）---\n${output.split(/\r?\n/).slice(-30).join("\n")}`);
    }

    browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
    const pages = browser.contexts().flatMap((c) => c.pages());
    page = pages.find((p) => p.url().includes("guizhi.localhost")) ?? pages[0];
    if (!page) throw new Error("已连接调试端口，但没有找到页面");
    await page.waitForLoadState("domcontentloaded");
    await page.getByTestId(READY_TEST_ID).waitFor({ state: "attached", timeout: READY_TIMEOUT_MS });

    const shot = async (name, options = {}) => {
      const file = path.join(args.out, `${name}.png`);
      await page.screenshot({ path: file, ...DEFAULT_SHOT_OPTIONS, ...options });
      taken.push(file);
      return file;
    };

    if (stepsFn) {
      await stepsFn({ win: page, app: unavailableApp(), shot, outDir: args.out, userDataDir, gpu: false });
      // steps 一张都没截时兜一张，免得跑完只得到一个空目录
      if (taken.length === 0 && !page.isClosed()) await shot("final");
    } else {
      await shot("home");
    }
  } catch (error) {
    failure = error;
    // 失败现场同样值得留一张：多数时候一眼就能看出卡在哪个界面
    try {
      if (!page || page.isClosed()) throw error;
      const file = path.join(args.out, "failure.png");
      await page.screenshot({ path: file, timeout: 10_000 });
      taken.push(file);
    } catch {
      /* 页面已经没了就算了 */
    }
  } finally {
    // 先断开 CDP（不会关掉壳），再结束本次启动的进程树；只按 PID，禁止按进程名批量结束。
    try { await browser?.close(); } catch { /* 已断开 */ }
    if (child && !exited) {
      const done = new Promise((resolve) => child.once("exit", resolve));
      spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" });
      await settlesWithin(done, EXIT_TIMEOUT_MS);
    }
    if (args.keepProfile) {
      fs.writeFileSync(path.join(args.out, "profile.json"), JSON.stringify({ userDataDir, executable: args.executable, failed: !!failure }, null, 2));
    } else {
      try {
        fs.rmSync(userDataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
      } catch (error) {
        // 清理失败不能盖住步骤本身的错误
        console.warn(`临时目录删除失败，请稍后手动清理：${userDataDir}（${error.message}）`);
      }
    }
  }

  for (const file of taken) console.log(file);
  if (failure) throw failure;
}

try {
  await main();
} catch (error) {
  console.error(error instanceof UsageError ? error.message : (error?.stack ?? error));
  process.exitCode = 1;
}
