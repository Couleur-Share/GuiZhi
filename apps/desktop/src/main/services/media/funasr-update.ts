/** Windows FunASR 更新：保留模型与配置，备份依赖环境，验收失败恢复。 */
import fs from "fs";
import path from "path";
import type {
  FunasrInstallProgress,
  ToolUpdateCheck,
} from "@guizhi/shared/types";
import { resolvePublicAddress } from "../net-safety";
import { fetchWithNetworkProxy, hasAnyProxyConfigured } from "../network-proxy";
import { ensureFunasrPip, type RunFunasrTool } from "./funasr-pip";
import {
  cleanupFunasrBackups,
  createFunasrBackup,
  discardFunasrBackup,
  markFunasrBackup,
  preserveFunasrRollbackEnvironment,
} from "./funasr-update-backup";
import { readDirectoryIdentity } from "./funasr-update-paths";
import {
  assertFunasrUpdateSpace,
  formatFunasrBytes,
  planFunasrUpdateSpace,
} from "./funasr-update-storage";
import {
  getFunasrPaths,
  readFunasrState,
  resolveFunasrEngineFlavor,
  writeFunasrState,
} from "./funasr-paths";
import {
  isFunasrPortListening,
  runFunasrMaintenance,
  stopFunasrService,
} from "./funasr-service";

const PYPI_URL = "https://pypi.org/pypi/funasr/json";
const VERSION_PATTERN = /^\d+\.\d+(?:\.\d+)*$/;

/** 只接收稳定数字版本，避免预发布、未知格式或版本字符串进入 pip 参数。 */
export function isNewerFunasrVersion(latest: string, current: string): boolean {
  if (!VERSION_PATTERN.test(latest) || !VERSION_PATTERN.test(current)) {
    throw new Error("无法识别 FunASR 版本，请重新安装引擎后再检查更新");
  }
  const a = latest.split(".").map(Number);
  const b = current.split(".").map(Number);
  for (let i = 0; i < Math.max(a.length, b.length); i += 1) {
    if ((a[i] ?? 0) !== (b[i] ?? 0)) return (a[i] ?? 0) > (b[i] ?? 0);
  }
  return false;
}

async function fetchLatestVersion(): Promise<string> {
  await resolvePublicAddress(new URL(PYPI_URL).hostname, {
    allowProxyCompatibilityAddress: hasAnyProxyConfigured(),
  });
  const response = await fetchWithNetworkProxy(PYPI_URL, {
    signal: AbortSignal.timeout(30_000),
    redirect: "error",
  });
  if (!response.ok)
    throw new Error(`PyPI 检查更新失败：HTTP ${response.status}`);
  const data = (await response.json()) as {
    info?: { version?: string; yanked?: boolean };
    urls?: { yanked?: boolean }[];
  };
  const version = data.info?.version;
  if (
    !version ||
    !VERSION_PATTERN.test(version) ||
    data.info?.yanked ||
    !data.urls?.some((file) => file.yanked === false)
  ) {
    throw new Error("PyPI 未返回可用的 FunASR 稳定版本");
  }
  return version;
}

export async function checkFunasrUpdate(): Promise<ToolUpdateCheck> {
  const paths = getFunasrPaths();
  if (
    process.platform !== "win32" ||
    resolveFunasrEngineFlavor(paths) !== "python"
  ) {
    throw new Error("当前引擎不支持独立更新；GGUF 运行时需随归知适配更新");
  }
  const current = readFunasrState(paths)?.funasrVersion;
  if (!current) throw new Error("无法读取已安装版本，请重新安装引擎");
  const latest = await fetchLatestVersion();
  return {
    current,
    latest,
    updateAvailable: isNewerFunasrVersion(latest, current),
  };
}

/** 等待进程确实退出；外部托管的服务仍占端口时拒绝覆盖它的依赖。 */
async function stopAndWait(): Promise<void> {
  stopFunasrService();
  for (let i = 0; i < 20; i += 1) {
    await new Promise((resolve) => setTimeout(resolve, 250));
    if (!(await isFunasrPortListening())) return;
  }
  throw new Error("本地转写服务仍在运行，请关闭外部转写服务后重试");
}

