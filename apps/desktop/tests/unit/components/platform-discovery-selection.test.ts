import { describe, expect, it } from "vitest";
import { updateDiscoverySelection } from "../../../src/renderer/components/imports/PlatformDiscoveryPanel";

describe("平台发现选择规则", () => {
  it("默认集合为空，已导入作品不可选", () => {
    expect(updateDiscoverySelection([], { externalId: "a", importedItemId: "item-1" })).toEqual({
      ids: [],
      reachedLimit: false,
    });
  });

  it("最多选择 50 条，再选不改变集合", () => {
    const current = Array.from({ length: 50 }, (_, index) => String(index));
    const result = updateDiscoverySelection(current, { externalId: "51" });
    expect(result.ids).toBe(current);
    expect(result.reachedLimit).toBe(true);
  });
});
