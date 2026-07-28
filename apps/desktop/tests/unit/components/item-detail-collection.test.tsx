import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { Collection, KnowledgeItem } from "@guizhi/shared/types";
import { installWindowMocks } from "../../helpers/window";
import { changeLanguage, i18nReady } from "../../../src/renderer/i18n";
import { ToastProvider } from "../../../src/renderer/components/ui/Toast";
import { ItemDetailHeader } from "../../../src/renderer/components/library/ItemDetailHeader";
import {
  __resetPendingSaves,
  useKnowledgeStore,
} from "../../../src/renderer/stores/knowledge.store";
import { useCollectionStore } from "../../../src/renderer/stores/collection.store";

const COLLECTIONS: Collection[] = [
  {
    id: "col-1",
    name: "心理情感",
    icon: "💗",
    sortOrder: 0,
    createdAt: 0,
    updatedAt: 0,
  },
];

function makeItem(): KnowledgeItem {
  return {
    id: "item-1",
    title: "情感冷漠型伴侣的特征与关系消耗机制",
    content: "正文",
    summary: null,
    transcript: null,
    itemType: "video",
    status: "active",
    collectionId: null,
    isFavorite: false,
    isPinned: false,
    sourceUri: null,
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
  };
}

describe("详情页的分类 chip", () => {
  let bulkUpdateCalls: Array<{ ids: string[]; patch: unknown }>;
  let countsCalls: number;

  beforeAll(async () => {
    installWindowMocks();
    await i18nReady;
    await changeLanguage("zh");
  });

  beforeEach(() => {
    __resetPendingSaves();
    bulkUpdateCalls = [];
    countsCalls = 0;
    installWindowMocks();
    window.api.knowledge = {
      bulkUpdate: vi.fn(async (ids: string[], patch: unknown) => {
        bulkUpdateCalls.push({ ids, patch });
        return ids.length;
      }),
      update: vi.fn(),
      list: vi.fn(async () => ({ entries: [], total: 0 })),
      get: vi.fn(async () => ({ ...makeItem(), collectionId: "col-1" })),
      counts: vi.fn(async () => {
        countsCalls += 1;
        return {
          uncategorized: 0,
          all: 1,
          favorites: 0,
          archived: 0,
          trash: 0,
          byCollection: { "col-1": 1 },
          byTag: {},
          byPlatform: {},
        };
      }),
    };
    useCollectionStore.setState({ collections: COLLECTIONS });
    useKnowledgeStore.setState({
      entries: [],
      selectedId: "item-1",
      selectedItem: makeItem(),
      counts: null,
    });
  });

  it("选中知识库后立即落盘，并重取侧栏计数", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <ItemDetailHeader item={makeItem()} isTrashed={false} />
      </ToastProvider>,
    );

    await user.click(screen.getByRole("button", { name: /未分类/ }));
    await user.click(screen.getByRole("button", { name: "心理情感" }));

    // 混进正文的防抖保存队列时，这里要等 800ms 才落盘、且永远不刷新计数
    expect(bulkUpdateCalls).toEqual([
      { ids: ["item-1"], patch: { collectionId: "col-1" } },
    ]);
    expect(countsCalls).toBeGreaterThan(0);
    expect(useKnowledgeStore.getState().counts?.byCollection["col-1"]).toBe(1);
  });

  it("移回未分类同样立即落盘", async () => {
    const user = userEvent.setup();
    render(
      <ToastProvider>
        <ItemDetailHeader
          item={{ ...makeItem(), collectionId: "col-1" }}
          isTrashed={false}
        />
      </ToastProvider>,
    );

    await user.click(screen.getByRole("button", { name: /心理情感/ }));
    await user.click(screen.getByRole("button", { name: "未分类" }));

    expect(bulkUpdateCalls).toEqual([
      { ids: ["item-1"], patch: { collectionId: null } },
    ]);
    expect(countsCalls).toBeGreaterThan(0);
  });
});
