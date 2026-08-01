import { beforeEach, describe, expect, it } from "vitest";
import {
  loadContentReadingMemory,
  patchContentReadingMemory,
  saveContentReadingMemory,
} from "../../../src/renderer/components/library/reading-memory";

describe("reading-memory", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("读写与 patch 合并 scrollTopByTab", () => {
    saveContentReadingMemory("a", {
      tab: "replies",
      scrollTopByTab: { replies: 120 },
      repliesQuery: "镜片",
      catalogOpen: true,
    });
    expect(loadContentReadingMemory("a")).toMatchObject({
      tab: "replies",
      scrollTopByTab: { replies: 120 },
      repliesQuery: "镜片",
      catalogOpen: true,
    });

    patchContentReadingMemory("a", {
      tab: "body",
      scrollTopByTab: { body: 40 },
    });
    expect(loadContentReadingMemory("a")).toMatchObject({
      tab: "body",
      scrollTopByTab: { replies: 120, body: 40 },
      repliesQuery: "镜片",
      catalogOpen: true,
    });
  });

  it("坏数据静默丢弃", () => {
    window.localStorage.setItem(
      "guizhi-content-reading-v1",
      JSON.stringify({ bad: { tab: "nope" }, ok: null }),
    );
    expect(loadContentReadingMemory("bad")).toBeNull();
  });

  it("超过 100 条按 updatedAt 淘汰最旧", () => {
    for (let i = 0; i < 105; i++) {
      saveContentReadingMemory(`item-${i}`, {
        tab: "body",
        scrollTopByTab: { body: i },
        updatedAt: i,
      });
    }
    expect(loadContentReadingMemory("item-0")).toBeNull();
    expect(loadContentReadingMemory("item-4")).toBeNull();
    expect(loadContentReadingMemory("item-104")?.scrollTopByTab.body).toBe(104);
  });
});
