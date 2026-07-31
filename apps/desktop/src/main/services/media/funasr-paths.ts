/**
 * 本地转写引擎路径与安装判定。
 *
 * 两套落盘形态共用同一 root（tools/funasr/）：
 * - Windows：Python 运行时 + venv（SenseVoice + cam++，含说话人分离）
 * - macOS arm64：FunASR llama.cpp / GGUF 预编译二进制（无 Python，无分离）
 */
import fs from "fs";
import path from "path";
import { getToolsDir } from "../../runtime-paths";

// 端口与内置条目 id 渲染进程也要用（设置页据此挡住删除内置引擎），落在 shared
export {
  FUNASR_BASE_URL,
  FUNASR_MODEL_ID,
  FUNASR_PORT,
  FUNASR_PROVIDER_ID,
} from "@guizhi/shared/constants";

/** funasr-server / GGUF shim 暴露的模型名 */
export const FUNASR_MODEL_NAME = "sensevoice";

/** SenseVoice q8 与 VAD 的固定文件名（与 Hugging Face 仓库一致） */
export const GGUF_SENSEVOICE_FILE = "sensevoice-small-q8.gguf";
export const GGUF_VAD_FILE = "fsmn-vad.gguf";
export const GGUF_CLI_NAME = "llama-funasr-sensevoice";

export type FunasrEngineFlavor = "python" | "gguf";

export interface FunasrPaths {
  root: string;
  /** 托管 Python 运行时（Windows） */
  pythonExe: string;
  venvDir: string;
  venvPython: string;
  /** 自带的服务脚本（由 funasr-server-script.ts 落盘） */
  serverScript: string;
  /** funasr 包安装位置，用作「Python 依赖装好了」的判据 */
  funasrPackage: string;
  /** MODELSCOPE_CACHE：Python 引擎的模型缓存目录 */
  modelsDir: string;
  /** GGUF 预编译二进制目录（macOS arm64） */
  ggufRuntimeDir: string;
  /** SenseVoice / VAD GGUF 权重目录 */
  ggufModelsDir: string;
  sensevoiceGguf: string;
  vadGguf: string;
  sensevoiceCli: string;
  stateFile: string;
}

export function getFunasrRoot(): string {
  return path.join(getToolsDir(), "funasr");
}

export function getFunasrPaths(root = getFunasrRoot()): FunasrPaths {
  const ggufRuntimeDir = path.join(root, "gguf-runtime");
  const ggufModelsDir = path.join(root, "gguf");
  return {
    root,
    pythonExe: path.join(root, "python", "python.exe"),
    venvDir: path.join(root, "env"),
    venvPython: path.join(root, "env", "Scripts", "python.exe"),
    serverScript: path.join(root, "server.py"),
    funasrPackage: path.join(
      root,
      "env",
      "Lib",
      "site-packages",
      "funasr",
      "__init__.py",
    ),
    modelsDir: path.join(root, "models"),
    ggufRuntimeDir,
    ggufModelsDir,
    sensevoiceGguf: path.join(ggufModelsDir, GGUF_SENSEVOICE_FILE),
    vadGguf: path.join(ggufModelsDir, GGUF_VAD_FILE),
    sensevoiceCli: path.join(ggufRuntimeDir, GGUF_CLI_NAME),
    stateFile: path.join(root, "state.json"),
  };
}

/** Windows Python 引擎是否装好（含说话人分离） */
export function isFunasrPythonInstalled(paths = getFunasrPaths()): boolean {
  return fs.existsSync(paths.funasrPackage) && fs.existsSync(paths.venvPython);
}

/**
 * 在 runtime 目录里找 CLI：官方 tar 可能平铺，也可能带一层目录。
 * 深度限制避免扫到无关树。
 */
export function findGgufSensevoiceCli(
  runtimeDir: string,
  maxDepth = 3,
): string | null {
  const walk = (dir: string, depth: number): string | null => {
    if (depth < 0 || !fs.existsSync(dir)) {
      return null;
    }
    let entries: fs.Dirent[];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return null;
    }
    for (const entry of entries) {
      if (entry.isFile() && entry.name === GGUF_CLI_NAME) {
        return path.join(dir, entry.name);
      }
    }
    for (const entry of entries) {
      if (entry.isDirectory()) {
        const found = walk(path.join(dir, entry.name), depth - 1);
        if (found) {
          return found;
        }
      }
    }
    return null;
  };
  return walk(runtimeDir, maxDepth);
}

/** macOS arm64 GGUF 引擎是否装好 */
export function isFunasrGgufInstalled(paths = getFunasrPaths()): boolean {
  const cli =
    fs.existsSync(paths.sensevoiceCli) && fs.statSync(paths.sensevoiceCli).isFile()
      ? paths.sensevoiceCli
      : findGgufSensevoiceCli(paths.ggufRuntimeDir);
  return (
    Boolean(cli) &&
    fs.existsSync(paths.sensevoiceGguf) &&
    fs.existsSync(paths.vadGguf)
  );
}

/**
 * 任一形态装好即视为已安装（供 ensure / status 共用）。
 * 服务脚本不计入：它由应用在每次启动前落盘，缺失可自愈。
 */
export function isFunasrInstalled(paths = getFunasrPaths()): boolean {
  return isFunasrPythonInstalled(paths) || isFunasrGgufInstalled(paths);
}

/** 当前实际可用的引擎形态；两套都在时 Python 优先（功能更全） */
export function resolveFunasrEngineFlavor(
  paths = getFunasrPaths(),
): FunasrEngineFlavor | null {
  if (isFunasrPythonInstalled(paths)) {
    return "python";
  }
  if (isFunasrGgufInstalled(paths)) {
    return "gguf";
  }
  return null;
}

/**
 * 解析 GGUF CLI 的绝对路径（安装后可能不在 gguf-runtime 根目录）。
 */
export function resolveGgufSensevoiceCli(paths = getFunasrPaths()): string | null {
  if (fs.existsSync(paths.sensevoiceCli)) {
    return paths.sensevoiceCli;
  }
  return findGgufSensevoiceCli(paths.ggufRuntimeDir);
}

export interface FunasrInstallState {
  funasrVersion?: string;
  pythonVersion?: string;
  /** 安装形态：缺省按路径探测（兼容旧 state.json） */
  flavor?: FunasrEngineFlavor;
  installedAt: string;
}

export function readFunasrState(
  paths = getFunasrPaths(),
): FunasrInstallState | null {
  try {
    const parsed = JSON.parse(
      fs.readFileSync(paths.stateFile, "utf8"),
    ) as FunasrInstallState;
    return typeof parsed?.installedAt === "string" ? parsed : null;
  } catch {
    return null;
  }
}

export function writeFunasrState(
  state: FunasrInstallState,
  paths = getFunasrPaths(),
): void {
  fs.writeFileSync(paths.stateFile, `${JSON.stringify(state, null, 2)}\n`);
}
