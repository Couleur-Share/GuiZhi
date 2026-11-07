/**
 * 交付体积门禁：Windows 安装包设一条上限。
 *
 * 背景：已发布的 x64 安装包从 v0.22.0 的 83.9 MiB 涨到 v0.26.0 的 208.3 MiB（v0.23–v0.24 随包 Chromium 时曾达 341 MiB），
 * 增量主要来自当时随包的 Python 运行包；renderer 的 gzip 预算（apps/desktop/bundle-budget.json）只覆盖前端资源，
 * 这几次增长没有任何门禁拦截。随包 Python 运行包已移除（网页正文改由主进程旁的 Node 线程提取），
 * 运行包预算随之取消。
 *
 * 预算见 config/package-size-budget.json：安装包取实测值上浮约 8%（应用代码会自然增长），避免正常波动误报。
 * 需要有意增长时，在同一个提交里修改预算并写明原因（升级 Electron、新增随包组件等），
 * 修改前先确认新增内容确实会在运行时被加载。
 *
 * 用法：
 *   node scripts/check-package-size.mjs --installer apps/desktop/dist/GuiZhi-Setup-<版本>-x64.exe
 * 退出码：0 通过；1 超出预算；2 参数或预算文件错误。
 */
import { readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const DEFAULT_BUDGET_FILE = path.join(ROOT, "config", "package-size-budget.json");
const MIB = 1024 * 1024;
const mib = (bytes) => (bytes / MIB).toFixed(1);

/** 返回违规说明列表；空数组表示通过。 */
export function checkInstaller(bytes, budget) {
  return bytes > budget.maxBytes
    ? [`安装包 ${mib(bytes)} MiB，超过上限 ${mib(budget.maxBytes)} MiB（超出 ${mib(bytes - budget.maxBytes)} MiB）`]
    : [];
}

function readBudget(file) {
  const budget = JSON.parse(readFileSync(file, "utf8"));
  const positive = (value) => Number.isInteger(value) && value > 0;
  if (!positive(budget.installer?.maxBytes))
    throw new Error(`预算文件缺少有效的 installer.maxBytes：${file}`);
  return budget;
}

function parseArguments(argv) {
  const options = { budget: DEFAULT_BUDGET_FILE };
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index];
    const value = argv[index + 1];
    if (!["--installer", "--budget"].includes(name) || !value) throw new Error(`参数无效：${name ?? ""}`);
    options[name.slice(2)] = value;
  }
  if (!options.installer) throw new Error("需要 --installer <文件>");
  return options;
}

/** 执行检查并返回退出码；log 便于测试捕获输出。 */
export function run(argv, log = console) {
  let options;
  let budget;
  try {
    options = parseArguments(argv);
    budget = readBudget(options.budget);
  } catch (error) {
    log.error(`${error.message}\n用法：node scripts/check-package-size.mjs --installer <文件> [--budget <文件>]`);
    return 2;
  }
  let problems;
  try {
    const { size } = statSync(options.installer);
    log.log(`[安装包] ${mib(size)} MiB / 上限 ${mib(budget.installer.maxBytes)} MiB`);
    problems = checkInstaller(size, budget.installer);
  } catch (error) {
    log.error(`无法读取待检查产物：${error.message}`);
    return 2;
  }
  if (problems.length === 0) {
    log.log("体积门禁通过");
    return 0;
  }
  for (const problem of problems) log.error(`体积门禁失败：${problem}`);
  log.error("先检查是否打进了运行时用不到的依赖或资源（files、extraResources、asarUnpack）；确属有意增长时，在同一提交里修改 config/package-size-budget.json 并写明原因。");
  return 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = run(process.argv.slice(2));
}
