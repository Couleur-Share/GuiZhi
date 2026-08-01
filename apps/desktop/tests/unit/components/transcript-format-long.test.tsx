/**
 * 长文字稿的 AI 排版确认。
 *
 * 几万字的稿子要拆成几十次串行请求、耗时数分钟且真实计费，
 * 不该在用户点一下图标按钮后就默默花掉——确认框弹出之前一次 IPC 都不发。
 * 短稿不受影响，照旧点了就排。
 */
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ContentPanel } from "../../../src/renderer/components/library/ContentPanel";

function makeItem(transcript: string): KnowledgeItem {
  return {
    id: "item-1",
    title: "三小时的播客",
    content: "> 平台：bilibili",
    summary: null,
    transcript,
    itemType: "video",
    status: "active",
    collectionId: null,
    isFavorite: false,
    isPinned: false,
    sourceUri: "https://www.bilibili.com/video/BV1xx411c7mD",
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
  };
}

let formatTranscript: ReturnType<typeof vi.fn>;

beforeAll(async () => {
  await i18nReady;
  await changeLanguage("zh");
});

beforeEach(() => {
  formatTranscript = vi.fn().mockResolvedValue({ success: true });
  installWindowMocks({ api: { media: { formatTranscript } } });
});

/** 文字稿标签页默认不选中，先切过去才有排版按钮 */
async function openTranscriptTab(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /^文字稿/ }));
}

describe("长文字稿的 AI 排版", () => {
  it("超过上限先确认，确认后才带 allowLong 发请求", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <ContentPanel item={makeItem("字".repeat(50_001))} isTrashed={false} />
      </ToastProvider>,
    );

    await openTranscriptTab(user);
    await user.click(screen.getByRole("button", { name: "AI 排版" }));
    expect(formatTranscript).not.toHaveBeenCalled();

    const dialog = screen.getByRole("alertdialog");
    // 代价要写清楚：多少字、拆成几次请求
    expect(within(dialog).getByText(/50001 字/)).toBeInTheDocument();
    expect(within(dialog).getByText(/32 次请求/)).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "确认" }));
    await waitFor(() =>
      expect(formatTranscript).toHaveBeenCalledWith("item-1", {
        allowLong: true,
      }),
    );
  });

  it("取消就什么也不做", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <ContentPanel item={makeItem("字".repeat(50_001))} isTrashed={false} />
      </ToastProvider>,
    );

    await openTranscriptTab(user);
    await user.click(screen.getByRole("button", { name: "AI 排版" }));
    await user.click(
      within(screen.getByRole("alertdialog")).getByRole("button", {
        name: "取消",
      }),
    );

    expect(formatTranscript).not.toHaveBeenCalled();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("上限以内的文字稿不弹确认，直接排版", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <ContentPanel item={makeItem("一段不算长的文字稿")} isTrashed={false} />
      </ToastProvider>,
    );

    await openTranscriptTab(user);
    await user.click(screen.getByRole("button", { name: "AI 排版" }));

    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await waitFor(() =>
      expect(formatTranscript).toHaveBeenCalledWith("item-1", {
        allowLong: false,
      }),
    );
  });
});
