import { useCallback, useEffect, useRef, useState } from "react";

/**
 * 渲染进程侧的引擎状态缓存。
 *
 * 设置页每次进入都会重建内容子树（SettingsPage 用 key 强制重挂载），若每次都
 * 从「检测中…」起步，用户看到的就是无意义的转圈。这里按 cacheKey 记住上次结果，
 * 重新挂载时先直接展示旧值，再在后台静默刷新（陈旧优先 / stale-while-revalidate）。
 *
 * 缓存随窗口生命周期存在；主进程侧还有一层带 TTL 的探测缓存，两者配合后
 * 反复进出设置页不会重复 spawn 可执行文件。
 */
const statusCache = new Map<string, unknown>();

/** 供测试重置模块级缓存 */
export function clearEngineStatusCache(): void {
  statusCache.clear();
}

export interface EngineStatusResult<T> {
  /** 已知状态（可能来自缓存）；从未探测成功过时为 null */
  status: T | null;
  /** 首次探测中——没有任何可展示的状态，此时才该显示转圈 */
  isProbing: boolean;
  /** 任一探测进行中，含后台静默刷新 */
  isRefreshing: boolean;
  /** 探测失败原因；成功后清空 */
  error: string | null;
  /** force 为 true 时绕过主进程缓存重新探测 */
  refresh: (force?: boolean) => Promise<void>;
}

export function useEngineStatus<T>(
  cacheKey: string,
  load: (force: boolean) => Promise<T>,
): EngineStatusResult<T> {
  // load 每次渲染都是新函数，用 ref 持有以免 effect 反复触发
  const loadRef = useRef(load);
  loadRef.current = load;

  const [status, setStatus] = useState<T | null>(
    () => (statusCache.get(cacheKey) as T | undefined) ?? null,
  );
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(
    async (force = false) => {
      setIsRefreshing(true);
      try {
        const next = await loadRef.current(force);
        statusCache.set(cacheKey, next);
        setStatus(next);
        setError(null);
      } catch (cause) {
        console.error(`采集引擎状态探测失败（${cacheKey}）：`, cause);
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setIsRefreshing(false);
      }
    },
    [cacheKey],
  );

  // cacheKey 变化意味着换了探测目标（如自定义路径改了）：先切到该键的缓存值再刷新
  useEffect(() => {
    setStatus((statusCache.get(cacheKey) as T | undefined) ?? null);
    setError(null);
    void refresh();
  }, [cacheKey, refresh]);

  return {
    status,
    isProbing: status === null && error === null,
    isRefreshing,
    error,
    refresh,
  };
}
