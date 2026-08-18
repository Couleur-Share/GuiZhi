import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImportTask } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ImportTaskRow } from "../../../src/renderer/components/imports/ImportTaskRow";
import { STALL_THRESHOLD_MS } from "../../../src/renderer/components/imports/import-task-meta";

const NOW = 1_800_000_000_000;

function makeTask(patch: Partial<ImportTask> = {}): ImportTask {
  return {
    id: "task-1",
    sourceKind: "url",
    sourceInput: "https://www.bilibili.com/video/BV1xx",
    displayName: "手写一个混合检索 RAG 系统",
    status: "processing",
    stage: "transcribing",
    error: null,
    warning: null,
    itemType: "video",
    resultItemId: null,
    duplicateItemId: null,
    collectionId: null,
    tagNames: [],
    stageStats: null,
    captureStrategy: "standard",
    commentLimit: 0,
    createdAt: NOW - 600_000,
    updatedAt: NOW - 10_000,
    ...patch,
  };
}

function renderRow(task: ImportTask, onOpenItem = vi.fn(), onOpenDetail = vi.fn()) {
  render(
    <ImportTaskRow
      task={task}
      now={NOW}
      isChecked={false}
      hasSelection={false}
      onToggle={vi.fn()}
      onOpenItem={onOpenItem}
      onOpenDetail={onOpenDetail}
    />,
  );
  return onOpenItem;
}

describe("导入任务行", () => {
  beforeAll(async () => {
    installWindowMocks();
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    installWindowMocks();
  });

  it("进行中：显示子阶段、来源域名与已用时长", () => {
    renderRow(makeTask());
    expect(screen.getByText("语音转写")).toBeInTheDocument();
    expect(screen.getByText("bilibili.com")).toBeInTheDocument();
    expect(screen.getByText("视频")).toBeInTheDocument();
    // 10 分钟：转写这类长任务只有一个转圈的话，用户无从判断是不是卡死了
    expect(screen.getByText("已用 10:00")).toBeInTheDocument();
  });

  it("单个阶段迟迟没动静时补一条本阶段耗时", () => {
    renderRow(makeTask({ updatedAt: NOW - STALL_THRESHOLD_MS - 30_000 }));
    expect(screen.getByText("本阶段 2:00")).toBeInTheDocument();
  });

  it("阶段推进中不打扰：未超阈值不显示本阶段耗时", () => {
    renderRow(makeTask({ updatedAt: NOW - 5_000 }));
    expect(screen.queryByText(/本阶段/)).not.toBeInTheDocument();
  });

  it("排队中报的是等待而不是「已用」：并发只有 2，后面的任务能等一个钟头", () => {
    // 入队一小时还没轮到，此时它一个字节都没抓，说「已用 1:00:00」是在讲一件没发生的事
    renderRow(
      makeTask({
        status: "pending",
        stage: null,
        createdAt: NOW - 3_600_000,
        updatedAt: NOW - 3_600_000,
      }),
    );
    expect(screen.getByText("已排队 1:00:00")).toBeInTheDocument();
    expect(screen.queryByText(/已用/)).not.toBeInTheDocument();
  });

  it("处理中的「已用」不含排队等待，与完成后的「共 X」同一口径", () => {
    // 入队 1 小时前，真正开跑只有 2 分 10 秒（已结算 2:00 + 当前阶段 10 秒）
    renderRow(
      makeTask({
        status: "processing",
        stage: "transcribing",
        createdAt: NOW - 3_600_000,
        updatedAt: NOW - 10_000,
        stageStats: [
          { stage: "fetching", ms: 5_000 },
          { stage: "video-metadata", ms: 115_000 },
          { stage: "transcribing", ms: 0 },
        ],
      }),
    );
    expect(screen.getByText("已用 2:10")).toBeInTheDocument();
  });

  it("已完成：标题可点开条目", async () => {
    const onOpenItem = renderRow(
      makeTask({ status: "completed", stage: null, resultItemId: "item-9" }),
    );
    await userEvent.click(
      screen.getByRole("button", { name: "手写一个混合检索 RAG 系统" }),
    );
    expect(onOpenItem).toHaveBeenCalledWith("item-9");
  });

  it("入库但缺了文字稿：徽标不能是绿色的「已完成」，原因要写在行里", () => {
    renderRow(
      makeTask({
        status: "completed",
        stage: null,
        resultItemId: "item-9",
        warning: "文字稿生成失败：本地转写服务启动失败",
      }),
    );
    expect(screen.getByText("完成（有缺失）")).toBeInTheDocument();
    expect(screen.queryByText("已完成")).not.toBeInTheDocument();
    expect(
      screen.getByText("文字稿生成失败：本地转写服务启动失败"),
    ).toBeInTheDocument();
  });

  it("重复：给出打开已有条目与创建副本两个出口", () => {
    renderRow(
      makeTask({
        status: "duplicate",
        stage: null,
        duplicateItemId: "item-3",
      }),
    );
    expect(screen.getByLabelText("打开已有条目")).toBeInTheDocument();
    expect(screen.getByLabelText("仍要创建副本")).toBeInTheDocument();
  });

  it("外部工具缺失的失败：除重试外还给出前往设置的入口", () => {
    renderRow(
      makeTask({
        status: "failed",
        stage: null,
        error: "检测到B站视频链接，但尚未安装 yt-dlp，无法解析视频信息。",
      }),
    );
    expect(screen.getByLabelText("重试")).toBeInTheDocument();
    expect(screen.getByLabelText("前往设置安装工具")).toBeInTheDocument();
  });

  it("普通失败只给重试，不误导用户去装工具", () => {
    renderRow(
      makeTask({
        status: "failed",
        stage: null,
        error: "网页抓取失败：HTTP 522",
      }),
    );
    expect(screen.getByLabelText("重试")).toBeInTheDocument();
    expect(screen.queryByLabelText("前往设置安装工具")).not.toBeInTheDocument();
  });

  it("只有匿名平台解析失败才显示登录态重试", () => {
    renderRow(makeTask({
      sourceInput: "https://www.xiaohongshu.com/explore/abc",
      status: "failed",
      stage: null,
      error: "[structure_missing] 笔记页未返回数据",
    }));
    expect(screen.getByLabelText("使用登录态重试")).toBeInTheDocument();
  });

  it("作品删除与已认证任务不显示登录态重试", () => {
    const { unmount } = render(
      <ImportTaskRow
        task={makeTask({
          sourceInput: "https://www.douyin.com/video/123",
          status: "failed",
          stage: null,
          error: "[note_unavailable] 作品已删除",
        })}
        now={NOW}
        isChecked={false}
        hasSelection={false}
        onToggle={vi.fn()}
        onOpenItem={vi.fn()}
        onOpenDetail={vi.fn()}
      />,
    );
    expect(screen.queryByLabelText("使用登录态重试")).not.toBeInTheDocument();
    unmount();
    renderRow(makeTask({
      sourceInput: "https://www.douyin.com/video/123",
      status: "failed",
      stage: null,
      error: "[structure_missing] 页面变化",
      captureStrategy: "authenticated",
    }));
    expect(screen.queryByLabelText("使用登录态重试")).not.toBeInTheDocument();
  });
});

