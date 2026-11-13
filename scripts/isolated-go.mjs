// Go 壳（MyGo + WebView2）的隔离验证入口：与 isolated-desktop.mjs 同一套思路。
//
//   node scripts/isolated-go.mjs shot [--steps <文件>] [--out <目录>] [--data-db <knowledge.db>] [--keep-profile]
//
// 流程：复制当前源码（含未提交改动）到系统临时目录 → 在副本里构建渲染进程、preload、阅读库和 Go 壳 → 离屏启动并截图。
// 开发目录的 dist/、out/ 与数据目录都不会被读写；WebView2 配置与用户数据同样落在一次性临时目录里。
//
// 构建步骤与 apps/desktop-go/scripts/build-shell.ps1 的 [1/4]~[3/4] 一致（那份脚本依赖 pnpm exec 与 .bin，副本里没有 .bin，
// 所以这里用包内 CLI 的绝对路径复刻）。改了其中一处，另一处要同步。
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { createDesktopSnapshot } from "./desktop-validation-snapshot.mjs";

const REPO_ROOT = path.resolve(import.meta.dirname, "..");

/** 副本里只需要 Go 壳构建用得到的文件；testdata 与 verify 体积大且与截图无关。 */
const GO_SOURCE_PATHS = [
  "apps/desktop-go",
  ":(exclude)apps/desktop-go/testdata",
  ":(exclude)apps/desktop-go/verify",
];

/** 不继承开发服务地址、Electron 模式，以及可能指向用户 WebView2 配置的变量。 */
export function goValidationEnv(env = process.env) {
  const result = { ...env, NODE_ENV: "production" };
  for (const key of [
    "VITE_DEV_SERVER_URL",
    "ELECTRON_RUN_AS_NODE",
    "WEBVIEW2_USER_DATA_FOLDER",
    "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
    "WEBVIEW2_BROWSER_EXECUTABLE_FOLDER",
  ])
    delete result[key];
  return result;
}

/** 把相对路径参数按调用方 cwd 解析成绝对路径；未指定 --out 时落到 apps/desktop-go/.tmp-shots。 */
export function goShotArgs(argv, cwd) {
  const pathOptions = new Set(["--steps", "--out", "--data-db"]);
  const result = [];
  for (let i = 0; i < argv.length; i += 1) {
    result.push(argv[i]);
    if (pathOptions.has(argv[i])) {
      const value = argv[++i];
      if (!value) throw new Error(`${argv[i - 1]} 缺少值`);
      result.push(path.resolve(cwd, value));
    }
  }
  if (!argv.includes("--out")) result.push("--out", path.join(REPO_ROOT, "apps/desktop-go/.tmp-shots"));
  return result;
}

function run(command, args, { cwd, env, logFile }) {
  return new Promise((resolve, reject) => {
    const fd = logFile ? fs.openSync(logFile, "a") : null;
    const child = spawn(command, args, {
      cwd,
      env,
      windowsHide: true,
      stdio: fd === null ? "inherit" : ["ignore", fd, fd],
    });
    // 只终止本次创建的子进程树，禁止按进程名批量结束。
    const stop = () => {
      if (!child.pid) return;
      try {
        execFileSync("taskkill", ["/pid", String(child.pid), "/T", "/F"], { windowsHide: true, stdio: "ignore" });
      } catch {
        /* 子进程可能已退出 */
      }
    };
    process.once("SIGINT", stop);
    process.once("SIGTERM", stop);
    const finish = () => {
      process.removeListener("SIGINT", stop);
      process.removeListener("SIGTERM", stop);
      if (fd !== null) fs.closeSync(fd);
    };
    child.once("error", (error) => { finish(); reject(error); });
    child.once("exit", (code) => { finish(); resolve(code ?? 1); });
  });
}

/** 在副本里构建；返回 { exe, dist }，失败时抛出带日志尾部的错误。 */
async function buildInSnapshot(snapshot, env) {
  const root = snapshot.root;
  const goRoot = path.join(root, "apps/desktop-go");
  const logFile = path.join(root, "build.log");
  const outDir = path.join(root, "out-go");
  fs.mkdirSync(outDir, { recursive: true });
  const version = JSON.parse(fs.readFileSync(path.join(snapshot.desktopRoot, "package.json"), "utf8")).version;
  const vite = path.join(snapshot.desktopRoot, "node_modules/vite/bin/vite.js");
  const tsx = path.join(root, "node_modules/tsx/dist/cli.mjs");
  for (const required of [vite, tsx])
    if (!fs.existsSync(required)) throw new Error(`缺少依赖 ${path.relative(root, required)}：请先在开发目录执行 pnpm install`);

  const steps = [
    ["渲染进程与 preload（vite）", process.execPath, [vite, "build", "--config", "../desktop-go/vite.preload.config.ts"], snapshot.desktopRoot],
    ["渲染进程与 preload（vite）", process.execPath, [vite, "build", "--config", "../desktop-go/vite.renderer.config.ts"], snapshot.desktopRoot],
    ["阅读库", process.execPath, [tsx, "apps/desktop-go/jsgen/build-reading-libs.mts"], root],
    [
      "Go 壳",
      "go",
      ["build", "-trimpath", "-ldflags", `-s -w -X main.appVersion=${version}`, "-o", path.join(outDir, "GuiZhi.exe"), "."],
      goRoot,
      { ...env, GOOS: "windows", GOARCH: "amd64", CGO_ENABLED: "0" },
    ],
  ];
  const started = Date.now();
  for (const [label, command, args, cwd, stepEnv] of steps) {
    console.log(`构建：${label}`);
    const status = await run(command, args, { cwd, env: stepEnv ?? env, logFile });
    if (status !== 0) {
      const tail = fs.readFileSync(logFile, "utf8").split(/\r?\n/).slice(-65).join("\n");
      throw new Error(`构建失败（${label}，退出码 ${status}）\n${tail}`);
    }
  }
  console.log(`隔离构建完成，用时 ${((Date.now() - started) / 1000).toFixed(1)}s；开发目录的 dist/ 与 out/ 未被使用。`);
  return { exe: path.join(outDir, "GuiZhi.exe"), dist: path.join(goRoot, "dist") };
}

/** 隔离构建并运行截图。返回进程退出码。 */
export async function runIsolatedGo(command, argv = [], cwd = process.cwd()) {
  if (command !== "shot") throw new Error("用法：node scripts/isolated-go.mjs shot [--steps <文件>] [--out <目录>] [--data-db <db>] [--keep-profile]");
  if (process.platform !== "win32") throw new Error("Go 壳仅支持 Windows（WebView2）");
  const args = goShotArgs(argv, cwd);
  const snapshot = createDesktopSnapshot(REPO_ROOT, { extraSourcePaths: GO_SOURCE_PATHS });
  const env = goValidationEnv();
  console.log(`隔离验证：已复制 ${snapshot.copied} 个当前文件 → ${snapshot.root}`);
  try {
    const { exe, dist } = await buildInSnapshot(snapshot, env);
    // 步骤脚本保持原来的相对 fixture 路径，所以以调用方 cwd 运行；脚本本身取自副本。
    return await run(
      process.execPath,
      [path.join(snapshot.root, "apps/desktop-go/scripts/shot.mjs"), "--executable", exe, "--dist", dist, ...args],
      { cwd, env },
    );
  } finally {
    snapshot.cleanup();
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  try {
    process.exitCode = await runIsolatedGo(process.argv[2], process.argv.slice(3));
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
