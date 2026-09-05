import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ItemListToolbar } from "../../../src/renderer/components/library/ItemListToolbar";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";
import { useTagStore } from "../../../src/renderer/stores/tag.store";

describe("列表工具栏的组合筛选入口", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    useKnowledgeStore.setState({
      scope: "all",
      collectionId: null,
      tagId: null,
      platform: null,
      searchQuery: "",
      total: 3,
    });
    useCollectionStore.setState({ collections: [] });
    useTagStore.setState({
      tags: [
        {
          id: "tag-1",
          name: "家庭网络",
          colorKey: "gray",
          createdAt: 0,
          updatedAt: 0,
        },
      ],
    });
    window.api.knowledge = {
      list: vi.fn().mockResolvedValue({ entries: [], total: 3 }),
      counts: vi.fn().mockResolvedValue(null),
    };
  });

  it.each([
    {},
    { collectionId: "col-1" },
    { tagId: "tag-1" },
    { platform: "douyin" },
  ])("零或单条件不重复展示筛选入口：%j", (filters) => {
    useKnowledgeStore.setState(filters);
    render(<ItemListToolbar />);
    expect(screen.queryByRole("button", { name: /组合筛选/ })).toBeNull();
    expect(screen.queryByText("家庭网络")).toBeNull();
    expect(screen.getByText("共 3 个")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /排序/ })).toBeInTheDocument();
  });

  it("多条件集中展示，移除一项保留其他条件与范围、搜索词", async () => {
    const user = userEvent.setup();
    useKnowledgeStore.setState({
      collectionId: "col-1",
      tagId: "tag-1",
      platform: "douyin",
      scope: "favorites",
      searchQuery: "路由器",
    });
    render(<ItemListToolbar />);
    await user.click(screen.getByRole("button", { name: "组合筛选 · 3" }));
    const panel = screen.getByRole("dialog", { name: "当前筛选" });
    expect(within(panel).getByText("抖音")).toBeInTheDocument();
    await user.click(
      within(panel).getByRole("button", { name: "取消标签筛选：家庭网络" }),
    );
    expect(useKnowledgeStore.getState()).toMatchObject({
      collectionId: "col-1",
      tagId: null,
      platform: "douyin",
      scope: "favorites",
      searchQuery: "路由器",
    });
    expect(screen.getByRole("button", { name: "组合筛选 · 2" })).toHaveFocus();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(window.api.knowledge.list).toHaveBeenLastCalledWith(
      expect.objectContaining({
        tagId: undefined,
        collectionId: "col-1",
        platform: "douyin",
      }),
    );

    await user.click(screen.getByRole("button", { name: "组合筛选 · 2" }));
    await user.click(
      screen.getByRole("button", { name: "取消平台筛选：抖音" }),
    );
    expect(screen.queryByRole("button", { name: /组合筛选/ })).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.querySelector(".library-list-toolbar")).toHaveFocus();
  });

  it("清空只取消细分条件，保留当前范围和搜索", async () => {
    const user = userEvent.setup();
    useKnowledgeStore.setState({
      collectionId: "col-1",
      tagId: "tag-1",
      scope: "favorites",
      searchQuery: "路由器",
    });
    render(<ItemListToolbar />);
    await user.click(screen.getByRole("button", { name: "组合筛选 · 2" }));
    await user.click(screen.getByRole("button", { name: "清空筛选" }));
    expect(useKnowledgeStore.getState()).toMatchObject({
      collectionId: null,
      tagId: null,
      platform: null,
      scope: "favorites",
      searchQuery: "路由器",
    });
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("支持 Escape 返回入口与点击外部关闭", async () => {
    const user = userEvent.setup();
    useKnowledgeStore.setState({ collectionId: "col-1", tagId: "tag-1" });
    render(<ItemListToolbar />);
    const trigger = screen.getByRole("button", { name: "组合筛选 · 2" });
    await user.click(trigger);
    expect(screen.getByRole("button", { name: "清空筛选" })).toHaveFocus();
    await user.keyboard("{Escape}");
    expect(trigger).toHaveFocus();
    expect(trigger).toHaveAttribute("aria-expanded", "false");
    await user.click(trigger);
    await user.click(screen.getByText("共 3 个"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });
});
