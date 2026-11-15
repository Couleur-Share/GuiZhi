/**
 * 生成「真实 WebView2 写出的 localStorage」夹具，供 webview2_localstorage_test.go 与设置导入测试读取。
 *
 * 用法（--executable 必须是仍带 React 渲染层的 Go 壳构建，例如迁移基线 D:\Project\GuiZhi-baseline 的产物）：
 *   node apps/desktop-go/testdata/make-webview2-localstorage.mjs --executable <GuiZhi.exe> [--dist <dist 目录>] [--out <输出目录>]
 *
 * 做法：与 scripts/shot.mjs 相同的隔离启动（-bench、一次性 userData、一次性 WEBVIEW2_USER_DATA_FOLDER、CDP 端口），
 * 经 CDP 往真实页面（http://guizhi.localhost）写入合成的种子数据，等 Chromium 落盘后结束进程树，
 * 把 <webview2 目录>\EBWebView\Default\Local Storage\leveldb 复制成夹具，并写下：
 *   - expected.json：页面自己读回的全部 localStorage 键值（对拍的真相），
 *   - meta.json：origin、WebView2 内核版本、相对路径（定位逻辑要与它一致）。
 * 全程不碰真实 %LOCALAPPDATA%\GuiZhi\WebView2 与 %APPDATA%\GuiZhi；只按 PID 结束自己启动的进程。
 * 种子里的口令、密钥都是合成的占位值。
 */
import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const HERE = path.dirname(fileURLToPath(import.meta.url));
const READY_TEST_ID = "topbar-search";
const START_TIMEOUT_MS = 90_000;
const READY_TIMEOUT_MS = 30_000;
const FLUSH_WAIT_MS = 8_000; // Chromium 把 localStorage 异步落盘（约 5 秒内）
const SEED_ATTEMPTS = 3;
const SKIP_FILES = new Set(["LOCK", "LOG", "LOG.old"]); // LOCK 不需要；LOG 含时间戳，只会让夹具每次生成都不同

/** 合成的种子：覆盖 Latin1 与 UTF-16 两种编码、旧设置里的机密字段、每个已登记的偏好键，以及一个无关键。 */
const SEED = {
  "guizhi-settings": JSON.stringify({
    state: {
      themeMode: "dark",
      themeColor: "green",
      themeHue: 150,
      themeSaturation: 30,
      fontSize: "large",
      language: "zh",
      motionPreference: "reduced",
      launchAtStartup: true,
      enableNotifications: false,
      backupAutoEnabled: true,
      backupIntervalHours: 12,
      backupKeepCount: 20,
      networkProxy: {
        mode: "manual",
        protocol: "http",
        host: "127.0.0.1",
        port: 7890,
        username: "fixture-user",
        password: "fixture-proxy-password",
        bypass: "<local>,localhost",
      },
      flareSolverr: { enabled: true, connection: "local", port: 8191, sshHost: "" },
      scenarioModelDefaults: { summary: "模型-甲", qa: "fixture-model" },
      aiApiKey: "sk-fixture-not-a-real-key",
    },
    version: 3,
  }),
  "ui-storage": JSON.stringify({
    state: {
      appModule: "wiki",
      isSidebarCollapsed: true,
      sidebarPanelWidth: 333,
      itemListPaneWidth: 410,
      wikiCatalogPaneWidth: 250,
      libraryViewMode: "list",
    },
    version: 0,
  }),
  "guizhi-library-table-config": JSON.stringify({
    columns: [
      { id: "title", visible: true, width: 320 },
      { id: "tags", visible: false, width: 160 },
    ],
  }),
  "guizhi-library-smart-views-v1": JSON.stringify([
    { id: "11111111-1111-4111-8111-111111111111", name: "未读文章", scope: "all", collectionId: null, tagId: null, platform: "wechat", createdAt: 1731600000000 },
  ]),
  "guizhi-content-reading-v1": JSON.stringify({
    "item-fixture-1": { tab: "body", scrollTopByTab: { body: 120 }, catalogOpen: true, updatedAt: 1731600000000 },
  }),
  "guizhi-snapshot-reading-v1": JSON.stringify({ "snapshot-fixture-1": { scrollTop: 64, updatedAt: 1731600000001 } }),
  "wechat-reader-width": "wide",
  "guizhi-ask-active-session": "ask-session-fixture",
  "guizhi-setup-dismissed": "1",
  "guizhi-migration-dismissed": "1",
  "reading-original-catalog:edit-fixture-1": "true",
  "reading-original-catalog:edit-fixture-2": "false",
  "reading-ai-catalog:item-fixture-1:body": "true",
  "无关键-fixture": "与归知无关的值",
};

