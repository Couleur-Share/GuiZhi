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
    createdAt: NOW - 600_000,
    updatedAt: NOW - 10_000,
    ...patch,
  };
}

function renderRow(task: ImportTask, onOpenItem = vi.fn()) {
  render(
    <ImportTaskRow
      task={task}
      now={NOW}
      isChecked={false}
      hasSelection={false}
      onToggle={vi.fn()}
      onOpenItem={onOpenItem}
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
});
