import { describe, expect, it, vi } from "vitest";

// recordMainAiUsage 的日用量落库这里不关心，只验它有没有先做任务归属
vi.mock("../../../src/main/database", () => ({
  tryGetDatabase: () => null,
}));

import {
  reportAiCall,
  runWithAiCallSink,
  type AiCallRecord,
} from "../../../src/main/services/ai-call-context";
import { recordMainAiUsage } from "../../../src/main/services/ai-usage";

const tick = () => new Promise((resolve) => setTimeout(resolve, 1));

describe("AI 调用归属上下文", () => {
  it("作用域内的调用汇报给 sink，跨 await 仍然认得出来", async () => {
    const seen: AiCallRecord[] = [];

    await runWithAiCallSink(
      (record) => seen.push(record),
      async () => {
        reportAiCall({ model: "qwen3.5-flash" });
        await tick();
        // 真实链路里的每一次记账都发生在若干层 await 之后
        reportAiCall({ model: "qwen3.5-flash", failed: true });
      },
    );

    expect(seen).toEqual([
      { model: "qwen3.5-flash" },
      { model: "qwen3.5-flash", failed: true },
    ]);
  });

  it("作用域外是空操作：详情页手动触发的排版不该记到任何任务上", () => {
    expect(() => reportAiCall({ model: "qwen3.5-flash" })).not.toThrow();
  });

  it("并发的两个作用域互不串账——导入队列并发是 2，这条是整个机制的前提", async () => {
    const left: string[] = [];
    const right: string[] = [];

    await Promise.all([
      runWithAiCallSink(
        (record) => left.push(record.model),
        async () => {
          reportAiCall({ model: "任务甲-1" });
          await tick();
          reportAiCall({ model: "任务甲-2" });
        },
      ),
      runWithAiCallSink(
        (record) => right.push(record.model),
        async () => {
          await tick();
          reportAiCall({ model: "任务乙-1" });
        },
      ),
    ]);

    expect(left).toEqual(["任务甲-1", "任务甲-2"]);
    expect(right).toEqual(["任务乙-1"]);
  });

  it("sink 抛错不连累调用方：记的是观测数据，不该弄砸一次几十秒的生成", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    await runWithAiCallSink(
      () => {
        throw new Error("统计炸了");
      },
      async () => {
        expect(() => reportAiCall({ model: "qwen3.5-flash" })).not.toThrow();
      },
    );

    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });
});

describe("recordMainAiUsage 的归属顺序", () => {
  it("用量库不可用时仍然归属到任务：阶段统计只写在任务行上，不该跟着一起丢", async () => {
    const seen: AiCallRecord[] = [];

    await runWithAiCallSink(
      (record) => seen.push(record),
      async () => {
        recordMainAiUsage({
          scenario: "formatting",
          model: "qwen3.5-flash",
          promptTokens: 1358,
          completionTokens: 8271,
        });
      },
    );

    expect(seen).toHaveLength(1);
    expect(seen[0]).toMatchObject({
      model: "qwen3.5-flash",
      completionTokens: 8271,
    });
  });

  it("没有模型名的调用不记：那是 bug，不是一笔账", async () => {
    const seen: AiCallRecord[] = [];
    await runWithAiCallSink(
      (record) => seen.push(record),
      async () => {
        recordMainAiUsage({ scenario: "formatting", model: "" });
      },
    );
    expect(seen).toEqual([]);
  });
});
