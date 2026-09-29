/**
 * 交付体积门禁：随包 Python 运行包与 Windows 安装包各设一条上限。
 *
 * 背景：已发布的 x64 安装包从 v0.22.0 的 83.9 MiB 涨到 v0.26.0 的 208.4 MiB（v0.23–v0.24 随包 Chromium 时曾达 341 MiB），
 * 增量主要来自随包 Python 运行包；renderer 的 gzip 预算（apps/desktop/bundle-budget.json）只覆盖前端资源，
 * 这几次增长没有任何门禁拦截。
 * 运行包文件数同样受限：每次启动 Python 提取进程前都会逐文件校验哈希，耗时与文件数、字节数成正比
 * （Windows x64 实测：15,128 个文件 / 585.9 MiB 约 11 s，裁剪后 8,099 个 / 193.7 MiB 约 4.7 s）。
 *
 * 预算见 config/package-size-budget.json：运行包取实测值上浮约 5%（同一份锁文件的构建结果几乎逐字节一致），
 * 安装包上浮约 8%（应用代码会自然增长），避免正常波动误报。
 * 需要有意增长时，在同一个提交里修改预算并写明原因（升级 Crawl4AI、新增随包组件等），
 * 修改前先确认没有引入提取路径不需要的依赖。
 *
 * 用法：
 *   node scripts/check-package-size.mjs --runtime apps/desktop/resources/crawl4ai
 *   node scripts/check-package-size.mjs --installer apps/desktop/dist/GuiZhi-Setup-<版本>-x64.exe
 * 退出码：0 通过；1 超出预算；2 参数或预算文件错误。
 */
import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
export const DEFAULT_BUDGET_FILE = path.join(ROOT, "config", "package-size-budget.json");
const MIB = 1024 * 1024;
const mib = (bytes) => (bytes / MIB).toFixed(1);
const count = (value) => value.toLocaleString("en-US");

/** 统计运行包的文件数与字节数。顶层 browser/ 会被 electron-builder 的资源过滤排除，这里与打包保持一致。 */
export function measureRuntime(directory) {
  let bytes = 0;
  let files = 0;
  const walk = (current, topLevel) => {
    for (const entry of readdirSync(current, { withFileTypes: true })) {
      if (topLevel && entry.name === "browser") continue;
      const full = path.join(current, entry.name);
      if (entry.isDirectory()) walk(full, false);
      else if (entry.isFile()) {
        files += 1;
        bytes += statSync(full).size;
      }
    }
  };
  walk(directory, true);
  return { bytes, files };
}

/** 返回违规说明列表；空数组表示通过。 */
export function checkRuntime(measured, budget) {
  const problems = [];
  if (measured.bytes > budget.maxBytes)
    problems.push(`运行包 ${mib(measured.bytes)} MiB，超过上限 ${mib(budget.maxBytes)} MiB（超出 ${mib(measured.bytes - budget.maxBytes)} MiB）`);
  if (measured.files > budget.maxFiles)
    problems.push(`运行包 ${count(measured.files)} 个文件，超过上限 ${count(budget.maxFiles)} 个`);
  return problems;
}

export function checkInstaller(bytes, budget) {
  return bytes > budget.maxBytes
    ? [`安装包 ${mib(bytes)} MiB，超过上限 ${mib(budget.maxBytes)} MiB（超出 ${mib(bytes - budget.maxBytes)} MiB）`]
    : [];
}

function readBudget(file) {
  const budget = JSON.parse(readFileSync(file, "utf8"));
  const positive = (value) => Number.isInteger(value) && value > 0;
  if (!positive(budget.runtime?.maxBytes) || !positive(budget.runtime?.maxFiles) || !positive(budget.installer?.maxBytes))
    throw new Error(`预算文件缺少有效的 runtime.maxBytes / runtime.maxFiles / installer.maxBytes：${file}`);
  return budget;
}

function parseArguments(argv) {
  const options = { budget: DEFAULT_BUDGET_FILE };
  for (let index = 0; index < argv.length; index += 2) {
    const name = argv[index];
    const value = argv[index + 1];
    if (!["--runtime", "--installer", "--budget"].includes(name) || !value) throw new Error(`参数无效：${name ?? ""}`);
    options[name.slice(2)] = value;
  }
  if (!options.runtime && !options.installer) throw new Error("需要 --runtime <目录> 或 --installer <文件>");
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
    log.error(`${error.message}\n用法：node scripts/check-package-size.mjs --runtime <目录> | --installer <文件> [--budget <文件>]`);
    return 2;
  }
  const problems = [];
  try {
    if (options.runtime) {
      const measured = measureRuntime(options.runtime);
      log.log(`[运行包] ${mib(measured.bytes)} MiB / 上限 ${mib(budget.runtime.maxBytes)} MiB；${count(measured.files)} 个文件 / 上限 ${count(budget.runtime.maxFiles)} 个`);
      problems.push(...checkRuntime(measured, budget.runtime));
    }
    if (options.installer) {
      const { size } = statSync(options.installer);
      log.log(`[安装包] ${mib(size)} MiB / 上限 ${mib(budget.installer.maxBytes)} MiB`);
      problems.push(...checkInstaller(size, budget.installer));
    }
  } catch (error) {
    log.error(`无法读取待检查产物：${error.message}`);
    return 2;
  }
  if (problems.length === 0) {
    log.log("体积门禁通过");
    return 0;
  }
  for (const problem of problems) log.error(`体积门禁失败：${problem}`);
  log.error("先检查是否引入了提取路径不需要的依赖（见 scripts/build-crawl4ai.py 的 PRUNED_DISTRIBUTIONS）；确属有意增长时，在同一提交里修改 config/package-size-budget.json 并写明原因。");
  return 1;
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  process.exitCode = run(process.argv.slice(2));
}