describe("终态任务的阶段耗时", () => {
  beforeAll(async () => {
    installWindowMocks();
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    installWindowMocks();
  });

  const finished = (patch: Partial<ImportTask> = {}) =>
    makeTask({
      status: "completed",
      stage: null,
      resultItemId: "item-9",
      stageStats: [
        { stage: "transcribing", ms: 126_000 },
        {
          stage: "formatting",
          ms: 493_000,
          calls: 7,
          failedCalls: 2,
          promptTokens: 9_500,
          completionTokens: 57_900,
          models: ["qwen3.5-flash"],
        },
      ],
      ...patch,
    });

  it("行上常驻总耗时与最慢阶段：扫一眼就看得出哪条不对劲", () => {
    renderRow(finished());
    expect(screen.getByText("共 10:19")).toBeInTheDocument();
    expect(screen.getByText("· 最慢 文字稿排版 8:13")).toBeInTheDocument();
  });

  it("点摘要打开详情：明细不摊在行上，一屏几十条会被撑成一片", async () => {
    const onOpenDetail = vi.fn();
    renderRow(finished(), vi.fn(), onOpenDetail);
    expect(screen.queryByText("2:06")).not.toBeInTheDocument();

    await userEvent.click(screen.getByText("共 10:19"));
    expect(onOpenDetail).toHaveBeenCalled();
  });

  it("失败任务没有耗时摘要可点，详情入口仍要够得着", async () => {
    // 而它恰恰是最需要看清楚的那种，所以入口常驻在动作条上
    const onOpenDetail = vi.fn();
    renderRow(
      finished({ status: "failed", stageStats: null, error: "HTTP 522" }),
      vi.fn(),
      onOpenDetail,
    );
    expect(screen.queryByText(/^共 /)).not.toBeInTheDocument();

    await userEvent.click(screen.getByLabelText("任务详情"));
    expect(onOpenDetail).toHaveBeenCalled();
  });

  it("只有一个阶段时不说「最慢」：那等于把总耗时念第二遍", () => {
    renderRow(finished({ stageStats: [{ stage: "fetching", ms: 3_000 }] }));
    expect(screen.getByText("共 0:03")).toBeInTheDocument();
    expect(screen.queryByText(/最慢/)).not.toBeInTheDocument();
  });

  it("运行中的任务不显示耗时摘要：那时该回答的是「还活着吗」，不是「花在哪了」", () => {
    renderRow(finished({ status: "processing", stage: "formatting" }));
    expect(screen.queryByText(/^共 /)).not.toBeInTheDocument();
    // 已结算 2:06 + 8:13 再加当前阶段的 10 秒；createdAt 在 10 分钟前，
    // 但那是入队时刻，与「这条跑了多久」无关
    expect(screen.getByText("已用 10:29")).toBeInTheDocument();
  });

  it("老任务没有统计时整块不出现，不摆一个空壳", () => {
    renderRow(finished({ stageStats: null }));
    expect(screen.queryByText(/^共 /)).not.toBeInTheDocument();
  });
});
