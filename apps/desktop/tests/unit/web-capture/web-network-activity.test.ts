import { describe, expect, it } from "vitest";
import {
  WebNetworkActivity,
  affectsDocument,
} from "../../../src/main/services/web-capture/web-network-activity";

function clock(start = 1000) {
  let now = start;
  return { now: () => now, advance: (ms: number) => (now += ms) };
}

describe("请求活动跟踪", () => {
  it("样式表、图片与文档类（子框架、预取）不会改动主框架 DOM；脚本/XHR 等其余请求一律保守计入", () => {
    for (const kind of ["style", "image", "document"])
      expect(affectsDocument(kind), kind).toBe(false);
    expect(affectsDocument("other")).toBe(true);
    // 未来新增的类别默认按“可能改动 DOM”处理：宁可多等。
    expect(affectsDocument("something-new")).toBe(true);
  });

  it("没有任何请求时，静止时长从创建时刻起算", () => {
    const time = clock();
    const activity = new WebNetworkActivity(time.now);
    time.advance(250);
    expect(activity.quietMs()).toBe(250);
  });

  it("会改动 DOM 的请求在途时静止时长为 0，结束后重新从 0 起算", () => {
    const time = clock();
    const activity = new WebNetworkActivity(time.now);
    time.advance(500);
    const end = activity.begin("other");
    time.advance(700);
    expect(activity.quietMs()).toBe(0);
    end();
    expect(activity.quietMs()).toBe(0);
    time.advance(300);
    expect(activity.quietMs()).toBe(300);
  });

  it("多个请求并发时，要全部结束才算静止", () => {
    const time = clock();
    const activity = new WebNetworkActivity(time.now);
    const first = activity.begin("other");
    const second = activity.begin("other");
    time.advance(100);
    first();
    time.advance(100);
    expect(activity.quietMs()).toBe(0);
    second();
    time.advance(50);
    expect(activity.quietMs()).toBe(50);
  });

  it("不会改动 DOM 的在途请求不影响静止判断，也不刷新静止计时", () => {
    const time = clock();
    const activity = new WebNetworkActivity(time.now);
    time.advance(400);
    const inert = ["image", "style", "document"].map((kind) => activity.begin(kind));
    time.advance(900);
    expect(activity.quietMs()).toBe(1300);
    for (const end of inert) end();
    time.advance(10);
    expect(activity.quietMs()).toBe(1310);
  });

  it("结束函数幂等：重复调用不会让计数变成负数或提前静止", () => {
    const time = clock();
    const activity = new WebNetworkActivity(time.now);
    const first = activity.begin("other");
    const second = activity.begin("other");
    first();
    first();
    time.advance(100);
    expect(activity.quietMs()).toBe(0);
    second();
    expect(activity.inflight()).toEqual({});
  });

  it("在途请求按类别计数（含不影响判定的类别），结束后移除", () => {
    const activity = new WebNetworkActivity(clock().now);
    const ends = [
      activity.begin("image"),
      activity.begin("image"),
      activity.begin("other"),
      activity.begin("document"),
    ];
    expect(activity.inflight()).toEqual({ image: 2, other: 1, document: 1 });
    ends[0]();
    ends[2]();
    expect(activity.inflight()).toEqual({ image: 1, document: 1 });
  });

  describe("阻止静止的请求（诊断）", () => {
    it("只列会改动 DOM 的在途请求，最久的在前，带类别、origin 与在途时长", () => {
      const time = clock();
      const activity = new WebNetworkActivity(time.now);
      activity.begin("other", "https://api.example.com");
      time.advance(200);
      activity.begin("image", "https://img.example.com");
      activity.begin("document", "https://frame.example.com");
      time.advance(100);
      activity.begin("other", "https://cdn.example.com");
      time.advance(50);
      expect(activity.blockers()).toEqual([
        { kind: "other", origin: "https://api.example.com", ageMs: 350 },
        { kind: "other", origin: "https://cdn.example.com", ageMs: 50 },
      ]);
    });

    it("请求结束后移除；未登记 origin 时不输出该字段", () => {
      const time = clock();
      const activity = new WebNetworkActivity(time.now);
      const end = activity.begin("other");
      expect(activity.blockers()).toEqual([{ kind: "other", ageMs: 0 }]);
      end();
      expect(activity.blockers()).toEqual([]);
    });

    it("数量受 limit 限制", () => {
      const activity = new WebNetworkActivity(clock().now);
      for (let index = 0; index < 8; index++) activity.begin("other");
      expect(activity.blockers()).toHaveLength(5);
      expect(activity.blockers(2)).toHaveLength(2);
    });
  });
});
