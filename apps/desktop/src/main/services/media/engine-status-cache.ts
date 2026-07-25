/**
 * 采集引擎状态缓存：yt-dlp / ffmpeg / 本地转写引擎的状态探测都很贵——
 * 前两者要 spawn 可执行文件跑 `--version`（各带 10 秒超时，PATH 回退时会跑两次），
 * 后者要对 127.0.0.1 发健康检查（3 秒超时，未启动时必然等满）。
 *
 * 设置页每次进入都会重新挂载并探测，因此这里按「配置键 + TTL」缓存结果，
 * 并合并并发探测（React StrictMode 的双次挂载、多窗口同时打开都会撞上）。
 * 安装 / 移除后由调用方显式失效；用户点「重新检测」时传 force 绕过缓存。
 */

/** 缓存有效期：足够覆盖设置页反复进出，又不至于长期掩盖外部变更 */
export const ENGINE_STATUS_TTL_MS = 5 * 60 * 1000;

export interface StatusCache<T> {
  /**
   * 读取状态。key 变化（如自定义路径改了）视为缓存未命中。
   * force 为 true 时跳过缓存，但仍会与进行中的探测合并。
   */
  read(key: string, load: () => Promise<T>, force?: boolean): Promise<T>;
  invalidate(): void;
}

export function createStatusCache<T>(
  ttlMs: number = ENGINE_STATUS_TTL_MS,
  now: () => number = Date.now,
): StatusCache<T> {
  let entry: { key: string; value: T; at: number } | null = null;
  let pending: { key: string; promise: Promise<T> } | null = null;

  return {
    read(key, load, force = false) {
      if (!force && entry && entry.key === key && now() - entry.at < ttlMs) {
        return Promise.resolve(entry.value);
      }
      if (pending && pending.key === key) {
        return pending.promise;
      }
      const promise = (async () => {
        try {
          const value = await load();
          entry = { key, value, at: now() };
          return value;
        } finally {
          if (pending?.key === key) {
            pending = null;
          }
        }
      })();
      pending = { key, promise };
      return promise;
    },
    invalidate() {
      entry = null;
    },
  };
}
