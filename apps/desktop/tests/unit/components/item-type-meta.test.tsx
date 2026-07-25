import { describe, expect, it } from "vitest";
import {
  ITEM_STATUS_META,
  ITEM_TYPE_META,
  getItemStatusMeta,
  getItemTypeMeta,
} from "../../../src/renderer/components/library/type-meta";

/**
 * 这两个值直接来自数据库，可能是本版本还不认识的取值——
 * v0.6.0 读到 v0.7.0 写入的 forum 条目时，下标取值拿到 undefined，
 * 再读 labelKey 就把整个知识库列表连同整个界面一起打掉了。
 */
describe("条目类型查表", () => {
  it("已知类型返回自己的元数据", () => {
    expect(getItemTypeMeta("forum")).toBe(ITEM_TYPE_META.forum);
    expect(getItemTypeMeta("note")).toBe(ITEM_TYPE_META.note);
  });

  it("未知类型退回兜底，而不是 undefined", () => {
    const meta = getItemTypeMeta("podcast");
    expect(meta.labelKey).toBe("library.typeUnknown");
    expect(meta.fallback).toBe("未知类型");
    expect(meta.icon).toBeTruthy();
  });

  it("原型链上的键不算已知类型", () => {
    expect(getItemTypeMeta("toString").labelKey).toBe("library.typeUnknown");
    expect(getItemTypeMeta("constructor").labelKey).toBe("library.typeUnknown");
  });
});

describe("条目状态查表", () => {
  it("已知状态返回自己的元数据", () => {
    expect(getItemStatusMeta("archived")).toBe(ITEM_STATUS_META.archived);
  });

  it("未知状态退回兜底", () => {
    const meta = getItemStatusMeta("pending");
    expect(meta.labelKey).toBe("library.statusUnknown");
    expect(meta.fallback).toBe("未知状态");
  });
});
