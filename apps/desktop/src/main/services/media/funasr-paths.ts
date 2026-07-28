/**
 * 本地转写引擎（托管 funasr-server）的路径与常量。
 * manager（安装/卸载）与 service（进程托管）共用，避免循环依赖。
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

/** funasr-server 暴露的模型名 */
export const FUNASR_MODEL_NAME = "sensevoice";

export interface FunasrPaths {
  root: string;
  /** 托管 Python 运行时 */
  pythonExe: string;
  venvDir: string;
  venvPython: string;
  /** 自带的服务脚本（由 funasr-server-script.ts 落盘） */
  serverScript: string;
  /** funasr 包安装位置，用作「依赖装好了」的判据 */
  funasrPackage: string;
  /** MODELSCOPE_CACHE：模型缓存目录 */
  modelsDir: string;
  stateFile: string;
}

export function getFunasrRoot(): string {
  return path.join(getToolsDir(), "funasr");
}

export function getFunasrPaths(root = getFunasrRoot()): FunasrPaths {
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
    stateFile: path.join(root, "state.json"),
  };
}

/**
 * 判据是「venv 里的 python 与 funasr 包都在」。
 * 服务脚本不计入：它由应用在每次启动前落盘，缺失可自愈，
 * 拿它当安装标记会让首次启动陷入先有鸡还是先有蛋。
 */
export function isFunasrInstalled(paths = getFunasrPaths()): boolean {
  return fs.existsSync(paths.funasrPackage) && fs.existsSync(paths.venvPython);
}

export interface FunasrInstallState {
  funasrVersion?: string;
  pythonVersion?: string;
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