class UsageError extends Error {}

function parseArgs(argv) {
  const args = { executable: null, dist: null, out: path.join(HERE, "webview2-localstorage") };
  for (let i = 0; i < argv.length; i += 1) {
    const take = () => {
      if (!argv[i + 1]) throw new UsageError(`${argv[i]} 缺少值`);
      return path.resolve(argv[++i]);
    };
    if (argv[i] === "--executable") args.executable = take();
    else if (argv[i] === "--dist") args.dist = take();
    else if (argv[i] === "--out") args.out = take();
    else throw new UsageError(`未知参数：${argv[i]}`);
  }
  if (!args.executable) throw new UsageError("必须给出 --executable（仍带 React 渲染层的 GuiZhi.exe）");
  args.dist ??= path.join(path.dirname(args.executable), "dist");
  for (const [label, p] of [["--executable", args.executable], ["--dist", args.dist]])
    if (!fs.existsSync(p)) throw new UsageError(`${label} 指向的路径不存在：${p}`);
  return args;
}

async function readDevToolsPort(userDataDir, isExited, deadline) {
  const file = path.join(userDataDir, "webview2", "EBWebView", "DevToolsActivePort");
  for (;;) {
    let text = "";
    try { text = fs.readFileSync(file, "utf8"); } catch { /* 还没写出来 */ }
    const lines = text.split(/\r?\n/).filter(Boolean);
    if (lines.length >= 2) return lines[0]; // Chromium 非原子写入：两行写全才算有效
    if (isExited()) throw new Error("Go 壳在打开调试端口前退出了");
    if (Date.now() > deadline) throw new Error("等待 WebView2 调试端口超时");
    await new Promise((r) => setTimeout(r, 50));
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const sortedEntries = (obj) => Object.fromEntries(Object.entries(obj).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));

/** 在 root 下找 `Local Storage/leveldb`（含 CURRENT 才算），返回绝对路径。 */
function findLevelDB(root) {
  const hits = [];
  const walk = (dir, depth) => {
    if (depth > 6) return;
    for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
      if (!e.isDirectory()) continue;
      const full = path.join(dir, e.name);
      if (e.name === "leveldb" && path.basename(dir) === "Local Storage" && fs.existsSync(path.join(full, "CURRENT"))) hits.push(full);
      else walk(full, depth + 1);
    }
  };
  walk(root, 0);
  if (hits.length !== 1) throw new Error(`期望恰好一个 Local Storage/leveldb，实际 ${hits.length} 个：${hits.join(" | ")}`);
  return hits[0];
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-ls-fixture-"));
  const webview2Dir = path.join(userDataDir, "webview2");
  const dbFile = path.join(userDataDir, "data", "knowledge.db");
  fs.mkdirSync(path.dirname(dbFile), { recursive: true });

  let child = null;
  let exited = false;
  let output = "";
  let browser = null;
  let result = null;
  let failure = null;
  try {
    child = spawn(args.executable, ["-dist", args.dist, "-userdata", userDataDir, "-db", dbFile, "-bench"], {
      windowsHide: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: {
        ...process.env,
        WEBVIEW2_USER_DATA_FOLDER: webview2Dir, // 一次性目录，绝不共用真实安装的 %LOCALAPPDATA%\GuiZhi\WebView2
        WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS: "--remote-debugging-port=0",
      },
    });
    child.stdout.on("data", (d) => { output += d; });
    child.stderr.on("data", (d) => { output += d; });
    child.once("exit", () => { exited = true; });
    child.once("error", (e) => { output += `\n[spawn error] ${e.message}`; exited = true; });

    let port;
    try {
      port = await readDevToolsPort(userDataDir, () => exited, Date.now() + START_TIMEOUT_MS);
    } catch (error) {
      throw new Error(`${error.message}\n--- Go 壳输出（末尾）---\n${output.split(/\r?\n/).slice(-30).join("\n")}`);
    }
    browser = await chromium.connectOverCDP(`http://127.0.0.1:${port}`);
    const pages = browser.contexts().flatMap((c) => c.pages());
    const page = pages.find((p) => p.url().includes("guizhi.localhost")) ?? pages[0];
    if (!page) throw new Error("已连接调试端口，但没有找到页面");
    await page.waitForLoadState("domcontentloaded");
    await page.getByTestId(READY_TEST_ID).waitFor({ state: "attached", timeout: READY_TIMEOUT_MS });
    const origin = await page.evaluate(() => location.origin);

    // 写入种子并等落盘；应用在空闲时不会改写这些键，若被改写（读回不一致）就重来。
    let entries = null;
    for (let attempt = 1; attempt <= SEED_ATTEMPTS && !entries; attempt += 1) {
      await page.evaluate((seed) => { for (const [k, v] of Object.entries(seed)) localStorage.setItem(k, v); }, SEED);
      await sleep(FLUSH_WAIT_MS);
      const readBack = await page.evaluate(() => Object.fromEntries(Object.keys(localStorage).map((k) => [k, localStorage.getItem(k)])));
      const drift = Object.keys(SEED).filter((k) => readBack[k] !== SEED[k]);
      if (drift.length === 0) entries = readBack;
      else console.warn(`第 ${attempt} 次：种子被应用改写（${drift.join("、")}），重试`);
    }
    if (!entries) throw new Error("种子反复被应用改写，无法得到稳定的夹具");
    result = { origin, entries: sortedEntries(entries), browserVersion: browser.version() };
  } catch (error) {
    failure = error;
  } finally {
    try { await browser?.close(); } catch { /* 已断开 */ }
    if (child && !exited) {
      const done = new Promise((resolve) => child.once("exit", resolve));
      spawnSync("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore" }); // 只按 PID
      await Promise.race([done, sleep(10_000)]);
    }
  }

  try {
    if (failure) throw failure;
    await sleep(1000); // 进程树退出后再等一下，确保文件句柄释放
    const src = findLevelDB(webview2Dir);
    const rel = path.relative(webview2Dir, src).split(path.sep).join("/");
    const dst = path.join(args.out, ...rel.split("/"));
    fs.rmSync(args.out, { recursive: true, force: true });
    fs.mkdirSync(dst, { recursive: true });
    for (const e of fs.readdirSync(src, { withFileTypes: true })) {
      if (e.isFile() && !SKIP_FILES.has(e.name)) fs.copyFileSync(path.join(src, e.name), path.join(dst, e.name));
    }
    fs.writeFileSync(path.join(args.out, ".gitattributes"), "# leveldb 夹具是二进制，禁止换行转换（CURRENT 文件被改成 CRLF 会让库打不开）\n* -text\n");
    fs.writeFileSync(path.join(args.out, "expected.json"), JSON.stringify({ origin: result.origin, entries: result.entries }, null, 2) + "\n");
    fs.writeFileSync(
      path.join(args.out, "meta.json"),
      JSON.stringify(
        {
          generator: "testdata/make-webview2-localstorage.mjs",
          generatedAt: new Date().toISOString(),
          executable: path.basename(args.executable),
          browserVersion: result.browserVersion,
          origin: result.origin,
          leveldbRelativePath: rel,
          seedKeys: Object.keys(SEED).sort(),
        },
        null,
        2,
      ) + "\n",
    );
    console.log(`夹具已写入 ${args.out}`);
    console.log(`  origin=${result.origin}  内核=${result.browserVersion}  路径=${rel}  键数=${Object.keys(result.entries).length}`);
  } finally {
    try {
      fs.rmSync(userDataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
    } catch (error) {
      console.warn(`临时目录删除失败，请稍后手动清理：${userDataDir}（${error.message}）`);
    }
  }
}

try {
  await main();
} catch (error) {
  console.error(error instanceof UsageError ? error.message : (error?.stack ?? error));
  process.exitCode = 1;
}
