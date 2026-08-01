import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  KnowledgeCounts,
  KnowledgeFacetCountsQuery,
  KnowledgeItemListEntry,
  KnowledgeItemQuery,
  KnowledgeItemListResult,
} from "@guizhi/shared/types";

import {
  DEFAULT_PAGE_SIZE,
  useKnowledgeStore,
} from "../../../src/renderer/stores/knowledge.store";

function makeEntry(id: string): KnowledgeItemListEntry {
  return {
    id,
    title: id,
    snippet: "",
    itemType: "note",
    status: "active",
    collectionId: null,
    isFavorite: false,
    isPinned: false,
    deletedAt: null,
    createdAt: 0,
    updatedAt: 0,
    tags: [],
  };
}

function stubList(
  impl: (query: KnowledgeItemQuery) => Promise<KnowledgeItemListResult>,
): void {
  window.api.knowledge = {
    ...(window.api.knowledge ?? {}),
    list: impl,
    counts: async () => EMPTY_COUNTS,
  };
}

const EMPTY_COUNTS: KnowledgeCounts = {
  uncategorized: 0,
  all: 0,
  favorites: 0,
  archived: 0,
  trash: 0,
  byCollection: {},
  byTag: {},
  byPlatform: {},
};

describe("knowledge.store 服务端分页", () => {
  beforeEach(() => {
    useKnowledgeStore.setState({
      scope: "all",
      collectionId: null,
      tagId: null,
      searchQuery: "",
      entries: [],
      total: 0,
      page: 1,
      pageSize: DEFAULT_PAGE_SIZE,
      isLoading: false,
      selectionIds: [],
      selectionAnchorId: null,
    });
  });

  it("查询带 limit/offset，翻页取下一段", async () => {
    const queries: KnowledgeItemQuery[] = [];
    stubList(async (query) => {
      queries.push(query);
      return { entries: [], total: 250 };
    });

    await useKnowledgeStore.getState().fetchList();
    expect(queries[0]).toMatchObject({ limit: DEFAULT_PAGE_SIZE, offset: 0 });

    useKnowledgeStore.getState().setPage(3);
    await vi.waitFor(() => expect(queries).toHaveLength(2));
    expect(queries[1]).toMatchObject({ limit: DEFAULT_PAGE_SIZE, offset: 40 });
  });

  it("第 200 条之后的条目可达（旧实现的硬上限）", async () => {
    const queries: KnowledgeItemQuery[] = [];
    stubList(async (query) => {
      queries.push(query);
      return { entries: [makeEntry("item-201")], total: 250 };
    });

    useKnowledgeStore.setState({ page: 11 });
    await useKnowledgeStore.getState().fetchList();

    expect(queries[0]).toMatchObject({ offset: 200 });
    expect(useKnowledgeStore.getState().entries[0].id).toBe("item-201");
    // 总数来自服务端，不是当前页长度
    expect(useKnowledgeStore.getState().total).toBe(250);
  });

  it("改每页条数后回到第一页", async () => {
    const queries: KnowledgeItemQuery[] = [];
    stubList(async (query) => {
      queries.push(query);
      return { entries: [], total: 250 };
    });

    useKnowledgeStore.setState({ page: 5 });
    useKnowledgeStore.getState().setPageSize(50);
    await vi.waitFor(() => expect(queries).toHaveLength(1));

    expect(useKnowledgeStore.getState().page).toBe(1);
    expect(queries[0]).toMatchObject({ limit: 50, offset: 0 });
  });

  it("切换范围 / 搜索时重置页码", async () => {
    stubList(async () => ({ entries: [], total: 0 }));

    useKnowledgeStore.setState({ page: 7 });
    useKnowledgeStore.getState().setSearchQuery("关键词");
    expect(useKnowledgeStore.getState().page).toBe(1);

    useKnowledgeStore.setState({ page: 7 });
    useKnowledgeStore.getState().setScope("archived");
    expect(useKnowledgeStore.getState().page).toBe(1);
  });

  it("当前页越界时回退到最后一页重取", async () => {
    const queries: KnowledgeItemQuery[] = [];
    stubList(async (query) => {
      queries.push(query);
      // 越界页为空，回退后的最后一页有数据
      return query.offset && query.offset >= 100
        ? { entries: [], total: 45 }
        : { entries: [makeEntry("tail")], total: 45 };
    });

    useKnowledgeStore.setState({ page: 20 });
    await useKnowledgeStore.getState().fetchList();

    expect(useKnowledgeStore.getState().page).toBe(3);
    expect(useKnowledgeStore.getState().entries[0].id).toBe("tail");
  });
});

