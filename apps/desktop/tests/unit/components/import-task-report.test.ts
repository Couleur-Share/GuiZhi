import { describe, expect, it } from "vitest";
import type { ImportTask } from "@guizhi/shared/types";
import { buildImportTaskReport } from "../../../src/renderer/components/imports/import-task-report";

/** 单测不拉 i18next：按 fallback 走，并把插值自己填了 */
const translate = (
  _key: string,
  fallback: string,
  options?: Record<string, unknown>,
): string =>
  fallback.replace(/\{\{(\w+)\}\}/g, (_, name: string) =>
    String(options?.[name] ?? ""),
  );

function makeTask(patch: Partial<ImportTask> = {}): ImportTask {
  return {
    id: "task-1",
    sourceKind: "url",
    sourceInput: "https://v.douyin.com/abcdefg/",
    displayName: "API中转站暴利神话的真相",
    status: "completed",
    stage: null,
    error: null,
    warning: null,
    itemType: "video",
    resultItemId: "item-9",
    duplicateItemId: null,
    collectionId: null,
    tagNames: [],
    stageStats: [
      { stage: "fetching", ms: 200 },
      { stage: "transcribing", ms: 46_000 },
      {
        stage: "formatting",
        ms: 21_000,
        calls: 4,
        failedCalls: 1,
        promptTokens: 3_100,
        completionTokens: 4_724,
        models: ["qwen3.5-flash"],
      },
    ],
    createdAt: 1_800_000_000_000,
    updatedAt: 1_800_000_100_000,
    ...patch,
  };
}

const context = {
  translate,
  formatTime: (ms: number) => `T${ms}`,
};

describe("buildImportTaskReport", () => {
  it("表头给出任务、状态、来源与时间", () => {
    const report = buildImportTaskReport(makeTask(), context);
    expect(report).toContain("# 导入任务诊断");
    expect(report).toContain("- 任务：API中转站暴利神话的真相");
    expect(report).toContain("- 状态：已完成");
    expect(report).toContain("- 来源：https://v.douyin.com/abcdefg/");
    expect(report).toContain("- 入队：T1800000000000");
    expect(report).toContain("- 任务 ID：task-1");
  });

  it("阶段表**不折叠**亚秒阶段：折叠是为了扫视，排查时少一行就是少一条线索", () => {
    const report = buildImportTaskReport(makeTask(), context);
    expect(report).toContain("## 阶段耗时（共 1:07）");
    // 界面上这一行会被折进「另有 N 个阶段不足 1 秒」
    expect(report).toContain("| 抓取中 | 0:00 | <1% |  |");
    expect(report).toContain("| 语音转写 | 0:46 | 68% |  |");
    expect(report).toContain(
      "| 文字稿排版 | 0:21 | 31% | 4 次调用 · 1 次失败 · 7824 token · qwen3.5-flash |",
    );
  });

  it("拿不到版本时不写那一行，而不是留一个空的「应用：」", () => {
    expect(buildImportTaskReport(makeTask(), context)).not.toContain("- 应用：");
    expect(
      buildImportTaskReport(makeTask(), {
        ...context,
        appVersion: "0.12.0",
        platform: "win32",
      }),
    ).toContain("- 应用：GuiZhi 0.12.0 · win32");
  });

  it("报错与缺失提示各占一节；没有就不留空标题", () => {
    const clean = buildImportTaskReport(makeTask(), context);
    expect(clean).not.toContain("## 报错");
    expect(clean).not.toContain("## 缺失提示");

    const noisy = buildImportTaskReport(
      makeTask({
        status: "failed",
        error: "网页抓取失败：HTTP 522",
        warning: "文字稿生成失败：本地转写服务启动失败",
      }),
      context,
    );
    expect(noisy).toContain("- 状态：失败");
    expect(noisy).toContain("## 报错\n\n网页抓取失败：HTTP 522");
    expect(noisy).toContain("## 缺失提示\n\n文字稿生成失败：本地转写服务启动失败");
  });

  it("标题里的竖线要转义，否则表格被从中间切断", () => {
    const report = buildImportTaskReport(
      makeTask({
        stageStats: [{ stage: "fetching", ms: 1_000, calls: 1, models: ["a|b"] }],
      }),
      context,
    );
    expect(report).toContain("a\\|b");
    expect(report.split("\n").filter((line) => line.startsWith("| 抓取中"))).toHaveLength(1);
  });

  it("没有阶段统计时整节不出现，不摆一张空表", () => {
    const report = buildImportTaskReport(
      makeTask({ stageStats: null, status: "canceled" }),
      context,
    );
    expect(report).not.toContain("## 阶段耗时");
    expect(report).toContain("- 状态：已取消");
  });
});
