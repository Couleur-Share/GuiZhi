import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { ImportTask } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ImportTaskDetailModal } from "../../../src/renderer/components/imports/ImportTaskDetailModal";

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
      { stage: "saving", ms: 100 },
    ],
    createdAt: 1_800_000_000_000,
    updatedAt: 1_800_000_620_000,
    ...patch,
  };
}

function renderModal(task: ImportTask, onOpenItem = vi.fn()) {
  render(
    <ToastProvider>
      <ImportTaskDetailModal
        task={task}
        isOpen
        onClose={vi.fn()}
        onOpenItem={onOpenItem}
      />
    </ToastProvider>,
  );
  return onOpenItem;
}

/** userEvent.setup() 会用自己的剪贴板 stub 换掉全局 mock，所以直接读回来 */
function copiedText(): Promise<string> {
  return navigator.clipboard.readText();
}

describe("导入任务详情弹窗", () => {
  beforeAll(async () => {
    installWindowMocks();
    await i18nReady;
    await changeLanguage("zh");
    // copyTextToClipboard 只在安全上下文里走 clipboard API，jsdom 默认不是
    Object.defineProperty(globalThis, "isSecureContext", {
      configurable: true,
      value: true,
    });
  });

  beforeEach(() => {
    installWindowMocks();
  });

  it("副标题是品牌 logo 加平台名，不是一行域名", () => {
    renderModal(makeTask());
    // v.douyin.com 得先读懂才知道是抖音
    expect(screen.getByText("抖音")).toBeInTheDocument();
    expect(screen.queryByText("v.douyin.com")).not.toBeInTheDocument();
    // 完整链接仍在「来源」那一格，信息没丢
    expect(screen.getByText("https://v.douyin.com/abcdefg/")).toBeInTheDocument();
  });

  it("通用网页给主机名而不是「网页」：后者什么都没说", () => {
    renderModal(
      makeTask({ sourceInput: "https://blog.example.com/posts/42" }),
    );
    expect(screen.getByText("blog.example.com")).toBeInTheDocument();
    expect(screen.queryByText("网页")).not.toBeInTheDocument();
  });

  it("默认读数是总耗时，悬停某段就地换成那一段", async () => {
    const user = userEvent.setup();
    renderModal(makeTask());
    expect(screen.getByText("共 10:19")).toBeInTheDocument();

    await user.hover(screen.getByText("文字稿排版"));
    // 换成当前段的名称与「耗时 · 占比」，而不是撑出一行或等 320ms 的气泡
    expect(screen.queryByText("共 10:19")).not.toBeInTheDocument();
    expect(screen.getByText("8:13 · 80%")).toBeInTheDocument();
  });

  it("逐阶段给出耗时、占比、调用次数与 token", () => {
    renderModal(makeTask());

    expect(screen.getByText("2:06")).toBeInTheDocument();
    expect(screen.getByText("8:13")).toBeInTheDocument();
    // 占比才是「时间花在哪」的直接答案，光给绝对值还得自己心算
    expect(screen.getByText("20%")).toBeInTheDocument();
    expect(screen.getByText("80%")).toBeInTheDocument();
    expect(
      screen.getByText(/7 次调用 · 2 次失败 · 67400 token · qwen3\.5-flash/),
    ).toBeInTheDocument();
  });

  it("亚秒阶段折成一行，点它展开全部", async () => {
    renderModal(makeTask());
    expect(screen.queryByText("抓取中")).not.toBeInTheDocument();

    // 折叠是为了扫视时的信噪比，但排查时「让我看全部」是正当需求
    await userEvent.click(screen.getByText("另有 2 个阶段不足 1 秒"));
    expect(screen.getByText("抓取中")).toBeInTheDocument();
    expect(screen.getByText("入库中")).toBeInTheDocument();
    expect(screen.queryByText(/另有/)).not.toBeInTheDocument();
  });

  it("没有阶段统计时说明白，而不是留一块空白", () => {
    renderModal(makeTask({ stageStats: null, status: "canceled" }));
    expect(screen.getByText("本次没有记录阶段耗时")).toBeInTheDocument();
    expect(screen.getByText("已取消")).toBeInTheDocument();
  });

  it("报错与缺失提示各占一块", () => {
    renderModal(
      makeTask({
        status: "completed",
        warning: "文字稿生成失败：本地转写服务启动失败",
      }),
    );
    expect(screen.getByText("缺失提示")).toBeInTheDocument();
    expect(
      screen.getByText("文字稿生成失败：本地转写服务启动失败"),
    ).toBeInTheDocument();
    expect(screen.queryByText("报错")).not.toBeInTheDocument();
  });

  it("复制诊断信息：整块 Markdown 进剪贴板，含全部阶段", async () => {
    const user = userEvent.setup();
    renderModal(makeTask());

    await user.click(screen.getByRole("button", { name: "复制诊断信息" }));

    const copied = await copiedText();
    expect(copied).toContain("# 导入任务诊断");
    expect(copied).toContain("- 来源：https://v.douyin.com/abcdefg/");
    // 界面上折起来的亚秒阶段，诊断文本里必须在——少一行就是少一条线索
    expect(copied).toContain("| 抓取中 |");
    expect(copied).toContain("| 入库中 |");
    expect(copied).toContain("- 应用：GuiZhi 0.0.0-test · win32");
    expect(screen.getByText("已复制诊断信息，可直接粘贴反馈")).toBeInTheDocument();
  });

  it("完成的任务给「打开条目」，取消的不给", async () => {
    const onOpenItem = renderModal(makeTask());
    await userEvent.click(screen.getByRole("button", { name: "打开条目" }));
    expect(onOpenItem).toHaveBeenCalledWith("item-9");
  });
});