describe("knowledge.store 组合导航筛选", () => {
  beforeEach(() => {
    useKnowledgeStore.setState({
      scope: "all",
      collectionId: null,
      tagId: null,
      platform: null,
      page: 1,
      pageSize: DEFAULT_PAGE_SIZE,
      selectionIds: [],
      selectionAnchorId: null,
    });
  });

  it("范围与知识库、标签、平台可组合，切换范围不丢失 facet", async () => {
    const queries: KnowledgeItemQuery[] = [];
    stubList(async (query) => {
      queries.push(query);
      return { entries: [], total: 0 };
    });

    useKnowledgeStore.getState().selectCollection("col-1");
    await vi.waitFor(() => expect(queries).toHaveLength(1));
    expect(queries[0]).toMatchObject({ scope: "all", collectionId: "col-1" });
    expect(queries[0].platform).toBeUndefined();

    // 平台与知识库组合，才能表达“某个库里的抖音条目”。
    useKnowledgeStore.getState().selectPlatform("douyin");
    await vi.waitFor(() => expect(queries).toHaveLength(2));
    expect(queries[1]).toMatchObject({
      scope: "all",
      collectionId: "col-1",
      platform: "douyin",
    });
    expect(useKnowledgeStore.getState().collectionId).toBe("col-1");

    // 再加标签仍保留前两项。
    useKnowledgeStore.getState().selectTag("tag-1");
    await vi.waitFor(() => expect(queries).toHaveLength(3));
    expect(queries[2]).toMatchObject({
      scope: "all",
      collectionId: "col-1",
      tagId: "tag-1",
      platform: "douyin",
    });

    useKnowledgeStore.getState().setScope("archived");
    await vi.waitFor(() => expect(queries).toHaveLength(4));
    expect(queries[3]).toMatchObject({
      scope: "archived",
      collectionId: "col-1",
      tagId: "tag-1",
      platform: "douyin",
    });
    expect(useKnowledgeStore.getState().platform).toBe("douyin");
  });

  it("每次组合筛选都把当前上下文交给分面计数", async () => {
    const countQueries: KnowledgeFacetCountsQuery[] = [];
    stubList(async () => ({ entries: [], total: 0 }));
    window.api.knowledge = {
      ...window.api.knowledge,
      counts: async (query?: KnowledgeFacetCountsQuery) => {
        countQueries.push(query!);
        return EMPTY_COUNTS;
      },
    };

    useKnowledgeStore.getState().selectCollection("col-1");
    await vi.waitFor(() => expect(countQueries).toHaveLength(1));
    expect(countQueries[0]).toMatchObject({
      scope: "all",
      collectionId: "col-1",
    });

    useKnowledgeStore.getState().selectPlatform("bilibili");
    await vi.waitFor(() => expect(countQueries).toHaveLength(2));
    expect(countQueries[1]).toMatchObject({
      scope: "all",
      collectionId: "col-1",
      platform: "bilibili",
    });
  });

  it("切换平台时回到第一页并清空多选", async () => {
    stubList(async () => ({ entries: [], total: 0 }));

    useKnowledgeStore.setState({ page: 7, selectionIds: ["a", "b"] });
    useKnowledgeStore.getState().selectPlatform("v2ex");

    expect(useKnowledgeStore.getState().page).toBe(1);
    expect(useKnowledgeStore.getState().selectionIds).toEqual([]);
  });
});

describe("knowledge.store 并发请求守卫", () => {
  beforeEach(() => {
    useKnowledgeStore.setState({
      entries: [],
      total: 0,
      page: 1,
      pageSize: DEFAULT_PAGE_SIZE,
      isLoading: false,
      selectionIds: [],
    });
  });

  it("先发的慢请求后到时不覆盖后发请求的结果", async () => {
    const resolvers: ((result: KnowledgeItemListResult) => void)[] = [];
    stubList(
      () =>
        new Promise<KnowledgeItemListResult>((resolve) => {
          resolvers.push(resolve);
        }),
    );

    const first = useKnowledgeStore.getState().fetchList();
    const second = useKnowledgeStore.getState().fetchList();
    await vi.waitFor(() => expect(resolvers).toHaveLength(2));

    // 后发的先返回，随后先发的才返回——切换范围时的典型时序
    resolvers[1]({ entries: [makeEntry("latest")], total: 1 });
    resolvers[0]({ entries: [makeEntry("stale")], total: 99 });
    await Promise.all([first, second]);

    expect(useKnowledgeStore.getState().entries.map((e) => e.id)).toEqual([
      "latest",
    ]);
    expect(useKnowledgeStore.getState().total).toBe(1);
    // 过期请求也不该提前收起加载态
    expect(useKnowledgeStore.getState().isLoading).toBe(false);
  });

  it("旧的侧栏计数响应不能覆盖新筛选的数字", async () => {
    const resolvers: ((counts: KnowledgeCounts) => void)[] = [];
    stubList(async () => ({ entries: [], total: 0 }));
    window.api.knowledge = {
      ...window.api.knowledge,
      counts: () =>
        new Promise<KnowledgeCounts>((resolve) => {
          resolvers.push(resolve);
        }),
    };

    const first = useKnowledgeStore.getState().refreshCounts();
    const second = useKnowledgeStore.getState().refreshCounts();
    await vi.waitFor(() => expect(resolvers).toHaveLength(2));

    resolvers[1]({ ...EMPTY_COUNTS, byPlatform: { bilibili: 1 } });
    resolvers[0]({ ...EMPTY_COUNTS, byPlatform: { douyin: 9 } });
    await Promise.all([first, second]);

    expect(useKnowledgeStore.getState().counts?.byPlatform).toEqual({
      bilibili: 1,
    });
  });
});
