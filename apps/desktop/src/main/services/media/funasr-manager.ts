/**
 * 本地转写引擎托管安装：下载独立 Python 运行时 → venv 安装 funasr 依赖
 * （清华镜像）→ 首次启动服务下载模型 → 自动写入内置模型与「语音转写」路由。
 *
 * 全程无需用户理解 API 概念；高级用户仍可在「模型服务」里编辑
 * 自动创建的服务商条目。仅支持 Windows（与应用打包目标一致）。
 */
import { spawn } from "child_process";
import fs from "fs";
import path from "path";
import {
  coreAIConfigService,
  type CoreAIConfigFile,
  type CoreAIModelConfig,
  type CoreAIProviderConfig,
} from "@guizhi/core";
import type { FunasrInstallProgress, FunasrStatus } from "@guizhi/shared/types";
import {
  downloadToFile,
  fetchExpectedSha256,
  sha256File,
} from "./tool-download";
import {
  FUNASR_BASE_URL,
  FUNASR_MODEL_ID,
  FUNASR_MODEL_NAME,
  FUNASR_PORT,
  FUNASR_PROVIDER_ID,
  getFunasrPaths,
  isFunasrInstalled,
  readFunasrState,
  writeFunasrState,
} from "./funasr-paths";
import {
  ensureFunasrService,
  probeFunasrHealth,
  stopFunasrService,
} from "./funasr-service";

// python-build-standalone 固定版本（install_only 含 pip，解压即用）
const PYTHON_RELEASE_TAG = "20260610";
/** SHA256SUMS 里的行名用的是 `+` 而不是 URL 里的 %2B */
const PYTHON_ASSET_NAME =
  "cpython-3.12.13+20260610-x86_64-pc-windows-msvc-install_only.tar.gz";
const PYTHON_DOWNLOAD_OFFICIAL = `https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON_RELEASE_TAG}/${PYTHON_ASSET_NAME.replace("+", "%2B")}`;
const PYTHON_CHECKSUM_OFFICIAL = `https://github.com/astral-sh/python-build-standalone/releases/download/${PYTHON_RELEASE_TAG}/SHA256SUMS`;
const PIP_INDEX_URL = "https://pypi.tuna.tsinghua.edu.cn/simple";
const PIP_PACKAGES = [
  "funasr",
  "fastapi",
  "uvicorn",
  "python-multipart",
  "torch",
  "torchaudio",
];

const TOOL_TIMEOUT_MS = 60 * 1000;
const VENV_TIMEOUT_MS = 3 * 60 * 1000;
const EXTRACT_TIMEOUT_MS = 5 * 60 * 1000;
const DEPS_TIMEOUT_MS = 30 * 60 * 1000;
const MODEL_BOOT_TIMEOUT_MS = 20 * 60 * 1000;
const DEPS_PROGRESS_THROTTLE_MS = 500;
const OUTPUT_TAIL_MAX = 4096;

export function getPythonDownloadUrls(
  platform: NodeJS.Platform = process.platform,
): string[] {
  if (platform !== "win32") {
    return [];
  }
  return [
    PYTHON_DOWNLOAD_OFFICIAL,
    `https://ghfast.top/${PYTHON_DOWNLOAD_OFFICIAL}`,
    `https://gh-proxy.com/${PYTHON_DOWNLOAD_OFFICIAL}`,
    `https://hub.gitmirror.com/${PYTHON_DOWNLOAD_OFFICIAL}`,
  ];
}

/** 官方发布的 SHA256SUMS；优先官方源，镜像只作兜底 */
export function getPythonChecksumUrls(): string[] {
  return [
    PYTHON_CHECKSUM_OFFICIAL,
    `https://ghfast.top/${PYTHON_CHECKSUM_OFFICIAL}`,
    `https://gh-proxy.com/${PYTHON_CHECKSUM_OFFICIAL}`,
    `https://hub.gitmirror.com/${PYTHON_CHECKSUM_OFFICIAL}`,
  ];
}

export function getPythonAssetName(): string {
  return PYTHON_ASSET_NAME;
}

/** 通用外部命令执行：收集输出、超时终止、失败附错误尾部 */
function runTool(
  executable: string,
  args: string[],
  options?: { timeoutMs?: number; onOutput?: (line: string) => void },
): Promise<{ stdout: string }> {
  return new Promise((resolve, reject) => {
    const child = spawn(executable, args, { windowsHide: true });
    let stdout = "";
    let tail = "";
    let settled = false;

    const finish = (action: () => void) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        action();
      }
    };
    const timer = setTimeout(
      () => {
        child.kill();
        finish(() =>
          reject(new Error(`${path.basename(executable)} 执行超时`)),
        );
      },
      options?.timeoutMs ?? TOOL_TIMEOUT_MS,
    );

    const handleChunk = (chunk: Buffer) => {
      const text = chunk.toString("utf8");
      tail = (tail + text).slice(-OUTPUT_TAIL_MAX);
      const lastLine = text
        .split(/\r?\n/)
        .map((line) => line.trim())
        .filter(Boolean)
        .pop();
      if (lastLine) {
        options?.onOutput?.(lastLine);
      }
    };
    child.stdout?.on("data", (chunk: Buffer) => {
      if (stdout.length < OUTPUT_TAIL_MAX * 4) {
        stdout += chunk.toString("utf8");
      }
      handleChunk(chunk);
    });
    child.stderr?.on("data", handleChunk);
    child.on("error", (error) => finish(() => reject(error)));
    child.on("close", (code) => {
      finish(() => {
        if (code === 0) {
          resolve({ stdout });
        } else {
          reject(
            new Error(
              `${path.basename(executable)} 退出码 ${code}: ${tail.trim().slice(-300)}`,
            ),
          );
        }
      });
    });
  });
}

