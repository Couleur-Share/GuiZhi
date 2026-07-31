/**
 * 本地转写引擎托管安装。
 *
 * - Windows：Python 运行时 → venv 装 funasr/torch → 首次启动下模型
 * - macOS arm64：FunASR llama.cpp 预编译包 + SenseVoice/VAD GGUF
 *
 * 装完写入内置模型并把「语音转写」路由指过来。
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
  GGUF_CLI_NAME,
  GGUF_SENSEVOICE_FILE,
  GGUF_VAD_FILE,
  getFunasrPaths,
  isFunasrInstalled,
  readFunasrState,
  resolveFunasrEngineFlavor,
  resolveGgufSensevoiceCli,
  writeFunasrState,
  type FunasrEngineFlavor,
} from "./funasr-paths";
import {
  ensureFunasrService,
  probeFunasrHealth,
  stopFunasrService,
} from "./funasr-service";

// ── Windows Python 运行时 ──────────────────────────────────────────────────
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

// ── macOS arm64 GGUF（与 FunASR v1.3.29 / runtime-llamacpp-v0.1.9 同源）──
const GGUF_RUNTIME_VERSION = "0.1.9";
const GGUF_RUNTIME_RELEASE = "v1.3.29";
const GGUF_RUNTIME_ASSET = "funasr-llamacpp-macos-arm64.tar.gz";
/** FunASR release notes 公布的 SHA-256 */
const GGUF_RUNTIME_SHA256 =
  "2d5786784ad09d8f4def1d942f678728638fe601d00acf0dad7cf094a9328363";
const GGUF_RUNTIME_OFFICIAL = `https://github.com/modelscope/FunASR/releases/download/${GGUF_RUNTIME_RELEASE}/${GGUF_RUNTIME_ASSET}`;
const GGUF_SENSEVOICE_OFFICIAL = `https://huggingface.co/FunAudioLLM/SenseVoiceSmall-GGUF/resolve/main/${GGUF_SENSEVOICE_FILE}`;
const GGUF_VAD_OFFICIAL = `https://huggingface.co/FunAudioLLM/fsmn-vad-GGUF/resolve/main/${GGUF_VAD_FILE}`;

const TOOL_TIMEOUT_MS = 60 * 1000;
const VENV_TIMEOUT_MS = 3 * 60 * 1000;
const EXTRACT_TIMEOUT_MS = 5 * 60 * 1000;
const DEPS_TIMEOUT_MS = 30 * 60 * 1000;
const MODEL_BOOT_TIMEOUT_MS = 20 * 60 * 1000;
/** GGUF 权重约 250MB，给足慢网时间 */
const GGUF_MODEL_DOWNLOAD_TIMEOUT_MS = 30 * 60 * 1000;
const DEPS_PROGRESS_THROTTLE_MS = 500;
const OUTPUT_TAIL_MAX = 4096;

function withGithubMirrors(official: string): string[] {
  return [
    official,
    `https://ghfast.top/${official}`,
    `https://gh-proxy.com/${official}`,
    `https://hub.gitmirror.com/${official}`,
  ];
}

function withHfMirrors(official: string): string[] {
  return [
    official,
    official.replace("https://huggingface.co/", "https://hf-mirror.com/"),
  ];
}

export function getPythonDownloadUrls(
  platform: NodeJS.Platform = process.platform,
): string[] {
  if (platform !== "win32") {
    return [];
  }
  return withGithubMirrors(PYTHON_DOWNLOAD_OFFICIAL);
}

export function getGgufRuntimeDownloadUrls(
  platform: NodeJS.Platform = process.platform,
  arch: NodeJS.Architecture = process.arch,
): string[] {
  if (platform !== "darwin" || arch !== "arm64") {
    return [];
  }
  return withGithubMirrors(GGUF_RUNTIME_OFFICIAL);
}

export function getGgufSensevoiceDownloadUrls(): string[] {
  return withHfMirrors(GGUF_SENSEVOICE_OFFICIAL);
}

export function getGgufVadDownloadUrls(): string[] {
  return withHfMirrors(GGUF_VAD_OFFICIAL);
}

export function getGgufRuntimeAssetName(): string {
  return GGUF_RUNTIME_ASSET;
}

export function getGgufRuntimeSha256(): string {
  return GGUF_RUNTIME_SHA256;
}

/**
 * 本地转写一键安装：Windows（Python）或 macOS Apple Silicon（GGUF）。
 */
export function isFunasrInstallSupported(
  platform: NodeJS.Platform = process.platform,
  arch: NodeJS.Architecture = process.arch,
): boolean {
  if (platform === "win32") {
    return true;
  }
  return platform === "darwin" && arch === "arm64";
}

