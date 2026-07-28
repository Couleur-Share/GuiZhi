/**
 * 本地转写服务进程托管：按需启动 funasr-server、健康检查、随应用退出停止。
 *
 * 设计：服务不常驻——首次转写 / 测试 / 安装时 ensure 启动（模型加载约
 * 10-20 秒），已在监听的实例（含外部手动启动的）直接采用；应用退出时
 * 结束由本模块拉起的进程。
 */
import { spawn, type ChildProcess } from "child_process";
import net from "net";
import { StringDecoder } from "string_decoder";
import { isManagedFunasrUrl } from "@guizhi/shared/constants";
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
const PORT_PROBE_TIMEOUT_MS = 1500;
const LOG_TAIL_MAX = 4096;

let child: ChildProcess | null = null;
let childExited = false;
let logTail = "";
let detachLogging: (() => void) | null = null;
let startingPromise: Promise<void> | null = null;
/** 本地引擎转写的串行链（见 runExclusiveLocalTranscription） */
let transcriptionChain: Promise<unknown> = Promise.resolve();
/**
 * 服务最后一次报告「还在处理」的时刻。
 *
 * 转写是一个不返回中间结果的长请求，界面上只能干等；服务脚本每处理完一段
 * 就往 stdout 打一条心跳，这里记下时间。有心跳说明它在动，长时间没有才是卡住——
 * funasr 给不出可用的分母（VAD 路径下 total 恒为 1），所以不做百分比。
 */
let lastActivityAt = 0;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * 单条流的日志汇入口。
 *
 * 每条流一个解码器：UTF-8 的多字节字符会被分片从中间切开，逐块 toString
 * 出来的是替换符——实测报错正文里的方块字符全成了 `?`。
 */
function makeLogSink(): (chunk: Buffer) => void {
  const decoder = new StringDecoder("utf8");
  return (chunk) => {
    const text = decoder.write(chunk);
    if (text.includes("[guizhi-asr] tick")) {
      lastActivityAt = Date.now();
    }
    logTail = (logTail + text).slice(-LOG_TAIL_MAX);
  };
}

/**
 * 把日志汇集接到进程上，同时摘掉上一个进程的。
 *
 * 不摘的话，旧进程仍在往 logTail 里写：实测一条转写正跑着的时候另起一个实例，
 * 新进程的失败原因被旧进程的推理进度条整个冲掉，用户拿到的报错是一串进度条。
 */
function attachLogging(target: ChildProcess): void {
  detachLogging?.();
  const onStdout = makeLogSink();
  const onStderr = makeLogSink();
  target.stdout?.on("data", onStdout);
  target.stderr?.on("data", onStderr);
  detachLogging = () => {
    target.stdout?.off("data", onStdout);
    target.stderr?.off("data", onStderr);
    detachLogging = null;
  };
}

const ANSI_ESCAPE = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;

/** tqdm 的进度条：`100%|███| 1/1 [00:01<00:00, 1.28s/it]` */
function isProgressBar(line: string): boolean {
  return /\d{1,3}%\|/.test(line) || /\d+(?:\.\d+)?(?:it\/s|s\/it)\]/.test(line);
}

/**
 * 把子进程日志的尾巴整理成一句能看的报错。
 *
 * funasr 的推理进度由 tqdm 用 `\r` 原地刷新，一秒能写几十次，裸取最后几百字
 * 只会切到一堆进度条——实测用户看到的报错正文就是「rtf_avg: 0.043: 100%|███|
 * 1/1」，真正的原因早被挤出窗口了。
 */
export function summarizeProcessLog(raw: string, maxChars = 300): string {
  const lines: string[] = [];
  for (const rawLine of raw.split(/\r?\n/)) {
    // 终端上看得见的只有最后一次回车之后那一段
    const visible = rawLine.split("\r").pop() ?? "";
    const text = visible.replace(ANSI_ESCAPE, "").trim();
    if (text && !isProgressBar(text)) {
      lines.push(text);
    }
  }
  const tail = lines.slice(-4).join(" | ");
  return tail.length > maxChars ? `…${tail.slice(-maxChars)}` : tail;
}

