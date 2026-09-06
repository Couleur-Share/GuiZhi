/**
 * 界面截图：拉起真实 Electron、截图、退出，全程不抢焦点也不占屏幕。
 *
 * 窗口走 GUIZHI_WINDOW_MODE=offscreen（挪到所有显示器之外、不进任务栏、不激活），
 * 所以可以在用户正干活时随便跑。数据目录是一次性临时目录，碰不到用户的库。
 *
 *   node scripts/screenshot.mjs                       # 首页截一张
 *   node scripts/screenshot.mjs --steps my-steps.mjs  # 按脚本走到指定界面再截
 *   node scripts/screenshot.mjs --data-db path/to/knowledge.db # 从数据库副本截图
 *   node scripts/screenshot.mjs --visible             # 让窗口正常显示（人工盯着看时用）
 *
 * steps 文件默认导出一个函数，拿到 Playwright 的窗口对象与 shot()：
 *
 *   export default async ({ win, shot }) => {
 *     await win.getByTestId("rail-settings").click();
 *     await shot("settings");
 *   };
 */
import fs from "node:fs";
import http from "node:http";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

// 验收包可复用随包 Playwright 驱动，不要求干净机器安装 Node/pnpm。
const { _electron: electron } = await import(
  process.env.GUIZHI_SHOT_PLAYWRIGHT
    ? pathToFileURL(path.resolve(process.env.GUIZHI_SHOT_PLAYWRIGHT)).href
    : "playwright"
);

const DESKTOP_ROOT = path.resolve(import.meta.dirname, "..");
const MAIN_ENTRY = path.join(DESKTOP_ROOT, "out/main/index.js");
const RENDERER_ENTRY = path.join(DESKTOP_ROOT, "out/renderer/index.html");
const RENDERER_ROOT = path.dirname(RENDERER_ENTRY);
/** 首屏就绪的判据：顶栏搜索框出现即说明渲染进程与数据库都通了 */
const READY_TEST_ID = "topbar-search";
const READY_TIMEOUT_MS = 30_000;
/**
 * 关动画、藏光标——不加这两项，界面就绪后立刻截的第一张会抓到淡入过渡的中间态：
 * 实测同一界面连截三张，默认选项下首张与后两张不一致，加上之后三张逐字节相同。
 * 调用方可以在 shot() 的第二个参数里覆盖。
 */
const DEFAULT_SHOT_OPTIONS = { animations: "disabled", caret: "hide" };

/** 用法错误（参数写错、产物没构建）只打一句话就够，调用栈是纯噪音 */
class UsageError extends Error {}

const CONTENT_TYPES = new Map([
  [".css", "text/css; charset=utf-8"],
  [".html", "text/html; charset=utf-8"],
  [".ico", "image/x-icon"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".png", "image/png"],
  [".svg", "image/svg+xml"],
  [".webp", "image/webp"],
]);

/**
 * Electron 33 在部分 Windows 图形环境里直接加载构建产物的 file:// 会返回
 * ERR_FAILED。截图期间只在回环地址提供 out/renderer，既绕过这个兼容问题，
 * 又不需要启动会热更新、会改源码时间戳的 Vite dev server。
 */
async function startRendererServer(rendererRoot = RENDERER_ROOT) {
  const server = http.createServer((request, response) => {
    let relativePath;
    try {
      const pathname = decodeURIComponent(
        new URL(request.url ?? "/", "http://127.0.0.1").pathname,
      );
      relativePath = pathname === "/" ? "index.html" : pathname.slice(1);
    } catch {
      response.writeHead(400).end("Bad request");
      return;
    }

    const filePath = path.resolve(rendererRoot, relativePath);
    if (
      filePath !== rendererRoot &&
      !filePath.startsWith(`${rendererRoot}${path.sep}`)
    ) {
      response.writeHead(403).end("Forbidden");
      return;
    }

    fs.stat(filePath, (error, stat) => {
      if (error || !stat.isFile()) {
        response.writeHead(404).end("Not found");
        return;
      }
      response.writeHead(200, {
        "Cache-Control": "no-store",
        "Content-Type":
          CONTENT_TYPES.get(path.extname(filePath)) ??
          "application/octet-stream",
      });
      fs.createReadStream(filePath).pipe(response);
    });
  });

  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  if (!address || typeof address === "string") {
    server.close();
    throw new Error("无法确定离屏截图 renderer 服务端口");
  }
  return {
    server,
    url: `http://127.0.0.1:${address.port}`,
  };
}

