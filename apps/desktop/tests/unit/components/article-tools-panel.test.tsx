import { useEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { ItemDetail } from "../../../src/renderer/components/library/ItemDetail";

const mocks = vi.hoisted(() => ({
  state: {} as any,
  mounted: vi.fn(),
  unmounted: vi.fn(),
  childEscape: vi.fn(),
}));
vi.mock("../../../src/renderer/stores/knowledge.store", () => ({
  useKnowledgeStore: (select: (state: any) => unknown) => select(mocks.state),
}));
vi.mock("../../../src/renderer/stores/ui.store", () => ({
  useUIStore: (select: (state: any) => unknown) =>
    select({ isFocusReadingMode: false }),
}));
vi.mock("react-i18next", () => ({
  useTranslation: () => ({ t: (_key: string, fallback: string) => fallback }),
}));
vi.mock("../../../src/renderer/components/library/ItemDetailHeader", () => ({
  ItemDetailHeader: ({ onToggleTools }: { onToggleTools: () => void }) => (
    <button onClick={onToggleTools}>文章信息与工具</button>
  ),
}));
vi.mock("../../../src/renderer/components/ask/ArticleAskReader", () => ({
  ArticleAskReader: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("../../../src/renderer/components/library/ContentPanel", () => ({
  ContentPanel: () => <main>可继续阅读的正文</main>,
}));
vi.mock(
  "../../../src/renderer/components/library/SourceCommentsContext",
  () => ({
    SourceCommentsProvider: ({ children }: { children: ReactNode }) => (
      <>{children}</>
    ),
  }),
);
vi.mock("../../../src/renderer/components/library/AiHandoffButton", () => ({
  AiHandoffButton: () => null,
}));
vi.mock("../../../src/renderer/components/library/AiOcrCard", () => ({
  AiOcrCard: () => null,
}));
vi.mock("../../../src/renderer/components/library/AiSummaryCard", () => ({
  AiSummaryCard: () => null,
}));
vi.mock("../../../src/renderer/components/library/SourceCommentsCard", () => ({
  SourceCommentsCard: () => null,
}));
vi.mock("../../../src/renderer/components/library/WebSourceVersions", () => ({
  WebSourceVersions: () => null,
}));
vi.mock("../../../src/renderer/components/library/IllustrationCard", () => ({
  IllustrationCard: function IllustrationFixture() {
    const [running, setRunning] = useState(false);
    useEffect(() => {
      mocks.mounted();
      return () => {
        mocks.unmounted();
      };
    }, []);
    return (
      <button onClick={() => setRunning(true)}>
        {running ? "配图任务进行中" : "开始模拟配图"}
      </button>
    );
  },
}));
vi.mock("../../../src/renderer/components/library/TagEditor", () => ({
  TagEditor: function TagFixture() {
    const [open, setOpen] = useState(false);
    return (
      <>
        <input aria-label="工具区本地输入" />
        <button onClick={() => setOpen(true)}>打开标签子面板</button>
        {open
          ? createPortal(
              <div
                role="dialog"
                aria-label="标签子面板"
                onKeyDown={(event) => {
                  if (event.key === "Escape") mocks.childEscape();
                }}
              >
                <input aria-label="标签子面板输入" />
                <button>标签子面板下一项</button>
              </div>,
              document.body,
            )
          : null}
      </>
    );
  },
}));

beforeEach(() => {
  vi.clearAllMocks();
  mocks.state = {
    selectedId: "media", detailLoading: false, detailError: null,
    selectedItem: {
      id: "media",
      title: "本地媒体",
      content: "[本地文件](local-video://sample.mp4)",
      itemType: "audio",
      tags: [],
      deletedAt: null,
      updatedAt: 1,
    } as KnowledgeItem,
    flushPendingSave: vi.fn().mockResolvedValue(true),
    updateSelected: vi.fn(),
  };
});

describe("文章工具的生命周期和子弹层", () => {
  it.each(["audio", "video"] as const)(
    "收起再展开不卸载 %s 或清空进行中的配图状态",
    (type) => {
      mocks.state.selectedItem.itemType = type;
      const { container } = render(<ItemDetail />);
      fireEvent.click(screen.getByRole("button", { name: "文章信息与工具" }));
      const panel = screen.getByRole("region", { name: "文章信息与工具" });
      const media = container.querySelector(type) as HTMLMediaElement;
      expect(media).not.toBeNull();
      // 仅验证真实媒体节点的生命周期与播放位置，不在 jsdom 中请求或播放媒体。
      media.currentTime = 17;
      fireEvent.click(screen.getByRole("button", { name: "开始模拟配图" }));
      fireEvent.click(screen.getByRole("button", { name: "收起文章工具" }));
      expect(panel).toHaveAttribute("hidden");
      expect(container.querySelector(type)).toBe(media);
      expect(media.isConnected).toBe(true);
      expect(media.currentTime).toBe(17);
      expect(mocks.unmounted).not.toHaveBeenCalled();
      expect(screen.getByText("可继续阅读的正文")).toBeVisible();
      fireEvent.click(screen.getByRole("button", { name: "文章信息与工具" }));
      expect(
        screen.getByRole("button", { name: "配图任务进行中" }),
      ).toBeVisible();
      expect(mocks.mounted).toHaveBeenCalledTimes(1);
    },
  );

  it("portal 中的 Esc 交给子面板，Tab 不会被工具区抢回", async () => {
    const user = userEvent.setup();
    render(<ItemDetail />);
    fireEvent.click(screen.getByRole("button", { name: "文章信息与工具" }));
    fireEvent.click(screen.getByRole("button", { name: "打开标签子面板" }));
    const childInput = screen.getByRole("textbox", { name: "标签子面板输入" });
    childInput.focus();
    fireEvent.keyDown(childInput, { key: "Escape" });
    expect(mocks.childEscape).toHaveBeenCalledTimes(1);
    expect(
      screen.getByRole("region", { name: "文章信息与工具" }),
    ).toBeVisible();
    expect(childInput).toHaveFocus();
    await user.tab();
    expect(
      screen.getByRole("button", { name: "标签子面板下一项" }),
    ).toHaveFocus();
  });

  it("工具区本地 Esc 收起工具，并将焦点还给原来的入口", () => {
    render(<ItemDetail />);
    const trigger = screen.getByRole("button", { name: "文章信息与工具" });
    trigger.focus();
    fireEvent.click(trigger);
    const panel = screen.getByRole("region", { name: "文章信息与工具" });
    const input = screen.getByRole("textbox", { name: "工具区本地输入" });
    input.focus();
    fireEvent.keyDown(input, { key: "Escape" });
    expect(panel).toHaveAttribute("hidden");
    expect(trigger).toHaveFocus();
    expect(mocks.unmounted).not.toHaveBeenCalled();
  });
});
