import { createRef } from "react";
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import type {
  ThemedReadingTask,
  ThemedReadingVersion,
} from "@guizhi/shared/types/themed-reading";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ThemedReadingPane } from "../../../src/renderer/components/themed-reading/ThemedReadingPane";
import { ThemedReadingStatus } from "../../../src/renderer/components/themed-reading/ThemedReadingStatus";
import { ReconstructionReader } from "../../../src/renderer/components/themed-reading/ReconstructionReader";

const item = {
  id: "a",
  title: "啤酒文章",
  content: "# 原文标题\n\n原文仍可阅读",
  itemType: "note",
} as KnowledgeItem;
const page = {
  id: "page",
  itemId: "a",
  sourceKind: "body",
  assets: [],
  warnings: [],
  formatVersion: 2,
  options: { style: "", generateImages: false, maxImages: 3 },
} as ThemedReadingVersion;
const failed = {
  id: "job",
  itemId: "a",
  sourceKind: "body",
  state: "failed",
  stage: "research",
  completed: 0,
  total: 1,
  error: "需要补查完整标准条文",
  usage: {
    textCalls: 5,
    imageCalls: 0,
    imagesSaved: 0,
    searchCalls: 3,
    pagesRead: 3,
  },
} as ThemedReadingTask;

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});
beforeEach(() => {
  installWindowMocks({
    api: {
      themedReading: {
        get: vi
          .fn()
          .mockResolvedValue({
            success: true,
            page,
            task: failed,
            models: { text: "text", image: null },
            document: "<p>已有阅读版本</p>",
          }),
        onProgress: vi.fn().mockReturnValue(() => {}),
        resume: vi.fn().mockResolvedValue({ success: true }),
      },
    },
  });
});

describe("安静阅读与任务恢复", () => {
  it("原文阅读不显示失败任务、过期提示或自动打开配置", async () => {
    render(
      <ToastProvider>
        <ThemedReadingPane
          item={item}
          sourceKind="body"
          showPage={false}
          original={<p>可阅读的原文</p>}
          findQuery=""
          findIndex={0}
          onFindCount={vi.fn()}
          onFindOpen={vi.fn()}
        />
      </ToastProvider>,
    );
    await waitFor(() =>
      expect(window.api.themedReading.get).toHaveBeenCalled(),
    );
    expect(screen.getByText("可阅读的原文")).toBeVisible();
    expect(screen.queryByTestId("reading-task-status")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "阅读页设置" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("旧成品保留阅读，失败只显示一个补查入口且详细原因默认收起", async () => {
    render(
      <ToastProvider>
        <ThemedReadingPane
          item={item}
          sourceKind="body"
          findQuery=""
          findIndex={0}
          onFindCount={vi.fn()}
          onFindOpen={vi.fn()}
        />
      </ToastProvider>,
    );
    expect(await screen.findByText("当前显示上次生成的版本。")).toBeVisible();
    expect(screen.getByLabelText("AI 重构阅读")).toBeInTheDocument();
    const resume = screen.getAllByRole("button", { name: "补查资料并继续" });
    expect(resume).toHaveLength(1);
    expect(
      screen.queryByRole("button", { name: "继续未完成部分" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "重试联网" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText(failed.error!)).not.toBeVisible();
    expect(
      screen.queryByRole("button", { name: "导出 HTML" }),
    ).not.toBeInTheDocument();
    fireEvent.click(resume[0]);
    await waitFor(() =>
      expect(window.api.themedReading.resume).toHaveBeenCalledWith("job"),
    );
    fireEvent.click(screen.getByText("查看生成详情"));
    expect(screen.getByText(failed.error!)).toBeVisible();
    expect(screen.getByRole("button", { name: "复制详细原因" })).toBeEnabled();
  });

  it("补查已达上限不再提供会重复计费的恢复入口", () => {
    const resume = vi.fn(),
      offline = vi.fn();
    render(
      <ThemedReadingStatus
        task={{
          ...failed,
          error: "补充查证仍未完成，已达到本次自动补查上限：标准条文缺失",
        }}
        onResume={resume}
        onCancel={vi.fn()}
        onOffline={offline}
      />,
    );
    expect(
      screen.queryByRole("button", { name: "补查资料并继续" }),
    ).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "不联网继续生成" }));
    expect(offline).toHaveBeenCalledOnce();
    expect(resume).not.toHaveBeenCalled();
  });

  it("版本、编辑与阅读页设置集中到父工具栏，阅读区不再新增工具栏", async () => {
    const target = document.createElement("div");
    document.body.appendChild(target);
    const select = vi.fn();
    const view = render(
      <ToastProvider>
        <ReconstructionReader
          item={item}
          toolbarTarget={target}
          mode={{
            sourceKind: "body",
            available: true,
            active: true,
            editing: false,
            select,
            toggle: vi.fn(),
            startEditing: vi.fn(),
            finishEditing: vi.fn(),
          }}
          scrollRef={createRef<HTMLDivElement>()}
          findQuery=""
          findIndex={0}
          onFindCount={vi.fn()}
          onFindOpen={vi.fn()}
          onModeChange={vi.fn()}
        />
      </ToastProvider>,
    );
    await within(target).findByRole("button", { name: "阅读页设置" });
    expect(
      within(target).getByRole("button", { name: "原文", exact: true }),
    ).toBeVisible();
    expect(
      within(target).getByRole("button", { name: "编辑原文" }),
    ).toBeVisible();
    expect(
      within(target).getByRole("button", { name: "目录", exact: true }),
    ).toBeVisible();
    expect(
      within(screen.getByTestId("reconstruction-reader")).queryByTestId(
        "reading-version-actions",
      ),
    ).not.toBeInTheDocument();
    fireEvent.click(
      within(target).getByRole("button", { name: "原文", exact: true }),
    );
    expect(select).toHaveBeenCalledWith(false);
    view.unmount();
    target.remove();
  });
});
