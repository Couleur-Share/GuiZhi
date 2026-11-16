// Go 壳（MyGo + WebView2）的隔离验证入口：复制当前源码到临时目录构建并离屏运行，不碰开发目录的 dist/out 与用户数据。
//
//   node scripts/isolated-go.mjs shot [--steps <文件>] [--out <目录>] [--data-db <knowledge.db>] [--keep-profile]
//   node scripts/isolated-go.mjs shot-native [--module <页>] [--theme light|dark] [--lang zh-Hans|en] [--out <目录>] [--data-db <db>]
//     原生界面（feat/native-ui）：只构建 Go 壳（不需要 vite/node_modules），离屏启动 -native 并由进程自己截图，见 native_boot.go。
//
// 流程：复制当前源码（含未提交改动）到系统临时目录 → 在副本里构建渲染进程、preload、阅读库和 Go 壳 → 离屏启动并截图。
// 开发目录的 dist/、out/ 与数据目录都不会被读写；WebView2 配置与用户数据同样落在一次性临时目录里。
//
// 构建步骤与 apps/desktop-go/scripts/build-shell.ps1 的 [1/4]~[3/4] 一致（那份脚本依赖 pnpm exec 与 .bin，副本里没有 .bin，
// 所以这里用包内 CLI 的绝对路径复刻），最后一步放置 SQLite 原生库则与它共用 ensure-sqlitecore.ps1。改了其中一处，另一处要同步。
import { execFileSync, spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { createDesktopSnapshot } from "./desktop-validation-snapshot.mjs";

const REPO_ROOT = path.resolve(import.meta.dirname, "..");

/** 副本里只需要 Go 壳构建用得到的文件；testdata 与 verify 体积大且与截图无关。 */
export const GO_SOURCE_PATHS = [
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

/** 返回去掉某个环境变量的副本（Windows 上变量名不区分大小写，按小写比较）。 */
export function omitEnv(env, name) {
  const lower = name.toLowerCase();
  return Object.fromEntries(Object.entries(env).filter(([key]) => key.toLowerCase() !== lower));
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

/**
 * 在副本里构建；返回 { exe, dist }，失败时抛出带日志尾部的错误。
 * renderer=false 只构建 Go 壳与 SQLite 原生库（原生界面用：不加载渲染进程，不需要 vite 与 node_modules）。
 */
export async function buildInSnapshot(snapshot, env, { renderer = true } = {}) {
  const root = snapshot.root;
  const goRoot = path.join(root, "apps/desktop-go");
  const logFile = path.join(root, "build.log");
  const outDir = path.join(root, "out-go");
  fs.mkdirSync(outDir, { recursive: true });
  const version = JSON.parse(fs.readFileSync(path.join(snapshot.desktopRoot, "package.json"), "utf8")).version;
  const vite = path.join(snapshot.desktopRoot, "node_modules/vite/bin/vite.js");
  const tsx = path.join(root, "node_modules/tsx/dist/cli.mjs");
  if (renderer)
    for (const required of [vite, tsx])
      if (!fs.existsSync(required)) throw new Error(`缺少依赖 ${path.relative(root, required)}：请先在开发目录执行 pnpm install`);

  const rendererSteps = renderer
    ? [
        ["渲染进程与 preload（vite）", process.execPath, [vite, "build", "--config", "../desktop-go/vite.preload.config.ts"], snapshot.desktopRoot],
        ["渲染进程与 preload（vite）", process.execPath, [vite, "build", "--config", "../desktop-go/vite.renderer.config.ts"], snapshot.desktopRoot],
        // 新产物必须在体积预算内（失败即中止，避免带着超预算的包去做截图）
        ["包体预算", process.execPath, ["--experimental-strip-types", "scripts/check-bundle-budget.mts"], snapshot.desktopRoot],
        ["阅读库", process.execPath, [tsx, "apps/desktop-go/jsgen/build-reading-libs.mts"], root],
      ]
    : [];
  const steps = [
    ...rendererSteps,
    [
      "Go 壳",
      "go",
      // mygo_nodownload 与发布构建一致：缺原生库时立刻报错，而不是静默联网下载。
      ["build", "-trimpath", "-tags", "mygo_nodownload", "-ldflags", `-s -w -X main.appVersion=${version}`, "-o", path.join(outDir, "GuiZhi.exe"), "."],
      goRoot,
      { ...env, GOOS: "windows", GOARCH: "amd64", CGO_ENABLED: "0" },
    ],
    // MyGo 0.9 起 SQLite 是原生库（Sqlitecore.dll），带 -trimpath 的二进制只会在 exe 同目录找它。
    // 与发布构建共用同一份脚本（取自用户缓存或按清单下载并校验）；必须排在 go build 之后：它靠 go list 找已下载的 MyGo 模块。
    // 子进程用 Windows PowerShell 5.1（一定存在）。从 pwsh 7 里启动时会继承它的 PSModulePath，5.1 会加载到 7 的核心模块而丢失
    // Get-FileHash 等命令，所以把 PSModulePath 去掉，让 5.1 用自己的默认值。
    [
      "SQLite 原生库",
      "powershell.exe",
      ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", path.join(goRoot, "scripts/ensure-sqlitecore.ps1"), "-Out", outDir, "-GoRoot", goRoot],
      root,
      omitEnv(env, "PSModulePath"),
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

const NATIVE_MODULES = ["library", "inbox", "ask", "wiki", "imports", "settings"];
const NATIVE_THEMES = ["light", "dark", "system"];
const NATIVE_FONTS = ["small", "medium", "large"];
const NATIVE_MOTIONS = ["system", "off", "reduced", "standard"];
const NATIVE_MATRICES = ["appearance", "fonts", "glass"];
const NATIVE_USAGE =
  "用法：node scripts/isolated-go.mjs shot-native [--module <library|inbox|ask|wiki|imports|settings>] [--theme light|dark|system] " +
  "[--hue <预设 ID|色相,饱和度>] [--font small|medium|large] [--motion system|off|reduced|standard] " +
  "[--wallpaper <demo|图片文件>] [--wallpaper-opacity <0-1>] [--wallpaper-blur <0-50>] [--matrix appearance|fonts|glass] " +
  "[--lang zh-Hans|en] [--width <DIP>] [--height <DIP>] [--wait <如 2s>] [--out <目录>] [--name <文件名>] [--data-db <db>]";

/** 缺省文件名：带上所有偏离默认的外观项，多张截图互不覆盖。 */
export function nativeShotName(args) {
  return [
    "native", args.module, args.theme, args.lang,
    ...(args.hue ? [`hue-${args.hue.replace(",", "x")}`] : []),
    ...(args.font ? [`font-${args.font}`] : []),
    ...(args.motion ? [`motion-${args.motion}`] : []),
    ...(args.wallpaper ? [`wallpaper${args.wallpaperBlur ? `-blur${args.wallpaperBlur}` : ""}`] : []),
  ].join("-");
}

/** 渲染层设置里的主题色预设 ID：矩阵直接读源文件，不在脚本里再抄一份。 */
export function huePresetIds(appearanceSource) {
  const ids = [...appearanceSource.matchAll(/\{\s*id:\s*"([^"]+)",\s*hue:\s*\d+,\s*saturation:\s*\d+/g)].map((m) => m[1]);
  if (ids.length === 0) throw new Error("settings-appearance.ts 里找不到 MORANDI_THEMES");
  return ids;
}

/**
 * 一次构建、多格截图：把 --matrix 展开成一组完整的参数。纯函数，便于单测。
 *   appearance：每个主题色预设 × 浅/深（12 格），看色相对强调色族的影响与对比度
 *   fonts：三个字号档（沿用 --theme）
 *   glass：演示壁纸 × 浅/深 × 模糊 0/12（4 格），看壁纸模式的半透明面板
 * 没有 --matrix 时就是原参数一格。
 */
export function nativeMatrixVariants(args, presetIds) {
  const vary = (patch) => {
    const v = { ...args, ...patch, matrix: null, name: null };
    v.name = nativeShotName(v);
    return v;
  };
  switch (args.matrix) {
    case "appearance":
      return presetIds.flatMap((hue) => ["light", "dark"].map((theme) => vary({ hue, theme })));
    case "fonts":
      return NATIVE_FONTS.map((font) => vary({ font }));
    case "glass":
      return ["light", "dark"].flatMap((theme) =>
        [0, 12].map((blur) => vary({ theme, wallpaper: "demo", wallpaperBlur: blur, wallpaperOpacity: args.wallpaperOpacity })));
    default:
      return [args];
  }
}

/** 解析并校验 shot-native 参数；路径按调用方 cwd 解析，缺省 --out 与 shot 一致。纯函数，便于单测。 */
export function nativeShotArgs(argv, cwd) {
  const args = {
    module: "imports", theme: "light", lang: "zh-Hans", width: 1280, height: 800, wait: "2s",
    out: path.join(REPO_ROOT, "apps/desktop-go/.tmp-shots"), name: null, dataDb: null,
    hue: null, font: null, motion: null, wallpaper: null, wallpaperOpacity: null, wallpaperBlur: null, matrix: null,
  };
  const ranged = (flag, text, lo, hi) => {
    const n = Number(text);
    if (text.trim() === "" || !Number.isFinite(n) || n < lo || n > hi) throw new Error(`${flag} 需要 ${lo}–${hi} 之间的数，得到 ${text}`);
    return n;
  };
  const integer = (flag, text) => {
    const n = Number(text);
    if (!Number.isInteger(n) || n < 320 || n > 7680) throw new Error(`${flag} 需要 320–7680 之间的整数，得到 ${text}`);
    return n;
  };
  for (let i = 0; i < argv.length; i += 1) {
    const flag = argv[i];
    const value = () => {
      if (argv[i + 1] === undefined) throw new Error(`${flag} 缺少值`);
      return argv[++i];
    };
    switch (flag) {
      case "--module": args.module = value(); break;
      case "--theme": args.theme = value(); break;
      case "--hue": args.hue = value(); break;
      case "--font": args.font = value(); break;
      case "--motion": args.motion = value(); break;
      case "--wallpaper": {
        const v = value();
        args.wallpaper = v === "demo" ? v : path.resolve(cwd, v);
        break;
      }
      case "--wallpaper-opacity": args.wallpaperOpacity = ranged(flag, value(), 0, 1); break;
      case "--wallpaper-blur": args.wallpaperBlur = ranged(flag, value(), 0, 50); break;
      case "--matrix": args.matrix = value(); break;
      case "--lang": args.lang = value(); break;
      case "--width": args.width = integer(flag, value()); break;
      case "--height": args.height = integer(flag, value()); break;
      case "--wait": args.wait = value(); break;
      case "--out": args.out = path.resolve(cwd, value()); break;
      case "--name": args.name = value(); break;
      case "--data-db": args.dataDb = path.resolve(cwd, value()); break;
      default: throw new Error(`未知参数 ${flag}\n${NATIVE_USAGE}`);
    }
  }
  if (!NATIVE_MODULES.includes(args.module)) throw new Error(`--module 取值：${NATIVE_MODULES.join("|")}，得到 ${args.module}`);
  if (!NATIVE_THEMES.includes(args.theme)) throw new Error(`--theme 取值：${NATIVE_THEMES.join("|")}，得到 ${args.theme}`);
  // 预设 ID 是否存在由 Go 侧校验（预设来自渲染层设置，这里不再抄一份）；这里只拦明显写错的格式，省下一次构建
  if (args.hue !== null && !/^([a-z][a-z-]*|\d+(\.\d+)?,\d+(\.\d+)?)$/.test(args.hue)) {
    throw new Error(`--hue 需要预设 ID（如 royal-blue）或「色相,饱和度」（如 150,30），得到 ${args.hue}`);
  }
  if (args.font !== null && !NATIVE_FONTS.includes(args.font)) throw new Error(`--font 取值：${NATIVE_FONTS.join("|")}，得到 ${args.font}`);
  if (args.motion !== null && !NATIVE_MOTIONS.includes(args.motion)) throw new Error(`--motion 取值：${NATIVE_MOTIONS.join("|")}，得到 ${args.motion}`);
  if (args.wallpaper === null && (args.wallpaperOpacity !== null || args.wallpaperBlur !== null)) {
    throw new Error("--wallpaper-opacity 与 --wallpaper-blur 需要同时给出 --wallpaper");
  }
  if (args.wallpaper !== null && args.wallpaper !== "demo" && !fs.existsSync(args.wallpaper)) throw new Error(`--wallpaper 文件不存在：${args.wallpaper}`);
  if (!["zh-Hans", "en"].includes(args.lang)) throw new Error(`--lang 取值：zh-Hans|en，得到 ${args.lang}`);
  if (!/^\d+(\.\d+)?(ms|s|m)$/.test(args.wait)) throw new Error(`--wait 需要 Go 时长写法（如 2s、500ms），得到 ${args.wait}`);
  if (args.matrix !== null && !NATIVE_MATRICES.includes(args.matrix)) throw new Error(`--matrix 取值：${NATIVE_MATRICES.join("|")}，得到 ${args.matrix}`);
  if (args.matrix !== null && args.name !== null) throw new Error("--matrix 给每一格自动命名，不能同时指定 --name");
  args.name ??= nativeShotName(args);
  return args;
}

/** 外观相关的 Go 命令行开关：只带用户给了的项，其余交给 Go 侧的默认值。纯函数，便于单测。 */
export function nativeAppearanceFlags(args) {
  return [
    ...(args.hue ? ["-native-hue", args.hue] : []),
    ...(args.font ? ["-native-font", args.font] : []),
    ...(args.motion ? ["-native-motion", args.motion] : []),
    ...(args.wallpaper ? ["-native-wallpaper", args.wallpaper] : []),
    ...(args.wallpaperOpacity !== null ? ["-native-wallpaper-opacity", String(args.wallpaperOpacity)] : []),
    ...(args.wallpaperBlur !== null ? ["-native-wallpaper-blur", String(args.wallpaperBlur)] : []),
  ];
}

/**
 * 原生界面截图进程的环境：不继承 WebView2 变量；APPDATA 指到一次性目录，
 * 这样 MyGo 自己的用户数据（语言偏好等，位于 %APPDATA%\归知）也碰不到真实安装；语言用 MYGO_LOCALE 临时覆盖。
 */
export function nativeShotEnv(env, userDataDir, lang) {
  const isolated = goValidationEnv(env);
  isolated.APPDATA = path.join(userDataDir, "appdata");
  isolated.MYGO_LOCALE = lang;
  return isolated;
}

/** 用已构建的 exe 拍一格：每格有自己一次性的用户数据目录与库，格与格之间不串状态。返回退出码。 */
async function shootNative(exe, args) {
  const userDataDir = fs.mkdtempSync(path.join(fs.realpathSync.native(os.tmpdir()), "guizhi-native-shot-"));
  try {
    const dbFile = path.join(userDataDir, "data", "knowledge.db");
    fs.mkdirSync(path.dirname(dbFile), { recursive: true });
    if (args.dataDb) {
      fs.copyFileSync(args.dataDb, dbFile);
      // WAL 模式下最近的写入可能还在 -wal 里：旁文件一起带上（源库须已关闭）。
      const wal = `${args.dataDb}-wal`;
      if (fs.existsSync(wal) && fs.statSync(wal).size > 0) fs.copyFileSync(wal, `${dbFile}-wal`);
    }
    const file = path.join(args.out, `${args.name}.png`);
    fs.rmSync(file, { force: true });
    const exeArgs = [
      "-native", "-native-module", args.module, "-native-theme", args.theme,
      ...nativeAppearanceFlags(args),
      "-native-width", String(args.width), "-native-height", String(args.height),
      "-userdata", userDataDir, "-db", dbFile, "-shot", file, "-wait", args.wait,
      // 没给库副本就用确定的演示数据（仅写这次的一次性库）
      ...(args.dataDb ? [] : ["-native-demo"]),
    ];
    const code = await run(exe, exeArgs, { cwd: userDataDir, env: nativeShotEnv(process.env, userDataDir, args.lang) });
    if (code !== 0) return code;
    if (!fs.existsSync(file)) throw new Error(`原生界面退出码为 0，但没有产出截图：${file}`);
    console.log(`截图：${file}`);
    return 0;
  } finally {
    fs.rmSync(userDataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 200 });
  }
}

/** 隔离构建 Go 壳并离屏启动原生界面截图；--matrix 时一次构建拍多格。返回进程退出码。 */
async function runIsolatedNative(argv, cwd) {
  const args = nativeShotArgs(argv, cwd);
  const presets = huePresetIds(fs.readFileSync(path.join(REPO_ROOT, "apps/desktop/src/renderer/stores/settings/settings-appearance.ts"), "utf8"));
  const variants = nativeMatrixVariants(args, presets);
  fs.mkdirSync(args.out, { recursive: true });
  const snapshot = createDesktopSnapshot(REPO_ROOT, { extraSourcePaths: GO_SOURCE_PATHS, reuseDependencies: false });
  console.log(`隔离验证：已复制 ${snapshot.copied} 个当前文件 → ${snapshot.root}`);
  try {
    const { exe } = await buildInSnapshot(snapshot, goValidationEnv(), { renderer: false });
    for (const variant of variants) {
      const code = await shootNative(exe, variant);
      if (code !== 0) return code;
    }
    if (variants.length > 1) console.log(`共 ${variants.length} 格，输出目录：${args.out}`);
    return 0;
  } finally {
    snapshot.cleanup();
  }
}

/** 隔离构建并运行截图。返回进程退出码。 */
export async function runIsolatedGo(command, argv = [], cwd = process.cwd()) {
  if (command === "shot-native") {
    if (process.platform !== "win32") throw new Error("Go 壳仅支持 Windows");
    return runIsolatedNative(argv, cwd);
  }
  if (command !== "shot") throw new Error("用法：node scripts/isolated-go.mjs shot [--steps <文件>] [--out <目录>] [--data-db <db>] [--keep-profile]\n" + NATIVE_USAGE);
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
