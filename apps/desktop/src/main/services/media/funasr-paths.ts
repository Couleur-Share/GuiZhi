/**
 * 本地转写引擎（托管 funasr-server）的路径与常量。
 * manager（安装/卸载）与 service（进程托管）共用，避免循环依赖。
 */
import fs from "fs";
import path from "path";
import { getToolsDir } from "../../runtime-paths";

/** 服务固定监听端口（仅绑定 127.0.0.1） */
export const FUNASR_PORT = 8620;
export const FUNASR_BASE_URL = `http://127.0.0.1:${FUNASR_PORT}/v1`;

/** ai-models.json 里内置条目的固定 id（安装时写入、卸载时移除） */
export const FUNASR_PROVIDER_ID = "provider_local_funasr";
export const FUNASR_MODEL_ID = "model_local_sensevoice";
/** funasr-server 暴露的模型名 */
export const FUNASR_MODEL_NAME = "sensevoice";

export interface FunasrPaths {
  root: string;
  /** 托管 Python 运行时 */
  pythonExe: string;
  venvDir: string;
  venvPython: string;
  /** funasr-server 控制台入口 */
  serverExe: string;
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
    serverExe: path.join(root, "env", "Scripts", "funasr-server.exe"),
    modelsDir: path.join(root, "models"),
    stateFile: path.join(root, "state.json"),
  };
}

export function isFunasrInstalled(paths = getFunasrPaths()): boolean {
  return fs.existsSync(paths.serverExe) && fs.existsSync(paths.venvPython);
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
