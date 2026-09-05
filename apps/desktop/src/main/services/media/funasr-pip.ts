/** 托管环境的 pip 自检与离线修复，不下载脚本、不改转写依赖。 */
export type RunFunasrTool = (
  executable: string,
  args: string[],
  options?: {
    timeoutMs?: number;
    onOutput?: (line: string) => void;
    env?: NodeJS.ProcessEnv;
  },
) => Promise<{ stdout: string }>;

// ensurepip --upgrade 会跳过同版本的损坏安装。直接从运行时自带的 wheel
// 加载完整 pip，强制离线重装自身，避免依赖已经无法启动的 site-packages/pip。
const REPAIR_PIP_SCRIPT = `
import ensurepip
from pathlib import Path
import runpy
import sys

wheel = Path(ensurepip.__file__).parent / "_bundled" / ("pip-" + ensurepip.version() + "-py3-none-any.whl")
if not wheel.is_file():
    raise RuntimeError("Bundled pip wheel is missing")
sys.path.insert(0, str(wheel))
sys.argv = ["pip", "--isolated", "--disable-pip-version-check", "install",
            "--no-index", "--no-deps", "--no-cache-dir", "--force-reinstall",
            "--no-warn-script-location", str(wheel)]
runpy.run_module("pip", run_name="__main__", alter_sys=True)
`;

export async function ensureFunasrPip(
  python: string,
  runTool: RunFunasrTool,
  onRepair?: () => void,
): Promise<void> {
  // --version 不加载安装命令的网络依赖，无法发现 cachecontrol 源码缺失。
  const probe = () => runTool(python, ["-I", "-m", "pip", "install", "--help"]);
  try {
    await probe();
    return;
  } catch {
    onRepair?.();
  }
  try {
    await runTool(python, ["-I", "-c", REPAIR_PIP_SCRIPT], {
      timeoutMs: 120_000,
    });
    await probe();
  } catch (cause) {
    throw new Error(`本地引擎 pip 离线修复失败：${String(cause)}`, { cause });
  }
}
