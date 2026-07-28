import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it } from "vitest";
import type { KnowledgeCounts, Tag } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { SidebarLibraryPanel } from "../../../src/renderer/components/library/SidebarLibraryPanel";
import { useKnowledgeStore } from "../../../src/renderer/stores/knowledge.store";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";
import { useTagStore } from "../../../src/renderer/stores/tag.store";

function makeTag(id: string, name: string): Tag {
  return {
    id,
    name,
    colorKey: "gray",
    createdAt: 0,
    updatedAt: 0,
  };
}

const TAGS = [makeTag("tag-read", "读书"), makeTag("tag-mind", "心理情感")];

function makeCounts(byTag: Record<string, number>): KnowledgeCounts {
  return {
    uncategorized: 0,
    all: 0,
    favorites: 0,
    archived: 0,
    trash: 0,
    byCollection: {},
    byTag,
    byPlatform: {},
  };
}

function renderPanel(byTag: Record<string, number>, tagId: string | null) {
  useTagStore.setState({ tags: TAGS });
  useCollectionStore.setState({ collections: [] });
  useKnowledgeStore.setState({
    scope: "all",
    collectionId: null,
    tagId,
    platform: null,
    counts: makeCounts(byTag),
  });
  return render(
    <ToastProvider>
      <SidebarLibraryPanel />
    </ToastProvider>,
  );
}

/**
 * 侧栏「标签」是导航轴，不是标签清单：一行点进去必然是空列表，
 * 它就不该占着位置。标签本身不删，所以这些断言只管看得见看不见。
 */
describe("侧栏标签分区的可见性", () => {
  beforeAll(async () => {
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    installWindowMocks();
  });

  it("摘光引用的标签不再占一行，仍有条目的照常列出", () => {
    renderPanel({ "tag-read": 3 }, null);

    expect(screen.getByRole("button", { name: "读书" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "心理情感" })).toBeNull();
  });

  it("正筛着的标签被摘空后仍留在原地，不让用户卡在看不见的筛选里", () => {
    renderPanel({ "tag-read": 3 }, "tag-mind");

    // 藏掉的话界面上就是一个空列表配一排没有高亮的行，
    // 用户既不知道自己在哪，也不知道该点什么回去
    expect(
      screen.getByRole("button", { name: "心理情感" }),
    ).toBeInTheDocument();
  });

  it("所有标签都没条目时回到空态文案，而不是列一排 0", () => {
    renderPanel({}, null);

    expect(screen.queryByRole("button", { name: "读书" })).toBeNull();
    expect(screen.queryByRole("button", { name: "心理情感" })).toBeNull();
    expect(screen.getByText("在条目详情中添加标签")).toBeInTheDocument();
  });

  it("开关能把藏起来的标签露出来，行菜单随之够得着", async () => {
    const user = userEvent.setup();
    renderPanel({ "tag-read": 3 }, null);

    await user.click(screen.getByRole("button", { name: /显示 1 个未使用的标签/ }));

    // 露出来的目的就是删：行尾的「更多」按钮是删除标签的唯一入口
    expect(
      screen.getByRole("button", { name: "心理情感" }),
    ).toBeInTheDocument();
    expect(
      screen.getAllByRole("button", { name: "更多操作" }).length,
    ).toBeGreaterThan(1);
  });

  it("没有未使用的标签时不摆这个开关", () => {
    renderPanel({ "tag-read": 3, "tag-mind": 1 }, null);

    expect(screen.queryByRole("button", { name: /未使用的标签/ })).toBeNull();
  });
});
