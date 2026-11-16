/**
 * 对等工程的本机路径约定。
 *
 * 基准目录（不入库，放 PNG、参照构建、报告）：环境变量 GUIZHI_BENCH_HOME，缺省为仓库同级的 GuiZhi-bench。
 * 对本仓库的 worktree（如 D:\Project\GuiZhi-viva）就是 D:\Project\GuiZhi-bench。
 */
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.resolve(here, "../../../../..");

export function benchHome(env = process.env) {
  return path.resolve(env.GUIZHI_BENCH_HOME || path.join(REPO_ROOT, "..", "GuiZhi-bench"));
}

/** React 参照构建（GuiZhi.exe + Sqlitecore.dll + renderer dist）所在目录。 */
export function reactRefDir(env = process.env) {
  return path.join(benchHome(env), "react-ref");
}

/** 采集与对比产物（PNG 与报告，不入库）。 */
export function parityOutDir(env = process.env) {
  return path.join(benchHome(env), "parity");
}
