import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  clearEngineStatusCache,
  useEngineStatus,
} from "../../../src/renderer/components/settings/capture/use-engine-status";

beforeEach(() => {
  clearEngineStatusCache();
});

describe("useEngineStatus", () => {
  it("首次挂载没有可展示的状态，处于探测中", async () => {
    const load = vi.fn(async () => "ready");
    const { result } = renderHook(() => useEngineStatus("ytdlp:", load));

    expect(result.current.isProbing).toBe(true);
    expect(result.current.status).toBeNull();

    await waitFor(() => expect(result.current.status).toBe("ready"));
    expect(result.current.isProbing).toBe(false);
  });

  /**
   * 设置页每次进入都会重建子树。这条用例锁住「重新挂载不再转圈」——
   * 也就是用户反馈的那个问题。
   */
  it("重新挂载直接展示缓存值，不回到探测中", async () => {
    const load = vi.fn(async () => "ready");
    const first = renderHook(() => useEngineStatus("ytdlp:", load));
    await waitFor(() => expect(first.result.current.status).toBe("ready"));
    first.unmount();

    const second = renderHook(() => useEngineStatus("ytdlp:", load));
    expect(second.result.current.status).toBe("ready");
    expect(second.result.current.isProbing).toBe(false);

    await waitFor(() => expect(second.result.current.isRefreshing).toBe(false));
  });

  it("重新挂载仍会在后台静默刷新", async () => {
    const load = vi.fn(async () => "ready");
    const first = renderHook(() => useEngineStatus("ytdlp:", load));
    await waitFor(() => expect(first.result.current.status).toBe("ready"));
    first.unmount();

    const second = renderHook(() => useEngineStatus("ytdlp:", load));
    await waitFor(() => {
      expect(load).toHaveBeenCalledTimes(2);
      expect(second.result.current.isRefreshing).toBe(false);
    });
  });

  it("换探测目标（自定义路径变了）不会沿用上一个目标的结果", async () => {
    const load = vi.fn(async (): Promise<string> => "ready");
    const { result, rerender } = renderHook(
      ({ key }: { key: string }) => useEngineStatus(key, load),
      { initialProps: { key: "ytdlp:" } },
    );
    await waitFor(() => expect(result.current.status).toBe("ready"));

    rerender({ key: "ytdlp:D:/tools/yt-dlp.exe" });
    expect(result.current.status).toBeNull();
    expect(result.current.isProbing).toBe(true);

    await waitFor(() => expect(result.current.status).toBe("ready"));
  });

  it("手动重新检测透传 force", async () => {
    const load = vi.fn(async () => "ready");
    const { result } = renderHook(() => useEngineStatus("ytdlp:", load));
    await waitFor(() => expect(result.current.status).toBe("ready"));

    await act(async () => {
      await result.current.refresh(true);
    });

    expect(load).toHaveBeenNthCalledWith(1, false);
    expect(load).toHaveBeenLastCalledWith(true);
  });

  it("探测失败时退出探测中并给出原因", async () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    const load = vi.fn(async () => {
      throw new Error("IPC 不可用");
    });
    const { result } = renderHook(() => useEngineStatus("ffmpeg:", load));

    await waitFor(() => expect(result.current.error).toBe("IPC 不可用"));
    expect(result.current.isProbing).toBe(false);
    expect(result.current.status).toBeNull();
  });
});
