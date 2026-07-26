/**
 * 外部引擎版本号的跨启动缓存。
 *
 * yt-dlp 是 PyInstaller 单文件包，每跑一次 `--version` 都要把整个 Python 运行时
 * 解压到临时目录——本机实测 2.8 秒。engine-status-cache 只活在进程内，于是每次
 * 冷启动后第一次进设置页都要重付这笔钱：「采集」三行一起转圈约 3 秒才落定。
 *
 * 版本号只随可执行文件本身变化，所以这里按「路径 + 大小 + mtime」把探测结果记在
 * %userData%/tools/engine-versions.json。文件没换就直接复用，换了（重装、升级、
 * 改了自定义路径）自然不命中；用户点「重新检测」时用 force 强制绕过。
 *
 * 只缓存成功的结果：探测失败可能只是杀软临时锁住了文件，把失败钉死会让用户
 * 在文件恢复后仍然看到「不可用」，而多跑一次探测的代价远小于此。
 */
import fs from "fs";
import path from "path";
import { getToolsDir } from "../../runtime-paths";

export type VersionProbe = (executable: string) => Promise<string | null>;

interface CachedVersion {
  size: number;
  mtimeMs: number;
  version: string;
}

type VersionCacheFile = Record<string, CachedVersion>;

interface ExecutableIdentity extends Omit<CachedVersion, "version"> {
  file: string;
}

export function getEngineVersionCachePath(): string {
  return path.join(getToolsDir(), "engine-versions.json");
}

/** 裸命令名（yt-dlp / ffmpeg）按 PATH + PATHEXT 找出真实文件，找不到返回 null */
function resolveFromSearchPath(command: string): string | null {
  const extensions =
    process.platform === "win32"
      ? ["", ...(process.env.PATHEXT ?? ".EXE").split(";").filter(Boolean)]
      : [""];
  for (const dir of (process.env.PATH ?? "").split(path.delimiter)) {
    if (!dir) {
      continue;
    }
    for (const extension of extensions) {
      const candidate = path.join(dir, `${command}${extension}`);
      if (fs.existsSync(candidate)) {
        return candidate;
      }
    }
  }
  return null;
}

/** 可执行文件的身份：拿不到（不存在 / 不是文件）就没有可缓存的东西 */
function identify(executable: string): ExecutableIdentity | null {
  const hasPathSeparator = /[\\/]/.test(executable);
  const file = hasPathSeparator ? executable : resolveFromSearchPath(executable);
  if (!file) {
    return null;
  }
  try {
    const stats = fs.statSync(file);
    return stats.isFile()
      ? { file: path.resolve(file), size: stats.size, mtimeMs: stats.mtimeMs }
      : null;
  } catch {
    return null;
  }
}

function readCache(): VersionCacheFile {
  try {
    const parsed: unknown = JSON.parse(
      fs.readFileSync(getEngineVersionCachePath(), "utf8"),
    );
    return parsed && typeof parsed === "object"
      ? (parsed as VersionCacheFile)
      : {};
  } catch {
    return {};
  }
}

/** 顺手丢掉指向已消失文件的条目，免得缓存随卸载与改路径无限增长 */
function writeCache(cache: VersionCacheFile): void {
  const alive = Object.fromEntries(
    Object.entries(cache).filter(([file]) => fs.existsSync(file)),
  );
  try {
    const cachePath = getEngineVersionCachePath();
    fs.mkdirSync(path.dirname(cachePath), { recursive: true });
    fs.writeFileSync(cachePath, `${JSON.stringify(alive, null, 2)}\n`);
  } catch (error) {
    console.warn("[engine-version] 版本缓存写入失败（不影响探测结果）:", error);
  }
}

/**
 * 给版本探测包一层持久化缓存：同一个文件只在首次（或 force）时真去 spawn。
 * 探测目标不落在磁盘上时原样透传，交由 probe 自己失败。
 */
export function withCachedVersion(
  probe: VersionProbe,
  force = false,
): VersionProbe {
  return async (executable) => {
    const identity = identify(executable);
    if (!identity) {
      return probe(executable);
    }

    if (!force) {
      const cached = readCache()[identity.file];
      if (
        cached &&
        cached.size === identity.size &&
        cached.mtimeMs === identity.mtimeMs
      ) {
        return cached.version;
      }
    }

    const version = await probe(executable);
    if (version) {
      writeCache({
        ...readCache(),
        [identity.file]: {
          size: identity.size,
          mtimeMs: identity.mtimeMs,
          version,
        },
      });
    }
    return version;
  };
}