/** 本平台安装会落成哪种引擎（未支持时 null） */
export function resolveInstallFlavor(
  platform: NodeJS.Platform = process.platform,
  arch: NodeJS.Architecture = process.arch,
): FunasrEngineFlavor | null {
  if (platform === "win32") {
    return "python";
  }
  if (platform === "darwin" && arch === "arm64") {
    return "gguf";
  }
  return null;
}

/** 官方发布的 SHA256SUMS；优先官方源，镜像只作兜底 */
export function getPythonChecksumUrls(): string[] {
  return withGithubMirrors(PYTHON_CHECKSUM_OFFICIAL);
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

function describeUrlHost(url: string): string {
  try {
    return new URL(url).hostname;
  } catch {
    return url;
  }
}

async function downloadFirstOk(
  urls: string[],
  targetPath: string,
  options: {
    expectedSha256?: string | null;
    onProgress?: (progress: FunasrInstallProgress) => void;
    phase: FunasrInstallProgress["phase"];
    timeoutMs?: number;
  },
): Promise<void> {
  const failures: string[] = [];
  for (const url of urls) {
    try {
      console.log(`[funasr] 开始下载: ${url}`);
      await downloadToFile(
        url,
        targetPath,
        (progress) => {
          options.onProgress?.({
            phase: options.phase,
            percent: progress.total
              ? Math.min(
                  100,
                  Math.round((progress.transferred / progress.total) * 100),
                )
              : null,
            detail: `${(progress.transferred / (1024 * 1024)).toFixed(1)} MB`,
          });
        },
        { timeoutMs: options.timeoutMs },
      );
      if (options.expectedSha256) {
        const actual = await sha256File(targetPath);
        if (actual !== options.expectedSha256) {
          throw new Error(
            `校验和不匹配（期望 ${options.expectedSha256.slice(0, 12)}…，实际 ${actual.slice(0, 12)}…）`,
          );
        }
      }
      return;
    } catch (error) {
      const host = describeUrlHost(url);
      failures.push(
        `${host}: ${error instanceof Error ? error.message : String(error)}`,
      );
      console.warn(`[funasr] 下载源失败（${host}），尝试下一个`);
      fs.rmSync(targetPath, { force: true });
    }
  }
  throw new Error(`全部下载源失败——${failures.join("；")}`);
}

/** 清掉 macOS quarantine，避免 Gatekeeper 把下载的 CLI 标成损坏 */
function clearMacQuarantine(targetDir: string): void {
  if (process.platform !== "darwin") {
    return;
  }
  try {
    spawn("xattr", ["-dr", "com.apple.quarantine", targetDir], {
      windowsHide: true,
      stdio: "ignore",
    });
  } catch {
    // 没有 xattr 或不支持时忽略
  }
}

export async function getFunasrStatus(options?: {
  platform?: NodeJS.Platform;
  arch?: NodeJS.Architecture;
}): Promise<FunasrStatus> {
  const paths = getFunasrPaths();
  const installed = isFunasrInstalled(paths);
  const running = installed ? await probeFunasrHealth() : false;
  const platform = options?.platform ?? process.platform;
  const arch = options?.arch ?? process.arch;
  const flavor =
    resolveFunasrEngineFlavor(paths) ??
    resolveInstallFlavor(platform, arch) ??
    undefined;
  return {
    installed,
    running,
    port: FUNASR_PORT,
    dir: paths.root,
    version: readFunasrState(paths)?.funasrVersion,
    installSupported: isFunasrInstallSupported(platform, arch),
    installFlavor: flavor,
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

async function installWindowsPython(
  onProgress?: (progress: FunasrInstallProgress) => void,
): Promise<{ version?: string }> {
  const paths = getFunasrPaths();
  stopFunasrService();
  fs.mkdirSync(paths.root, { recursive: true });
  // 清掉可能存在的半成品（models 目录保留复用）
  await removeDirWithRetry(path.join(paths.root, "python"));
  await removeDirWithRetry(paths.venvDir);

  // ── 阶段 1：Python 运行时 ────────────────────────────────────────────
  const tarPath = path.join(paths.root, "python.download.tar.gz");
  const expectedSha256 = await fetchExpectedSha256(
    getPythonChecksumUrls(),
    getPythonAssetName(),
  );
  if (!expectedSha256) {
    console.warn("[funasr] 未能获取官方校验和，本次安装跳过哈希校验");
  }
  try {
    await downloadFirstOk(getPythonDownloadUrls(), tarPath, {
      expectedSha256,
      onProgress,
      phase: "runtime",
    });
    await runTool("tar", ["-xzf", tarPath, "-C", paths.root], {
      timeoutMs: EXTRACT_TIMEOUT_MS,
    });
    if (!fs.existsSync(paths.pythonExe)) {
      throw new Error("解压后未找到 python.exe");
    }
  } finally {
    fs.rmSync(tarPath, { force: true });
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
  if (!fs.existsSync(paths.funasrPackage)) {
    throw new Error("依赖安装完成但未找到 funasr 包，请重试");
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

  writeFunasrState(
    {
      funasrVersion,
      pythonVersion,
      flavor: "python",
      installedAt: new Date().toISOString(),
    },
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
}

async function installDarwinGguf(
  onProgress?: (progress: FunasrInstallProgress) => void,
): Promise<{ version?: string }> {
  const paths = getFunasrPaths();
  stopFunasrService();
  fs.mkdirSync(paths.root, { recursive: true });
  await removeDirWithRetry(paths.ggufRuntimeDir);
  await removeDirWithRetry(paths.ggufModelsDir);
  fs.mkdirSync(paths.ggufRuntimeDir, { recursive: true });
  fs.mkdirSync(paths.ggufModelsDir, { recursive: true });

  // ── 阶段 1：llama.cpp 预编译包 ───────────────────────────────────────
  const tarPath = path.join(paths.root, "gguf-runtime.tar.gz");
  try {
    await downloadFirstOk(getGgufRuntimeDownloadUrls(), tarPath, {
      expectedSha256: GGUF_RUNTIME_SHA256,
      onProgress,
      phase: "runtime",
    });
    await runTool("tar", ["-xzf", tarPath, "-C", paths.ggufRuntimeDir], {
      timeoutMs: EXTRACT_TIMEOUT_MS,
    });
  } finally {
    fs.rmSync(tarPath, { force: true });
  }

  const cliPath = resolveGgufSensevoiceCli(paths);
  if (!cliPath) {
    throw new Error(`解压后未找到 ${GGUF_CLI_NAME}`);
  }
  fs.chmodSync(cliPath, 0o755);
  clearMacQuarantine(paths.ggufRuntimeDir);

  // ── 阶段 2：SenseVoice + VAD GGUF（复用 models 进度相位）─────────────
  onProgress?.({ phase: "models", percent: null });
  const sensevoiceTmp = path.join(paths.ggufModelsDir, `${GGUF_SENSEVOICE_FILE}.partial`);
  const vadTmp = path.join(paths.ggufModelsDir, `${GGUF_VAD_FILE}.partial`);
  try {
    await downloadFirstOk(getGgufSensevoiceDownloadUrls(), sensevoiceTmp, {
      onProgress,
      phase: "models",
      timeoutMs: GGUF_MODEL_DOWNLOAD_TIMEOUT_MS,
    });
    fs.renameSync(sensevoiceTmp, paths.sensevoiceGguf);

    await downloadFirstOk(getGgufVadDownloadUrls(), vadTmp, {
      onProgress,
      phase: "models",
      timeoutMs: GGUF_MODEL_DOWNLOAD_TIMEOUT_MS,
    });
    fs.renameSync(vadTmp, paths.vadGguf);
  } finally {
    fs.rmSync(sensevoiceTmp, { force: true });
    fs.rmSync(vadTmp, { force: true });
  }

  clearMacQuarantine(paths.root);
  const version = `gguf-${GGUF_RUNTIME_VERSION}`;
  writeFunasrState(
    {
      funasrVersion: version,
      flavor: "gguf",
      installedAt: new Date().toISOString(),
    },
    paths,
  );
  coreAIConfigService.write(
    upsertBuiltinTranscription(
      coreAIConfigService.read(),
      new Date().toISOString(),
    ),
  );
  // 冒烟：拉起 shim 做健康探测（模型已在本地，无需再下）
  await ensureFunasrService();
  console.log(`[funasr] GGUF 安装完成（${version}），已接入语音转写路由`);
  return { version };
}

/**
 * 一键安装。Windows ≈ 3GB；macOS arm64 ≈ 260MB（runtime + q8 模型）。
 */
export async function installFunasr(
  onProgress?: (progress: FunasrInstallProgress) => void,
): Promise<{ version?: string }> {
  if (operationInFlight) {
    throw new Error("已有安装 / 卸载任务进行中");
  }
  const flavor = resolveInstallFlavor();
  if (!flavor) {
    throw new Error(
      process.platform === "darwin"
        ? "本地转写引擎仅支持 Apple Silicon（arm64）Mac；Intel Mac 请配置云端语音转写"
        : "本地转写引擎目前仅支持 Windows 与 Apple Silicon Mac",
    );
  }
  operationInFlight = true;
  try {
    return flavor === "gguf"
      ? await installDarwinGguf(onProgress)
      : await installWindowsPython(onProgress);
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
    // taskkill / 关 shim 异步生效，等文件句柄释放
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
