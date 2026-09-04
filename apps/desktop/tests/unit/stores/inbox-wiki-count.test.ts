import { beforeEach, describe, expect, it, vi } from "vitest";
import { installWindowMocks } from "../../helpers/window";

let wikiPending = 0;
const { classifyInboxItems } = vi.hoisted(() => ({
  classifyInboxItems: vi.fn(),
}));

vi.mock("../../../src/renderer/services/knowledge-ai/wiki-compile", () => ({
  countPendingWikiItems: vi.fn(async () => wikiPending),
}));
vi.mock(
  "../../../src/renderer/services/knowledge-ai/classify-collections",
  () => ({
    classifyInboxItems,
  }),
);

import { useInboxStore } from "../../../src/renderer/stores/inbox.store";

const EMPTY_COUNTS = {
  "review-required": 0,
  unclassified: 0,
  "import-issue": 0,
  "discovery-candidate": 0,
  "semantic-pending": 0,
  "wiki-pending": 0,
};

describe("inbox.store Wiki 待编译计数", () => {
  beforeEach(() => {
    wikiPending = 0;
    useInboxStore.setState({
      items: [],
      counts: { ...EMPTY_COUNTS },
      total: 0,
      selectionIds: [],
      isLoading: false,
      loadError: null,
    });
  });

  it("以编译器的精确计数替换主进程旧聚合值", async () => {
    installWindowMocks({
      api: {
        inbox: {
          list: vi.fn().mockResolvedValue({
            items: [
              {
                kind: "wiki-pending",
                id: "aggregate:wiki",
                count: 47,
                createdAt: 1,
              },
            ],
            counts: { ...EMPTY_COUNTS, "wiki-pending": 1 },
            total: 1,
          }),
        },
      },
    });

    await useInboxStore.getState().refresh();
    expect(useInboxStore.getState()).toMatchObject({
      items: [],
      counts: { "wiki-pending": 0 },
      total: 0,
    });

    wikiPending = 3;
    await useInboxStore.getState().refresh();
    expect(useInboxStore.getState()).toMatchObject({
      items: [
        {
          kind: "wiki-pending",
          id: "aggregate:wiki",
          count: 3,
        },
      ],
      counts: { "wiki-pending": 1 },
      total: 1,
    });
  });

  it("读取有界素材、生成计划后才原子应用并刷新处理中心", async () => {
    const sources = [{ itemId: "item-1", title: "条目", excerpt: "正文" }];
    const assignments = [{ itemId: "item-1", collectionName: "编程开发" }];
    const applyResult = {
      classified: 1,
      skipped: 0,
      createdCollectionNames: [],
    };
    classifyInboxItems.mockResolvedValue(assignments);
    const aiClassificationSources = vi.fn().mockResolvedValue(sources);
    const applyAiClassification = vi.fn().mockResolvedValue(applyResult);
    installWindowMocks({
      api: {
        inbox: {
          list: vi.fn().mockResolvedValue({
            items: [],
            counts: { ...EMPTY_COUNTS },
            total: 0,
          }),
          aiClassificationSources,
          applyAiClassification,
        },
      },
    });
    const onProgress = vi.fn();

    await expect(
      useInboxStore
        .getState()
        .smartClassify(["item-1"], ["编程开发"], { onProgress }),
    ).resolves.toEqual(applyResult);
    expect(aiClassificationSources).toHaveBeenCalledWith(["item-1"]);
    expect(classifyInboxItems).toHaveBeenCalledWith(sources, ["编程开发"], {
      onProgress,
    });
    expect(applyAiClassification).toHaveBeenCalledWith({ assignments });
    expect(useInboxStore.getState().items).toEqual([]);
  });
});
