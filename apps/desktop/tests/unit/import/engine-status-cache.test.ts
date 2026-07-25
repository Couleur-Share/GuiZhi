import { describe, expect, it, vi } from "vitest";
import { createStatusCache } from "../../../src/main/services/media/engine-status-cache";

const TTL = 1000;

function fixedClock(start = 0) {
  let value = start;
  return {
    now: () => value,
    advance: (ms: number) => {
      value += ms;
    },
  };
}

describe("采集引擎状态缓存", () => {
  it("同一个键在 TTL 内只探测一次", async () => {
    const clock = fixedClock();
    const cache = createStatusCache<string>(TTL, clock.now);
    const load = vi.fn(async () => "ready");

    expect(await cache.read("", load)).toBe("ready");
    clock.advance(TTL - 1);
    expect(await cache.read("", load)).toBe("ready");

    expect(load).toHaveBeenCalledTimes(1);
  });

  it("TTL 过期后重新探测", async () => {
    const clock = fixedClock();
    const cache = createStatusCache<string>(TTL, clock.now);
    const load = vi.fn(async () => "ready");

    await cache.read("", load);
    clock.advance(TTL);
    await cache.read("", load);

    expect(load).toHaveBeenCalledTimes(2);
  });

  it("键变化（自定义路径改了）视为未命中", async () => {
    const cache = createStatusCache<string>(TTL, fixedClock().now);
    const load = vi.fn(async (): Promise<string> => "ready");

    await cache.read("", load);
    await cache.read("D:/tools/yt-dlp.exe", load);

    expect(load).toHaveBeenCalledTimes(2);
  });

  it("force 绕过缓存", async () => {
    const cache = createStatusCache<string>(TTL, fixedClock().now);
    const load = vi.fn(async () => "ready");

    await cache.read("", load);
    await cache.read("", load, true);

    expect(load).toHaveBeenCalledTimes(2);
  });

  it("invalidate 后下一次读取重新探测", async () => {
    const cache = createStatusCache<string>(TTL, fixedClock().now);
    const load = vi.fn(async () => "ready");

    await cache.read("", load);
    cache.invalidate();
    await cache.read("", load);

    expect(load).toHaveBeenCalledTimes(2);
  });

  // StrictMode 的双次挂载、多窗口同时打开设置页都会并发调进来，
  // 不合并的话就是重复 spawn 可执行文件。
  it("并发读取合并为一次探测", async () => {
    const cache = createStatusCache<string>(TTL, fixedClock().now);
    let release: (value: string) => void = () => {};
    const load = vi.fn(
      () =>
        new Promise<string>((resolve) => {
          release = resolve;
        }),
    );

    const first = cache.read("", load);
    const second = cache.read("", load);
    release("ready");

    expect(await first).toBe("ready");
    expect(await second).toBe("ready");
    expect(load).toHaveBeenCalledTimes(1);
  });

  it("探测失败不写入缓存，下一次重新尝试", async () => {
    const cache = createStatusCache<string>(TTL, fixedClock().now);
    const load = vi
      .fn<() => Promise<string>>()
      .mockRejectedValueOnce(new Error("spawn 失败"))
      .mockResolvedValueOnce("ready");

    await expect(cache.read("", load)).rejects.toThrow("spawn 失败");
    expect(await cache.read("", load)).toBe("ready");
    expect(load).toHaveBeenCalledTimes(2);
  });
});
