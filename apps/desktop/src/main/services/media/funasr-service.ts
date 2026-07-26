/**
 * 本地转写服务进程托管：按需启动 funasr-server、健康检查、随应用退出停止。
 *
 * 设计：服务不常驻——首次转写 / 测试 / 安装时 ensure 启动（模型加载约
 * 10-20 秒），已在监听的实例（含外部手动启动的）直接采用；应用退出时
 * 结束由本模块拉起的进程。
 */
import { spawn, type ChildProcess } from "child_process";
import {
  FUNASR_BASE_URL,
  FUNASR_MODEL_NAME,
  FUNASR_PORT,
  getFunasrPaths,
  isFunasrInstalled,
} from "./funasr-paths";
import { ensureFunasrServerScript } from "./funasr-server-script";

const DEFAULT_BOOT_TIMEOUT_MS = 2 * 60 * 1000;
const HEALTH_POLL_INTERVAL_MS = 2000;
const LOG_TAIL_MAX = 4096;

let child: ChildProcess | null = null;
let childExited = false;
let logTail = "";
let startingPromise: Promise<void> | null = null;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function appendLogTail(chunk: Buffer): void {
  logTail = (logTail + chunk.toString("utf8")).slice(-LOG_TAIL_MAX);
}

/** 服务健康探测：/v1/models 应答即视为可用 */
export async function probeFunasrHealth(timeoutMs = 3000): Promise<boolean> {
  try {
    const response = await fetch(`${FUNASR_BASE_URL}/models`, {
      signal: AbortSignal.timeout(timeoutMs),
    });
    return response.ok;
  } catch {
    return false;
  }
}

/** 判断 apiUrl 是否指向托管的本地转写服务 */
export function isManagedFunasrUrl(apiUrl: string): boolean {
  try {
    const url = new URL(apiUrl.trim().replace(/#$/, ""));
    return (
      (url.hostname === "127.0.0.1" || url.hostname === "localhost") &&
      url.port === String(FUNASR_PORT)
    );
  } catch {
    return false;
  }
}

async function startAndWait(bootTimeoutMs: number): Promise<void> {
  const paths = getFunasrPaths();
  // 脚本内容随应用版本走：升级后无需重装引擎，这里覆盖到最新
  ensureFunasrServerScript(paths.serverScript);
  logTail = "";
  childExited = false;
  child = spawn(
    paths.venvPython,
    [
      paths.serverScript,
      "--device",
      "cpu",
      "--model",
      FUNASR_MODEL_NAME,
      "--host",
      "127.0.0.1",
      "--port",
      String(FUNASR_PORT),
    ],
    {
      windowsHide: true,
      env: { ...process.env, MODELSCOPE_CACHE: paths.modelsDir },
    },
  );
  child.stdout?.on("data", appendLogTail);
  child.stderr?.on("data", appendLogTail);
  child.on("exit", () => {
    childExited = true;
  });
  child.on("error", () => {
    childExited = true;
  });

  const deadline = Date.now() + bootTimeoutMs;
  while (Date.now() < deadline) {
    if (childExited) {
      child = null;
      throw new Error(
        `本地转写服务启动失败：${logTail.trim().slice(-300) || "进程异常退出"}`,
      );
    }
    if (await probeFunasrHealth(2000)) {
      console.log(`[funasr] 服务已就绪（127.0.0.1:${FUNASR_PORT}）`);
      return;
    }
    await sleep(HEALTH_POLL_INTERVAL_MS);
  }

  stopFunasrService();
  throw new Error("本地转写服务启动超时");
}

/**
 * 确保托管服务可用：已监听则直接采用；否则拉起并等待健康。
 * 首次安装（需下载模型）可传更长的 bootTimeoutMs。
 */
export async function ensureFunasrService(
  bootTimeoutMs = DEFAULT_BOOT_TIMEOUT_MS,
): Promise<void> {
  if (await probeFunasrHealth()) {
    return;
  }
  // 判的是「有没有在途的启动」，不是 await 这个 Promise
  // eslint-disable-next-line @typescript-eslint/no-misused-promises
  if (startingPromise) {
    return startingPromise;
  }
  if (!isFunasrInstalled()) {
    throw new Error("本地转写引擎未安装");
  }
  startingPromise = startAndWait(bootTimeoutMs).finally(() => {
    startingPromise = null;
  });
  return startingPromise;
}

/** 停止由本模块拉起的服务进程（Windows 下杀进程树，避免遗留 python 子进程） */
export function stopFunasrService(): void {
  const pid = child?.pid;
  child = null;
  if (!pid) {
    return;
  }
  try {
    if (process.platform === "win32") {
      spawn("taskkill", ["/T", "/F", "/PID", String(pid)], {
        windowsHide: true,
      });
    } else {
      process.kill(pid);
    }
    console.log(`[funasr] 已停止服务进程（pid=${pid}）`);
  } catch {
    // 进程可能已自行退出
  }
}

/**
 * 转写前置钩子：目标是托管服务地址且引擎已安装时，确保服务在运行。
 * 指向其他地址（云端 / 用户自管服务）时不做任何事。
 */
export async function ensureLocalTranscriptionService(
  apiUrl: string,
): Promise<void> {
  if (!isManagedFunasrUrl(apiUrl) || !isFunasrInstalled()) {
    return;
  }
  await ensureFunasrService();
}
