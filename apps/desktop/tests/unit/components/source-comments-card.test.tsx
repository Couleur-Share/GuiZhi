import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { SourceCommentsCard } from "../../../src/renderer/components/library/SourceCommentsCard";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import type { KnowledgeItem } from "@guizhi/shared/types";

const item: KnowledgeItem = {
  id: "item-1",
  title: "测试",
  content: "正文",
  itemType: "webpage",
  status: "active",
  isFavorite: false,
  isPinned: false,
  sourceUri: "https://www.xiaohongshu.com/explore/note-1",
  createdAt: 1,
  updatedAt: 1,
  tags: [],
};

describe("来源评论", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    installWindowMocks({
      api: {
        platformCapture: {
          getStatuses: vi.fn(),
          login: vi.fn(),
          cancelLogin: vi.fn(),
          logout: vi.fn(),
          clearAllData: vi.fn(),
          discoverCreator: vi.fn(),
          search: vi.fn(),
          cancelDiscovery: vi.fn(),
          refreshComments: vi.fn(),
          listComments: vi.fn().mockResolvedValue([
            {
              id: "comment-1",
              itemId: "item-1",
              platform: "xiaohongshu",
              externalId: "c1",
              authorName: "读者",
              content: "<script>alert(1)</script> **不是粗体**",
              likeCount: 3,
              publishedAt: null,
              capturedAt: 1,
            },
          ]),
        },
      },
    });
  });

  it("默认折叠，展开后按不可信纯文本展示", async () => {
    const { container } = render(<SourceCommentsCard item={item} />);
    expect(screen.getByRole("button", { name: "20" })).toHaveClass("h-7");
    expect(screen.getByRole("button", { name: "20" })).not.toHaveClass("h-10");
    expect(screen.queryByText(/不是粗体/)).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /来源评论/ }));
    expect(
      await screen.findByText("<script>alert(1)</script> **不是粗体**"),
    ).toBeInTheDocument();
    expect(container.querySelector("script")).toBeNull();
    expect(container.querySelector("strong")).toBeNull();
  });

  it("首次补采失败时自动展开并显示原因", async () => {
    vi.mocked(window.api.platformCapture.refreshComments).mockRejectedValueOnce(
      new Error("平台登录状态已失效，请重新登录"),
    );

    render(<SourceCommentsCard item={item} />);
    await userEvent.click(screen.getByRole("button", { name: "补采" }));

    expect(
      await screen.findByText("平台登录状态已失效，请重新登录"),
    ).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /来源评论/ })).toHaveAttribute(
        "aria-expanded",
        "true",
      ),
    );
  });

  it("LINUX DO 条目不显示与讨论区重复的来源评论", () => {
    const { container } = render(
      <SourceCommentsCard
        item={{
          ...item,
          sourceUri: "https://linux.do/t/topic/123",
        }}
      />,
    );

    expect(container).toBeEmptyDOMElement();
    expect(window.api.platformCapture.refreshComments).not.toHaveBeenCalled();
  });
});
