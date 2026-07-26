import { describe, expect, it } from "vitest";
import type { ImportTask } from "@guizhi/shared/types";
import {
  formatDuration,
  needsCaptureToolSetup,
  resolveTaskFolder,
  resolveTaskHost,
} from "../../../src/renderer/components/imports/import-task-meta";
import {
  countByFilter,
  filterTasks,
} from "../../../src/renderer/stores/import.store";

function makeTask(patch: Partial<ImportTask> = {}): ImportTask {
  return {
    id: patch.id ?? "task-1",
    sourceKind: patch.sourceKind ?? "url",
    sourceInput: patch.sourceInput ?? "https://example.com/a",
    displayName: patch.displayName ?? "示例条目",
    status: patch.status ?? "completed",
    stage: patch.stage ?? null,
    error: patch.error ?? null,
    itemType: patch.itemType ?? null,
    resultItemId: patch.resultItemId ?? null,
    duplicateItemId: patch.duplicateItemId ?? null,
    collectionId: null,
    tagNames: [],
    createdAt: patch.createdAt ?? 0,
    updatedAt: patch.updatedAt ?? 0,
  };
}

describe("formatDuration", () => {
  it("一小时以内走 M:SS", () => {
    expect(formatDuration(0)).toBe("0:00");
    expect(formatDuration(7_000)).toBe("0:07");
    expect(formatDuration(187_000)).toBe("3:07");
  });

  it("超过一小时补上小时段", () => {
    expect(formatDuration(3_600_000)).toBe("1:00:00");
    expect(formatDuration(4_325_000)).toBe("1:12:05");
  });

  it("负数按 0 处理（时钟偏移不该显示成 -1:59）", () => {
    expect(formatDuration(-5_000)).toBe("0:00");
  });
});

describe("来源标记", () => {
  it("URL 任务取域名并去掉 www.", () => {
    expect(resolveTaskHost(makeTask({ sourceInput: "https://www.v2ex.com/t/1" }))).toBe(
      "v2ex.com",
    );
    expect(
      resolveTaskHost(makeTask({ sourceInput: "https://www.iesdouyin.com/share/video/1/" })),
    ).toBe("iesdouyin.com");
  });

  it("非 URL 任务与非法链接不给域名", () => {
    expect(resolveTaskHost(makeTask({ sourceKind: "text", sourceInput: "一段笔记" }))).toBeNull();
    expect(resolveTaskHost(makeTask({ sourceInput: "not a url" }))).toBeNull();
  });

  it("文件任务给出所在目录", () => {
    expect(
      resolveTaskFolder(
        makeTask({ sourceKind: "file", sourceInput: "D:\\docs\\读书笔记.md" }),
      ),
    ).toBe("D:/docs");
    expect(resolveTaskFolder(makeTask())).toBeNull();
  });
});

describe("needsCaptureToolSetup", () => {
  it("认得出外部工具缺失（重试解决不了，得先去装）", () => {
    expect(
      needsCaptureToolSetup(
        "检测到B站视频链接，但尚未安装 yt-dlp，无法解析视频信息。",
      ),
    ).toBe(true);
  });

  it("普通抓取失败不给安装入口", () => {
    expect(needsCaptureToolSetup("HTTP 522：源站没有响应")).toBe(false);
    expect(needsCaptureToolSetup("yt-dlp 退出码 1: 视频不存在")).toBe(false);
    expect(needsCaptureToolSetup(null)).toBe(false);
  });
});

describe("列表筛选", () => {
  const tasks = [
    makeTask({ id: "a", status: "processing", displayName: "正在采集" }),
    makeTask({ id: "b", status: "pending", displayName: "排队中" }),
    makeTask({ id: "c", status: "completed", displayName: "SQLite 队列" }),
    makeTask({ id: "d", status: "failed", displayName: "失败的" }),
    makeTask({ id: "e", status: "duplicate", displayName: "重复的" }),
    makeTask({ id: "f", status: "canceled", displayName: "取消的" }),
  ];

  it("active 合并 pending 与 processing", () => {
    expect(filterTasks(tasks, "active", "").map((task) => task.id)).toEqual([
      "a",
      "b",
    ]);
  });

  it("其余档位按状态精确匹配", () => {
    expect(filterTasks(tasks, "failed", "").map((task) => task.id)).toEqual(["d"]);
    expect(filterTasks(tasks, "canceled", "").map((task) => task.id)).toEqual(["f"]);
    expect(filterTasks(tasks, "all", "")).toHaveLength(6);
  });

  it("搜索同时匹配显示名与原始链接", () => {
    const withLink = [
      makeTask({
        id: "g",
        displayName: "为什么 SQLite 不适合做队列",
        sourceInput: "https://www.v2ex.com/t/1223399",
      }),
    ];
    // 采集成功后显示名换成了标题，用户手里往往只有链接
    expect(filterTasks(withLink, "all", "1223399")).toHaveLength(1);
    expect(filterTasks(withLink, "all", "sqlite")).toHaveLength(1);
    expect(filterTasks(withLink, "all", "抖音")).toHaveLength(0);
  });

  it("计数与筛选口径一致", () => {
    const counts = countByFilter(tasks);
    expect(counts).toEqual({
      all: 6,
      active: 2,
      completed: 1,
      duplicate: 1,
      failed: 1,
      canceled: 1,
    });
  });
});
