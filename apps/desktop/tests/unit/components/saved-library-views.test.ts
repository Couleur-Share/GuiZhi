import { beforeEach, describe, expect, it } from "vitest";
import {
  createSavedLibraryView,
  readSavedLibraryViews,
  writeSavedLibraryViews,
} from "../../../src/renderer/components/library/saved-library-views";

describe("智能视图持久化", () => {
  beforeEach(() => localStorage.clear());

  it("保存并恢复结构化筛选，不携带临时搜索词", () => {
    const view = createSavedLibraryView("工作收藏", {
      scope: "favorites",
      collectionId: "work",
      tagId: "important",
      platform: "bilibili",
    });
    expect(view).not.toBeNull();
    writeSavedLibraryViews([view!]);

    expect(readSavedLibraryViews()).toMatchObject([
      {
        name: "工作收藏",
        scope: "favorites",
        collectionId: "work",
        tagId: "important",
        platform: "bilibili",
      },
    ]);
  });

  it("损坏或不合法的本地数据静默丢弃", () => {
    localStorage.setItem("guizhi-library-smart-views-v1", "not json");
    expect(readSavedLibraryViews()).toEqual([]);
    localStorage.setItem(
      "guizhi-library-smart-views-v1",
      JSON.stringify([{ id: "x", name: "坏数据", scope: "made-up" }]),
    );
    expect(readSavedLibraryViews()).toEqual([]);
  });
});