export async function updateWindowsFunasr(
  version: string,
  runTool: RunFunasrTool,
  onProgress?: (progress: FunasrInstallProgress) => void,
): Promise<{ version: string; warning?: string }> {
  if (typeof version !== "string" || !VERSION_PATTERN.test(version)) {
    throw new Error("无效的 FunASR 更新版本");
  }
  const emitProgress = (progress: FunasrInstallProgress) => {
    try {
      onProgress?.(progress);
    } catch {
      console.warn("[funasr] 更新进度通知失败，继续维护事务");
    }
  };
  // 重新核实远端，避免检查后撤回的版本或渲染进程传入任意包。
  const check = await checkFunasrUpdate();
  if (check.latest !== version)
    throw new Error("可用版本已变化，请重新检查更新");
  if (!check.updateAvailable) return { version: check.current! };
  return runFunasrMaintenance(async (startService) => {
    const paths = getFunasrPaths();
    const state = readFunasrState(paths);
    if (!state) throw new Error("引擎安装记录不存在，请重新安装");
    emitProgress({ phase: "prepare", percent: null });
    const warnings = await cleanupFunasrBackups(paths.root);
    // 检查发生在停服务、创建备份和改依赖之前，空间不足不影响原引擎。
    const budget = await planFunasrUpdateSpace(paths.root, paths.venvDir);
    const environmentIdentity = await readDirectoryIdentity(paths.venvDir);
    const backup = await createFunasrBackup(paths.root, state, version);
    const backupEnv = path.join(backup.directory, "env");
    const temporary = path.join(backup.directory, "tmp");
    // pip 缓存禁用、临时目录固定，避免更新偷偷占满未检查的系统临时盘。
    const runUpdateTool: RunFunasrTool = (executable, args, options) =>
      runTool(executable, args, {
        ...options,
        env: {
          ...process.env,
          TMPDIR: temporary,
          TMP: temporary,
          TEMP: temporary,
        },
      });
    let changed = false;
    let stopped = false;
    let preserveBackup = false;
    let failure: unknown;
    try {
      emitProgress({
        phase: "backup",
        percent: null,
        detail:
          `预计备份 ${formatFunasrBytes(budget.backupBytes)}；更新预留 ${formatFunasrBytes(budget.workBytes)}。\n${warnings.join("\n")}`.trim(),
      });
      await stopAndWait();
      stopped = true;
      // venv 不可迁移执行；备份只用于还原到原路径，升级仍在原环境内完成。
      await fs.promises.cp(paths.venvDir, backupEnv, { recursive: true });
      await markFunasrBackup(backup, "ready");
      // 备份期间其他程序也可能占空间；进入写入阶段前再检查一次。
      await assertFunasrUpdateSpace(paths.root, budget.workBytes);
      await fs.promises.mkdir(temporary);
      await markFunasrBackup(backup, "updating");
      // 默认保留：只有新版本验收或旧版本恢复成功，才重新允许清理。
      preserveBackup = true;
      changed = true;
      emitProgress({ phase: "deps", percent: null });
      // 修复也在备份保护范围内；失败仍恢复完整旧环境。
      await ensureFunasrPip(paths.venvPython, runUpdateTool, () => {
        emitProgress({
          phase: "deps",
          percent: null,
          detail: "正在离线修复更新工具 pip",
        });
      });
      let lastEmit = 0;
      await runUpdateTool(
        paths.venvPython,
        [
          "-I",
          "-m",
          "pip",
          "install",
          "--no-cache-dir",
          "--upgrade",
          "--upgrade-strategy",
          "only-if-needed",
          "--no-warn-script-location",
          "--only-binary=:all:",
          `funasr==${version}`,
          "-i",
          "https://pypi.org/simple",
        ],
        {
          timeoutMs: 30 * 60_000,
          onOutput: (line) => {
            if (Date.now() - lastEmit < 500) return;
            lastEmit = Date.now();
            emitProgress({
              phase: "deps",
              percent: null,
              detail: line.slice(0, 80),
            });
          },
        },
      );
      emitProgress({ phase: "verify", percent: null });
      await runUpdateTool(paths.venvPython, ["-I", "-m", "pip", "check"]);
      const installed = (
        await runUpdateTool(paths.venvPython, [
          "-I",
          "-c",
          "from importlib.metadata import version; print(version('funasr'))",
        ])
      ).stdout.trim();
      if (installed !== version)
        throw new Error(`更新后版本不符：${installed}`);
      await startService();
      writeFunasrState({ ...state, funasrVersion: installed }, paths);
      preserveBackup = false;
    } catch (cause) {
      failure = cause instanceof Error ? cause : new Error(String(cause));
      if (changed) {
        emitProgress({ phase: "rollback", percent: null });
        try {
          await stopAndWait();
          await preserveFunasrRollbackEnvironment(
            backup,
            paths.venvDir,
            environmentIdentity,
          );
          await fs.promises.cp(backupEnv, paths.venvDir, { recursive: true });
          writeFunasrState(state, paths);
          await startService();
          preserveBackup = false;
          failure = new Error(`更新失败，已恢复原版本：${String(cause)}`, {
            cause,
          });
        } catch (rollbackError) {
          preserveBackup = true;
          // 即使标记失败，磁盘上先前的 updating 也不会被自动清理。
          await markFunasrBackup(backup, "recovery-required").catch(
            () => undefined,
          );
          failure = new Error(
            `更新失败：${String(cause)}；恢复失败：${String(rollbackError)}；备份保留在 ${backup.directory}`,
            { cause: rollbackError },
          );
        }
      } else if (stopped) {
        // 备份失败或二次空间检查失败时依赖没动过，但先前停掉的服务要恢复。
        try {
          await startService();
        } catch (restartError) {
          failure = new Error(
            `更新尚未开始：${String(cause)}；原引擎重新启动失败：${String(restartError)}`,
            { cause: restartError },
          );
        }
      }
    } finally {
      if (!preserveBackup) {
        const warning = await discardFunasrBackup(backup);
        if (warning) warnings.push(warning);
      }
    }
    const warning = warnings.join("\n");
    if (failure) {
      if (!warning) throw failure;
      throw new Error(`${String(failure)}\n${warning}`, { cause: failure });
    }
    return { version, ...(warning ? { warning } : {}) };
  });
}