/** 托管服务最后一次心跳的时刻（0 表示本次进程还没报过） */
export function getTranscriptionActivityAt(): number {
  return lastActivityAt;
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

/**
 * 端口上是否已有实例在监听。
 *
 * 与健康探测不是一回事：服务脚本的 `generate()` 是阻塞调用，转写期间
 * `/v1/models` 一概不应答，健康探测必然超时——但进程活得好好的，端口也占着。
 * TCP 握手由内核完成、不需要应用层 accept，所以这一问在忙的时候照样答得出来。
 */
export function isFunasrPortListening(
  timeoutMs = PORT_PROBE_TIMEOUT_MS,
): Promise<boolean> {
  return new Promise((resolve) => {
    const socket = net.connect({ host: "127.0.0.1", port: FUNASR_PORT });
    const finish = (listening: boolean) => {
      socket.destroy();
      resolve(listening);
    };
    socket.setTimeout(timeoutMs, () => finish(false));
    socket.once("connect", () => finish(true));
    socket.once("error", () => finish(false));
  });
}

export { isManagedFunasrUrl } from "@guizhi/shared/constants";

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
      env: {
        ...process.env,
        MODELSCOPE_CACHE: paths.modelsDir,
        // 不指定的话 Windows 上 Python 按 ANSI 代码页写管道（简中是 cp936），
        // 这边按 UTF-8 解，报错正文里的中文与符号会整段变成乱码
        PYTHONIOENCODING: "utf-8",
      },
    },
  );
  attachLogging(child);
  child.on("exit", () => {
    childExited = true;
  });
  child.on("error", () => {
    childExited = true;
  });

  const deadline = Date.now() + bootTimeoutMs;
  while (Date.now() < deadline) {
    if (childExited) {
      const reason =
        summarizeProcessLog(logTail) || "进程异常退出（无可用日志）";
      detachLogging?.();
      child = null;
      throw new Error(`本地转写服务启动失败：${reason}`);
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
 * 健康探测失败之后的分流：端口被占着就认下这个实例，否则才拉新的。
 *
 * 「探测不通」不等于「服务没了」。转写期间 `generate()` 占着事件循环，
 * `/v1/models` 三秒必超时，而实例活得好好的——此时再拉一个只会撞端口占用退出，
 * 用户拿到一句「本地转写服务启动失败」，而他要的那条转写其实只是需要排队。
 * 实测就是这么发生的：一批抖音视频并发导入，前一条正在推理，后一条当场判死。
 */
async function startOrAdopt(bootTimeoutMs: number): Promise<void> {
  if (await isFunasrPortListening()) {
    console.log("[funasr] 端口已被实例占用（正忙），本次排队复用");
    return;
  }
  if (!isFunasrInstalled()) {
    throw new Error("本地转写引擎未安装");
  }
  await startAndWait(bootTimeoutMs);
}

/**
 * 确保托管服务可用：已就绪或正忙则直接采用；否则拉起并等待健康。
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
  // 这一行与上面的判断之间不能有 await：并发调用会各自穿过检查、各拉一个进程
  startingPromise = startOrAdopt(bootTimeoutMs).finally(() => {
    startingPromise = null;
  });
  return startingPromise;
}

/**
 * 串行化本地引擎的转写请求。
 *
 * 服务端本来就一次只做一条（`generate()` 占着事件循环），但客户端不排队的话，
 * 后到的请求会带着自己那 10 分钟的超时预算干等前一条做完，等出来的失败看着像
 * 「转写超时」，实际只是排队排掉的。锁在这里，超时才从真正开始干活时起算。
 */
export function runExclusiveLocalTranscription<T>(
  task: () => Promise<T>,
): Promise<T> {
  const next = transcriptionChain.then(task, task);
  // 前一条的成败不该影响后一条排队，链条本身只用来定序
  transcriptionChain = next.then(
    () => undefined,
    () => undefined,
  );
  return next;
}

/** 停止由本模块拉起的服务进程（Windows 下杀进程树，避免遗留 python 子进程） */
export function stopFunasrService(): void {
  const pid = child?.pid;
  detachLogging?.();
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
