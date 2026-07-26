import { beforeEach, describe, expect, it, vi } from "vitest";
import { useAskStore } from "../../../src/renderer/stores/ask.store";
import { useImportStore } from "../../../src/renderer/stores/import.store";
import { useWikiStore } from "../../../src/renderer/stores/wiki.store";

/**
 * 首次加载门禁。
 *
 * 问答 / Wiki / 导入三页都靠 hasLoaded 区分「真的是空的」与「还没读出来」，
 * 没有它，界面会先铺一屏空态引导再被真实内容整块换掉——这就是首次进入时
 * 看到的那一下闪烁。加载失败时同样必须放行：否则页面永远停在加载态，
 * 连空态引导都看不到。
 */

beforeEach(() => {
  vi.spyOn(console, "error").mockImplementation(() => {});
});

describe("问答会话首次加载", () => {
  it("失败也放行，且失败后仍可重试", async () => {
    // 去重标志是模块级的，成功一次之后就不再重跑，所以失败分支必须先验
    window.api.askSession = {
      list: vi.fn().mockRejectedValue(new Error("IPC 断了")),
      get: vi.fn(),
    };
    await useAskStore.getState().initialize();
    expect(useAskStore.getState().hasLoaded).toBe(true);
    expect(useAskStore.getState().sessions).toEqual([]);

    useAskStore.setState({ hasLoaded: false });
    window.api.askSession = {
      list: vi
        .fn()
        .mockResolvedValue([
          { id: "s1", title: "上次的对话", createdAt: 1, updatedAt: 2 },
        ]),
      get: vi.fn(),
    };
    await useAskStore.getState().initialize();
    expect(useAskStore.getState().hasLoaded).toBe(true);
    expect(useAskStore.getState().sessions).toHaveLength(1);
  });
});

describe("Wiki 目录首次加载", () => {
  beforeEach(() => {
    useWikiStore.setState({ hasLoaded: false, catalog: [], status: null });
  });

  it("目录到手后放行", async () => {
    window.api.wiki = {
      catalog: vi.fn().mockResolvedValue([]),
      status: vi.fn().mockResolvedValue({
        pageCount: 0,
        compiledItemCount: 0,
        eligibleItemCount: 3,
      }),
      backlinkCounts: vi.fn().mockResolvedValue({}),
    };
    await useWikiStore.getState().refresh();
    expect(useWikiStore.getState().hasLoaded).toBe(true);
  });

  it("加载失败也放行", async () => {
    window.api.wiki = {
      catalog: vi.fn().mockRejectedValue(new Error("IPC 断了")),
      status: vi.fn().mockResolvedValue(null),
      backlinkCounts: vi.fn().mockResolvedValue({}),
    };
    await useWikiStore.getState().refresh();
    expect(useWikiStore.getState().hasLoaded).toBe(true);
  });
});

describe("导入队列首次加载", () => {
  beforeEach(() => {
    useImportStore.setState({ hasLoaded: false, tasks: [] });
  });

  it("空队列也放行，界面据此才敢画引导页", async () => {
    window.api.import = { list: vi.fn().mockResolvedValue([]) };
    await useImportStore.getState().fetchTasks();
    expect(useImportStore.getState().hasLoaded).toBe(true);
    expect(useImportStore.getState().tasks).toEqual([]);
  });

  it("加载失败也放行", async () => {
    window.api.import = {
      list: vi.fn().mockRejectedValue(new Error("IPC 断了")),
    };
    await useImportStore.getState().fetchTasks();
    expect(useImportStore.getState().hasLoaded).toBe(true);
  });
});