/** 带重试的目录删除（Windows 上刚结束的进程可能短暂占用文件） */
async function removeDirWithRetry(dir: string, attempts = 5): Promise<void> {
  for (let i = 0; i < attempts; i += 1) {
    try {
      fs.rmSync(dir, { recursive: true, force: true });
      return;
    } catch (error) {
      if (i === attempts - 1) {
        throw error;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
}

export async function getFunasrStatus(): Promise<FunasrStatus> {
  const paths = getFunasrPaths();
  const installed = isFunasrInstalled(paths);
  const running = installed ? await probeFunasrHealth() : false;
  return {
    installed,
    running,
    port: FUNASR_PORT,
    dir: paths.root,
    version: readFunasrState(paths)?.funasrVersion,
  };
}

/** 写入 / 更新内置服务商与模型条目，并把「语音转写」路由指向本地引擎（纯函数） */
export function upsertBuiltinTranscription(
  config: CoreAIConfigFile,
  verifiedAt: string,
): CoreAIConfigFile {
  const provider: CoreAIProviderConfig = {
    id: FUNASR_PROVIDER_ID,
    name: "本地转写引擎",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "local",
    apiUrl: FUNASR_BASE_URL,
    lastVerifiedAt: verifiedAt,
  };
  const existingModel = config.models.find(
    (model) => model.id === FUNASR_MODEL_ID,
  );
  const model: CoreAIModelConfig = {
    id: FUNASR_MODEL_ID,
    name: "SenseVoiceSmall（本地）",
    providerId: FUNASR_PROVIDER_ID,
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "local",
    apiUrl: FUNASR_BASE_URL,
    model: FUNASR_MODEL_NAME,
    isDefault: existingModel?.isDefault === true,
    lastVerifiedAt: verifiedAt,
    capabilities: { chat: false, audioTranscription: true },
  };

  const providers = config.providers.some((p) => p.id === FUNASR_PROVIDER_ID)
    ? config.providers.map((p) => (p.id === FUNASR_PROVIDER_ID ? provider : p))
    : [...config.providers, provider];
  const models = existingModel
    ? config.models.map((m) => (m.id === FUNASR_MODEL_ID ? model : m))
    : [...config.models, model];

  return {
    ...config,
    providers,
    models,
    modelRouteDefaults: {
      ...config.modelRouteDefaults,
      audioText: FUNASR_MODEL_ID,
    },
  };
}

/** 移除内置条目；「语音转写」路由仅在仍指向本地引擎时清除（纯函数） */
export function removeBuiltinTranscription(
  config: CoreAIConfigFile,
): CoreAIConfigFile {
  const modelRouteDefaults = { ...config.modelRouteDefaults };
  if (modelRouteDefaults.audioText === FUNASR_MODEL_ID) {
    delete modelRouteDefaults.audioText;
  }
  return {
    ...config,
    providers: config.providers.filter((p) => p.id !== FUNASR_PROVIDER_ID),
    models: config.models.filter((m) => m.id !== FUNASR_MODEL_ID),
    modelRouteDefaults,
  };
}

let operationInFlight = false;

/**
 * 一键安装：运行时（约 45MB）→ 依赖（约 700MB 下载 / 3GB 落盘）→
 * 模型（约 1GB，随服务首次启动下载）→ 自动接线路由。
 * 已有的 models 目录会保留复用（重装 / 迁移场景免重复下载）。
 */
export async function installFunasr(
  onProgress?: (progress: FunasrInstallProgress) => void,
): Promise<{ version?: string }> {
  if (operationInFlight) {
    throw new Error("已有安装 / 卸载任务进行中");
  }
  if (process.platform !== "win32") {
    throw new Error("本地转写引擎目前仅支持 Windows");
  }
  operationInFlight = true;
  try {
    const paths = getFunasrPaths();
    stopFunasrService();
    fs.mkdirSync(paths.root, { recursive: true });
    // 清掉可能存在的半成品（models 目录保留复用）
    await removeDirWithRetry(path.join(paths.root, "python"));
    await removeDirWithRetry(paths.venvDir);

    // ── 阶段 1：Python 运行时 ────────────────────────────────────────────
    const tarPath = path.join(paths.root, "python.download.tar.gz");
    // 运行时来自第三方 GitHub 代理，而解压出来的是要执行的 python.exe；
    // 校验和从官方源单独取，拿不到才降级（正需要镜像的网络里官方也常不通）
    const expectedSha256 = await fetchExpectedSha256(
      getPythonChecksumUrls(),
      getPythonAssetName(),
    );
    if (!expectedSha256) {
      console.warn("[funasr] 未能获取官方校验和，本次安装跳过哈希校验");
    }
    const failures: string[] = [];
    let runtimeReady = false;
    for (const url of getPythonDownloadUrls()) {
      try {
        console.log(`[funasr] 下载 Python 运行时: ${url}`);
        await downloadToFile(url, tarPath, (progress) => {
          onProgress?.({
            phase: "runtime",
            percent: progress.total
              ? Math.min(
                  100,
                  Math.round((progress.transferred / progress.total) * 100),
                )
              : null,
            detail: `${(progress.transferred / (1024 * 1024)).toFixed(1)} MB`,
          });
        });
        if (expectedSha256) {
          const actual = await sha256File(tarPath);
          if (actual !== expectedSha256) {
            throw new Error(
              `校验和不匹配（期望 ${expectedSha256.slice(0, 12)}…，实际 ${actual.slice(0, 12)}…）`,
            );
          }
        }
        await runTool("tar", ["-xzf", tarPath, "-C", paths.root], {
          timeoutMs: EXTRACT_TIMEOUT_MS,
        });
        if (!fs.existsSync(paths.pythonExe)) {
          throw new Error("解压后未找到 python.exe");
        }
        runtimeReady = true;
        break;
      } catch (error) {
        const host = (() => {
          try {
            return new URL(url).hostname;
          } catch {
            return url;
          }
        })();
        failures.push(
          `${host}: ${error instanceof Error ? error.message : String(error)}`,
        );
        console.warn(`[funasr] 运行时下载源失败（${host}），尝试下一个`);
      } finally {
        fs.rmSync(tarPath, { force: true });
      }
    }
    if (!runtimeReady) {
      throw new Error(`Python 运行时下载失败——${failures.join("；")}`);
    }
    const pythonVersion = (
      await runTool(paths.pythonExe, ["--version"])
    ).stdout.trim();

    // ── 阶段 2：依赖 ────────────────────────────────────────────────────
    onProgress?.({ phase: "deps", percent: null });
    await runTool(paths.pythonExe, ["-m", "venv", paths.venvDir], {
      timeoutMs: VENV_TIMEOUT_MS,
    });
    let lastEmit = 0;
    await runTool(
      paths.venvPython,
      [
        "-m",
        "pip",
        "install",
        "--no-warn-script-location",
        ...PIP_PACKAGES,
        "-i",
        PIP_INDEX_URL,
      ],
      {
        timeoutMs: DEPS_TIMEOUT_MS,
        onOutput: (line) => {
          const now = Date.now();
          if (now - lastEmit >= DEPS_PROGRESS_THROTTLE_MS) {
            lastEmit = now;
            onProgress?.({
              phase: "deps",
              percent: null,
              detail: line.slice(0, 80),
            });
          }
        },
      },
    );
    if (!fs.existsSync(paths.serverExe)) {
      throw new Error("依赖安装完成但未找到 funasr-server，请重试");
    }
    const pipShow = await runTool(paths.venvPython, [
      "-m",
      "pip",
      "show",
      "funasr",
    ]);
    const funasrVersion = pipShow.stdout.match(/^Version:\s*(.+)$/m)?.[1]?.trim();

    // ── 阶段 3：模型（随服务首次启动下载）────────────────────────────────
    onProgress?.({ phase: "models", percent: null });
    await ensureFunasrService(MODEL_BOOT_TIMEOUT_MS);

    // ── 收尾：状态与路由接线 ────────────────────────────────────────────
    writeFunasrState(
      { funasrVersion, pythonVersion, installedAt: new Date().toISOString() },
      paths,
    );
    coreAIConfigService.write(
      upsertBuiltinTranscription(
        coreAIConfigService.read(),
        new Date().toISOString(),
      ),
    );
    console.log(
      `[funasr] 安装完成（funasr ${funasrVersion ?? "?"}，${pythonVersion}），已接入语音转写路由`,
    );
    return { version: funasrVersion };
  } finally {
    operationInFlight = false;
  }
}

/** 卸载：停服务 → 删除托管目录（含模型）→ 移除内置模型与路由 */
export async function uninstallFunasr(): Promise<void> {
  if (operationInFlight) {
    throw new Error("已有安装 / 卸载任务进行中");
  }
  operationInFlight = true;
  try {
    const paths = getFunasrPaths();
    stopFunasrService();
    // taskkill 异步生效，等进程释放文件句柄
    await new Promise((resolve) => setTimeout(resolve, 1500));
    await removeDirWithRetry(paths.root);
    coreAIConfigService.write(
      removeBuiltinTranscription(coreAIConfigService.read()),
    );
    console.log("[funasr] 已卸载本地转写引擎");
  } finally {
    operationInFlight = false;
  }
}