function parseArgs(argv) {
  const args = {
    out: path.join(DESKTOP_ROOT, ".tmp-shots"),
    steps: null,
    dataDb: null,
    visible: false,
    staleOk: false,
    executable: null,
    rendererRoot: null,
    keepProfile: false,
  };
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--out") args.out = path.resolve(argv[++i]);
    else if (arg === "--steps") args.steps = path.resolve(argv[++i]);
    else if (arg === "--data-db") args.dataDb = path.resolve(argv[++i]);
    else if (arg === "--visible") args.visible = true;
    else if (arg === "--stale-ok") args.staleOk = true;
    else if (arg === "--executable") args.executable = path.resolve(argv[++i]);
    else if (arg === "--renderer-root")
      args.rendererRoot = path.resolve(argv[++i]);
    else if (arg === "--keep-profile") args.keepProfile = true;
    else throw new UsageError(`未知参数：${arg}`);
  }
  return args;
}

/** 构建产物的 mtime 必然晚于源码，扫进来会让陈旧检测永久误报 */
const NON_SOURCE_DIRS = new Set(["node_modules", "dist", "out", "build"]);

function latestSourceMtime(dir) {
  let latest = 0;
  const walk = (current) => {
    let entries;
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      if (NON_SOURCE_DIRS.has(entry.name) || entry.name.startsWith("."))
        continue;
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) {
        walk(full);
      } else if (/\.(ts|tsx|css|html|json)$/.test(entry.name)) {
        const { mtimeMs } = fs.statSync(full);
        if (mtimeMs > latest) latest = mtimeMs;
      }
    }
  };
  walk(dir);
  return latest;
}

/**
 * 产物陈旧检测。少了这一步，改完源码忘了构建就会截到上一版界面，
 * 而截图看着完全正常——这种「改动像是没生效」最难自查。
 */
function assertFreshBuild(staleOk) {
  for (const entry of [MAIN_ENTRY, RENDERER_ENTRY]) {
    if (!fs.existsSync(entry)) {
      throw new UsageError(
        `构建产物缺失：${path.relative(DESKTOP_ROOT, entry)}\n先运行：pnpm --filter @guizhi/desktop build`,
      );
    }
  }
  if (staleOk) return;

  // 取最大值（构建结束时刻）而不是最小值：vite 分步构建，renderer 与 main 的
  // 产物实测能差 97 秒，而构建过程本身会触碰源文件的 mtime——按最小值算，
  // 那些落在两个产物之间的源文件每次都会被误判成「构建后又改过」。
  const builtAt = Math.max(
    fs.statSync(MAIN_ENTRY).mtimeMs,
    fs.statSync(RENDERER_ENTRY).mtimeMs,
  );
  const sourceRoots = [
    path.join(DESKTOP_ROOT, "src"),
    path.join(DESKTOP_ROOT, "../../packages"),
  ];
  const changedAt = Math.max(...sourceRoots.map(latestSourceMtime));
  if (changedAt > builtAt) {
    throw new UsageError(
      "源码比 out/ 里的构建产物新，截出来的会是上一版界面。\n" +
        "先运行：pnpm --filter @guizhi/desktop build\n" +
        "（确认无所谓时可加 --stale-ok 跳过这项检查）",
    );
  }
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.executable) {
    if (
      !fs.existsSync(args.executable) ||
      !fs.existsSync(
        path.join(path.dirname(args.executable), "resources/app.asar"),
      )
    )
      throw new UsageError("候选可执行文件或 app.asar 不存在");
  } else {
    assertFreshBuild(args.staleOk);
  }
  fs.mkdirSync(args.out, { recursive: true });

  const stepsFn = args.steps
    ? (await import(pathToFileURL(args.steps).href)).default
    : null;
  if (args.steps && typeof stepsFn !== "function") {
    throw new UsageError(`${args.steps} 需要默认导出一个函数`);
  }
  if (args.dataDb && !fs.existsSync(args.dataDb)) {
    throw new UsageError(`数据库不存在：${args.dataDb}`);
  }
  if (args.dataDb && fs.existsSync(`${args.dataDb}.lock`)) {
    throw new UsageError("数据库正在使用，请先退出归知再从数据副本截图");
  }

  const userDataDir = fs.mkdtempSync(
    path.join(args.keepProfile ? args.out : os.tmpdir(), "guizhi-shot-"),
  );
  if (args.dataDb) {
    const dataDir = path.join(userDataDir, "data");
    fs.mkdirSync(dataDir, { recursive: true });
    // 只复制知识库，不复制 AI Key、浏览器登录态与缓存；应用写入的也是临时副本。
    fs.copyFileSync(args.dataDb, path.join(dataDir, "knowledge.db"));
  }
  const taken = [];
  let failure = null;
  const renderer =
    args.executable && !args.rendererRoot
      ? null
      : await startRendererServer(args.rendererRoot || RENDERER_ROOT);
  let app = null;
  let appClosed = false;

  try {
    app = await electron.launch({
      ...(args.executable ? { executablePath: args.executable } : {}),
      // 离屏实例固定使用 Electron 自带的 SwiftShader，避免依赖主机显卡驱动，
      // 也让不同机器上的截图栅格化结果更接近。
      args: [
        "--use-gl=angle",
        "--use-angle=swiftshader",
        "--disable-gpu-sandbox",
        ...(args.executable ? [] : [MAIN_ENTRY]),
      ],
      env: {
        ...process.env,
        GUIZHI_E2E: "1",
        GUIZHI_E2E_USER_DATA_DIR: userDataDir,
        GUIZHI_E2E_RENDERER_URL: renderer?.url || "",
        GUIZHI_WINDOW_MODE: args.visible ? "visible" : "offscreen",
      },
    });
    app.once("close", () => {
      appClosed = true;
    });
    const win = await app.firstWindow();
    await win.waitForLoadState("domcontentloaded");
    await win
      .getByTestId(READY_TEST_ID)
      .waitFor({ state: "attached", timeout: READY_TIMEOUT_MS });

    const shot = async (name, options = {}) => {
      const file = path.join(args.out, `${name}.png`);
      await win.screenshot({ path: file, ...DEFAULT_SHOT_OPTIONS, ...options });
      taken.push(file);
      return file;
    };

    if (stepsFn) {
      await stepsFn({ win, app, shot, outDir: args.out, userDataDir });
      // steps 一张都没截时兜一张，免得跑完只得到一个空目录
      if (taken.length === 0 && !win.isClosed()) await shot("final");
    } else {
      await shot("home");
    }
  } catch (error) {
    failure = error;
    // 失败现场同样值得留一张：多数时候一眼就能看出是卡在哪个界面
    try {
      if (!app) throw error;
      const win = await app.firstWindow();
      const file = path.join(args.out, "failure.png");
      await win.screenshot({ path: file, timeout: 10_000 });
      taken.push(file);
    } catch {
      // 窗口已经没了就算了
    }
  } finally {
    // 步骤可能已完成真实重启，不能再次关闭已经退出的旧实例。
    if (app && !appClosed) {
      await app.close().catch(() => {});
    }
    if (renderer)
      await new Promise((resolve) => renderer.server.close(resolve));
    if (args.keepProfile) {
      fs.writeFileSync(
        path.join(args.out, "profile.json"),
        JSON.stringify(
          { userDataDir, executable: args.executable, failed: !!failure },
          null,
          2,
        ),
      );
    } else fs.rmSync(userDataDir, { recursive: true, force: true });
  }

  for (const file of taken) {
    console.log(file);
  }
  if (failure) throw failure;
}

try {
  await main();
} catch (error) {
  if (error instanceof UsageError) {
    console.error(error.message);
    process.exit(1);
  }
  throw error;
}
